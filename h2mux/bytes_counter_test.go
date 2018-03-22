package h2mux

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCounter(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(dataPoints)
	c := AtomicCounter{}
	for i := 0; i < dataPoints; i++ {
		go func() {
			defer wg.Done()
			c.IncrementBy(uint64(1))
		}()
	}
	wg.Wait()
	assert.Equal(t, uint64(dataPoints), c.Count())
	assert.Equal(t, uint64(0), c.Count())
}
