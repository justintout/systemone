package systemone

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// DefaultTimeout bounds one attempt unless [WithTimeout] says otherwise.
const DefaultTimeout = 10 * time.Second

// ErrNoAPIKey is returned by [New] when no API key is given.
var ErrNoAPIKey = errors.New("systemone: no API key")

// ClientOption configures a [Client]. The clients in packages jev and clef take
// the same options, applied after their own defaults, so an option given there
// overrides the deployment's setting.
type ClientOption interface {
	apply(*Client) error
}

type clientOptionFunc func(*Client) error

func (f clientOptionFunc) apply(c *Client) error { return f(c) }

// WithAPIKey sets the API key.
func WithAPIKey(key string) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		if key == "" {
			return ErrNoAPIKey
		}
		c.apiKey = key
		return nil
	})
}

// WithBaseURL sets the API root. Endpoint paths are appended to it.
func WithBaseURL(rawURL string) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		u, err := url.Parse(rawURL)
		if err != nil {
			return fmt.Errorf("systemone: base URL: %w", err)
		}
		if u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("systemone: base URL %q is not absolute", rawURL)
		}
		u.Path = strings.TrimSuffix(u.Path, "/")
		c.baseURL = u
		return nil
	})
}

// WithModel sets the model used when a request does not name one.
func WithModel(model string) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		if model == "" {
			return errors.New("systemone: model is empty")
		}
		c.model = model
		return nil
	})
}

// WithEvaluatePath sets the path an evaluation is posted to, appended to the
// base URL. A [ModelPlaceholder] in it is replaced with the model of the
// request, for deployments that name the model in the URL as well as the body.
func WithEvaluatePath(path string) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		if path == "" {
			return errors.New("systemone: evaluate path is empty")
		}
		c.evaluatePath = path
		return nil
	})
}

// WithModelsPath sets the path [Client.Models] reads. An empty path says the
// deployment publishes no listing, and Models then returns [ErrNoModelList].
func WithModelsPath(path string) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		c.modelsPath = path
		return nil
	})
}

// WithResultKey sets the response field the payload is wrapped in, for a
// deployment that wraps it in an envelope. An empty key says the body is the
// payload.
func WithResultKey(key string) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		c.resultKey = key
		return nil
	})
}

// WithRequestIDHeader sets the response header read into [Response.RequestID]
// and [Error.RequestID]. An empty name says the deployment sends none.
func WithRequestIDHeader(name string) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		c.requestIDHdr = name
		return nil
	})
}

// WithHTTPClient sets the HTTP client used for every request. Use it to
// configure transport, proxies, or connection pooling. The client's own Timeout
// field, if set, bounds the whole call including retries, which is separate
// from [WithTimeout].
func WithHTTPClient(hc *http.Client) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		if hc == nil {
			return errors.New("systemone: nil HTTP client")
		}
		c.http = hc
		return nil
	})
}

// WithHeader adds a header sent on every request. A [Request] header of the
// same name replaces it for that request, except for Authorization, Accept,
// Content-Type, Content-Encoding and User-Agent, which the client sets itself.
// [WithUserAgent] changes the last of those.
func WithHeader(key, value string) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		if key == "" {
			return errors.New("systemone: empty header name")
		}
		c.header.Set(key, value)
		return nil
	})
}

// WithTimeout bounds one attempt, not the whole call. A zero duration removes
// the bound. The default is [DefaultTimeout].
func WithTimeout(d time.Duration) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		if d < 0 {
			return errors.New("systemone: negative timeout")
		}
		c.timeout = d
		return nil
	})
}

// WithRetry turns on retries. The client does not retry by default; pass
// [DefaultRetry] for a sensible policy.
func WithRetry(p RetryPolicy) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		if p.MaxRetries < 0 {
			return errors.New("systemone: negative MaxRetries")
		}
		if p.Jitter < 0 || p.Jitter > 1 {
			return fmt.Errorf("systemone: Jitter is %v, want between 0 and 1", p.Jitter)
		}
		c.retry = p
		return nil
	})
}

// WithLogger sends one record per HTTP attempt to log. The client logs nothing
// until this option is given, so it stays quiet inside a library by default.
//
// The level defaults to info; [WithLogLevel] changes it. At info each record
// is a summary: method, URL, attempt, duration, status, and request ID. At
// debug it also carries headers and bodies.
//
// Credential headers are redacted. Bodies are not, and the request body holds
// the state, so debug logging writes whatever you are evaluating to the log.
func WithLogger(log *slog.Logger) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		if log == nil {
			return errors.New("systemone: nil logger")
		}
		c.logger.log = log
		return nil
	})
}

// WithLogLevel sets the level [WithLogger] logs at.
// Pass [LevelOff] to disable logging without removing the logger.
func WithLogLevel(level slog.Level) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		c.logger.level = level
		return nil
	})
}

// compressionThreshold is the smallest body [WithCompression] compresses,
// below which the saving does not cover the frame. It matches TypeSafe's SDKs.
const compressionThreshold = 1024

// WithCompression compresses request bodies of 1 KiB or more with zstd, sent
// as Content-Encoding: zstd. The client in package jev turns it on, since
// TypeSafe accepts it; turn it on elsewhere only for a server that does.
func WithCompression(on bool) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		if !on {
			c.zstd = nil
			return nil
		}
		// The API caps the zstd window at 8 MiB. EncodeAll on one goroutine
		// is safe for concurrent use, so one encoder serves the client.
		enc, err := zstd.NewWriter(nil, zstd.WithWindowSize(8<<20), zstd.WithEncoderConcurrency(1))
		if err != nil {
			return fmt.Errorf("systemone: zstd: %w", err)
		}
		c.zstd = enc
		return nil
	})
}

// WithUserAgent replaces the default User-Agent header.
func WithUserAgent(ua string) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		if ua == "" {
			return errors.New("systemone: empty user agent")
		}
		c.userAgent = ua
		return nil
	})
}
