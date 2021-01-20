package h2mux

import (
	"sync"
	"time"

	"github.com/golang-collections/collections/queue"
	"github.com/rs/zerolog"
)

// data points used to compute average receive window and send window size
const (
	// data points used to compute average receive window and send window size
	dataPoints = 100
	// updateFreq is set to 1 sec so we can get inbound & outbound byes/sec
	updateFreq = time.Second
)

type muxMetricsUpdater interface {
	// metrics returns the latest metrics
	metrics() *MuxerMetrics
	// run is a blocking call to start the event loop
	run(log *zerolog.Logger) error
	// updateRTTChan is called by muxReader to report new RTT measurements
	updateRTT(rtt *roundTripMeasurement)
	//updateReceiveWindowChan is called by muxReader and muxWriter when receiveWindow size is updated
	updateReceiveWindow(receiveWindow uint32)
	//updateSendWindowChan is called by muxReader and muxWriter when sendWindow size is updated
	updateSendWindow(sendWindow uint32)
	// updateInBoundBytesChan is called  periodicallyby muxReader to report bytesRead
	updateInBoundBytes(inBoundBytes uint64)
	// updateOutBoundBytesChan is called periodically by muxWriter to report bytesWrote
	updateOutBoundBytes(outBoundBytes uint64)
}

type muxMetricsUpdaterImpl struct {
	// rttData keeps record of rtt, rttMin, rttMax and last measured time
	rttData *rttData
	// receiveWindowData keeps record of receive window measurement
	receiveWindowData *flowControlData
	// sendWindowData keeps record of send window measurement
	sendWindowData *flowControlData
	// inBoundRate is incoming bytes/sec
	inBoundRate *rate
	// outBoundRate is outgoing bytes/sec
	outBoundRate *rate
	// updateRTTChan is the channel to receive new RTT measurement
	updateRTTChan chan *roundTripMeasurement
	//updateReceiveWindowChan is the channel to receive updated receiveWindow size
	updateReceiveWindowChan chan uint32
	//updateSendWindowChan is the channel to receive updated sendWindow size
	updateSendWindowChan chan uint32
	// updateInBoundBytesChan us the channel to receive bytesRead
	updateInBoundBytesChan chan uint64
	// updateOutBoundBytesChan us the channel to receive bytesWrote
	updateOutBoundBytesChan chan uint64
	// shutdownC is to signal the muxerMetricsUpdater to shutdown
	abortChan <-chan struct{}

	compBytesBefore, compBytesAfter *AtomicCounter
}

type MuxerMetrics struct {
	RTT, RTTMin, RTTMax                                              time.Duration
	ReceiveWindowAve, SendWindowAve                                  float64
	ReceiveWindowMin, ReceiveWindowMax, SendWindowMin, SendWindowMax uint32
	InBoundRateCurr, InBoundRateMin, InBoundRateMax                  uint64
	OutBoundRateCurr, OutBoundRateMin, OutBoundRateMax               uint64
	CompBytesBefore, CompBytesAfter                                  *AtomicCounter
}

func (m *MuxerMetrics) CompRateAve() float64 {
	if m.CompBytesBefore.Value() == 0 {
		return 1.
	}
	return float64(m.CompBytesAfter.Value()) / float64(m.CompBytesBefore.Value())
}

type roundTripMeasurement struct {
	receiveTime, sendTime time.Time
}

type rttData struct {
	rtt, rttMin, rttMax time.Duration
	lastMeasurementTime time.Time
	lock                sync.RWMutex
}

type flowControlData struct {
	sum      uint64
	min, max uint32
	queue    *queue.Queue
	lock     sync.RWMutex
}

type rate struct {
	curr     uint64
	min, max uint64
	lock     sync.RWMutex
}

func newMuxMetricsUpdater(
	abortChan <-chan struct{},
	compBytesBefore, compBytesAfter *AtomicCounter,
) muxMetricsUpdater {
	updateRTTChan := make(chan *roundTripMeasurement, 1)
	updateReceiveWindowChan := make(chan uint32, 1)
	updateSendWindowChan := make(chan uint32, 1)
	updateInBoundBytesChan := make(chan uint64)
	updateOutBoundBytesChan := make(chan uint64)

	return &muxMetricsUpdaterImpl{
		rttData:                 newRTTData(),
		receiveWindowData:       newFlowControlData(),
		sendWindowData:          newFlowControlData(),
		inBoundRate:             newRate(),
		outBoundRate:            newRate(),
		updateRTTChan:           updateRTTChan,
		updateReceiveWindowChan: updateReceiveWindowChan,
		updateSendWindowChan:    updateSendWindowChan,
		updateInBoundBytesChan:  updateInBoundBytesChan,
		updateOutBoundBytesChan: updateOutBoundBytesChan,
		abortChan:               abortChan,
		compBytesBefore:         compBytesBefore,
		compBytesAfter:          compBytesAfter,
	}
}

func (updater *muxMetricsUpdaterImpl) metrics() *MuxerMetrics {
	m := &MuxerMetrics{}
	m.RTT, m.RTTMin, m.RTTMax = updater.rttData.metrics()
	m.ReceiveWindowAve, m.ReceiveWindowMin, m.ReceiveWindowMax = updater.receiveWindowData.metrics()
	m.SendWindowAve, m.SendWindowMin, m.SendWindowMax = updater.sendWindowData.metrics()
	m.InBoundRateCurr, m.InBoundRateMin, m.InBoundRateMax = updater.inBoundRate.get()
	m.OutBoundRateCurr, m.OutBoundRateMin, m.OutBoundRateMax = updater.outBoundRate.get()
	m.CompBytesBefore, m.CompBytesAfter = updater.compBytesBefore, updater.compBytesAfter
	return m
}

