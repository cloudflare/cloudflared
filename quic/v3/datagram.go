package v3

import (
	"encoding/binary"
	"net/netip"
	"time"
)

type DatagramType byte

const (
	// UDP Registration
	UDPSessionRegistrationType DatagramType = 0x0
	// UDP Session Payload
	UDPSessionPayloadType DatagramType = 0x1
	// DatagramTypeICMP (supporting both ICMPv4 and ICMPv6)
	ICMPType DatagramType = 0x2
	// UDP Session Registration Response
	UDPSessionRegistrationResponseType DatagramType = 0x3
)

const (
	// Total number of bytes representing the [DatagramType]
	datagramTypeLen = 1

	// 1280 is the default datagram packet length used before MTU discovery: https://github.com/quic-go/quic-go/blob/v0.45.0/internal/protocol/params.go#L12
	maxDatagramPayloadLen = 1280
)

func ParseDatagramType(data []byte) (DatagramType, error) {
	if len(data) < datagramTypeLen {
		return 0, ErrDatagramHeaderTooSmall
	}
	return DatagramType(data[0]), nil
}

// UDPSessionRegistrationDatagram handles a request to initialize a UDP session on the remote client.
type UDPSessionRegistrationDatagram struct {
	RequestID        RequestID
	Dest             netip.AddrPort
	Traced           bool
	IdleDurationHint time.Duration
	Payload          []byte
}

const (
	sessionRegistrationFlagsIPMask      byte = 0b0000_0001
	sessionRegistrationFlagsTracedMask  byte = 0b0000_0010
	sessionRegistrationFlagsBundledMask byte = 0b0000_0100

	sessionRegistrationIPv4DatagramHeaderLen = datagramTypeLen +
		1 + // Flag length
		2 + // Destination port length
		2 + // Idle duration seconds length
		datagramRequestIdLen + // Request ID length
		4 // IPv4 address length

	// The IPv4 and IPv6 address share space, so adding 12 to the header length gets the space taken by the IPv6 field.
	sessionRegistrationIPv6DatagramHeaderLen = sessionRegistrationIPv4DatagramHeaderLen + 12
)

// The datagram structure for UDPSessionRegistrationDatagram is:
//
//   0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//  0|      Type     |     Flags     |       Destination Port        |
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//  4|     Idle Duration Seconds     |                               |
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+                               +
//  8|                                                               |
//   +                       Session Identifier                      +
// 12|                           (16 Bytes)                          |
//   +                                                               +
// 16|                                                               |
//   +                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 20|                               |   Destination IPv4 Address    |
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+- - - - - - - - - - - - - - - -+
// 24| Destination IPv4 Address cont |                               |
//   +- - - - - - - - - - - - - - - -                                +
// 28|                   Destination IPv6 Address                    |
//   +                  (extension of IPv4 region)                   +
// 32|                                                               |
//   +                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 36|                               |                               |
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+                               +
//   .                                                               .
//   .                         Bundle Payload                        .
//   .                                                               .
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

