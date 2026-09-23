package systemone_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justintout/systemone"
)

type Dept string

const (
	Billing   Dept = "billing"
	Technical Dept = "technical"
	Sales     Dept = "sales"
)

type Frustration int

const (
	Calm Frustration = iota
	Frustrated
	VeryAngry
)

var (
	dept = systemone.NewChoice[Dept]("department", "Which team should handle this?",
		systemone.Option(Billing, "Payments, invoicing, refunds"),
		systemone.Option(Technical, "Bugs, outages, integrations"),
		systemone.Option(Sales, nil),
	)
	urgent = systemone.NewNoul("is_urgent", "Does this convey urgency?",
		systemone.Yes("Explicitly time-sensitive"),
		systemone.No("No urgency expressed"),
	)
	frustration = systemone.NewScore[Frustration]("frustration", "How frustrated is the customer?",
		systemone.Levels[Frustration]("Calm", "Frustrated", "Very angry"),
	)
)

// answersJSON answers all three questions above. It is the shape the API
// reference documents.
const answersJSON = `{
  "model": "jev-1.13.0",
  "answers": {
    "department": {"type":"choice","choice":"billing","probabilities":{"billing":0.88,"technical":0.12,"sales":0.0},"confidence":0.81},
    "is_urgent": {"type":"noul","noul":0.95},
    "frustration": {"type":"score","score":1.05,"legend":{"0":"Calm","1":"Frustrated","2":"Very angry"},"probabilities":{"0":0.0,"1":0.95,"2":0.05},"confidence":0.92}
  },
  "usage": {"input_tokens": 296, "output_tokens": 20}
}`

// recorder keeps the last request a stub server received.
type recorder struct {
	Request *http.Request
	Body    []byte
	Calls   atomic.Int32
}

// stub starts an API stub that replies with reply, and returns a client aimed
// at it alongside the recorder holding what it received.
func stub(t *testing.T, reply string, opts ...systemone.ClientOption) (*systemone.Client, *recorder) {
	t.Helper()
	rec := &recorder{}
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		rec.Calls.Add(1)
		rec.Body, _ = io.ReadAll(r.Body)
		rec.Request = r
		_, _ = io.WriteString(w, reply)
	}, opts...), rec
}

func serve(t *testing.T, h http.HandlerFunc, opts ...systemone.ClientOption) *systemone.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	all := append([]systemone.ClientOption{
		systemone.WithAPIKey("test-key"),
		systemone.WithBaseURL(srv.URL),
	}, opts...)
	c, err := systemone.New(all...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// field returns one top-level field of the recorded request body, undecoded, so
// a test can compare against the wire format byte for byte.
func (r *recorder) field(t *testing.T, name string) string {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(r.Body, &body); err != nil {
		t.Fatalf("request body: %v", err)
	}
	return string(body[name])
}

// Questions must serialize exactly as the API reference documents, including
// the order of Choice options, which a Go map would shuffle.
func TestQuestionWireFormat(t *testing.T) {
	tests := []struct {
		name     string
		question systemone.Question
		want     string
	}{{
		name:     "noul",
		question: systemone.NewNoul("q", "Does this convey urgency?"),
		want:     `{"q":{"type":"noul","instructions":"Does this convey urgency?"}}`,
	}, {
		name: "noul with criteria",
		question: systemone.NewNoul("q", "Urgent?",
			systemone.Yes("Time-sensitive"), systemone.No("Not time-sensitive")),
		want: `{"q":{"type":"noul","instructions":"Urgent?","criteria":{"true":"Time-sensitive","false":"Not time-sensitive"}}}`,
	}, {
		name: "choice keeps option order and sends null for an undescribed option",
		question: systemone.NewChoice[Dept]("q", "Which team?",
			systemone.Option(Billing, "Payments"),
			systemone.Option(Technical, "Bugs"),
			systemone.Option(Sales, nil)),
		want: `{"q":{"type":"choice","instructions":"Which team?","criteria":{"billing":"Payments","technical":"Bugs","sales":null}}}`,
	}, {
		name:     "choice from bare options",
		question: systemone.NewChoice[Dept]("q", "Which team?", systemone.Options(Billing, Sales)),
		want:     `{"q":{"type":"choice","instructions":"Which team?","criteria":{"billing":null,"sales":null}}}`,
	}, {
		name: "score levels are an ordered array",
		question: systemone.NewScore[Frustration]("q", "How frustrated?",
			systemone.Levels[Frustration]("Calm", "Frustrated", "Very angry")),
		want: `{"q":{"type":"score","instructions":"How frustrated?","criteria":["Calm","Frustrated","Very angry"]}}`,
	}, {
		name: "structured instructions and criteria pass through",
		question: systemone.NewNoul("q",
			map[string]any{"question": "Same person as `candidate`?"},
			systemone.Yes(map[string]any{"what": "A match"})),
		want: `{"q":{"type":"noul","instructions":{"question":"Same person as ` + "`candidate`" + `?"},"criteria":{"true":{"what":"A match"}}}}`,
	}, {
		name: "raw question is sent as given",
		question: systemone.RawQuestion{QuestionID: "q", Body: map[string]any{
			"type": "tally", "instructions": "Count them",
		}},
		want: `{"q":{"instructions":"Count them","type":"tally"}}`,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := stub(t, answersJSON)
			if _, err := c.Ask(context.Background(), "state", tt.question); err != nil {
				t.Fatal(err)
			}
			if got := rec.field(t, "questions"); got != tt.want {
				t.Errorf("questions =\n %s\nwant\n %s", got, tt.want)
			}
		})
	}
}

