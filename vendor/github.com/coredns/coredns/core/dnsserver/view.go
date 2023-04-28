package dnsserver

import (
	"context"

	"github.com/coredns/coredns/request"
)

// Viewer - If Viewer is implemented by a plugin in a server block, its Filter()
// is added to the server block's filter functions when starting the server. When a running server
// serves a DNS request, it will route the request to the first Config (server block) that passes
// all its filter functions.
type Viewer interface {
	// Filter returns true if the server should use the server block in which the implementing plugin resides, and the
	// name of the view for metrics logging.
	Filter(ctx context.Context, req *request.Request) bool

	// ViewName returns the name of the view
	ViewName() string
}
