package quic

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/netip"
	"testing"
	"time"

	"github.com/google/gopacket/layers"
	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/packet"
	"github.com/cloudflare/cloudflared/tracing"
)

var (
	testSessionID = uuid.New()
)

func TestSuffixThenRemoveSessionID(t *testing.T) {
	msg := []byte(t.Name())
	msgWithID, err := SuffixSessionID(testSessionID, msg)
	require.NoError(t, err)
	require.Len(t, msgWithID, len(msg)+sessionIDLen)

	sessionID, msgWithoutID, err := extractSessionID(msgWithID)
	require.NoError(t, err)
	require.Equal(t, msg, msgWithoutID)
	require.Equal(t, testSessionID, sessionID)
}

func TestRemoveSessionIDError(t *testing.T) {
	// message is too short to contain session ID
	msg := []byte("test")
	_, _, err := extractSessionID(msg)
	require.Error(t, err)
}

func TestSuffixSessionIDError(t *testing.T) {
	msg := make([]byte, MaxDatagramFrameSize-sessionIDLen)
	_, err := SuffixSessionID(testSessionID, msg)
	require.NoError(t, err)

	msg = make([]byte, MaxDatagramFrameSize-sessionIDLen+1)
	_, err = SuffixSessionID(testSessionID, msg)
	require.Error(t, err)
}

func TestDatagram(t *testing.T) {
	maxPayload := make([]byte, maxDatagramPayloadSize)
	noPayloadSession := uuid.New()
	maxPayloadSession := uuid.New()
	sessionToPayload := []*packet.Session{
		{
			ID:      noPayloadSession,
			Payload: make([]byte, 0),
		},
		{
			ID:      maxPayloadSession,
			Payload: maxPayload,
		},
	}

	packets := []packet.ICMP{
		{
			IP: &packet.IP{
				Src:      netip.MustParseAddr("172.16.0.1"),
				Dst:      netip.MustParseAddr("192.168.0.1"),
				Protocol: layers.IPProtocolICMPv4,
			},
			Message: &icmp.Message{
				Type: ipv4.ICMPTypeTimeExceeded,
				Code: 0,
				Body: &icmp.TimeExceeded{
					Data: []byte("original packet"),
				},
			},
		},
		{
			IP: &packet.IP{
				Src:      netip.MustParseAddr("172.16.0.2"),
				Dst:      netip.MustParseAddr("192.168.0.2"),
				Protocol: layers.IPProtocolICMPv4,
			},
			Message: &icmp.Message{
				Type: ipv4.ICMPTypeEcho,
				Code: 0,
				Body: &icmp.Echo{
					ID:   6182,
					Seq:  9151,
					Data: []byte("Test ICMP echo"),
				},
			},
		},
	}

	testDatagram(t, 1, sessionToPayload, nil)
	testDatagram(t, 2, sessionToPayload, packets)
}

