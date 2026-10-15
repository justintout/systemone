//go:build live

package clef_test

import (
	"context"
	"testing"

	"github.com/justintout/systemone"
	"github.com/justintout/systemone/clef"
	"github.com/justintout/systemone/internal/livetest"
)

// TestLive sends a request with an image to each Clef model on Workers AI and
// reads every answer back. Run it with CLOUDFLARE_ACCOUNT_ID and
// CLOUDFLARE_API_TOKEN set:
//
//	go test -tags live -count=1 ./clef
func TestLive(t *testing.T) {
	c, err := clef.New(livetest.Env(t, "CLOUDFLARE_ACCOUNT_ID"),
		systemone.WithAPIKey(livetest.Env(t, "CLOUDFLARE_API_TOKEN")))
	if err != nil {
		t.Fatal(err)
	}
	img, err := clef.NewImage(livetest.Image(t))
	if err != nil {
		t.Fatal(err)
	}

	type Shape string
	shape := systemone.NewChoice[Shape]("shape", "Which shape does the image show?",
		systemone.Options[Shape]("rectangle", "circle", "none"))
	blue := systemone.NewNoul("blue", "Is the shape blue?")
	size := systemone.NewScore[int]("size", "How much of the image does the shape fill?",
		systemone.Levels[int]("Under a tenth", "About a quarter", "Over half"))

	for _, model := range []string{clef.Model, clef.Flash} {
		t.Run(model, func(t *testing.T) {
			res, err := c.Do(context.Background(), clef.Request{
				State:     "An image of a shape.",
				Questions: []systemone.Question{shape, blue, size},
				Images:    []clef.Image{img},
				Model:     model,
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.Model == "" || res.RequestID == "" || res.Usage.InputTokens == 0 {
				t.Errorf("model %q, request ID %q, %d input tokens", res.Model, res.RequestID, res.Usage.InputTokens)
			}
			if _, err := shape.From(res); err != nil {
				t.Error(err)
			}
			if _, err := blue.From(res); err != nil {
				t.Error(err)
			}
			if _, err := size.From(res); err != nil {
				t.Error(err)
			}
		})
	}
}
