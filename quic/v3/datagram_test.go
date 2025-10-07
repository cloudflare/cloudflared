package v3_test

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	v3 "github.com/cloudflare/cloudflared/quic/v3"
)

func makePayload(size int) []byte {
	payload := make([]byte, size)
	_, _ = rand.Read(payload)
	return payload
}

func makePayloads(size int, count int) [][]byte {
	payloads := make([][]byte, count)
	for i := range payloads {
		payloads[i] = makePayload(size)
	}
	return payloads
}

func TestSessionRegistration_MarshalUnmarshal(t *testing.T) {
	payload := makePayload(1280)
	tests := []*v3.UDPSessionRegistrationDatagram{
		// Default (IPv4)
		{
			RequestID:        testRequestID,
			Dest:             netip.MustParseAddrPort("1.1.1.1:8080"),
			Traced:           false,
			IdleDurationHint: 5 * time.Second,
			Payload:          nil,
		},
		// Request ID (max)
		{
			RequestID: mustRequestID([16]byte{
				^uint8(0), ^uint8(0), ^uint8(0), ^uint8(0),
				^uint8(0), ^uint8(0), ^uint8(0), ^uint8(0),
				^uint8(0), ^uint8(0), ^uint8(0), ^uint8(0),
				^uint8(0), ^uint8(0), ^uint8(0), ^uint8(0),
			}),
			Dest:             netip.MustParseAddrPort("1.1.1.1:8080"),
			Traced:           false,
			IdleDurationHint: 5 * time.Second,
			Payload:          nil,
		},
		// IPv6
		{
			RequestID:        testRequestID,
			Dest:             netip.MustParseAddrPort("[fc00::0]:8080"),
			Traced:           false,
			IdleDurationHint: 5 * time.Second,
			Payload:          nil,
		},
		// Traced
		{
			RequestID:        testRequestID,
			Dest:             netip.MustParseAddrPort("1.1.1.1:8080"),
			Traced:           true,
			IdleDurationHint: 5 * time.Second,
			Payload:          nil,
		},
		// IdleDurationHint (max)
		{
			RequestID:        testRequestID,
			Dest:             netip.MustParseAddrPort("1.1.1.1:8080"),
			Traced:           false,
			IdleDurationHint: 65535 * time.Second,
			Payload:          nil,
		},
		// Payload
		{
			RequestID:        testRequestID,
			Dest:             netip.MustParseAddrPort("1.1.1.1:8080"),
			Traced:           false,
			IdleDurationHint: 5 * time.Second,
			Payload:          []byte{0xff, 0xaa, 0xcc, 0x44},
		},
		// Payload (max: 1254) for IPv4
		{
			RequestID:        testRequestID,
			Dest:             netip.MustParseAddrPort("1.1.1.1:8080"),
			Traced:           false,
			IdleDurationHint: 5 * time.Second,
			Payload:          payload,
		},
		// Payload (max: 1242) for IPv4
		{
			RequestID:        testRequestID,
			Dest:             netip.MustParseAddrPort("1.1.1.1:8080"),
			Traced:           false,
			IdleDurationHint: 5 * time.Second,
			Payload:          payload[:1242],
		},
	}
	for _, tt := range tests {
		marshaled, err := tt.MarshalBinary()
		if err != nil {
			t.Error(err)
		}
		unmarshaled := v3.UDPSessionRegistrationDatagram{}
		err = unmarshaled.UnmarshalBinary(marshaled)
		if err != nil {
			t.Error(err)
		}
		if !compareRegistrationDatagrams(t, tt, &unmarshaled) {
			t.Errorf("not equal:\n%+v\n%+v", tt, &unmarshaled)
		}
	}
}

