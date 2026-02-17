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
	ls1, err := metrics.CreateMetricsListener(&listeners, []string{metrics.GetMetricsDefaultAddress("host")})
	require.NoError(t, err)
	require.Len(t, ls1, 1)
	listener1 := ls1[0]
	assert.Equal(t, "127.0.0.1:20241", listener1.Addr().String())

	ls2, err := metrics.CreateMetricsListener(&listeners, []string{metrics.GetMetricsDefaultAddress("host")})
	require.NoError(t, err)
	require.Len(t, ls2, 1)
	listener2 := ls2[0]
	assert.Equal(t, "127.0.0.1:20242", listener2.Addr().String())

	ls3, err := metrics.CreateMetricsListener(&listeners, []string{metrics.GetMetricsDefaultAddress("host")})
	require.NoError(t, err)
	require.Len(t, ls3, 1)
	listener3 := ls3[0]
	assert.Equal(t, "127.0.0.1:20243", listener3.Addr().String())

	ls4, err := metrics.CreateMetricsListener(&listeners, []string{metrics.GetMetricsDefaultAddress("host")})
	require.NoError(t, err)
	require.Len(t, ls4, 1)
	listener4 := ls4[0]
	assert.Equal(t, "127.0.0.1:20244", listener4.Addr().String())

	ls5, err := metrics.CreateMetricsListener(&listeners, []string{metrics.GetMetricsDefaultAddress("host")})
	require.NoError(t, err)
	require.Len(t, ls5, 1)
	listener5 := ls5[0]
	assert.Equal(t, "127.0.0.1:20245", listener5.Addr().String())

	ls6, err := metrics.CreateMetricsListener(&listeners, []string{metrics.GetMetricsDefaultAddress("host")})
	require.NoError(t, err)
	require.Len(t, ls6, 1)
	listener6 := ls6[0]
	addresses := [5]string{"127.0.0.1:20241", "127.0.0.1:20242", "127.0.0.1:20243", "127.0.0.1:20244", "127.0.0.1:20245"}
	assert.NotContains(t, addresses, listener6.Addr().String())

	ls7, err := metrics.CreateMetricsListener(&listeners, []string{"localhost:12345"})
	require.NoError(t, err)
	require.Len(t, ls7, 1)
	listener7 := ls7[0]
	assert.Equal(t, "127.0.0.1:12345", listener7.Addr().String())

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

	ls8, err := metrics.CreateMetricsListener(&listeners, []string{"127.0.0.1:12346", "127.0.0.1:12347"})
	require.NoError(t, err)
	require.Len(t, ls8, 2)
	assert.Equal(t, "127.0.0.1:12346", ls8[0].Addr().String())
	assert.Equal(t, "127.0.0.1:12347", ls8[1].Addr().String())
	err = ls8[0].Close()
	require.NoError(t, err)
	err = ls8[1].Close()
	require.NoError(t, err)
}
