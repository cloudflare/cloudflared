package quic

import (
	"fmt"
	"io"

	capnp "zombiezen.com/go/capnproto2"

	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// RequestClientStream is a stream to provide requests to the server. This operation is typically driven by the edge service.
type RequestClientStream struct {
	io.ReadWriteCloser
}

// WriteConnectRequestData writes requestMeta to a stream.
func (rcs *RequestClientStream) WriteConnectRequestData(dest string, connectionType pogs.ConnectionType, metadata ...pogs.Metadata) error {
	connectRequest := &pogs.ConnectRequest{
		Dest:     dest,
		Type:     connectionType,
		Metadata: metadata,
	}

	msg, err := connectRequest.ToPogs()
	if err != nil {
		return err
	}

	if err := writeDataStreamPreamble(rcs); err != nil {
		return err
	}
	return capnp.NewEncoder(rcs).Encode(msg)
}

// ReadConnectResponseData reads the response from the rpc stream to a ConnectResponse.
func (rcs *RequestClientStream) ReadConnectResponseData() (*pogs.ConnectResponse, error) {
	signature, err := determineProtocol(rcs)
	if err != nil {
		return nil, err
	}
	if signature != dataStreamProtocolSignature {
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

	r := &pogs.ConnectResponse{}
	if err := r.FromPogs(msg); err != nil {
		return nil, err
	}
	return r, nil
}
