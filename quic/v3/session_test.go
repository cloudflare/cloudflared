package v3_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/rs/zerolog"

	v3 "github.com/kjake/cloudflared/quic/v3"
)

var (
	expectedContextCanceled = errors.New("expected context canceled")

	testOriginAddr = net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:0"))
	testLocalAddr  = net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:0"))
)

func TestSessionNew(t *testing.T) {
	log := zerolog.Nop()
	session := v3.NewSession(testRequestID, 5*time.Second, nil, testOriginAddr, testLocalAddr, &noopEyeball{}, &noopMetrics{}, &log)
	if testRequestID != session.ID() {
		t.Fatalf("session id doesn't match: %s != %s", testRequestID, session.ID())
	}
}

func testSessionWrite(t *testing.T, payload []byte) {
	log := zerolog.Nop()
	origin, server := net.Pipe()
	defer origin.Close()
	defer server.Close()
	// Start origin server read
	serverRead := make(chan []byte, 1)
	go func() {
		read := make([]byte, 1500)
		server.Read(read[:])
		serverRead <- read
	}()
	// Create session and write to origin
	session := v3.NewSession(testRequestID, 5*time.Second, origin, testOriginAddr, testLocalAddr, &noopEyeball{}, &noopMetrics{}, &log)
	n, err := session.Write(payload)
	defer session.Close()
	if err != nil {
		t.Fatal(err)
	}
	if n != len(payload) {
		t.Fatal("unable to write the whole payload")
	}

	read := <-serverRead
	if !slices.Equal(payload, read[:len(payload)]) {
		t.Fatal("payload provided from origin and read value are not the same")
	}
}

func TestSessionWrite_Max(t *testing.T) {
	defer leaktest.Check(t)()
	payload := makePayload(1280)
	testSessionWrite(t, payload)
}

func TestSessionWrite_Min(t *testing.T) {
	defer leaktest.Check(t)()
	payload := makePayload(0)
	testSessionWrite(t, payload)
}

func TestSessionServe_OriginMax(t *testing.T) {
	defer leaktest.Check(t)()
	payload := makePayload(1280)
	testSessionServe_Origin(t, payload)
}

func TestSessionServe_OriginMin(t *testing.T) {
	defer leaktest.Check(t)()
	payload := makePayload(0)
	testSessionServe_Origin(t, payload)
}

func testSessionServe_Origin(t *testing.T, payload []byte) {
	log := zerolog.Nop()
	origin, server := net.Pipe()
	defer origin.Close()
	defer server.Close()
	eyeball := newMockEyeball()
	session := v3.NewSession(testRequestID, 3*time.Second, origin, testOriginAddr, testLocalAddr, &eyeball, &noopMetrics{}, &log)
	defer session.Close()

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)
	done := make(chan error)
	go func() {
		done <- session.Serve(ctx)
	}()

	// Write from the origin server
	_, err := server.Write(payload)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case data := <-eyeball.recvData:
		// check received data matches provided from origin
		expectedData := makePayload(1500)
		v3.MarshalPayloadHeaderTo(testRequestID, expectedData[:])
		copy(expectedData[17:], payload)
		if !slices.Equal(expectedData[:v3.DatagramPayloadHeaderLen+len(payload)], data) {
			t.Fatal("expected datagram did not equal expected")
		}
		cancel(expectedContextCanceled)
	case err := <-ctx.Done():
		// we expect the payload to return before the context to cancel on the session
		t.Fatal(err)
	}

	err = <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !errors.Is(context.Cause(ctx), expectedContextCanceled) {
		t.Fatal(err)
	}
}

func TestSessionServe_OriginTooLarge(t *testing.T) {
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	eyeball := newMockEyeball()
	payload := makePayload(1281)
	origin, server := net.Pipe()
	defer origin.Close()
	defer server.Close()
	session := v3.NewSession(testRequestID, 2*time.Second, origin, testOriginAddr, testLocalAddr, &eyeball, &noopMetrics{}, &log)
	defer session.Close()

	done := make(chan error)
	go func() {
		done <- session.Serve(context.Background())
	}()

	// Attempt to write a payload too large from the origin
	_, err := server.Write(payload)
	if err != nil {
		t.Fatal(err)
	}

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
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	eyeball := newMockEyeball()
	pipe1, pipe2 := net.Pipe()
	session := v3.NewSession(testRequestID, 2*time.Second, pipe2, testOriginAddr, testLocalAddr, &eyeball, &noopMetrics{}, &log)
	defer session.Close()

	done := make(chan error)
	eyeball1Ctx, cancel := context.WithCancelCause(context.Background())
	go func() {
		done <- session.Serve(eyeball1Ctx)
	}()

	// Migrate the session to a new connection before origin sends data
	eyeball2 := newMockEyeball()
	eyeball2.connID = 1
	eyeball2Ctx := context.Background()
	session.Migrate(&eyeball2, eyeball2Ctx, &log)

	// Cancel the origin eyeball context; this should not cancel the session
	contextCancelErr := errors.New("context canceled for first eyeball connection")
	cancel(contextCancelErr)
	select {
	case <-done:
		t.Fatalf("expected session to still be running")
	default:
	}
	if context.Cause(eyeball1Ctx) != contextCancelErr {
		t.Fatalf("first eyeball context should be cancelled manually: %+v", context.Cause(eyeball1Ctx))
	}

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
	if eyeball2Ctx.Err() != nil {
		t.Fatalf("second eyeball context should be not be cancelled")
	}
}