func (s *UDPSessionRegistrationDatagram) MarshalBinary() (data []byte, err error) {
	ipv6 := s.Dest.Addr().Is6()
	var flags byte
	if s.Traced {
		flags |= sessionRegistrationFlagsTracedMask
	}
	hasPayload := len(s.Payload) > 0
	if hasPayload {
		flags |= sessionRegistrationFlagsBundledMask
	}
	var maxPayloadLen int
	if ipv6 {
		maxPayloadLen = maxDatagramPayloadLen + sessionRegistrationIPv6DatagramHeaderLen
		flags |= sessionRegistrationFlagsIPMask
	} else {
		maxPayloadLen = maxDatagramPayloadLen + sessionRegistrationIPv4DatagramHeaderLen
	}
	// Make sure that the payload being bundled can actually fit in the payload destination
	if len(s.Payload) > maxPayloadLen {
		return nil, wrapMarshalErr(ErrDatagramPayloadTooLarge)
	}
	// Allocate the buffer with the right size for the destination IP family
	if ipv6 {
		data = make([]byte, sessionRegistrationIPv6DatagramHeaderLen+len(s.Payload))
	} else {
		data = make([]byte, sessionRegistrationIPv4DatagramHeaderLen+len(s.Payload))
	}
	data[0] = byte(UDPSessionRegistrationType)
	data[1] = flags
	binary.BigEndian.PutUint16(data[2:4], s.Dest.Port())
	binary.BigEndian.PutUint16(data[4:6], uint16(s.IdleDurationHint.Seconds()))
	err = s.RequestID.MarshalBinaryTo(data[6:22])
	if err != nil {
		return nil, wrapMarshalErr(err)
	}
	var end int
	if ipv6 {
		copy(data[22:38], s.Dest.Addr().AsSlice())
		end = 38
	} else {
		copy(data[22:26], s.Dest.Addr().AsSlice())
		end = 26
	}

	if hasPayload {
		copy(data[end:], s.Payload)
	}

	return data, nil
}

func (s *UDPSessionRegistrationDatagram) UnmarshalBinary(data []byte) error {
	datagramType, err := ParseDatagramType(data)
	if err != nil {
		return err
	}
	if datagramType != UDPSessionRegistrationType {
		return wrapUnmarshalErr(ErrInvalidDatagramType)
	}

	requestID, err := RequestIDFromSlice(data[6:22])
	if err != nil {
		return wrapUnmarshalErr(err)
	}

	traced := (data[1] & sessionRegistrationFlagsTracedMask) == sessionRegistrationFlagsTracedMask
	bundled := (data[1] & sessionRegistrationFlagsBundledMask) == sessionRegistrationFlagsBundledMask
	ipv6 := (data[1] & sessionRegistrationFlagsIPMask) == sessionRegistrationFlagsIPMask

	port := binary.BigEndian.Uint16(data[2:4])
	var datagramHeaderSize int
	var dest netip.AddrPort
	if ipv6 {
		datagramHeaderSize = sessionRegistrationIPv6DatagramHeaderLen
		dest = netip.AddrPortFrom(netip.AddrFrom16([16]byte(data[22:38])), port)
	} else {
		datagramHeaderSize = sessionRegistrationIPv4DatagramHeaderLen
		dest = netip.AddrPortFrom(netip.AddrFrom4([4]byte(data[22:26])), port)
	}

	idle := time.Duration(binary.BigEndian.Uint16(data[4:6])) * time.Second

	var payload []byte
	if bundled && len(data) >= datagramHeaderSize && len(data[datagramHeaderSize:]) > 0 {
		payload = data[datagramHeaderSize:]
	}

	*s = UDPSessionRegistrationDatagram{
		RequestID:        requestID,
		Dest:             dest,
		Traced:           traced,
		IdleDurationHint: idle,
		Payload:          payload,
	}
	return nil
}

// UDPSessionPayloadDatagram provides the payload for a session to be send to either the origin or the client.
type UDPSessionPayloadDatagram struct {
	RequestID RequestID
	Payload   []byte
}

const (
	DatagramPayloadHeaderLen = datagramTypeLen + datagramRequestIdLen

	// The maximum size that a proxied UDP payload can be in a [UDPSessionPayloadDatagram]
	maxPayloadPlusHeaderLen = maxDatagramPayloadLen + DatagramPayloadHeaderLen
)

// The datagram structure for UDPSessionPayloadDatagram is:
//
//   0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//  0|      Type     |                                               |
//   +-+-+-+-+-+-+-+-+                                               +
//  4|                                                               |
//   +                                                               +
//  8|                      Session Identifier                       |
//   +                           (16 Bytes)                          +
// 12|                                                               |
//   +                                               +-+-+-+-+-+-+-+-+
// 16|                                               |               |
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+               +
//   .                                                               .
//   .                             Payload                           .
//   .                                                               .
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

