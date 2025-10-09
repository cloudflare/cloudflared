package v3

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/packet"
)

const (
	// Allocating a 16 channel buffer here allows for the writer to be slightly faster than the reader.
	// This has worked previously well for datagramv2, so we will start with this as well
	demuxChanCapacity = 16
	// This provides a small buffer for the PacketRouter to poll ICMP packets from the QUIC connection
	// before writing them to the origin.
	icmpDatagramChanCapacity = 128

	logSrcKey      = "src"
	logDstKey      = "dst"
	logICMPTypeKey = "type"
	logDurationKey = "durationMS"
)

// DatagramConn is the bridge that multiplexes writes and reads of datagrams for UDP sessions and ICMP packets to
// a connection.
type DatagramConn interface {
	DatagramUDPWriter
	DatagramICMPWriter
	// Serve provides a server interface to process and handle incoming QUIC datagrams and demux their datagram v3 payloads.
	Serve(context.Context) error
	// ID indicates connection index identifier
	ID() uint8
}

// DatagramUDPWriter provides the Muxer interface to create proper UDP Datagrams when sending over a connection.
type DatagramUDPWriter interface {
	SendUDPSessionDatagram(datagram []byte) error
	SendUDPSessionResponse(id RequestID, resp SessionRegistrationResp) error
}

// DatagramICMPWriter provides the Muxer interface to create ICMP Datagrams when sending over a connection.
type DatagramICMPWriter interface {
	SendICMPPacket(icmp *packet.ICMP) error
	SendICMPTTLExceed(icmp *packet.ICMP, rawPacket packet.RawPacket) error
}

// QuicConnection provides an interface that matches [quic.Connection] for only the datagram operations.
//
// We currently rely on the mutex for the [quic.Connection.SendDatagram] and [quic.Connection.ReceiveDatagram] and
// do not have any locking for them. If the implementation in quic-go were to ever change, we would need to make
// sure that we lock properly on these operations.
type QuicConnection interface {
	Context() context.Context
	SendDatagram(payload []byte) error
	ReceiveDatagram(context.Context) ([]byte, error)
}

type datagramConn struct {
	conn             QuicConnection
	index            uint8
	sessionManager   SessionManager
	icmpRouter       ingress.ICMPRouter
	metrics          Metrics
	logger           *zerolog.Logger
	datagrams        chan []byte
	icmpDatagramChan chan *ICMPDatagram
	readErrors       chan error

	icmpEncoderPool sync.Pool // a pool of *packet.Encoder
	icmpDecoderPool sync.Pool
}

func NewDatagramConn(conn QuicConnection, sessionManager SessionManager, icmpRouter ingress.ICMPRouter, index uint8, metrics Metrics, logger *zerolog.Logger) DatagramConn {
	log := logger.With().Uint8("datagramVersion", 3).Logger()
	return &datagramConn{
		conn:             conn,
		index:            index,
		sessionManager:   sessionManager,
		icmpRouter:       icmpRouter,
		metrics:          metrics,
		logger:           &log,
		datagrams:        make(chan []byte, demuxChanCapacity),
		icmpDatagramChan: make(chan *ICMPDatagram, icmpDatagramChanCapacity),
		readErrors:       make(chan error, 2),
		icmpEncoderPool: sync.Pool{
			New: func() any {
				return packet.NewEncoder()
			},
		},
		icmpDecoderPool: sync.Pool{
			New: func() any {
				return packet.NewICMPDecoder()
			},
		},
	}
}

func (c *datagramConn) ID() uint8 {
	return c.index
}

func (c *datagramConn) SendUDPSessionDatagram(datagram []byte) error {
	return c.conn.SendDatagram(datagram)
}

func (c *datagramConn) SendUDPSessionResponse(id RequestID, resp SessionRegistrationResp) error {
	datagram := UDPSessionRegistrationResponseDatagram{
		RequestID:    id,
		ResponseType: resp,
	}
	data, err := datagram.MarshalBinary()
	if err != nil {
		return err
	}
	return c.conn.SendDatagram(data)
}

func (c *datagramConn) SendICMPPacket(icmp *packet.ICMP) error {
	cachedEncoder := c.icmpEncoderPool.Get()
	// The encoded packet is a slice to a buffer owned by the encoder, so we shouldn't return the encoder back to the
	// pool until the encoded packet is sent.
	defer c.icmpEncoderPool.Put(cachedEncoder)
	encoder, ok := cachedEncoder.(*packet.Encoder)
	if !ok {
		return fmt.Errorf("encoderPool returned %T, expect *packet.Encoder", cachedEncoder)
	}
	payload, err := encoder.Encode(icmp)
	if err != nil {
		return err
	}
	icmpDatagram := ICMPDatagram{
		Payload: payload.Data,
	}
	datagram, err := icmpDatagram.MarshalBinary()
	if err != nil {
		return err
	}
	return c.conn.SendDatagram(datagram)
}

