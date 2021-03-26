package retry

import (
	"context"
	"math/rand"
	"time"
)

// Redeclare time functions so they can be overridden in tests.
type clock struct {
	Now   func() time.Time
	After func(d time.Duration) <-chan time.Time
}

var Clock = clock{
	Now:   time.Now,
	After: time.After,
}

// BackoffHandler manages exponential backoff and limits the maximum number of retries.
// The base time period is 1 second, doubling with each retry.
// After initial success, a grace period can be set to reset the backoff timer if
// a connection is maintained successfully for a long enough period. The base grace period
// is 2 seconds, doubling with each retry.
type BackoffHandler struct {
	// MaxRetries sets the maximum number of retries to perform. The default value
	// of 0 disables retry completely.
	MaxRetries uint
	// RetryForever caps the exponential backoff period according to MaxRetries
	// but allows you to retry indefinitely.
	RetryForever bool
	// BaseTime sets the initial backoff period.
	BaseTime time.Duration

	retries       uint
	resetDeadline time.Time
}

func (b BackoffHandler) GetMaxBackoffDuration(ctx context.Context) (time.Duration, bool) {
	// Follows the same logic as Backoff, but without mutating the receiver.
	// This select has to happen first to reflect the actual behaviour of the Backoff function.
	select {
	case <-ctx.Done():
		return time.Duration(0), false
	default:
	}
	if !b.resetDeadline.IsZero() && Clock.Now().After(b.resetDeadline) {
		// b.retries would be set to 0 at this point
		return time.Second, true
	}
	if b.retries >= b.MaxRetries && !b.RetryForever {
		return time.Duration(0), false
	}
	maxTimeToWait := b.GetBaseTime() * 1 << (b.retries + 1)
	return maxTimeToWait, true
}

// BackoffTimer returns a channel that sends the current time when the exponential backoff timeout expires.
// Returns nil if the maximum number of retries have been used.
func (b *BackoffHandler) BackoffTimer() <-chan time.Time {
	if !b.resetDeadline.IsZero() && Clock.Now().After(b.resetDeadline) {
		b.retries = 0
		b.resetDeadline = time.Time{}
	}
	if b.retries >= b.MaxRetries {
		if !b.RetryForever {
			return nil
		}
	} else {
		b.retries++
	}
	maxTimeToWait := time.Duration(b.GetBaseTime() * 1 << (b.retries))
	timeToWait := time.Duration(rand.Int63n(maxTimeToWait.Nanoseconds()))
	return Clock.After(timeToWait)
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

// Sets a grace period within which the the backoff timer is maintained. After the grace
// period expires, the number of retries & backoff duration is reset.
func (b *BackoffHandler) SetGracePeriod() {
	maxTimeToWait := b.GetBaseTime() * 2 << (b.retries + 1)
	timeToWait := time.Duration(rand.Int63n(maxTimeToWait.Nanoseconds()))
	b.resetDeadline = Clock.Now().Add(timeToWait)
}

func (b BackoffHandler) GetBaseTime() time.Duration {
	if b.BaseTime == 0 {
		return time.Second
	}
	return b.BaseTime
}

// Retries returns the number of retries consumed so far.
func (b *BackoffHandler) Retries() int {
	return int(b.retries)
}

func (b *BackoffHandler) ReachedMaxRetries() bool {
	return b.retries == b.MaxRetries
}

func (b *BackoffHandler) ResetNow() {
	b.resetDeadline = time.Now()
}