// MarshalPayloadHeaderTo provides a way to insert the Session Payload header into an already existing byte slice
// without having to allocate and copy the payload into the destination.
//
// This method should be used in-place of MarshalBinary which will allocate in-place the required byte array to return.
func MarshalPayloadHeaderTo(requestID RequestID, payload []byte) error {
	if len(payload) < DatagramPayloadHeaderLen {
		return wrapMarshalErr(ErrDatagramPayloadHeaderTooSmall)
	}
	payload[0] = byte(UDPSessionPayloadType)
	return requestID.MarshalBinaryTo(payload[1:DatagramPayloadHeaderLen])
}

func (s *UDPSessionPayloadDatagram) UnmarshalBinary(data []byte) error {
	datagramType, err := ParseDatagramType(data)
	if err != nil {
		return err
	}
	if datagramType != UDPSessionPayloadType {
		return wrapUnmarshalErr(ErrInvalidDatagramType)
	}

	// Make sure that the slice provided is the right size to be parsed.
	if len(data) < DatagramPayloadHeaderLen || len(data) > maxPayloadPlusHeaderLen {
		return wrapUnmarshalErr(ErrDatagramPayloadInvalidSize)
	}

	requestID, err := RequestIDFromSlice(data[1:DatagramPayloadHeaderLen])
	if err != nil {
		return wrapUnmarshalErr(err)
	}

	*s = UDPSessionPayloadDatagram{
		RequestID: requestID,
		Payload:   data[DatagramPayloadHeaderLen:],
	}
	return nil
}

// UDPSessionRegistrationResponseDatagram is used to either return a successful registration or error to the client
// that requested the registration of a UDP session.
type UDPSessionRegistrationResponseDatagram struct {
	RequestID    RequestID
	ResponseType SessionRegistrationResp
	ErrorMsg     string
}

const (
	datagramRespTypeLen   = 1
	datagramRespErrMsgLen = 2

	datagramSessionRegistrationResponseLen = datagramTypeLen + datagramRespTypeLen + datagramRequestIdLen + datagramRespErrMsgLen

	// The maximum size that an error message can be in a [UDPSessionRegistrationResponseDatagram].
	maxResponseErrorMessageLen = maxDatagramPayloadLen - datagramSessionRegistrationResponseLen
)

// SessionRegistrationResp represents all of the responses that a UDP session registration response
// can return back to the client.
type SessionRegistrationResp byte

const (
	// Session was received and is ready to proxy.
	ResponseOk SessionRegistrationResp = 0x00
	// Session registration was unable to reach the requested origin destination.
	ResponseDestinationUnreachable SessionRegistrationResp = 0x01
	// Session registration was unable to bind to a local UDP socket.
	ResponseUnableToBindSocket SessionRegistrationResp = 0x02
	// Session registration failed due to the number of flows being higher than the limit.
	ResponseTooManyActiveFlows SessionRegistrationResp = 0x03
	// Session registration failed with an unexpected error but provided a message.
	ResponseErrorWithMsg SessionRegistrationResp = 0xff
)

// The datagram structure for UDPSessionRegistrationResponseDatagram is:
//
//   0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//  0|      Type     |   Resp Type   |                               |
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+                               +
//  4|                                                               |
//   +                       Session Identifier                      +
//  8|                           (16 Bytes)                          |
//   +                                                               +
// 12|                                                               |
//   +                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 16|                               |          Error Length         |
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//   .                                                               .
//   .                                                               .
//   .                                                               .
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

