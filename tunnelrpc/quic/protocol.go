package quic

import (
	"fmt"
	"io"
)

// protocolSignature defines the first 6 bytes of the stream, which is used to distinguish the type of stream. It
// ensures whoever performs a handshake does not write data before writing the metadata.
type protocolSignature [6]byte

var (
	// dataStreamProtocolSignature is a custom protocol signature for data stream
	dataStreamProtocolSignature = protocolSignature{0x0A, 0x36, 0xCD, 0x12, 0xA1, 0x3E}

	// rpcStreamProtocolSignature is a custom protocol signature for RPC stream
	rpcStreamProtocolSignature = protocolSignature{0x52, 0xBB, 0x82, 0x5C, 0xDB, 0x65}

	errDataStreamNotSupported = fmt.Errorf("data protocol not supported")
	errRPCStreamNotSupported  = fmt.Errorf("rpc protocol not supported")
)

type protocolVersion string

const (
	protocolV1 protocolVersion = "01"

	protocolVersionLength = 2
)

// determineProtocol reads the first 6 bytes from the stream to determine which protocol is spoken by the client.
// The protocols are magic byte arrays understood by both sides of the stream.
func determineProtocol(stream io.Reader) (protocolSignature, error) {
	signature, err := readSignature(stream)
	if err != nil {
		return protocolSignature{}, err
	}
	switch signature {
	case dataStreamProtocolSignature:
		return dataStreamProtocolSignature, nil
	case rpcStreamProtocolSignature:
		return rpcStreamProtocolSignature, nil
	default:
		return protocolSignature{}, fmt.Errorf("unknown signature %v", signature)
	}
}

func writeDataStreamPreamble(stream io.Writer) error {
	if err := writeSignature(stream, dataStreamProtocolSignature); err != nil {
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

func readSignature(stream io.Reader) (protocolSignature, error) {
	var signature protocolSignature
	if _, err := io.ReadFull(stream, signature[:]); err != nil {
		return protocolSignature{}, err
	}
	return signature, nil
}

func writeSignature(stream io.Writer, signature protocolSignature) error {
	_, err := stream.Write(signature[:])
	return err
}
