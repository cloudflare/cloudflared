package ingress

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "cloudflared"
)

var (
	icmpRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "icmp",
		Name:      "total_requests",
		Help:      "Total count of ICMP requests that have been proxied to any origin",
	})
	icmpReplies = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "icmp",
		Name:      "total_replies",
		Help:      "Total count of ICMP replies that have been proxied from any origin",
	})
)

func init() {
	prometheus.MustRegister(
		icmpRequests,
		icmpReplies,
	)
}

func incrementICMPRequest() {
	icmpRequests.Inc()
}

func incrementICMPReply() {
	icmpReplies.Inc()
}
