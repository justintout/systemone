package systemone_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/justintout/systemone"
	"github.com/justintout/systemone/pattern"
)

// Questions are values. Declare them once and reuse them across requests.
type Team string

const (
	TeamBilling   Team = "billing"
	TeamTechnical Team = "technical"
	TeamSales     Team = "sales"
)

type Anger int

const (
	AngerCalm Anger = iota
	AngerFrustrated
	AngerFurious
)

var (
	team = systemone.NewChoice[Team]("team", "Which team should handle this message?",
		systemone.Option(TeamBilling, "Payments, invoicing, refunds"),
		systemone.Option(TeamTechnical, "Bugs, outages, integrations"),
		systemone.Option(TeamSales, "Pricing, upgrades, new accounts"),
	)
	timeSensitive = systemone.NewNoul("time_sensitive", "Does this convey urgency?",
		systemone.Yes("Explicitly time-sensitive"),
		systemone.No("No urgency expressed"),
	)
	anger = systemone.NewScore[Anger]("anger", "How frustrated is the customer?",
		systemone.Levels[Anger]("Calm and matter-of-fact", "Visibly frustrated", "Angry, threatening to leave"),
	)
)

// Independent questions about the same state go in one request. They are
// answered in parallel and cannot see one another's answers.
func Example() {
	client, err := systemone.New()
	if err != nil {
		log.Fatal(err)
	}

	ticket := map[string]any{
		"subject": "Payouts failing",
		"body":    "Help! My payouts have been failing for 3 days and nobody has replied.",
		"plan":    "enterprise",
	}

	res, err := client.Ask(context.Background(), ticket, team, timeSensitive, anger)
	if err != nil {
		log.Fatal(err)
	}

	t, err := team.From(res)
	if err != nil {
		log.Fatal(err)
	}
	u, err := timeSensitive.From(res)
	if err != nil {
		log.Fatal(err)
	}
	a, err := anger.From(res)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(t.Value, t.Confidence, u.Yes(0.8), a.Level, a.Describe())
}

// Confidence says whether to act on an answer, not whether the answer is right.
// Gate riskier actions at a higher bar.
func Example_confidenceGatedRouting() {
	client, err := systemone.New()
	if err != nil {
		log.Fatal(err)
	}
	res, err := client.Ask(context.Background(), "Move everything into savings.", team)
	if err != nil {
		log.Fatal(err)
	}
	t, err := team.From(res)
	if err != nil {
		log.Fatal(err)
	}

	switch (pattern.Thresholds{Medium: 0.6, High: 0.85}).Band(t.Confidence) {
	case pattern.High:
		fmt.Println("route to", t.Value)
	case pattern.Medium:
		fmt.Println("confirm with the customer, then route to", t.Value)
	case pattern.Low:
		fmt.Println("hand to a support agent")
	}
}

// Score each dimension on its own, then combine with weights your code owns.
// Changing a weight reranks existing answers without a new request.
func Example_compositeScoring() {
	client, err := systemone.New()
	if err != nil {
		log.Fatal(err)
	}
	res, err := client.Ask(context.Background(), "resume text", anger, timeSensitive)
	if err != nil {
		log.Fatal(err)
	}
	a, _ := anger.From(res)
	u, _ := timeSensitive.From(res)

	priority := pattern.Composite(
		pattern.Weigh(a, 0.7),
		pattern.WeighNoul(u, 0.3),
	)
	fmt.Printf("priority %.2f\n", priority)
}

// Retries are off by default. Turn them on with a policy you can tune.
func ExampleWithRetry() {
	policy := systemone.DefaultRetry()
	policy.MaxRetries = 4
	policy.MaxBackoff = 10 * time.Second

	client, err := systemone.New(
		systemone.WithRetry(policy),
		systemone.WithTimeout(30*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}
	_ = client
}

// Classify an HTTP failure by its status.
func ExampleError() {
	client, err := systemone.New()
	if err != nil {
		log.Fatal(err)
	}
	_, err = client.Ask(context.Background(), "state", team)

	var apiErr *systemone.Error
	switch {
	case err == nil:
	case errors.Is(err, systemone.ErrRateLimit):
		fmt.Println("back off and retry")
	case errors.Is(err, systemone.ErrUnprocessable):
		// The body names the offending field.
		errors.As(err, &apiErr)
		fmt.Println("bad question:", string(apiErr.Body))
	default:
		fmt.Println(err)
	}
}
