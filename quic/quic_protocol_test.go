package quic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testCloseIdleAfterHint = time.Minute * 2
)

func TestConnectRequestData(t *testing.T) {
	var tests = []struct {
		name           string
		hostname       string
		connectionType ConnectionType
		metadata       []Metadata
	}{
		{
			name:           "Signature verified and request metadata is unmarshaled and read correctly",
			hostname:       "tunnel.com",
			connectionType: ConnectionTypeHTTP,
			metadata: []Metadata{
				{
					Key: "key",
					Val: "1234",
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := &bytes.Buffer{}
			reqClientStream := RequestClientStream{noopCloser{b}}
			err := reqClientStream.WriteConnectRequestData(test.hostname, test.connectionType, test.metadata...)
			require.NoError(t, err)
			protocol, err := DetermineProtocol(b)
			require.NoError(t, err)
			reqServerStream, err := NewRequestServerStream(noopCloser{b}, protocol)
			require.NoError(t, err)

			reqMeta, err := reqServerStream.ReadConnectRequestData()
			require.NoError(t, err)

			assert.Equal(t, test.metadata, reqMeta.Metadata)
			assert.Equal(t, test.hostname, reqMeta.Dest)
			assert.Equal(t, test.connectionType, reqMeta.Type)
		})
	}
}

func TestConnectResponseMeta(t *testing.T) {
	var tests = []struct {
		name     string
		err      error
		metadata []Metadata
	}{
		{
			name: "Signature verified and response metadata is unmarshaled and read correctly",
			metadata: []Metadata{
				{
					Key: "key",
					Val: "1234",
				},
			},
		},
		{
			name: "If error is not empty, other fields should be blank",
			err:  errors.New("something happened"),
			metadata: []Metadata{
				{
					Key: "key",
					Val: "1234",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := &bytes.Buffer{}
			reqServerStream := RequestServerStream{noopCloser{b}}
			err := reqServerStream.WriteConnectResponseData(test.err, test.metadata...)
			require.NoError(t, err)

			reqClientStream := RequestClientStream{noopCloser{b}}
			respMeta, err := reqClientStream.ReadConnectResponseData()
			require.NoError(t, err)

			if respMeta.Error == "" {
				assert.Equal(t, test.metadata, respMeta.Metadata)
			} else {
				assert.Equal(t, 0, len(respMeta.Metadata))
			}
		})
	}
}

func TestRegisterUdpSession(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	clientStream := mockRPCStream{clientReader, clientWriter}
	serverStream := mockRPCStream{serverReader, serverWriter}

	unregisterMessage := "closed by eyeball"
	rpcServer := mockRPCServer{
		sessionID:         uuid.New(),
		dstIP:             net.IP{172, 16, 0, 1},
		dstPort:           8000,
		closeIdleAfter:    testCloseIdleAfterHint,
		unregisterMessage: unregisterMessage,
	}
	logger := zerolog.Nop()
	sessionRegisteredChan := make(chan struct{})
	go func() {
		protocol, err := DetermineProtocol(serverStream)
		assert.NoError(t, err)
		rpcServerStream, err := NewRPCServerStream(serverStream, protocol)
		assert.NoError(t, err)
		err = rpcServerStream.Serve(rpcServer, &logger)
		assert.NoError(t, err)

		serverStream.Close()
		close(sessionRegisteredChan)
	}()

	rpcClientStream, err := NewRPCClientStream(context.Background(), clientStream, &logger)
	assert.NoError(t, err)

	assert.NoError(t, rpcClientStream.RegisterUdpSession(context.Background(), rpcServer.sessionID, rpcServer.dstIP, rpcServer.dstPort, testCloseIdleAfterHint))

	// Different sessionID, the RPC server should reject the registraion
	assert.Error(t, rpcClientStream.RegisterUdpSession(context.Background(), uuid.New(), rpcServer.dstIP, rpcServer.dstPort, testCloseIdleAfterHint))

	assert.NoError(t, rpcClientStream.UnregisterUdpSession(context.Background(), rpcServer.sessionID, unregisterMessage))

	// Different sessionID, the RPC server should reject the unregistraion
	assert.Error(t, rpcClientStream.UnregisterUdpSession(context.Background(), uuid.New(), unregisterMessage))

	rpcClientStream.Close()
	<-sessionRegisteredChan
}

type mockRPCServer struct {
	sessionID         uuid.UUID
	dstIP             net.IP
	dstPort           uint16
	closeIdleAfter    time.Duration
	unregisterMessage string
}

func (s mockRPCServer) RegisterUdpSession(ctx context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16, closeIdleAfter time.Duration) error {
	if s.sessionID != sessionID {
		return fmt.Errorf("expect session ID %s, got %s", s.sessionID, sessionID)
	}
	if !s.dstIP.Equal(dstIP) {
		return fmt.Errorf("expect destination IP %s, got %s", s.dstIP, dstIP)
	}
	if s.dstPort != dstPort {
		return fmt.Errorf("expect destination port %d, got %d", s.dstPort, dstPort)
	}
	if s.closeIdleAfter != closeIdleAfter {
		return fmt.Errorf("expect closeIdleAfter %d, got %d", s.closeIdleAfter, closeIdleAfter)
	}
	return nil
}

func (s mockRPCServer) UnregisterUdpSession(ctx context.Context, sessionID uuid.UUID, message string) error {
	if s.sessionID != sessionID {
		return fmt.Errorf("expect session ID %s, got %s", s.sessionID, sessionID)
	}
	if s.unregisterMessage != message {
		return fmt.Errorf("expect unregister message %s, got %s", s.unregisterMessage, message)
	}
	return nil
}

type mockRPCStream struct {
	io.ReadCloser
	io.WriteCloser
}

func (s mockRPCStream) Close() error {
	_ = s.ReadCloser.Close()
	_ = s.WriteCloser.Close()
	return nil
}

type noopCloser struct {
	io.ReadWriter
}

func (noopCloser) Close() error {
	return nil
}
