package h2mux

// Signal describes an event that can be waited on for at least one signal.
// Signalling the event while it is in the signalled state is a noop.
// When the waiter wakes up, the signal is set to unsignalled.
// It is a way for any number of writers to inform a reader (without blocking)
// that an event has happened.
type Signal struct {
	c chan struct{}
}

// NewSignal creates a new Signal.
func NewSignal() Signal {
	return Signal{c: make(chan struct{}, 1)}
}

// Signal signals the event.
func (s Signal) Signal() {
	// This channel is buffered, so the nonblocking send will always succeed if the buffer is empty.
	select {
	case s.c <- struct{}{}:
	default:
	}
}

// Wait for the event to be signalled.
func (s Signal) Wait() {
	<-s.c
}

// WaitChannel returns a channel that is readable after Signal is called.
func (s Signal) WaitChannel() <-chan struct{} {
	return s.c
}
