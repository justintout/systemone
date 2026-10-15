# systemone

[![CI](https://github.com/justintout/systemone/actions/workflows/ci.yml/badge.svg)](https://github.com/justintout/systemone/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/justintout/systemone.svg)](https://pkg.go.dev/github.com/justintout/systemone)

A Go client for System One APIs. A System One model answers typed questions
about a state: it returns a probability, a choice among fixed options, or a
position on an ordered scale, and writes no prose. TypeSafe
[defined the API](https://docs.typesafe.ai), and other providers now serve
models that speak it.

This module sends one state and a set of questions to any of those deployments
and returns one answer per question. Each answer is bound to the Go type you
declared the question with, so the compiler checks options and score levels.
Your code never compares them against string literals.

```
go get github.com/justintout/systemone
```

> This is an unofficial, community-maintained client. TypeSafe, Cloudflare and
> Microsoft do not build, endorse, or support it.

## Usage

A question is a value. Declare it once with the Go type its answer uses, then
read the answer back through the same handle.

```go
type Team string

const (
	Billing   Team = "billing"
	Technical Team = "technical"
)

var (
	team = systemone.NewChoice[Team]("team", "Which team should handle this message?",
		systemone.Option(Billing, "Payments, invoicing, refunds"),
		systemone.Option(Technical, "Bugs, outages, integrations"),
	)
	urgent = systemone.NewNoul("urgent", "Does this convey urgency?")
)

client, err := jev.New() // or clef.New, foundry.New, systemone.New

res, err := client.Ask(ctx, ticket, team, urgent)

t, err := team.From(res)   // systemone.ChoiceAnswer[Team]
u, err := urgent.From(res) // systemone.NoulAnswer

fmt.Println(t.Value, t.Confidence, u.Value)
```

`From` is the only way to reach an answer, which is what keeps the typing
honest: there is no untyped map to fall back to. `t.Value` is a `Team`, so a
`switch` over it is checked against the set the question declared. A typo in an
option name, or a level enum borrowed from a different question, is a build
failure rather than a branch that silently never fires.

## Deployments

Questions and answers belong to package `systemone` and are the same for every
deployment. Only the client changes. Each client takes the same
`systemone.Question` values and returns the same `*systemone.Response`, and all
of them satisfy `systemone.Asker`, so code that asks questions does not need
to know which deployment answers them.

| Package | Deployment | Extensions |
| --- | --- | --- |
| `jev` | TypeSafe's hosted [Jev](https://docs.typesafe.ai/models) models | Images, Box questions, long context |
| `clef` | Cloudflare's [Clef](https://developers.cloudflare.com/workers-ai/models/clef/) models on Workers AI | Images |
| `foundry` | Microsoft's [Microsoft-Decision-1](https://learn.microsoft.com/azure/foundry/foundry-models/how-to/use-foundry-models-microsoft-decision) in Foundry | None |
| `systemone` | Any server that implements the base API | None |

Extensions travel in the deployment's own request type, `jev.Request` or
`clef.Request`, so only that deployment's client sends them. A `jev.Box`
handed to another client is a build failure. A `jev.Image` placed in another
client's request fails before it is sent.

### Jev

`jev.New` reads `TYPESAFE_API_KEY`, `TYPESAFE_BASE_URL`,
`TYPESAFE_DEFAULT_MODEL` and `TYPESAFE_LOG_LEVEL` (`debug`, `info`, `warn`,
`error`, `off`). It sends `jev-latest` unless told otherwise. It compresses
request bodies of 1 KiB or more with zstd, as TypeSafe's SDKs do;
`systemone.WithCompression(false)` turns that off.

#### Images

An image goes anywhere text can: the state, a question's instructions, or its
criteria. A `jev.Image` placed as a value becomes the image there, and `Ref()`
writes it into a sentence. Every image placed in the request is also listed in
`Images`, which uploads it once:

```go
door, err := jev.NewImage(doorJPG) // PNG or JPEG
badge, err := jev.NewImage(badgeJPG)

var (
	match = systemone.NewNoul("match",
		"Is the person in `door` the person in this badge photo: "+badge.Ref()+"?")
	where = jev.NewBox("where", "the person in `door`")
)

res, err := client.Do(ctx, jev.Request{
	State:     map[string]any{"door": door},
	Questions: []systemone.Question{match},
	Boxes:     []*jev.Box{where},
	Images:    []jev.Image{door, badge},
	Model:     jev.Preview, // Box needs Jev 1.14
})
b, err := where.From(res) // jev.BoxAnswer: Top, Bottom, Left, Right, Confidence
crop := b.Rect(width, height)
```

A `jev.Box` finds where something is in an image. Only Jev answers it, so it
is not a `systemone.Question`: it goes in `Boxes`, and a Box handed to another
deployment's client is a build failure.

Images travel as multipart file parts, so they are not inflated by base64. A
request that places an image without listing it, or lists one it never places,
fails before it is sent. The client sets the
`TypeSafe-Danger-Zone-Images` header TypeSafe requires on any request with an
image or a Box question. Images and Box are experimental at TypeSafe, and
[their limits](https://docs.typesafe.ai/images#limits) are enforced there, not
here.

#### Long context

`Request.LongContext` sets `TypeSafe-Danger-Zone-Long-Context`, which raises
the request's context limits to 262k tokens at some cost in speed and accuracy.

### Clef

`clef.New` takes the Workers AI account ID and a Workers AI token:

```go
client, err := clef.New(accountID, systemone.WithAPIKey(os.Getenv("CLOUDFLARE_API_TOKEN")))
```

That sends `clef`; set `Request.Model` to `clef.Flash` for the faster one. Clef
reads images placed before the state rather than referenced from it, and `clef.NewImage` reads the content type from the
bytes:

```go
data, err := os.ReadFile("checkout.png")
img, err := clef.NewImage(data) // PNG, JPEG or WebP

res, err := client.Do(ctx, clef.Request{
	State:     "A customer sent this screenshot of the checkout page.",
	Questions: []systemone.Question{broken, severity},
	Images:    []clef.Image{img},
})
```

Cloudflare's limits (four images, 4 MiB and 16 megapixels each) are enforced by
the deployment, not here. A fine-tuned or self-hosted Clef is `clef.New` with
`systemone.WithBaseURL` and `systemone.WithEvaluatePath`.

### Microsoft-Decision-1

`foundry.New` takes the Foundry resource endpoint and the deployment name.
Requests name the deployment, not the model, and the response's `Model` reports
the underlying model. Foundry takes an Entra ID token as a bearer token, or a
resource API key in its `api-key` header:

```go
client, err := foundry.New(endpoint, "triage-d1", systemone.WithAPIKey(entraToken))

client, err := foundry.New(endpoint, "triage-d1",
	systemone.WithAPIKey(key), systemone.WithAPIKeyHeader(foundry.APIKeyHeader))
```

The client sends the token it was built with and never refreshes it, so this
module does not depend on Azure's SDK. An Entra token expires, usually within
an hour. A long-running process should use an API key, or get tokens from
[azidentity](https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/azidentity)
and build a new client before each one expires. A client is cheap to build,
and azidentity caches the token until it nears expiry:

```go
cred, err := azidentity.NewDefaultAzureCredential(nil)

tok, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{foundry.TokenScope}})
client, err := foundry.New(endpoint, "triage-d1", systemone.WithAPIKey(tok.Token))
// Build the next client before tok.ExpiresOn.
```

Microsoft-Decision-1 reads text and JSON state, with no images, and has no
model listing.

### Your own server

A server that implements only the base API has no default, so name it:

```go
client, err := systemone.New(
	systemone.WithBaseURL("https://decisions.internal"),
	systemone.WithModel("support-ft"),
	systemone.WithAPIKey(key),
)
```

`WithEvaluatePath`, `WithModelsPath`, `WithResultKey`, `WithRequestIDHeader`
and `WithAPIKeyHeader` cover a deployment whose paths, envelope, request ID
header or credential header differ. A `{model}` in the evaluate path is
replaced with the model of the request. `WithCompression(true)` sends zstd
request bodies to a server that accepts them.

See [`examples/`](examples) for worked examples against Jev and Clef.

## Primitives

| Question | Answer | Use it for |
| --- | --- | --- |
| `NewChoice[T ~string]` | `ChoiceAnswer[T]`: `Value`, `Probabilities`, `Confidence` | One of a defined set |
| `NewNoul` | `NoulAnswer`: `Value`, the probability of yes | Whether a condition holds |
| `NewScore[T ~int]` | `ScoreAnswer[T]`: `Value`, `Level`, `Legend`, `Probabilities`, `Confidence` | Degree along ordered levels |

Instructions and every criterion take a string or any value that encodes to a
JSON object or array, so a question can carry named data it refers to by
backticked path. Your own types work; there is no reason to reach for a map:

```go
type Candidate struct {
	Name         string `json:"name"`
	LastEmployer string `json:"last_employer"`
}

type match struct {
	Candidate Candidate `json:"candidate"`
	Question  string    `json:"question"`
}

var duplicate = systemone.NewNoul("duplicate", match{
	Candidate: Candidate{Name: "John Smith", LastEmployer: "Google"},
	Question:  "Is the resume for the same person as `candidate`?",
})
```

Build errors, such as a duplicate option or a level declared out of order, are
reported by the call that sends the question rather than at construction, so
questions can be package-level variables.

A `ScoreAnswer`'s `Legend` holds each level's description as it was sent, so
structured levels come back structured. `Describe()` renders the current level
as text, JSON-encoding a structured one.

## Client

Every constructor takes the same `systemone.ClientOption` values. `jev.New`,
`clef.New` and `foundry.New` apply them after the deployment's defaults, so
an option overrides a default:

```go
client, err := systemone.New(
	systemone.WithBaseURL("https://decisions.internal"),
	systemone.WithModel("support-ft"), // for requests that name no model
	systemone.WithAPIKey(key),
	systemone.WithHTTPClient(hc),
	systemone.WithTimeout(30*time.Second), // per attempt
	systemone.WithRetry(systemone.DefaultRetry()),
	systemone.WithHeader("X-Trace-Id", id),
	systemone.WithLogger(slog.Default()),
)
```

`Models` lists the models the account can send, on a deployment that publishes
a listing; elsewhere it returns `systemone.ErrNoModelList`. `Do` takes a
`Request` when one call needs to differ from the client's defaults:

```go
res, err := client.Do(ctx, systemone.Request{
	State:     ticket,
	Questions: []systemone.Question{team, urgent},
	Model:     "support-ft-2",
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
body, the response headers, and the request ID, read from whichever header the
deployment names. Classify it with
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
`log/slog` record per HTTP attempt at `info`, or the level `WithLogLevel` sets.

At `info` each record is a summary: method, URL, attempt, duration, status, and
request ID. At `debug` it also carries headers and bodies. Credential headers
are redacted; bodies are not, and the request body holds your state.

## Patterns

`pattern` composes answers into larger decisions, following the patterns in
TypeSafe's documentation. It is ordinary code over the answers this module
returns, so it works with every deployment. You choose the thresholds and
weights.

- `Thresholds.Band` splits confidence into low, medium, and high so riskier
  actions can sit behind a higher bar.
- `Composite` takes the weighted mean of normalized `Weigh`/`WeighNoul` terms.
  Changing a weight reranks existing answers without a new request.
- `Walk` classifies down a `Tree` one Choice per level, showing each option's
  subtree so the model can see what lives under a branch before committing.
  Every step carries the full sibling distribution, so a beam search over close
  branches is a few lines on top.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the checks CI runs, the conventions
the code follows, and the versioning and release policy.

## License

MIT. See [LICENSE](LICENSE).
