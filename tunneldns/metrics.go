package tunneldns

import (
	"context"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"
	"github.com/coredns/coredns/plugin/metrics/vars"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/pkg/rcode"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
)

const (
	pluginName = "cloudflared"
)

// MetricsPlugin is an adapter for CoreDNS and built-in metrics
type MetricsPlugin struct {
	Next plugin.Handler
}

// NewMetricsPlugin creates a plugin with configured metrics
func NewMetricsPlugin(next plugin.Handler) *MetricsPlugin {
	return &MetricsPlugin{Next: next}
}

// ServeDNS implements the CoreDNS plugin interface
func (p MetricsPlugin) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}

	rw := dnstest.NewRecorder(w)
	status, err := plugin.NextOrFailure(p.Name(), p.Next, ctx, rw, r)

	// Update built-in metrics
	server := metrics.WithServer(ctx)
	vars.Report(server, state, ".", "", rcode.ToString(rw.Rcode), pluginName, rw.Len, rw.Start)

	return status, err
}

// Name implements the CoreDNS plugin interface
func (p MetricsPlugin) Name() string { return "metrics" }
