//go:build !darwin && !linux && (!windows || !cgo)

package ingress

import (
	"context"
	"fmt"
	"net/netip"
	"runtime"
	"time"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/packet"
)

var errICMPProxyNotImplemented = fmt.Errorf("ICMP proxy is not implemented on %s %s", runtime.GOOS, runtime.GOARCH)

type icmpProxy struct{}

func (ip icmpProxy) Request(ctx context.Context, pk *packet.ICMP, responder ICMPResponder) error {
	return errICMPProxyNotImplemented
}

func (ip *icmpProxy) Serve(ctx context.Context) error {
	return errICMPProxyNotImplemented
}

func newICMPProxy(listenIP netip.Addr, logger *zerolog.Logger, idleTimeout time.Duration) (*icmpProxy, error) {
	return nil, errICMPProxyNotImplemented
}
