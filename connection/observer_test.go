package connection

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRegisterServerLocation(t *testing.T) {
	m := newTunnelMetrics()
	tunnels := 20
	var wg sync.WaitGroup
	wg.Add(tunnels)
	for i := 0; i < tunnels; i++ {
		go func(i int) {
			id := strconv.Itoa(i)
			m.registerServerLocation(id, "LHR")
			wg.Done()
		}(i)
	}
	wg.Wait()
	for i := 0; i < tunnels; i++ {
		id := strconv.Itoa(i)
		assert.Equal(t, "LHR", m.oldServerLocations[id])
	}

	wg.Add(tunnels)
	for i := 0; i < tunnels; i++ {
		go func(i int) {
			id := strconv.Itoa(i)
			m.registerServerLocation(id, "AUS")
			wg.Done()
		}(i)
	}
	wg.Wait()
	for i := 0; i < tunnels; i++ {
		id := strconv.Itoa(i)
		assert.Equal(t, "AUS", m.oldServerLocations[id])
	}

}

func TestObserverEventsDontBlock(t *testing.T) {
	observer := NewObserver(&log, &log, false)
	var mu sync.Mutex
	observer.RegisterSink(EventSinkFunc(func(_ Event) {
		// callback will block if lock is already held
		mu.Lock()
		mu.Unlock()
	}))

	timeout := time.AfterFunc(5*time.Second, func() {
		mu.Unlock() // release the callback on timer expiration
		t.Fatal("observer is blocked")
	})

	mu.Lock() // block the callback
	for i := 0; i < 2*observerChannelBufferSize; i++ {
		observer.sendRegisteringEvent(0)
	}
	if pending := timeout.Stop(); pending {
		// release the callback if timer hasn't expired yet
		mu.Unlock()
	}
}

type eventCollectorSink struct {
	observedEvents []Event
	mu             sync.Mutex
}

func (s *eventCollectorSink) OnTunnelEvent(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observedEvents = append(s.observedEvents, event)
}

func (s *eventCollectorSink) assertSawEvent(t *testing.T, event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	assert.Contains(t, s.observedEvents, event)
}
