// Command screenshot triages a support ticket that came with a screenshot,
// using Cloudflare's Clef models on Workers AI. Clef reads images placed
// before the state, so the screenshot travels in Request.Images and the
// questions refer to it in words.
//
// Set CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN, a Workers AI token,
// then:
//
//	go run ./examples/screenshot checkout.png "The checkout button does nothing when I click it."
//
// The screenshot may be PNG, JPEG or WebP.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/justintout/systemone"
	"github.com/justintout/systemone/clef"
)

// Area is the part of the product the ticket is about.
type Area string

const (
	Checkout Area = "checkout"
	Account  Area = "account"
	Other    Area = "other"
)

// Severity runs from cosmetic to blocking, lowest first.
type Severity int

const (
	Cosmetic Severity = iota
	Degraded
	Blocking
)

var (
	area = systemone.NewChoice[Area]("area", "Which part of the product is the customer's problem in, judging by the message and the screenshot?",
		systemone.Option(Checkout, "Cart, payment, or order confirmation"),
		systemone.Option(Account, "Sign-in, profile, or settings"),
		systemone.Option(Other, "Anything else"),
	)
	errorShown = systemone.NewNoul("error_shown", "Does the screenshot show an error message?")
	severity   = systemone.NewScore[Severity]("severity", "How badly does the problem affect the customer?",
		systemone.Levels[Severity](
			"Cosmetic: something looks wrong but works",
			"Degraded: it works with a workaround or after retrying",
			"Blocking: the customer cannot finish what they came to do",
		),
	)
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: screenshot IMAGE MESSAGE")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	shot, err := clef.NewImage(data)
	if err != nil {
		log.Fatal(err)
	}

	client, err := clef.New(os.Getenv("CLOUDFLARE_ACCOUNT_ID"), systemone.WithAPIKey(os.Getenv("CLOUDFLARE_API_TOKEN")))
	if err != nil {
		log.Fatal(err)
	}
	res, err := client.Do(context.Background(), clef.Request{
		State:     map[string]any{"message": os.Args[2]},
		Questions: []systemone.Question{area, errorShown, severity},
		Images:    []clef.Image{shot},
	})
	if err != nil {
		log.Fatal(err)
	}

	a, err := area.From(res)
	if err != nil {
		log.Fatal(err)
	}
	e, err := errorShown.From(res)
	if err != nil {
		log.Fatal(err)
	}
	s, err := severity.From(res)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("answered by %s (%d input tokens)\n\n", res.Model, res.Usage.InputTokens)
	fmt.Printf("area:        %-9s confidence %.2f\n", a.Value, a.Confidence)
	fmt.Printf("error shown: p=%.2f\n", e.Value)
	fmt.Printf("severity:    %.2f      %s\n", s.Value, s.Describe())
}
