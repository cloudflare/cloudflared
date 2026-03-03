package telemetry

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

const defaultCapacity = 100

// RingBuffer is a thread-safe ring buffer with overflow policies.
type RingBuffer[T any] struct {
	mu       sync.RWMutex
	items    []T
	head     int
	tail     int
	size     int
	capacity int

	category       ratelimit.Category
	priority       ratelimit.Priority
	overflowPolicy OverflowPolicy

	batchSize     int
	timeout       time.Duration
	lastFlushTime time.Time

	offered   int64
	dropped   int64
	onDropped func(item T, reason string)
}

func NewRingBuffer[T any](category ratelimit.Category, capacity int, overflowPolicy OverflowPolicy, batchSize int, timeout time.Duration) *RingBuffer[T] {
	if capacity <= 0 {
		capacity = defaultCapacity
	}

	if batchSize <= 0 {
		batchSize = 1
	}

	if timeout < 0 {
		timeout = 0
	}

	return &RingBuffer[T]{
		items:          make([]T, capacity),
		capacity:       capacity,
		category:       category,
		priority:       category.GetPriority(),
		overflowPolicy: overflowPolicy,
		batchSize:      batchSize,
		timeout:        timeout,
		lastFlushTime:  time.Now(),
	}
}

func (b *RingBuffer[T]) SetDroppedCallback(callback func(item T, reason string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onDropped = callback
}

func (b *RingBuffer[T]) Offer(item T) bool {
	atomic.AddInt64(&b.offered, 1)

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.size < b.capacity {
		b.items[b.tail] = item
		b.tail = (b.tail + 1) % b.capacity
		b.size++
		return true
	}

	switch b.overflowPolicy {
	case OverflowPolicyDropOldest:
		oldItem := b.items[b.head]
		b.items[b.head] = item
		b.head = (b.head + 1) % b.capacity
		b.tail = (b.tail + 1) % b.capacity

		atomic.AddInt64(&b.dropped, 1)
		if b.onDropped != nil {
			b.onDropped(oldItem, "buffer_full_drop_oldest")
		}
		return true

	case OverflowPolicyDropNewest:
		atomic.AddInt64(&b.dropped, 1)
		if b.onDropped != nil {
			b.onDropped(item, "buffer_full_drop_newest")
		}
		return false

	default:
		atomic.AddInt64(&b.dropped, 1)
		if b.onDropped != nil {
			b.onDropped(item, "unknown_overflow_policy")
		}
		return false
	}
}

func (b *RingBuffer[T]) Poll() (T, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	var zero T
	if b.size == 0 {
		return zero, false
	}

	item := b.items[b.head]
	b.items[b.head] = zero
	b.head = (b.head + 1) % b.capacity
	b.size--

	return item, true
}

func (b *RingBuffer[T]) PollBatch(maxItems int) []T {
	if maxItems <= 0 {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.size == 0 {
		return nil
	}

	itemCount := maxItems
	if itemCount > b.size {
		itemCount = b.size
	}

	result := make([]T, itemCount)
	var zero T

	for i := 0; i < itemCount; i++ {
		result[i] = b.items[b.head]
		b.items[b.head] = zero
		b.head = (b.head + 1) % b.capacity
		b.size--
	}

	return result
}

func (b *RingBuffer[T]) Drain() []T {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.size == 0 {
		return nil
	}

	result := make([]T, b.size)
	index := 0
	var zero T

	for i := 0; i < b.size; i++ {
		pos := (b.head + i) % b.capacity
		result[index] = b.items[pos]
		b.items[pos] = zero
		index++
	}

	b.head = 0
	b.tail = 0
	b.size = 0

	return result
}

func (b *RingBuffer[T]) Peek() (T, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var zero T
	if b.size == 0 {
		return zero, false
	}

	return b.items[b.head], true
}

func (b *RingBuffer[T]) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size
}

func (b *RingBuffer[T]) Capacity() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.capacity
}

