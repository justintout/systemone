package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"runtime"
	"slices"
	"time"

	"github.com/justintout/systemone/internal/bridge"
	"github.com/klauspost/compress/zstd"
)

// NoTimeout removes the per-attempt bound from a call when given as a
// [Request.Timeout]. Any negative duration does the same.
const NoTimeout = -1 * time.Nanosecond

// Client sends requests to a deployment of the base System One API: text
// state, and Choice, Noul and Score questions. It is safe for concurrent use.
//
// Use it for a server that implements the API itself. TypeSafe's Jev and
// Cloudflare's Clef extend the API, and their clients, in packages jev and
// clef, configure this one and add those extensions.
type Client struct {
	http    *http.Client
	baseURL *url.URL
	apiKey  string
	model   string
	header  http.Header
	// Paths, envelope and request ID header of the deployment.
	evaluatePath string
	modelsPath   string
	resultKey    string
	requestIDHdr string
	retry        RetryPolicy
	timeout      time.Duration
	zstd         *zstd.Encoder
	userAgent    string
	logger       logger
	now          func() time.Time
}

// New builds a client. A deployment has no default, so [WithAPIKey],
// [WithBaseURL] and [WithModel] are required:
//
//	c, err := systemone.New(
//		systemone.WithBaseURL("https://decisions.internal"),
//		systemone.WithModel("support-ft"),
//		systemone.WithAPIKey(key),
//	)
//
// Evaluations are posted to /v1/systemone and models are listed from
// /v1/models, the paths the API defines, unless [WithEvaluatePath] and
// [WithModelsPath] say otherwise.
func New(opts ...ClientOption) (*Client, error) {
	c := &Client{
		http:         &http.Client{},
		header:       make(http.Header),
		evaluatePath: "/v1/systemone",
		modelsPath:   "/v1/models",
		timeout:      DefaultTimeout,
		userAgent:    fmt.Sprintf("go-systemone/%s (%s; %s)", version(), runtime.Version(), runtime.GOOS),
		logger:       logger{level: slog.LevelInfo},
		now:          time.Now,
	}
	for _, opt := range opts {
		if err := opt.apply(c); err != nil {
			return nil, err
		}
	}
	switch {
	case c.apiKey == "":
		return nil, ErrNoAPIKey
	case c.baseURL == nil:
		return nil, errors.New("systemone: no base URL; see WithBaseURL")
	case c.model == "":
		return nil, errors.New("systemone: no model; see WithModel")
	}
	return c, nil
}

// Model returns the model used for requests that do not name one.
func (c *Client) Model() string { return c.model }

// Request is one evaluation: a state, the questions to ask about it, and how to
// send them.
type Request struct {
	// State is the content to evaluate: a string, or any value that encodes to
	// a JSON object or array. See https://docs.typesafe.ai/concepts/state.
	State any
	// Questions are asked in one call and answered in parallel. They cannot see
	// one another's answers.
	Questions []Question
	// Model overrides the client default for this request.
	Model string
	// Header adds or replaces headers for this request. Authorization, Accept,
	// Content-Type, Content-Encoding and User-Agent are set by the client after
	// this is applied, so they cannot be overridden here; see [WithUserAgent].
	Header http.Header
	// Retry overrides the client's retry policy for this request. A nil Retry
	// inherits it; a zero RetryPolicy turns retries off for this call alone.
	Retry *RetryPolicy
	// Timeout overrides the client's per-attempt timeout for this request. Zero
	// inherits it, and [NoTimeout] removes the bound. To bound the whole call
	// including retries, give ctx a deadline.
	Timeout time.Duration
}

// Asker asks questions about a state. [Client] and the clients in packages jev
// and clef all satisfy it, for code that chooses a deployment at run time.
type Asker interface {
	Ask(ctx context.Context, state any, questions ...Question) (*Response, error)
}

func init() {
	bridge.RegisterDo(func(ctx context.Context, c *Client, req Request, ext bridge.Extension) (*Response, error) {
		return c.do(ctx, req, ext)
	})
	bridge.Kind = func(q any) string { return q.(Question).wireType() }
}

// Ask evaluates state against questions using the client's default model. It is
// [Client.Do] for the common case.
func (c *Client) Ask(ctx context.Context, state any, questions ...Question) (*Response, error) {
	return c.Do(ctx, Request{State: state, Questions: questions})
}

