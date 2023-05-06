package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"net"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/logging"
)

type client struct {
	sendConn sendConn

	use0RTT bool

	packetHandlers packetHandlerManager
	onClose        func()

	tlsConf *tls.Config
	config  *Config

	connIDGenerator ConnectionIDGenerator
	srcConnID       protocol.ConnectionID
	destConnID      protocol.ConnectionID

	initialPacketNumber  protocol.PacketNumber
	hasNegotiatedVersion bool
	version              protocol.VersionNumber

	handshakeChan chan struct{}

	conn quicConn

	tracer    logging.ConnectionTracer
	tracingID uint64
	logger    utils.Logger
}

// make it possible to mock connection ID for initial generation in the tests
var generateConnectionIDForInitial = protocol.GenerateConnectionIDForInitial

// DialAddr establishes a new QUIC connection to a server.
// It uses a new UDP connection and closes this connection when the QUIC connection is closed.
func DialAddr(ctx context.Context, addr string, tlsConf *tls.Config, conf *Config) (Connection, error) {
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	dl, err := setupTransport(udpConn, tlsConf, true)
	if err != nil {
		return nil, err
	}
	return dl.Dial(ctx, udpAddr, tlsConf, conf)
}

// DialAddrEarly establishes a new 0-RTT QUIC connection to a server.
// It uses a new UDP connection and closes this connection when the QUIC connection is closed.
func DialAddrEarly(ctx context.Context, addr string, tlsConf *tls.Config, conf *Config) (EarlyConnection, error) {
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	dl, err := setupTransport(udpConn, tlsConf, true)
	if err != nil {
		return nil, err
	}
	conn, err := dl.DialEarly(ctx, udpAddr, tlsConf, conf)
	if err != nil {
		dl.Close()
		return nil, err
	}
	return conn, nil
}

// DialEarly establishes a new 0-RTT QUIC connection to a server using a net.PacketConn using the provided context.
// See DialEarly for details.
func DialEarly(ctx context.Context, c net.PacketConn, addr net.Addr, tlsConf *tls.Config, conf *Config) (EarlyConnection, error) {
	dl, err := setupTransport(c, tlsConf, false)
	if err != nil {
		return nil, err
	}
	conn, err := dl.DialEarly(ctx, addr, tlsConf, conf)
	if err != nil {
		dl.Close()
		return nil, err
	}
	return conn, nil
}

// Dial establishes a new QUIC connection to a server using a net.PacketConn. If
// the PacketConn satisfies the OOBCapablePacketConn interface (as a net.UDPConn
// does), ECN and packet info support will be enabled. In this case, ReadMsgUDP
// and WriteMsgUDP will be used instead of ReadFrom and WriteTo to read/write
// packets.
// The tls.Config must define an application protocol (using NextProtos).
func Dial(ctx context.Context, c net.PacketConn, addr net.Addr, tlsConf *tls.Config, conf *Config) (Connection, error) {
	dl, err := setupTransport(c, tlsConf, false)
	if err != nil {
		return nil, err
	}
	conn, err := dl.Dial(ctx, addr, tlsConf, conf)
	if err != nil {
		dl.Close()
		return nil, err
	}
	return conn, nil
}

func setupTransport(c net.PacketConn, tlsConf *tls.Config, createdPacketConn bool) (*Transport, error) {
	if tlsConf == nil {
		return nil, errors.New("quic: tls.Config not set")
	}
	return &Transport{
		Conn:        c,
		createdConn: createdPacketConn,
		isSingleUse: true,
	}, nil
}

func dial(
	ctx context.Context,
	conn sendConn,
	connIDGenerator ConnectionIDGenerator,
	packetHandlers packetHandlerManager,
	tlsConf *tls.Config,
	config *Config,
	onClose func(),
	use0RTT bool,
) (quicConn, error) {
	c, err := newClient(conn, connIDGenerator, config, tlsConf, onClose, use0RTT)
	if err != nil {
		return nil, err
	}
	c.packetHandlers = packetHandlers

	c.tracingID = nextConnTracingID()
	if c.config.Tracer != nil {
		c.tracer = c.config.Tracer(context.WithValue(ctx, ConnectionTracingKey, c.tracingID), protocol.PerspectiveClient, c.destConnID)
	}
	if c.tracer != nil {
		c.tracer.StartedConnection(c.sendConn.LocalAddr(), c.sendConn.RemoteAddr(), c.srcConnID, c.destConnID)
	}
	if err := c.dial(ctx); err != nil {
		return nil, err
	}
	return c.conn, nil
}

func newClient(sendConn sendConn, connIDGenerator ConnectionIDGenerator, config *Config, tlsConf *tls.Config, onClose func(), use0RTT bool) (*client, error) {
	if tlsConf == nil {
		tlsConf = &tls.Config{}
	} else {
		tlsConf = tlsConf.Clone()
	}

	srcConnID, err := connIDGenerator.GenerateConnectionID()
	if err != nil {
		return nil, err
	}
	destConnID, err := generateConnectionIDForInitial()
	if err != nil {
		return nil, err
	}
	c := &client{
		connIDGenerator: connIDGenerator,
		srcConnID:       srcConnID,
		destConnID:      destConnID,
		sendConn:        sendConn,
		use0RTT:         use0RTT,
		onClose:         onClose,
		tlsConf:         tlsConf,
		config:          config,
		version:         config.Versions[0],
		handshakeChan:   make(chan struct{}),
		logger:          utils.DefaultLogger.WithPrefix("client"),
	}
	return c, nil
}

func (c *client) dial(ctx context.Context) error {
	c.logger.Infof("Starting new connection to %s (%s -> %s), source connection ID %s, destination connection ID %s, version %s", c.tlsConf.ServerName, c.sendConn.LocalAddr(), c.sendConn.RemoteAddr(), c.srcConnID, c.destConnID, c.version)

	c.conn = newClientConnection(
		c.sendConn,
		c.packetHandlers,
		c.destConnID,
		c.srcConnID,
		c.connIDGenerator,
		c.config,
		c.tlsConf,
		c.initialPacketNumber,
		c.use0RTT,
		c.hasNegotiatedVersion,
		c.tracer,
		c.tracingID,
		c.logger,
		c.version,
	)
	c.packetHandlers.Add(c.srcConnID, c.conn)

	errorChan := make(chan error, 1)
	recreateChan := make(chan errCloseForRecreating)
	go func() {
		err := c.conn.run()
		var recreateErr *errCloseForRecreating
		if errors.As(err, &recreateErr) {
			recreateChan <- *recreateErr
			return
		}
		if c.onClose != nil {
			c.onClose()
		}
		errorChan <- err // returns as soon as the connection is closed
	}()

	// only set when we're using 0-RTT
	// Otherwise, earlyConnChan will be nil. Receiving from a nil chan blocks forever.
	var earlyConnChan <-chan struct{}
	if c.use0RTT {
		earlyConnChan = c.conn.earlyConnReady()
	}

	select {
	case <-ctx.Done():
		c.conn.shutdown()
		return ctx.Err()
	case err := <-errorChan:
		return err
	case recreateErr := <-recreateChan:
		c.initialPacketNumber = recreateErr.nextPacketNumber
		c.version = recreateErr.nextVersion
		c.hasNegotiatedVersion = true
		return c.dial(ctx)
	case <-earlyConnChan:
		// ready to send 0-RTT data
		return nil
	case <-c.conn.HandshakeComplete():
		// handshake successfully completed
		return nil
	}
}
