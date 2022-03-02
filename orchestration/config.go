package orchestration

import (
	"github.com/cloudflare/cloudflared/ingress"
)

type newConfig struct {
	ingress.RemoteConfig
	// Add more fields when we support other settings in tunnel orchestration
}

type Config struct {
	Ingress            *ingress.Ingress
	WarpRoutingEnabled bool
}
