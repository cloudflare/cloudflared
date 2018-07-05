package h2mux

import (
	"sync/atomic"
)

type AtomicCounter struct {
	count uint64
}

func NewAtomicCounter(initCount uint64) *AtomicCounter {
	return &AtomicCounter{count: initCount}
}

func (c *AtomicCounter) IncrementBy(number uint64) {
	atomic.AddUint64(&c.count, number)
}

// Count returns the current value of counter and reset it to 0
func (c *AtomicCounter) Count() uint64 {
	return atomic.SwapUint64(&c.count, 0)
}

// Value returns the current value of counter
func (c *AtomicCounter) Value() uint64 {
	return atomic.LoadUint64(&c.count)
}