func TestRequestTransport(t *testing.T) {
	c, rec := stub(t, answersJSON)
	if _, err := c.Ask(context.Background(), "state", dept); err != nil {
		t.Fatal(err)
	}

	req := rec.Request
	for _, tt := range []struct{ name, got, want string }{
		{"path", req.URL.Path, "/v1/systemone"},
		{"method", req.Method, http.MethodPost},
		{"auth", req.Header.Get("Authorization"), "Bearer test-key"},
		{"content type", req.Header.Get("Content-Type"), "application/json"},
		{"state", rec.field(t, "state"), `"state"`},
		{"model", rec.field(t, "model"), `"` + systemone.DefaultModel + `"`},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
	if ua := req.Header.Get("User-Agent"); !strings.HasPrefix(ua, "go-systemone/") {
		t.Errorf("User-Agent = %q", ua)
	}

	// A request may override the client's model.
	if _, err := c.Do(context.Background(), systemone.Request{
		State: "state", Questions: []systemone.Question{dept}, Model: "jev-1.13.0",
	}); err != nil {
		t.Fatal(err)
	}
	if got := rec.field(t, "model"); got != `"jev-1.13.0"` {
		t.Errorf("overridden model = %s", got)
	}
}

func TestTypedAnswers(t *testing.T) {
	c, _ := stub(t, answersJSON)
	res, err := c.Ask(context.Background(), "state", dept, urgent, frustration)
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != "jev-1.13.0" || res.Usage.InputTokens != 296 {
		t.Errorf("response = %+v", res)
	}

	d, err := dept.From(res)
	if err != nil {
		t.Fatal(err)
	}
	if d.Value != Billing || d.Confidence != 0.81 {
		t.Errorf("choice = %+v", d)
	}
	if got := d.Probability(Technical); got != 0.12 {
		t.Errorf("Probability(Technical) = %v", got)
	}
	if got := d.Ranked(); got[0] != Billing || got[len(got)-1] != Sales {
		t.Errorf("Ranked() = %v", got)
	}

	u, err := urgent.From(res)
	if err != nil {
		t.Fatal(err)
	}
	if !u.Yes(0.9) || u.Yes(0.99) {
		t.Errorf("noul = %+v", u)
	}

	f, err := frustration.From(res)
	if err != nil {
		t.Fatal(err)
	}
	if f.Level != Frustrated || f.Describe() != "Frustrated" {
		t.Errorf("score level = %v %q", f.Level, f.Describe())
	}
	if got := f.Probability(VeryAngry); got != 0.05 {
		t.Errorf("Probability(VeryAngry) = %v", got)
	}
	if got := f.Normalized(); got < 0.52 || got > 0.53 {
		t.Errorf("Normalized() = %v, want ~0.525", got)
	}
}

// Declared sets are readable off the question, which a caller needs to drive a
// UI or build a follow-up question.
func TestQuestionAccessors(t *testing.T) {
	if got := dept.Options(); len(got) != 3 || got[0] != Billing || got[2] != Sales {
		t.Errorf("Options() = %v", got)
	}
	if got := frustration.Levels(); got != 3 {
		t.Errorf("Levels() = %d, want 3", got)
	}
}

// A Score level declared with a structured description comes back structured.
func TestStructuredScoreLegend(t *testing.T) {
	const reply = `{"model":"jev-1.13.0","answers":{"scope":{"type":"score","score":1.0,
	  "legend":{"0":{"summary":"One change"},"1":{"summary":"Several changes"}},
	  "probabilities":{"0":0.1,"1":0.9},"confidence":0.8}},
	  "usage":{"input_tokens":1,"output_tokens":1}}`

	type Scope int
	scope := systemone.NewScore[Scope]("scope", "How focused is this pull request?",
		systemone.Level(Scope(0), map[string]any{"summary": "One change"}),
		systemone.Level(Scope(1), map[string]any{"summary": "Several changes"}),
	)
	c, _ := stub(t, reply)
	res, err := c.Ask(context.Background(), "diff", scope)
	if err != nil {
		t.Fatal(err)
	}
	a, err := scope.From(res)
	if err != nil {
		t.Fatal(err)
	}
	level, ok := a.Legend[1].(map[string]any)
	if !ok {
		t.Fatalf("legend[1] = %#v, want a structured description", a.Legend[1])
	}
	if level["summary"] != "Several changes" {
		t.Errorf("legend[1] = %v", level)
	}
	if want := `{"summary":"Several changes"}`; a.Describe() != want {
		t.Errorf("Describe() = %q, want %q", a.Describe(), want)
	}
}

func TestAnswerLookupErrors(t *testing.T) {
	c, _ := stub(t, answersJSON)
	res, err := c.Ask(context.Background(), "state", dept)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		read func() error
		want error
	}{{
		name: "no answer under that id",
		read: func() error { _, err := systemone.NewNoul("absent", "Anything?").From(res); return err },
		want: systemone.ErrNoAnswer,
	}, {
		name: "id answered by a different primitive",
		read: func() error { _, err := systemone.NewNoul("department", "Anything?").From(res); return err },
		want: systemone.ErrAnswerType,
	}, {
		name: "raw question with no answer",
		read: func() error { _, err := systemone.RawQuestion{QuestionID: "absent"}.From(res); return err },
		want: systemone.ErrNoAnswer,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.read()
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			var qerr *systemone.QuestionError
			if !errors.As(err, &qerr) || !strings.Contains(err.Error(), qerr.QuestionID) {
				t.Errorf("error should name the question: %v", err)
			}
		})
	}
}

