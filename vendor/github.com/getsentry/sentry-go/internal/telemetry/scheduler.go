package telemetry

import (
	"context"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
)

// Scheduler implements a weighted round-robin scheduler for processing buffered events.
type Scheduler struct {
	buffers   map[ratelimit.Category]Buffer[protocol.TelemetryItem]
	transport protocol.TelemetryTransport
	dsn       *protocol.Dsn
	sdkInfo   *protocol.SdkInfo

	currentCycle []ratelimit.Priority
	cyclePos     int

	ctx          context.Context
	cancel       context.CancelFunc
	processingWg sync.WaitGroup

	mu         sync.Mutex
	cond       *sync.Cond
	startOnce  sync.Once
	finishOnce sync.Once
}

func NewScheduler(
	buffers map[ratelimit.Category]Buffer[protocol.TelemetryItem],
	transport protocol.TelemetryTransport,
	dsn *protocol.Dsn,
	sdkInfo *protocol.SdkInfo,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	priorityWeights := map[ratelimit.Priority]int{
		ratelimit.PriorityCritical: 5,
		ratelimit.PriorityHigh:     4,
		ratelimit.PriorityMedium:   3,
		ratelimit.PriorityLow:      2,
		ratelimit.PriorityLowest:   1,
	}

	var currentCycle []ratelimit.Priority
	for priority, weight := range priorityWeights {
		hasBuffers := false
		for _, buffer := range buffers {
			if buffer.Priority() == priority {
				hasBuffers = true
				break
			}
		}

		if hasBuffers {
			for i := 0; i < weight; i++ {
				currentCycle = append(currentCycle, priority)
			}
		}
	}

	s := &Scheduler{
		buffers:      buffers,
		transport:    transport,
		dsn:          dsn,
		sdkInfo:      sdkInfo,
		currentCycle: currentCycle,
		ctx:          ctx,
		cancel:       cancel,
	}
	s.cond = sync.NewCond(&s.mu)

	return s
}

func (s *Scheduler) Start() {
	s.startOnce.Do(func() {
		s.processingWg.Add(1)
		go s.run()
	})
}

func (s *Scheduler) Stop(timeout time.Duration) {
	s.finishOnce.Do(func() {
		s.Flush(timeout)

		s.cancel()
		s.cond.Broadcast()

		done := make(chan struct{})
		go func() {
			defer close(done)
			s.processingWg.Wait()
		}()

		select {
		case <-done:
		case <-time.After(timeout):
			debuglog.Printf("scheduler stop timed out after %v", timeout)
		}
	})
}

func (s *Scheduler) Signal() {
	s.cond.Signal()
}

func (s *Scheduler) Add(item protocol.TelemetryItem) bool {
	category := item.GetCategory()
	buffer, exists := s.buffers[category]
	if !exists {
		return false
	}

	accepted := buffer.Offer(item)
	if accepted {
		s.Signal()
	}
	return accepted
}

func (s *Scheduler) Flush(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.FlushWithContext(ctx)
}

func (s *Scheduler) FlushWithContext(ctx context.Context) bool {
	s.flushBuffers()
	return s.transport.FlushWithContext(ctx)
}

func (s *Scheduler) run() {
	defer s.processingWg.Done()

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.cond.Broadcast()
			case <-s.ctx.Done():
				return
			}
		}
	}()

	for {
		s.mu.Lock()

		for !s.hasWork() && s.ctx.Err() == nil {
			s.cond.Wait()
		}

		if s.ctx.Err() != nil {
			s.mu.Unlock()
			return
		}

		s.mu.Unlock()
		s.processNextBatch()
	}
}

func (s *Scheduler) hasWork() bool {
	for _, buffer := range s.buffers {
		if buffer.IsReadyToFlush() {
			return true
		}
	}
	return false
}

