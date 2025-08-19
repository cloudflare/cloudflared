package connection

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	pkgerrors "github.com/pkg/errors"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	cfdflow "github.com/cloudflare/cloudflared/flow"

	"github.com/cloudflare/cloudflared/datagramsession"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/management"
	"github.com/cloudflare/cloudflared/packet"
	cfdquic "github.com/cloudflare/cloudflared/quic"
	"github.com/cloudflare/cloudflared/tracing"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	rpcquic "github.com/cloudflare/cloudflared/tunnelrpc/quic"
)

const (
	// emperically this capacity has been working well
	demuxChanCapacity = 16
)

var (
	errInvalidDestinationIP = errors.New("unable to parse destination IP")
)

// DatagramSessionHandler is a service that can serve datagrams for a connection and handle sessions from incoming
// connection streams.
type DatagramSessionHandler interface {
	Serve(context.Context) error

	pogs.SessionManager
}

type datagramV2Connection struct {
	conn  quic.Connection
	index uint8

	// sessionManager tracks active sessions. It receives datagrams from quic connection via datagramMuxer
	sessionManager datagramsession.Manager
	// flowLimiter tracks active sessions across the tunnel and limits new sessions if they are above the limit.
	flowLimiter cfdflow.Limiter

	// datagramMuxer mux/demux datagrams from quic connection
	datagramMuxer *cfdquic.DatagramMuxerV2
	// originDialer is the origin dialer for UDP requests
	originDialer ingress.OriginUDPDialer
	// packetRouter acts as the origin router for ICMP requests
	packetRouter *ingress.PacketRouter

	rpcTimeout         time.Duration
	streamWriteTimeout time.Duration

	logger *zerolog.Logger
}

func NewDatagramV2Connection(ctx context.Context,
	conn quic.Connection,
	originDialer ingress.OriginUDPDialer,
	icmpRouter ingress.ICMPRouter,
	index uint8,
	rpcTimeout time.Duration,
	streamWriteTimeout time.Duration,
	flowLimiter cfdflow.Limiter,
	logger *zerolog.Logger,
) DatagramSessionHandler {
	sessionDemuxChan := make(chan *packet.Session, demuxChanCapacity)
	datagramMuxer := cfdquic.NewDatagramMuxerV2(conn, logger, sessionDemuxChan)
	sessionManager := datagramsession.NewManager(logger, datagramMuxer.SendToSession, sessionDemuxChan)
	packetRouter := ingress.NewPacketRouter(icmpRouter, datagramMuxer, index, logger)

	return &datagramV2Connection{
		conn:               conn,
		index:              index,
		sessionManager:     sessionManager,
		flowLimiter:        flowLimiter,
		datagramMuxer:      datagramMuxer,
		originDialer:       originDialer,
		packetRouter:       packetRouter,
		rpcTimeout:         rpcTimeout,
		streamWriteTimeout: streamWriteTimeout,
		logger:             logger,
	}
}

func (d *datagramV2Connection) Serve(ctx context.Context) error {
	// If either goroutine from the errgroup returns at all (error or nil), we rely on its cancellation to make sure
	// the other goroutines as well.
	errGroup, ctx := errgroup.WithContext(ctx)

	errGroup.Go(func() error {
		return d.sessionManager.Serve(ctx)
	})
	errGroup.Go(func() error {
		return d.datagramMuxer.ServeReceive(ctx)
	})
	errGroup.Go(func() error {
		return d.packetRouter.Serve(ctx)
	})

	return errGroup.Wait()
}

