package metadata

import (
	"context"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

// Metadata implements collecting metadata information from all plugins that
// implement the Provider interface.
type Metadata struct {
	Zones     []string
	Providers []Provider
	Next      plugin.Handler
}

// Name implements the Handler interface.
func (m *Metadata) Name() string { return "metadata" }

// ContextWithMetadata is exported for use by provider tests
func ContextWithMetadata(ctx context.Context) context.Context {
	return context.WithValue(ctx, key{}, md{})
}

// ServeDNS implements the plugin.Handler interface.
func (m *Metadata) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	rcode, err := plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
	return rcode, err
}

// Collect will retrieve metadata functions from each metadata provider and update the context
func (m *Metadata) Collect(ctx context.Context, state request.Request) context.Context {
	ctx = ContextWithMetadata(ctx)
	if plugin.Zones(m.Zones).Matches(state.Name()) != "" {
		// Go through all Providers and collect metadata.
		for _, p := range m.Providers {
			ctx = p.Metadata(ctx, state)
		}
	}
	return ctx
}
