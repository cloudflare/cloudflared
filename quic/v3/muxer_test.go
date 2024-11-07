package v3_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/ingress"
	v3 "github.com/cloudflare/cloudflared/quic/v3"
)

type noopEyeball struct {
	connID uint8
}

func (noopEyeball) Serve(ctx context.Context) error              { return nil }
func (n noopEyeball) ID() uint8                                  { return n.connID }
func (noopEyeball) SendUDPSessionDatagram(datagram []byte) error { return nil }
func (noopEyeball) SendUDPSessionResponse(id v3.RequestID, resp v3.SessionRegistrationResp) error {
	return nil
}

type mockEyeball struct {
	connID uint8
	// datagram sent via SendUDPSessionDatagram
	recvData chan []byte
	// responses sent via SendUDPSessionResponse
	recvResp chan struct {
		id   v3.RequestID
		resp v3.SessionRegistrationResp
	}
}

func newMockEyeball() mockEyeball {
	return mockEyeball{
		connID:   0,
		recvData: make(chan []byte, 1),
		recvResp: make(chan struct {
			id   v3.RequestID
			resp v3.SessionRegistrationResp
		}, 1),
	}
}

func (mockEyeball) Serve(ctx context.Context) error { return nil }
func (m *mockEyeball) ID() uint8                    { return m.connID }

func (m *mockEyeball) SendUDPSessionDatagram(datagram []byte) error {
	b := make([]byte, len(datagram))
	copy(b, datagram)
	m.recvData <- b
	return nil
}

func (m *mockEyeball) SendUDPSessionResponse(id v3.RequestID, resp v3.SessionRegistrationResp) error {
	m.recvResp <- struct {
		id   v3.RequestID
		resp v3.SessionRegistrationResp
	}{
		id, resp,
	}
	return nil
}

func TestDatagramConn_New(t *testing.T) {
	log := zerolog.Nop()
	conn := v3.NewDatagramConn(newMockQuicConn(), v3.NewSessionManager(&noopMetrics{}, &log, ingress.DialUDPAddrPort), 0, &noopMetrics{}, &log)
	if conn == nil {
		t.Fatal("expected valid connection")
	}
}

func TestDatagramConn_SendUDPSessionDatagram(t *testing.T) {
	log := zerolog.Nop()
	quic := newMockQuicConn()
	conn := v3.NewDatagramConn(quic, v3.NewSessionManager(&noopMetrics{}, &log, ingress.DialUDPAddrPort), 0, &noopMetrics{}, &log)

	payload := []byte{0xef, 0xef}
	conn.SendUDPSessionDatagram(payload)
	p := <-quic.recv
	if !slices.Equal(p, payload) {
		t.Fatal("datagram sent does not match datagram received on quic side")
	}
}

func TestDatagramConn_SendUDPSessionResponse(t *testing.T) {
	log := zerolog.Nop()
	quic := newMockQuicConn()
	conn := v3.NewDatagramConn(quic, v3.NewSessionManager(&noopMetrics{}, &log, ingress.DialUDPAddrPort), 0, &noopMetrics{}, &log)

	conn.SendUDPSessionResponse(testRequestID, v3.ResponseDestinationUnreachable)
	resp := <-quic.recv
	var response v3.UDPSessionRegistrationResponseDatagram
	err := response.UnmarshalBinary(resp)
	if err != nil {
		t.Fatal(err)
	}
	expected := v3.UDPSessionRegistrationResponseDatagram{
		RequestID:    testRequestID,
		ResponseType: v3.ResponseDestinationUnreachable,
	}
	if response != expected {
		t.Fatal("datagram response sent does not match expected datagram response received")
	}
}