func TestSessionRegistration_MarshalBinary(t *testing.T) {
	t.Run("idle hint too large", func(t *testing.T) {
		// idle hint duration overflows back to 1
		datagram := &v3.UDPSessionRegistrationDatagram{
			RequestID:        testRequestID,
			Dest:             netip.MustParseAddrPort("1.1.1.1:8080"),
			Traced:           false,
			IdleDurationHint: 65537 * time.Second,
			Payload:          nil,
		}
		expected := &v3.UDPSessionRegistrationDatagram{
			RequestID:        testRequestID,
			Dest:             netip.MustParseAddrPort("1.1.1.1:8080"),
			Traced:           false,
			IdleDurationHint: 1 * time.Second,
			Payload:          nil,
		}
		marshaled, err := datagram.MarshalBinary()
		if err != nil {
			t.Error(err)
		}
		unmarshaled := v3.UDPSessionRegistrationDatagram{}
		err = unmarshaled.UnmarshalBinary(marshaled)
		if err != nil {
			t.Error(err)
		}
		if !compareRegistrationDatagrams(t, expected, &unmarshaled) {
			t.Errorf("not equal:\n%+v\n%+v", expected, &unmarshaled)
		}
	})
}

func TestTypeUnmarshalErrors(t *testing.T) {
	t.Run("invalid length", func(t *testing.T) {
		d1 := v3.UDPSessionRegistrationDatagram{}
		err := d1.UnmarshalBinary([]byte{})
		if !errors.Is(err, v3.ErrDatagramHeaderTooSmall) {
			t.Errorf("expected invalid length to throw error")
		}

		d2 := v3.UDPSessionPayloadDatagram{}
		err = d2.UnmarshalBinary([]byte{})
		if !errors.Is(err, v3.ErrDatagramHeaderTooSmall) {
			t.Errorf("expected invalid length to throw error")
		}

		d3 := v3.UDPSessionRegistrationResponseDatagram{}
		err = d3.UnmarshalBinary([]byte{})
		if !errors.Is(err, v3.ErrDatagramHeaderTooSmall) {
			t.Errorf("expected invalid length to throw error")
		}

		d4 := v3.ICMPDatagram{}
		err = d4.UnmarshalBinary([]byte{})
		if !errors.Is(err, v3.ErrDatagramHeaderTooSmall) {
			t.Errorf("expected invalid length to throw error")
		}
	})

	t.Run("invalid types", func(t *testing.T) {
		d1 := v3.UDPSessionRegistrationDatagram{}
		err := d1.UnmarshalBinary([]byte{byte(v3.UDPSessionRegistrationResponseType)})
		if !errors.Is(err, v3.ErrInvalidDatagramType) {
			t.Errorf("expected invalid type to throw error")
		}

		d2 := v3.UDPSessionPayloadDatagram{}
		err = d2.UnmarshalBinary([]byte{byte(v3.UDPSessionRegistrationType)})
		if !errors.Is(err, v3.ErrInvalidDatagramType) {
			t.Errorf("expected invalid type to throw error")
		}

		d3 := v3.UDPSessionRegistrationResponseDatagram{}
		err = d3.UnmarshalBinary([]byte{byte(v3.UDPSessionPayloadType)})
		if !errors.Is(err, v3.ErrInvalidDatagramType) {
			t.Errorf("expected invalid type to throw error")
		}

		d4 := v3.ICMPDatagram{}
		err = d4.UnmarshalBinary([]byte{byte(v3.UDPSessionPayloadType)})
		if !errors.Is(err, v3.ErrInvalidDatagramType) {
			t.Errorf("expected invalid type to throw error")
		}
	})
}