func (c *datagramConn) SendICMPTTLExceed(icmp *packet.ICMP, rawPacket packet.RawPacket) error {
	return c.SendICMPPacket(c.icmpRouter.ConvertToTTLExceeded(icmp, rawPacket))
}

// pollDatagrams will read datagrams from the underlying connection until the provided context is done.
func (c *datagramConn) pollDatagrams(ctx context.Context) {
	for ctx.Err() == nil {
		datagram, err := c.conn.ReceiveDatagram(ctx)
		// If the read returns an error, we want to return the failure to the channel.
		if err != nil {
			c.readErrors <- err
			return
		}
		c.datagrams <- datagram
	}
	if ctx.Err() != nil {
		c.readErrors <- ctx.Err()
	}
}

// Serve will begin the process of receiving datagrams from the [quic.Connection] and demuxing them to their destination.
// The [DatagramConn] when serving, will be responsible for the sessions it accepts.
func (c *datagramConn) Serve(ctx context.Context) error {
	connCtx := c.conn.Context()
	// We want to make sure that we cancel the reader context if the Serve method returns. This could also mean that the
	// underlying connection is also closing, but that is handled outside of the context of the datagram muxer.
	readCtx, cancel := context.WithCancel(connCtx)
	defer cancel()
	go c.pollDatagrams(readCtx)
	// Processing ICMP datagrams also monitors the reader context since the ICMP datagrams from the reader are the input
	// for the routine.
	go c.processICMPDatagrams(readCtx)
	for {
		// We make sure to monitor the context of cloudflared and the underlying connection to return if any errors occur.
		var datagram []byte
		select {
		// Monitor the context of cloudflared
		case <-ctx.Done():
			return ctx.Err()
		// Monitor the context of the underlying quic connection
		case <-connCtx.Done():
			return connCtx.Err()
		// Monitor for any hard errors from reading the connection
		case err := <-c.readErrors:
			return err
		// Wait and dequeue datagrams as they come in
		case d := <-c.datagrams:
			datagram = d
		}

		// Each incoming datagram will be processed in a new go routine to handle the demuxing and action associated.
		typ, err := ParseDatagramType(datagram)
		if err != nil {
			c.logger.Err(err).Msgf("unable to parse datagram type: %d", typ)
			continue
		}
		switch typ {
		case UDPSessionRegistrationType:
			reg := &UDPSessionRegistrationDatagram{}
			err := reg.UnmarshalBinary(datagram)
			if err != nil {
				c.logger.Err(err).Msgf("unable to unmarshal session registration datagram")
				continue
			}
			logger := c.logger.With().Str(logFlowID, reg.RequestID.String()).Logger()
			// We bind the new session to the quic connection context instead of cloudflared context to allow for the
			// quic connection to close and close only the sessions bound to it. Closing of cloudflared will also
			// initiate the close of the quic connection, so we don't have to worry about the application context
			// in the scope of a session.
			//
			// Additionally, we spin out the registration into a separate go routine to handle the Serve'ing of the
			// session in a separate routine from the demuxer.
			go c.handleSessionRegistrationDatagram(connCtx, reg, &logger)
		case UDPSessionPayloadType:
			payload := &UDPSessionPayloadDatagram{}
			err := payload.UnmarshalBinary(datagram)
			if err != nil {
				c.logger.Err(err).Msgf("unable to unmarshal session payload datagram")
				continue
			}
			logger := c.logger.With().Str(logFlowID, payload.RequestID.String()).Logger()
			c.handleSessionPayloadDatagram(payload, &logger)
		case ICMPType:
			packet := &ICMPDatagram{}
			err := packet.UnmarshalBinary(datagram)
			if err != nil {
				c.logger.Err(err).Msgf("unable to unmarshal icmp datagram")
				continue
			}
			c.handleICMPPacket(packet)
		case UDPSessionRegistrationResponseType:
			// cloudflared should never expect to receive UDP session responses as it will not initiate new
			// sessions towards the edge.
			c.logger.Error().Msgf("unexpected datagram type received: %d", UDPSessionRegistrationResponseType)
			continue
		default:
			c.logger.Error().Msgf("unknown datagram type received: %d", typ)
		}
	}
}

