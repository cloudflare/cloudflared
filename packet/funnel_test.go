package packet

import "net/netip"

type mockFunnelUniPipe struct {
	uniPipe chan RawPacket
}

func (mfui *mockFunnelUniPipe) SendPacket(dst netip.Addr, pk RawPacket) error {
	mfui.uniPipe <- pk
	return nil
}

func (mfui *mockFunnelUniPipe) Close() error {
	return nil
}
