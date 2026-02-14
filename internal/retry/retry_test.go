package retry

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestDo_ImmediateSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		val, err := Do(t.Context(), func() (int, error) {
			return 42, nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != 42 {
			t.Fatalf("got %d, want 42", val)
		}
	})
}

func TestDo_RetriesOnTransientError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		val, err := Do(t.Context(), func() (string, error) {
			calls++
			if calls < 3 {
				return "", errors.New("transient")
			}
			return "ok", nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "ok" {
			t.Fatalf("got %q, want %q", val, "ok")
		}
		if calls != 3 {
			t.Fatalf("expected 3 calls, got %d", calls)
		}
	})
}

func TestDo_ContextCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())

		errCh := make(chan error, 1)
		go func() {
			_, err := Do(ctx, func() (int, error) {
				return 0, errors.New("always fails")
			})
			errCh <- err
		}()

		// Let the first attempt and backoff start.
		synctest.Wait()

		cancel()
		synctest.Wait()

		err := <-errCh
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	})
}

func TestDo_ExponentialBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Track timestamps of each call to fn.
		var timestamps []time.Duration
		start := time.Now()
		calls := 0

		val, err := Do(t.Context(), func() (int, error) {
			timestamps = append(timestamps, time.Since(start))
			calls++
			if calls < 5 {
				return 0, errors.New("fail")
			}
			return 99, nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != 99 {
			t.Fatalf("got %d, want 99", val)
		}

		// Expected schedule:
		//   call 1: t=0
		//   call 2: t=100ms  (waited 100ms)
		//   call 3: t=300ms  (waited 200ms)
		//   call 4: t=700ms  (waited 400ms)
		//   call 5: t=1500ms (waited 800ms)
		want := []time.Duration{
			0,
			100 * time.Millisecond,
			300 * time.Millisecond,
			700 * time.Millisecond,
			1500 * time.Millisecond,
		}
		if len(timestamps) != len(want) {
			t.Fatalf("got %d timestamps, want %d", len(timestamps), len(want))
		}
		for i, w := range want {
			if timestamps[i] != w {
				t.Errorf("call %d: got %v, want %v", i+1, timestamps[i], w)
			}
		}
	})
}

func TestDo_BackoffCapsAt30s(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var timestamps []time.Duration
		start := time.Now()
		calls := 0

		// Need enough failures that backoff exceeds 30s.
		// 100ms, 200ms, 400ms, 800ms, 1.6s, 3.2s, 6.4s, 12.8s, 25.6s, 30s, 30s
		// That's 11 backoffs before the cap takes effect, so 12 calls to hit it.
		// We'll fail 12 times, succeed on call 13.
		val, err := Do(t.Context(), func() (int, error) {
			timestamps = append(timestamps, time.Since(start))
			calls++
			if calls < 13 {
				return 0, errors.New("fail")
			}
			return 1, nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != 1 {
			t.Fatalf("got %d, want 1", val)
		}

		// Verify the last two gaps are exactly 30s (the cap).
		if len(timestamps) < 3 {
			t.Fatal("not enough timestamps")
		}
		for i := len(timestamps) - 2; i < len(timestamps); i++ {
			gap := timestamps[i] - timestamps[i-1]
			if gap != 30*time.Second {
				t.Errorf("gap before call %d: got %v, want 30s", i+1, gap)
			}
		}
	})
}
