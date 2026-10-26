package v3_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/google/gopacket/layers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"

	cfdflow "github.com/cloudflare/cloudflared/flow"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/packet"
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
func (noopEyeball) SendICMPPacket(icmp *packet.ICMP) error                                { return nil }
func (noopEyeball) SendICMPTTLExceed(icmp *packet.ICMP, rawPacket packet.RawPacket) error { return nil }

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

func (m *mockEyeball) SendICMPPacket(icmp *packet.ICMP) error { return nil }
func (m *mockEyeball) SendICMPTTLExceed(icmp *packet.ICMP, rawPacket packet.RawPacket) error {
	return nil
}

func TestDatagramConn_New(t *testing.T) {
	log := zerolog.Nop()
	originDialerService := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer:   testDefaultDialer,
		TCPWriteTimeout: 0,
	}, &log)
	conn := v3.NewDatagramConn(newMockQuicConn(t.Context()), v3.NewSessionManager(&noopMetrics{}, &log, originDialerService, cfdflow.NewLimiter(0)), &noopICMPRouter{}, 0, &noopMetrics{}, &log)
	if conn == nil {
		t.Fatal("expected valid connection")
	}
}

func TestDatagramConn_SendUDPSessionDatagram(t *testing.T) {
	log := zerolog.Nop()
	originDialerService := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer:   testDefaultDialer,
		TCPWriteTimeout: 0,
	}, &log)
	connCtx, connCancel := context.WithCancelCause(t.Context())
	defer connCancel(context.Canceled)
	quic := newMockQuicConn(connCtx)
	conn := v3.NewDatagramConn(quic, v3.NewSessionManager(&noopMetrics{}, &log, originDialerService, cfdflow.NewLimiter(0)), &noopICMPRouter{}, 0, &noopMetrics{}, &log)

	payload := []byte{0xef, 0xef}
	err := conn.SendUDPSessionDatagram(payload)
	require.NoError(t, err)

	p := <-quic.recv
	if !slices.Equal(p, payload) {
		t.Fatal("datagram sent does not match datagram received on quic side")
	}
}

func TestDatagramConn_SendUDPSessionResponse(t *testing.T) {
	log := zerolog.Nop()
	originDialerService := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer:   testDefaultDialer,
		TCPWriteTimeout: 0,
	}, &log)
	connCtx, connCancel := context.WithCancelCause(t.Context())
	defer connCancel(context.Canceled)
	quic := newMockQuicConn(connCtx)
	conn := v3.NewDatagramConn(quic, v3.NewSessionManager(&noopMetrics{}, &log, originDialerService, cfdflow.NewLimiter(0)), &noopICMPRouter{}, 0, &noopMetrics{}, &log)

	err := conn.SendUDPSessionResponse(testRequestID, v3.ResponseDestinationUnreachable)
	require.NoError(t, err)

	resp := <-quic.recv
	var response v3.UDPSessionRegistrationResponseDatagram
	err = response.UnmarshalBinary(resp)
	require.NoError(t, err)

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
	originDialerService := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer:   testDefaultDialer,
		TCPWriteTimeout: 0,
	}, &log)
	connCtx, connCancel := context.WithCancelCause(t.Context())
	defer connCancel(context.Canceled)
	quic := newMockQuicConn(connCtx)
	conn := v3.NewDatagramConn(quic, v3.NewSessionManager(&noopMetrics{}, &log, originDialerService, cfdflow.NewLimiter(0)), &noopICMPRouter{}, 0, &noopMetrics{}, &log)

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()
	err := conn.Serve(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestDatagramConnServe_ConnectionClosed(t *testing.T) {
	log := zerolog.Nop()
	originDialerService := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer:   testDefaultDialer,
		TCPWriteTimeout: 0,
	}, &log)
	connCtx, connCancel := context.WithCancelCause(t.Context())
	defer connCancel(context.Canceled)
	quic := newMockQuicConn(connCtx)
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()
	quic.ctx = ctx
	conn := v3.NewDatagramConn(quic, v3.NewSessionManager(&noopMetrics{}, &log, originDialerService, cfdflow.NewLimiter(0)), &noopICMPRouter{}, 0, &noopMetrics{}, &log)

	err := conn.Serve(t.Context())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestDatagramConnServe_ReceiveDatagramError(t *testing.T) {
	log := zerolog.Nop()
	originDialerService := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer:   testDefaultDialer,
		TCPWriteTimeout: 0,
	}, &log)
	quic := &mockQuicConnReadError{err: net.ErrClosed}
	conn := v3.NewDatagramConn(quic, v3.NewSessionManager(&noopMetrics{}, &log, originDialerService, cfdflow.NewLimiter(0)), &noopICMPRouter{}, 0, &noopMetrics{}, &log)

	err := conn.Serve(t.Context())
	if !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

