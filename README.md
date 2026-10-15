# systemone

[![CI](https://github.com/justintout/systemone/actions/workflows/ci.yml/badge.svg)](https://github.com/justintout/systemone/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/justintout/systemone.svg)](https://pkg.go.dev/github.com/justintout/systemone)

A Go client for the [TypeSafe System One API](https://docs.typesafe.ai). Send
one state and a set of questions; get one answer per question, each bound to
the Go type you declared the question with, so options and score levels are
checked by the compiler rather than compared against string literals.

```
go get github.com/justintout/systemone
```

> This is an unofficial, community-maintained client. It is not built,
> endorsed, or supported by TypeSafe. The official SDKs are
> [Python](https://docs.typesafe.ai/sdk/python) and
> [JavaScript](https://docs.typesafe.ai/sdk/javascript).

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

client, err := jev.New() // reads TYPESAFE_API_KEY

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

The client depends on the deployment. Questions and answers do not: every
client takes the same `systemone.Question` values and returns the same
`*systemone.Response`, and all of them satisfy `systemone.Asker`.

| Package | Deployment |
| --- | --- |
| `jev` | TypeSafe's hosted Jev models |
| `clef` | Cloudflare's [Clef](https://developers.cloudflare.com/workers-ai/models/clef/) models on Workers AI |
| `systemone` | A server that implements the base API itself |

`jev.New` reads `TYPESAFE_API_KEY`, `TYPESAFE_BASE_URL`,
`TYPESAFE_DEFAULT_MODEL` and `TYPESAFE_LOG_LEVEL`. It compresses request bodies
of 1 KiB or more with zstd, as TypeSafe's SDKs do;
`systemone.WithCompression(false)` turns that off, and `WithCompression(true)`
turns it on for another deployment that accepts it.

### Jev images

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

### Your own server

A server that implements only the base API has no default, so name it:

```go
client, err := systemone.New(
	systemone.WithBaseURL("https://decisions.internal"),
	systemone.WithModel("support-ft"),
	systemone.WithAPIKey(key),
)
```

`WithEvaluatePath`, `WithModelsPath`, `WithResultKey` and `WithRequestIDHeader`
cover a deployment whose paths, envelope or request ID header differ. A
`{model}` in the evaluate path is replaced with the model of the request.

See [`examples/`](examples) for more, including the support-ticket walkthrough
from the [quick start](https://docs.typesafe.ai/introduction/quickstart)
written with this SDK.

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

```go
client, err := jev.New(
	systemone.WithAPIKey(key),          // default: $TYPESAFE_API_KEY
	systemone.WithBaseURL(url),         // default: $TYPESAFE_BASE_URL, then api.typesafe.ai
	systemone.WithModel("jev-1.13.0"),  // default: $TYPESAFE_DEFAULT_MODEL, then jev-latest
	systemone.WithHTTPClient(hc),
	systemone.WithTimeout(30*time.Second), // per attempt
	systemone.WithRetry(systemone.DefaultRetry()),
	systemone.WithHeader("X-Trace-Id", id),
	systemone.WithLogger(slog.Default()),
)
```

The same options configure `clef.New` and `systemone.New`. `Models` lists the
models the account can send. `Do` takes a `Request` when one call needs to
differ from the client's defaults:

```go
res, err := client.Do(ctx, jev.Request{
	State:     ticket,
	Questions: []systemone.Question{team, urgent},
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
`jev.New` reads it from `$TYPESAFE_LOG_LEVEL` (`debug`, `info`, `warn`,
`error`, `off`).

At `info` each record is a summary: method, URL, attempt, duration, status, and
request ID. At `debug` it also carries headers and bodies. Credential headers
are redacted; bodies are not, and the request body holds your state.

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

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the checks CI runs, the conventions
the code follows, and the versioning and release policy.

## License

MIT. See [LICENSE](LICENSE).
