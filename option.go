package systemone

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Environment variables read when the matching option is not given.
const (
	APIKeyEnv       = "TYPESAFE_API_KEY"
	BaseURLEnv      = "TYPESAFE_BASE_URL"
	DefaultModelEnv = "TYPESAFE_DEFAULT_MODEL"
	LogLevelEnv     = "TYPESAFE_LOG_LEVEL"
)

// Client defaults.
const (
	DefaultBaseURL = "https://api.typesafe.ai"
	DefaultModel   = "jev-latest"
	DefaultTimeout = 10 * time.Second
)

// ErrNoAPIKey is returned by [New] when no API key is given and APIKeyEnv is
// unset.
var ErrNoAPIKey = errors.New("systemone: no API key")

// ClientOption configures a [Client]. Explicit options take precedence over
// environment variables, which take precedence over the defaults.
type ClientOption interface {
	apply(*Client) error
}

type clientOptionFunc func(*Client) error

func (f clientOptionFunc) apply(c *Client) error { return f(c) }

// WithAPIKey sets the API key, overriding APIKeyEnv.
func WithAPIKey(key string) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		if key == "" {
			return ErrNoAPIKey
		}
		c.apiKey = key
		return nil
	})
}

// WithBaseURL sets the API root, overriding BaseURLEnv. Endpoint paths are
// appended to it.
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

// WithModel sets the model used when a request does not name one, overriding
// DefaultModelEnv. See https://docs.typesafe.ai/models.
func WithModel(model string) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		if model == "" {
			return errors.New("systemone: model is empty")
		}
		c.model = model
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
// Content-Type and User-Agent, which the client always sets itself.
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
// The level comes from LogLevelEnv, which accepts debug, info, warn, error and
// off, and defaults to info; [WithLogLevel] overrides it. At info each record
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

// WithLogLevel sets the level [WithLogger] logs at, overriding LogLevelEnv.
// Pass [LevelOff] to disable logging without removing the logger.
func WithLogLevel(level slog.Level) ClientOption {
	return clientOptionFunc(func(c *Client) error {
		c.logger.level = level
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
