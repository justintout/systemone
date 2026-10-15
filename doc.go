// Package systemone is a client for the TypeSafe System One API.
//
// A request sends one state and a set of typed questions; the response carries
// one answer per question. Questions are values you declare once and reuse:
//
//	type Dept string
//
//	const (
//		Billing   Dept = "billing"
//		Technical Dept = "technical"
//	)
//
//	var (
//		dept = systemone.NewChoice[Dept]("department", "Which team should handle this?",
//			systemone.Option(Billing, "Payments, invoicing, refunds"),
//			systemone.Option(Technical, "Bugs, outages, integrations"),
//		)
//		urgent = systemone.NewNoul("is_urgent", "Does this convey urgency?")
//	)
//
//	client, err := jev.New()
//	res, err := client.Ask(ctx, "Help! My payouts have been failing for 3 days.", dept, urgent)
//	if err != nil {
//		return err
//	}
//	d, err := dept.From(res)   // ChoiceAnswer[Dept]
//	u, err := urgent.From(res) // NoulAnswer
//
// Reading an answer through its question handle is the only way to reach it, so
// the option type, the level type, and the answer type are all checked at
// compile time.
//
// See https://docs.typesafe.ai for the concepts behind state, questions,
// probabilities, and confidence.
//
// The client depends on the deployment. Package jev reaches TypeSafe's hosted
// Jev models and package clef reaches Cloudflare's Clef models; each adds its
// deployment's extensions to the API. [Client] in this package speaks the base
// API alone, for a server that implements it itself. All three take the same
// questions, return the same [Response], and satisfy [Asker].
//
// This is an unofficial, community-maintained client, not built, endorsed, or
// supported by TypeSafe.
package systemone
