package datagramsession

import (
	"context"
	"net"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestCloseSession makes sure a session will stop after context is done
func TestSessionCtxDone(t *testing.T) {
	testSessionReturns(t, true)
}

// TestCloseSession makes sure a session will stop after close method is called
func TestCloseSession(t *testing.T) {
	testSessionReturns(t, false)
}

func testSessionReturns(t *testing.T, closeByContext bool) {
	sessionID := uuid.New()
	cfdConn, originConn := net.Pipe()
	payload := testPayload(sessionID)
	transport := &mockQUICTransport{
		reqChan:  newDatagramChannel(),
		respChan: newDatagramChannel(),
	}
	session := newSession(sessionID, transport, cfdConn)

	ctx, cancel := context.WithCancel(context.Background())
	sessionDone := make(chan struct{})
	go func() {
		session.Serve(ctx)
		close(sessionDone)
	}()

	go func() {
		n, err := session.writeToDst(payload)
		require.NoError(t, err)
		require.Equal(t, len(payload), n)
	}()

	readBuffer := make([]byte, len(payload)+1)
	n, err := originConn.Read(readBuffer)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)

	if closeByContext {
		cancel()
	} else {
		session.close()
	}

	<-sessionDone
	// call cancelled again otherwise the linter will warn about possible context leak
	cancel()
}