func TestDatagramConnServe_SessionRegistrationRateLimit(t *testing.T) {
	log := zerolog.Nop()
	connCtx, connCancel := context.WithCancelCause(t.Context())
	defer connCancel(context.Canceled)
	quic := newMockQuicConn(connCtx)
	sessionManager := &mockSessionManager{
		expectedRegErr: v3.ErrSessionRegistrationRateLimited,
	}
	conn := v3.NewDatagramConn(quic, sessionManager, &noopICMPRouter{}, 0, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(context.Canceled)
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

	require.EqualValues(t, testRequestID, resp.RequestID)
	require.EqualValues(t, v3.ResponseTooManyActiveFlows, resp.ResponseType)

	assertContextClosed(t, ctx, done, cancel)
}

func TestDatagramConnServe_ErrorDatagramTypes(t *testing.T) {
	defer leaktest.Check(t)()
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
			connCtx, connCancel := context.WithCancelCause(t.Context())
			defer connCancel(context.Canceled)
			quic := newMockQuicConn(connCtx)
			quic.send <- test.input
			conn := v3.NewDatagramConn(quic, &mockSessionManager{}, &noopICMPRouter{}, 0, &noopMetrics{}, &log)

			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
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
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	connCtx, connCancel := context.WithCancelCause(t.Context())
	defer connCancel(context.Canceled)
	quic := newMockQuicConn(connCtx)
	expectedErr := errors.New("unable to register session")
	sessionManager := mockSessionManager{expectedRegErr: expectedErr}
	conn := v3.NewDatagramConn(quic, &sessionManager, &noopICMPRouter{}, 0, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(t.Context())
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
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	connCtx, connCancel := context.WithCancelCause(t.Context())
	defer connCancel(context.Canceled)
	quic := newMockQuicConn(connCtx)
	session := newMockSession()
	sessionManager := mockSessionManager{session: &session}
	conn := v3.NewDatagramConn(quic, &sessionManager, &noopICMPRouter{}, 0, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(t.Context())
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

// This test exists because decoding multiple packets in parallel with the same decoder
// instances causes inteference resulting in multiple different raw packets being decoded
// as the same decoded packet.
func TestDatagramConnServeDecodeMultipleICMPInParallel(t *testing.T) {
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	connCtx, connCancel := context.WithCancelCause(t.Context())
	defer connCancel(context.Canceled)
	quic := newMockQuicConn(connCtx)
	session := newMockSession()
	sessionManager := mockSessionManager{session: &session}
	router := newMockICMPRouter()
	conn := v3.NewDatagramConn(quic, &sessionManager, router, 0, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(errors.New("other error"))
	done := make(chan error, 1)
	go func() {
		done <- conn.Serve(ctx)
	}()

	packetCount := 100
	packets := make([]*packet.ICMP, 100)
	ipTemplate := "10.0.0.%d"
	for i := 1; i <= packetCount; i++ {
		packets[i-1] = &packet.ICMP{
			IP: &packet.IP{
				Src:      netip.MustParseAddr("192.168.1.1"),
				Dst:      netip.MustParseAddr(fmt.Sprintf(ipTemplate, i)),
				Protocol: layers.IPProtocolICMPv4,
				TTL:      20,
			},
			Message: &icmp.Message{
				Type: ipv4.ICMPTypeEcho,
				Code: 0,
				Body: &icmp.Echo{
					ID:   25821,
					Seq:  58129,
					Data: []byte("test"),
				},
			},
		}
	}

	wg := sync.WaitGroup{}
	var receivedPackets []*packet.ICMP
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case icmpPacket := <-router.recv:
				receivedPackets = append(receivedPackets, icmpPacket)
				wg.Done()
			}
		}
	}()

	for _, p := range packets {
		// We increment here but only decrement when receiving the packet
		wg.Add(1)
		go func() {
			datagram := newICMPDatagram(p)
			quic.send <- datagram
		}()
	}

	wg.Wait()

	// If there were duplicates then we won't have the same number of IPs
	packetSet := make(map[netip.Addr]*packet.ICMP, 0)
	for _, p := range receivedPackets {
		packetSet[p.Dst] = p
	}
	assert.Equal(t, len(packetSet), len(packets))

	// Sort the slice by last byte of IP address (the one we increment for each destination)
	// and then check that we have one match for each packet sent
	sort.Slice(receivedPackets, func(i, j int) bool {
		return receivedPackets[i].Dst.As4()[3] < receivedPackets[j].Dst.As4()[3]
	})
	for i, p := range receivedPackets {
		assert.Equal(t, p.Dst, packets[i].Dst)
	}

	// Cancel the muxer Serve context and make sure it closes with the expected error
	assertContextClosed(t, ctx, done, cancel)
}

func TestDatagramConnServe_RegisterTwice(t *testing.T) {
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	connCtx, connCancel := context.WithCancelCause(t.Context())
	defer connCancel(context.Canceled)
	quic := newMockQuicConn(connCtx)
	session := newMockSession()
	sessionManager := mockSessionManager{session: &session}
	conn := v3.NewDatagramConn(quic, &sessionManager, &noopICMPRouter{}, 0, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(t.Context())
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
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	connCtx, connCancel := context.WithCancelCause(t.Context())
	defer connCancel(context.Canceled)
	quic := newMockQuicConn(connCtx)
	session := newMockSession()
	sessionManager := mockSessionManager{session: &session}
	conn := v3.NewDatagramConn(quic, &sessionManager, &noopICMPRouter{}, 0, &noopMetrics{}, &log)
	conn2Ctx, conn2Cancel := context.WithCancelCause(t.Context())
	defer conn2Cancel(context.Canceled)
	quic2 := newMockQuicConn(conn2Ctx)
	conn2 := v3.NewDatagramConn(quic2, &sessionManager, &noopICMPRouter{}, 1, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(errors.New("other error"))
	done := make(chan error, 1)
	go func() {
		done <- conn.Serve(ctx)
	}()

	ctx2, cancel2 := context.WithCancelCause(t.Context())
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
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	connCtx, connCancel := context.WithCancelCause(t.Context())
	defer connCancel(context.Canceled)
	quic := newMockQuicConn(connCtx)
	// mockSessionManager will return the ErrSessionNotFound for any session attempting to be queried by the muxer
	sessionManager := mockSessionManager{session: nil, expectedGetErr: v3.ErrSessionNotFound}
	conn := v3.NewDatagramConn(quic, &sessionManager, &noopICMPRouter{}, 0, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(t.Context())
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

func TestDatagramConnServe_Payloads(t *testing.T) {
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	connCtx, connCancel := context.WithCancelCause(t.Context())
	defer connCancel(context.Canceled)
	quic := newMockQuicConn(connCtx)
	session := newMockSession()
	sessionManager := mockSessionManager{session: &session}
	conn := v3.NewDatagramConn(quic, &sessionManager, &noopICMPRouter{}, 0, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(errors.New("other error"))
	done := make(chan error, 1)
	go func() {
		done <- conn.Serve(ctx)
	}()

	// Send session payloads
	expectedPayloads := makePayloads(256, 16)
	go func() {
		for _, payload := range expectedPayloads {
			datagram := newSessionPayloadDatagram(testRequestID, payload)
			quic.send <- datagram
		}
	}()

	// Session should receive the payloads (in-order)
	for i, payload := range expectedPayloads {
		select {
		case recv := <-session.recv:
			if !slices.Equal(recv, payload) {
				t.Fatalf("expected session receieve the payload[%d] sent via the muxer: (%x) (%x)", i, recv[:16], payload[:16])
			}
		case err := <-ctx.Done():
			// we expect the payload to return before the context to cancel on the session
			t.Fatal(err)
		}
	}

	// Cancel the muxer Serve context and make sure it closes with the expected error
	assertContextClosed(t, ctx, done, cancel)
}

func TestDatagramConnServe_ICMPDatagram_TTLDecremented(t *testing.T) {
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	connCtx, connCancel := context.WithCancelCause(t.Context())
	defer connCancel(context.Canceled)
	quic := newMockQuicConn(connCtx)
	router := newMockICMPRouter()
	conn := v3.NewDatagramConn(quic, &mockSessionManager{}, router, 0, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(errors.New("other error"))
	done := make(chan error, 1)
	go func() {
		done <- conn.Serve(ctx)
	}()

	// Send new ICMP Echo request
	expectedICMP := &packet.ICMP{
		IP: &packet.IP{
			Src:      netip.MustParseAddr("192.168.1.1"),
			Dst:      netip.MustParseAddr("10.0.0.1"),
			Protocol: layers.IPProtocolICMPv4,
			TTL:      20,
		},
		Message: &icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   25821,
				Seq:  58129,
				Data: []byte("test ttl=0"),
			},
		},
	}
	datagram := newICMPDatagram(expectedICMP)
	quic.send <- datagram

	// Router should receive the packet
	actualICMP := <-router.recv
	assertICMPEqual(t, expectedICMP, actualICMP)
	if expectedICMP.TTL-1 != actualICMP.TTL {
		t.Fatalf("TTL should be decremented by one before sending to origin: %d, %d", expectedICMP.TTL, actualICMP.TTL)
	}

	// Cancel the muxer Serve context and make sure it closes with the expected error
	assertContextClosed(t, ctx, done, cancel)
}

func TestDatagramConnServe_ICMPDatagram_TTLExceeded(t *testing.T) {
	defer leaktest.Check(t)()
	log := zerolog.Nop()
	connCtx, connCancel := context.WithCancelCause(t.Context())
	defer connCancel(context.Canceled)
	quic := newMockQuicConn(connCtx)
	router := newMockICMPRouter()
	conn := v3.NewDatagramConn(quic, &mockSessionManager{}, router, 0, &noopMetrics{}, &log)

	// Setup the muxer
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(errors.New("other error"))
	done := make(chan error, 1)
	go func() {
		done <- conn.Serve(ctx)
	}()

	// Send new ICMP Echo request
	expectedICMP := &packet.ICMP{
		IP: &packet.IP{
			Src:      netip.MustParseAddr("192.168.1.1"),
			Dst:      netip.MustParseAddr("10.0.0.1"),
			Protocol: layers.IPProtocolICMPv4,
			TTL:      0,
		},
		Message: &icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   25821,
				Seq:  58129,
				Data: []byte("test ttl=0"),
			},
		},
	}
	datagram := newICMPDatagram(expectedICMP)
	quic.send <- datagram

	// Origin should not receive a packet
	select {
	case <-router.recv:
		t.Fatalf("TTL should be expired and no origin ICMP sent")
	default:
	}

	// Eyeball should receive the packet
	datagram = <-quic.recv
	icmpDatagram := v3.ICMPDatagram{}
	err := icmpDatagram.UnmarshalBinary(datagram)
	if err != nil {
		t.Fatal(err)
	}
	decoder := packet.NewICMPDecoder()
	ttlExpiredICMP, err := decoder.Decode(packet.RawPacket{Data: icmpDatagram.Payload})
	if err != nil {
		t.Fatal(err)
	}

	// Packet should be a TTL Exceeded ICMP
	if ttlExpiredICMP.TTL != packet.DefaultTTL || ttlExpiredICMP.Message.Type != ipv4.ICMPTypeTimeExceeded {
		t.Fatalf("ICMP packet should be a ICMP Exceeded: %+v", ttlExpiredICMP)
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

func newSessionPayloadDatagram(id v3.RequestID, payload []byte) []byte {
	datagram := make([]byte, len(payload)+17)
	err := v3.MarshalPayloadHeaderTo(id, datagram[:])
	if err != nil {
		panic(err)
	}
	copy(datagram[17:], payload)
	return datagram
}

func newICMPDatagram(pk *packet.ICMP) []byte {
	encoder := packet.NewEncoder()
	rawPacket, err := encoder.Encode(pk)
	if err != nil {
		panic(err)
	}
	datagram := v3.ICMPDatagram{
		Payload: rawPacket.Data,
	}
	payload, err := datagram.MarshalBinary()
	if err != nil {
		panic(err)
	}
	return payload
}

// Cancel the provided context and make sure it closes with the expected cancellation error
func assertContextClosed(t *testing.T, ctx context.Context, done <-chan error, cancel context.CancelCauseFunc) {
	cancel(errExpectedContextCanceled)
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !errors.Is(context.Cause(ctx), errExpectedContextCanceled) {
		t.Fatal(err)
	}
}

type mockQuicConn struct {
	ctx  context.Context
	send chan []byte
	recv chan []byte
}

func newMockQuicConn(ctx context.Context) *mockQuicConn {
	return &mockQuicConn{
		ctx:  ctx,
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
	select {
	case <-m.ctx.Done():
		return nil, m.ctx.Err()
	case b := <-m.send:
		return b, nil
	}
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

func (m *mockSession) ID() v3.RequestID     { return testRequestID }
func (m *mockSession) RemoteAddr() net.Addr { return testOriginAddr }
func (m *mockSession) LocalAddr() net.Addr  { return testLocalAddr }
func (m *mockSession) ConnectionID() uint8  { return 0 }
func (m *mockSession) Migrate(conn v3.DatagramConn, ctx context.Context, log *zerolog.Logger) {
	m.migrated <- conn.ID()
}
func (m *mockSession) ResetIdleTimer() {}

func (m *mockSession) Serve(ctx context.Context) error {
	close(m.served)
	return v3.SessionCloseErr
}

func (m *mockSession) Write(payload []byte) {
	b := make([]byte, len(payload))
	copy(b, payload)
	m.recv <- b
}

func (m *mockSession) Close() error {
	return nil
}