func (s *UDPSessionRegistrationResponseDatagram) MarshalBinary() (data []byte, err error) {
	if len(s.ErrorMsg) > maxResponseErrorMessageLen {
		return nil, wrapMarshalErr(ErrDatagramResponseMsgInvalidSize)
	}
	// nolint: gosec
	errMsgLen := uint16(len(s.ErrorMsg))

	data = make([]byte, datagramSessionRegistrationResponseLen+errMsgLen)
	data[0] = byte(UDPSessionRegistrationResponseType)
	data[1] = byte(s.ResponseType)
	err = s.RequestID.MarshalBinaryTo(data[2:18])
	if err != nil {
		return nil, wrapMarshalErr(err)
	}

	if errMsgLen > 0 {
		binary.BigEndian.PutUint16(data[18:20], errMsgLen)
		copy(data[20:], []byte(s.ErrorMsg))
	}

	return data, nil
}

func (s *UDPSessionRegistrationResponseDatagram) UnmarshalBinary(data []byte) error {
	datagramType, err := ParseDatagramType(data)
	if err != nil {
		return wrapUnmarshalErr(err)
	}
	if datagramType != UDPSessionRegistrationResponseType {
		return wrapUnmarshalErr(ErrInvalidDatagramType)
	}

	if len(data) < datagramSessionRegistrationResponseLen {
		return wrapUnmarshalErr(ErrDatagramResponseInvalidSize)
	}

	respType := SessionRegistrationResp(data[1])

	requestID, err := RequestIDFromSlice(data[2:18])
	if err != nil {
		return wrapUnmarshalErr(err)
	}

	errMsgLen := binary.BigEndian.Uint16(data[18:20])
	if errMsgLen > maxResponseErrorMessageLen {
		return wrapUnmarshalErr(ErrDatagramResponseMsgTooLargeMaximum)
	}

	if len(data[20:]) < int(errMsgLen) {
		return wrapUnmarshalErr(ErrDatagramResponseMsgTooLargeDatagram)
	}

	var errMsg string
	if errMsgLen > 0 {
		errMsg = string(data[20:])
	}

	*s = UDPSessionRegistrationResponseDatagram{
		RequestID:    requestID,
		ResponseType: respType,
		ErrorMsg:     errMsg,
	}
	return nil
}

// ICMPDatagram is used to propagate ICMPv4 and ICMPv6 payloads.
type ICMPDatagram struct {
	Payload []byte
}

// The maximum size that an ICMP packet can be.
const maxICMPPayloadLen = maxDatagramPayloadLen

// The datagram structure for ICMPDatagram is:
//
//   0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//  0|      Type     |                                               |
//   +-+-+-+-+-+-+-+-+                                               +
//   .                          Payload                              .
//   .                                                               .
//   .                                                               .
//   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

func (d *ICMPDatagram) MarshalBinary() (data []byte, err error) {
	if len(d.Payload) > maxICMPPayloadLen {
		return nil, wrapMarshalErr(ErrDatagramICMPPayloadTooLarge)
	}
	// We shouldn't attempt to marshal an ICMP datagram with no ICMP payload provided
	if len(d.Payload) == 0 {
		return nil, wrapMarshalErr(ErrDatagramICMPPayloadMissing)
	}
	// Make room for the 1 byte ICMPType header
	datagram := make([]byte, len(d.Payload)+datagramTypeLen)
	datagram[0] = byte(ICMPType)
	copy(datagram[1:], d.Payload)
	return datagram, nil
}

func (d *ICMPDatagram) UnmarshalBinary(data []byte) error {
	datagramType, err := ParseDatagramType(data)
	if err != nil {
		return wrapUnmarshalErr(err)
	}
	if datagramType != ICMPType {
		return wrapUnmarshalErr(ErrInvalidDatagramType)
	}

	if len(data[1:]) > maxDatagramPayloadLen {
		return wrapUnmarshalErr(ErrDatagramICMPPayloadTooLarge)
	}

	// We shouldn't attempt to unmarshal an ICMP datagram with no ICMP payload provided
	if len(data[1:]) == 0 {
		return wrapUnmarshalErr(ErrDatagramICMPPayloadMissing)
	}

	payload := make([]byte, len(data[1:]))
	copy(payload, data[1:])
	d.Payload = payload
	return nil
}
