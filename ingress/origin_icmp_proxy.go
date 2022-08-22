package ingress

import (
	"context"
	"net"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/packet"
)

// ICMPProxy sends ICMP messages and listens for their responses
type ICMPProxy interface {
	// Request sends an ICMP message
	Request(pk *packet.ICMP, responder packet.FlowResponder) error
	// ListenResponse listens for responses to the requests until context is done
	ListenResponse(ctx context.Context) error
}

func NewICMPProxy(listenIP net.IP, logger *zerolog.Logger) (ICMPProxy, error) {
	return newICMPProxy(listenIP, logger)
}
