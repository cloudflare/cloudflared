package pogs

import (
	"fmt"

	capnp "zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/pogs"

	"github.com/cloudflare/cloudflared/tunnelrpc/proto"
)

// ConnectionType indicates the type of underlying connection proxied within the QUIC stream.
type ConnectionType uint16

const (
	ConnectionTypeHTTP ConnectionType = iota
	ConnectionTypeWebsocket
	ConnectionTypeTCP
)

var (
	// ErrorFlowConnectRateLimitedMetadata is the Metadata entry that allows to know if a request was rate limited on connect.
	ErrorFlowConnectRateLimitedMetadata = Metadata{Key: "FlowConnectRateLimited", Val: "true"}
)

func (c ConnectionType) String() string {
	switch c {
	case ConnectionTypeHTTP:
		return "http"
	case ConnectionTypeWebsocket:
		return "ws"
	case ConnectionTypeTCP:
		return "tcp"
	}
	panic(fmt.Sprintf("invalid ConnectionType: %d", c))
}

// ConnectRequest is the representation of metadata sent at the start of a QUIC application handshake.
type ConnectRequest struct {
	Dest     string         `capnp:"dest"`
	Type     ConnectionType `capnp:"type"`
	Metadata []Metadata     `capnp:"metadata"`
}

// Metadata is a representation of key value based data sent via RequestMeta.
type Metadata struct {
	Key string `capnp:"key"`
	Val string `capnp:"val"`
}

// MetadataMap returns a map format of []Metadata.
func (r *ConnectRequest) MetadataMap() map[string]string {
	metadataMap := make(map[string]string)
	for _, metadata := range r.Metadata {
		metadataMap[metadata.Key] = metadata.Val
	}
	return metadataMap
}

func (r *ConnectRequest) FromPogs(msg *capnp.Message) error {
	metadata, err := proto.ReadRootConnectRequest(msg)
	if err != nil {
		return err
	}
	return pogs.Extract(r, proto.ConnectRequest_TypeID, metadata.Struct)
}

func (r *ConnectRequest) ToPogs() (*capnp.Message, error) {
	msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return nil, err
	}

	root, err := proto.NewRootConnectRequest(seg)
	if err != nil {
		return nil, err
	}

	if err := pogs.Insert(proto.ConnectRequest_TypeID, root.Struct, r); err != nil {
		return nil, err
	}

	return msg, nil
}

// ConnectResponse is a representation of metadata sent as a response to a QUIC application handshake.
type ConnectResponse struct {
	Error    string     `capnp:"error"`
	Metadata []Metadata `capnp:"metadata"`
}

func (r *ConnectResponse) FromPogs(msg *capnp.Message) error {
	metadata, err := proto.ReadRootConnectResponse(msg)
	if err != nil {
		return err
	}
	return pogs.Extract(r, proto.ConnectResponse_TypeID, metadata.Struct)
}

func (r *ConnectResponse) ToPogs() (*capnp.Message, error) {
	msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return nil, err
	}

	root, err := proto.NewRootConnectResponse(seg)
	if err != nil {
		return nil, err
	}

	if err := pogs.Insert(proto.ConnectResponse_TypeID, root.Struct, r); err != nil {
		return nil, err
	}

	return msg, nil
}
