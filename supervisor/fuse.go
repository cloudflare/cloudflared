package supervisor

import "sync"

// booleanFuse is a data structure that can be set once to a particular value using Fuse(value).
// Subsequent calls to Fuse() will have no effect.
type booleanFuse struct {
	value int32
	mu    sync.Mutex
	cond  *sync.Cond
}

func newBooleanFuse() *booleanFuse {
	f := &booleanFuse{}
	f.cond = sync.NewCond(&f.mu)
	return f
}

// Value gets the value
func (f *booleanFuse) Value() bool {
	// 0: unset
	// 1: set true
	// 2: set false
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.value == 1
}

func (f *booleanFuse) Fuse(result bool) {
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
func (f *booleanFuse) Await() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for f.value == 0 {
		f.cond.Wait()
	}
	return f.value == 1
}
