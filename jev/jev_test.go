package jev_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justintout/systemone"
	"github.com/justintout/systemone/jev"
)

var urgent = systemone.NewNoul("is_urgent", "Does this convey urgency?")

const answersJSON = `{
  "model": "jev-1.13.0",
  "answers": {"is_urgent": {"type":"noul","noul":0.95}},
  "usage": {"input_tokens": 296, "output_tokens": 20}
}`

// recorder keeps the last request a stub server received.
type recorder struct {
	Request *http.Request
	Body    []byte
}

// field returns one top-level field of the recorded request body, undecoded.
func (r *recorder) field(t *testing.T, name string) string {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(r.Body, &body); err != nil {
		t.Fatalf("request body: %v", err)
	}
	return string(body[name])
}

// stub starts an API stub and points BaseURLEnv at it, so the client under
// test is configured the way a caller's would be.
func stub(t *testing.T) *recorder {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.Body, _ = io.ReadAll(r.Body)
		rec.Request = r
		w.Header().Set("X-Typesafe-Request-Id", "req_123")
		_, _ = io.WriteString(w, answersJSON)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(jev.BaseURLEnv, srv.URL)
	t.Setenv(jev.APIKeyEnv, "from-env")
	t.Setenv(jev.DefaultModelEnv, "")
	t.Setenv(jev.LogLevelEnv, "")
	return rec
}

func TestDefaults(t *testing.T) {
	rec := stub(t)
	c, err := jev.New()
	if err != nil {
		t.Fatal(err)
	}
	if c.Model() != jev.Latest {
		t.Errorf("model = %q, want %q", c.Model(), jev.Latest)
	}
	res, err := c.Ask(context.Background(), "state", urgent)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, got, want string }{
		{"path", rec.Request.URL.Path, "/v1/systemone"},
		{"auth", rec.Request.Header.Get("Authorization"), "Bearer from-env"},
		{"model", rec.field(t, "model"), `"jev-latest"`},
		{"request id", res.RequestID, "req_123"},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestEnvironment(t *testing.T) {
	t.Run("model, and an option over it", func(t *testing.T) {
		stub(t)
		t.Setenv(jev.DefaultModelEnv, "jev-1.13.0")
		c, err := jev.New()
		if err != nil {
			t.Fatal(err)
		}
		if c.Model() != "jev-1.13.0" {
			t.Errorf("model = %q", c.Model())
		}
		c, err = jev.New(systemone.WithModel("jev-preview"))
		if err != nil {
			t.Fatal(err)
		}
		if c.Model() != "jev-preview" {
			t.Errorf("model = %q", c.Model())
		}
	})

	t.Run("no api key anywhere", func(t *testing.T) {
		stub(t)
		t.Setenv(jev.APIKeyEnv, "")
		if _, err := jev.New(); !errors.Is(err, systemone.ErrNoAPIKey) {
			t.Errorf("err = %v, want ErrNoAPIKey", err)
		}
	})

	t.Run("log level", func(t *testing.T) {
		for _, tt := range []struct {
			env               string
			wantLog, wantBody bool
		}{
			{"debug", true, true},
			{"INFO", true, false},
			{"warn", false, false},
			{"off", false, false},
		} {
			stub(t)
			t.Setenv(jev.LogLevelEnv, tt.env)
			var buf bytes.Buffer
			log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
			c, err := jev.New(systemone.WithLogger(log))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Ask(context.Background(), "state", urgent); err != nil {
				t.Fatal(err)
			}
			if logged := buf.Len() > 0; logged != tt.wantLog {
				t.Errorf("%s: logged = %v, want %v", tt.env, logged, tt.wantLog)
			}
			if body := strings.Contains(buf.String(), "request_body"); body != tt.wantBody {
				t.Errorf("%s: body logged = %v, want %v", tt.env, body, tt.wantBody)
			}
		}
		stub(t)
		t.Setenv(jev.LogLevelEnv, "chatty")
		if _, err := jev.New(); err == nil {
			t.Error("want an error for an unknown level")
		}
	})
}
