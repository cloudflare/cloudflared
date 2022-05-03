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
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestManagerServe(t *testing.T) {
	const (
		sessions            = 20
		msgs                = 50
		remoteUnregisterMsg = "eyeball closed connection"
	)

	mg, transport := newTestManager(1)

	eyeballTracker := make(map[uuid.UUID]*datagramChannel)
	for i := 0; i < sessions; i++ {
		sessionID := uuid.New()
		eyeballTracker[sessionID] = newDatagramChannel(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan struct{})
	go func(ctx context.Context) {
		mg.Serve(ctx)
		close(serveDone)
	}(ctx)

	go func(ctx context.Context) {
		for {
			sessionID, payload, err := transport.respChan.Receive(ctx)
			if err != nil {
				require.Equal(t, context.Canceled, err)
				return
			}
			respChan := eyeballTracker[sessionID]
			require.NoError(t, respChan.Send(ctx, sessionID, payload))
		}
	}(ctx)

	errGroup, ctx := errgroup.WithContext(ctx)
	for sID, receiver := range eyeballTracker {
		// Assign loop variables to local variables
		sessionID := sID
		eyeballRespReceiver := receiver
		errGroup.Go(func() error {
			payload := testPayload(sessionID)
			expectResp := testResponse(payload)

			cfdConn, originConn := net.Pipe()

			origin := mockOrigin{
				expectMsgCount: msgs,
				expectedMsg:    payload,
				expectedResp:   expectResp,
				conn:           originConn,
			}
			eyeball := mockEyeball{
				expectMsgCount:  msgs,
				expectedMsg:     expectResp,
				expectSessionID: sessionID,
				respReceiver:    eyeballRespReceiver,
			}

			reqErrGroup, reqCtx := errgroup.WithContext(ctx)
			reqErrGroup.Go(func() error {
				return origin.serve()
			})
			reqErrGroup.Go(func() error {
				return eyeball.serve(reqCtx)
			})

			session, err := mg.RegisterSession(ctx, sessionID, cfdConn)
			require.NoError(t, err)

			sessionDone := make(chan struct{})
			go func() {
				closedByRemote, err := session.Serve(ctx, time.Minute*2)
				closeSession := &errClosedSession{
					message:  remoteUnregisterMsg,
					byRemote: true,
				}
				require.Equal(t, closeSession, err)
				require.True(t, closedByRemote)
				close(sessionDone)
			}()

			for i := 0; i < msgs; i++ {
				require.NoError(t, transport.newRequest(ctx, sessionID, testPayload(sessionID)))
			}

			// Make sure eyeball and origin have received all messages before unregistering the session
			require.NoError(t, reqErrGroup.Wait())

			require.NoError(t, mg.UnregisterSession(ctx, sessionID, remoteUnregisterMsg, true))
			<-sessionDone

			return nil
		})
	}

	require.NoError(t, errGroup.Wait())
	cancel()
	transport.close()
	<-serveDone
}

func TestTimeout(t *testing.T) {
	const (
		testTimeout = time.Millisecond * 50
	)

	mg, _ := newTestManager(1)
	mg.timeout = testTimeout
	ctx := context.Background()
	sessionID := uuid.New()
	// session manager is not running, so event loop is not running and therefore calling the APIs should timeout
	session, err := mg.RegisterSession(ctx, sessionID, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, session)

	err = mg.UnregisterSession(ctx, sessionID, "session gone", true)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestCloseTransportCloseSessions(t *testing.T) {
	mg, transport := newTestManager(1)
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := mg.Serve(ctx)
		require.Error(t, err)
	}()

	cfdConn, eyeballConn := net.Pipe()
	session, err := mg.RegisterSession(ctx, uuid.New(), cfdConn)
	require.NoError(t, err)
	require.NotNil(t, session)

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := eyeballConn.Write([]byte(t.Name()))
		require.NoError(t, err)
		transport.close()
	}()

	closedByRemote, err := session.Serve(ctx, time.Minute)
	require.True(t, closedByRemote)
	require.Error(t, err)

	wg.Wait()
}

func newTestManager(capacity uint) (*manager, *mockQUICTransport) {
	log := zerolog.Nop()
	transport := &mockQUICTransport{
		reqChan:  newDatagramChannel(capacity),
		respChan: newDatagramChannel(capacity),
	}
	return NewManager(transport, &log), transport
}

type mockOrigin struct {
	expectMsgCount int
	expectedMsg    []byte
	expectedResp   []byte
	conn           io.ReadWriteCloser
}

func (mo *mockOrigin) serve() error {
	expectedMsgLen := len(mo.expectedMsg)
	readBuffer := make([]byte, expectedMsgLen+1)
	for i := 0; i < mo.expectMsgCount; i++ {
		n, err := mo.conn.Read(readBuffer)
		if err != nil {
			return err
		}
		if n != expectedMsgLen {
			return fmt.Errorf("Expect to read %d bytes, read %d", expectedMsgLen, n)
		}
		if !bytes.Equal(readBuffer[:n], mo.expectedMsg) {
			return fmt.Errorf("Expect %v, read %v", mo.expectedMsg, readBuffer[:n])
		}

		_, err = mo.conn.Write(mo.expectedResp)
		if err != nil {
			return err
		}
	}
	return nil
}

func testPayload(sessionID uuid.UUID) []byte {
	return []byte(fmt.Sprintf("Message from %s", sessionID))
}

func testResponse(msg []byte) []byte {
	return []byte(fmt.Sprintf("Response to %v", msg))
}

type mockEyeball struct {
	expectMsgCount  int
	expectedMsg     []byte
	expectSessionID uuid.UUID
	respReceiver    *datagramChannel
}

func (me *mockEyeball) serve(ctx context.Context) error {
	for i := 0; i < me.expectMsgCount; i++ {
		sessionID, msg, err := me.respReceiver.Receive(ctx)
		if err != nil {
			return err
		}
		if sessionID != me.expectSessionID {
			return fmt.Errorf("Expect session %s, got %s", me.expectSessionID, sessionID)
		}
		if !bytes.Equal(msg, me.expectedMsg) {
			return fmt.Errorf("Expect %v, read %v", me.expectedMsg, msg)
		}
	}
	return nil
}

// datagramChannel is a channel for Datagram with wrapper to send/receive with context
type datagramChannel struct {
	datagramChan chan *newDatagram
	closedChan   chan struct{}
}

func newDatagramChannel(capacity uint) *datagramChannel {
	return &datagramChannel{
		datagramChan: make(chan *newDatagram, capacity),
		closedChan:   make(chan struct{}),
	}
}

func (rc *datagramChannel) Send(ctx context.Context, sessionID uuid.UUID, payload []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-rc.closedChan:
		return &errClosedSession{
			message:  fmt.Errorf("datagram channel closed").Error(),
			byRemote: true,
		}
	case rc.datagramChan <- &newDatagram{sessionID: sessionID, payload: payload}:
		return nil
	}
}

func (rc *datagramChannel) Receive(ctx context.Context) (uuid.UUID, []byte, error) {
	select {
	case <-ctx.Done():
		return uuid.Nil, nil, ctx.Err()
	case <-rc.closedChan:
		err := &errClosedSession{
			message:  fmt.Errorf("datagram channel closed").Error(),
			byRemote: true,
		}
		return uuid.Nil, nil, err
	case msg := <-rc.datagramChan:
		return msg.sessionID, msg.payload, nil
	}
}

func (rc *datagramChannel) Close() {
	// No need to close msgChan, it will be garbage collect once there is no reference to it
	close(rc.closedChan)
}
