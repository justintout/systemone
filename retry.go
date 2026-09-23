package systemone

import (
	"math"
	"math/rand/v2"
	"time"
)

// RetryPolicy controls whether the client retries a failed attempt. The zero
// value performs no retries, which is the client default; pass
// [DefaultRetry] to [WithRetry] to turn retries on.
//
// Retries apply to the whole request, including transport failures, so a
// retried request may be evaluated more than once by the service.
type RetryPolicy struct {
	// MaxRetries is how many attempts follow the first one.
	MaxRetries int
	// InitialBackoff is the wait before the first retry. It doubles each
	// attempt up to MaxBackoff.
	InitialBackoff time.Duration
	// MaxBackoff caps the wait between attempts.
	MaxBackoff time.Duration
	// Jitter spreads each wait by up to this fraction in either direction, so
	// concurrent clients do not retry in lockstep. 0.25 means plus or minus a
	// quarter of the computed wait. It must be between 0 and 1: above 1 the
	// spread exceeds the delay and produces a negative wait.
	Jitter float64
	// Retry reports whether a response with this status should be retried. A
	// nil Retry retries 408, 429, and every 5xx.
	Retry func(status int) bool
	// RetryTransportError retries a request that failed without a response,
	// including a per-attempt timeout.
	RetryTransportError bool
	// RespectRetryAfter uses the server's retry-after header in place of the
	// computed backoff when the response carries one.
	RespectRetryAfter bool
}

// DefaultRetry is the policy to start from: two retries with exponential
// backoff from 500ms, capped at 5s, honoring retry-after.
func DefaultRetry() RetryPolicy {
	return RetryPolicy{
		MaxRetries:          2,
		InitialBackoff:      500 * time.Millisecond,
		MaxBackoff:          5 * time.Second,
		Jitter:              0.25,
		RetryTransportError: true,
		RespectRetryAfter:   true,
	}
}

func (p RetryPolicy) retryStatus(status int) bool {
	if p.Retry != nil {
		return p.Retry(status)
	}
	return status == 408 || status == 429 || (status >= 500 && status <= 599)
}

// backoff returns the wait before attempt n, counting the first attempt as 0.
func (p RetryPolicy) backoff(n int) time.Duration {
	d := p.InitialBackoff
	if d <= 0 {
		return 0
	}
	for range n {
		// Without a cap, doubling far enough wraps int64 negative, which
		// sleep reads as no wait at all.
		if d > math.MaxInt64/2 {
			break
		}
		d *= 2
		if p.MaxBackoff > 0 && d >= p.MaxBackoff {
			return p.jitter(p.MaxBackoff)
		}
	}
	if p.MaxBackoff > 0 && d > p.MaxBackoff {
		d = p.MaxBackoff
	}
	return p.jitter(d)
}

func (p RetryPolicy) jitter(d time.Duration) time.Duration {
	if p.Jitter <= 0 {
		return d
	}
	spread := float64(d) * p.Jitter
	return time.Duration(float64(d) + spread*(2*rand.Float64()-1))
}
