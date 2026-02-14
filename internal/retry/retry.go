// Package retry provides a generic retry-with-backoff helper.
package retry

import (
	"context"
	"time"
)

const (
	initialBackoff = 100 * time.Millisecond
	maxBackoff     = 30 * time.Second
)

// Do calls fn repeatedly until it succeeds or ctx is canceled. On failure it
// waits with exponential backoff (100ms initial, doubling, capped at 30s)
// before retrying. If ctx is canceled during a backoff wait, Do returns the
// zero value and the context error.
func Do[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	backoff := initialBackoff
	for {
		val, err := fn()
		if err == nil {
			return val, nil
		}

		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}
