package systemone

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"
)

// LevelOff disables logging. It sits above every level [slog] defines, so no
// record is ever enabled.
const LevelOff = slog.Level(math.MaxInt32)

// parseLogLevel reads the level names LogLevelEnv accepts, which match the
// other TypeSafe SDKs.
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
		return LevelOff, true
	}
	return 0, false
}

// logger wraps the caller's slog.Logger. A client with no logger does nothing.
type logger struct {
	log   *slog.Logger
	level slog.Level
}

func (l logger) enabled(level slog.Level) bool {
	return l.log != nil && level >= l.level
}

// attempt logs one HTTP attempt once it has finished. At info it is a summary
// line; at debug it also carries headers and bodies.
func (l logger) attempt(ctx context.Context, method, endpoint string, attempt int, start time.Time, req *http.Request, reqBody []byte, resp *http.Response, respBody []byte, err error) {
	if !l.enabled(slog.LevelInfo) {
		return
	}
	attrs := []any{
		slog.String("method", method),
		slog.String("url", endpoint),
		slog.Int("attempt", attempt),
		slog.Duration("duration", time.Since(start)),
	}
	level := slog.LevelInfo
	switch {
	case err != nil:
		attrs = append(attrs, slog.Any("error", err))
		level = slog.LevelWarn
	case resp != nil:
		attrs = append(attrs, slog.Int("status", resp.StatusCode))
		if id := resp.Header.Get(requestIDHdr); id != "" {
			attrs = append(attrs, slog.String("request_id", id))
		}
		if resp.StatusCode >= 400 {
			level = slog.LevelWarn
		}
	}
	if l.enabled(slog.LevelDebug) {
		level = slog.LevelDebug
		if req != nil {
			attrs = append(attrs, slog.Any("request_headers", redact(req.Header)))
		}
		if reqBody != nil {
			attrs = append(attrs, slog.String("request_body", string(reqBody)))
		}
		if resp != nil {
			attrs = append(attrs, slog.Any("response_headers", redact(resp.Header)))
		}
		if respBody != nil {
			attrs = append(attrs, slog.String("response_body", string(respBody)))
		}
	}
	l.log.Log(ctx, level, "systemone request", attrs...)
}

// redact replaces credential header values. Bodies are not redacted, and the
// request body holds the state, so debug logging exposes it.
func redact(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for name, values := range h {
		if isSecretHeader(name) {
			out[name] = "REDACTED"
			continue
		}
		out[name] = strings.Join(values, ", ")
	}
	return out
}

func isSecretHeader(name string) bool {
	lower := strings.ToLower(name)
	if lower == "authorization" || lower == "cookie" || lower == "set-cookie" {
		return true
	}
	return strings.Contains(lower, "key") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "secret")
}
