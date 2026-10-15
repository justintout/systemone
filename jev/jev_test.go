package jev_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
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

// pngBytes and jpegBytes are enough of each format for http.DetectContentType
// to name it.
var (
	pngBytes  = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	jpegBytes = append([]byte("\xff\xd8\xff"), make([]byte, 32)...)
)

func newImage(t *testing.T, data []byte) jev.Image {
	t.Helper()
	img, err := jev.NewImage(data)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// Images follow https://docs.typesafe.ai/images: each listed once under its
// reference in assets, and the reference written wherever the image belongs.
func TestImages(t *testing.T) {
	type Gesture string

	t.Run("placed in state, instructions and criteria", func(t *testing.T) {
		rec := stub(t)
		c, err := jev.New()
		if err != nil {
			t.Fatal(err)
		}
		door, badge, thumb := newImage(t, jpegBytes), newImage(t, jpegBytes), newImage(t, pngBytes)
		match := systemone.NewNoul("match", "Is the person in `door` the person in this badge photo: "+badge.Ref()+"?")
		gesture := systemone.NewChoice[Gesture]("gesture", "Which gesture is the hand in `door` making?",
			systemone.Option[Gesture]("thumbs_up", thumb),
			systemone.Option[Gesture]("none", "No hand"),
		)
		if _, err := c.Do(context.Background(), jev.Request{
			State:     map[string]any{"door": door},
			Questions: []systemone.Question{match, gesture},
			Images:    []jev.Image{door, badge, thumb},
		}); err != nil {
			t.Fatal(err)
		}

		if got, want := rec.field(t, "state"), `{"door":"`+door.Ref()+`"}`; got != want {
			t.Errorf("state = %s, want %s", got, want)
		}
		var assets map[string]map[string]string
		if err := json.Unmarshal([]byte(rec.field(t, "assets")), &assets); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"type": "image", "format": "jpg", "data_base64": base64.StdEncoding.EncodeToString(jpegBytes)}
		if got := assets[badge.Ref()]; !maps.Equal(got, want) {
			t.Errorf("badge asset = %v, want %v", got, want)
		}
		if got := assets[thumb.Ref()]["format"]; got != "png" {
			t.Errorf("thumb format = %q", got)
		}
		if len(assets) != 3 {
			t.Errorf("assets = %d, want 3", len(assets))
		}
		if got := rec.Request.Header.Get(jev.ImagesHeader); got != "true" {
			t.Errorf("%s = %q", jev.ImagesHeader, got)
		}
	})

	t.Run("references that do not match the list fail before sending", func(t *testing.T) {
		a, b := newImage(t, pngBytes), newImage(t, pngBytes)
		for name, req := range map[string]jev.Request{
			"placed but not listed": {State: map[string]any{"photo": a}, Questions: []systemone.Question{urgent}},
			"listed but not placed": {State: map[string]any{"photo": a}, Questions: []systemone.Question{urgent}, Images: []jev.Image{a, b}},
			"listed twice":          {State: map[string]any{"photo": a}, Questions: []systemone.Question{urgent}, Images: []jev.Image{a, a}},
			"not built by NewImage": {State: "state", Questions: []systemone.Question{urgent}, Images: []jev.Image{{}}},
		} {
			rec := stub(t)
			c, err := jev.New()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Do(context.Background(), req); !errors.Is(err, systemone.ErrImage) {
				t.Errorf("%s: err = %v, want ErrImage", name, err)
			}
			if rec.Request != nil {
				t.Errorf("%s: the request was sent", name)
			}
		}
	})

	t.Run("other deployments refuse them", func(t *testing.T) {
		rec := stub(t)
		img := newImage(t, pngBytes)
		base, err := systemone.New(systemone.WithAPIKey("k"), systemone.WithBaseURL(os.Getenv(jev.BaseURLEnv)), systemone.WithModel("m"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := base.Ask(context.Background(), map[string]any{"photo": img}, urgent); !errors.Is(err, systemone.ErrImage) {
			t.Errorf("err = %v, want ErrImage", err)
		}
		if rec.Request != nil {
			t.Error("the request was sent")
		}
	})

	t.Run("formats", func(t *testing.T) {
		for name, data := range map[string][]byte{
			"empty": nil,
			"gif":   []byte("GIF89a and then some"),
			"webp":  []byte("RIFF\x00\x00\x00\x00WEBPVP8 "),
		} {
			if _, err := jev.NewImage(data); !errors.Is(err, systemone.ErrImage) {
				t.Errorf("%s: err = %v, want ErrImage", name, err)
			}
		}
	})
}

func TestDangerZoneHeaders(t *testing.T) {
	mug := systemone.NewBox("mug", "the coffee mug in `photo`")
	shared := http.Header{"X-Trace-Id": {"abc"}}
	for _, tt := range []struct {
		name                string
		req                 jev.Request
		images, longContext string
	}{
		{"plain", jev.Request{State: "state", Questions: []systemone.Question{urgent}, Header: shared}, "", ""},
		{"a box needs images on", jev.Request{State: "state", Questions: []systemone.Question{urgent, mug}, Header: shared}, "true", ""},
		{"long context", jev.Request{State: "state", Questions: []systemone.Question{urgent}, LongContext: true, Header: shared}, "", "true"},
	} {
		rec := stub(t)
		c, err := jev.New()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Do(context.Background(), tt.req); err != nil {
			t.Fatal(err)
		}
		h := rec.Request.Header
		if got := h.Get(jev.ImagesHeader); got != tt.images {
			t.Errorf("%s: %s = %q, want %q", tt.name, jev.ImagesHeader, got, tt.images)
		}
		if got := h.Get(jev.LongContextHeader); got != tt.longContext {
			t.Errorf("%s: %s = %q, want %q", tt.name, jev.LongContextHeader, got, tt.longContext)
		}
		if h.Get("X-Trace-Id") != "abc" {
			t.Errorf("%s: the caller's header was dropped", tt.name)
		}
		if got := rec.field(t, "assets"); got != "" {
			t.Errorf("%s: assets = %s, want none", tt.name, got)
		}
	}
	if len(shared) != 1 {
		t.Errorf("the caller's header was changed: %v", shared)
	}
}
