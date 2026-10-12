package gitlab

import (
	"context"
	"testing"
	"time"
)

// SetRetryWait replaces the wait before the single 429 retry for the length of the test.
func SetRetryWait(t testing.TB, fn func(ctx context.Context, d time.Duration) error) {
	t.Helper()
	old := retryWait
	retryWait = fn
	t.Cleanup(func() { retryWait = old })
}

// RetryWait calls the real (or currently replaced) wait.
func RetryWait(ctx context.Context, d time.Duration) error { return retryWait(ctx, d) }
