package origin

import (
	"time"

	"golang.org/x/net/context"
)

// Redeclare time functions so they can be overridden in tests.
var (
	timeNow   = time.Now
	timeAfter = time.After
)

// BackoffHandler manages exponential backoff and limits the maximum number of retries.
// The base time period is 1 second, doubling with each retry.
// After initial success, a grace period can be set to reset the backoff timer if
// a connection is maintained successfully for a long enough period. The base grace period
// is 2 seconds, doubling with each retry.
type BackoffHandler struct {
	// MaxRetries sets the maximum number of retries to perform. The default value
	// of 0 disables retry completely.
	MaxRetries uint

	retries       uint
	resetDeadline time.Time
}

func (b BackoffHandler) GetBackoffDuration(ctx context.Context) (time.Duration, bool) {
	// Follows the same logic as Backoff, but without mutating the receiver.
	// This select has to happen first to reflect the actual behaviour of the Backoff function.
	select {
	case <-ctx.Done():
		return time.Duration(0), false
	default:
	}
	if !b.resetDeadline.IsZero() && timeNow().After(b.resetDeadline) {
		// b.retries would be set to 0 at this point
		return time.Second, true
	}
	if b.retries >= b.MaxRetries {
		return time.Duration(0), false
	}
	return time.Duration(time.Second * 1 << b.retries), true
}

// Backoff is used to wait according to exponential backoff. Returns false if the
// maximum number of retries have been used or if the underlying context has been cancelled.
func (b *BackoffHandler) Backoff(ctx context.Context) bool {
	if !b.resetDeadline.IsZero() && timeNow().After(b.resetDeadline) {
		b.retries = 0
		b.resetDeadline = time.Time{}
	}
	if b.retries >= b.MaxRetries {
		return false
	}
	select {
	case <-timeAfter(time.Duration(time.Second * 1 << b.retries)):
		b.retries++
		return true
	case <-ctx.Done():
		return false
	}
}

// Sets a grace period within which the the backoff timer is maintained. After the grace
// period expires, the number of retries & backoff duration is reset.
func (b *BackoffHandler) SetGracePeriod() {
	b.resetDeadline = timeNow().Add(time.Duration(time.Second * 2 << b.retries))
}