// A question that cannot be sent as built fails the call rather than the
// constructor, so questions can be package-level variables.
func TestInvalidQuestionsFailTheCall(t *testing.T) {
	tests := []struct {
		name     string
		question systemone.Question
	}{
		{"duplicate option", systemone.NewChoice[Dept]("q", "Pick",
			systemone.Option(Billing, nil), systemone.Option(Billing, nil))},
		{"no options", systemone.NewChoice[Dept]("q", "Pick")},
		{"empty option name", systemone.NewChoice[Dept]("q", "Pick", systemone.Option(Dept(""), nil))},
		{"empty instructions", systemone.NewNoul("q", "")},
		{"empty id", systemone.NewNoul("", "Anything?")},
		{"criterion declared twice", systemone.NewNoul("q", "Urgent?",
			systemone.Yes("a"), systemone.Yes("b"))},
		{"level out of order", systemone.NewScore[Frustration]("q", "Rate",
			systemone.Level(Calm, "Calm"), systemone.Level(VeryAngry, "Very angry"))},
		{"level with no description", systemone.NewScore[Frustration]("q", "Rate",
			systemone.Level(Calm, ""), systemone.Level(Frustrated, "Frustrated"))},
		{"too few levels", systemone.NewScore[Frustration]("q", "Rate",
			systemone.Level(Calm, "Calm"))},
		{"raw question with no body", systemone.RawQuestion{QuestionID: "q"}},
		{"raw question with no id", systemone.RawQuestion{Body: map[string]any{"type": "noul"}}},
	}

	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("an invalid question reached the network")
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var qerr *systemone.QuestionError
			if _, err := c.Ask(context.Background(), "state", tt.question); !errors.As(err, &qerr) {
				t.Fatalf("err = %v, want *QuestionError", err)
			}
		})
	}
}