func testDatagram(t *testing.T, version uint8, sessionToPayloads []*packet.Session, packets []packet.ICMP) {
	quicConfig := &quic.Config{
		KeepAlivePeriod:      5 * time.Millisecond,
		EnableDatagrams:      true,
		MaxDatagramFrameSize: MaxDatagramFrameSize,
	}
	quicListener := newQUICListener(t, quicConfig)
	defer quicListener.Close()

	logger := zerolog.Nop()

	tracingIdentity, err := tracing.NewIdentity("ec31ad8a01fde11fdcabe2efdce36873:52726f6cabc144f5:0:1")
	require.NoError(t, err)
	serializedTracingID, err := tracingIdentity.MarshalBinary()
	require.NoError(t, err)
	tracingSpan := &TracingSpanPacket{
		Spans:           []byte("tracing"),
		TracingIdentity: serializedTracingID,
	}

	errGroup, ctx := errgroup.WithContext(context.Background())
	// Run edge side of datagram muxer
	errGroup.Go(func() error {
		// Accept quic connection
		quicSession, err := quicListener.Accept(ctx)
		if err != nil {
			return err
		}

		sessionDemuxChan := make(chan *packet.Session, 16)

		switch version {
		case 1:
			muxer := NewDatagramMuxer(quicSession, &logger, sessionDemuxChan)
			muxer.ServeReceive(ctx)
		case 2:
			muxer := NewDatagramMuxerV2(quicSession, &logger, sessionDemuxChan)
			muxer.ServeReceive(ctx)

			for _, pk := range packets {
				received, err := muxer.ReceivePacket(ctx)
				require.NoError(t, err)
				validateIPPacket(t, received, &pk)
				received, err = muxer.ReceivePacket(ctx)
				require.NoError(t, err)
				validateIPPacketWithTracing(t, received, &pk, serializedTracingID)
			}
			received, err := muxer.ReceivePacket(ctx)
			require.NoError(t, err)
			validateTracingSpans(t, received, tracingSpan)
		default:
			return fmt.Errorf("unknown datagram version %d", version)
		}

		for _, expectedPayload := range sessionToPayloads {
			actualPayload := <-sessionDemuxChan
			require.Equal(t, expectedPayload, actualPayload)
		}
		return nil
	})

	largePayload := make([]byte, MaxDatagramFrameSize)
	// Run cloudflared side of datagram muxer
	errGroup.Go(func() error {
		tlsClientConfig := &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"argotunnel"},
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// Establish quic connection
		quicSession, err := quic.DialAddrEarly(ctx, quicListener.Addr().String(), tlsClientConfig, quicConfig)
		require.NoError(t, err)
		defer quicSession.CloseWithError(0, "")

		// Wait a few milliseconds for MTU discovery to take place
		time.Sleep(time.Millisecond * 100)

		var muxer BaseDatagramMuxer
		switch version {
		case 1:
			muxer = NewDatagramMuxer(quicSession, &logger, nil)
		case 2:
			muxerV2 := NewDatagramMuxerV2(quicSession, &logger, nil)
			encoder := packet.NewEncoder()
			for _, pk := range packets {
				encodedPacket, err := encoder.Encode(&pk)
				require.NoError(t, err)
				require.NoError(t, muxerV2.SendPacket(RawPacket(encodedPacket)))
				require.NoError(t, muxerV2.SendPacket(&TracedPacket{
					Packet:          encodedPacket,
					TracingIdentity: serializedTracingID,
				}))
			}
			require.NoError(t, muxerV2.SendPacket(tracingSpan))
			// Payload larger than transport MTU, should not be sent
			require.Error(t, muxerV2.SendPacket(RawPacket{
				Data: largePayload,
			}))
			muxer = muxerV2
		default:
			return fmt.Errorf("unknown datagram version %d", version)
		}

		for _, session := range sessionToPayloads {
			require.NoError(t, muxer.SendToSession(session))
		}
		// Payload larger than transport MTU, should not be sent
		require.Error(t, muxer.SendToSession(&packet.Session{
			ID:      testSessionID,
			Payload: largePayload,
		}))

		// Wait for edge to finish receiving the messages
		time.Sleep(time.Millisecond * 100)

		return nil
	})

	require.NoError(t, errGroup.Wait())
}

func validateIPPacket(t *testing.T, receivedPacket Packet, expectedICMP *packet.ICMP) {
	require.Equal(t, DatagramTypeIP, receivedPacket.Type())
	rawPacket := receivedPacket.(RawPacket)
	decoder := packet.NewICMPDecoder()
	receivedICMP, err := decoder.Decode(packet.RawPacket(rawPacket))
	require.NoError(t, err)
	validateICMP(t, expectedICMP, receivedICMP)
}

func validateIPPacketWithTracing(t *testing.T, receivedPacket Packet, expectedICMP *packet.ICMP, serializedTracingID []byte) {
	require.Equal(t, DatagramTypeIPWithTrace, receivedPacket.Type())
	tracedPacket := receivedPacket.(*TracedPacket)
	decoder := packet.NewICMPDecoder()
	receivedICMP, err := decoder.Decode(tracedPacket.Packet)
	require.NoError(t, err)
	validateICMP(t, expectedICMP, receivedICMP)
	require.True(t, bytes.Equal(tracedPacket.TracingIdentity, serializedTracingID))
}

func validateICMP(t *testing.T, expected, actual *packet.ICMP) {
	require.Equal(t, expected.IP, actual.IP)
	require.Equal(t, expected.Type, actual.Type)
	require.Equal(t, expected.Code, actual.Code)
	require.Equal(t, expected.Body, actual.Body)
}

func validateTracingSpans(t *testing.T, receivedPacket Packet, expectedSpan *TracingSpanPacket) {
	require.Equal(t, DatagramTypeTracingSpan, receivedPacket.Type())
	tracingSpans := receivedPacket.(*TracingSpanPacket)
	require.Equal(t, tracingSpans, expectedSpan)
}

func newQUICListener(t *testing.T, config *quic.Config) *quic.Listener {
	// Create a simple tls config.
	tlsConfig := generateTLSConfig()

	listener, err := quic.ListenAddr("127.0.0.1:0", tlsConfig, config)
	require.NoError(t, err)

	return listener
}

func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"argotunnel"},
	}
}