// This method handles new registrations of a session and the serve loop for the session.
func (c *datagramConn) handleSessionRegistrationDatagram(ctx context.Context, datagram *UDPSessionRegistrationDatagram, logger *zerolog.Logger) {
	log := logger.With().
		Str(logFlowID, datagram.RequestID.String()).
		Str(logDstKey, datagram.Dest.String()).
		Logger()
	session, err := c.sessionManager.RegisterSession(datagram, c)
	if err != nil {
		switch err {
		case ErrSessionAlreadyRegistered:
			// Session is already registered and likely the response got lost
			c.handleSessionAlreadyRegistered(datagram.RequestID, &log)
		case ErrSessionBoundToOtherConn:
			// Session is already registered but to a different connection
			c.handleSessionMigration(datagram.RequestID, &log)
		case ErrSessionRegistrationRateLimited:
			// There are too many concurrent sessions so we return an error to force a retry later
			c.handleSessionRegistrationRateLimited(datagram, &log)
		default:
			log.Err(err).Msg("flow registration failure")
			c.handleSessionRegistrationFailure(datagram.RequestID, &log)
		}
		return
	}
	log = log.With().Str(logSrcKey, session.LocalAddr().String()).Logger()
	c.metrics.IncrementFlows(c.index)
	// Make sure to eventually remove the session from the session manager when the session is closed
	defer c.sessionManager.UnregisterSession(session.ID())
	defer c.metrics.DecrementFlows(c.index)

	// Respond that we are able to process the new session
	err = c.SendUDPSessionResponse(datagram.RequestID, ResponseOk)
	if err != nil {
		log.Err(err).Msgf("flow registration failure: unable to send session registration response")
		return
	}

	// We bind the context of the session to the [quic.Connection] that initiated the session.
	// [Session.Serve] is blocking and will continue this go routine till the end of the session lifetime.
	start := time.Now()
	err = session.Serve(ctx)
	elapsedMS := time.Since(start).Milliseconds()
	log = log.With().Int64(logDurationKey, elapsedMS).Logger()
	if err == nil {
		// We typically don't expect a session to close without some error response. [SessionIdleErr] is the typical
		// expected error response.
		log.Warn().Msg("flow closed: no explicit close or timeout elapsed")
		return
	}
	// SessionIdleErr and SessionCloseErr are valid and successful error responses to end a session.
	if errors.Is(err, SessionIdleErr{}) || errors.Is(err, SessionCloseErr) {
		log.Debug().Msgf("flow closed: %s", err.Error())
		return
	}

	// All other errors should be reported as errors
	log.Err(err).Msgf("flow closed with an error")
}

func (c *datagramConn) handleSessionAlreadyRegistered(requestID RequestID, logger *zerolog.Logger) {
	// Send another registration response since the session is already active
	err := c.SendUDPSessionResponse(requestID, ResponseOk)
	if err != nil {
		logger.Err(err).Msgf("flow registration failure: unable to send an additional flow registration response")
		return
	}

	session, err := c.sessionManager.GetSession(requestID)
	if err != nil {
		// If for some reason we can not find the session after attempting to register it, we can just return
		// instead of trying to reset the idle timer for it.
		return
	}
	// The session is already running in another routine so we want to restart the idle timeout since no proxied
	// packets have come down yet.
	session.ResetIdleTimer()
	c.metrics.RetryFlowResponse(c.index)
	logger.Debug().Msgf("flow registration response retry")
}

func (c *datagramConn) handleSessionMigration(requestID RequestID, logger *zerolog.Logger) {
	// We need to migrate the currently running session to this edge connection.
	session, err := c.sessionManager.GetSession(requestID)
	if err != nil {
		// If for some reason we can not find the session after attempting to register it, we can just return
		// instead of trying to reset the idle timer for it.
		return
	}

	// Migrate the session to use this edge connection instead of the currently running one.
	// We also pass in this connection's logger to override the existing logger for the session.
	session.Migrate(c, c.conn.Context(), c.logger)

	// Send another registration response since the session is already active
	err = c.SendUDPSessionResponse(requestID, ResponseOk)
	if err != nil {
		logger.Err(err).Msgf("flow registration failure: unable to send an additional flow registration response")
		return
	}
	logger.Debug().Msgf("flow registration migration")
}