func TestInvalidRequestsFailBeforeSending(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("an invalid request reached the network")
	})
	tests := []struct {
		name string
		req  systemone.Request
	}{
		{"no state", systemone.Request{Questions: []systemone.Question{dept}}},
		{"no questions", systemone.Request{State: "state"}},
		{"one question asked twice", systemone.Request{
			State: "state", Questions: []systemone.Question{dept, dept}}},
		{"extra field collides with the body", systemone.Request{
			State: "state", Questions: []systemone.Question{dept},
			Extra: map[string]any{"questions": "nope"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := c.Do(context.Background(), tt.req); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestHTTPErrorsClassify(t *testing.T) {
	tests := []struct {
		status  int
		matches []error
		misses  []error
	}{
		{http.StatusBadRequest, []error{systemone.ErrInvalidRequest}, []error{systemone.ErrServer}},
		{http.StatusUnauthorized, []error{systemone.ErrAuthentication}, []error{systemone.ErrPermissionDenied}},
		{http.StatusForbidden, []error{systemone.ErrPermissionDenied}, nil},
		{http.StatusNotFound, []error{systemone.ErrNotFound}, nil},
		{http.StatusUnprocessableEntity, []error{systemone.ErrUnprocessable}, []error{systemone.ErrInvalidRequest}},
		{http.StatusTooManyRequests, []error{systemone.ErrRateLimit}, []error{systemone.ErrServer}},
		{http.StatusInternalServerError, []error{systemone.ErrServer}, []error{systemone.ErrOverloaded}},
		{529, []error{systemone.ErrOverloaded, systemone.ErrServer}, []error{systemone.ErrRateLimit}},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			c := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Typesafe-Request-Id", "req_123")
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, `{"detail":"nope"}`)
			})
			_, err := c.Ask(context.Background(), "state", dept)
			for _, want := range tt.matches {
				if !errors.Is(err, want) {
					t.Errorf("err = %v, want %v", err, want)
				}
			}
			for _, unwanted := range tt.misses {
				if errors.Is(err, unwanted) {
					t.Errorf("err = %v, should not match %v", err, unwanted)
				}
			}
			var apiErr *systemone.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *Error", err)
			}
			if apiErr.StatusCode != tt.status || apiErr.RequestID != "req_123" {
				t.Errorf("error = %+v", apiErr)
			}
			if !strings.Contains(err.Error(), "nope") {
				t.Errorf("body missing from %q", err)
			}
		})
	}
}

