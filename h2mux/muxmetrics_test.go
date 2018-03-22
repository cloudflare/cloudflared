package h2mux

import (
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func ave(sum uint64, len int) float64 {
	return float64(sum) / float64(len)
}

func TestRTTUpdate(t *testing.T) {
	r := newRTTData()
	start := time.Now()
	// send at 0 ms, receive at 2 ms, RTT = 2ms
	m := &roundTripMeasurement{receiveTime: start.Add(2 * time.Millisecond), sendTime: start}
	r.update(m)
	assert.Equal(t, start, r.lastMeasurementTime)
	assert.Equal(t, 2*time.Millisecond, r.rtt)
	assert.Equal(t, 2*time.Millisecond, r.rttMin)
	assert.Equal(t, 2*time.Millisecond, r.rttMax)

	// send at 3 ms, receive at 6 ms, RTT = 3ms
	m = &roundTripMeasurement{receiveTime: start.Add(6 * time.Millisecond), sendTime: start.Add(3 * time.Millisecond)}
	r.update(m)
	assert.Equal(t, start.Add(3*time.Millisecond), r.lastMeasurementTime)
	assert.Equal(t, 3*time.Millisecond, r.rtt)
	assert.Equal(t, 2*time.Millisecond, r.rttMin)
	assert.Equal(t, 3*time.Millisecond, r.rttMax)

	// send at 7 ms, receive at 8 ms, RTT = 1ms
	m = &roundTripMeasurement{receiveTime: start.Add(8 * time.Millisecond), sendTime: start.Add(7 * time.Millisecond)}
	r.update(m)
	assert.Equal(t, start.Add(7*time.Millisecond), r.lastMeasurementTime)
	assert.Equal(t, 1*time.Millisecond, r.rtt)
	assert.Equal(t, 1*time.Millisecond, r.rttMin)
	assert.Equal(t, 3*time.Millisecond, r.rttMax)

	// send at -4 ms, receive at 0 ms, RTT = 4ms, but this ping is before last measurement
	// so it will be discarded
	m = &roundTripMeasurement{receiveTime: start, sendTime: start.Add(-2 * time.Millisecond)}
	r.update(m)
	assert.Equal(t, start.Add(7*time.Millisecond), r.lastMeasurementTime)
	assert.Equal(t, 1*time.Millisecond, r.rtt)
	assert.Equal(t, 1*time.Millisecond, r.rttMin)
	assert.Equal(t, 3*time.Millisecond, r.rttMax)
}

func TestFlowControlDataUpdate(t *testing.T) {
	f := newFlowControlData()
	assert.Equal(t, 0, f.queue.Len())
	assert.Equal(t, float64(0), f.ave())

	var sum uint64
	min := maxWindowSize - dataPoints
	max := maxWindowSize
	for i := 1; i <= dataPoints; i++ {
		size := maxWindowSize - uint32(i)
		f.update(size)
		assert.Equal(t, max - uint32(1), f.max)
		assert.Equal(t, size, f.min)

		assert.Equal(t, i, f.queue.Len())

		sum += uint64(size)
		assert.Equal(t, sum, f.sum)
		assert.Equal(t, ave(sum, f.queue.Len()), f.ave())
	}

	// queue is full, should start to dequeue first element
	for i := 1; i <= dataPoints; i++ {
		f.update(max)
		assert.Equal(t, max, f.max)
		assert.Equal(t, min, f.min)

		assert.Equal(t, dataPoints, f.queue.Len())

		sum += uint64(i)
		assert.Equal(t, sum, f.sum)
		assert.Equal(t, ave(sum, dataPoints), f.ave())
	}
}

func TestMuxMetricsUpdater(t *testing.T) {
	updateRTTChan := make(chan *roundTripMeasurement)
	updateReceiveWindowChan := make(chan uint32)
	updateSendWindowChan := make(chan uint32)
	updateInBoundBytesChan := make(chan uint64)
	updateOutBoundBytesChan := make(chan uint64)
	abortChan := make(chan struct{})
	errChan := make(chan error)
	m := newMuxMetricsUpdater(updateRTTChan,
		updateReceiveWindowChan,
		updateSendWindowChan,
		updateInBoundBytesChan,
		updateOutBoundBytesChan,
		abortChan,
	)
	logger := log.NewEntry(log.New())

	go func() {
		errChan <- m.run(logger)
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	// mock muxReader
	readerStart := time.Now()
	rm := &roundTripMeasurement{receiveTime: readerStart, sendTime: readerStart}
	updateRTTChan <- rm
	go func() {
		defer wg.Done()
		// Becareful if dataPoints is not divisibile by 4
		readerSend := readerStart.Add(time.Millisecond)
		for i := 1; i <= dataPoints/4; i++ {
			readerReceive := readerSend.Add(time.Duration(i) * time.Millisecond)
			rm := &roundTripMeasurement{receiveTime: readerReceive, sendTime: readerSend}
			updateRTTChan <- rm
			readerSend = readerReceive.Add(time.Millisecond)

			updateReceiveWindowChan <- uint32(i)
			updateSendWindowChan <- uint32(i)

			updateInBoundBytesChan <- uint64(i)
		}
	}()

	// mock muxWriter
	go func() {
		defer wg.Done()
		for j := dataPoints/4 + 1; j <= dataPoints/2; j++ {
			updateReceiveWindowChan <- uint32(j)
			updateSendWindowChan <- uint32(j)

			// should always be disgard since the send time is before readerSend
			rm := &roundTripMeasurement{receiveTime: readerStart, sendTime: readerStart.Add(-time.Duration(j*dataPoints) * time.Millisecond)}
			updateRTTChan <- rm

			updateOutBoundBytesChan <- uint64(j)
		}

	}()
	wg.Wait()

	metrics := m.Metrics()
	points := dataPoints / 2
	assert.Equal(t, time.Millisecond, metrics.RTTMin)
	assert.Equal(t, time.Duration(dataPoints/4)*time.Millisecond, metrics.RTTMax)

	// sum(1..i) = i*(i+1)/2, ave(1..i) = i*(i+1)/2/i = (i+1)/2
	assert.Equal(t, float64(points+1)/float64(2), metrics.ReceiveWindowAve)
	assert.Equal(t, uint32(1), metrics.ReceiveWindowMin)
	assert.Equal(t, uint32(points), metrics.ReceiveWindowMax)

	assert.Equal(t, float64(points+1)/float64(2), metrics.SendWindowAve)
	assert.Equal(t, uint32(1), metrics.SendWindowMin)
	assert.Equal(t, uint32(points), metrics.SendWindowMax)

	assert.Equal(t, uint64(dataPoints/4), metrics.InBoundRateCurr)
	assert.Equal(t, uint64(1), metrics.InBoundRateMin)
	assert.Equal(t, uint64(dataPoints/4), metrics.InBoundRateMax)

	assert.Equal(t, uint64(dataPoints/2), metrics.OutBoundRateCurr)
	assert.Equal(t, uint64(dataPoints/4+1), metrics.OutBoundRateMin)
	assert.Equal(t, uint64(dataPoints/2), metrics.OutBoundRateMax)

	close(abortChan)
	assert.Nil(t, <-errChan)
	close(errChan)

}
