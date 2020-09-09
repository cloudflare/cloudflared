package tunneldns

import (
	"context"
	"sync"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"
	"github.com/coredns/coredns/plugin/metrics/vars"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/pkg/rcode"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
)

var once sync.Once

// MetricsPlugin is an adapter for CoreDNS and built-in metrics
type MetricsPlugin struct {
	Next plugin.Handler
}

// NewMetricsPlugin creates a plugin with configured metrics
func NewMetricsPlugin(next plugin.Handler) *MetricsPlugin {
	once.Do(func() {
		prometheus.MustRegister(vars.RequestCount)
		prometheus.MustRegister(vars.RequestDuration)
		prometheus.MustRegister(vars.RequestSize)
		prometheus.MustRegister(vars.RequestDo)
		prometheus.MustRegister(vars.ResponseSize)
		prometheus.MustRegister(vars.ResponseRcode)
	})
	return &MetricsPlugin{Next: next}
}

// ServeDNS implements the CoreDNS plugin interface
func (p MetricsPlugin) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}

	rw := dnstest.NewRecorder(w)
	status, err := plugin.NextOrFailure(p.Name(), p.Next, ctx, rw, r)

	// Update built-in metrics
	server := metrics.WithServer(ctx)
	vars.Report(server, state, ".", rcode.ToString(rw.Rcode), rw.Len, rw.Start)

	return status, err
}

// Name implements the CoreDNS plugin interface
func (p MetricsPlugin) Name() string { return "metrics" }
