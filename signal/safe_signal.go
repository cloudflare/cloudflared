package signal

import (
	"sync"
)

// Signal lets goroutines signal that some event has occurred. Other goroutines can wait for the signal.
type Signal struct {
	ch   chan struct{}
	once sync.Once
}

// New wraps a channel and turns it into a signal for a one-time event.
func New(ch chan struct{}) *Signal {
	return &Signal{
		ch:   ch,
		once: sync.Once{},
	}
}

// Notify alerts any goroutines waiting on this signal that the event has occurred.
// After the first call to Notify(), future calls are no-op.
func (s *Signal) Notify() {
	s.once.Do(func() {
		close(s.ch)
	})
}

// Wait returns a channel which will be written to when Notify() is called for the first time.
// This channel will never be written to a second time.
func (s *Signal) Wait() <-chan struct{} {
	return s.ch
}
