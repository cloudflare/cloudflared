package flow

import (
	"errors"
	"sync"
)

const (
	unlimitedActiveFlows = 0
)

var (
	ErrTooManyActiveFlows = errors.New("too many active flows")
)

type Limiter interface {
	// Acquire tries to acquire a free slot for a flow, if the value of flows is already above
	// the maximum it returns ErrTooManyActiveFlows.
	Acquire(flowType string) error
	// Release releases a slot for a flow.
	Release()
	// SetLimit allows to hot swap the limit value of the limiter.
	SetLimit(uint64)
}

type flowLimiter struct {
	limiterLock        sync.Mutex
	activeFlowsCounter uint64
	maxActiveFlows     uint64
	unlimited          bool
}

func NewLimiter(maxActiveFlows uint64) Limiter {
	flowLimiter := &flowLimiter{
		maxActiveFlows: maxActiveFlows,
		unlimited:      isUnlimited(maxActiveFlows),
	}

	return flowLimiter
}

func (s *flowLimiter) Acquire(flowType string) error {
	s.limiterLock.Lock()
	defer s.limiterLock.Unlock()

	if !s.unlimited && s.activeFlowsCounter >= s.maxActiveFlows {
		flowRegistrationsDropped.WithLabelValues(flowType).Inc()
		return ErrTooManyActiveFlows
	}

	s.activeFlowsCounter++
	return nil
}

func (s *flowLimiter) Release() {
	s.limiterLock.Lock()
	defer s.limiterLock.Unlock()

	if s.activeFlowsCounter <= 0 {
		return
	}

	s.activeFlowsCounter--
}

func (s *flowLimiter) SetLimit(newMaxActiveFlows uint64) {
	s.limiterLock.Lock()
	defer s.limiterLock.Unlock()

	s.maxActiveFlows = newMaxActiveFlows
	s.unlimited = isUnlimited(newMaxActiveFlows)
}

// isUnlimited checks if the value received matches the configuration for the unlimited flow limiter.
func isUnlimited(value uint64) bool {
	return value == unlimitedActiveFlows
}
