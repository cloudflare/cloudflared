package h2mux

import (
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
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
		assert.Equal(t, max-uint32(1), f.max)
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
	t.Skip("Inherently racy test due to muxMetricsUpdaterImpl.run()")
	errChan := make(chan error)
	abortChan := make(chan struct{})
	compBefore, compAfter := NewAtomicCounter(0), NewAtomicCounter(0)
	m := newMuxMetricsUpdater(abortChan, compBefore, compAfter)
	log := zerolog.Nop()

	go func() {
		errChan <- m.run(&log)
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	// mock muxReader
	readerStart := time.Now()
	rm := &roundTripMeasurement{receiveTime: readerStart, sendTime: readerStart}
	m.updateRTT(rm)
	go func() {
		defer wg.Done()
		assert.Equal(t, 0, dataPoints%4,
			"dataPoints is not divisible by 4; this test should be adjusted accordingly")
		readerSend := readerStart.Add(time.Millisecond)
		for i := 1; i <= dataPoints/4; i++ {
			readerReceive := readerSend.Add(time.Duration(i) * time.Millisecond)
			rm := &roundTripMeasurement{receiveTime: readerReceive, sendTime: readerSend}
			m.updateRTT(rm)
			readerSend = readerReceive.Add(time.Millisecond)
			m.updateReceiveWindow(uint32(i))
			m.updateSendWindow(uint32(i))

			m.updateInBoundBytes(uint64(i))
		}
	}()

	// mock muxWriter
	go func() {
		defer wg.Done()
		assert.Equal(t, 0, dataPoints%4,
			"dataPoints is not divisible by 4; this test should be adjusted accordingly")
		for j := dataPoints/4 + 1; j <= dataPoints/2; j++ {
			m.updateReceiveWindow(uint32(j))
			m.updateSendWindow(uint32(j))

			// should always be discarded since the send time is before readerSend
			rm := &roundTripMeasurement{receiveTime: readerStart, sendTime: readerStart.Add(-time.Duration(j*dataPoints) * time.Millisecond)}
			m.updateRTT(rm)

			m.updateOutBoundBytes(uint64(j))
		}

	}()
	wg.Wait()

	metrics := m.metrics()
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
