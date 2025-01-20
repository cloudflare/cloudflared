package connection

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/kjake/cloudflared/datagramsession"
	"github.com/kjake/cloudflared/ingress"
	"github.com/kjake/cloudflared/management"
	"github.com/kjake/cloudflared/packet"
	cfdquic "github.com/kjake/cloudflared/quic"
	"github.com/kjake/cloudflared/tracing"
	"github.com/kjake/cloudflared/tunnelrpc/pogs"
	tunnelpogs "github.com/kjake/cloudflared/tunnelrpc/pogs"
	rpcquic "github.com/kjake/cloudflared/tunnelrpc/quic"
)

const (
	// emperically this capacity has been working well
	demuxChanCapacity = 16
)

// DatagramSessionHandler is a service that can serve datagrams for a connection and handle sessions from incoming
// connection streams.
type DatagramSessionHandler interface {
	Serve(context.Context) error

	pogs.SessionManager
}

type datagramV2Connection struct {
	conn quic.Connection

	// sessionManager tracks active sessions. It receives datagrams from quic connection via datagramMuxer
	sessionManager datagramsession.Manager
	// datagramMuxer mux/demux datagrams from quic connection
	datagramMuxer *cfdquic.DatagramMuxerV2
	packetRouter  *ingress.PacketRouter

	rpcTimeout         time.Duration
	streamWriteTimeout time.Duration

	logger *zerolog.Logger
}

func NewDatagramV2Connection(ctx context.Context,
	conn quic.Connection,
	icmpRouter ingress.ICMPRouter,
	index uint8,
	rpcTimeout time.Duration,
	streamWriteTimeout time.Duration,
	logger *zerolog.Logger,
) DatagramSessionHandler {
	sessionDemuxChan := make(chan *packet.Session, demuxChanCapacity)
	datagramMuxer := cfdquic.NewDatagramMuxerV2(conn, logger, sessionDemuxChan)
	sessionManager := datagramsession.NewManager(logger, datagramMuxer.SendToSession, sessionDemuxChan)
	packetRouter := ingress.NewPacketRouter(icmpRouter, datagramMuxer, index, logger)

	return &datagramV2Connection{
		conn,
		sessionManager,
		datagramMuxer,
		packetRouter,
		rpcTimeout,
		streamWriteTimeout,
		logger,
	}
}

func (d *datagramV2Connection) Serve(ctx context.Context) error {
	// If either goroutine returns nil error, we rely on this cancellation to make sure the other goroutine exits
	// as fast as possible as well. Nil error means we want to exit for good (caller code won't retry serving this
	// connection).
	// If either goroutine returns a non nil error, then the error group cancels the context, thus also canceling the
	// other goroutine as fast as possible.
	ctx, cancel := context.WithCancel(ctx)
	errGroup, ctx := errgroup.WithContext(ctx)

	errGroup.Go(func() error {
		defer cancel()
		return d.sessionManager.Serve(ctx)
	})
	errGroup.Go(func() error {
		defer cancel()
		return d.datagramMuxer.ServeReceive(ctx)
	})
	errGroup.Go(func() error {
		defer cancel()
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
	// Each session is a series of datagram from an eyeball to a dstIP:dstPort.
	// (src port, dst IP, dst port) uniquely identifies a session, so it needs a dedicated connected socket.
	originProxy, err := ingress.DialUDP(dstIP, dstPort)
	if err != nil {
		log.Err(err).Msgf("Failed to create udp proxy to %s:%d", dstIP, dstPort)
		tracing.EndWithErrorStatus(registerSpan, err)
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
		return nil, err
	}

	go q.serveUDPSession(session, closeAfterIdleHint)

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
	q.sessionManager.UnregisterSession(ctx, sessionID, message, false)
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
