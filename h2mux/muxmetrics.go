package h2mux

import (
	"sync"
	"time"

	"github.com/golang-collections/collections/queue"

	log "github.com/sirupsen/logrus"
)

// data points used to compute average receive window and send window size
const (
	// data points used to compute average receive window and send window size
	dataPoints = 100
	// updateFreq is set to 1 sec so we can get inbound & outbound byes/sec
	updateFreq = time.Second
)

type muxMetricsUpdater struct {
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
	// updateRTTChan is the channel to receive new RTT measurement from muxReader
	updateRTTChan <-chan *roundTripMeasurement
	//updateReceiveWindowChan is the channel to receive updated receiveWindow size from muxReader and muxWriter
	updateReceiveWindowChan <-chan uint32
	//updateSendWindowChan is the channel to receive updated sendWindow size from muxReader and muxWriter
	updateSendWindowChan <-chan uint32
	// updateInBoundBytesChan us the channel to receive bytesRead from muxReader
	updateInBoundBytesChan <-chan uint64
	// updateOutBoundBytesChan us the channel to receive bytesWrote from muxWriter
	updateOutBoundBytesChan <-chan uint64
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
	updateRTTChan <-chan *roundTripMeasurement,
	updateReceiveWindowChan <-chan uint32,
	updateSendWindowChan <-chan uint32,
	updateInBoundBytesChan <-chan uint64,
	updateOutBoundBytesChan <-chan uint64,
	abortChan <-chan struct{},
	compBytesBefore, compBytesAfter *AtomicCounter,
) *muxMetricsUpdater {
	return &muxMetricsUpdater{
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

func (updater *muxMetricsUpdater) Metrics() *MuxerMetrics {
	m := &MuxerMetrics{}
	m.RTT, m.RTTMin, m.RTTMax = updater.rttData.metrics()
	m.ReceiveWindowAve, m.ReceiveWindowMin, m.ReceiveWindowMax = updater.receiveWindowData.metrics()
	m.SendWindowAve, m.SendWindowMin, m.SendWindowMax = updater.sendWindowData.metrics()
	m.InBoundRateCurr, m.InBoundRateMin, m.InBoundRateMax = updater.inBoundRate.get()
	m.OutBoundRateCurr, m.OutBoundRateMin, m.OutBoundRateMax = updater.outBoundRate.get()
	m.CompBytesBefore, m.CompBytesAfter = updater.compBytesBefore, updater.compBytesAfter
	return m
}

func (updater *muxMetricsUpdater) run(parentLogger *log.Entry) error {
	logger := parentLogger.WithFields(log.Fields{
		"subsystem": "mux",
		"dir":       "metrics",
	})
	defer logger.Debug("event loop finished")
	for {
		select {
		case <-updater.abortChan:
			logger.Infof("Stopping mux metrics updater")
			return nil
		case roundTripMeasurement := <-updater.updateRTTChan:
			go updater.rttData.update(roundTripMeasurement)
			logger.Debug("Update rtt")
		case receiveWindow := <-updater.updateReceiveWindowChan:
			go updater.receiveWindowData.update(receiveWindow)
			logger.Debug("Update receive window")
		case sendWindow := <-updater.updateSendWindowChan:
			go updater.sendWindowData.update(sendWindow)
			logger.Debug("Update send window")
		case inBoundBytes := <-updater.updateInBoundBytesChan:
			// inBoundBytes is bytes/sec because the update interval is 1 sec
			go updater.inBoundRate.update(inBoundBytes)
			logger.Debugf("Inbound bytes %d", inBoundBytes)
		case outBoundBytes := <-updater.updateOutBoundBytesChan:
			// outBoundBytes is bytes/sec because the update interval is 1 sec
			go updater.outBoundRate.update(outBoundBytes)
			logger.Debugf("Outbound bytes %d", outBoundBytes)
		}
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
