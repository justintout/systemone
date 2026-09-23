package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"slices"
	"time"
)

const (
	evaluatePath = "/v1/systemone"
	modelsPath   = "/v1/models"
	requestIDHdr = "X-Typesafe-Request-Id"
)

// NoTimeout removes the per-attempt bound from a call when given as a
// [Request.Timeout]. Any negative duration does the same.
const NoTimeout = -1 * time.Nanosecond

// Client sends requests to the System One API. It is safe for concurrent use.
type Client struct {
	http      *http.Client
	baseURL   *url.URL
	apiKey    string
	model     string
	header    http.Header
	retry     RetryPolicy
	timeout   time.Duration
	userAgent string
	logger    logger
	now       func() time.Time
}

// New builds a client. Without options it reads the API key from APIKeyEnv, the
// base URL from BaseURLEnv, and the default model from DefaultModelEnv.
func New(opts ...ClientOption) (*Client, error) {
	c := &Client{
		http:      &http.Client{},
		apiKey:    os.Getenv(APIKeyEnv),
		model:     DefaultModel,
		header:    make(http.Header),
		timeout:   DefaultTimeout,
		userAgent: fmt.Sprintf("go-systemone/%s (%s; %s)", version(), runtime.Version(), runtime.GOOS),
		logger:    logger{level: slog.LevelInfo},
		now:       time.Now,
	}
	base := DefaultBaseURL
	if v := os.Getenv(BaseURLEnv); v != "" {
		base = v
	}
	if err := WithBaseURL(base).apply(c); err != nil {
		return nil, err
	}
	if v := os.Getenv(DefaultModelEnv); v != "" {
		c.model = v
	}
	if v := os.Getenv(LogLevelEnv); v != "" {
		level, ok := parseLogLevel(v)
		if !ok {
			return nil, fmt.Errorf("systemone: %s=%q is not a level", LogLevelEnv, v)
		}
		c.logger.level = level
	}
	for _, opt := range opts {
		if err := opt.apply(c); err != nil {
			return nil, err
		}
	}
	if c.apiKey == "" {
		return nil, ErrNoAPIKey
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
	// Content-Type and User-Agent are set by the client after this is applied,
	// so they cannot be overridden here; see [WithUserAgent].
	Header http.Header
	// Retry overrides the client's retry policy for this request. A nil Retry
	// inherits it; a zero RetryPolicy turns retries off for this call alone.
	Retry *RetryPolicy
	// Timeout overrides the client's per-attempt timeout for this request. Zero
	// inherits it, and [NoTimeout] removes the bound. To bound the whole call
	// including retries, give ctx a deadline.
	Timeout time.Duration
	// Extra adds top-level fields to the request body, for API features this
	// package does not model yet. A key that collides with state, model or
	// questions is an error rather than an override. Prefer upgrading the SDK.
	Extra map[string]any
}

type requestBody struct {
	State     any    `json:"state"`
	Model     string `json:"model"`
	Questions pairs  `json:"questions"`
}

// reserved names the top-level fields [Request.Extra] may not set.
var reserved = []string{"state", "model", "questions"}

// Ask evaluates state against questions using the client's default model. It is
// [Client.Do] for the common case.
func (c *Client) Ask(ctx context.Context, state any, questions ...Question) (*Response, error) {
	return c.Do(ctx, Request{State: state, Questions: questions})
}

// Do sends one evaluation request.
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	body, err := c.encodeRequest(req)
	if err != nil {
		return nil, err
	}
	res, err := c.send(ctx, call{
		method:  http.MethodPost,
		path:    evaluatePath,
		body:    body,
		header:  req.Header,
		retry:   req.Retry,
		timeout: req.Timeout,
	})
	if err != nil {
		return nil, err
	}
	out, err := decodeResponse(res.body)
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
	res, err := c.send(ctx, call{method: http.MethodGet, path: modelsPath})
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

func (c *Client) encodeRequest(req Request) ([]byte, error) {
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
	model := req.Model
	if model == "" {
		model = c.model
	}
	body, err := json.Marshal(requestBody{State: req.State, Model: model, Questions: qs})
	if err != nil {
		return nil, err
	}
	if len(req.Extra) == 0 {
		return body, nil
	}
	return mergeExtra(body, req.Extra)
}

// mergeExtra adds Request.Extra to an encoded request body.
func mergeExtra(body []byte, extra map[string]any) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	for key, value := range extra {
		if slices.Contains(reserved, key) {
			return nil, fmt.Errorf("systemone: extra field %q is part of the request body", key)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("systemone: extra field %q: %w", key, err)
		}
		fields[key] = encoded
	}
	return json.Marshal(fields)
}

// call is one API call and the per-request overrides that apply to it.
type call struct {
	method  string
	path    string
	body    []byte
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
	endpoint := c.baseURL.JoinPath(call.path).String()
	policy := c.policy(call)
	var lastErr error
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, retryDelay(policy, attempt-1, lastErr)); err != nil {
				return rawResponse{}, err
			}
		}
		res, err := c.attempt(ctx, endpoint, call, attempt)
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

func (c *Client) attempt(ctx context.Context, endpoint string, call call, attempt int) (rawResponse, error) {
	if timeout := c.attemptTimeout(call); timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var reader io.Reader
	if call.body != nil {
		reader = bytes.NewReader(call.body)
	}
	req, err := http.NewRequestWithContext(ctx, call.method, endpoint, reader)
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
	if call.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	start := c.now()
	resp, err := c.http.Do(req)
	if err != nil {
		err = fmt.Errorf("systemone: %s %s: %w", call.method, endpoint, err)
		c.logger.attempt(ctx, call.method, endpoint, attempt, start, req, call.body, nil, nil, err)
		return rawResponse{}, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		err = fmt.Errorf("systemone: reading response: %w", err)
		c.logger.attempt(ctx, call.method, endpoint, attempt, start, req, call.body, resp, nil, err)
		return rawResponse{}, err
	}
	c.logger.attempt(ctx, call.method, endpoint, attempt, start, req, call.body, resp, data, nil)

	id := resp.Header.Get(requestIDHdr)
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