func (b *RingBuffer[T]) Category() ratelimit.Category {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.category
}

func (b *RingBuffer[T]) Priority() ratelimit.Priority {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.priority
}

func (b *RingBuffer[T]) IsEmpty() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size == 0
}

func (b *RingBuffer[T]) IsFull() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size == b.capacity
}

func (b *RingBuffer[T]) Utilization() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return float64(b.size) / float64(b.capacity)
}

func (b *RingBuffer[T]) OfferedCount() int64 {
	return atomic.LoadInt64(&b.offered)
}

func (b *RingBuffer[T]) DroppedCount() int64 {
	return atomic.LoadInt64(&b.dropped)
}

func (b *RingBuffer[T]) AcceptedCount() int64 {
	return b.OfferedCount() - b.DroppedCount()
}

func (b *RingBuffer[T]) DropRate() float64 {
	offered := b.OfferedCount()
	if offered == 0 {
		return 0.0
	}
	return float64(b.DroppedCount()) / float64(offered)
}

func (b *RingBuffer[T]) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	var zero T
	for i := 0; i < b.capacity; i++ {
		b.items[i] = zero
	}

	b.head = 0
	b.tail = 0
	b.size = 0
}

func (b *RingBuffer[T]) GetMetrics() BufferMetrics {
	b.mu.RLock()
	size := b.size
	util := float64(b.size) / float64(b.capacity)
	b.mu.RUnlock()

	return BufferMetrics{
		Category:      b.category,
		Priority:      b.priority,
		Capacity:      b.capacity,
		Size:          size,
		Utilization:   util,
		OfferedCount:  b.OfferedCount(),
		DroppedCount:  b.DroppedCount(),
		AcceptedCount: b.AcceptedCount(),
		DropRate:      b.DropRate(),
		LastUpdated:   time.Now(),
	}
}

func (b *RingBuffer[T]) IsReadyToFlush() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.size == 0 {
		return false
	}

	if b.size >= b.batchSize {
		return true
	}

	if b.timeout > 0 && time.Since(b.lastFlushTime) >= b.timeout {
		return true
	}

	return false
}

func (b *RingBuffer[T]) MarkFlushed() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastFlushTime = time.Now()
}

func (b *RingBuffer[T]) PollIfReady() []T {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.size == 0 {
		return nil
	}

	ready := b.size >= b.batchSize ||
		(b.timeout > 0 && time.Since(b.lastFlushTime) >= b.timeout)

	if !ready {
		return nil
	}

	itemCount := b.batchSize
	if itemCount > b.size {
		itemCount = b.size
	}

	result := make([]T, itemCount)
	var zero T

	for i := 0; i < itemCount; i++ {
		result[i] = b.items[b.head]
		b.items[b.head] = zero
		b.head = (b.head + 1) % b.capacity
		b.size--
	}

	b.lastFlushTime = time.Now()
	return result
}

type BufferMetrics struct {
	Category      ratelimit.Category `json:"category"`
	Priority      ratelimit.Priority `json:"priority"`
	Capacity      int                `json:"capacity"`
	Size          int                `json:"size"`
	Utilization   float64            `json:"utilization"`
	OfferedCount  int64              `json:"offered_count"`
	DroppedCount  int64              `json:"dropped_count"`
	AcceptedCount int64              `json:"accepted_count"`
	DropRate      float64            `json:"drop_rate"`
	LastUpdated   time.Time          `json:"last_updated"`
}

// OverflowPolicy defines how the ring buffer handles overflow.
type OverflowPolicy int

const (
	OverflowPolicyDropOldest OverflowPolicy = iota
	OverflowPolicyDropNewest
)

func (op OverflowPolicy) String() string {
	switch op {
	case OverflowPolicyDropOldest:
		return "drop_oldest"
	case OverflowPolicyDropNewest:
		return "drop_newest"
	default:
		return "unknown"
	}
}
