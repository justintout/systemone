package systemone

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Sentinels for classifying an [*Error] with [errors.Is]:
//
//	if errors.Is(err, systemone.ErrRateLimit) { ... }
//
// ErrServer also matches ErrOverloaded, since 529 is a server status.
var (
	ErrInvalidRequest   = errors.New("systemone: invalid request")
	ErrAuthentication   = errors.New("systemone: authentication failed")
	ErrPermissionDenied = errors.New("systemone: permission denied")
	ErrNotFound         = errors.New("systemone: not found")
	ErrUnprocessable    = errors.New("systemone: request failed validation")
	ErrRateLimit        = errors.New("systemone: rate limit exceeded")
	ErrOverloaded       = errors.New("systemone: service overloaded")
	ErrServer           = errors.New("systemone: server error")
)

// Error is an unsuccessful HTTP response from the API.
type Error struct {
	// StatusCode is the HTTP status code.
	StatusCode int
	// RequestID is the x-typesafe-request-id response header, empty if absent.
	RequestID string
	// Body is the raw response body, which describes the offending field on a
	// 422. It is not parsed, because the API does not contract its shape.
	Body []byte
	// Header is the response header.
	Header http.Header
	// RetryAfter is the wait the server asked for, or 0 if it asked for none.
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	var b strings.Builder
	if text := http.StatusText(e.StatusCode); text != "" {
		fmt.Fprintf(&b, "systemone: %s (%d)", text, e.StatusCode)
	} else {
		fmt.Fprintf(&b, "systemone: status %d", e.StatusCode)
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, " request %s", e.RequestID)
	}
	if body := strings.TrimSpace(string(e.Body)); body != "" {
		const max = 512
		if len(body) > max {
			body = body[:max] + "..."
		}
		fmt.Fprintf(&b, ": %s", body)
	}
	return b.String()
}

// Is reports whether the status this error carries is the class target names.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrInvalidRequest:
		return e.StatusCode == http.StatusBadRequest
	case ErrAuthentication:
		return e.StatusCode == http.StatusUnauthorized
	case ErrPermissionDenied:
		return e.StatusCode == http.StatusForbidden
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrUnprocessable:
		return e.StatusCode == http.StatusUnprocessableEntity
	case ErrRateLimit:
		return e.StatusCode == http.StatusTooManyRequests
	case ErrOverloaded:
		return e.StatusCode == statusOverloaded
	case ErrServer:
		return e.StatusCode >= 500 && e.StatusCode <= 599
	}
	return false
}

// statusOverloaded is the 529 TypeSafe returns when it is temporarily
// overloaded. It has no net/http constant.
const statusOverloaded = 529

// retryAfter reads the retry-after header, which is either a number of seconds
// or an HTTP date.
func retryAfter(h http.Header, now time.Time) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs * float64(time.Second))
	}
	t, err := http.ParseTime(v)
	if err != nil {
		return 0
	}
	if d := t.Sub(now); d > 0 {
		return d
	}
	return 0
}