func TestSessionPayload(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		payload := makePayload(128)
		err := v3.MarshalPayloadHeaderTo(testRequestID, payload[0:17])
		if err != nil {
			t.Error(err)
		}
		unmarshaled := v3.UDPSessionPayloadDatagram{}
		err = unmarshaled.UnmarshalBinary(payload)
		if err != nil {
			t.Error(err)
		}
		require.Equal(t, testRequestID, unmarshaled.RequestID)
		require.Equal(t, payload[17:], unmarshaled.Payload)
	})

	t.Run("empty", func(t *testing.T) {
		payload := makePayload(17)
		err := v3.MarshalPayloadHeaderTo(testRequestID, payload)
		if err != nil {
			t.Error(err)
		}
		unmarshaled := v3.UDPSessionPayloadDatagram{}
		err = unmarshaled.UnmarshalBinary(payload)
		if err != nil {
			t.Error(err)
		}
		require.Equal(t, testRequestID, unmarshaled.RequestID)
		require.Equal(t, payload[17:], unmarshaled.Payload)
	})

	t.Run("header size too small", func(t *testing.T) {
		payload := makePayload(16)
		err := v3.MarshalPayloadHeaderTo(testRequestID, payload)
		if !errors.Is(err, v3.ErrDatagramPayloadHeaderTooSmall) {
			t.Errorf("expected an error")
		}
	})

	t.Run("payload size too small", func(t *testing.T) {
		payload := makePayload(17)
		err := v3.MarshalPayloadHeaderTo(testRequestID, payload)
		if err != nil {
			t.Error(err)
		}
		unmarshaled := v3.UDPSessionPayloadDatagram{}
		err = unmarshaled.UnmarshalBinary(payload[:16])
		if !errors.Is(err, v3.ErrDatagramPayloadInvalidSize) {
			t.Errorf("expected an error: %s", err)
		}
	})

	t.Run("payload size too large", func(t *testing.T) {
		datagram := makePayload(17 + 1281) // 1280 is the largest payload size allowed
		err := v3.MarshalPayloadHeaderTo(testRequestID, datagram)
		if err != nil {
			t.Error(err)
		}
		unmarshaled := v3.UDPSessionPayloadDatagram{}
		err = unmarshaled.UnmarshalBinary(datagram[:])
		if !errors.Is(err, v3.ErrDatagramPayloadInvalidSize) {
			t.Errorf("expected an error: %s", err)
		}
	})
}

func TestSessionRegistrationResponse(t *testing.T) {
	validRespTypes := []v3.SessionRegistrationResp{
		v3.ResponseOk,
		v3.ResponseDestinationUnreachable,
		v3.ResponseUnableToBindSocket,
		v3.ResponseErrorWithMsg,
	}
	t.Run("basic", func(t *testing.T) {
		for _, responseType := range validRespTypes {
			datagram := &v3.UDPSessionRegistrationResponseDatagram{
				RequestID:    testRequestID,
				ResponseType: responseType,
				ErrorMsg:     "test",
			}
			marshaled, err := datagram.MarshalBinary()
			if err != nil {
				t.Error(err)
			}
			unmarshaled := &v3.UDPSessionRegistrationResponseDatagram{}
			err = unmarshaled.UnmarshalBinary(marshaled)
			if err != nil {
				t.Error(err)
			}
			require.Equal(t, datagram, unmarshaled)
		}
	})

	t.Run("unsupported resp type is valid", func(t *testing.T) {
		datagram := &v3.UDPSessionRegistrationResponseDatagram{
			RequestID:    testRequestID,
			ResponseType: v3.SessionRegistrationResp(0xfc),
			ErrorMsg:     "",
		}
		marshaled, err := datagram.MarshalBinary()
		if err != nil {
			t.Error(err)
		}
		unmarshaled := &v3.UDPSessionRegistrationResponseDatagram{}
		err = unmarshaled.UnmarshalBinary(marshaled)
		if err != nil {
			t.Error(err)
		}
		require.Equal(t, datagram, unmarshaled)
	})

	t.Run("too small to unmarshal", func(t *testing.T) {
		payload := makePayload(17)
		payload[0] = byte(v3.UDPSessionRegistrationResponseType)
		unmarshaled := &v3.UDPSessionRegistrationResponseDatagram{}
		err := unmarshaled.UnmarshalBinary(payload)
		if !errors.Is(err, v3.ErrDatagramResponseInvalidSize) {
			t.Errorf("expected an error")
		}
	})

	t.Run("error message too long", func(t *testing.T) {
		message := ""
		for i := 0; i < 1280; i++ {
			message += "a"
		}
		datagram := &v3.UDPSessionRegistrationResponseDatagram{
			RequestID:    testRequestID,
			ResponseType: v3.SessionRegistrationResp(0xfc),
			ErrorMsg:     message,
		}
		_, err := datagram.MarshalBinary()
		if !errors.Is(err, v3.ErrDatagramResponseMsgInvalidSize) {
			t.Errorf("expected an error")
		}
	})

	t.Run("error message too large to unmarshal", func(t *testing.T) {
		payload := makePayload(1280)
		payload[0] = byte(v3.UDPSessionRegistrationResponseType)
		binary.BigEndian.PutUint16(payload[18:20], 1280) // larger than the datagram size could be
		unmarshaled := &v3.UDPSessionRegistrationResponseDatagram{}
		err := unmarshaled.UnmarshalBinary(payload)
		if !errors.Is(err, v3.ErrDatagramResponseMsgTooLargeMaximum) {
			t.Errorf("expected an error: %v", err)
		}
	})

	t.Run("error message larger than provided buffer", func(t *testing.T) {
		payload := makePayload(1000)
		payload[0] = byte(v3.UDPSessionRegistrationResponseType)
		binary.BigEndian.PutUint16(payload[18:20], 1001) // larger than the datagram size provided
		unmarshaled := &v3.UDPSessionRegistrationResponseDatagram{}
		err := unmarshaled.UnmarshalBinary(payload)
		if !errors.Is(err, v3.ErrDatagramResponseMsgTooLargeDatagram) {
			t.Errorf("expected an error: %v", err)
		}
	})
}

