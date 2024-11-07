package v3_test

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	v3 "github.com/cloudflare/cloudflared/quic/v3"
)

var expectedContextCanceled = errors.New("expected context canceled")

func TestSessionNew(t *testing.T) {
	log := zerolog.Nop()
	session := v3.NewSession(testRequestID, 5*time.Second, nil, &noopEyeball{}, &noopMetrics{}, &log)
	if testRequestID != session.ID() {
		t.Fatalf("session id doesn't match: %s != %s", testRequestID, session.ID())
	}
}

func testSessionWrite(t *testing.T, payload []byte) {
	log := zerolog.Nop()
	origin := newTestOrigin(makePayload(1280))
	session := v3.NewSession(testRequestID, 5*time.Second, &origin, &noopEyeball{}, &noopMetrics{}, &log)
	n, err := session.Write(payload)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(payload) {
		t.Fatal("unable to write the whole payload")
	}
	if !slices.Equal(payload, origin.write[:len(payload)]) {
		t.Fatal("payload provided from origin and read value are not the same")
	}
}

func TestSessionWrite_Max(t *testing.T) {
	payload := makePayload(1280)
	testSessionWrite(t, payload)
}

func TestSessionWrite_Min(t *testing.T) {
	payload := makePayload(0)
	testSessionWrite(t, payload)
}

func TestSessionServe_OriginMax(t *testing.T) {
	payload := makePayload(1280)
	testSessionServe_Origin(t, payload)
}

func TestSessionServe_OriginMin(t *testing.T) {
	payload := makePayload(0)
	testSessionServe_Origin(t, payload)
}

func testSessionServe_Origin(t *testing.T, payload []byte) {
	log := zerolog.Nop()
	eyeball := newMockEyeball()
	origin := newTestOrigin(payload)
	session := v3.NewSession(testRequestID, 3*time.Second, &origin, &eyeball, &noopMetrics{}, &log)
	defer session.Close()

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)
	done := make(chan error)
	go func() {
		done <- session.Serve(ctx)
	}()

	select {
	case data := <-eyeball.recvData:
		// check received data matches provided from origin
		expectedData := makePayload(1500)
		v3.MarshalPayloadHeaderTo(testRequestID, expectedData[:])
		copy(expectedData[17:], payload)
		if !slices.Equal(expectedData[:17+len(payload)], data) {
			t.Fatal("expected datagram did not equal expected")
		}
		cancel(expectedContextCanceled)
	case err := <-ctx.Done():
		// we expect the payload to return before the context to cancel on the session
		t.Fatal(err)
	}

	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !errors.Is(context.Cause(ctx), expectedContextCanceled) {
		t.Fatal(err)
	}
}

func TestSessionServe_OriginTooLarge(t *testing.T) {
	log := zerolog.Nop()
	eyeball := newMockEyeball()
	payload := makePayload(1281)
	origin := newTestOrigin(payload)
	session := v3.NewSession(testRequestID, 2*time.Second, &origin, &eyeball, &noopMetrics{}, &log)
	defer session.Close()

	done := make(chan error)
	go func() {
		done <- session.Serve(context.Background())
	}()

	select {
	case data := <-eyeball.recvData:
		// we never expect a read to make it here because the origin provided a payload that is too large
		// for cloudflared to proxy and it will drop it.
		t.Fatalf("we should never proxy a payload of this size: %d", len(data))
	case err := <-done:
		if !errors.Is(err, v3.SessionIdleErr{}) {
			t.Error(err)
		}
	}
}

func TestSessionServe_Migrate(t *testing.T) {
	log := zerolog.Nop()
	eyeball := newMockEyeball()
	pipe1, pipe2 := net.Pipe()
	session := v3.NewSession(testRequestID, 2*time.Second, pipe2, &eyeball, &noopMetrics{}, &log)
	defer session.Close()

	done := make(chan error)
	go func() {
		done <- session.Serve(context.Background())
	}()

	// Migrate the session to a new connection before origin sends data
	eyeball2 := newMockEyeball()
	eyeball2.connID = 1
	session.Migrate(&eyeball2)

	// Origin sends data
	payload2 := []byte{0xde}
	pipe1.Write(payload2)

	// Expect write to eyeball2
	data := <-eyeball2.recvData
	if len(data) <= 17 || !slices.Equal(payload2, data[17:]) {
		t.Fatalf("expected data to write to eyeball2 after migration: %+v", data)
	}

	select {
	case data := <-eyeball.recvData:
		t.Fatalf("expected no data to write to eyeball1 after migration: %+v", data)
	default:
	}

	err := <-done
	if !errors.Is(err, v3.SessionIdleErr{}) {
		t.Error(err)
	}
}

