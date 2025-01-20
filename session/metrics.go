package session

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "session"
)

var (
	labels = []string{"session_type"}

	sessionRegistrationsDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "client",
		Name:      "registrations_rate_limited_total",
		Help:      "Count registrations dropped due to high number of concurrent sessions being handled",
	},
		labels,
	)
)
