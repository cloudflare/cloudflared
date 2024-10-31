package v3_test

import v3 "github.com/cloudflare/cloudflared/quic/v3"

type noopEyeball struct{}

func (noopEyeball) SendUDPSessionDatagram(datagram []byte) error {
	return nil
}

func (noopEyeball) SendUDPSessionResponse(id v3.RequestID, resp v3.SessionRegistrationResp) error {
	return nil
}

type mockEyeball struct {
	// datagram sent via SendUDPSessionDatagram
	recvData chan []byte
	// responses sent via SendUDPSessionResponse
	recvResp chan struct {
		id   v3.RequestID
		resp v3.SessionRegistrationResp
	}
}

func newMockEyeball() mockEyeball {
	return mockEyeball{
		recvData: make(chan []byte, 1),
		recvResp: make(chan struct {
			id   v3.RequestID
			resp v3.SessionRegistrationResp
		}, 1),
	}
}

func (m *mockEyeball) SendUDPSessionDatagram(datagram []byte) error {
	b := make([]byte, len(datagram))
	copy(b, datagram)
	m.recvData <- b
	return nil
}

func (m *mockEyeball) SendUDPSessionResponse(id v3.RequestID, resp v3.SessionRegistrationResp) error {
	m.recvResp <- struct {
		id   v3.RequestID
		resp v3.SessionRegistrationResp
	}{
		id, resp,
	}
	return nil
}
