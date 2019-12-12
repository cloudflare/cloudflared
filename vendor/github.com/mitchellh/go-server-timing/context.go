package servertiming

import (
	"context"
)

// NewContext returns a new Context that carries the Header value h.
func NewContext(ctx context.Context, h *Header) context.Context {
	return context.WithValue(ctx, contextKey, h)
}

// FromContext returns the *Header in the context, if any. If no Header
// value exists, nil is returned.
func FromContext(ctx context.Context) *Header {
	h, _ := ctx.Value(contextKey).(*Header)
	return h
}

type contextKeyType struct{}

// The key where the header value is stored. This is globally unique since
// it uses a custom unexported type. The struct{} costs zero allocations.
var contextKey = contextKeyType(struct{}{})
