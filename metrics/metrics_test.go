package metrics_test

import (
	"testing"

	"github.com/facebookgo/grace/gracenet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/metrics"
)

func TestMetricsListenerCreation(t *testing.T) {
	t.Parallel()
	listeners := gracenet.Net{}
	listener1, err := metrics.CreateMetricsListener(&listeners, metrics.GetMetricsDefaultAddress("host"))
	assert.Equal(t, "127.0.0.1:20241", listener1.Addr().String())
	require.NoError(t, err)
	listener2, err := metrics.CreateMetricsListener(&listeners, metrics.GetMetricsDefaultAddress("host"))
	assert.Equal(t, "127.0.0.1:20242", listener2.Addr().String())
	require.NoError(t, err)
	listener3, err := metrics.CreateMetricsListener(&listeners, metrics.GetMetricsDefaultAddress("host"))
	assert.Equal(t, "127.0.0.1:20243", listener3.Addr().String())
	require.NoError(t, err)
	listener4, err := metrics.CreateMetricsListener(&listeners, metrics.GetMetricsDefaultAddress("host"))
	assert.Equal(t, "127.0.0.1:20244", listener4.Addr().String())
	require.NoError(t, err)
	listener5, err := metrics.CreateMetricsListener(&listeners, metrics.GetMetricsDefaultAddress("host"))
	assert.Equal(t, "127.0.0.1:20245", listener5.Addr().String())
	require.NoError(t, err)
	listener6, err := metrics.CreateMetricsListener(&listeners, metrics.GetMetricsDefaultAddress("host"))
	addresses := [5]string{"127.0.0.1:20241", "127.0.0.1:20242", "127.0.0.1:20243", "127.0.0.1:20244", "127.0.0.1:20245"}
	assert.NotContains(t, addresses, listener6.Addr().String())
	require.NoError(t, err)
	listener7, err := metrics.CreateMetricsListener(&listeners, "localhost:12345")
	assert.Equal(t, "127.0.0.1:12345", listener7.Addr().String())
	require.NoError(t, err)
	err = listener1.Close()
	require.NoError(t, err)
	err = listener2.Close()
	require.NoError(t, err)
	err = listener3.Close()
	require.NoError(t, err)
	err = listener4.Close()
	require.NoError(t, err)
	err = listener5.Close()
	require.NoError(t, err)
	err = listener6.Close()
	require.NoError(t, err)
	err = listener7.Close()
	require.NoError(t, err)
}
