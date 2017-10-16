package h2mux

import (
	"sync/atomic"
	"time"
)

// PingTimestamp is an atomic interface around ping timestamping and signalling.
type PingTimestamp struct {
	ts     int64
	signal Signal
}

func NewPingTimestamp() *PingTimestamp {
	return &PingTimestamp{signal: NewSignal()}
}

func (pt *PingTimestamp) Set(v int64) {
	if atomic.SwapInt64(&pt.ts, v) != 0 {
		pt.signal.Signal()
	}
}

func (pt *PingTimestamp) Get() int64 {
	return atomic.SwapInt64(&pt.ts, 0)
}

func (pt *PingTimestamp) GetUpdateChan() <-chan struct{} {
	return pt.signal.WaitChannel()
}

// RTTMeasurement encapsulates a continuous round trip time measurement.
type RTTMeasurement struct {
	Current, Min, Max   time.Duration
	lastMeasurementTime time.Time
}

// Update updates the computed values with a new measurement.
// outgoingTime is the time that the probe was sent.
// We assume that time.Now() is the time we received that probe.
func (r *RTTMeasurement) Update(outgoingTime time.Time) {
	if !r.lastMeasurementTime.Before(outgoingTime) {
		return
	}
	r.lastMeasurementTime = outgoingTime
	r.Current = time.Since(outgoingTime)
	if r.Max < r.Current {
		r.Max = r.Current
	}
	if r.Min > r.Current {
		r.Min = r.Current
	}
}
