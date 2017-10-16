package h2mux

import (
	"sync"

	"golang.org/x/net/http2"
)

// StreamErrorMap is used to track stream errors. This is a separate structure to ActiveStreamMap because
// errors can be raised against non-existent or closed streams.
type StreamErrorMap struct {
	sync.RWMutex
	// errors tracks per-stream errors
	errors map[uint32]http2.ErrCode
	// hasError is signaled whenever an error is raised.
	hasError Signal
}

// NewStreamErrorMap creates a new StreamErrorMap.
func NewStreamErrorMap() *StreamErrorMap {
	return &StreamErrorMap{
		errors:   make(map[uint32]http2.ErrCode),
		hasError: NewSignal(),
	}
}

// RaiseError raises a stream error.
func (s *StreamErrorMap) RaiseError(streamID uint32, err http2.ErrCode) {
	s.Lock()
	s.errors[streamID] = err
	s.Unlock()
	s.hasError.Signal()
}

// GetSignalChan returns a channel that is signalled when an error is raised.
func (s *StreamErrorMap) GetSignalChan() <-chan struct{} {
	return s.hasError.WaitChannel()
}

// GetErrors retrieves all errors currently raised. This resets the currently-tracked errors.
func (s *StreamErrorMap) GetErrors() map[uint32]http2.ErrCode {
	s.Lock()
	errors := s.errors
	s.errors = make(map[uint32]http2.ErrCode)
	s.Unlock()
	return errors
}
