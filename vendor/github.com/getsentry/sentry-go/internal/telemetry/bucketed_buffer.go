package telemetry

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

const (
	defaultBucketedCapacity = 100
	perBucketItemLimit      = 100
)

type Bucket[T any] struct {
	traceID       string
	items         []T
	createdAt     time.Time
	lastUpdatedAt time.Time
}

// BucketedBuffer groups items by trace id, flushing per bucket.
type BucketedBuffer[T any] struct {
	mu sync.RWMutex

	buckets    []*Bucket[T]
	traceIndex map[string]int

	head int
	tail int

	itemCapacity   int
	bucketCapacity int

	totalItems  int
	bucketCount int

	category       ratelimit.Category
	priority       ratelimit.Priority
	overflowPolicy OverflowPolicy
	batchSize      int
	timeout        time.Duration
	lastFlushTime  time.Time

	offered   int64
	dropped   int64
	onDropped func(item T, reason string)
}

func NewBucketedBuffer[T any](
	category ratelimit.Category,
	capacity int,
	overflowPolicy OverflowPolicy,
	batchSize int,
	timeout time.Duration,
) *BucketedBuffer[T] {
	if capacity <= 0 {
		capacity = defaultBucketedCapacity
	}
	if batchSize <= 0 {
		batchSize = 1
	}
	if timeout < 0 {
		timeout = 0
	}

	bucketCapacity := capacity / 10
	if bucketCapacity < 10 {
		bucketCapacity = 10
	}

	return &BucketedBuffer[T]{
		buckets:        make([]*Bucket[T], bucketCapacity),
		traceIndex:     make(map[string]int),
		itemCapacity:   capacity,
		bucketCapacity: bucketCapacity,
		category:       category,
		priority:       category.GetPriority(),
		overflowPolicy: overflowPolicy,
		batchSize:      batchSize,
		timeout:        timeout,
		lastFlushTime:  time.Now(),
	}
}

