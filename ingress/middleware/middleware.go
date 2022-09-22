package middleware

import (
	"context"
	"net/http"
)

type HandleResult struct {
	// Tells that the request didn't passed the handler and should be filtered
	ShouldFilterRequest bool
	// The status code to return in case ShouldFilterRequest is true.
	StatusCode int
	Reason     string
}

type Handler interface {
	Name() string
	Handle(ctx context.Context, r *http.Request) (result *HandleResult, err error)
}
