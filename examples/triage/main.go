// Command triage is the support-ticket example from the TypeSafe quick start,
// written with this SDK.
//
// It asks three questions about one ticket in a single request: which team
// should handle it, how frustrated the customer is, and whether the message is
// time-sensitive. The state, instructions, and criteria are the ones at
// https://docs.typesafe.ai/introduction/quickstart.
//
// Set TYPESAFE_API_KEY, then:
//
//	go run ./examples/triage
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/justintout/systemone"
)

// Department is the set of teams a ticket can be routed to. Declaring it as a
// Go type is what makes the answer a Department rather than a string.
type Department string

const (
	Billing   Department = "billing"
	Technical Department = "technical"
	Sales     Department = "sales"
)

// Frustration levels run from calm to angry, lowest first.
type Frustration int

const (
	Calm Frustration = iota
	Civil
	Angry
)

var (
	department = systemone.NewChoice[Department]("department", "Which team should handle this?",
		systemone.Option(Billing, "Payment or subscription issues"),
		systemone.Option(Technical, "Bugs or integration problems"),
		systemone.Option(Sales, "Pricing or account questions"),
	)

	frustration = systemone.NewScore[Frustration]("frustration", "How frustrated the customer appears",
		systemone.Levels[Frustration](
			"Calm, just stating facts",
			"Frustrated but civil",
			"Very angry, strong language",
		),
	)

	isUrgent = systemone.NewNoul("is_urgent", "The message conveys urgency or time-sensitivity")
)

const ticket = "Hi, I've been trying to connect my Stripe account for 3 days " +
	"and the integration keeps failing. I'm losing sales. Please help ASAP."

func main() {
	client, err := systemone.New()
	if err != nil {
		log.Fatal(err)
	}

	// All three questions go in one request. They are answered in parallel
	// against the same state and cannot see one another's answers.
	res, err := client.Ask(context.Background(), ticket, department, frustration, isUrgent)
	if err != nil {
		log.Fatal(err)
	}

	team, err := department.From(res)
	if err != nil {
		log.Fatal(err)
	}
	mood, err := frustration.From(res)
	if err != nil {
		log.Fatal(err)
	}
	urgent, err := isUrgent.From(res)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("answered by %s (%d input tokens)\n\n", res.Model, res.Usage.InputTokens)
	fmt.Printf("department:  %-10s confidence %.2f\n", team.Value, team.Confidence)
	fmt.Printf("frustration: %-10.2f %s\n", mood.Value, mood.Describe())
	fmt.Printf("urgent:      %-10t p=%.2f\n\n", urgent.Yes(0.5), urgent.Value)

	// The answers are values, so routing is ordinary Go. team.Value is a
	// Department, so this switch is checked against the set declared above.
	switch {
	case team.Confidence < 0.5:
		fmt.Println("route: a human, the model is not sure")
	case team.Value == Technical && urgent.Yes(0.8):
		fmt.Println("route: technical, paged")
	case mood.Level == Angry:
		fmt.Printf("route: %s, flagged for a supervisor\n", team.Value)
	default:
		fmt.Printf("route: %s\n", team.Value)
	}
}
