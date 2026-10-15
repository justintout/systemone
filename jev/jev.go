// Package jev is a client for TypeSafe's hosted System One API and its Jev
// models. See https://docs.typesafe.ai.
//
// Questions and answers are the ones package systemone defines:
//
//	client, err := jev.New() // reads TYPESAFE_API_KEY
//	res, err := client.Ask(ctx, ticket, team, urgent)
//	t, err := team.From(res)
package jev

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/justintout/systemone"
	"github.com/justintout/systemone/internal/bridge"
)

// BaseURL is TypeSafe's hosted API.
const BaseURL = "https://api.typesafe.ai"

// Model aliases. An alias moves when a new release ships, so send a versioned
// ID such as jev-1.13.0 when answers have to stay put. See
// https://docs.typesafe.ai/models.
const (
	// Latest is the most recent stable release, and the default.
	Latest = "jev-latest"
	// Preview is the most recent release, stable or not. [Box] questions need
	// it while Jev 1.14 is a preview.
	Preview = "jev-preview"
)

// Headers that turn on TypeSafe's experimental features for one request.
// [Client.Do] sets them; they are exported for logs and proxies.
const (
	// ImagesHeader turns on images and Box questions. Do sets it on a request
	// with an image or a Box question.
	ImagesHeader = "TypeSafe-Danger-Zone-Images"
	// LongContextHeader raises both context limits to 262k tokens, at some
	// cost in speed and accuracy. Do sets it when [Request.LongContext] is
	// true.
	LongContextHeader = "TypeSafe-Danger-Zone-Long-Context"
)

// Environment variables [New] reads. The names match TypeSafe's own SDKs.
const (
	APIKeyEnv       = "TYPESAFE_API_KEY"
	BaseURLEnv      = "TYPESAFE_BASE_URL"
	DefaultModelEnv = "TYPESAFE_DEFAULT_MODEL"
	LogLevelEnv     = "TYPESAFE_LOG_LEVEL"
)

// Client sends requests to Jev. It is safe for concurrent use.
type Client struct {
	base *systemone.Client
}

// New builds a client for TypeSafe's hosted API. It compresses request bodies
// with zstd, which [systemone.WithCompression] turns off. It reads the API key from
// APIKeyEnv, the base URL from BaseURLEnv, the default model from
// DefaultModelEnv and the log level from LogLevelEnv, which accepts debug,
// info, warn, error and off. Options override all of them.
func New(opts ...systemone.ClientOption) (*Client, error) {
	defaults := []systemone.ClientOption{
		systemone.WithBaseURL(BaseURL),
		systemone.WithModel(Latest),
		systemone.WithRequestIDHeader("X-Typesafe-Request-Id"),
		systemone.WithCompression(true),
	}
	if v := os.Getenv(APIKeyEnv); v != "" {
		defaults = append(defaults, systemone.WithAPIKey(v))
	}
	if v := os.Getenv(BaseURLEnv); v != "" {
		defaults = append(defaults, systemone.WithBaseURL(v))
	}
	if v := os.Getenv(DefaultModelEnv); v != "" {
		defaults = append(defaults, systemone.WithModel(v))
	}
	if v := os.Getenv(LogLevelEnv); v != "" {
		level, ok := parseLogLevel(v)
		if !ok {
			return nil, fmt.Errorf("jev: %s=%q is not a level", LogLevelEnv, v)
		}
		defaults = append(defaults, systemone.WithLogLevel(level))
	}
	base, err := systemone.New(append(defaults, opts...)...)
	if err != nil {
		return nil, err
	}
	return &Client{base: base}, nil
}

func parseLogLevel(name string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	case "off":
		return systemone.LevelOff, true
	}
	return 0, false
}

// Model returns the model used for requests that do not name one.
func (c *Client) Model() string { return c.base.Model() }

// Models lists the models and aliases the account can send.
func (c *Client) Models(ctx context.Context) ([]systemone.Model, error) {
	return c.base.Models(ctx)
}

// Request is one evaluation. Its fields other than Boxes, Images and
// LongContext mean what they do in [systemone.Request].
type Request struct {
	State     any
	Questions []systemone.Question
	// Boxes are asked alongside Questions, sharing their IDs' namespace.
	Boxes []*Box
	// Images lists every [Image] placed in the state, instructions or
	// criteria. A request that places an image without listing it, or lists
	// one it never places, fails before it is sent.
	Images []Image
	// LongContext raises the request's context limits to 262k tokens. It is
	// slower and less accurate than staying within the defaults, so turn it
	// on only for requests that need it. See https://docs.typesafe.ai/models.
	LongContext bool
	Model       string
	Header      http.Header
	Retry       *systemone.RetryPolicy
	Timeout     time.Duration
}

// Ask evaluates state against questions using the client's default model.
func (c *Client) Ask(ctx context.Context, state any, questions ...systemone.Question) (*systemone.Response, error) {
	return c.Do(ctx, Request{State: state, Questions: questions})
}

// Do sends one evaluation request. It turns on images for a request with an
// image or a [Box] question, which TypeSafe requires and which is otherwise a
// 400.
func (c *Client) Do(ctx context.Context, req Request) (*systemone.Response, error) {
	listed, err := assets(req.Images)
	if err != nil {
		return nil, err
	}
	ext := bridge.Extension{Check: func(body []byte) error { return checkRefs(body, listed) }}
	for _, b := range req.Boxes {
		q, err := b.wire()
		if err != nil {
			return nil, err
		}
		ext.Questions = append(ext.Questions, q)
	}
	if len(listed) > 0 {
		ext.Fields = map[string]any{"assets": listed}
		ext.Wrap = func(body []byte) ([]byte, string, error) { return multipartBody(body, req.Images) }
	}
	images := len(listed) > 0 || len(req.Boxes) > 0
	header := req.Header
	if images || req.LongContext {
		// The caller's header may be shared across requests, so set ours on a
		// copy.
		header = req.Header.Clone()
		if header == nil {
			header = make(http.Header)
		}
		if images {
			header.Set(ImagesHeader, "true")
		}
		if req.LongContext {
			header.Set(LongContextHeader, "true")
		}
	}
	return bridge.Do[*systemone.Response](ctx, c.base, systemone.Request{
		State:     req.State,
		Questions: req.Questions,
		Model:     req.Model,
		Header:    header,
		Retry:     req.Retry,
		Timeout:   req.Timeout,
	}, ext)
}
