package datagramsession

import (
	"context"

	"github.com/google/uuid"
)

type mockQUICTransport struct {
	reqChan  *datagramChannel
	respChan *datagramChannel
}

func (mt *mockQUICTransport) SendTo(sessionID uuid.UUID, payload []byte) error {
	buf := make([]byte, len(payload))
	// The QUIC implementation copies data to another buffer before returning https://github.com/lucas-clemente/quic-go/blob/v0.24.0/session.go#L1967-L1975
	copy(buf, payload)
	return mt.respChan.Send(context.Background(), sessionID, buf)
}

func (mt *mockQUICTransport) ReceiveFrom() (uuid.UUID, []byte, error) {
	return mt.reqChan.Receive(context.Background())
}

func (mt *mockQUICTransport) MTU() uint {
	return 1217
}

func (mt *mockQUICTransport) newRequest(ctx context.Context, sessionID uuid.UUID, payload []byte) error {
	return mt.reqChan.Send(ctx, sessionID, payload)
}

func (mt *mockQUICTransport) close() {
	mt.reqChan.Close()
	mt.respChan.Close()
}