// RegisterUdpSession is the RPC method invoked by edge to register and run a session
func (q *datagramV2Connection) RegisterUdpSession(ctx context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16, closeAfterIdleHint time.Duration, traceContext string) (*tunnelpogs.RegisterUdpSessionResponse, error) {
	traceCtx := tracing.NewTracedContext(ctx, traceContext, q.logger)
	ctx, registerSpan := traceCtx.Tracer().Start(traceCtx, "register-session", trace.WithAttributes(
		attribute.String("session-id", sessionID.String()),
		attribute.String("dst", fmt.Sprintf("%s:%d", dstIP, dstPort)),
	))
	log := q.logger.With().Int(management.EventTypeKey, int(management.UDP)).Logger()

	// Try to start a new session
	if err := q.flowLimiter.Acquire(management.UDP.String()); err != nil {
		log.Warn().Msgf("Too many concurrent sessions being handled, rejecting udp proxy to %s:%d", dstIP, dstPort)

		err := pkgerrors.Wrap(err, "failed to start udp session due to rate limiting")
		tracing.EndWithErrorStatus(registerSpan, err)
		return nil, err
	}
	// We need to force the net.IP to IPv4 (if it's an IPv4 address) otherwise the net.IP conversion from capnp
	// will be a IPv4-mapped-IPv6 address.
	// In the case that the address is IPv6 we leave it untouched and parse it as normal.
	ip := dstIP.To4()
	if ip == nil {
		ip = dstIP
	}
	// Parse the dstIP and dstPort into a netip.AddrPort
	// This should never fail because the IP was already parsed as a valid net.IP
	destAddr, ok := netip.AddrFromSlice(ip)
	if !ok {
		log.Err(errInvalidDestinationIP).Msgf("Failed to parse destination proxy IP: %s", ip)
		tracing.EndWithErrorStatus(registerSpan, errInvalidDestinationIP)
		q.flowLimiter.Release()
		return nil, errInvalidDestinationIP
	}
	dstAddrPort := netip.AddrPortFrom(destAddr, dstPort)

	// Each session is a series of datagram from an eyeball to a dstIP:dstPort.
	// (src port, dst IP, dst port) uniquely identifies a session, so it needs a dedicated connected socket.
	originProxy, err := q.originDialer.DialUDP(dstAddrPort)
	if err != nil {
		log.Err(err).Msgf("Failed to create udp proxy to %s", dstAddrPort)
		tracing.EndWithErrorStatus(registerSpan, err)
		q.flowLimiter.Release()
		return nil, err
	}
	registerSpan.SetAttributes(
		attribute.Bool("socket-bind-success", true),
		attribute.String("src", originProxy.LocalAddr().String()),
	)

	session, err := q.sessionManager.RegisterSession(ctx, sessionID, originProxy)
	if err != nil {
		originProxy.Close()
		log.Err(err).Str(datagramsession.LogFieldSessionID, datagramsession.FormatSessionID(sessionID)).Msgf("Failed to register udp session")
		tracing.EndWithErrorStatus(registerSpan, err)
		q.flowLimiter.Release()
		return nil, err
	}

	go func() {
		defer q.flowLimiter.Release() // we do the release here, instead of inside the `serveUDPSession` just to keep all acquire/release calls in the same method.
		q.serveUDPSession(session, closeAfterIdleHint)
	}()

	log.Debug().
		Str(datagramsession.LogFieldSessionID, datagramsession.FormatSessionID(sessionID)).
		Str("src", originProxy.LocalAddr().String()).
		Str("dst", fmt.Sprintf("%s:%d", dstIP, dstPort)).
		Msgf("Registered session")
	tracing.End(registerSpan)

	resp := tunnelpogs.RegisterUdpSessionResponse{
		Spans: traceCtx.GetProtoSpans(),
	}

	return &resp, nil
}

// UnregisterUdpSession is the RPC method invoked by edge to unregister and terminate a sesssion
func (q *datagramV2Connection) UnregisterUdpSession(ctx context.Context, sessionID uuid.UUID, message string) error {
	return q.sessionManager.UnregisterSession(ctx, sessionID, message, true)
}

func (q *datagramV2Connection) serveUDPSession(session *datagramsession.Session, closeAfterIdleHint time.Duration) {
	ctx := q.conn.Context()
	closedByRemote, err := session.Serve(ctx, closeAfterIdleHint)
	// If session is terminated by remote, then we know it has been unregistered from session manager and edge
	if !closedByRemote {
		if err != nil {
			q.closeUDPSession(ctx, session.ID, err.Error())
		} else {
			q.closeUDPSession(ctx, session.ID, "terminated without error")
		}
	}
	q.logger.Debug().Err(err).
		Int(management.EventTypeKey, int(management.UDP)).
		Str(datagramsession.LogFieldSessionID, datagramsession.FormatSessionID(session.ID)).
		Msg("Session terminated")
}

// closeUDPSession first unregisters the session from session manager, then it tries to unregister from edge
func (q *datagramV2Connection) closeUDPSession(ctx context.Context, sessionID uuid.UUID, message string) {
	_ = q.sessionManager.UnregisterSession(ctx, sessionID, message, false)
	quicStream, err := q.conn.OpenStream()
	if err != nil {
		// Log this at debug because this is not an error if session was closed due to lost connection
		// with edge
		q.logger.Debug().Err(err).
			Int(management.EventTypeKey, int(management.UDP)).
			Str(datagramsession.LogFieldSessionID, datagramsession.FormatSessionID(sessionID)).
			Msgf("Failed to open quic stream to unregister udp session with edge")
		return
	}

	stream := cfdquic.NewSafeStreamCloser(quicStream, q.streamWriteTimeout, q.logger)
	defer stream.Close()
	rpcClientStream, err := rpcquic.NewSessionClient(ctx, stream, q.rpcTimeout)
	if err != nil {
		// Log this at debug because this is not an error if session was closed due to lost connection
		// with edge
		q.logger.Err(err).Str(datagramsession.LogFieldSessionID, datagramsession.FormatSessionID(sessionID)).
			Msgf("Failed to open rpc stream to unregister udp session with edge")
		return
	}
	defer rpcClientStream.Close()

	if err := rpcClientStream.UnregisterUdpSession(ctx, sessionID, message); err != nil {
		q.logger.Err(err).Str(datagramsession.LogFieldSessionID, datagramsession.FormatSessionID(sessionID)).
			Msgf("Failed to unregister udp session with edge")
	}
}
