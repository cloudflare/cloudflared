package metrics

import (
	"context"
	"path/filepath"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics/vars"
	"github.com/coredns/coredns/plugin/pkg/rcode"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

// ServeDNS implements the Handler interface.
func (m *Metrics) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}

	// Capture the original request size before any plugins modify it
	originalSize := r.Len()

	qname := state.QName()
	zone := plugin.Zones(m.ZoneNames()).Matches(qname)
	if zone == "" {
		zone = "."
	}

	// Record response to get status code and size of the reply.
	rw := NewRecorder(w)
	status, err := plugin.NextOrFailure(m.Name(), m.Next, ctx, rw, r)

	rc := rw.Rcode
	if !plugin.ClientWrite(status) {
		// when no response was written, fallback to status returned from next plugin as this status
		// is actually used as rcode of DNS response
		// see https://github.com/coredns/coredns/blob/master/core/dnsserver/server.go#L318
		rc = status
	}
	plugin := m.authoritativePlugin(rw.Caller)
	// Pass the original request size to vars.Report
	vars.Report(WithServer(ctx), state, zone, WithView(ctx), rcode.ToString(rc), plugin,
		rw.Len, rw.Start, vars.WithOriginalReqSize(originalSize))

	return status, err
}

// Name implements the Handler interface.
func (m *Metrics) Name() string { return "prometheus" }

// authoritativePlugin returns which of made the write, if none is found the empty string is returned.
func (m *Metrics) authoritativePlugin(caller [3]string) string {
	// a b and c contain the full path of the caller, the plugin name 2nd last elements
	// .../coredns/plugin/whoami/whoami.go --> whoami
	// this is likely FS specific, so use filepath.
	for _, c := range caller {
		plug := filepath.Base(filepath.Dir(c))
		if _, ok := m.plugins[plug]; ok {
			return plug
		}
	}
	return ""
}
