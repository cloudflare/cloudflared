package retry

import (
	"context"
	"math"
	"time"

	"github.com/cloudflare/backoff"
)

const (
	DefaultBaseTime           time.Duration = time.Second
	initialBackoffExponent                  = 1
	gracePeriodExponentOffset               = 2
	// int64OverflowExponent is the first exponent that shifts into int64's sign bit.
	int64OverflowExponent = 63
)

// Redeclare time functions so they can be overridden in tests.
type Clock struct {
	Now   func() time.Time
	After func(d time.Duration) <-chan time.Time
}

// BackoffHandler manages exponential backoff and limits the maximum number of retries.
// The base time period is 1 second, doubling with each retry.
// After initial success, a grace period can be set to reset the backoff timer if
// a connection is maintained successfully for a long enough period. The base grace period
// is 2 seconds, doubling with each retry.
type BackoffHandler struct {
	// MaxRetries sets the maximum number of retries to perform. The default value
	// of 0 disables retry completely.
	maxRetries uint
	// RetryForever caps the exponential backoff period according to MaxRetries
	// but allows you to retry indefinitely.
	retryForever bool
	// BaseTime sets the initial backoff period.
	baseTime time.Duration

	retries       uint
	resetDeadline time.Time
	strategy      *backoff.Backoff

	Clock Clock
}

func NewBackoff(maxRetries uint, baseTime time.Duration, retryForever bool) BackoffHandler {
	return BackoffHandler{
		maxRetries:   maxRetries,
		baseTime:     baseTime,
		retryForever: retryForever,
		Clock:        Clock{Now: time.Now, After: time.After},
	}
}

func (b BackoffHandler) GetMaxBackoffDuration(ctx context.Context) (time.Duration, bool) {
	// Follows the same logic as Backoff, but without mutating the receiver.
	// This select has to happen first to reflect the actual behaviour of the Backoff function.
	select {
	case <-ctx.Done():
		return time.Duration(0), false
	default:
	}

	retries := b.retries
	if !b.resetDeadline.IsZero() && b.Clock.Now().After(b.resetDeadline) {
		retries = 0
	}
	if retries >= b.maxRetries && !b.retryForever {
		return time.Duration(0), false
	}
	if retries < b.maxRetries {
		retries++
	}

	return exponentialBackoffDuration(b.GetBaseTime(), retries), true
}

// BackoffTimer returns a channel that sends the current time when the exponential backoff timeout expires.
// Returns nil if the maximum number of retries have been used.
func (b *BackoffHandler) BackoffTimer() <-chan time.Time {
	if !b.resetDeadline.IsZero() && b.Clock.Now().After(b.resetDeadline) {
		b.reset()
	}
	if b.retries >= b.maxRetries {
		if !b.retryForever {
			return nil
		}
	} else {
		b.retries++
	}

	return b.Clock.After(b.backoffStrategy().Duration())
}

// Backoff is used to wait according to exponential backoff. Returns false if the
// maximum number of retries have been used or if the underlying context has been cancelled.
func (b *BackoffHandler) Backoff(ctx context.Context) bool {
	c := b.BackoffTimer()
	if c == nil {
		return false
	}
	select {
	case <-c:
		return true
	case <-ctx.Done():
		return false
	}
}

// Sets a grace period within which the backoff timer is maintained. After the grace
// period expires, the number of retries & backoff duration is reset.
func (b *BackoffHandler) SetGracePeriod() time.Duration {
	gracePeriodExponent := b.retries
	if gracePeriodExponent < int64OverflowExponent {
		gracePeriodExponent += gracePeriodExponentOffset
	}
	maxTimeToWait := exponentialBackoffDuration(b.GetBaseTime(), gracePeriodExponent)
	timeToWait := backoff.New(maxTimeToWait, maxTimeToWait).Duration()
	b.resetDeadline = b.Clock.Now().Add(timeToWait)

	return timeToWait
}

func (b BackoffHandler) GetBaseTime() time.Duration {
	if b.baseTime == 0 {
		return DefaultBaseTime
	}
	return b.baseTime
}

// Retries returns the number of retries consumed so far.
func (b *BackoffHandler) Retries() int {
	return int(b.retries) // #nosec G115
}

func (b *BackoffHandler) ReachedMaxRetries() bool {
	return b.retries == b.maxRetries
}

func (b *BackoffHandler) ResetNow() {
	b.reset()
	b.resetDeadline = b.Clock.Now()
}

func (b *BackoffHandler) backoffStrategy() *backoff.Backoff {
	if b.strategy == nil {
		b.strategy = backoff.New(
			exponentialBackoffDuration(b.GetBaseTime(), b.maxRetries),
			exponentialBackoffDuration(b.GetBaseTime(), initialBackoffExponent),
		)
	}
	return b.strategy
}

func (b *BackoffHandler) reset() {
	b.retries = 0
	b.resetDeadline = time.Time{}
	if b.strategy != nil {
		b.strategy.Reset()
	}
}

func exponentialBackoffDuration(baseTime time.Duration, retries uint) time.Duration {
	if baseTime <= 0 {
		baseTime = DefaultBaseTime
	}
	if retries >= int64OverflowExponent {
		return time.Duration(math.MaxInt64)
	}

	multiplier := int64(1) << retries
	if int64(baseTime) > math.MaxInt64/multiplier {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(int64(baseTime) * multiplier)
}
