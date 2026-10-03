package systemone

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Models the published deployments answer to. JevLatest is TypeSafe's alias;
// Clef and ClefFlash are Cloudflare's models, which speak the same API.
const (
	JevLatest = "jev-latest"
	Clef      = "clef"
	ClefFlash = "clef-flash"
)

// ModelPlaceholder is replaced with the model of the request in
// [Provider.EvaluatePath], for deployments that name the model in the URL.
const ModelPlaceholder = "{model}"

// ErrNoModelList is returned by [Client.Models] when the provider has no model
// listing.
var ErrNoModelList = errors.New("systemone: provider has no model listing")

// Provider is one deployment of the System One API: where it is, how it names
// the model, and how it wraps the payload. The request body and the answers
// are the same everywhere, so a Provider is the whole difference between
// TypeSafe's Jev and Cloudflare's Clef.
//
// [TypeSafe] and [Cloudflare] return the two published deployments. Build a
// Provider by hand for a fine-tuned or self-hosted model.
type Provider struct {
	// BaseURL is the API root, an absolute URL. Paths are appended to it.
	BaseURL string
	// DefaultModel is sent when a request does not name a model.
	DefaultModel string
	// EvaluatePath is the path an evaluation is posted to. A
	// [ModelPlaceholder] in it is replaced with the model of the request, for
	// deployments that name the model in the URL rather than only in the body.
	EvaluatePath string
	// ModelsPath is the path [Client.Models] reads. Empty means the deployment
	// publishes no listing, and Models returns [ErrNoModelList].
	ModelsPath string
	// ResultKey is the response field the payload is wrapped in. Empty means
	// the body is the payload.
	ResultKey string
	// RequestIDHeader is the response header carrying the request ID, read
	// into [Response.RequestID] and [Error.RequestID]. Empty means the
	// deployment sends none.
	RequestIDHeader string
}

// TypeSafe is the hosted System One API at api.typesafe.ai, which the client
// uses by default. See https://docs.typesafe.ai.
func TypeSafe() Provider {
	return Provider{
		BaseURL:         "https://api.typesafe.ai",
		DefaultModel:    JevLatest,
		EvaluatePath:    "/v1/systemone",
		ModelsPath:      "/v1/models",
		RequestIDHeader: "X-Typesafe-Request-Id",
	}
}

// Cloudflare is the Clef models on Workers AI, for the account with the given
// ID. Clef is API-compatible with Jev, but names the model in the URL, wraps
// the payload in the Cloudflare envelope, and publishes no System One model
// listing. The API key is a Workers AI token, not a TypeSafe one, so pass it
// with [WithAPIKey].
//
// A request for [ClefFlash] is routed to it by name:
//
//	c, err := systemone.New(
//		systemone.WithProvider(systemone.Cloudflare(accountID)),
//		systemone.WithAPIKey(os.Getenv("CLOUDFLARE_AUTH_TOKEN")),
//	)
//	res, err := c.Do(ctx, systemone.Request{State: s, Questions: qs, Model: systemone.ClefFlash})
//
// See https://developers.cloudflare.com/workers-ai/models/clef.
func Cloudflare(accountID string) Provider {
	return Provider{
		BaseURL:         "https://api.cloudflare.com",
		DefaultModel:    Clef,
		EvaluatePath:    "/client/v4/accounts/" + accountID + "/ai/run/@cf/cloudflare/" + ModelPlaceholder,
		ResultKey:       "result",
		RequestIDHeader: "Cf-Ray",
	}
}

// WithProvider points the client at one deployment, setting the base URL,
// default model, paths, envelope and request ID header together. It overrides
// BaseURLEnv and DefaultModelEnv, and a later option overrides it in turn.
func WithProvider(p Provider) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		if p.DefaultModel == "" {
			return errors.New("systemone: provider has no default model")
		}
		if p.EvaluatePath == "" {
			return errors.New("systemone: provider has no evaluate path")
		}
		if err := WithBaseURL(p.BaseURL).apply(c); err != nil {
			return err
		}
		c.model = p.DefaultModel
		c.evaluatePath = p.EvaluatePath
		c.modelsPath = p.ModelsPath
		c.resultKey = p.ResultKey
		c.requestIDHdr = p.RequestIDHeader
		return nil
	})
}

// evaluateEndpoint is the URL an evaluation of model is posted to.
func (c *Client) evaluateEndpoint(model string) string {
	path := strings.ReplaceAll(c.evaluatePath, ModelPlaceholder, model)
	return c.baseURL.JoinPath(path).String()
}

// unwrap takes the payload out of the provider's envelope.
func unwrap(data []byte, key string) ([]byte, error) {
	if key == "" {
		return data, nil
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	payload, ok := body[key]
	if !ok {
		return nil, fmt.Errorf("systemone: response has no %q field", key)
	}
	return payload, nil
}