func TestRetryAfterHeader(t *testing.T) {
	future := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	tests := []struct {
		name   string
		header string
		want   time.Duration
		approx bool
	}{
		{name: "absent"},
		{name: "seconds", header: "2", want: 2 * time.Second},
		{name: "fractional seconds", header: "0.5", want: 500 * time.Millisecond},
		{name: "http date", header: future, want: 90 * time.Second, approx: true},
		{name: "past date", header: "Mon, 02 Jan 2006 15:04:05 GMT"},
		{name: "zero", header: "0"},
		{name: "negative", header: "-5"},
		{name: "unparseable", header: "soon"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if tt.header != "" {
					w.Header().Set("Retry-After", tt.header)
				}
				w.WriteHeader(http.StatusTooManyRequests)
			})
			_, err := c.Ask(context.Background(), "state", dept)
			var apiErr *systemone.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v", err)
			}
			got := apiErr.RetryAfter
			if tt.approx {
				if got < tt.want-5*time.Second || got > tt.want {
					t.Errorf("RetryAfter = %v, want about %v", got, tt.want)
				}
				return
			}
			if got != tt.want {
				t.Errorf("RetryAfter = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRetries(t *testing.T) {
	fast := systemone.DefaultRetry()
	fast.InitialBackoff = time.Millisecond

	noRetryOn500 := fast
	noRetryOn500.Retry = func(status int) bool { return status == http.StatusTooManyRequests }

	tests := []struct {
		name     string
		policy   *systemone.RetryPolicy // nil leaves the client at its default
		perCall  *systemone.RetryPolicy
		failures int32
		want     int32
		wantErr  bool
	}{
		{name: "off by default", failures: 1, want: 1, wantErr: true},
		{name: "client policy retries", policy: &fast, failures: 2, want: 3},
		{name: "per-call policy overrides an unset client", perCall: &fast, failures: 1, want: 2},
		{name: "per-call policy disables the client's", policy: &fast,
			perCall: &systemone.RetryPolicy{}, failures: 1, want: 1, wantErr: true},
		{name: "exhausted retries surface the last error", policy: &fast, failures: 9, want: 3, wantErr: true},
		{name: "predicate excludes the status", policy: &noRetryOn500, failures: 1, want: 1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			var opts []systemone.ClientOption
			if tt.policy != nil {
				opts = append(opts, systemone.WithRetry(*tt.policy))
			}
			c := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) <= tt.failures {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				_, _ = io.WriteString(w, answersJSON)
			}, opts...)

			_, err := c.Do(context.Background(), systemone.Request{
				State: "state", Questions: []systemone.Question{dept}, Retry: tt.perCall,
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if got := calls.Load(); got != tt.want {
				t.Errorf("attempts = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRetriesStopWhenTheCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	policy := systemone.DefaultRetry()
	policy.MaxRetries = 5
	policy.InitialBackoff = time.Millisecond
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		cancel()
		w.WriteHeader(http.StatusInternalServerError)
	}, systemone.WithRetry(policy))

	if _, err := c.Ask(ctx, "state", dept); err == nil {
		t.Fatal("want an error")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("attempts = %d, want 1", n)
	}
}

func TestPerCallTimeout(t *testing.T) {
	slow := func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		_, _ = io.WriteString(w, answersJSON)
	}

	t.Run("a short timeout bounds the attempt", func(t *testing.T) {
		c := serve(t, slow)
		_, err := c.Do(context.Background(), systemone.Request{
			State: "state", Questions: []systemone.Question{dept}, Timeout: 5 * time.Millisecond,
		})
		if err == nil {
			t.Fatal("want a timeout")
		}
	})

	t.Run("NoTimeout removes the client's bound", func(t *testing.T) {
		c := serve(t, slow, systemone.WithTimeout(5*time.Millisecond))
		if _, err := c.Do(context.Background(), systemone.Request{
			State: "state", Questions: []systemone.Question{dept}, Timeout: systemone.NoTimeout,
		}); err != nil {
			t.Fatalf("err = %v, want the call to outlive the client timeout", err)
		}
	})
}

func TestExtraBodyFields(t *testing.T) {
	c, rec := stub(t, answersJSON)
	if _, err := c.Do(context.Background(), systemone.Request{
		State: "state", Questions: []systemone.Question{dept},
		Extra: map[string]any{"beam_width": 4},
	}); err != nil {
		t.Fatal(err)
	}
	if got := rec.field(t, "beam_width"); got != "4" {
		t.Errorf("beam_width = %s", got)
	}
	if got := rec.field(t, "state"); got != `"state"` {
		t.Errorf("extra fields clobbered the body: %s", got)
	}
}

// An answer kind this package does not model is still reachable, undecoded.
func TestRawAnswers(t *testing.T) {
	const reply = `{"model":"jev-1.13.0","answers":{"future":{"type":"tally","tally":[1,2]}},"usage":{"input_tokens":1,"output_tokens":1}}`
	c, _ := stub(t, reply)
	q := systemone.RawQuestion{QuestionID: "future", Body: map[string]any{"type": "tally", "instructions": "Count"}}

	res, err := c.Ask(context.Background(), "state", q)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := q.From(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(answer), `"tally"`) {
		t.Errorf("raw answer = %s", answer)
	}
	if !strings.Contains(string(res.Raw()), `"tally"`) {
		t.Errorf("Raw() = %s", res.Raw())
	}
	if ids := res.IDs(); len(ids) != 1 || ids[0] != "future" {
		t.Errorf("IDs() = %v", ids)
	}
}

func TestModels(t *testing.T) {
	// These are the literal values the live API returns: RFC 3339 with
	// microseconds and an offset. An earlier fixture used the bare YYYY-MM-DD
	// date the SDK documentation shows, which is not what the API sends, so
	// the test confirmed a misreading instead of catching it.
	c, rec := stub(t, `{"models":[
	  {"name":"jev-latest","description":"Flagship","release_date":"2026-09-10T18:38:01.391457+00:00"},
	  {"name":"jev-preview","description":"Preview","release_date":""}]}`)
	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rec.Request.URL.Path != "/v1/models" || rec.Request.Method != http.MethodGet {
		t.Errorf("request = %s %s", rec.Request.Method, rec.Request.URL.Path)
	}
	if len(models) != 2 {
		t.Fatalf("models = %+v", models)
	}
	if want := time.Date(2026, 9, 10, 18, 38, 1, 391457000, time.UTC); !models[0].ReleaseDate.Equal(want) {
		t.Errorf("ReleaseDate = %v, want %v", models[0].ReleaseDate, want)
	}
	if !models[1].ReleaseDate.IsZero() {
		t.Errorf("missing date = %v, want the zero time", models[1].ReleaseDate)
	}

	bad, _ := stub(t, `{"models":[{"name":"jev-latest","release_date":"sometime last autumn"}]}`)
	if _, err := bad.Models(context.Background()); err == nil {
		t.Error("want an error for an unparseable release date")
	}
}

func TestClientOptions(t *testing.T) {
	t.Run("defaults come from the environment", func(t *testing.T) {
		t.Setenv(systemone.APIKeyEnv, "from-env")
		t.Setenv(systemone.DefaultModelEnv, "jev-1.13.0")
		c, err := systemone.New()
		if err != nil {
			t.Fatal(err)
		}
		if c.Model() != "jev-1.13.0" {
			t.Errorf("model = %q", c.Model())
		}
		// An explicit option wins over the environment.
		c, err = systemone.New(systemone.WithModel("jev-preview"))
		if err != nil {
			t.Fatal(err)
		}
		if c.Model() != "jev-preview" {
			t.Errorf("model = %q", c.Model())
		}
	})

	t.Run("rejected values", func(t *testing.T) {
		tests := []struct {
			name string
			opt  systemone.ClientOption
		}{
			{"empty api key", systemone.WithAPIKey("")},
			{"relative base url", systemone.WithBaseURL("/v1")},
			{"unparseable base url", systemone.WithBaseURL("http://a b")},
			{"empty model", systemone.WithModel("")},
			{"nil http client", systemone.WithHTTPClient(nil)},
			{"empty header name", systemone.WithHeader("", "v")},
			{"negative timeout", systemone.WithTimeout(-time.Second)},
			{"negative retries", systemone.WithRetry(systemone.RetryPolicy{MaxRetries: -1})},
			{"empty user agent", systemone.WithUserAgent("")},
			{"nil logger", systemone.WithLogger(nil)},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if _, err := systemone.New(systemone.WithAPIKey("k"), tt.opt); err == nil {
					t.Error("want an error")
				}
			})
		}
	})

	t.Run("no api key anywhere", func(t *testing.T) {
		t.Setenv(systemone.APIKeyEnv, "")
		if _, err := systemone.New(); !errors.Is(err, systemone.ErrNoAPIKey) {
			t.Errorf("err = %v, want ErrNoAPIKey", err)
		}
	})

	t.Run("headers, user agent and http client reach the request", func(t *testing.T) {
		tripped := false
		hc := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			tripped = true
			return http.DefaultTransport.RoundTrip(r)
		})}
		c, rec := stub(t, answersJSON,
			systemone.WithHTTPClient(hc),
			systemone.WithHeader("X-Trace-Id", "abc"),
			systemone.WithUserAgent("my-app/1.0"),
			systemone.WithTimeout(30*time.Second),
		)
		if _, err := c.Ask(context.Background(), "state", dept); err != nil {
			t.Fatal(err)
		}
		if !tripped {
			t.Error("the supplied http.Client was not used")
		}
		if got := rec.Request.Header.Get("X-Trace-Id"); got != "abc" {
			t.Errorf("X-Trace-Id = %q", got)
		}
		if got := rec.Request.Header.Get("User-Agent"); got != "my-app/1.0" {
			t.Errorf("User-Agent = %q", got)
		}

		// A per-request header replaces the client's.
		if _, err := c.Do(context.Background(), systemone.Request{
			State: "state", Questions: []systemone.Question{dept},
			Header: http.Header{"X-Trace-Id": {"xyz"}},
		}); err != nil {
			t.Fatal(err)
		}
		if got := rec.Request.Header.Get("X-Trace-Id"); got != "xyz" {
			t.Errorf("per-request X-Trace-Id = %q", got)
		}
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLogging(t *testing.T) {
	t.Run("levels", func(t *testing.T) {
		tests := []struct {
			name     string
			env      string
			level    *slog.Level
			wantLog  bool
			wantBody bool
			wantErr  bool
		}{
			{name: "no logger at all", wantLog: false},
			{name: "default level is info", level: ptr(slog.LevelInfo), wantLog: true},
			{name: "debug adds bodies", level: ptr(slog.LevelDebug), wantLog: true, wantBody: true},
			{name: "off is silent", level: ptr(systemone.LevelOff)},
			{name: "env sets the level", env: "debug", wantLog: true, wantBody: true},
			{name: "env warn hides a successful call", env: "warn"},
			{name: "env error hides a successful call", env: "error"},
			{name: "env off is silent", env: "off"},
			{name: "env rejects a bad level", env: "chatty", wantErr: true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var buf bytes.Buffer
				log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
				opts := []systemone.ClientOption{}
				if tt.level != nil || tt.env != "" || tt.wantLog {
					opts = append(opts, systemone.WithLogger(log))
				}
				if tt.level != nil {
					opts = append(opts, systemone.WithLogLevel(*tt.level))
				}
				if tt.env != "" {
					t.Setenv(systemone.LogLevelEnv, tt.env)
				}

				if tt.wantErr {
					if _, err := systemone.New(systemone.WithAPIKey("k")); err == nil {
						t.Fatal("want an error")
					}
					return
				}

				c, _ := stub(t, answersJSON, opts...)
				if _, err := c.Ask(context.Background(), "state", dept); err != nil {
					t.Fatal(err)
				}
				if logged := buf.Len() > 0; logged != tt.wantLog {
					t.Fatalf("logged = %v, want %v (%q)", logged, tt.wantLog, buf.String())
				}
				if body := strings.Contains(buf.String(), "request_body"); body != tt.wantBody {
					t.Errorf("body logged = %v, want %v", body, tt.wantBody)
				}
			})
		}
	})

	t.Run("credentials are redacted", func(t *testing.T) {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		c, _ := stub(t, answersJSON,
			systemone.WithLogger(log), systemone.WithLogLevel(slog.LevelDebug),
			systemone.WithHeader("X-Api-Key", "sk-secret"),
		)
		if _, err := c.Ask(context.Background(), "state", dept); err != nil {
			t.Fatal(err)
		}
		for _, leaked := range []string{"test-key", "sk-secret"} {
			if strings.Contains(buf.String(), leaked) {
				t.Errorf("%q leaked into the log: %s", leaked, buf.String())
			}
		}
		if !strings.Contains(buf.String(), "REDACTED") {
			t.Errorf("nothing redacted: %s", buf.String())
		}
	})

	t.Run("a failed attempt is logged", func(t *testing.T) {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		c := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}, systemone.WithLogger(log))
		if _, err := c.Ask(context.Background(), "state", dept); err == nil {
			t.Fatal("want an error")
		}
		if !strings.Contains(buf.String(), "status=500") {
			t.Errorf("status missing from %q", buf.String())
		}
	})
}

