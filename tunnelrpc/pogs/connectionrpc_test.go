package pogs

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	capnp "zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/rpc"

	"github.com/cloudflare/cloudflared/tunnelrpc"
)

const testAccountTag = "abc123"

func TestMarshalConnectionOptions(t *testing.T) {
	clientID := uuid.New()
	orig := ConnectionOptions{
		Client: ClientInfo{
			ClientID: clientID[:],
			Features: []string{"a", "b"},
			Version:  "1.2.3",
			Arch:     "macos",
		},
		OriginLocalIP:      []byte{10, 2, 3, 4},
		ReplaceExisting:    false,
		CompressionQuality: 1,
	}

	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	require.NoError(t, err)
	capnpOpts, err := tunnelrpc.NewConnectionOptions(seg)
	require.NoError(t, err)

	err = orig.MarshalCapnproto(capnpOpts)
	assert.NoError(t, err)

	var pogsOpts ConnectionOptions
	err = pogsOpts.UnmarshalCapnproto(capnpOpts)
	assert.NoError(t, err)

	assert.Equal(t, orig, pogsOpts)
}

func TestConnectionRegistrationRPC(t *testing.T) {
	p1, p2 := net.Pipe()

	t1, t2 := rpc.StreamTransport(p1), rpc.StreamTransport(p2)

	// Server-side
	testImpl := testConnectionRegistrationServer{}
	srv := TunnelServer_ServerToClient(&testImpl)
	serverConn := rpc.NewConn(t1, rpc.MainInterface(srv.Client))
	defer serverConn.Wait()

	ctx := context.Background()
	clientConn := rpc.NewConn(t2)
	defer clientConn.Close()
	client := TunnelServer_PogsClient{
		RegistrationServer_PogsClient: RegistrationServer_PogsClient{
			Client: clientConn.Bootstrap(ctx),
			Conn:   clientConn,
		},
		Client: clientConn.Bootstrap(ctx),
		Conn:   clientConn,
	}
	defer client.Close()

	clientID := uuid.New()
	options := &ConnectionOptions{
		Client: ClientInfo{
			ClientID: clientID[:],
			Features: []string{"foo"},
			Version:  "1.2.3",
			Arch:     "macos",
		},
		OriginLocalIP:      net.IP{10, 20, 30, 40},
		ReplaceExisting:    true,
		CompressionQuality: 0,
	}

	expectedDetails := ConnectionDetails{
		UUID:     uuid.New(),
		Location: "TEST",
	}
	testImpl.details = &expectedDetails
	testImpl.err = nil

	auth := TunnelAuth{
		AccountTag:   testAccountTag,
		TunnelSecret: []byte{1, 2, 3, 4},
	}

	// success
	tunnelID := uuid.New()
	details, err := client.RegisterConnection(ctx, auth, tunnelID, 2, options)
	assert.NoError(t, err)
	assert.Equal(t, expectedDetails, *details)

	// regular error
	testImpl.details = nil
	testImpl.err = errors.New("internal")

	_, err = client.RegisterConnection(ctx, auth, tunnelID, 2, options)
	assert.EqualError(t, err, "internal")

	// retriable error
	testImpl.details = nil
	const delay = 27 * time.Second
	testImpl.err = RetryErrorAfter(errors.New("retryable"), delay)

	_, err = client.RegisterConnection(ctx, auth, tunnelID, 2, options)
	assert.EqualError(t, err, "retryable")

	re, ok := err.(*RetryableError)
	assert.True(t, ok)
	assert.Equal(t, delay, re.Delay)
}

type testConnectionRegistrationServer struct {
	mockTunnelServerBase

	details *ConnectionDetails
	err     error
}

func (t *testConnectionRegistrationServer) UpdateLocalConfiguration(ctx context.Context, config []byte) error {
	// do nothing at this point
	return nil
}

func (t *testConnectionRegistrationServer) RegisterConnection(ctx context.Context, auth TunnelAuth, tunnelID uuid.UUID, connIndex byte, options *ConnectionOptions) (*ConnectionDetails, error) {
	if auth.AccountTag != testAccountTag {
		panic("bad account tag: " + auth.AccountTag)
	}
	if t.err != nil {
		return nil, t.err
	}
	if t.details != nil {
		return t.details, nil
	}

	panic("either details or err mush be set")
}
