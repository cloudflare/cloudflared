package v3

import (
	"context"
	"errors"

	"github.com/rs/zerolog"
)

const (
	// Allocating a 16 channel buffer here allows for the writer to be slightly faster than the reader.
	// This has worked previously well for datagramv2, so we will start with this as well
	demuxChanCapacity = 16
)

// DatagramConn is the bridge that multiplexes writes and reads of datagrams for UDP sessions and ICMP packets to
// a connection.
type DatagramConn interface {
	DatagramWriter
	// Serve provides a server interface to process and handle incoming QUIC datagrams and demux their datagram v3 payloads.
	Serve(context.Context) error
	// ID indicates connection index identifier
	ID() uint8
}

// DatagramWriter provides the Muxer interface to create proper Datagrams when sending over a connection.
type DatagramWriter interface {
	SendUDPSessionDatagram(datagram []byte) error
	SendUDPSessionResponse(id RequestID, resp SessionRegistrationResp) error
	//SendICMPPacket(packet packet.IP) error
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
	conn           QuicConnection
	index          uint8
	sessionManager SessionManager
	metrics        Metrics
	logger         *zerolog.Logger

	datagrams  chan []byte
	readErrors chan error
}

func NewDatagramConn(conn QuicConnection, sessionManager SessionManager, index uint8, metrics Metrics, logger *zerolog.Logger) DatagramConn {
	log := logger.With().Uint8("datagramVersion", 3).Logger()
	return &datagramConn{
		conn:           conn,
		index:          index,
		sessionManager: sessionManager,
		metrics:        metrics,
		logger:         &log,
		datagrams:      make(chan []byte, demuxChanCapacity),
		readErrors:     make(chan error, 2),
	}
}

func (c datagramConn) ID() uint8 {
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

var errReadTimeout error = errors.New("receive datagram timeout")

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
	for {
		// We make sure to monitor the context of cloudflared and the underlying connection to return if any errors occur.
		var datagram []byte
		select {
		// Monitor the context of cloudflared
		case <-ctx.Done():
			return ctx.Err()
		// Monitor the context of the underlying connection
		case <-connCtx.Done():
			return connCtx.Err()
		// Monitor for any hard errors from reading the connection
		case err := <-c.readErrors:
			return err
		// Otherwise, wait and dequeue datagrams as they come in
		case d := <-c.datagrams:
			datagram = d
		}

		// Each incoming datagram will be processed in a new go routine to handle the demuxing and action associated.
		go func() {
			typ, err := ParseDatagramType(datagram)
			if err != nil {
				c.logger.Err(err).Msgf("unable to parse datagram type: %d", typ)
				return
			}
			switch typ {
			case UDPSessionRegistrationType:
				reg := &UDPSessionRegistrationDatagram{}
				err := reg.UnmarshalBinary(datagram)
				if err != nil {
					c.logger.Err(err).Msgf("unable to unmarshal session registration datagram")
					return
				}
				logger := c.logger.With().Str(logFlowID, reg.RequestID.String()).Logger()
				// We bind the new session to the quic connection context instead of cloudflared context to allow for the
				// quic connection to close and close only the sessions bound to it. Closing of cloudflared will also
				// initiate the close of the quic connection, so we don't have to worry about the application context
				// in the scope of a session.
				c.handleSessionRegistrationDatagram(connCtx, reg, &logger)
			case UDPSessionPayloadType:
				payload := &UDPSessionPayloadDatagram{}
				err := payload.UnmarshalBinary(datagram)
				if err != nil {
					c.logger.Err(err).Msgf("unable to unmarshal session payload datagram")
					return
				}
				logger := c.logger.With().Str(logFlowID, payload.RequestID.String()).Logger()
				c.handleSessionPayloadDatagram(payload, &logger)
			case UDPSessionRegistrationResponseType:
				// cloudflared should never expect to receive UDP session responses as it will not initiate new
				// sessions towards the edge.
				c.logger.Error().Msgf("unexpected datagram type received: %d", UDPSessionRegistrationResponseType)
				return
			default:
				c.logger.Error().Msgf("unknown datagram type received: %d", typ)
			}
		}()
	}
}

// This method handles new registrations of a session and the serve loop for the session.
func (c *datagramConn) handleSessionRegistrationDatagram(ctx context.Context, datagram *UDPSessionRegistrationDatagram, logger *zerolog.Logger) {
	session, err := c.sessionManager.RegisterSession(datagram, c)
	switch err {
	case nil:
		// Continue as normal
	case ErrSessionAlreadyRegistered:
		// Session is already registered and likely the response got lost
		c.handleSessionAlreadyRegistered(datagram.RequestID, logger)
		return
	case ErrSessionBoundToOtherConn:
		// Session is already registered but to a different connection
		c.handleSessionMigration(datagram.RequestID, logger)
		return
	default:
		logger.Err(err).Msgf("flow registration failure")
		c.handleSessionRegistrationFailure(datagram.RequestID, logger)
		return
	}
	c.metrics.IncrementFlows()
	// Make sure to eventually remove the session from the session manager when the session is closed
	defer c.sessionManager.UnregisterSession(session.ID())
	defer c.metrics.DecrementFlows()

	// Respond that we are able to process the new session
	err = c.SendUDPSessionResponse(datagram.RequestID, ResponseOk)
	if err != nil {
		logger.Err(err).Msgf("flow registration failure: unable to send session registration response")
		return
	}

	// We bind the context of the session to the [quic.Connection] that initiated the session.
	// [Session.Serve] is blocking and will continue this go routine till the end of the session lifetime.
	err = session.Serve(ctx)
	if err == nil {
		// We typically don't expect a session to close without some error response. [SessionIdleErr] is the typical
		// expected error response.
		logger.Warn().Msg("flow was closed without explicit close or timeout")
		return
	}
	// SessionIdleErr and SessionCloseErr are valid and successful error responses to end a session.
	if errors.Is(err, SessionIdleErr{}) || errors.Is(err, SessionCloseErr) {
		logger.Debug().Msg(err.Error())
		return
	}

	// All other errors should be reported as errors
	logger.Err(err).Msgf("flow was closed with an error")
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
	c.metrics.RetryFlowResponse()
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
	session.Migrate(c)

	// Send another registration response since the session is already active
	err = c.SendUDPSessionResponse(requestID, ResponseOk)
	if err != nil {
		logger.Err(err).Msgf("flow registration failure: unable to send an additional flow registration response")
		return
	}
}

func (c *datagramConn) handleSessionRegistrationFailure(requestID RequestID, logger *zerolog.Logger) {
	err := c.SendUDPSessionResponse(requestID, ResponseUnableToBindSocket)
	if err != nil {
		logger.Err(err).Msgf("unable to send flow registration error response (%d)", ResponseUnableToBindSocket)
	}
}

// Handles incoming datagrams that need to be sent to a registered session.
func (c *datagramConn) handleSessionPayloadDatagram(datagram *UDPSessionPayloadDatagram, logger *zerolog.Logger) {
	s, err := c.sessionManager.GetSession(datagram.RequestID)
	if err != nil {
		logger.Err(err).Msgf("unable to find flow")
		return
	}
	// We ignore the bytes written to the socket because any partial write must return an error.
	_, err = s.Write(datagram.Payload)
	if err != nil {
		logger.Err(err).Msgf("unable to write payload for the flow")
		return
	}
}
