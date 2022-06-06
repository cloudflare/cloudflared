package quic

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lucas-clemente/quic-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

var (
	testSessionID = uuid.New()
)

func TestSuffixThenRemoveSessionID(t *testing.T) {
	msg := []byte(t.Name())
	msgWithID, err := suffixSessionID(testSessionID, msg)
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
	_, err := suffixSessionID(testSessionID, msg)
	require.NoError(t, err)

	msg = make([]byte, MaxDatagramFrameSize-sessionIDLen+1)
	_, err = suffixSessionID(testSessionID, msg)
	require.Error(t, err)
}

func TestMaxDatagramPayload(t *testing.T) {
	payload := make([]byte, maxDatagramPayloadSize)

	quicConfig := &quic.Config{
		KeepAlivePeriod:      5 * time.Millisecond,
		EnableDatagrams:      true,
		MaxDatagramFrameSize: MaxDatagramFrameSize,
	}
	quicListener := newQUICListener(t, quicConfig)
	defer quicListener.Close()

	errGroup, ctx := errgroup.WithContext(context.Background())
	// Run edge side of datagram muxer
	errGroup.Go(func() error {
		// Accept quic connection
		quicSession, err := quicListener.Accept(ctx)
		if err != nil {
			return err
		}

		logger := zerolog.Nop()
		muxer, err := NewDatagramMuxer(quicSession, &logger)
		if err != nil {
			return err
		}

		sessionID, receivedPayload, err := muxer.ReceiveFrom()
		if err != nil {
			return err
		}
		require.Equal(t, testSessionID, sessionID)
		require.True(t, bytes.Equal(payload, receivedPayload))

		return nil
	})

	// Run cloudflared side of datagram muxer
	errGroup.Go(func() error {
		tlsClientConfig := &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"argotunnel"},
		}
		// Establish quic connection
		quicSession, err := quic.DialAddrEarly(quicListener.Addr().String(), tlsClientConfig, quicConfig)
		require.NoError(t, err)

		logger := zerolog.Nop()
		muxer, err := NewDatagramMuxer(quicSession, &logger)
		if err != nil {
			return err
		}

		// Wait a few milliseconds for MTU discovery to take place
		time.Sleep(time.Millisecond * 100)
		err = muxer.SendTo(testSessionID, payload)
		if err != nil {
			return err
		}

		// Payload larger than transport MTU, should return an error
		largePayload := make([]byte, MaxDatagramFrameSize)
		err = muxer.SendTo(testSessionID, largePayload)
		require.Error(t, err)

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
