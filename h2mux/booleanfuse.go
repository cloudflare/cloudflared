package h2mux

import "sync/atomic"

// BooleanFuse is a data structure that can be set once to a particular value using Fuse(value).
// Subsequent calls to Fuse() will have no effect.
type BooleanFuse struct {
	value int32
}

// Value gets the value
func (f *BooleanFuse) Value() bool {
	// 0: unset
	// 1: set true
	// 2: set false
	return atomic.LoadInt32(&f.value) == 1
}

func (f *BooleanFuse) Fuse(result bool) {
	newValue := int32(2)
	if result {
		newValue = 1
	}
	atomic.CompareAndSwapInt32(&f.value, 0, newValue)
}
