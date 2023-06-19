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

	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
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

func TestUnregisterUdpSession(t *testing.T) {
	unregisterMessage := "closed by eyeball"

	var tests = []struct {
		name             string
		sessionRPCServer mockSessionRPCServer
		timeout          time.Duration
	}{

		{
			name: "UnregisterUdpSessionTimesout if the RPC server does not respond",
			sessionRPCServer: mockSessionRPCServer{
				sessionID:         uuid.New(),
				dstIP:             net.IP{172, 16, 0, 1},
				dstPort:           8000,
				closeIdleAfter:    testCloseIdleAfterHint,
				unregisterMessage: unregisterMessage,
				traceContext:      "1241ce3ecdefc68854e8514e69ba42ca:b38f1bf5eae406f3:0:1",
			},
			// very very low value so we trigger the timeout every time.
			timeout: time.Nanosecond * 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger := zerolog.Nop()
			clientStream, serverStream := newMockRPCStreams()
			sessionRegisteredChan := make(chan struct{})
			go func() {
				protocol, err := DetermineProtocol(serverStream)
				assert.NoError(t, err)
				rpcServerStream, err := NewRPCServerStream(serverStream, protocol)
				assert.NoError(t, err)
				err = rpcServerStream.Serve(test.sessionRPCServer, nil, &logger)
				assert.NoError(t, err)

				serverStream.Close()
				close(sessionRegisteredChan)
			}()

			rpcClientStream, err := NewRPCClientStream(context.Background(), clientStream, test.timeout, &logger)
			assert.NoError(t, err)

			reg, err := rpcClientStream.RegisterUdpSession(context.Background(), test.sessionRPCServer.sessionID, test.sessionRPCServer.dstIP, test.sessionRPCServer.dstPort, testCloseIdleAfterHint, test.sessionRPCServer.traceContext)
			assert.NoError(t, err)
			assert.NoError(t, reg.Err)

			assert.Error(t, rpcClientStream.UnregisterUdpSession(context.Background(), test.sessionRPCServer.sessionID, unregisterMessage))

			rpcClientStream.Close()
			<-sessionRegisteredChan
		})
	}

}

func TestRegisterUdpSession(t *testing.T) {
	unregisterMessage := "closed by eyeball"

	var tests = []struct {
		name             string
		sessionRPCServer mockSessionRPCServer
	}{
		{
			name: "RegisterUdpSession (no trace context)",
			sessionRPCServer: mockSessionRPCServer{
				sessionID:         uuid.New(),
				dstIP:             net.IP{172, 16, 0, 1},
				dstPort:           8000,
				closeIdleAfter:    testCloseIdleAfterHint,
				unregisterMessage: unregisterMessage,
				traceContext:      "",
			},
		},
		{
			name: "RegisterUdpSession (with trace context)",
			sessionRPCServer: mockSessionRPCServer{
				sessionID:         uuid.New(),
				dstIP:             net.IP{172, 16, 0, 1},
				dstPort:           8000,
				closeIdleAfter:    testCloseIdleAfterHint,
				unregisterMessage: unregisterMessage,
				traceContext:      "1241ce3ecdefc68854e8514e69ba42ca:b38f1bf5eae406f3:0:1",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger := zerolog.Nop()
			clientStream, serverStream := newMockRPCStreams()
			sessionRegisteredChan := make(chan struct{})
			go func() {
				protocol, err := DetermineProtocol(serverStream)
				assert.NoError(t, err)
				rpcServerStream, err := NewRPCServerStream(serverStream, protocol)
				assert.NoError(t, err)
				err = rpcServerStream.Serve(test.sessionRPCServer, nil, &logger)
				assert.NoError(t, err)

				serverStream.Close()
				close(sessionRegisteredChan)
			}()

			rpcClientStream, err := NewRPCClientStream(context.Background(), clientStream, 5*time.Second, &logger)
			assert.NoError(t, err)

			reg, err := rpcClientStream.RegisterUdpSession(context.Background(), test.sessionRPCServer.sessionID, test.sessionRPCServer.dstIP, test.sessionRPCServer.dstPort, testCloseIdleAfterHint, test.sessionRPCServer.traceContext)
			assert.NoError(t, err)
			assert.NoError(t, reg.Err)

			// Different sessionID, the RPC server should reject the registraion
			reg, err = rpcClientStream.RegisterUdpSession(context.Background(), uuid.New(), test.sessionRPCServer.dstIP, test.sessionRPCServer.dstPort, testCloseIdleAfterHint, test.sessionRPCServer.traceContext)
			assert.NoError(t, err)
			assert.Error(t, reg.Err)

			assert.NoError(t, rpcClientStream.UnregisterUdpSession(context.Background(), test.sessionRPCServer.sessionID, unregisterMessage))

			// Different sessionID, the RPC server should reject the unregistraion
			assert.Error(t, rpcClientStream.UnregisterUdpSession(context.Background(), uuid.New(), unregisterMessage))

			rpcClientStream.Close()
			<-sessionRegisteredChan
		})
	}
}

