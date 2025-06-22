package quic

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/cloudflare/cloudflared/tunnelrpc"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// SessionManagerServer handles streams with the SessionManager RPCs.
type SessionManagerServer struct {
	sessionManager  pogs.SessionManager
	responseTimeout time.Duration
}

func NewSessionManagerServer(sessionManager pogs.SessionManager, responseTimeout time.Duration) *SessionManagerServer {
	return &SessionManagerServer{
		sessionManager:  sessionManager,
		responseTimeout: responseTimeout,
	}
}

func (s *SessionManagerServer) Serve(ctx context.Context, stream io.ReadWriteCloser) error {
	signature, err := determineProtocol(stream)
	if err != nil {
		return err
	}
	switch signature {
	case rpcStreamProtocolSignature:
		break
	case dataStreamProtocolSignature:
		return errDataStreamNotSupported
	default:
		return fmt.Errorf("unknown protocol %v", signature)
	}

	// Every new quic.Stream request aligns to a new RPC request, this is why there is a timeout for the server-side
	// of the RPC request.
	ctx, cancel := context.WithTimeout(ctx, s.responseTimeout)
	defer cancel()

	transport := tunnelrpc.SafeTransport(stream)
	defer transport.Close()

	main := pogs.SessionManager_ServerToClient(s.sessionManager)
	rpcConn := tunnelrpc.NewServerConn(transport, main.Client)
	defer rpcConn.Close()

	select {
	case <-rpcConn.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
