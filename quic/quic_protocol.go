package quic

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	capnp "zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/rpc"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/tunnelrpc"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// ProtocolSignature defines the first 6 bytes of the stream, which is used to distinguish the type of stream. It
// ensures whoever performs a handshake does not write data before writing the metadata.
type ProtocolSignature [6]byte

var (
	// DataStreamProtocolSignature is a custom protocol signature for data stream
	DataStreamProtocolSignature = ProtocolSignature{0x0A, 0x36, 0xCD, 0x12, 0xA1, 0x3E}

	// RPCStreamProtocolSignature is a custom protocol signature for RPC stream
	RPCStreamProtocolSignature = ProtocolSignature{0x52, 0xBB, 0x82, 0x5C, 0xDB, 0x65}
)

type protocolVersion string

const (
	protocolV1 protocolVersion = "01"

	protocolVersionLength = 2

	HandshakeIdleTimeout = 5 * time.Second
	MaxIdleTimeout       = 5 * time.Second
	MaxIdlePingPeriod    = 1 * time.Second
)

// RequestServerStream is a stream to serve requests
type RequestServerStream struct {
	io.ReadWriteCloser
}

func NewRequestServerStream(stream io.ReadWriteCloser, signature ProtocolSignature) (*RequestServerStream, error) {
	if signature != DataStreamProtocolSignature {
		return nil, fmt.Errorf("RequestClientStream can only be created from data stream")
	}
	return &RequestServerStream{stream}, nil
}

// ReadConnectRequestData reads the handshake data from a QUIC stream.
func (rss *RequestServerStream) ReadConnectRequestData() (*ConnectRequest, error) {
	// This is a NO-OP for now. We could cause a branching if we wanted to use multiple versions.
	if _, err := readVersion(rss); err != nil {
		return nil, err
	}

	msg, err := capnp.NewDecoder(rss).Decode()
	if err != nil {
		return nil, err
	}

	r := &ConnectRequest{}
	if err := r.fromPogs(msg); err != nil {
		return nil, err
	}
	return r, nil
}

// WriteConnectResponseData writes response to a QUIC stream.
func (rss *RequestServerStream) WriteConnectResponseData(respErr error, metadata ...Metadata) error {
	var connectResponse *ConnectResponse
	if respErr != nil {
		connectResponse = &ConnectResponse{
			Error: respErr.Error(),
		}
	} else {
		connectResponse = &ConnectResponse{
			Metadata: metadata,
		}
	}

	msg, err := connectResponse.toPogs()
	if err != nil {
		return err
	}

	if err := writeDataStreamPreamble(rss); err != nil {
		return err
	}
	return capnp.NewEncoder(rss).Encode(msg)
}

type RequestClientStream struct {
	io.ReadWriteCloser
}

// WriteConnectRequestData writes requestMeta to a stream.
func (rcs *RequestClientStream) WriteConnectRequestData(dest string, connectionType ConnectionType, metadata ...Metadata) error {
	connectRequest := &ConnectRequest{
		Dest:     dest,
		Type:     connectionType,
		Metadata: metadata,
	}

	msg, err := connectRequest.toPogs()
	if err != nil {
		return err
	}

	if err := writeDataStreamPreamble(rcs); err != nil {
		return err
	}
	return capnp.NewEncoder(rcs).Encode(msg)
}

// ReadConnectResponseData reads the response to a RequestMeta in a stream.
func (rcs *RequestClientStream) ReadConnectResponseData() (*ConnectResponse, error) {
	signature, err := DetermineProtocol(rcs)
	if err != nil {
		return nil, err
	}
	if signature != DataStreamProtocolSignature {
		return nil, fmt.Errorf("wrong protocol signature %v", signature)
	}

	// This is a NO-OP for now. We could cause a branching if we wanted to use multiple versions.
	if _, err := readVersion(rcs); err != nil {
		return nil, err
	}

	msg, err := capnp.NewDecoder(rcs).Decode()
	if err != nil {
		return nil, err
	}

	r := &ConnectResponse{}
	if err := r.fromPogs(msg); err != nil {
		return nil, err
	}
	return r, nil
}

// RPCServerStream is a stream to serve RPCs. It is closed when the RPC client is done
type RPCServerStream struct {
	io.ReadWriteCloser
}