// Do sends one evaluation request.
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	return c.do(ctx, req, bridge.Extension{})
}

func (c *Client) do(ctx context.Context, req Request, ext bridge.Extension) (*Response, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}
	encoded, err := encodeRequest(req, model, ext)
	if err != nil {
		return nil, err
	}
	call := call{
		method:      http.MethodPost,
		endpoint:    c.evaluateEndpoint(model),
		body:        encoded,
		logBody:     encoded,
		contentType: "application/json",
		header:      req.Header,
		retry:       req.Retry,
		timeout:     req.Timeout,
	}
	if ext.Wrap != nil {
		if call.body, call.contentType, err = ext.Wrap(encoded); err != nil {
			return nil, err
		}
	}
	if c.zstd != nil && len(call.body) >= compressionThreshold {
		call.body = c.zstd.EncodeAll(call.body, nil)
		call.contentEncoding = "zstd"
	}
	res, err := c.send(ctx, call)
	if err != nil {
		return nil, err
	}
	payload, err := unwrap(res.body, c.resultKey)
	if err != nil {
		return nil, err
	}
	out, err := decodeResponse(payload)
	if err != nil {
		return nil, fmt.Errorf("systemone: decoding response: %w", err)
	}
	out.RequestID = res.requestID
	return out, nil
}

// Model describes a model or alias the account may send in a request.
type Model struct {
	Name        string
	Description string
	// ReleaseDate is when the model or alias was released. It is the zero time
	// when the API omits it.
	//
	// The API sends RFC 3339, for example 2026-09-10T18:38:01.391457+00:00,
	// despite the SDK documentation showing a bare YYYY-MM-DD date.
	ReleaseDate time.Time
}

