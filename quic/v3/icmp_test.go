package v3_test

import (
	"context"
	"testing"

	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/packet"
)

type noopICMPRouter struct{}

func (noopICMPRouter) Request(ctx context.Context, pk *packet.ICMP, responder ingress.ICMPResponder) error {
	return nil
}
func (noopICMPRouter) ConvertToTTLExceeded(pk *packet.ICMP, rawPacket packet.RawPacket) *packet.ICMP {
	return nil
}

type mockICMPRouter struct {
	recv chan *packet.ICMP
}

func newMockICMPRouter() *mockICMPRouter {
	return &mockICMPRouter{
		recv: make(chan *packet.ICMP, 1),
	}
}

func (m *mockICMPRouter) Request(ctx context.Context, pk *packet.ICMP, responder ingress.ICMPResponder) error {
	m.recv <- pk
	return nil
}
func (mockICMPRouter) ConvertToTTLExceeded(pk *packet.ICMP, rawPacket packet.RawPacket) *packet.ICMP {
	return packet.NewICMPTTLExceedPacket(pk.IP, rawPacket, testLocalAddr.AddrPort().Addr())
}

func assertICMPEqual(t *testing.T, expected *packet.ICMP, actual *packet.ICMP) {
	if expected.Src != actual.Src {
		t.Fatalf("Src address not equal: %+v\t%+v", expected, actual)
	}
	if expected.Dst != actual.Dst {
		t.Fatalf("Dst address not equal: %+v\t%+v", expected, actual)
	}
}