func ptr[T any](v T) *T { return &v }

// Normalized must divide by the level count the question declared, not by the
// size of whatever legend came back. A response is free to carry a partial
// legend, and inferring the scale from it reports a maximal score as zero.
func TestNormalizedUsesTheDeclaredLevelCount(t *testing.T) {
	const top = 2.0 // the highest level of the three frustration declares

	tests := []struct {
		name   string
		legend string
	}{
		{"full legend", `{"0":"Calm","1":"Frustrated","2":"Very angry"}`},
		{"partial legend", `{"2":"Very angry"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reply := fmt.Sprintf(`{"model":"m","answers":{"frustration":{"type":"score",`+
				`"score":%v,"legend":%s,"probabilities":{"2":1.0},"confidence":0.9}},`+
				`"usage":{"input_tokens":1,"output_tokens":1}}`, top, tt.legend)

			c, _ := stub(t, reply)
			res, err := c.Ask(context.Background(), "state", frustration)
			if err != nil {
				t.Fatal(err)
			}
			a, err := frustration.From(res)
			if err != nil {
				t.Fatal(err)
			}
			if a.LevelCount != 3 {
				t.Errorf("LevelCount = %d, want 3", a.LevelCount)
			}
			if got := a.Normalized(); got != 1 {
				t.Errorf("Normalized() = %v, want 1 for the top of the scale", got)
			}
		})
	}
}

// An answer missing a documented field must be an error, not a zero. A
// truncated noul that reads as a confident "no" is worse than a failure.
func TestIncompleteAnswersAreRejected(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		read   func(*systemone.Response) error
	}{{
		name:   "noul without a value",
		answer: `"is_urgent":{"type":"noul"}`,
		read:   func(r *systemone.Response) error { _, err := urgent.From(r); return err },
	}, {
		name:   "choice without a selection",
		answer: `"department":{"type":"choice","probabilities":{"billing":1},"confidence":0.9}`,
		read:   func(r *systemone.Response) error { _, err := dept.From(r); return err },
	}, {
		name:   "choice without confidence",
		answer: `"department":{"type":"choice","choice":"billing","probabilities":{"billing":1}}`,
		read:   func(r *systemone.Response) error { _, err := dept.From(r); return err },
	}, {
		name:   "choice without probabilities",
		answer: `"department":{"type":"choice","choice":"billing","confidence":0.9}`,
		read:   func(r *systemone.Response) error { _, err := dept.From(r); return err },
	}, {
		name:   "score without a value",
		answer: `"frustration":{"type":"score","legend":{"0":"Calm"},"probabilities":{"0":1},"confidence":0.9}`,
		read:   func(r *systemone.Response) error { _, err := frustration.From(r); return err },
	}, {
		name:   "score without a legend",
		answer: `"frustration":{"type":"score","score":1,"probabilities":{"0":1},"confidence":0.9}`,
		read:   func(r *systemone.Response) error { _, err := frustration.From(r); return err },
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reply := `{"model":"m","answers":{` + tt.answer + `},"usage":{"input_tokens":1,"output_tokens":1}}`
			c, _ := stub(t, reply)
			res, err := c.Ask(context.Background(), "state", dept, urgent, frustration)
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.read(res); !errors.Is(err, systemone.ErrIncompleteAnswer) {
				t.Errorf("err = %v, want ErrIncompleteAnswer", err)
			}
		})
	}
}

func TestMalformedResponses(t *testing.T) {
	tests := []struct {
		name  string
		reply string
	}{
		{"not json", `not json`},
		{"empty body", ``},
		{"answer is not an object", `{"model":"m","answers":{"department":7},"usage":{}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := stub(t, tt.reply)
			res, err := c.Ask(context.Background(), "state", dept)
			if err != nil {
				return // rejected while decoding, which is fine
			}
			if _, err := dept.From(res); err == nil {
				t.Error("want an error reading the answer")
			}
		})
	}
}
