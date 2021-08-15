package quic

import (
	"bytes"
	"fmt"
	"io"

	capnp "zombiezen.com/go/capnproto2"
)

// protocolSignature is a custom protocol signature to ensure that whoever performs a handshake does not write data
// before writing the metadata.
var protocolSignature = []byte{0x0A, 0x36, 0xCD, 0x12, 0xA1, 0x3E}

// ReadConnectRequestData reads the handshake data from a QUIC stream.
func ReadConnectRequestData(stream io.Reader) (*ConnectRequest, error) {
	if err := verifySignature(stream); err != nil {
		return nil, err
	}

	msg, err := capnp.NewDecoder(stream).Decode()
	if err != nil {
		return nil, err
	}

	r := &ConnectRequest{}
	if err := r.fromPogs(msg); err != nil {
		return nil, err
	}
	return r, nil
}

// WriteConnectRequestData writes requestMeta to a stream.
func WriteConnectRequestData(stream io.Writer, dest string, connectionType ConnectionType, metadata ...Metadata) error {
	connectRequest := &ConnectRequest{
		Dest:     dest,
		Type:     connectionType,
		Metadata: metadata,
	}

	msg, err := connectRequest.toPogs()
	if err != nil {
		return err
	}

	if err := writePreamble(stream); err != nil {
		return err
	}
	return capnp.NewEncoder(stream).Encode(msg)
}

// ReadConnectResponseData reads the response to a RequestMeta in a stream.
func ReadConnectResponseData(stream io.Reader) (*ConnectResponse, error) {
	if err := verifySignature(stream); err != nil {
		return nil, err
	}

	msg, err := capnp.NewDecoder(stream).Decode()
	if err != nil {
		return nil, err
	}

	r := &ConnectResponse{}
	if err := r.fromPogs(msg); err != nil {
		return nil, err
	}
	return r, nil
}

// WriteConnectResponseData writes response to a QUIC stream.
func WriteConnectResponseData(stream io.Writer, respErr error, metadata ...Metadata) error {
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

	if err := writePreamble(stream); err != nil {
		return err
	}
	return capnp.NewEncoder(stream).Encode(msg)
}

func writePreamble(stream io.Writer) error {
	return writeSignature(stream)
	// TODO : TUN-4613 Write protocol version here
}

func writeSignature(stream io.Writer) error {
	_, err := stream.Write(protocolSignature)
	return err
}

func verifySignature(stream io.Reader) error {
	signature := make([]byte, len(protocolSignature))
	if _, err := io.ReadFull(stream, signature); err != nil {
		return err
	}

	if !bytes.Equal(signature[0:], protocolSignature) {
		return fmt.Errorf("Wrong signature: %v", signature)
	}

	return nil
}