func (c *datagramConn) handleSessionRegistrationFailure(requestID RequestID, logger *zerolog.Logger) {
	err := c.SendUDPSessionResponse(requestID, ResponseUnableToBindSocket)
	if err != nil {
		logger.Err(err).Msgf("unable to send flow registration error response (%d)", ResponseUnableToBindSocket)
	}
}

func (c *datagramConn) handleSessionRegistrationRateLimited(datagram *UDPSessionRegistrationDatagram, logger *zerolog.Logger) {
	c.logger.Warn().Msg("Too many concurrent sessions being handled, rejecting udp proxy")

	rateLimitResponse := ResponseTooManyActiveFlows
	err := c.SendUDPSessionResponse(datagram.RequestID, rateLimitResponse)
	if err != nil {
		logger.Err(err).Msgf("unable to send flow registration error response (%d)", rateLimitResponse)
	}
}

// Handles incoming datagrams that need to be sent to a registered session.
func (c *datagramConn) handleSessionPayloadDatagram(datagram *UDPSessionPayloadDatagram, logger *zerolog.Logger) {
	s, err := c.sessionManager.GetSession(datagram.RequestID)
	if err != nil {
		c.metrics.DroppedUDPDatagram(c.index, DroppedWriteFlowUnknown)
		logger.Err(err).Msgf("unable to find flow")
		return
	}
	s.Write(datagram.Payload)
}

// Handles incoming ICMP datagrams into a serialized channel to be handled by a single consumer.
func (c *datagramConn) handleICMPPacket(datagram *ICMPDatagram) {
	if c.icmpRouter == nil {
		// ICMPRouter is disabled so we drop the current packet and ignore all incoming ICMP packets
		return
	}
	select {
	case c.icmpDatagramChan <- datagram:
	default:
		// If the ICMP datagram channel is full, drop any additional incoming.
		c.metrics.DroppedICMPPackets(c.index, DroppedWriteFull)
		c.logger.Warn().Msg("failed to write icmp packet to origin: dropped")
	}
}

// Consumes from the ICMP datagram channel to write out the ICMP requests to an origin.
func (c *datagramConn) processICMPDatagrams(ctx context.Context) {
	if c.icmpRouter == nil {
		// ICMPRouter is disabled so we ignore all incoming ICMP packets
		return
	}

	for {
		select {
		// If the provided context is closed we want to exit the write loop
		case <-ctx.Done():
			return
		case datagram := <-c.icmpDatagramChan:
			c.writeICMPPacket(datagram)
		}
	}
}

func (c *datagramConn) writeICMPPacket(datagram *ICMPDatagram) {
	// Decode the provided ICMPDatagram as an ICMP packet
	rawPacket := packet.RawPacket{Data: datagram.Payload}
	cachedDecoder := c.icmpDecoderPool.Get()
	defer c.icmpDecoderPool.Put(cachedDecoder)
	decoder, ok := cachedDecoder.(*packet.ICMPDecoder)
	if !ok {
		c.metrics.DroppedICMPPackets(c.index, DroppedWriteFailed)
		c.logger.Error().Msg("Could not get ICMPDecoder from the pool. Dropping packet")
		return
	}

	icmp, err := decoder.Decode(rawPacket)

	if err != nil {
		c.metrics.DroppedICMPPackets(c.index, DroppedWriteFailed)
		c.logger.Err(err).Msgf("unable to marshal icmp packet")
		return
	}

	// If the ICMP packet's TTL is expired, we won't send it to the origin and immediately return a TTL Exceeded Message
	if icmp.TTL <= 1 {
		if err := c.SendICMPTTLExceed(icmp, rawPacket); err != nil {
			c.metrics.DroppedICMPPackets(c.index, DroppedWriteFailed)
			c.logger.Err(err).Msg("failed to return ICMP TTL exceed error")
		}
		return
	}
	icmp.TTL--

	// The context isn't really needed here since it's only really used throughout the ICMP router as a way to store
	// the tracing context, however datagram V3 does not support tracing ICMP packets, so we just pass the current
	// connection context which will have no tracing information available.
	err = c.icmpRouter.Request(c.conn.Context(), icmp, newPacketResponder(c, c.index))
	if err != nil {
		c.metrics.DroppedICMPPackets(c.index, DroppedWriteFailed)
		c.logger.Err(err).
			Str(logSrcKey, icmp.Src.String()).
			Str(logDstKey, icmp.Dst.String()).
			Interface(logICMPTypeKey, icmp.Type).
			Msgf("unable to write icmp datagram to origin")
		return
	}
}
