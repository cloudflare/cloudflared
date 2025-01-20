package flow

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "flow"
)

var (
	labels = []string{"flow_type"}

	flowRegistrationsDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "client",
		Name:      "registrations_rate_limited_total",
		Help:      "Count registrations dropped due to high number of concurrent flows being handled",
	},
		labels,
	)
)
