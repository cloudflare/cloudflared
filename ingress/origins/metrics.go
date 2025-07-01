package origins

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "cloudflared"
	subsystem = "virtual_origins"
)

type Metrics interface {
	IncrementDNSUDPRequests()
	IncrementDNSTCPRequests()
}

type metrics struct {
	dnsResolverRequests *prometheus.CounterVec
}

func (m *metrics) IncrementDNSUDPRequests() {
	m.dnsResolverRequests.WithLabelValues("udp").Inc()
}

func (m *metrics) IncrementDNSTCPRequests() {
	m.dnsResolverRequests.WithLabelValues("tcp").Inc()
}

func NewMetrics(registerer prometheus.Registerer) Metrics {
	m := &metrics{
		dnsResolverRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "dns_requests_total",
			Help:      "Total count of DNS requests that have been proxied to the virtual DNS resolver origin",
		}, []string{"protocol"}),
	}
	registerer.MustRegister(m.dnsResolverRequests)
	return m
}