func TestManageConfiguration(t *testing.T) {
	var (
		version int32 = 168
		config        = []byte(t.Name())
	)
	clientStream, serverStream := newMockRPCStreams()

	configRPCServer := mockConfigRPCServer{
		version: version,
		config:  config,
	}

	logger := zerolog.Nop()
	updatedChan := make(chan struct{})
	go func() {
		protocol, err := DetermineProtocol(serverStream)
		assert.NoError(t, err)
		rpcServerStream, err := NewRPCServerStream(serverStream, protocol)
		assert.NoError(t, err)
		err = rpcServerStream.Serve(nil, configRPCServer, &logger)
		assert.NoError(t, err)

		serverStream.Close()
		close(updatedChan)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	rpcClientStream, err := NewRPCClientStream(ctx, clientStream, 5*time.Second, &logger)
	assert.NoError(t, err)

	result, err := rpcClientStream.UpdateConfiguration(ctx, version, config)
	assert.NoError(t, err)

	require.Equal(t, version, result.LastAppliedVersion)
	require.NoError(t, result.Err)

	rpcClientStream.Close()
	<-updatedChan
}

type mockSessionRPCServer struct {
	sessionID         uuid.UUID
	dstIP             net.IP
	dstPort           uint16
	closeIdleAfter    time.Duration
	unregisterMessage string
	traceContext      string
}

func (s mockSessionRPCServer) RegisterUdpSession(_ context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16, closeIdleAfter time.Duration, traceContext string) (*tunnelpogs.RegisterUdpSessionResponse, error) {
	if s.sessionID != sessionID {
		return nil, fmt.Errorf("expect session ID %s, got %s", s.sessionID, sessionID)
	}
	if !s.dstIP.Equal(dstIP) {
		return nil, fmt.Errorf("expect destination IP %s, got %s", s.dstIP, dstIP)
	}
	if s.dstPort != dstPort {
		return nil, fmt.Errorf("expect destination port %d, got %d", s.dstPort, dstPort)
	}
	if s.closeIdleAfter != closeIdleAfter {
		return nil, fmt.Errorf("expect closeIdleAfter %d, got %d", s.closeIdleAfter, closeIdleAfter)
	}
	if s.traceContext != traceContext {
		return nil, fmt.Errorf("expect traceContext %s, got %s", s.traceContext, traceContext)
	}
	return &tunnelpogs.RegisterUdpSessionResponse{}, nil
}

func (s mockSessionRPCServer) UnregisterUdpSession(_ context.Context, sessionID uuid.UUID, message string) error {
	if s.sessionID != sessionID {
		return fmt.Errorf("expect session ID %s, got %s", s.sessionID, sessionID)
	}
	if s.unregisterMessage != message {
		return fmt.Errorf("expect unregister message %s, got %s", s.unregisterMessage, message)
	}
	return nil
}

type mockConfigRPCServer struct {
	version int32
	config  []byte
}

func (s mockConfigRPCServer) UpdateConfiguration(_ context.Context, version int32, config []byte) *tunnelpogs.UpdateConfigurationResponse {
	if s.version != version {
		return &tunnelpogs.UpdateConfigurationResponse{
			Err: fmt.Errorf("expect version %d, got %d", s.version, version),
		}
	}
	if !bytes.Equal(s.config, config) {
		return &tunnelpogs.UpdateConfigurationResponse{
			Err: fmt.Errorf("expect config %v, got %v", s.config, config),
		}
	}
	return &tunnelpogs.UpdateConfigurationResponse{LastAppliedVersion: version}
}

type mockRPCStream struct {
	io.ReadCloser
	io.WriteCloser
}

func newMockRPCStreams() (client io.ReadWriteCloser, server io.ReadWriteCloser) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	client = mockRPCStream{clientReader, clientWriter}
	server = mockRPCStream{serverReader, serverWriter}
	return
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