func (updater *muxMetricsUpdaterImpl) run(log *zerolog.Logger) error {
	defer log.Debug().Msg("mux - metrics: event loop finished")
	for {
		select {
		case <-updater.abortChan:
			log.Debug().Msgf("mux - metrics: Stopping mux metrics updater")
			return nil
		case roundTripMeasurement := <-updater.updateRTTChan:
			go updater.rttData.update(roundTripMeasurement)
			log.Debug().Msg("mux - metrics: Update rtt")
		case receiveWindow := <-updater.updateReceiveWindowChan:
			go updater.receiveWindowData.update(receiveWindow)
			log.Debug().Msg("mux - metrics: Update receive window")
		case sendWindow := <-updater.updateSendWindowChan:
			go updater.sendWindowData.update(sendWindow)
			log.Debug().Msg("mux - metrics: Update send window")
		case inBoundBytes := <-updater.updateInBoundBytesChan:
			// inBoundBytes is bytes/sec because the update interval is 1 sec
			go updater.inBoundRate.update(inBoundBytes)
			log.Debug().Msgf("mux - metrics: Inbound bytes %d", inBoundBytes)
		case outBoundBytes := <-updater.updateOutBoundBytesChan:
			// outBoundBytes is bytes/sec because the update interval is 1 sec
			go updater.outBoundRate.update(outBoundBytes)
			log.Debug().Msgf("mux - metrics: Outbound bytes %d", outBoundBytes)
		}
	}
}

func (updater *muxMetricsUpdaterImpl) updateRTT(rtt *roundTripMeasurement) {
	select {
	case updater.updateRTTChan <- rtt:
	case <-updater.abortChan:
	}

}

func (updater *muxMetricsUpdaterImpl) updateReceiveWindow(receiveWindow uint32) {
	select {
	case updater.updateReceiveWindowChan <- receiveWindow:
	case <-updater.abortChan:
	}
}

func (updater *muxMetricsUpdaterImpl) updateSendWindow(sendWindow uint32) {
	select {
	case updater.updateSendWindowChan <- sendWindow:
	case <-updater.abortChan:
	}
}

func (updater *muxMetricsUpdaterImpl) updateInBoundBytes(inBoundBytes uint64) {
	select {
	case updater.updateInBoundBytesChan <- inBoundBytes:
	case <-updater.abortChan:
	}

}

func (updater *muxMetricsUpdaterImpl) updateOutBoundBytes(outBoundBytes uint64) {
	select {
	case updater.updateOutBoundBytesChan <- outBoundBytes:
	case <-updater.abortChan:
	}
}

func newRTTData() *rttData {
	return &rttData{}
}

func (r *rttData) update(measurement *roundTripMeasurement) {
	r.lock.Lock()
	defer r.lock.Unlock()
	// discard pings before lastMeasurementTime
	if r.lastMeasurementTime.After(measurement.sendTime) {
		return
	}
	r.lastMeasurementTime = measurement.sendTime
	r.rtt = measurement.receiveTime.Sub(measurement.sendTime)
	if r.rttMax < r.rtt {
		r.rttMax = r.rtt
	}
	if r.rttMin == 0 || r.rttMin > r.rtt {
		r.rttMin = r.rtt
	}
}

func (r *rttData) metrics() (rtt, rttMin, rttMax time.Duration) {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return r.rtt, r.rttMin, r.rttMax
}

func newFlowControlData() *flowControlData {
	return &flowControlData{queue: queue.New()}
}

func (f *flowControlData) update(measurement uint32) {
	f.lock.Lock()
	defer f.lock.Unlock()
	var firstItem uint32
	// store new data into queue, remove oldest data if queue is full
	f.queue.Enqueue(measurement)
	if f.queue.Len() > dataPoints {
		// data type should always be uint32
		firstItem = f.queue.Dequeue().(uint32)
	}
	// if (measurement - firstItem) < 0, uint64(measurement - firstItem)
	// will overflow and become a large positive number
	f.sum += uint64(measurement)
	f.sum -= uint64(firstItem)
	if measurement > f.max {
		f.max = measurement
	}
	if f.min == 0 || measurement < f.min {
		f.min = measurement
	}
}

// caller of ave() should acquire lock first
func (f *flowControlData) ave() float64 {
	if f.queue.Len() == 0 {
		return 0
	}
	return float64(f.sum) / float64(f.queue.Len())
}

func (f *flowControlData) metrics() (ave float64, min, max uint32) {
	f.lock.RLock()
	defer f.lock.RUnlock()
	return f.ave(), f.min, f.max
}

func newRate() *rate {
	return &rate{}
}

func (r *rate) update(measurement uint64) {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.curr = measurement
	// if measurement is 0, then there is no incoming/outgoing connection, don't update min/max
	if r.curr == 0 {
		return
	}
	if measurement > r.max {
		r.max = measurement
	}
	if r.min == 0 || measurement < r.min {
		r.min = measurement
	}
}

func (r *rate) get() (curr, min, max uint64) {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return r.curr, r.min, r.max
}