func TestSessionClose_Multiple(t *testing.T) {
	log := zerolog.Nop()
	origin := newTestOrigin(makePayload(128))
	session := v3.NewSession(testRequestID, 5*time.Second, &origin, &noopEyeball{}, &noopMetrics{}, &log)
	err := session.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !origin.closed.Load() {
		t.Fatal("origin wasn't closed")
	}
	// subsequent closes shouldn't call close again or cause any errors
	err = session.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSessionServe_IdleTimeout(t *testing.T) {
	log := zerolog.Nop()
	origin := newTestIdleOrigin(10 * time.Second) // Make idle time longer than closeAfterIdle
	closeAfterIdle := 2 * time.Second
	session := v3.NewSession(testRequestID, closeAfterIdle, &origin, &noopEyeball{}, &noopMetrics{}, &log)
	err := session.Serve(context.Background())
	if !errors.Is(err, v3.SessionIdleErr{}) {
		t.Fatal(err)
	}
	// session should be closed
	if !origin.closed {
		t.Fatalf("session should be closed after Serve returns")
	}
	// closing a session again should not return an error
	err = session.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSessionServe_ParentContextCanceled(t *testing.T) {
	log := zerolog.Nop()
	// Make idle time and idle timeout longer than closeAfterIdle
	origin := newTestIdleOrigin(10 * time.Second)
	closeAfterIdle := 10 * time.Second

	session := v3.NewSession(testRequestID, closeAfterIdle, &origin, &noopEyeball{}, &noopMetrics{}, &log)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := session.Serve(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	// session should be closed
	if !origin.closed {
		t.Fatalf("session should be closed after Serve returns")
	}
	// closing a session again should not return an error
	err = session.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSessionServe_ReadErrors(t *testing.T) {
	log := zerolog.Nop()
	origin := newTestErrOrigin(net.ErrClosed, nil)
	session := v3.NewSession(testRequestID, 30*time.Second, &origin, &noopEyeball{}, &noopMetrics{}, &log)
	err := session.Serve(context.Background())
	if !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

type testOrigin struct {
	// bytes from Write
	write []byte
	// bytes provided to Read
	read     []byte
	readOnce atomic.Bool
	closed   atomic.Bool
}

func newTestOrigin(payload []byte) testOrigin {
	return testOrigin{
		read: payload,
	}
}

func (o *testOrigin) Read(p []byte) (n int, err error) {
	if o.closed.Load() {
		return -1, net.ErrClosed
	}
	if o.readOnce.Load() {
		// We only want to provide one read so all other reads will be blocked
		time.Sleep(10 * time.Second)
	}
	o.readOnce.Store(true)
	return copy(p, o.read), nil
}

func (o *testOrigin) Write(p []byte) (n int, err error) {
	if o.closed.Load() {
		return -1, net.ErrClosed
	}
	o.write = make([]byte, len(p))
	copy(o.write, p)
	return len(p), nil
}

func (o *testOrigin) Close() error {
	o.closed.Store(true)
	return nil
}

type testIdleOrigin struct {
	duration time.Duration
	closed   bool
}

func newTestIdleOrigin(d time.Duration) testIdleOrigin {
	return testIdleOrigin{
		duration: d,
	}
}

func (o *testIdleOrigin) Read(p []byte) (n int, err error) {
	time.Sleep(o.duration)
	return -1, nil
}

func (o *testIdleOrigin) Write(p []byte) (n int, err error) {
	return 0, nil
}

func (o *testIdleOrigin) Close() error {
	o.closed = true
	return nil
}

type testErrOrigin struct {
	readErr  error
	writeErr error
}

func newTestErrOrigin(readErr error, writeErr error) testErrOrigin {
	return testErrOrigin{readErr, writeErr}
}

func (o *testErrOrigin) Read(p []byte) (n int, err error) {
	return 0, o.readErr
}

func (o *testErrOrigin) Write(p []byte) (n int, err error) {
	return len(p), o.writeErr
}

func (o *testErrOrigin) Close() error {
	return nil
}