func TestSessionServe_Migrate_CloseContext2(t *testing.T) {
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	eyeball := newMockEyeball()
	pipe1, pipe2 := net.Pipe()
	session := v3.NewSession(testRequestID, 2*time.Second, pipe2, testOriginAddr, testLocalAddr, &eyeball, &noopMetrics{}, &log)
	defer session.Close()

	done := make(chan error)
	eyeball1Ctx, cancel := context.WithCancelCause(context.Background())
	go func() {
		done <- session.Serve(eyeball1Ctx)
	}()

	// Migrate the session to a new connection before origin sends data
	eyeball2 := newMockEyeball()
	eyeball2.connID = 1
	eyeball2Ctx, cancel2 := context.WithCancelCause(context.Background())
	session.Migrate(&eyeball2, eyeball2Ctx, &log)

	// Cancel the origin eyeball context; this should not cancel the session
	contextCancelErr := errors.New("context canceled for first eyeball connection")
	cancel(contextCancelErr)
	select {
	case <-done:
		t.Fatalf("expected session to still be running")
	default:
	}
	if context.Cause(eyeball1Ctx) != contextCancelErr {
		t.Fatalf("first eyeball context should be cancelled manually: %+v", context.Cause(eyeball1Ctx))
	}

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

	// Close the connection2 context manually
	contextCancel2Err := errors.New("context canceled for second eyeball connection")
	cancel2(contextCancel2Err)
	err := <-done
	if err != context.Canceled {
		t.Fatalf("session Serve should be done: %+v", err)
	}
	if context.Cause(eyeball2Ctx) != contextCancel2Err {
		t.Fatalf("second eyeball context should have been cancelled manually: %+v", context.Cause(eyeball2Ctx))
	}
}

func TestSessionClose_Multiple(t *testing.T) {
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	origin, server := net.Pipe()
	defer origin.Close()
	defer server.Close()
	session := v3.NewSession(testRequestID, 5*time.Second, origin, testOriginAddr, testLocalAddr, &noopEyeball{}, &noopMetrics{}, &log)
	err := session.Close()
	if err != nil {
		t.Fatal(err)
	}
	b := [1500]byte{}
	_, err = server.Read(b[:])
	if !errors.Is(err, io.EOF) {
		t.Fatalf("origin server connection should be closed: %s", err)
	}
	// subsequent closes shouldn't call close again or cause any errors
	err = session.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.Read(b[:])
	if !errors.Is(err, io.EOF) {
		t.Fatalf("origin server connection should still be closed: %s", err)
	}
}

func TestSessionServe_IdleTimeout(t *testing.T) {
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	origin, server := net.Pipe()
	defer origin.Close()
	defer server.Close()
	closeAfterIdle := 2 * time.Second
	session := v3.NewSession(testRequestID, closeAfterIdle, origin, testOriginAddr, testLocalAddr, &noopEyeball{}, &noopMetrics{}, &log)
	err := session.Serve(context.Background())
	if !errors.Is(err, v3.SessionIdleErr{}) {
		t.Fatal(err)
	}
	// session should be closed
	b := [1500]byte{}
	_, err = server.Read(b[:])
	if !errors.Is(err, io.EOF) {
		t.Fatalf("session should be closed after Serve returns")
	}
	// closing a session again should not return an error
	err = session.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSessionServe_ParentContextCanceled(t *testing.T) {
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	origin, server := net.Pipe()
	defer origin.Close()
	defer server.Close()
	closeAfterIdle := 10 * time.Second

	session := v3.NewSession(testRequestID, closeAfterIdle, origin, testOriginAddr, testLocalAddr, &noopEyeball{}, &noopMetrics{}, &log)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := session.Serve(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	// session should be closed
	b := [1500]byte{}
	_, err = server.Read(b[:])
	if !errors.Is(err, io.EOF) {
		t.Fatalf("session should be closed after Serve returns")
	}
	// closing a session again should not return an error
	err = session.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSessionServe_ReadErrors(t *testing.T) {
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	origin := newTestErrOrigin(net.ErrClosed, nil)
	session := v3.NewSession(testRequestID, 30*time.Second, &origin, testOriginAddr, testLocalAddr, &noopEyeball{}, &noopMetrics{}, &log)
	err := session.Serve(context.Background())
	if !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
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