func (s *Scheduler) processNextBatch() {
	if len(s.currentCycle) == 0 {
		return
	}

	priority := s.currentCycle[s.cyclePos]
	s.cyclePos = (s.cyclePos + 1) % len(s.currentCycle)

	var bufferToProcess Buffer[protocol.TelemetryItem]
	var categoryToProcess ratelimit.Category
	for category, buffer := range s.buffers {
		if buffer.Priority() == priority && buffer.IsReadyToFlush() {
			bufferToProcess = buffer
			categoryToProcess = category
			break
		}
	}

	if bufferToProcess != nil {
		s.processItems(bufferToProcess, categoryToProcess, false)
	}
}

func (s *Scheduler) processItems(buffer Buffer[protocol.TelemetryItem], category ratelimit.Category, force bool) {
	var items []protocol.TelemetryItem

	if force {
		items = buffer.Drain()
	} else {
		items = buffer.PollIfReady()
	}

	// drop the current batch if rate-limited or if transport is full
	if len(items) == 0 || s.isRateLimited(category) || !s.transport.HasCapacity() {
		return
	}

	switch category {
	case ratelimit.CategoryLog:
		logs := protocol.Logs(items)
		header := &protocol.EnvelopeHeader{EventID: protocol.GenerateEventID(), SentAt: time.Now(), Sdk: s.sdkInfo}
		if s.dsn != nil {
			header.Dsn = s.dsn.String()
		}
		envelope := protocol.NewEnvelope(header)
		item, err := logs.ToEnvelopeItem()
		if err != nil {
			debuglog.Printf("error creating log batch envelope item: %v", err)
			return
		}
		envelope.AddItem(item)
		if err := s.transport.SendEnvelope(envelope); err != nil {
			debuglog.Printf("error sending envelope: %v", err)
		}
		return
	case ratelimit.CategoryTraceMetric:
		metrics := protocol.Metrics(items)
		header := &protocol.EnvelopeHeader{EventID: protocol.GenerateEventID(), SentAt: time.Now(), Sdk: s.sdkInfo}
		if s.dsn != nil {
			header.Dsn = s.dsn.String()
		}
		envelope := protocol.NewEnvelope(header)
		item, err := metrics.ToEnvelopeItem()
		if err != nil {
			debuglog.Printf("error creating trace metric batch envelope item: %v", err)
			return
		}
		envelope.AddItem(item)
		if err := s.transport.SendEnvelope(envelope); err != nil {
			debuglog.Printf("error sending envelope: %v", err)
		}
		return
	default:
		// if the buffers are properly configured, buffer.PollIfReady should return a single item for every category
		// other than logs. We still iterate over the items just in case, because we don't want to send broken envelopes.
		for _, it := range items {
			convertible, ok := it.(protocol.EnvelopeItemConvertible)
			if !ok {
				debuglog.Printf("item does not implement EnvelopeItemConvertible: %T", it)
				continue
			}
			s.sendItem(convertible)
		}
	}
}

func (s *Scheduler) sendItem(item protocol.EnvelopeItemConvertible) {
	header := &protocol.EnvelopeHeader{
		EventID: item.GetEventID(),
		SentAt:  time.Now(),
		Trace:   item.GetDynamicSamplingContext(),
		Sdk:     s.sdkInfo,
	}
	if header.EventID == "" {
		header.EventID = protocol.GenerateEventID()
	}
	if s.dsn != nil {
		header.Dsn = s.dsn.String()
	}
	envelope := protocol.NewEnvelope(header)
	envItem, err := item.ToEnvelopeItem()
	if err != nil {
		debuglog.Printf("error while converting to envelope item: %v", err)
		return
	}
	envelope.AddItem(envItem)
	if err := s.transport.SendEnvelope(envelope); err != nil {
		debuglog.Printf("error sending envelope: %v", err)
	}
}

func (s *Scheduler) flushBuffers() {
	for category, buffer := range s.buffers {
		if !buffer.IsEmpty() {
			s.processItems(buffer, category, true)
		}
	}
}

func (s *Scheduler) isRateLimited(category ratelimit.Category) bool {
	return s.transport.IsRateLimited(category)
}
