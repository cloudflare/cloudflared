package connection

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"

	"github.com/gobwas/ws/wsutil"
	"github.com/lucas-clemente/quic-go"
	"github.com/pkg/errors"
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

	// Setup test.
	log := zerolog.New(os.Stdout)

	// Start a UDP Listener for QUIC.
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	udpListener, err := net.ListenUDP(udpAddr.Network(), udpAddr)
	require.NoError(t, err)
	defer udpListener.Close()

	// Create a simple tls config.
	tlsConfig := generateTLSConfig()

	// Create a client config
	tlsClientConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"argotunnel"},
	}

	// Start a mock httpProxy
	originProxy := &mockOriginProxyWithRequest{}

	// This is simply a sample websocket frame message.
	wsBuf := &bytes.Buffer{}
	wsutil.WriteClientText(wsBuf, []byte("Hello"))

	var tests = []struct {
		desc             string
		dest             string
		connectionType   quicpogs.ConnectionType
		metadata         []quicpogs.Metadata
		message          []byte
		expectedResponse []byte
	}{
		{
			desc:           "test http proxy",
			dest:           "/ok",
			connectionType: quicpogs.ConnectionTypeHTTP,
			metadata: []quicpogs.Metadata{
				quicpogs.Metadata{
					Key: "HttpHeader:Cf-Ray",
					Val: "123123123",
				},
				quicpogs.Metadata{
					Key: "HttpHost",
					Val: "cf.host",
				},
				quicpogs.Metadata{
					Key: "HttpMethod",
					Val: "GET",
				},
			},
			expectedResponse: []byte("OK"),
		},
		{
			desc:           "test http body request streaming",
			dest:           "/echo_body",
			connectionType: quicpogs.ConnectionTypeHTTP,
			metadata: []quicpogs.Metadata{
				quicpogs.Metadata{
					Key: "HttpHeader:Cf-Ray",
					Val: "123123123",
				},
				quicpogs.Metadata{
					Key: "HttpHost",
					Val: "cf.host",
				},
				quicpogs.Metadata{
					Key: "HttpMethod",
					Val: "POST",
				},
			},
			message:          []byte("This is the message body"),
			expectedResponse: []byte("This is the message body"),
		},
		{
			desc:           "test ws proxy",
			dest:           "/ok",
			connectionType: quicpogs.ConnectionTypeWebsocket,
			metadata: []quicpogs.Metadata{
				quicpogs.Metadata{
					Key: "HttpHeader:Cf-Cloudflared-Proxy-Connection-Upgrade",
					Val: "Websocket",
				},
				quicpogs.Metadata{
					Key: "HttpHeader:Another-Header",
					Val: "Misc",
				},
				quicpogs.Metadata{
					Key: "HttpHost",
					Val: "cf.host",
				},
				quicpogs.Metadata{
					Key: "HttpMethod",
					Val: "get",
				},
			},
			message:          wsBuf.Bytes(),
			expectedResponse: []byte{0x81, 0x5, 0x48, 0x65, 0x6c, 0x6c, 0x6f},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			var wg sync.WaitGroup
			go func() {
				wg.Add(1)
				defer wg.Done()
				quicServer(
					t, udpListener, tlsConfig, quicConfig,
					test.dest, test.connectionType, test.metadata, test.message, test.expectedResponse,
				)
			}()

			qC, err := NewQUICConnection(ctx, quicConfig, udpListener.LocalAddr(), tlsClientConfig, originProxy, log)
			require.NoError(t, err)
			go qC.Serve(ctx)

			wg.Wait()
			cancel()
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
		_, err := stream.Write([]byte(message))
		require.NoError(t, err)
	}

	response := make([]byte, len(expectedResponse))
	stream.Read(response)
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

type mockOriginProxyWithRequest struct{}

func (moc *mockOriginProxyWithRequest) ProxyHTTP(w ResponseWriter, r *http.Request, isWebsocket bool) error {
	// These are a series of crude tests to ensure the headers and http related data is transferred from
	// metadata.
	if r.Method == "" {
		return errors.New("method not sent")
	}
	if r.Host == "" {
		return errors.New("host not sent")
	}
	if len(r.Header) == 0 {
		return errors.New("headers not set")
	}

	if isWebsocket {
		return wsEndpoint(w, r)
	}
	switch r.URL.Path {
	case "/ok":
		originRespEndpoint(w, http.StatusOK, []byte(http.StatusText(http.StatusOK)))
	case "/echo_body":
		resp := &http.Response{
			StatusCode: http.StatusOK,
		}
		_ = w.WriteRespHeaders(resp.StatusCode, resp.Header)
		io.Copy(w, r.Body)
	case "/error":
		return fmt.Errorf("Failed to proxy to origin")
	default:
		originRespEndpoint(w, http.StatusNotFound, []byte("page not found"))
	}
	return nil
}

func (moc *mockOriginProxyWithRequest) ProxyTCP(ctx context.Context, rwa ReadWriteAcker, tcpRequest *TCPRequest) error {
	return nil
}
