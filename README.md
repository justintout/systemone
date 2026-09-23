# systemone

[![CI](https://github.com/justintout/systemone/actions/workflows/ci.yml/badge.svg)](https://github.com/justintout/systemone/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/justintout/systemone.svg)](https://pkg.go.dev/github.com/justintout/systemone)

A fully typed Go client for the [TypeSafe System One API](https://docs.typesafe.ai).
Send one state and a set of questions; get one answer per question, each bound
to the Go type you declared the question with, so options and score levels are
checked by the compiler rather than compared against string literals.

```
go get github.com/justintout/systemone
```

No dependencies outside the standard library. Requires Go 1.24.

> This is an unofficial, community-maintained client. It is not built,
> endorsed, or supported by TypeSafe. The official SDKs are
> [Python](https://docs.typesafe.ai/sdk/python) and
> [JavaScript](https://docs.typesafe.ai/sdk/javascript).

## Usage

A question is a value. Declare it once with the Go type its answer uses, then
read the answer back through the same handle.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/justintout/systemone"
)

type Team string

const (
	Billing   Team = "billing"
	Technical Team = "technical"
	Sales     Team = "sales"
)

type Anger int

const (
	Calm Anger = iota
	Frustrated
	Furious
)

var (
	team = systemone.NewChoice[Team]("team", "Which team should handle this message?",
		systemone.Option(Billing, "Payments, invoicing, refunds"),
		systemone.Option(Technical, "Bugs, outages, integrations"),
		systemone.Option(Sales, "Pricing, upgrades, new accounts"),
	)
	urgent = systemone.NewNoul("urgent", "Does this convey urgency?",
		systemone.Yes("Explicitly time-sensitive"),
		systemone.No("No urgency expressed"),
	)
	anger = systemone.NewScore[Anger]("anger", "How frustrated is the customer?",
		systemone.Levels[Anger](
			"Calm and matter-of-fact",
			"Visibly frustrated",
			"Angry, threatening to leave",
		),
	)
)

func main() {
	client, err := systemone.New() // reads TYPESAFE_API_KEY
	if err != nil {
		log.Fatal(err)
	}

	ticket := map[string]any{
		"subject": "Payouts failing",
		"body":    "Help! My payouts have been failing for 3 days and nobody has replied.",
	}

	res, err := client.Ask(context.Background(), ticket, team, urgent, anger)
	if err != nil {
		log.Fatal(err)
	}

	t, err := team.From(res)   // systemone.ChoiceAnswer[Team]
	if err != nil {
		log.Fatal(err)
	}
	a, err := anger.From(res)  // systemone.ScoreAnswer[Anger]
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(t.Value, t.Confidence, a.Level, a.Describe())
}
```

`From` is the only way to reach an answer, which is what keeps the typing
honest: there is no untyped map to fall back to. `t.Value` is a `Team`, so a
`switch` over it is checked against the set the question declared. A typo in an
option name, or a level enum borrowed from a different question, is a build
failure rather than a branch that silently never fires.

Independent questions belong in one request. They are answered in parallel
against the same state and cannot see one another's answers, which makes
speculative questions cheap: ask the branch-specific ones up front and read only
the answers your code ends up needing.

## Primitives

| Question | Answer | Use it for |
| --- | --- | --- |
| `NewChoice[T ~string]` | `ChoiceAnswer[T]`: `Value`, `Probabilities`, `Confidence` | One of a defined set |
| `NewNoul` | `NoulAnswer`: `Value`, the probability of yes | Whether a condition holds |
| `NewScore[T ~int]` | `ScoreAnswer[T]`: `Value`, `Level`, `Legend`, `Probabilities`, `Confidence` | Degree along ordered levels |

Instructions and every criterion take a string or any JSON-encodable structure,
so a question can carry named data it refers to by backticked path:

```go
var duplicate = systemone.NewNoul("duplicate", map[string]any{
	"candidate": map[string]string{"name": "John Smith", "last_employer": "Google"},
	"question":  "Is the resume for the same person as `candidate`?",
})
```

Build errors, such as a duplicate option or a level declared out of order, are
reported by the call that sends the question rather than at construction, so
questions can be package-level variables.

A `ScoreAnswer`'s `Legend` holds each level's description as it was sent, so
structured levels come back structured. `Describe()` renders the current level
as text, JSON-encoding a structured one.

## Client

```go
client, err := systemone.New(
	systemone.WithAPIKey(key),          // default: $TYPESAFE_API_KEY
	systemone.WithBaseURL(url),         // default: $TYPESAFE_BASE_URL, then https://api.typesafe.ai
	systemone.WithModel("jev-1.13.0"),  // default: $TYPESAFE_DEFAULT_MODEL, then jev-latest
	systemone.WithHTTPClient(hc),
	systemone.WithTimeout(30*time.Second), // per attempt
	systemone.WithRetry(systemone.DefaultRetry()),
	systemone.WithHeader("X-Trace-Id", id),
	systemone.WithLogger(slog.Default()),
)
```

`Client.Models` lists the models the account can send. `Client.Do` takes a
`Request` when one call needs to differ from the client's defaults:

```go
res, err := client.Do(ctx, systemone.Request{
	State:     ticket,
	Questions: []systemone.Question{team, urgent, anger},
	Model:     "jev-1.13.0",
	Header:    http.Header{"X-Trace-Id": {id}},
	Retry:     &policy,              // nil inherits the client's
	Timeout:   45 * time.Second,     // per attempt; zero inherits, NoTimeout removes the bound
})
```

Give `ctx` a deadline to bound the whole call including retries; `Timeout`
bounds one attempt.

### Retries

The client does not retry. `WithRetry(systemone.DefaultRetry())` turns on two
retries with exponential backoff, jitter, and `retry-after` support; adjust the
`RetryPolicy` fields or supply your own `Retry` predicate. `Request.Retry` sets
a policy for one call, which is worth reaching for when a cheap three-question
triage and an expensive two-hundred-question fan-out share a client. A retried
request may be evaluated more than once by the service.

### Errors

An unsuccessful response is a `*systemone.Error` carrying the status, the raw
body, the response headers, and the `x-typesafe-request-id`. Classify it with
`errors.Is`:

```go
switch {
case errors.Is(err, systemone.ErrRateLimit):     // 429
case errors.Is(err, systemone.ErrUnprocessable): // 422, body names the field
case errors.Is(err, systemone.ErrOverloaded):    // 529
case errors.Is(err, systemone.ErrServer):        // any 5xx
}
```

### Logging

The client is silent until you give it a logger. `WithLogger(log)` writes one
`log/slog` record per HTTP attempt at the level in `$TYPESAFE_LOG_LEVEL`
(`debug`, `info`, `warn`, `error`, `off`; default `info`), which
`WithLogLevel` overrides.

At `info` each record is a summary: method, URL, attempt, duration, status, and
request ID. At `debug` it also carries headers and bodies. Credential headers
are redacted; bodies are not, and the request body holds your state.

### Forward compatibility

Two escape hatches let you use an API feature before this package models it.
Both skip every check the typed path applies, so upgrade instead when you can.

`Request.Extra` adds top-level request fields. A key that collides with
`state`, `model` or `questions` is an error rather than an override.

```go
res, err := client.Do(ctx, systemone.Request{
	State:     ticket,
	Questions: []systemone.Question{team},
	Extra:     map[string]any{"beam_width": 4},
})
```

`RawQuestion` sends a question shape this package does not model. Its answer
comes back as undecoded JSON, and `Response.Raw()` gives you the whole body,
which is how you reach answer kinds the typed path skips.

```go
q := systemone.RawQuestion{
	QuestionID: "tally",
	Body:       map[string]any{"type": "tally", "instructions": "Count the line items"},
}
res, err := client.Ask(ctx, invoice, q)
answer, err := q.From(res) // json.RawMessage
```

## Contributing

CI runs on every pull request: build and race tests on Go
1.24 and current stable, `golangci-lint` (which covers `go vet`, `staticcheck`,
`errcheck`, `unused` and `gofmt`), a `go mod tidy` check, and `govulncheck`.

To run the same checks locally:

```sh
go test -race ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Releases and the versioning policy are described in
[CONTRIBUTING.md](CONTRIBUTING.md#versioning).

## Examples

[`examples/triage`](examples/triage) is the support-ticket walkthrough from the
[quick start](https://docs.typesafe.ai/introduction/quickstart), written with
this SDK: one request, three questions, routing in code.

```sh
export TYPESAFE_API_KEY=...
go run ./examples/triage
```

## Patterns

`pattern` composes answers the way the TypeSafe docs describe. It is ordinary
code over answers this package returns; the thresholds and weights are yours.

- `Thresholds.Band` splits confidence into low, medium, and high so riskier
  actions can sit behind a higher bar.
- `Composite` takes the weighted mean of normalized `Weigh`/`WeighNoul` terms.
  Changing a weight reranks existing answers without a new request.
- `Walk` classifies down a `Tree` one Choice per level, showing each option's
  subtree so the model can see what lives under a branch before committing.
  Every step carries the full sibling distribution, so a beam search over close
  branches is a few lines on top.

## License

MIT. See [LICENSE](LICENSE).
