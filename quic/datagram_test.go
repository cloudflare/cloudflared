package quic

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/google/gopacket/layers"
	"github.com/google/uuid"
	"github.com/lucas-clemente/quic-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/packet"
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

	ctx, cancel := context.WithCancel(context.Background())
	errGroup, _ := errgroup.WithContext(ctx)
	var receivedMessages sync.WaitGroup
	receivedMessages.Add(1)
	// Run edge side of datagram muxer
	errGroup.Go(func() error {
		defer receivedMessages.Done()
		defer cancel()

		// Accept quic connection
		quicSession, err := quicListener.Accept(ctx)
		if err != nil {
			return err
		}

		sessionDemuxChan := make(chan *packet.Session, 16)

		switch version {
		case 1:
			muxer := NewDatagramMuxer(quicSession, &logger, sessionDemuxChan)
			go muxer.ServeReceive(ctx)
		case 2:
			muxer := NewDatagramMuxerV2(quicSession, &logger, sessionDemuxChan)
			go muxer.ServeReceive(ctx)

			icmpDecoder := packet.NewICMPDecoder()
			for _, pk := range packets {
				received, err := muxer.ReceivePacket(ctx)
				require.NoError(t, err)

				receivedICMP, err := icmpDecoder.Decode(received)
				require.NoError(t, err)
				require.Equal(t, pk.IP, receivedICMP.IP)
				require.Equal(t, pk.Type, receivedICMP.Type)
				require.Equal(t, pk.Code, receivedICMP.Code)
				require.Equal(t, pk.Body, receivedICMP.Body)
			}
		default:
			return fmt.Errorf("unknown datagram version %d", version)
		}

		for _, expectedPayload := range sessionToPayloads {
			select {
			case actualPayload := <-sessionDemuxChan:
				require.Equal(t, expectedPayload, actualPayload)
			case <-ctx.Done():
				t.Fatal("edge side got context cancelled before receiving all expected payloads")
			}
		}
		return nil
	})

	largePayload := make([]byte, MaxDatagramFrameSize)
	// Run cloudflared side of datagram muxer
	errGroup.Go(func() error {
		defer cancel()

		tlsClientConfig := &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"argotunnel"},
		}
		// Establish quic connection
		quicSession, err := quic.DialAddrEarly(quicListener.Addr().String(), tlsClientConfig, quicConfig)
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
				require.NoError(t, muxerV2.SendPacket(encodedPacket))
			}
			// Payload larger than transport MTU, should not be sent
			require.Error(t, muxerV2.SendPacket(packet.RawPacket{
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
		receivedMessages.Wait()

		return nil
	})

	require.NoError(t, errGroup.Wait())
}

func newQUICListener(t *testing.T, config *quic.Config) quic.Listener {
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
