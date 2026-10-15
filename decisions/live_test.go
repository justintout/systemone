//go:build live

package decisions_test

import (
	"context"
	"testing"

	"github.com/justintout/systemone"
	"github.com/justintout/systemone/decisions"
	"github.com/justintout/systemone/internal/livetest"
)

// TestLive sends one request with each question type to a Microsoft-Decision-1
// deployment and reads every answer back. The variables are the ones
// Microsoft's documentation uses:
//
//	export AZURE_ENDPOINT=... DEPLOYMENT_NAME=... AZURE_API_KEY=...
//	go test -tags live -count=1 ./decisions
func TestLive(t *testing.T) {
	c, err := decisions.New(livetest.Env(t, "AZURE_ENDPOINT"), livetest.Env(t, "DEPLOYMENT_NAME"),
		systemone.WithAPIKey(livetest.Env(t, "AZURE_API_KEY")),
		systemone.WithAPIKeyHeader(decisions.APIKeyHeader))
	if err != nil {
		t.Fatal(err)
	}

	type Team string
	team := systemone.NewChoice[Team]("team", "Which team should handle this ticket?",
		systemone.Options[Team]("billing", "engineering", "support"))
	urgent := systemone.NewNoul("urgent", "Does this convey urgency?")
	severity := systemone.NewScore[int]("severity", "How severe is this incident?",
		systemone.Levels[int]("Cosmetic", "Minor", "Major", "Critical"))

	res, err := c.Ask(context.Background(), "Checkout is down for all EU customers since 09:00.", team, urgent, severity)
	if err != nil {
		t.Fatal(err)
	}
	if res.Model == "" || res.Usage.InputTokens == 0 {
		t.Errorf("model %q, %d input tokens", res.Model, res.Usage.InputTokens)
	}
	if _, err := team.From(res); err != nil {
		t.Error(err)
	}
	if _, err := urgent.From(res); err != nil {
		t.Error(err)
	}
	if _, err := severity.From(res); err != nil {
		t.Error(err)
	}
}
