package connection

import (
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendUrl(t *testing.T) {
	observer := NewObserver(&log)

	observer.SendURL("my-url.com")
	assert.InEpsilon(t, 1.0, getCounterValue(t, observer.metrics.userHostnamesCounts, "https://my-url.com"), 0.001)

	observer.SendURL("https://another-long-one.com")
	assert.InEpsilon(t, 1.0, getCounterValue(t, observer.metrics.userHostnamesCounts, "https://another-long-one.com"), 0.001)
}

func getCounterValue(t *testing.T, metric *prometheus.CounterVec, val string) float64 {
	var m = &dto.Metric{}
	err := metric.WithLabelValues(val).Write(m)
	require.NoError(t, err)
	return m.Counter.GetValue()
}

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
	observer := NewObserver(&log)
	var mu sync.Mutex
	observer.RegisterSink(EventSinkFunc(func(_ Event) {
		// callback will block if lock is already held
		mu.Lock()
		defer mu.Unlock()
		runtime.Gosched()
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

type countingSink struct {
	mu     sync.Mutex
	events []Event
}

func (s *countingSink) OnTunnelEvent(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *countingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

func TestObserverClose(t *testing.T) {
	t.Run("stops dispatching events", func(t *testing.T) {
		observer := NewObserver(&log)
		sink := &countingSink{}
		observer.RegisterSink(sink)
		// Registration and events travel on separate channels, so resend until
		// the dispatcher has installed the sink and delivers an event to it.
		require.Eventually(t, func() bool {
			observer.SendDisconnect(0)
			return sink.count() > 0
		}, time.Second, time.Millisecond)

		observer.Close()
		// Let any delivery already in flight when Close was called finish. After
		// that, Close has priority in the dispatcher and nothing more is delivered.
		time.Sleep(50 * time.Millisecond)
		before := sink.count()
		observer.SendDisconnect(0)
		time.Sleep(50 * time.Millisecond)
		assert.Equal(t, before, sink.count())
	})

	t.Run("RegisterSink does not block after close", func(t *testing.T) {
		observer := NewObserver(&log)
		observer.Close()
		time.Sleep(50 * time.Millisecond)

		done := make(chan struct{})
		go func() {
			// More than the channel buffer, so a blocked send would hang.
			for i := 0; i < observerChannelBufferSize*2; i++ {
				observer.RegisterSink(&countingSink{})
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("RegisterSink blocked after Close")
		}
	})

	t.Run("Close is idempotent", func(t *testing.T) {
		observer := NewObserver(&log)
		observer.Close()
		assert.NotPanics(t, observer.Close)
	})
}
