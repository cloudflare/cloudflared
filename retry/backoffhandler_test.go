package retry

import (
	"context"
	"testing"
	"time"
)

func immediateTimeAfter(time.Duration) <-chan time.Time {
	c := make(chan time.Time, 1)
	c <- time.Now()
	return c
}

func TestBackoffRetries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// make backoff return immediately
	backoff := BackoffHandler{maxRetries: 3, Clock: Clock{time.Now, immediateTimeAfter}}
	if !backoff.Backoff(ctx) {
		t.Fatalf("backoff failed immediately")
	}
	if !backoff.Backoff(ctx) {
		t.Fatalf("backoff failed after 1 retry")
	}
	if !backoff.Backoff(ctx) {
		t.Fatalf("backoff failed after 2 retry")
	}
	if backoff.Backoff(ctx) {
		t.Fatalf("backoff allowed after 3 (max) retries")
	}
}

func TestBackoffCancel(t *testing.T) {
	t.Parallel()

	ctx, cancelFunc := context.WithCancel(context.Background())
	// prevent backoff from returning normally
	after := func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	backoff := BackoffHandler{maxRetries: 3, Clock: Clock{time.Now, after}}
	cancelFunc()
	if backoff.Backoff(ctx) {
		t.Fatalf("backoff allowed after cancel")
	}
	if _, ok := backoff.GetMaxBackoffDuration(ctx); ok {
		t.Fatalf("backoff allowed after cancel")
	}
}

func TestBackoffGracePeriod(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	currentTime := time.Now()
	// make Clock.Now return whatever we like
	now := func() time.Time { return currentTime }
	// make backoff return immediately
	backoff := BackoffHandler{maxRetries: 1, Clock: Clock{now, immediateTimeAfter}}
	if !backoff.Backoff(ctx) {
		t.Fatalf("backoff failed immediately")
	}
	// the next call to Backoff would fail unless it's after the grace period
	gracePeriod := backoff.SetGracePeriod()
	// advance time to after the grace period, which at most will be 8 seconds, but we will advance +1 second.
	currentTime = currentTime.Add(gracePeriod + time.Second)
	if !backoff.Backoff(ctx) {
		t.Fatalf("backoff failed after the grace period expired")
	}
	// confirm we ignore grace period after backoff
	if backoff.Backoff(ctx) {
		t.Fatalf("backoff allowed after 1 (max) retry")
	}
}

func TestGetMaxBackoffDurationRetries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// make backoff return immediately
	backoff := BackoffHandler{maxRetries: 3, Clock: Clock{time.Now, immediateTimeAfter}}
	if _, ok := backoff.GetMaxBackoffDuration(ctx); !ok {
		t.Fatalf("backoff failed immediately")
	}
	backoff.Backoff(ctx) // noop
	if _, ok := backoff.GetMaxBackoffDuration(ctx); !ok {
		t.Fatalf("backoff failed after 1 retry")
	}
	backoff.Backoff(ctx) // noop
	if _, ok := backoff.GetMaxBackoffDuration(ctx); !ok {
		t.Fatalf("backoff failed after 2 retry")
	}
	backoff.Backoff(ctx) // noop
	if _, ok := backoff.GetMaxBackoffDuration(ctx); ok {
		t.Fatalf("backoff allowed after 3 (max) retries")
	}
	if backoff.Backoff(ctx) {
		t.Fatalf("backoff allowed after 3 (max) retries")
	}
}

func TestGetMaxBackoffDuration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// make backoff return immediately
	backoff := BackoffHandler{maxRetries: 3, Clock: Clock{time.Now, immediateTimeAfter}}
	if duration, ok := backoff.GetMaxBackoffDuration(ctx); !ok || duration != 2*time.Second {
		t.Fatalf("backoff returned %s instead of 2 seconds on first retry", duration)
	}
	backoff.Backoff(ctx) // noop
	if duration, ok := backoff.GetMaxBackoffDuration(ctx); !ok || duration != 4*time.Second {
		t.Fatalf("backoff returned %s instead of 4 seconds on second retry", duration)
	}
	backoff.Backoff(ctx) // noop
	if duration, ok := backoff.GetMaxBackoffDuration(ctx); !ok || duration != 8*time.Second {
		t.Fatalf("backoff returned %s instead of 8 seconds on third retry", duration)
	}
	backoff.Backoff(ctx) // noop
	if duration, ok := backoff.GetMaxBackoffDuration(ctx); ok || duration != 0 {
		t.Fatalf("backoff returned %s instead of 0 seconds after exhausting retries", duration)
	}
}

func TestBackoffRetryForever(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// make backoff return immediately
	backoff := BackoffHandler{maxRetries: 3, retryForever: true, Clock: Clock{time.Now, immediateTimeAfter}}
	if duration, ok := backoff.GetMaxBackoffDuration(ctx); !ok || duration != 2*time.Second {
		t.Fatalf("backoff returned %s instead of 2 seconds on first retry", duration)
	}
	backoff.Backoff(ctx) // noop
	if duration, ok := backoff.GetMaxBackoffDuration(ctx); !ok || duration != 4*time.Second {
		t.Fatalf("backoff returned %s instead of 4 seconds on second retry", duration)
	}
	backoff.Backoff(ctx) // noop
	if duration, ok := backoff.GetMaxBackoffDuration(ctx); !ok || duration != 8*time.Second {
		t.Fatalf("backoff returned %s instead of 8 seconds on third retry", duration)
	}
	if !backoff.Backoff(ctx) {
		t.Fatalf("backoff refused on fourth retry despite RetryForever")
	}
	if duration, ok := backoff.GetMaxBackoffDuration(ctx); !ok || duration != 8*time.Second {
		t.Fatalf("backoff returned %s instead of 8 seconds on fourth retry", duration)
	}
	if !backoff.Backoff(ctx) {
		t.Fatalf("backoff refused on fifth retry despite RetryForever")
	}
	if duration, ok := backoff.GetMaxBackoffDuration(ctx); !ok || duration != 8*time.Second {
		t.Fatalf("backoff returned %s instead of 8 seconds on fifth retry", duration)
	}
}
