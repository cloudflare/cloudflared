package connection

import (
	"net"
	"testing"

	rpcquic "github.com/cloudflare/cloudflared/tunnelrpc/quic"
	"github.com/stretchr/testify/require"
)

func TestHTTPResponseAdapterHijackRequiresResponse(t *testing.T) {
	t.Parallel()

	stream, peer := net.Pipe()
	defer stream.Close()
	defer peer.Close()
	adapter := httpResponseAdapter{
		RequestServerStream: &rpcquic.RequestServerStream{ReadWriteCloser: stream},
	}
	conn, readWriter, err := adapter.Hijack()

	require.Nil(t, conn)
	require.Nil(t, readWriter)
	require.EqualError(t, err, "status not yet written before attempting to hijack connection")
}

func TestHTTPResponseAdapterHijackAfterResponse(t *testing.T) {
	t.Parallel()

	stream, peer := net.Pipe()
	defer stream.Close()
	defer peer.Close()
	adapter := httpResponseAdapter{
		RequestServerStream: &rpcquic.RequestServerStream{ReadWriteCloser: stream},
		connectResponseSent: true,
	}
	conn, readWriter, err := adapter.Hijack()

	require.NoError(t, err)
	require.NotNil(t, conn)
	require.NotNil(t, readWriter)
}