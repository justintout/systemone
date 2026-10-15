package foundry_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/justintout/systemone"
	"github.com/justintout/systemone/foundry"
)

var urgent = systemone.NewNoul("is_urgent", "Does this convey urgency?")

// The response names the underlying model, not the deployment the request
// named, as Microsoft's documentation describes.
const answersJSON = `{
  "model": "microsoft-decision-1",
  "answers": {"is_urgent": {"type":"noul","noul":0.95}},
  "usage": {"input_tokens": 41, "output_tokens": 0}
}`

// Foundry sends and receives the base API's payload at its own path, and reads
// either credential.
func TestFoundry(t *testing.T) {
	var got *http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		body, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, answersJSON)
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		name       string
		opts       []systemone.ClientOption
		wantHeader string
		wantValue  string
	}{
		{"entra token", []systemone.ClientOption{systemone.WithAPIKey("entra-token")}, "Authorization", "Bearer entra-token"},
		{"api key", []systemone.ClientOption{systemone.WithAPIKey("res-key"), systemone.WithAPIKeyHeader(foundry.APIKeyHeader)}, "Api-Key", "res-key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := foundry.New(srv.URL, "triage-d1", tt.opts...)
			if err != nil {
				t.Fatal(err)
			}
			res, err := c.Ask(context.Background(), "Checkout has been failing for an hour.", urgent)
			if err != nil {
				t.Fatal(err)
			}
			if want := "/providers/microsoft/v1/systemone"; got.URL.Path != want {
				t.Errorf("path = %q, want %q", got.URL.Path, want)
			}
			if v := got.Header.Get(tt.wantHeader); v != tt.wantValue {
				t.Errorf("%s = %q, want %q", tt.wantHeader, v, tt.wantValue)
			}
			if tt.wantHeader != "Authorization" && got.Header.Get("Authorization") != "" {
				t.Error("Authorization sent alongside the API key header")
			}
			var sent struct{ Model string }
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Fatal(err)
			}
			if sent.Model != "triage-d1" {
				t.Errorf("body model = %q, want the deployment", sent.Model)
			}
			u, err := urgent.From(res)
			if err != nil {
				t.Fatal(err)
			}
			if u.Value != 0.95 {
				t.Errorf("noul = %v", u.Value)
			}
		})
	}

	c, err := foundry.New(srv.URL, "triage-d1", systemone.WithAPIKey("k"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Models(context.Background()); !errors.Is(err, systemone.ErrNoModelList) {
		t.Errorf("Models err = %v, want ErrNoModelList", err)
	}
	if _, err := foundry.New("", "d", systemone.WithAPIKey("k")); err == nil {
		t.Error("want an error without an endpoint")
	}
	if _, err := foundry.New(srv.URL, "", systemone.WithAPIKey("k")); err == nil {
		t.Error("want an error without a deployment")
	}
}