func NewRPCServerStream(stream io.ReadWriteCloser, protocol ProtocolSignature) (*RPCServerStream, error) {
	if protocol != RPCStreamProtocolSignature {
		return nil, fmt.Errorf("RPCStream can only be created from rpc stream")
	}
	return &RPCServerStream{stream}, nil
}

func (s *RPCServerStream) Serve(sessionManager tunnelpogs.SessionManager, configManager tunnelpogs.ConfigurationManager, logger *zerolog.Logger) error {
	// RPC logs are very robust, create a new logger that only logs error to reduce noise
	rpcLogger := logger.Level(zerolog.ErrorLevel)
	rpcTransport := tunnelrpc.NewTransportLogger(&rpcLogger, rpc.StreamTransport(s))
	defer rpcTransport.Close()

	main := tunnelpogs.CloudflaredServer_ServerToClient(sessionManager, configManager)
	rpcConn := rpc.NewConn(
		rpcTransport,
		rpc.MainInterface(main.Client),
		tunnelrpc.ConnLog(&rpcLogger),
	)
	defer rpcConn.Close()

	return rpcConn.Wait()
}

func DetermineProtocol(stream io.Reader) (ProtocolSignature, error) {
	signature, err := readSignature(stream)
	if err != nil {
		return ProtocolSignature{}, err
	}
	switch signature {
	case DataStreamProtocolSignature:
		return DataStreamProtocolSignature, nil
	case RPCStreamProtocolSignature:
		return RPCStreamProtocolSignature, nil
	default:
		return ProtocolSignature{}, fmt.Errorf("unknown signature %v", signature)
	}
}

func writeDataStreamPreamble(stream io.Writer) error {
	if err := writeSignature(stream, DataStreamProtocolSignature); err != nil {
		return err
	}

	return writeVersion(stream)
}

func writeVersion(stream io.Writer) error {
	_, err := stream.Write([]byte(protocolV1)[:protocolVersionLength])
	return err
}

func readVersion(stream io.Reader) (string, error) {
	version := make([]byte, protocolVersionLength)
	_, err := stream.Read(version)
	return string(version), err
}

func readSignature(stream io.Reader) (ProtocolSignature, error) {
	var signature ProtocolSignature
	if _, err := io.ReadFull(stream, signature[:]); err != nil {
		return ProtocolSignature{}, err
	}
	return signature, nil
}

func writeSignature(stream io.Writer, signature ProtocolSignature) error {
	_, err := stream.Write(signature[:])
	return err
}

// RPCClientStream is a stream to call methods of SessionManager
type RPCClientStream struct {
	client    tunnelpogs.CloudflaredServer_PogsClient
	transport rpc.Transport
}

func NewRPCClientStream(ctx context.Context, stream io.ReadWriteCloser, logger *zerolog.Logger) (*RPCClientStream, error) {
	n, err := stream.Write(RPCStreamProtocolSignature[:])
	if err != nil {
		return nil, err
	}
	if n != len(RPCStreamProtocolSignature) {
		return nil, fmt.Errorf("expect to write %d bytes for RPC stream protocol signature, wrote %d", len(RPCStreamProtocolSignature), n)
	}
	transport := tunnelrpc.NewTransportLogger(logger, rpc.StreamTransport(stream))
	conn := rpc.NewConn(
		transport,
		tunnelrpc.ConnLog(logger),
	)
	return &RPCClientStream{
		client:    tunnelpogs.NewCloudflaredServer_PogsClient(conn.Bootstrap(ctx), conn),
		transport: transport,
	}, nil
}

func (rcs *RPCClientStream) RegisterUdpSession(ctx context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16, closeIdleAfterHint time.Duration) error {
	resp, err := rcs.client.RegisterUdpSession(ctx, sessionID, dstIP, dstPort, closeIdleAfterHint)
	if err != nil {
		return err
	}
	return resp.Err
}

func (rcs *RPCClientStream) UnregisterUdpSession(ctx context.Context, sessionID uuid.UUID, message string) error {
	return rcs.client.UnregisterUdpSession(ctx, sessionID, message)
}

func (rcs *RPCClientStream) UpdateConfiguration(ctx context.Context, version int32, config []byte) (*tunnelpogs.UpdateConfigurationResponse, error) {
	return rcs.client.UpdateConfiguration(ctx, version, config)
}

func (rcs *RPCClientStream) Close() {
	_ = rcs.client.Close()
	_ = rcs.transport.Close()
}
