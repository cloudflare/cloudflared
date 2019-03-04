package signal

import (
	"testing"
)

func TestMultiNotifyDoesntCrash(t *testing.T) {
	sig := New(make(chan struct{}))
	sig.Notify()
	sig.Notify()
	// If code has reached here without crashing, the test has passed.
}

func TestWait(t *testing.T) {
	sig := New(make(chan struct{}))
	sig.Notify()
	select {
	case <-sig.Wait():
		// Test succeeds
		return
	default:
		// sig.Wait() should have been read from, because sig.Notify() wrote to it.
		t.Fail()
	}
}
