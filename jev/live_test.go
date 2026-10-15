//go:build live

package jev_test

import (
	"context"
	"strings"
	"testing"

	"github.com/justintout/systemone"
	"github.com/justintout/systemone/internal/livetest"
	"github.com/justintout/systemone/jev"
)

// TestLive sends each kind of request to TypeSafe's hosted API and reads every
// answer back. Run it with TYPESAFE_API_KEY set:
//
//	go test -tags live -count=1 ./jev
func TestLive(t *testing.T) {
	livetest.Env(t, jev.APIKeyEnv)
	c, err := jev.New()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	type Team string
	team := systemone.NewChoice[Team]("team", "Which team should handle this?",
		systemone.Option[Team]("billing", "Payments, invoicing, refunds"),
		systemone.Option[Team]("technical", "Bugs, outages, integrations"),
	)
	mood := systemone.NewScore[int]("mood", "How frustrated is the customer?",
		systemone.Levels[int]("Calm", "Frustrated", "Very angry"))
	urgent := systemone.NewNoul("urgent", "Does this convey urgency?")

	read := func(t *testing.T, res *systemone.Response) {
		t.Helper()
		if res.Model == "" || res.RequestID == "" {
			t.Errorf("model %q, request ID %q", res.Model, res.RequestID)
		}
		if res.Usage.InputTokens == 0 {
			t.Error("no input tokens counted")
		}
	}

	t.Run("text", func(t *testing.T) {
		res, err := c.Ask(ctx, "Help! My payouts have been failing for 3 days.", team, mood, urgent)
		if err != nil {
			t.Fatal(err)
		}
		read(t, res)
		if _, err := team.From(res); err != nil {
			t.Error(err)
		}
		if _, err := mood.From(res); err != nil {
			t.Error(err)
		}
		if _, err := urgent.From(res); err != nil {
			t.Error(err)
		}
	})

	t.Run("compressed, long context", func(t *testing.T) {
		long := strings.Repeat("Our payouts have failed for three days and we are losing money. ", 60)
		res, err := c.Do(ctx, jev.Request{State: long, Questions: []systemone.Question{urgent}, LongContext: true})
		if err != nil {
			t.Fatal(err)
		}
		read(t, res)
		if _, err := urgent.From(res); err != nil {
			t.Error(err)
		}
	})

	t.Run("images and a box", func(t *testing.T) {
		photo, err := jev.NewImage(livetest.Image(t))
		if err != nil {
			t.Fatal(err)
		}
		example, err := jev.NewImage(livetest.Image(t))
		if err != nil {
			t.Fatal(err)
		}
		same := systemone.NewNoul("same", "Is `photo` the same picture as this one: "+example.Ref()+"?")
		where := jev.NewBox("where", "the blue rectangle in `photo`")
		res, err := c.Do(ctx, jev.Request{
			State:     map[string]any{"photo": photo},
			Questions: []systemone.Question{same},
			Boxes:     []*jev.Box{where},
			Images:    []jev.Image{photo, example},
			Model:     jev.Preview,
		})
		if err != nil {
			t.Fatal(err)
		}
		read(t, res)
		if _, err := same.From(res); err != nil {
			t.Error(err)
		}
		if _, err := where.From(res); err != nil {
			t.Error(err)
		}
	})

	t.Run("models", func(t *testing.T) {
		models, err := c.Models(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(models) == 0 {
			t.Error("no models listed")
		}
	})
}
