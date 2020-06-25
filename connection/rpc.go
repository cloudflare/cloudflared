package connection

import (
	"context"
	"fmt"
	"time"

	rpc "zombiezen.com/go/capnproto2/rpc"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// NewRPCClient creates and returns a new RPC client, which will communicate
// using a stream on the given muxer
func NewRPCClient(
	ctx context.Context,
	muxer *h2mux.Muxer,
	logger logger.Service,
	openStreamTimeout time.Duration,
) (client tunnelpogs.TunnelServer_PogsClient, err error) {
	openStreamCtx, openStreamCancel := context.WithTimeout(ctx, openStreamTimeout)
	defer openStreamCancel()
	stream, err := muxer.OpenRPCStream(openStreamCtx)
	if err != nil {
		return
	}

	if !isRPCStreamResponse(stream.Headers) {
		stream.Close()
		err = fmt.Errorf("rpc: bad response headers: %v", stream.Headers)
		return
	}

	conn := rpc.NewConn(
		tunnelrpc.NewTransportLogger(logger, rpc.StreamTransport(stream)),
		tunnelrpc.ConnLog(logger),
	)
	client = tunnelpogs.TunnelServer_PogsClient{Client: conn.Bootstrap(ctx), Conn: conn}
	return client, nil
}

func isRPCStreamResponse(headers []h2mux.Header) bool {
	return len(headers) == 1 &&
		headers[0].Name == ":status" &&
		headers[0].Value == "200"
}
