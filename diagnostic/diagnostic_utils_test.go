package diagnostic_test

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/facebookgo/grace/gracenet"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/diagnostic"
	"github.com/cloudflare/cloudflared/metrics"
	"github.com/cloudflare/cloudflared/tunnelstate"
)

func helperCreateServer(t *testing.T, listeners *gracenet.Net, tunnelID uuid.UUID, connectorID uuid.UUID) func() {
	t.Helper()
	listener, err := metrics.CreateMetricsListener(listeners, "localhost:0")
	require.NoError(t, err)
	log := zerolog.Nop()
	tracker := tunnelstate.NewConnTracker(&log)
	handler := diagnostic.NewDiagnosticHandler(&log, 0, nil, tunnelID, connectorID, tracker, map[string]string{}, []string{})
	router := http.NewServeMux()
	router.HandleFunc("/diag/tunnel", handler.TunnelStateHandler)
	server := &http.Server{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		Handler:      router,
	}

	var wgroup sync.WaitGroup

	wgroup.Add(1)

	go func() {
		defer wgroup.Done()

		_ = server.Serve(listener)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)

	cleanUp := func() {
		_ = server.Shutdown(ctx)

		cancel()
		wgroup.Wait()
	}

	return cleanUp
}

func TestFindMetricsServer_WhenSingleServerIsRunning_ReturnState(t *testing.T) {
	listeners := gracenet.Net{}
	tid1 := uuid.New()
	cid1 := uuid.New()

	cleanUp := helperCreateServer(t, &listeners, tid1, cid1)
	defer cleanUp()

	log := zerolog.Nop()
	client := diagnostic.NewHTTPClient()
	addresses := metrics.GetMetricsKnownAddresses("host")
	url1, err := url.Parse("http://localhost:20241")
	require.NoError(t, err)

	tunnel1 := &diagnostic.AddressableTunnelState{
		TunnelState: &diagnostic.TunnelState{
			TunnelID:    tid1,
			ConnectorID: cid1,
			Connections: nil,
		},
		URL: url1,
	}

	state, tunnels, err := diagnostic.FindMetricsServer(&log, client, addresses[:])
	if err != nil {
		require.ErrorIs(t, err, diagnostic.ErrMultipleMetricsServerFound)
	}

	assert.Equal(t, tunnel1, state)
	assert.Nil(t, tunnels)
}

func TestFindMetricsServer_WhenMultipleServerAreRunning_ReturnError(t *testing.T) {
	listeners := gracenet.Net{}
	tid1 := uuid.New()
	cid1 := uuid.New()
	cid2 := uuid.New()

	cleanUp := helperCreateServer(t, &listeners, tid1, cid1)
	defer cleanUp()

	cleanUp = helperCreateServer(t, &listeners, tid1, cid2)
	defer cleanUp()

	log := zerolog.Nop()
	client := diagnostic.NewHTTPClient()
	addresses := metrics.GetMetricsKnownAddresses("host")
	url1, err := url.Parse("http://localhost:20241")
	require.NoError(t, err)
	url2, err := url.Parse("http://localhost:20242")
	require.NoError(t, err)

	tunnel1 := &diagnostic.AddressableTunnelState{
		TunnelState: &diagnostic.TunnelState{
			TunnelID:    tid1,
			ConnectorID: cid1,
			Connections: nil,
		},
		URL: url1,
	}
	tunnel2 := &diagnostic.AddressableTunnelState{
		TunnelState: &diagnostic.TunnelState{
			TunnelID:    tid1,
			ConnectorID: cid2,
			Connections: nil,
		},
		URL: url2,
	}

	state, tunnels, err := diagnostic.FindMetricsServer(&log, client, addresses[:])
	if err != nil {
		require.ErrorIs(t, err, diagnostic.ErrMultipleMetricsServerFound)
	}

	assert.Nil(t, state)
	assert.Equal(t, []*diagnostic.AddressableTunnelState{tunnel1, tunnel2}, tunnels)
}

func TestFindMetricsServer_WhenNoInstanceIsRuning_ReturnError(t *testing.T) {
	log := zerolog.Nop()
	client := diagnostic.NewHTTPClient()
	addresses := metrics.GetMetricsKnownAddresses("host")

	state, tunnels, err := diagnostic.FindMetricsServer(&log, client, addresses[:])
	require.ErrorIs(t, err, diagnostic.ErrMetricsServerNotFound)

	assert.Nil(t, state)
	assert.Nil(t, tunnels)
}
