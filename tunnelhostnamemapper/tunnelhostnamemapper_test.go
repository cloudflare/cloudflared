package tunnelhostnamemapper

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/originservice"
	"github.com/stretchr/testify/assert"
)

const (
	routines = 1000
)

func TestTunnelHostnameMapperConcurrentAccess(t *testing.T) {
	thm := NewTunnelHostnameMapper()

	concurrentOps(t, func(i int) {
		// om is empty
		os, ok := thm.Get(tunnelHostname(i))
		assert.False(t, ok)
		assert.Nil(t, os)
	})

	httpOS := originservice.NewHTTPService(http.DefaultTransport, "127.0.0.1:8080", false)
	concurrentOps(t, func(i int) {
		thm.Add(tunnelHostname(i), httpOS)
	})

	concurrentOps(t, func(i int) {
		os, ok := thm.Get(tunnelHostname(i))
		assert.True(t, ok)
		assert.Equal(t, httpOS, os)
	})

	secondHTTPOS := originservice.NewHTTPService(http.DefaultTransport, "127.0.0.1:8090", true)
	concurrentOps(t, func(i int) {
		// Add should httpOS with secondHTTPOS
		thm.Add(tunnelHostname(i), secondHTTPOS)
	})

	concurrentOps(t, func(i int) {
		os, ok := thm.Get(tunnelHostname(i))
		assert.True(t, ok)
		assert.Equal(t, secondHTTPOS, os)
	})

	thm.DeleteAll()
	assert.Empty(t, thm.tunnelHostnameToOrigin)
}

func concurrentOps(t *testing.T, f func(i int)) {
	var wg sync.WaitGroup
	wg.Add(routines)
	for i := 0; i < routines; i++ {
		go func(i int) {
			f(i)
			wg.Done()
		}(i)
	}
	wg.Wait()
}

func tunnelHostname(i int) h2mux.TunnelHostname {
	return h2mux.TunnelHostname(fmt.Sprintf("%d.cftunnel.com", i))
}
