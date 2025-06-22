package quic

import (
	"io"

	capnp "zombiezen.com/go/capnproto2"

	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// RequestServerStream is a stream to serve requests
type RequestServerStream struct {
	io.ReadWriteCloser
}

// ReadConnectRequestData reads the handshake data from a QUIC stream.
func (rss *RequestServerStream) ReadConnectRequestData() (*pogs.ConnectRequest, error) {
	// This is a NO-OP for now. We could cause a branching if we wanted to use multiple versions.
	if _, err := readVersion(rss); err != nil {
		return nil, err
	}

	msg, err := capnp.NewDecoder(rss).Decode()
	if err != nil {
		return nil, err
	}

	r := &pogs.ConnectRequest{}
	if err := r.FromPogs(msg); err != nil {
		return nil, err
	}
	return r, nil
}

// WriteConnectResponseData writes response to a QUIC stream.
func (rss *RequestServerStream) WriteConnectResponseData(respErr error, metadata ...pogs.Metadata) error {
	var connectResponse *pogs.ConnectResponse
	if respErr != nil {
		connectResponse = &pogs.ConnectResponse{
			Error:    respErr.Error(),
			Metadata: metadata,
		}
	} else {
		connectResponse = &pogs.ConnectResponse{
			Metadata: metadata,
		}
	}

	msg, err := connectResponse.ToPogs()
	if err != nil {
		return err
	}

	if err := writeDataStreamPreamble(rss); err != nil {
		return err
	}
	return capnp.NewEncoder(rss).Encode(msg)
}
