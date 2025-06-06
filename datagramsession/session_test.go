package datagramsession

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/packet"
)

// TestCloseSession makes sure a session will stop after context is done
func TestSessionCtxDone(t *testing.T) {
	testSessionReturns(t, closeByContext, time.Minute*2)
}

// TestCloseSession makes sure a session will stop after close method is called
func TestCloseSession(t *testing.T) {
	testSessionReturns(t, closeByCallingClose, time.Minute*2)
}

// TestCloseIdle makess sure a session will stop after there is no read/write for a period defined by closeAfterIdle
func TestCloseIdle(t *testing.T) {
	testSessionReturns(t, closeByTimeout, time.Millisecond*100)
}

func testSessionReturns(t *testing.T, closeBy closeMethod, closeAfterIdle time.Duration) {
	localCloseReason := &errClosedSession{
		message:  "connection closed by origin",
		byRemote: false,
	}
	sessionID := uuid.New()
	cfdConn, originConn := net.Pipe()
	payload := testPayload(sessionID)

	log := zerolog.Nop()
	mg := NewManager(&log, nil, nil)
	session := mg.newSession(sessionID, cfdConn)

	ctx, cancel := context.WithCancel(t.Context())
	sessionDone := make(chan struct{})
	go func() {
		closedByRemote, err := session.Serve(ctx, closeAfterIdle)
		switch closeBy {
		case closeByContext:
			assert.Equal(t, context.Canceled, err)
			assert.False(t, closedByRemote)
		case closeByCallingClose:
			assert.Equal(t, localCloseReason, err)
			assert.Equal(t, localCloseReason.byRemote, closedByRemote)
		case closeByTimeout:
			assert.Equal(t, SessionIdleErr(closeAfterIdle), err)
			assert.False(t, closedByRemote)
		}
		close(sessionDone)
	}()

	go func() {
		n, err := session.transportToDst(payload)
		assert.NoError(t, err)
		assert.Equal(t, len(payload), n)
	}()

	readBuffer := make([]byte, len(payload)+1)
	n, err := originConn.Read(readBuffer)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)

	lastRead := time.Now()

	switch closeBy {
	case closeByContext:
		cancel()
	case closeByCallingClose:
		session.close(localCloseReason)
	default:
		// ignore
	}

	<-sessionDone
	if closeBy == closeByTimeout {
		require.True(t, time.Now().After(lastRead.Add(closeAfterIdle)))
	}
	// call cancelled again otherwise the linter will warn about possible context leak
	cancel()
}

type closeMethod int

const (
	closeByContext closeMethod = iota
	closeByCallingClose
	closeByTimeout
)

func TestWriteToDstSessionPreventClosed(t *testing.T) {
	testActiveSessionNotClosed(t, false, true)
}

func TestReadFromDstSessionPreventClosed(t *testing.T) {
	testActiveSessionNotClosed(t, true, false)
}

func testActiveSessionNotClosed(t *testing.T, readFromDst bool, writeToDst bool) {
	const closeAfterIdle = time.Millisecond * 100
	const activeTime = time.Millisecond * 500

	sessionID := uuid.New()
	cfdConn, originConn := net.Pipe()
	payload := testPayload(sessionID)

	respChan := make(chan *packet.Session)
	sender := newMockTransportSender(sessionID, payload)
	mg := NewManager(&nopLogger, sender.muxSession, respChan)
	session := mg.newSession(sessionID, cfdConn)

	startTime := time.Now()
	activeUntil := startTime.Add(activeTime)
	ctx, cancel := context.WithCancel(t.Context())
	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		_, _ = session.Serve(ctx, closeAfterIdle)
		if time.Now().Before(startTime.Add(activeTime)) {
			return fmt.Errorf("session closed while it's still active")
		}
		return nil
	})

	if readFromDst {
		errGroup.Go(func() error {
			for {
				if time.Now().After(activeUntil) {
					return nil
				}
				if _, err := originConn.Write(payload); err != nil {
					return err
				}
				time.Sleep(closeAfterIdle / 2)
			}
		})
	}
	if writeToDst {
		errGroup.Go(func() error {
			readBuffer := make([]byte, len(payload))
			for {
				n, err := originConn.Read(readBuffer)
				if err != nil {
					if err == io.EOF || err == io.ErrClosedPipe {
						return nil
					}
					return err
				}
				if !bytes.Equal(payload, readBuffer[:n]) {
					return fmt.Errorf("payload %v is not equal to %v", readBuffer[:n], payload)
				}
			}
		})
		errGroup.Go(func() error {
			for {
				if time.Now().After(activeUntil) {
					return nil
				}
				if _, err := session.transportToDst(payload); err != nil {
					return err
				}
				time.Sleep(closeAfterIdle / 2)
			}
		})
	}

	require.NoError(t, errGroup.Wait())
	cancel()
}

func TestMarkActiveNotBlocking(t *testing.T) {
	const concurrentCalls = 50
	mg := NewManager(&nopLogger, nil, nil)
	session := mg.newSession(uuid.New(), nil)
	var wg sync.WaitGroup
	wg.Add(concurrentCalls)
	for i := 0; i < concurrentCalls; i++ {
		go func() {
			session.markActive()
			wg.Done()
		}()
	}
	wg.Wait()
}

// Some UDP application might send 0-size payload.
func TestZeroBytePayload(t *testing.T) {
	sessionID := uuid.New()
	cfdConn, originConn := net.Pipe()

	sender := sendOnceTransportSender{
		baseSender: newMockTransportSender(sessionID, make([]byte, 0)),
		sentChan:   make(chan struct{}),
	}
	mg := NewManager(&nopLogger, sender.muxSession, nil)
	session := mg.newSession(sessionID, cfdConn)

	ctx, cancel := context.WithCancel(t.Context())
	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		// Read from underlying conn and send to transport
		closedByRemote, err := session.Serve(ctx, time.Minute*2)
		require.Equal(t, context.Canceled, err)
		require.False(t, closedByRemote)
		return nil
	})

	errGroup.Go(func() error {
		// Write to underlying connection
		n, err := originConn.Write([]byte{})
		require.NoError(t, err)
		require.Equal(t, 0, n)
		return nil
	})

	<-sender.sentChan
	cancel()
	require.NoError(t, errGroup.Wait())
}

type mockTransportSender struct {
	expectedSessionID uuid.UUID
	expectedPayload   []byte
}

func newMockTransportSender(expectedSessionID uuid.UUID, expectedPayload []byte) *mockTransportSender {
	return &mockTransportSender{
		expectedSessionID: expectedSessionID,
		expectedPayload:   expectedPayload,
	}
}

func (mts *mockTransportSender) muxSession(session *packet.Session) error {
	if session.ID != mts.expectedSessionID {
		return fmt.Errorf("Expect session %s, got %s", mts.expectedSessionID, session.ID)
	}
	if !bytes.Equal(session.Payload, mts.expectedPayload) {
		return fmt.Errorf("Expect %v, read %v", mts.expectedPayload, session.Payload)
	}
	return nil
}

type sendOnceTransportSender struct {
	baseSender *mockTransportSender
	sentChan   chan struct{}
}

func (sots *sendOnceTransportSender) muxSession(session *packet.Session) error {
	defer close(sots.sentChan)
	return sots.baseSender.muxSession(session)
}
