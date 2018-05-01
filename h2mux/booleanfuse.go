package h2mux

import "sync"

// BooleanFuse is a data structure that can be set once to a particular value using Fuse(value).
// Subsequent calls to Fuse() will have no effect.
type BooleanFuse struct {
	value int32
	mu    sync.Mutex
	cond  *sync.Cond
}

func NewBooleanFuse() *BooleanFuse {
	f := &BooleanFuse{}
	f.cond = sync.NewCond(&f.mu)
	return f
}

// Value gets the value
func (f *BooleanFuse) Value() bool {
	// 0: unset
	// 1: set true
	// 2: set false
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.value == 1
}

func (f *BooleanFuse) Fuse(result bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	newValue := int32(2)
	if result {
		newValue = 1
	}
	if f.value == 0 {
		f.value = newValue
		f.cond.Broadcast()
	}
}

// Await blocks until Fuse has been called at least once.
func (f *BooleanFuse) Await() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for f.value == 0 {
		f.cond.Wait()
	}
	return f.value == 1
}