func (b *BucketedBuffer[T]) Offer(item T) bool {
	atomic.AddInt64(&b.offered, 1)

	traceID := ""
	if ta, ok := any(item).(TraceAware); ok {
		if tid, hasTrace := ta.GetTraceID(); hasTrace {
			traceID = tid
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	return b.offerToBucket(item, traceID)
}

func (b *BucketedBuffer[T]) offerToBucket(item T, traceID string) bool {
	if traceID != "" {
		if idx, exists := b.traceIndex[traceID]; exists {
			bucket := b.buckets[idx]
			if len(bucket.items) >= perBucketItemLimit {
				delete(b.traceIndex, traceID)
			} else {
				bucket.items = append(bucket.items, item)
				bucket.lastUpdatedAt = time.Now()
				b.totalItems++
				return true
			}
		}
	}

	if b.totalItems >= b.itemCapacity {
		return b.handleOverflow(item, traceID)
	}
	if b.bucketCount >= b.bucketCapacity {
		return b.handleOverflow(item, traceID)
	}

	bucket := &Bucket[T]{
		traceID:       traceID,
		items:         []T{item},
		createdAt:     time.Now(),
		lastUpdatedAt: time.Now(),
	}
	b.buckets[b.tail] = bucket
	if traceID != "" {
		b.traceIndex[traceID] = b.tail
	}
	b.tail = (b.tail + 1) % b.bucketCapacity
	b.bucketCount++
	b.totalItems++
	return true
}

func (b *BucketedBuffer[T]) handleOverflow(item T, traceID string) bool {
	switch b.overflowPolicy {
	case OverflowPolicyDropOldest:
		oldestBucket := b.buckets[b.head]
		if oldestBucket == nil {
			atomic.AddInt64(&b.dropped, 1)
			if b.onDropped != nil {
				b.onDropped(item, "buffer_full_invalid_state")
			}
			return false
		}
		if oldestBucket.traceID != "" {
			delete(b.traceIndex, oldestBucket.traceID)
		}
		droppedCount := len(oldestBucket.items)
		atomic.AddInt64(&b.dropped, int64(droppedCount))
		if b.onDropped != nil {
			for _, di := range oldestBucket.items {
				b.onDropped(di, "buffer_full_drop_oldest_bucket")
			}
		}
		b.totalItems -= droppedCount
		b.bucketCount--
		b.head = (b.head + 1) % b.bucketCapacity
		// add new bucket
		bucket := &Bucket[T]{traceID: traceID, items: []T{item}, createdAt: time.Now(), lastUpdatedAt: time.Now()}
		b.buckets[b.tail] = bucket
		if traceID != "" {
			b.traceIndex[traceID] = b.tail
		}
		b.tail = (b.tail + 1) % b.bucketCapacity
		b.bucketCount++
		b.totalItems++
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

func (b *BucketedBuffer[T]) Poll() (T, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var zero T
	if b.bucketCount == 0 {
		return zero, false
	}
	bucket := b.buckets[b.head]
	if bucket == nil || len(bucket.items) == 0 {
		return zero, false
	}
	item := bucket.items[0]
	bucket.items = bucket.items[1:]
	b.totalItems--
	if len(bucket.items) == 0 {
		if bucket.traceID != "" {
			delete(b.traceIndex, bucket.traceID)
		}
		b.buckets[b.head] = nil
		b.head = (b.head + 1) % b.bucketCapacity
		b.bucketCount--
	}
	return item, true
}

func (b *BucketedBuffer[T]) PollBatch(maxItems int) []T {
	if maxItems <= 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bucketCount == 0 {
		return nil
	}
	res := make([]T, 0, maxItems)
	for len(res) < maxItems && b.bucketCount > 0 {
		bucket := b.buckets[b.head]
		if bucket == nil {
			break
		}
		n := maxItems - len(res)
		if n > len(bucket.items) {
			n = len(bucket.items)
		}
		res = append(res, bucket.items[:n]...)
		bucket.items = bucket.items[n:]
		b.totalItems -= n
		if len(bucket.items) == 0 {
			if bucket.traceID != "" {
				delete(b.traceIndex, bucket.traceID)
			}
			b.buckets[b.head] = nil
			b.head = (b.head + 1) % b.bucketCapacity
			b.bucketCount--
		}
	}
	return res
}

func (b *BucketedBuffer[T]) PollIfReady() []T {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bucketCount == 0 {
		return nil
	}
	ready := b.totalItems >= b.batchSize || (b.timeout > 0 && time.Since(b.lastFlushTime) >= b.timeout)
	if !ready {
		return nil
	}
	oldest := b.buckets[b.head]
	if oldest == nil {
		return nil
	}
	items := oldest.items
	if oldest.traceID != "" {
		delete(b.traceIndex, oldest.traceID)
	}
	b.buckets[b.head] = nil
	b.head = (b.head + 1) % b.bucketCapacity
	b.totalItems -= len(items)
	b.bucketCount--
	b.lastFlushTime = time.Now()
	return items
}

func (b *BucketedBuffer[T]) Drain() []T {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bucketCount == 0 {
		return nil
	}
	res := make([]T, 0, b.totalItems)
	for i := 0; i < b.bucketCount; i++ {
		idx := (b.head + i) % b.bucketCapacity
		bucket := b.buckets[idx]
		if bucket != nil {
			res = append(res, bucket.items...)
			b.buckets[idx] = nil
		}
	}
	b.traceIndex = make(map[string]int)
	b.head = 0
	b.tail = 0
	b.totalItems = 0
	b.bucketCount = 0
	return res
}

func (b *BucketedBuffer[T]) Peek() (T, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var zero T
	if b.bucketCount == 0 {
		return zero, false
	}
	bucket := b.buckets[b.head]
	if bucket == nil || len(bucket.items) == 0 {
		return zero, false
	}
	return bucket.items[0], true
}

func (b *BucketedBuffer[T]) Size() int     { b.mu.RLock(); defer b.mu.RUnlock(); return b.totalItems }
func (b *BucketedBuffer[T]) Capacity() int { b.mu.RLock(); defer b.mu.RUnlock(); return b.itemCapacity }
func (b *BucketedBuffer[T]) Category() ratelimit.Category {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.category
}
func (b *BucketedBuffer[T]) Priority() ratelimit.Priority {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.priority
}
func (b *BucketedBuffer[T]) IsEmpty() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.bucketCount == 0
}
func (b *BucketedBuffer[T]) IsFull() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.totalItems >= b.itemCapacity
}
func (b *BucketedBuffer[T]) Utilization() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.itemCapacity == 0 {
		return 0
	}
	return float64(b.totalItems) / float64(b.itemCapacity)
}
func (b *BucketedBuffer[T]) OfferedCount() int64  { return atomic.LoadInt64(&b.offered) }
func (b *BucketedBuffer[T]) DroppedCount() int64  { return atomic.LoadInt64(&b.dropped) }
func (b *BucketedBuffer[T]) AcceptedCount() int64 { return b.OfferedCount() - b.DroppedCount() }
func (b *BucketedBuffer[T]) DropRate() float64 {
	off := b.OfferedCount()
	if off == 0 {
		return 0
	}
	return float64(b.DroppedCount()) / float64(off)
}

func (b *BucketedBuffer[T]) GetMetrics() BufferMetrics {
	b.mu.RLock()
	size := b.totalItems
	util := 0.0
	if b.itemCapacity > 0 {
		util = float64(b.totalItems) / float64(b.itemCapacity)
	}
	b.mu.RUnlock()
	return BufferMetrics{Category: b.category, Priority: b.priority, Capacity: b.itemCapacity, Size: size, Utilization: util, OfferedCount: b.OfferedCount(), DroppedCount: b.DroppedCount(), AcceptedCount: b.AcceptedCount(), DropRate: b.DropRate(), LastUpdated: time.Now()}
}

func (b *BucketedBuffer[T]) SetDroppedCallback(callback func(item T, reason string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onDropped = callback
}
func (b *BucketedBuffer[T]) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := 0; i < b.bucketCapacity; i++ {
		b.buckets[i] = nil
	}
	b.traceIndex = make(map[string]int)
	b.head = 0
	b.tail = 0
	b.totalItems = 0
	b.bucketCount = 0
}
func (b *BucketedBuffer[T]) IsReadyToFlush() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.bucketCount == 0 {
		return false
	}
	if b.totalItems >= b.batchSize {
		return true
	}
	if b.timeout > 0 && time.Since(b.lastFlushTime) >= b.timeout {
		return true
	}
	return false
}
func (b *BucketedBuffer[T]) MarkFlushed() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastFlushTime = time.Now()
}
