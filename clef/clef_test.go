package clef_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/justintout/systemone"
	"github.com/justintout/systemone/clef"
)

var urgent = systemone.NewNoul("is_urgent", "Does this convey urgency?")

const answersJSON = `{
  "model": "clef-flash",
  "answers": {"is_urgent": {"type":"noul","noul":0.95}},
  "usage": {"input_tokens": 296, "output_tokens": 20}
}`

// recorder keeps the last request a stub server received.
type recorder struct {
	Request *http.Request
	Body    []byte
	Calls   int
}

func (r *recorder) field(t *testing.T, name string) string {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(r.Body, &body); err != nil {
		t.Fatalf("request body: %v", err)
	}
	return string(body[name])
}

// stub answers the way Workers AI does: the payload wrapped in Cloudflare's
// envelope, with the request ID in Cf-Ray.
func stub(t *testing.T) (*clef.Client, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.Calls++
		rec.Body, _ = io.ReadAll(r.Body)
		rec.Request = r
		w.Header().Set("Cf-Ray", "9d1f2c3a4b5e6f70-ORD")
		fmt.Fprintf(w, `{"result":%s,"success":true,"errors":[],"messages":[]}`, answersJSON)
	}))
	t.Cleanup(srv.Close)
	c, err := clef.New("acct-1", systemone.WithAPIKey("cf-token"), systemone.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return c, rec
}

// Clef sends and receives the same payload as the base API; the client moves
// the model into the URL and unwraps the envelope.
func TestWorkersAI(t *testing.T) {
	c, rec := stub(t)
	if c.Model() != clef.Model {
		t.Errorf("model = %q, want %q", c.Model(), clef.Model)
	}
	res, err := c.Do(context.Background(), clef.Request{
		State: "Checkout has been failing for an hour.", Questions: []systemone.Question{urgent},
		Model: clef.Flash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "/client/v4/accounts/acct-1/ai/run/@cf/cloudflare/clef-flash"; rec.Request.URL.Path != want {
		t.Errorf("path = %q, want %q", rec.Request.URL.Path, want)
	}
	// The model stays in the body as well, which is what Clef validates.
	if got := rec.field(t, "model"); got != `"clef-flash"` {
		t.Errorf("body model = %s", got)
	}
	if res.RequestID != "9d1f2c3a4b5e6f70-ORD" {
		t.Errorf("RequestID = %q", res.RequestID)
	}
	u, err := urgent.From(res)
	if err != nil {
		t.Fatal(err)
	}
	if u.Value != 0.95 {
		t.Errorf("noul = %v", u.Value)
	}

	if _, err := clef.New("", systemone.WithAPIKey("k")); err == nil {
		t.Error("want an error without an account ID")
	}
}

// pngBytes is enough of a PNG for http.DetectContentType to name it one.
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)

func TestImages(t *testing.T) {
	img, err := clef.NewImage(pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	if img.ContentType != "image/png" {
		t.Errorf("ContentType = %q", img.ContentType)
	}

	t.Run("sent as base64 before the state", func(t *testing.T) {
		c, rec := stub(t)
		if _, err := c.Do(context.Background(), clef.Request{
			State: "the checkout page", Questions: []systemone.Question{urgent}, Images: []clef.Image{img},
		}); err != nil {
			t.Fatal(err)
		}
		want := `[{"content_type":"image/png","base64":"` + base64.StdEncoding.EncodeToString(pngBytes) + `"}]`
		if got := rec.field(t, "images"); got != want {
			t.Errorf("images = %s, want %s", got, want)
		}
	})

	t.Run("absent when there are none", func(t *testing.T) {
		c, rec := stub(t)
		if _, err := c.Ask(context.Background(), "state", urgent); err != nil {
			t.Fatal(err)
		}
		if got := rec.field(t, "images"); got != "" {
			t.Errorf("images = %s, want none", got)
		}
	})

	t.Run("rejected images", func(t *testing.T) {
		if _, err := clef.NewImage(nil); !errors.Is(err, systemone.ErrImage) {
			t.Errorf("empty data err = %v, want ErrImage", err)
		}
		if _, err := clef.NewImage([]byte("GIF89a and then some")); !errors.Is(err, systemone.ErrImage) {
			t.Errorf("gif err = %v, want ErrImage", err)
		}
		// An Image built by hand rather than through NewImage is checked when
		// it is encoded, so a half-filled one cannot reach the wire.
		c, rec := stub(t)
		_, err := c.Do(context.Background(), clef.Request{
			State: "state", Questions: []systemone.Question{urgent},
			Images: []clef.Image{{ContentType: "image/png"}},
		})
		if !errors.Is(err, systemone.ErrImage) {
			t.Errorf("err = %v, want ErrImage", err)
		}
		if rec.Calls != 0 {
			t.Errorf("calls = %d, want the request never sent", rec.Calls)
		}
	})
}

func TestBoxIsRefused(t *testing.T) {
	c, rec := stub(t)
	mug := systemone.NewBox("mug", "the coffee mug in `photo`")
	_, err := c.Ask(context.Background(), "state", urgent, mug)
	var qerr *systemone.QuestionError
	if !errors.As(err, &qerr) || qerr.QuestionID != "mug" {
		t.Errorf("err = %v, want a QuestionError for mug", err)
	}
	if rec.Calls != 0 {
		t.Errorf("calls = %d, want the request never sent", rec.Calls)
	}
}
