package middleware

import (
	"context"
	"net/http"
)

type Handler interface {
	Handle(ctx context.Context, r *http.Request) error
}
