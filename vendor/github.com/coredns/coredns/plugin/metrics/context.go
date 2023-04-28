package metrics

import (
	"context"

	"github.com/coredns/coredns/core/dnsserver"
)

// WithServer returns the current server handling the request. It returns the
// server listening address: <scheme>://[<bind>]:<port> Normally this is
// something like "dns://:53", but if the bind plugin is used, i.e. "bind
// 127.0.0.53", it will be "dns://127.0.0.53:53", etc. If not address is found
// the empty string is returned.
//
// Basic usage with a metric:
//
// <metric>.WithLabelValues(metrics.WithServer(ctx), labels..).Add(1)
func WithServer(ctx context.Context) string {
	srv := ctx.Value(dnsserver.Key{})
	if srv == nil {
		return ""
	}
	return srv.(*dnsserver.Server).Addr
}

// WithView returns the name of the view currently handling the request, if a view is defined.
//
// Basic usage with a metric:
//
// <metric>.WithLabelValues(metrics.WithView(ctx), labels..).Add(1)
func WithView(ctx context.Context) string {
	v := ctx.Value(dnsserver.ViewKey{})
	if v == nil {
		return ""
	}
	return v.(string)
}
