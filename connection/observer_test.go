package connection

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// can only be called once
var m = newTunnelMetrics()

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
