package systemone

import (
	"testing"
	"time"
)

// backoff doubles from InitialBackoff and stops at MaxBackoff. Jitter is
// checked as a band rather than an exact value, since it is random by design.
func TestBackoff(t *testing.T) {
	steady := RetryPolicy{InitialBackoff: 100 * time.Millisecond, MaxBackoff: time.Second}

	tests := []struct {
		name    string
		policy  RetryPolicy
		attempt int
		want    time.Duration
	}{
		{"first retry", steady, 0, 100 * time.Millisecond},
		{"doubles", steady, 1, 200 * time.Millisecond},
		{"doubles again", steady, 2, 400 * time.Millisecond},
		{"caps", steady, 5, time.Second},
		{"caps far out", steady, 40, time.Second},
		{"no initial backoff disables the wait", RetryPolicy{MaxBackoff: time.Second}, 3, 0},
		{"no cap keeps doubling", RetryPolicy{InitialBackoff: time.Second}, 2, 4 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.backoff(tt.attempt); got != tt.want {
				t.Errorf("backoff(%d) = %v, want %v", tt.attempt, got, tt.want)
			}
		})
	}

	t.Run("jitter stays within its band", func(t *testing.T) {
		policy := RetryPolicy{InitialBackoff: time.Second, MaxBackoff: time.Minute, Jitter: 0.25}
		low, high := 750*time.Millisecond, 1250*time.Millisecond
		var sawSpread bool
		for range 200 {
			got := policy.backoff(0)
			if got < low || got > high {
				t.Fatalf("backoff = %v, want within [%v, %v]", got, low, high)
			}
			if got != time.Second {
				sawSpread = true
			}
		}
		if !sawSpread {
			t.Error("jitter never moved the delay")
		}
	})
}

func TestRetryStatus(t *testing.T) {
	tests := []struct {
		name   string
		policy RetryPolicy
		status int
		want   bool
	}{
		{"default retries request timeout", RetryPolicy{}, 408, true},
		{"default retries rate limit", RetryPolicy{}, 429, true},
		{"default retries server error", RetryPolicy{}, 500, true},
		{"default retries overloaded", RetryPolicy{}, 529, true},
		{"default leaves client errors alone", RetryPolicy{}, 400, false},
		{"default leaves success alone", RetryPolicy{}, 200, false},
		{"a predicate replaces the default", RetryPolicy{
			Retry: func(status int) bool { return status == 503 },
		}, 500, false},
		{"a predicate is used as given", RetryPolicy{
			Retry: func(status int) bool { return status == 503 },
		}, 503, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.retryStatus(tt.status); got != tt.want {
				t.Errorf("retryStatus(%d) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