// UnmarshalJSON decodes a model entry, parsing its release date, which the API
// sends as an RFC 3339 timestamp.
func (m *Model) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		ReleaseDate string `json:"release_date"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Name, m.Description = raw.Name, raw.Description
	if raw.ReleaseDate == "" {
		m.ReleaseDate = time.Time{}
		return nil
	}
	released, err := time.Parse(time.RFC3339, raw.ReleaseDate)
	if err != nil {
		return fmt.Errorf("release date of model %q: %w", raw.Name, err)
	}
	m.ReleaseDate = released
	return nil
}

// Models lists the models the account can use. See
// https://docs.typesafe.ai/models.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	if c.modelsPath == "" {
		return nil, ErrNoModelList
	}
	res, err := c.send(ctx, call{method: http.MethodGet, endpoint: c.baseURL.JoinPath(c.modelsPath).String()})
	if err != nil {
		return nil, err
	}
	var body struct {
		Models []Model `json:"models"`
	}
	if err := json.Unmarshal(res.body, &body); err != nil {
		return nil, fmt.Errorf("systemone: decoding models: %w", err)
	}
	return body.Models, nil
}

func encodeRequest(req Request, model string, ext bridge.Extension) ([]byte, error) {
	if req.State == nil {
		return nil, errors.New("systemone: request has no state")
	}
	if len(req.Questions) == 0 {
		return nil, errors.New("systemone: request has no questions")
	}
	qs := make(pairs, 0, len(req.Questions))
	seen := make(map[string]bool, len(req.Questions))
	for _, q := range req.Questions {
		id := q.ID()
		if seen[id] {
			return nil, &QuestionError{QuestionID: id, Err: errors.New("asked twice in one request")}
		}
		seen[id] = true
		w, err := q.encode()
		if err != nil {
			return nil, &QuestionError{QuestionID: id, Err: err}
		}
		qs = append(qs, pair{key: id, value: w})
	}
	body, err := json.Marshal(pairs{{key: "state", value: req.State}, {key: "model", value: model}, {key: "questions", value: qs}})
	if err != nil {
		return nil, err
	}
	switch {
	case ext.Check != nil:
		err = ext.Check(body)
	case bytes.Contains(body, []byte(bridge.ImageRefPrefix)):
		err = fmt.Errorf("%w: a jev.Image can only be sent by a jev client", ErrImage)
	}
	if err != nil || len(ext.Fields) == 0 {
		return body, err
	}
	extra := make(pairs, 0, len(ext.Fields))
	for _, k := range slices.Sorted(maps.Keys(ext.Fields)) {
		extra = append(extra, pair{key: k, value: ext.Fields[k]})
	}
	fields, err := json.Marshal(extra)
	if err != nil {
		return nil, err
	}
	// Both are JSON objects, so the extra fields splice in before the closing
	// brace and the state is encoded once.
	return append(append(body[:len(body)-1], ','), fields[1:]...), nil
}

// requestID reads the request ID the deployment names, empty when it names
// none or the response never arrived.
func (c *Client) requestID(resp *http.Response) string {
	if resp == nil || c.requestIDHdr == "" {
		return ""
	}
	return resp.Header.Get(c.requestIDHdr)
}

// call is one API call and the per-request overrides that apply to it.
type call struct {
	method          string
	endpoint        string
	body            []byte
	contentType     string
	contentEncoding string
	// logBody is the JSON request, which is what a debug log can show; body
	// may be multipart or compressed.
	logBody []byte
	header  http.Header
	retry   *RetryPolicy
	timeout time.Duration
}

func (c *Client) policy(call call) RetryPolicy {
	if call.retry != nil {
		return *call.retry
	}
	return c.retry
}

func (c *Client) attemptTimeout(call call) time.Duration {
	switch {
	case call.timeout < 0:
		return 0
	case call.timeout > 0:
		return call.timeout
	}
	return c.timeout
}

type rawResponse struct {
	body      []byte
	requestID string
}

// send performs one API call, retrying it when the applicable policy allows.
func (c *Client) send(ctx context.Context, call call) (rawResponse, error) {
	policy := c.policy(call)
	var lastErr error
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, retryDelay(policy, attempt-1, lastErr)); err != nil {
				return rawResponse{}, err
			}
		}
		res, err := c.attempt(ctx, call, attempt)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			// The caller's context is done, so stop rather than sleeping on it.
			// err is whatever the attempt produced, which is the context error
			// for a cancelled transport but an *Error for a response that was
			// read in full before the cancellation landed.
			return rawResponse{}, err
		}
		if attempt >= policy.MaxRetries || !retryable(policy, err) {
			return rawResponse{}, err
		}
	}
}

func (c *Client) attempt(ctx context.Context, call call, attempt int) (rawResponse, error) {
	if timeout := c.attemptTimeout(call); timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var reader io.Reader
	if call.body != nil {
		reader = bytes.NewReader(call.body)
	}
	req, err := http.NewRequestWithContext(ctx, call.method, call.endpoint, reader)
	if err != nil {
		return rawResponse{}, fmt.Errorf("systemone: building request: %w", err)
	}
	for k, vs := range c.header {
		req.Header[k] = vs
	}
	for k, vs := range call.header {
		req.Header[http.CanonicalHeaderKey(k)] = vs
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if call.contentType != "" {
		req.Header.Set("Content-Type", call.contentType)
	}
	if call.contentEncoding != "" {
		req.Header.Set("Content-Encoding", call.contentEncoding)
	}

	start := c.now()
	resp, err := c.http.Do(req)
	if err != nil {
		err = fmt.Errorf("systemone: %s %s: %w", call.method, call.endpoint, err)
		c.logger.attempt(ctx, call.method, call.endpoint, attempt, start, req, call.logBody, nil, nil, "", err)
		return rawResponse{}, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		err = fmt.Errorf("systemone: reading response: %w", err)
		c.logger.attempt(ctx, call.method, call.endpoint, attempt, start, req, call.logBody, resp, nil, c.requestID(resp), err)
		return rawResponse{}, err
	}
	id := c.requestID(resp)
	c.logger.attempt(ctx, call.method, call.endpoint, attempt, start, req, call.logBody, resp, data, id, nil)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return rawResponse{}, &Error{
			StatusCode: resp.StatusCode,
			RequestID:  id,
			Body:       data,
			Header:     resp.Header,
			RetryAfter: retryAfter(resp.Header, c.now()),
		}
	}
	return rawResponse{body: data, requestID: id}, nil
}

func retryable(policy RetryPolicy, err error) bool {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return policy.retryStatus(apiErr.StatusCode)
	}
	return policy.RetryTransportError
}

func retryDelay(policy RetryPolicy, attempt int, err error) time.Duration {
	if policy.RespectRetryAfter {
		var apiErr *Error
		if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
			return apiErr.RetryAfter
		}
	}
	return policy.backoff(attempt)
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