func TestDatagramConnServe_ApplicationClosed(t *testing.T) {
	log := zerolog.Nop()
	quic := newMockQuicConn()
	conn := v3.NewDatagramConn(quic, v3.NewSessionManager(&noopMetrics{}, &log, ingress.DialUDPAddrPort), 0, &noopMetrics{}, &log)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	err := conn.Serve(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestDatagramConnServe_ConnectionClosed(t *testing.T) {
	log := zerolog.Nop()
	quic := newMockQuicConn()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	quic.ctx = ctx
	conn := v3.NewDatagramConn(quic, v3.NewSessionManager(&noopMetrics{}, &log, ingress.DialUDPAddrPort), 0, &noopMetrics{}, &log)

	err := conn.Serve(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestDatagramConnServe_ReceiveDatagramError(t *testing.T) {
	log := zerolog.Nop()
	quic := &mockQuicConnReadError{err: net.ErrClosed}
	conn := v3.NewDatagramConn(quic, v3.NewSessionManager(&noopMetrics{}, &log, ingress.DialUDPAddrPort), 0, &noopMetrics{}, &log)

	err := conn.Serve(context.Background())
	if !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

func TestDatagramConnServe_ErrorDatagramTypes(t *testing.T) {
	for _, test := range []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			"empty",
			[]byte{},
			"{\"level\":\"error\",\"datagramVersion\":3,\"error\":\"datagram should have at least 1 byte\",\"message\":\"unable to parse datagram type: 0\"}\n",
		},
		{
			"unexpected",
			[]byte{byte(v3.UDPSessionRegistrationResponseType)},
			"{\"level\":\"error\",\"datagramVersion\":3,\"message\":\"unexpected datagram type received: 3\"}\n",
		},
		{
			"unknown",
			[]byte{99},
			"{\"level\":\"error\",\"datagramVersion\":3,\"message\":\"unknown datagram type received: 99\"}\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			logOutput := new(LockedBuffer)
			log := zerolog.New(logOutput)
			quic := newMockQuicConn()
			quic.send <- test.input
			conn := v3.NewDatagramConn(quic, &mockSessionManager{}, 0, &noopMetrics{}, &log)

			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			err := conn.Serve(ctx)
			// we cancel the Serve method to check to see if the log output was written since the unsupported datagram
			// is dropped with only a log message as a side-effect.
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}

			out := logOutput.String()
			if out != test.expected {
				t.Fatalf("incorrect log output expected: %s", out)
			}
		})
	}
}

type LockedBuffer struct {
	bytes.Buffer
	l sync.Mutex
}

func (b *LockedBuffer) Write(p []byte) (n int, err error) {
	b.l.Lock()
	defer b.l.Unlock()
	return b.Buffer.Write(p)
}

func (b *LockedBuffer) String() string {
	b.l.Lock()
	defer b.l.Unlock()
	return b.Buffer.String()
}

func TestDatagramConnServe_RegisterSession_SessionManagerError(t *testing.T) {
	log := zerolog.Nop()
	quic := newMockQuicConn()
	expectedErr := errors.New("unable to register session")
	sessionManager := mockSessionManager{expectedRegErr: expectedErr}
	conn := v3.NewDatagramConn(quic, &sessionManager, 0, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(errors.New("other error"))
	done := make(chan error, 1)
	go func() {
		done <- conn.Serve(ctx)
	}()

	// Send new session registration
	datagram := newRegisterSessionDatagram(testRequestID)
	quic.send <- datagram

	// Wait for session registration response with failure
	datagram = <-quic.recv
	var resp v3.UDPSessionRegistrationResponseDatagram
	err := resp.UnmarshalBinary(datagram)
	if err != nil {
		t.Fatal(err)
	}

	if resp.RequestID != testRequestID || resp.ResponseType != v3.ResponseUnableToBindSocket {
		t.Fatalf("expected registration response failure")
	}

	// Cancel the muxer Serve context and make sure it closes with the expected error
	assertContextClosed(t, ctx, done, cancel)
}

func TestDatagramConnServe(t *testing.T) {
	log := zerolog.Nop()
	quic := newMockQuicConn()
	session := newMockSession()
	sessionManager := mockSessionManager{session: &session}
	conn := v3.NewDatagramConn(quic, &sessionManager, 0, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(errors.New("other error"))
	done := make(chan error, 1)
	go func() {
		done <- conn.Serve(ctx)
	}()

	// Send new session registration
	datagram := newRegisterSessionDatagram(testRequestID)
	quic.send <- datagram

	// Wait for session registration response with success
	datagram = <-quic.recv
	var resp v3.UDPSessionRegistrationResponseDatagram
	err := resp.UnmarshalBinary(datagram)
	if err != nil {
		t.Fatal(err)
	}

	if resp.RequestID != testRequestID || resp.ResponseType != v3.ResponseOk {
		t.Fatalf("expected registration response ok")
	}

	// We expect the session to be served
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case <-session.served:
		break
	case <-timer.C:
		t.Fatalf("expected session serve to be called")
	}

	// Cancel the muxer Serve context and make sure it closes with the expected error
	assertContextClosed(t, ctx, done, cancel)
}

func TestDatagramConnServe_RegisterTwice(t *testing.T) {
	log := zerolog.Nop()
	quic := newMockQuicConn()
	session := newMockSession()
	sessionManager := mockSessionManager{session: &session}
	conn := v3.NewDatagramConn(quic, &sessionManager, 0, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(errors.New("other error"))
	done := make(chan error, 1)
	go func() {
		done <- conn.Serve(ctx)
	}()

	// Send new session registration
	datagram := newRegisterSessionDatagram(testRequestID)
	quic.send <- datagram

	// Wait for session registration response with success
	datagram = <-quic.recv
	var resp v3.UDPSessionRegistrationResponseDatagram
	err := resp.UnmarshalBinary(datagram)
	if err != nil {
		t.Fatal(err)
	}

	if resp.RequestID != testRequestID || resp.ResponseType != v3.ResponseOk {
		t.Fatalf("expected registration response ok")
	}

	// Set the session manager to return already registered
	sessionManager.expectedRegErr = v3.ErrSessionAlreadyRegistered
	// Send the registration again as if we didn't receive it at the edge
	datagram = newRegisterSessionDatagram(testRequestID)
	quic.send <- datagram

	// Wait for session registration response with success
	datagram = <-quic.recv
	err = resp.UnmarshalBinary(datagram)
	if err != nil {
		t.Fatal(err)
	}

	if resp.RequestID != testRequestID || resp.ResponseType != v3.ResponseOk {
		t.Fatalf("expected registration response ok")
	}

	// We expect the session to be served
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case <-session.served:
		break
	case <-timer.C:
		t.Fatalf("expected session serve to be called")
	}

	// Cancel the muxer Serve context and make sure it closes with the expected error
	assertContextClosed(t, ctx, done, cancel)
}

func TestDatagramConnServe_MigrateConnection(t *testing.T) {
	log := zerolog.Nop()
	quic := newMockQuicConn()
	session := newMockSession()
	sessionManager := mockSessionManager{session: &session}
	conn := v3.NewDatagramConn(quic, &sessionManager, 0, &noopMetrics{}, &log)
	quic2 := newMockQuicConn()
	conn2 := v3.NewDatagramConn(quic2, &sessionManager, 1, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(errors.New("other error"))
	done := make(chan error, 1)
	go func() {
		done <- conn.Serve(ctx)
	}()

	ctx2, cancel2 := context.WithCancelCause(context.Background())
	defer cancel2(errors.New("other error"))
	done2 := make(chan error, 1)
	go func() {
		done2 <- conn2.Serve(ctx2)
	}()

	// Send new session registration
	datagram := newRegisterSessionDatagram(testRequestID)
	quic.send <- datagram

	// Wait for session registration response with success
	datagram = <-quic.recv
	var resp v3.UDPSessionRegistrationResponseDatagram
	err := resp.UnmarshalBinary(datagram)
	if err != nil {
		t.Fatal(err)
	}

	if resp.RequestID != testRequestID || resp.ResponseType != v3.ResponseOk {
		t.Fatalf("expected registration response ok")
	}

	// Set the session manager to return already registered to another connection
	sessionManager.expectedRegErr = v3.ErrSessionBoundToOtherConn
	// Send the registration again as if we didn't receive it at the edge for a new connection
	datagram = newRegisterSessionDatagram(testRequestID)
	quic2.send <- datagram

	// Wait for session registration response with success
	datagram = <-quic2.recv
	err = resp.UnmarshalBinary(datagram)
	if err != nil {
		t.Fatal(err)
	}

	if resp.RequestID != testRequestID || resp.ResponseType != v3.ResponseOk {
		t.Fatalf("expected registration response ok")
	}

	// We expect the session to be served
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case <-session.served:
		break
	case <-timer.C:
		t.Fatalf("expected session serve to be called")
	}

	// Expect session to be migrated
	select {
	case id := <-session.migrated:
		if id != conn2.ID() {
			t.Fatalf("expected session to be migrated to connection 2")
		}
	case <-timer.C:
		t.Fatalf("expected session migration to be called")
	}

	// Cancel the muxer Serve context and make sure it closes with the expected error
	assertContextClosed(t, ctx, done, cancel)
	// Cancel the second muxer Serve context and make sure it closes with the expected error
	assertContextClosed(t, ctx2, done2, cancel2)
}

func TestDatagramConnServe_Payload_GetSessionError(t *testing.T) {
	log := zerolog.Nop()
	quic := newMockQuicConn()
	// mockSessionManager will return the ErrSessionNotFound for any session attempting to be queried by the muxer
	sessionManager := mockSessionManager{session: nil, expectedGetErr: v3.ErrSessionNotFound}
	conn := v3.NewDatagramConn(quic, &sessionManager, 0, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(errors.New("other error"))
	done := make(chan error, 1)
	go func() {
		done <- conn.Serve(ctx)
	}()

	// Send new session registration
	datagram := newSessionPayloadDatagram(testRequestID, []byte{0xef, 0xef})
	quic.send <- datagram

	// Since the muxer should eventually discard a failed registration request, there is no side-effect
	// that the registration was failed beyond the muxer accepting the registration request. As such, the
	// test can only ensure that the quic.send channel was consumed and that the muxer closes normally
	// afterwards with the expected context cancelled trigger.

	// Cancel the muxer Serve context and make sure it closes with the expected error
	assertContextClosed(t, ctx, done, cancel)
}

func TestDatagramConnServe_Payload(t *testing.T) {
	log := zerolog.Nop()
	quic := newMockQuicConn()
	session := newMockSession()
	sessionManager := mockSessionManager{session: &session}
	conn := v3.NewDatagramConn(quic, &sessionManager, 0, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(errors.New("other error"))
	done := make(chan error, 1)
	go func() {
		done <- conn.Serve(ctx)
	}()

	// Send new session registration
	expectedPayload := []byte{0xef, 0xef}
	datagram := newSessionPayloadDatagram(testRequestID, expectedPayload)
	quic.send <- datagram

	// Session should receive the payload
	payload := <-session.recv
	if !slices.Equal(expectedPayload, payload) {
		t.Fatalf("expected session receieve the payload sent via the muxer")
	}

	// Cancel the muxer Serve context and make sure it closes with the expected error
	assertContextClosed(t, ctx, done, cancel)
}

func newRegisterSessionDatagram(id v3.RequestID) []byte {
	datagram := v3.UDPSessionRegistrationDatagram{
		RequestID:        id,
		Dest:             netip.MustParseAddrPort("127.0.0.1:8080"),
		IdleDurationHint: 5 * time.Second,
	}
	payload, err := datagram.MarshalBinary()
	if err != nil {
		panic(err)
	}
	return payload
}

func newRegisterResponseSessionDatagram(id v3.RequestID, resp v3.SessionRegistrationResp) []byte {
	datagram := v3.UDPSessionRegistrationResponseDatagram{
		RequestID:    id,
		ResponseType: resp,
	}
	payload, err := datagram.MarshalBinary()
	if err != nil {
		panic(err)
	}
	return payload
}

func newSessionPayloadDatagram(id v3.RequestID, payload []byte) []byte {
	datagram := make([]byte, len(payload)+17)
	err := v3.MarshalPayloadHeaderTo(id, datagram[:])
	if err != nil {
		panic(err)
	}
	copy(datagram[17:], payload)
	return datagram
}

// Cancel the provided context and make sure it closes with the expected cancellation error
func assertContextClosed(t *testing.T, ctx context.Context, done <-chan error, cancel context.CancelCauseFunc) {
	cancel(expectedContextCanceled)
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !errors.Is(context.Cause(ctx), expectedContextCanceled) {
		t.Fatal(err)
	}
}

type mockQuicConn struct {
	ctx  context.Context
	send chan []byte
	recv chan []byte
}

func newMockQuicConn() *mockQuicConn {
	return &mockQuicConn{
		ctx:  context.Background(),
		send: make(chan []byte, 1),
		recv: make(chan []byte, 1),
	}
}

func (m *mockQuicConn) Context() context.Context {
	return m.ctx
}

func (m *mockQuicConn) SendDatagram(payload []byte) error {
	b := make([]byte, len(payload))
	copy(b, payload)
	m.recv <- b
	return nil
}

func (m *mockQuicConn) ReceiveDatagram(_ context.Context) ([]byte, error) {
	return <-m.send, nil
}

type mockQuicConnReadError struct {
	err error
}

func (m *mockQuicConnReadError) Context() context.Context {
	return context.Background()
}

func (m *mockQuicConnReadError) SendDatagram(payload []byte) error {
	return nil
}

func (m *mockQuicConnReadError) ReceiveDatagram(_ context.Context) ([]byte, error) {
	return nil, m.err
}

type mockSessionManager struct {
	session v3.Session

	expectedRegErr error
	expectedGetErr error
}

func (m *mockSessionManager) RegisterSession(request *v3.UDPSessionRegistrationDatagram, conn v3.DatagramConn) (v3.Session, error) {
	return m.session, m.expectedRegErr
}

func (m *mockSessionManager) GetSession(requestID v3.RequestID) (v3.Session, error) {
	return m.session, m.expectedGetErr
}

func (m *mockSessionManager) UnregisterSession(requestID v3.RequestID) {}

type mockSession struct {
	served   chan struct{}
	migrated chan uint8
	recv     chan []byte
}

func newMockSession() mockSession {
	return mockSession{
		served:   make(chan struct{}),
		migrated: make(chan uint8, 2),
		recv:     make(chan []byte, 1),
	}
}

func (m *mockSession) ID() v3.RequestID {
	return testRequestID
}

func (m *mockSession) ConnectionID() uint8 {
	return 0
}

func (m *mockSession) Migrate(conn v3.DatagramConn) { m.migrated <- conn.ID() }
func (m *mockSession) ResetIdleTimer()              {}

func (m *mockSession) Serve(ctx context.Context) error {
	close(m.served)
	return v3.SessionCloseErr
}

func (m *mockSession) Write(payload []byte) (n int, err error) {
	b := make([]byte, len(payload))
	copy(b, payload)
	m.recv <- b
	return len(b), nil
}

func (m *mockSession) Close() error {
	return nil
}
