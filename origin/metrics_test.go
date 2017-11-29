package origin

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// can only be called once
var m = NewTunnelMetrics()

func TestConcurrentRequestsSingleTunnel(t *testing.T) {
	routines := 20
	var wg sync.WaitGroup
	wg.Add(routines)
	for i := 0; i < routines; i++ {
		go func() {
			m.incrementRequests("0")
			wg.Done()
		}()
	}
	wg.Wait()
	assert.Len(t, m.concurrentRequests, 1)
	assert.Equal(t, uint64(routines), m.concurrentRequests["0"])
	assert.Len(t, m.maxConcurrentRequests, 1)
	assert.Equal(t, uint64(routines), m.maxConcurrentRequests["0"])

	wg.Add(routines / 2)
	for i := 0; i < routines/2; i++ {
		go func() {
			m.decrementConcurrentRequests("0")
			wg.Done()
		}()
	}
	wg.Wait()
	assert.Equal(t, uint64(routines-routines/2), m.concurrentRequests["0"])
	assert.Equal(t, uint64(routines), m.maxConcurrentRequests["0"])
}

func TestConcurrentRequestsMultiTunnel(t *testing.T) {
	m.concurrentRequests = make(map[string]uint64)
	m.maxConcurrentRequests = make(map[string]uint64)
	tunnels := 20
	var wg sync.WaitGroup
	wg.Add(tunnels)
	for i := 0; i < tunnels; i++ {
		go func(i int) {
			// if we have j < i, then tunnel 0 won't have a chance to call incrementRequests
			for j := 0; j < i+1; j++ {
				id := strconv.Itoa(i)
				m.incrementRequests(id)
			}
			wg.Done()
		}(i)
	}
	wg.Wait()

	assert.Len(t, m.concurrentRequests, tunnels)
	assert.Len(t, m.maxConcurrentRequests, tunnels)
	for i := 0; i < tunnels; i++ {
		id := strconv.Itoa(i)
		assert.Equal(t, uint64(i+1), m.concurrentRequests[id])
		assert.Equal(t, uint64(i+1), m.maxConcurrentRequests[id])
	}

	wg.Add(tunnels)
	for i := 0; i < tunnels; i++ {
		go func(i int) {
			for j := 0; j < i+1; j++ {
				id := strconv.Itoa(i)
				m.decrementConcurrentRequests(id)
			}
			wg.Done()
		}(i)
	}
	wg.Wait()

	assert.Len(t, m.concurrentRequests, tunnels)
	assert.Len(t, m.maxConcurrentRequests, tunnels)
	for i := 0; i < tunnels; i++ {
		id := strconv.Itoa(i)
		assert.Equal(t, uint64(0), m.concurrentRequests[id])
		assert.Equal(t, uint64(i+1), m.maxConcurrentRequests[id])
	}

}

func TestRegisterServerLocation(t *testing.T) {
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
