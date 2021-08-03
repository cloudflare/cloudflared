package connection

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"sync"
	"testing"

	"github.com/lucas-clemente/quic-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicpogs "github.com/cloudflare/cloudflared/quic"
)

// TestQUICServer tests if a quic server accepts and responds to a quic client with the acceptance protocol.
// It also serves as a demonstration for communication with the QUIC connection started by a cloudflared.
func TestQUICServer(t *testing.T) {
	quicConfig := &quic.Config{
		KeepAlive: true,
	}

	log := zerolog.New(os.Stdout)

	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")

	require.NoError(t, err)
	udpListener, err := net.ListenUDP(udpAddr.Network(), udpAddr)
	require.NoError(t, err)
	defer udpListener.Close()
	tlsConfig := generateTLSConfig()
	tlsConfClient := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"argotunnel"},
	}
	var tests = []struct {
		desc            string
		dest            string
		connectionType  quicpogs.ConnectionType
		metadata        []quicpogs.Metadata
		message         []byte
		expectedMessage []byte
	}{
		{
			desc:           "",
			dest:           "somehost.com",
			connectionType: quicpogs.ConnectionTypeWebsocket,
			metadata: []quicpogs.Metadata{
				quicpogs.Metadata{
					Key: "key",
					Val: "value",
				},
			},
			expectedMessage: []byte("OK"),
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())

			var wg sync.WaitGroup
			go func() {
				wg.Add(1)
				quicServer(
					t, udpListener, tlsConfig, quicConfig,
					test.dest, test.connectionType, test.metadata, test.message, test.expectedMessage,
				)
				wg.Done()
			}()

			qC, err := NewQUICConnection(context.Background(), quicConfig, udpListener.LocalAddr(), tlsConfClient, log)
			require.NoError(t, err)

			go func() {
				wg.Wait()
				cancel()
			}()

			qC.Serve(ctx)

		})
	}

}

func quicServer(
	t *testing.T,
	conn net.PacketConn,
	tlsConf *tls.Config,
	config *quic.Config,
	dest string,
	connectionType quicpogs.ConnectionType,
	metadata []quicpogs.Metadata,
	message []byte,
	expectedResponse []byte,
) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	earlyListener, err := quic.ListenEarly(conn, tlsConf, config)
	require.NoError(t, err)

	session, err := earlyListener.Accept(ctx)
	require.NoError(t, err)

	stream, err := session.OpenStreamSync(context.Background())
	require.NoError(t, err)

	// Start off ALPN
	err = quicpogs.WriteConnectRequestData(stream, dest, connectionType, metadata...)
	require.NoError(t, err)

	_, err = quicpogs.ReadConnectResponseData(stream)
	require.NoError(t, err)

	if message != nil {
		// ALPN successful. Write data.
		_, err = stream.Write([]byte(message))
		require.NoError(t, err)
	}

	response, err := ioutil.ReadAll(stream)
	require.NoError(t, err)

	// For now it is an echo server. Verify if the same data is returned.
	assert.Equal(t, expectedResponse, response)
}

// Setup a bare-bones TLS config for the server
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
