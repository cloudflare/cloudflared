package h2mux

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func assertEmpty(t *testing.T, rl *ReadyList) {
	select {
	case <-rl.ReadyChannel():
		t.Fatal("Spurious wakeup")
	default:
	}
}

func assertClosed(t *testing.T, rl *ReadyList) {
	select {
	case _, ok := <-rl.ReadyChannel():
		assert.False(t, ok, "ReadyChannel was not closed")
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("Timeout")
	}
}

func receiveWithTimeout(t *testing.T, rl *ReadyList) uint32 {
	select {
	case i := <-rl.ReadyChannel():
		return i
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("Timeout")
		return 0
	}
}

func TestReadyListEmpty(t *testing.T) {
	rl := NewReadyList()

	// no signals, receive should fail
	assertEmpty(t, rl)
}
func TestReadyListSignal(t *testing.T) {
	rl := NewReadyList()
	assertEmpty(t, rl)

	rl.Signal(0)
	if receiveWithTimeout(t, rl) != 0 {
		t.Fatalf("Received wrong ID of signalled event")
	}

	assertEmpty(t, rl)
}

func TestReadyListMultipleSignals(t *testing.T) {
	rl := NewReadyList()
	assertEmpty(t, rl)

	// Signals should not block;
	// Duplicate unhandled signals should not cause multiple wakeups
	signalled := [5]bool{}
	for i := range signalled {
		rl.Signal(uint32(i))
		rl.Signal(uint32(i))
	}
	// All signals should be received once (in any order)
	for range signalled {
		i := receiveWithTimeout(t, rl)
		if signalled[i] {
			t.Fatalf("Received signal %d more than once", i)
		}
		signalled[i] = true
	}
	for i := range signalled {
		if !signalled[i] {
			t.Fatalf("Never received signal %d", i)
		}
	}
	assertEmpty(t, rl)
}

func TestReadyListClose(t *testing.T) {
	rl := NewReadyList()
	rl.Close()

	// readyList.run() occurs in a separate goroutine,
	// so there's no way to directly check that run() has terminated.
	// Perform an indirect check: is the ready channel closed?
	assertClosed(t, rl)

	// a second rl.Close() shouldn't cause a panic
	rl.Close()

	// Signal shouldn't block after Close()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 5; i++ {
			rl.Signal(uint32(i))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Test timed out")
	}
}

func TestReadyDescriptorQueue(t *testing.T) {
	var queue readyDescriptorQueue
	items := [4]readyDescriptor{}
	for i := range items {
		items[i].ID = uint32(i)
	}

	if !queue.Empty() {
		t.Fatalf("nil queue should be empty")
	}
	queue.Enqueue(&items[3])
	queue.Enqueue(&items[1])
	queue.Enqueue(&items[0])
	queue.Enqueue(&items[2])
	if queue.Empty() {
		t.Fatalf("Empty should be false after enqueue")
	}
	i := queue.Dequeue().ID
	if i != 3 {
		t.Fatalf("item 3 should have been dequeued, got %d instead", i)
	}
	i = queue.Dequeue().ID
	if i != 1 {
		t.Fatalf("item 1 should have been dequeued, got %d instead", i)
	}
	i = queue.Dequeue().ID
	if i != 0 {
		t.Fatalf("item 0 should have been dequeued, got %d instead", i)
	}
	i = queue.Dequeue().ID
	if i != 2 {
		t.Fatalf("item 2 should have been dequeued, got %d instead", i)
	}
	if !queue.Empty() {
		t.Fatal("queue should be empty after dequeuing all items")
	}
	if queue.Dequeue() != nil {
		t.Fatal("dequeue on empty queue should return nil")
	}
}

func TestReadyDescriptorMap(t *testing.T) {
	m := newReadyDescriptorMap()
	m.Delete(42)
	// (delete of missing key should be a noop)
	x := m.SetIfMissing(42)
	if x == nil {
		t.Fatal("SetIfMissing for new key returned nil")
	}
	if m.SetIfMissing(42) != nil {
		t.Fatal("SetIfMissing for existing key returned non-nil")
	}
	// this delete has effect
	m.Delete(42)
	// the next set should reuse the old object
	y := m.SetIfMissing(666)
	if y == nil {
		t.Fatal("SetIfMissing for new key returned nil")
	}
	if x != y {
		t.Fatal("SetIfMissing didn't reuse freed object")
	}
}