func TestICMPDatagram(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		payload := makePayload(128)
		datagram := v3.ICMPDatagram{Payload: payload}
		marshaled, err := datagram.MarshalBinary()
		if err != nil {
			t.Error(err)
		}
		unmarshaled := &v3.ICMPDatagram{}
		err = unmarshaled.UnmarshalBinary(marshaled)
		if err != nil {
			t.Error(err)
		}
		require.Equal(t, payload, unmarshaled.Payload)
	})

	t.Run("payload size empty", func(t *testing.T) {
		payload := []byte{}
		datagram := v3.ICMPDatagram{Payload: payload}
		_, err := datagram.MarshalBinary()
		if !errors.Is(err, v3.ErrDatagramICMPPayloadMissing) {
			t.Errorf("expected an error: %s", err)
		}
		payload = []byte{byte(v3.ICMPType)}
		unmarshaled := &v3.ICMPDatagram{}
		err = unmarshaled.UnmarshalBinary(payload)
		if !errors.Is(err, v3.ErrDatagramICMPPayloadMissing) {
			t.Errorf("expected an error: %s", err)
		}
	})

	t.Run("payload size too large", func(t *testing.T) {
		payload := makePayload(1280 + 1) // larger than the datagram size could be
		datagram := v3.ICMPDatagram{Payload: payload}
		_, err := datagram.MarshalBinary()
		if !errors.Is(err, v3.ErrDatagramICMPPayloadTooLarge) {
			t.Errorf("expected an error: %s", err)
		}
		payload = makePayload(1280 + 2) // larger than the datagram size could be + header
		payload[0] = byte(v3.ICMPType)
		unmarshaled := &v3.ICMPDatagram{}
		err = unmarshaled.UnmarshalBinary(payload)
		if !errors.Is(err, v3.ErrDatagramICMPPayloadTooLarge) {
			t.Errorf("expected an error: %s", err)
		}
	})
}

func compareRegistrationDatagrams(t *testing.T, l *v3.UDPSessionRegistrationDatagram, r *v3.UDPSessionRegistrationDatagram) bool {
	require.Equal(t, l.Payload, r.Payload)
	return l.RequestID == r.RequestID &&
		l.Dest == r.Dest &&
		l.IdleDurationHint == r.IdleDurationHint &&
		l.Traced == r.Traced
}

func FuzzRegistrationDatagram(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		unmarshaled := v3.UDPSessionRegistrationDatagram{}
		err := unmarshaled.UnmarshalBinary(data)
		if err == nil {
			_, _ = unmarshaled.MarshalBinary()
		}
	})
}

func FuzzPayloadDatagram(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		unmarshaled := v3.UDPSessionPayloadDatagram{}
		_ = unmarshaled.UnmarshalBinary(data)
	})
}

func FuzzRegistrationResponseDatagram(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		unmarshaled := v3.UDPSessionRegistrationResponseDatagram{}
		err := unmarshaled.UnmarshalBinary(data)
		if err == nil {
			_, _ = unmarshaled.MarshalBinary()
		}
	})
}

func FuzzICMPDatagram(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		unmarshaled := v3.ICMPDatagram{}
		err := unmarshaled.UnmarshalBinary(data)
		if err == nil {
			_, _ = unmarshaled.MarshalBinary()
		}
	})
}
