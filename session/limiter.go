package session

import (
	"errors"
	"sync"
)

const (
	unlimitedActiveSessions = 0
)

var (
	ErrTooManyActiveSessions = errors.New("too many active sessions")
)

type Limiter interface {
	// Acquire tries to acquire a free slot for a session, if the value of sessions is already above
	// the maximum it returns ErrTooManyActiveSessions.
	Acquire(sessionType string) error
	// Release releases a slot for a session.
	Release()
	// SetLimit allows to hot swap the limit value of the limiter.
	SetLimit(uint64)
}

type sessionLimiter struct {
	limiterLock           sync.Mutex
	activeSessionsCounter uint64
	maxActiveSessions     uint64
	unlimited             bool
}

func NewLimiter(maxActiveSessions uint64) Limiter {
	sessionLimiter := &sessionLimiter{
		maxActiveSessions: maxActiveSessions,
		unlimited:         isUnlimited(maxActiveSessions),
	}

	return sessionLimiter
}

func (s *sessionLimiter) Acquire(sessionType string) error {
	s.limiterLock.Lock()
	defer s.limiterLock.Unlock()

	if !s.unlimited && s.activeSessionsCounter >= s.maxActiveSessions {
		sessionRegistrationsDropped.WithLabelValues(sessionType).Inc()
		return ErrTooManyActiveSessions
	}

	s.activeSessionsCounter++
	return nil
}

func (s *sessionLimiter) Release() {
	s.limiterLock.Lock()
	defer s.limiterLock.Unlock()

	if s.activeSessionsCounter <= 0 {
		return
	}

	s.activeSessionsCounter--
}

func (s *sessionLimiter) SetLimit(newMaxActiveSessions uint64) {
	s.limiterLock.Lock()
	defer s.limiterLock.Unlock()

	s.maxActiveSessions = newMaxActiveSessions
	s.unlimited = isUnlimited(newMaxActiveSessions)
}

// isUnlimited checks if the value received matches the configuration for the unlimited session limiter.
func isUnlimited(value uint64) bool {
	return value == unlimitedActiveSessions
}
