package origin

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// can only be called once
var testMetrics = make([]string, 0)
var m = NewTunnelMetrics(testMetrics)

func TestConcurrentRequestsSingleTunnel(t *testing.T) {
	routines := 20
	var wg sync.WaitGroup
	wg.Add(routines)

	baseLabels := []string{"0"}
	hashKey := hashLabelValues(baseLabels)

	for i := 0; i < routines; i++ {
		go func() {
			m.incrementRequests(baseLabels)
			wg.Done()
		}()
	}
	wg.Wait()
	assert.Len(t, m.concurrentRequests, 1)
	assert.Equal(t, uint64(routines), m.concurrentRequests[hashKey])
	assert.Len(t, m.maxConcurrentRequests, 1)
	assert.Equal(t, uint64(routines), m.maxConcurrentRequests[hashKey])

	wg.Add(routines / 2)
	for i := 0; i < routines/2; i++ {
		go func() {
			m.decrementConcurrentRequests(baseLabels)
			wg.Done()
		}()
	}
	wg.Wait()
	assert.Equal(t, uint64(routines-routines/2), m.concurrentRequests[hashKey])
	assert.Equal(t, uint64(routines), m.maxConcurrentRequests[hashKey])
}

func TestConcurrentRequestsMultiTunnel(t *testing.T) {
	m.concurrentRequests = make(map[uint64]uint64)
	m.maxConcurrentRequests = make(map[uint64]uint64)
	tunnels := 20
	var wg sync.WaitGroup
	wg.Add(tunnels)
	for i := 0; i < tunnels; i++ {
		go func(i int) {
			// if we have j < i, then tunnel 0 won't have a chance to call incrementRequests
			for j := 0; j < i+1; j++ {
				labels := []string{strconv.Itoa(i)}
				m.incrementRequests(labels)
			}
			wg.Done()
		}(i)
	}
	wg.Wait()

	assert.Len(t, m.concurrentRequests, tunnels)
	assert.Len(t, m.maxConcurrentRequests, tunnels)
	for i := 0; i < tunnels; i++ {
		labels := []string{strconv.Itoa(i)}
		hashKey := hashLabelValues(labels)
		assert.Equal(t, uint64(i+1), m.concurrentRequests[hashKey])
		assert.Equal(t, uint64(i+1), m.maxConcurrentRequests[hashKey])
	}

	wg.Add(tunnels)
	for i := 0; i < tunnels; i++ {
		go func(i int) {
			for j := 0; j < i+1; j++ {
				labels := []string{strconv.Itoa(i)}
				m.decrementConcurrentRequests(labels)
			}
			wg.Done()
		}(i)
	}
	wg.Wait()

	assert.Len(t, m.concurrentRequests, tunnels)
	assert.Len(t, m.maxConcurrentRequests, tunnels)
	for i := 0; i < tunnels; i++ {
		labels := []string{strconv.Itoa(i)}
		hashKey := hashLabelValues(labels)
		assert.Equal(t, uint64(0), m.concurrentRequests[hashKey])
		assert.Equal(t, uint64(i+1), m.maxConcurrentRequests[hashKey])
	}

}
func TestRegisterServerLocation(t *testing.T) {
	tunnels := 20
	var wg sync.WaitGroup
	wg.Add(tunnels)
	for i := 0; i < tunnels; i++ {
		go func(i int) {
			labels := []string{strconv.Itoa(i)}
			m.registerServerLocation(labels, "LHR")
			wg.Done()
		}(i)
	}
	wg.Wait()
	for i := 0; i < tunnels; i++ {
		labels := []string{strconv.Itoa(i)}
		hashKey := hashLabelValues(labels)
		assert.Equal(t, "LHR", m.oldServerLocations[hashKey])
	}

	wg.Add(tunnels)
	for i := 0; i < tunnels; i++ {
		go func(i int) {
			labels := []string{strconv.Itoa(i)}
			m.registerServerLocation(labels, "AUS")
			wg.Done()
		}(i)
	}
	wg.Wait()
	for i := 0; i < tunnels; i++ {
		labels := []string{strconv.Itoa(i)}
		hashKey := hashLabelValues(labels)
		assert.Equal(t, "AUS", m.oldServerLocations[hashKey])
	}

}
