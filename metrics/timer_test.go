package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestEnd(t *testing.T) {
	m := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "TestCallLatencyWithoutMeasurement",
			Name:      "Latency",
			Buckets:   prometheus.LinearBuckets(0, 50, 100),
		},
		[]string{"key"},
	)
	timer := NewTimer(m, time.Millisecond, "key")
	assert.Equal(t, time.Duration(0), timer.End("dne"))
	timer.Start("test")
	assert.NotEqual(t, time.Duration(0), timer.End("test"))
}
