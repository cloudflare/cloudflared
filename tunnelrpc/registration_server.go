package tunnelrpc

import (
	"context"
	"io"

	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// RegistrationServer provides a handler interface for a client to provide methods to handle the different types of
// requests that can be communicated by the stream.
type RegistrationServer struct {
	registrationServer pogs.RegistrationServer
}

func NewRegistrationServer(registrationServer pogs.RegistrationServer) *RegistrationServer {
	return &RegistrationServer{
		registrationServer: registrationServer,
	}
}

// Serve listens for all RegistrationServer RPCs, including UnregisterConnection until the underlying connection
// is terminated.
func (s *RegistrationServer) Serve(ctx context.Context, stream io.ReadWriteCloser) error {
	transport := SafeTransport(stream)
	defer transport.Close()

	main := pogs.RegistrationServer_ServerToClient(s.registrationServer)
	rpcConn := NewServerConn(transport, main.Client)

	select {
	case <-rpcConn.Done():
		return rpcConn.Wait()
	case <-ctx.Done():
		return ctx.Err()
	}
}
