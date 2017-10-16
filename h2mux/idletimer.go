package h2mux

import (
	"math/rand"
	"sync"
	"time"
)

// IdleTimer is a type of Timer designed for managing heartbeats on an idle connection.
// The timer ticks on an interval with added jitter to avoid accidental synchronisation
// between two endpoints. It tracks the number of retries/ticks since the connection was
// last marked active.
//
// The methods of IdleTimer must not be called while a goroutine is reading from C.
type IdleTimer struct {
	// The channel on which ticks are delivered.
	C <-chan time.Time

	// A timer used to measure idle connection time. Reset after sending data.
	idleTimer *time.Timer
	// The maximum length of time a connection is idle before sending a ping.
	idleDuration time.Duration
	// A pseudorandom source used to add jitter to the idle duration.
	randomSource *rand.Rand
	// The maximum number of retries allowed.
	maxRetries uint64
	// The number of retries since the connection was last marked active.
	retries uint64
	// A lock to prevent race condition while checking retries
	stateLock sync.RWMutex
}

func NewIdleTimer(idleDuration time.Duration, maxRetries uint64) *IdleTimer {
	t := &IdleTimer{
		idleTimer:    time.NewTimer(idleDuration),
		idleDuration: idleDuration,
		randomSource: rand.New(rand.NewSource(time.Now().Unix())),
		maxRetries:   maxRetries,
	}
	t.C = t.idleTimer.C
	return t
}

// Retry should be called when retrying the idle timeout. If the maximum number of retries
// has been met, returns false.
// After calling this function and sending a heartbeat, call ResetTimer. Since sending the
// heartbeat could be a blocking operation, we resetting the timer after the write completes
// to avoid it expiring during the write.
func (t *IdleTimer) Retry() bool {
	t.stateLock.Lock()
	defer t.stateLock.Unlock()
	if t.retries >= t.maxRetries {
		return false
	}
	t.retries++
	return true
}

func (t *IdleTimer) RetryCount() uint64 {
	t.stateLock.RLock()
	defer t.stateLock.RUnlock()
	return t.retries
}

// MarkActive resets the idle connection timer and suppresses any outstanding idle events.
func (t *IdleTimer) MarkActive() {
	if !t.idleTimer.Stop() {
		// eat the timer event to prevent spurious pings
		<-t.idleTimer.C
	}
	t.stateLock.Lock()
	t.retries = 0
	t.stateLock.Unlock()
	t.ResetTimer()
}

// Reset the idle timer according to the configured duration, with some added jitter.
func (t *IdleTimer) ResetTimer() {
	jitter := time.Duration(t.randomSource.Int63n(int64(t.idleDuration)))
	t.idleTimer.Reset(t.idleDuration + jitter)
}
