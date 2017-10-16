package h2mux

import (
	"testing"
	"time"
)

func TestReadyList(t *testing.T) {
	rl := NewReadyList()
	c := rl.ReadyChannel()
	// helper functions
	assertEmpty := func() {
		select {
		case <-c:
			t.Fatalf("Spurious wakeup")
		default:
		}
	}
	receiveWithTimeout := func() uint32 {
		select {
		case i := <-c:
			return i
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Timeout")
			return 0
		}
	}
	// no signals, receive should fail
	assertEmpty()
	rl.Signal(0)
	if receiveWithTimeout() != 0 {
		t.Fatalf("Received wrong ID of signalled event")
	}
	// no new signals, receive should fail
	assertEmpty()
	// Signals should not block;
	// Duplicate unhandled signals should not cause multiple wakeups
	signalled := [5]bool{}
	for i := range signalled {
		rl.Signal(uint32(i))
		rl.Signal(uint32(i))
	}
	// All signals should be received once (in any order)
	for range signalled {
		i := receiveWithTimeout()
		if signalled[i] {
			t.Fatalf("Received signal %d more than once", i)
		}
		signalled[i] = true
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
