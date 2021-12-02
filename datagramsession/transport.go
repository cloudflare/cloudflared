package datagramsession

import "github.com/google/uuid"

// Transport is a connection between cloudflared and edge that can multiplex datagrams from multiple sessions
type transport interface {
	// SendTo writes payload for a session to the transport
	SendTo(sessionID uuid.UUID, payload []byte) error
	// ReceiveFrom reads the next datagram from the transport
	ReceiveFrom() (uuid.UUID, []byte, error)
	// Max transmission unit of the transport
	MTU() uint
}
