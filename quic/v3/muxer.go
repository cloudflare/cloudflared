package v3

// DatagramWriter provides the Muxer interface to create proper Datagrams when sending over a connection.
type DatagramWriter interface {
	SendUDPSessionDatagram(datagram []byte) error
	SendUDPSessionResponse(id RequestID, resp SessionRegistrationResp) error
	//SendICMPPacket(packet packet.IP) error
}
