package websocket

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net"
	"testing"
	"time"

	"github.com/gobwas/ws/wsutil"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// readThrough reads exactly want bytes from conn using buffers of size bufSize,
// the way cfio.Copy feeds Conn.Read from its fixed-size pool buffer.
func readThrough(t *testing.T, conn *Conn, bufSize, want int) []byte {
	t.Helper()
	var out bytes.Buffer
	buf := make([]byte, bufSize)
	for out.Len() < want {
		n, err := conn.Read(buf)
		require.NoError(t, err)
		require.LessOrEqual(t, n, bufSize)
		out.Write(buf[:n])
	}
	return out.Bytes()
}

func randomPayload(t *testing.T, size int) []byte {
	t.Helper()
	payload := make([]byte, size)
	_, err := io.ReadFull(rand.Reader, payload)
	require.NoError(t, err)
	return payload
}

func newTestConn(t *testing.T) (*Conn, net.Conn) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	server, client := net.Pipe()
	log := zerolog.Nop()
	// Bound the test: a Read that never sees the rest of a message would otherwise block forever
	require.NoError(t, server.SetReadDeadline(time.Now().Add(10*time.Second)))
	conn := NewConn(ctx, server, &log)
	t.Cleanup(func() {
		cancel()
		conn.Close()
		_ = server.Close()
		_ = client.Close()
	})
	return conn, client
}

// TestConnReadMessageLargerThanBuffer verifies that a single client message larger
// than the buffer passed to Read is delivered whole across multiple Read calls,
// rather than truncated to the first len(buffer) bytes.
func TestConnReadMessageLargerThanBuffer(t *testing.T) {
	const bufSize = 16 * 1024
	const msgSize = 100 * 1024

	conn, client := newTestConn(t)
	payload := randomPayload(t, msgSize)

	go func() {
		_ = wsutil.WriteClientBinary(client, payload)
	}()

	got := readThrough(t, conn, bufSize, msgSize)
	require.Equal(t, payload, got)
}

// TestConnReadMixedMessageSizes sends several messages around the buffer boundary
// and verifies all bytes arrive in order.
func TestConnReadMixedMessageSizes(t *testing.T) {
	const bufSize = 16 * 1024
	sizes := []int{1, bufSize - 1, bufSize, bufSize + 1, 40000, 3 * bufSize, 100 * 1024, 7, bufSize}

	conn, client := newTestConn(t)

	var expected bytes.Buffer
	payloads := make([][]byte, 0, len(sizes))
	for _, size := range sizes {
		payload := randomPayload(t, size)
		payloads = append(payloads, payload)
		expected.Write(payload)
	}

	go func() {
		for _, payload := range payloads {
			if err := wsutil.WriteClientBinary(client, payload); err != nil {
				return
			}
		}
	}()

	got := readThrough(t, conn, bufSize, expected.Len())
	require.Equal(t, expected.Bytes(), got)
}
