package quic

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/quic-go/quic-go/internal/wire"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/logging"
)

type Transport struct {
	// A single net.PacketConn can only be handled by one Transport.
	// Bad things will happen if passed to multiple Transports.
	//
	// If the connection satisfies the OOBCapablePacketConn interface
	// (as a net.UDPConn does), ECN and packet info support will be enabled.
	// In this case, optimized syscalls might be used, skipping the
	// ReadFrom and WriteTo calls to read / write packets.
	Conn net.PacketConn

	// The length of the connection ID in bytes.
	// It can be 0, or any value between 4 and 18.
	// If unset, a 4 byte connection ID will be used.
	ConnectionIDLength int

	// Use for generating new connection IDs.
	// This allows the application to control of the connection IDs used,
	// which allows routing / load balancing based on connection IDs.
	// All Connection IDs returned by the ConnectionIDGenerator MUST
	// have the same length.
	ConnectionIDGenerator ConnectionIDGenerator

	// The StatelessResetKey is used to generate stateless reset tokens.
	// If no key is configured, sending of stateless resets is disabled.
	StatelessResetKey *StatelessResetKey

	// A Tracer traces events that don't belong to a single QUIC connection.
	Tracer logging.Tracer

	handlerMap packetHandlerManager

	mutex    sync.Mutex
	initOnce sync.Once
	initErr  error

	// Set in init.
	// If no ConnectionIDGenerator is set, this is the ConnectionIDLength.
	connIDLen int
	// Set in init.
	// If no ConnectionIDGenerator is set, this is set to a default.
	connIDGenerator ConnectionIDGenerator

	server unknownPacketHandler

	conn rawConn

	closeQueue chan closePacket

	listening   chan struct{} // is closed when listen returns
	closed      bool
	createdConn bool
	isSingleUse bool // was created for a single server or client, i.e. by calling quic.Listen or quic.Dial

	logger utils.Logger
}

// Listen starts listening for incoming QUIC connections.
// There can only be a single listener on any net.PacketConn.
// Listen may only be called again after the current Listener was closed.
func (t *Transport) Listen(tlsConf *tls.Config, conf *Config) (*Listener, error) {
	if tlsConf == nil {
		return nil, errors.New("quic: tls.Config not set")
	}
	if err := validateConfig(conf); err != nil {
		return nil, err
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.server != nil {
		return nil, errListenerAlreadySet
	}
	conf = populateServerConfig(conf)
	if err := t.init(true); err != nil {
		return nil, err
	}
	s, err := newServer(t.conn, t.handlerMap, t.connIDGenerator, tlsConf, conf, t.Tracer, t.closeServer, false)
	if err != nil {
		return nil, err
	}
	t.server = s
	return &Listener{baseServer: s}, nil
}

// ListenEarly starts listening for incoming QUIC connections.
// There can only be a single listener on any net.PacketConn.
// Listen may only be called again after the current Listener was closed.
func (t *Transport) ListenEarly(tlsConf *tls.Config, conf *Config) (*EarlyListener, error) {
	if tlsConf == nil {
		return nil, errors.New("quic: tls.Config not set")
	}
	if err := validateConfig(conf); err != nil {
		return nil, err
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.server != nil {
		return nil, errListenerAlreadySet
	}
	conf = populateServerConfig(conf)
	if err := t.init(true); err != nil {
		return nil, err
	}
	s, err := newServer(t.conn, t.handlerMap, t.connIDGenerator, tlsConf, conf, t.Tracer, t.closeServer, true)
	if err != nil {
		return nil, err
	}
	t.server = s
	return &EarlyListener{baseServer: s}, nil
}

// Dial dials a new connection to a remote host (not using 0-RTT).
func (t *Transport) Dial(ctx context.Context, addr net.Addr, tlsConf *tls.Config, conf *Config) (Connection, error) {
	if err := validateConfig(conf); err != nil {
		return nil, err
	}
	conf = populateConfig(conf)
	if err := t.init(false); err != nil {
		return nil, err
	}
	var onClose func()
	if t.isSingleUse {
		onClose = func() { t.Close() }
	}
	return dial(ctx, newSendConn(t.conn, addr, nil), t.connIDGenerator, t.handlerMap, tlsConf, conf, onClose, false)
}

// DialEarly dials a new connection, attempting to use 0-RTT if possible.
func (t *Transport) DialEarly(ctx context.Context, addr net.Addr, tlsConf *tls.Config, conf *Config) (EarlyConnection, error) {
	if err := validateConfig(conf); err != nil {
		return nil, err
	}
	conf = populateConfig(conf)
	if err := t.init(false); err != nil {
		return nil, err
	}
	var onClose func()
	if t.isSingleUse {
		onClose = func() { t.Close() }
	}
	return dial(ctx, newSendConn(t.conn, addr, nil), t.connIDGenerator, t.handlerMap, tlsConf, conf, onClose, true)
}

func setReceiveBuffer(c net.PacketConn, logger utils.Logger) error {
	conn, ok := c.(interface{ SetReadBuffer(int) error })
	if !ok {
		return errors.New("connection doesn't allow setting of receive buffer size. Not a *net.UDPConn?")
	}
	size, err := inspectReadBuffer(c)
	if err != nil {
		return fmt.Errorf("failed to determine receive buffer size: %w", err)
	}
	if size >= protocol.DesiredReceiveBufferSize {
		logger.Debugf("Conn has receive buffer of %d kiB (wanted: at least %d kiB)", size/1024, protocol.DesiredReceiveBufferSize/1024)
		return nil
	}
	if err := conn.SetReadBuffer(protocol.DesiredReceiveBufferSize); err != nil {
		return fmt.Errorf("failed to increase receive buffer size: %w", err)
	}
	newSize, err := inspectReadBuffer(c)
	if err != nil {
		return fmt.Errorf("failed to determine receive buffer size: %w", err)
	}
	if newSize == size {
		return fmt.Errorf("failed to increase receive buffer size (wanted: %d kiB, got %d kiB)", protocol.DesiredReceiveBufferSize/1024, newSize/1024)
	}
	if newSize < protocol.DesiredReceiveBufferSize {
		return fmt.Errorf("failed to sufficiently increase receive buffer size (was: %d kiB, wanted: %d kiB, got: %d kiB)", size/1024, protocol.DesiredReceiveBufferSize/1024, newSize/1024)
	}
	logger.Debugf("Increased receive buffer size to %d kiB", newSize/1024)
	return nil
}

// only print warnings about the UDP receive buffer size once
var receiveBufferWarningOnce sync.Once

func (t *Transport) init(isServer bool) error {
	t.initOnce.Do(func() {
		getMultiplexer().AddConn(t.Conn)

		conn, err := wrapConn(t.Conn)
		if err != nil {
			t.initErr = err
			return
		}

		t.logger = utils.DefaultLogger // TODO: make this configurable
		t.conn = conn
		t.handlerMap = newPacketHandlerMap(t.StatelessResetKey, t.enqueueClosePacket, t.logger)
		t.listening = make(chan struct{})

		t.closeQueue = make(chan closePacket, 4)

		if t.ConnectionIDGenerator != nil {
			t.connIDGenerator = t.ConnectionIDGenerator
			t.connIDLen = t.ConnectionIDGenerator.ConnectionIDLen()
		} else {
			connIDLen := t.ConnectionIDLength
			if t.ConnectionIDLength == 0 && (!t.isSingleUse || isServer) {
				connIDLen = protocol.DefaultConnectionIDLength
			}
			t.connIDLen = connIDLen
			t.connIDGenerator = &protocol.DefaultConnectionIDGenerator{ConnLen: t.connIDLen}
		}

		go t.listen(conn)
		go t.runCloseQueue()
	})
	return t.initErr
}

func (t *Transport) enqueueClosePacket(p closePacket) {
	select {
	case t.closeQueue <- p:
	default:
		// Oops, we're backlogged.
		// Just drop the packet, sending CONNECTION_CLOSE copies is best effort anyway.
	}
}

func (t *Transport) runCloseQueue() {
	for {
		select {
		case <-t.listening:
			return
		case p := <-t.closeQueue:
			t.conn.WritePacket(p.payload, p.addr, p.info.OOB())
		}
	}
}

// Close closes the underlying connection and waits until listen has returned.
// It is invalid to start new listeners or connections after that.
func (t *Transport) Close() error {
	t.close(errors.New("closing"))
	if t.createdConn {
		if err := t.conn.Close(); err != nil {
			return err
		}
	} else {
		t.conn.SetReadDeadline(time.Now())
		defer func() { t.conn.SetReadDeadline(time.Time{}) }()
	}
	<-t.listening // wait until listening returns
	return nil
}

func (t *Transport) closeServer() {
	t.handlerMap.CloseServer()
	t.mutex.Lock()
	t.server = nil
	if t.isSingleUse {
		t.closed = true
	}
	t.mutex.Unlock()
	if t.createdConn {
		t.Conn.Close()
	}
	if t.isSingleUse {
		t.conn.SetReadDeadline(time.Now())
		defer func() { t.conn.SetReadDeadline(time.Time{}) }()
		<-t.listening // wait until listening returns
	}
}

func (t *Transport) close(e error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.closed {
		return
	}

	t.handlerMap.Close(e)
	if t.server != nil {
		t.server.setCloseError(e)
	}
	t.closed = true
}

func (t *Transport) listen(conn rawConn) {
	defer close(t.listening)
	defer getMultiplexer().RemoveConn(t.Conn)

	if err := setReceiveBuffer(t.Conn, t.logger); err != nil {
		if !strings.Contains(err.Error(), "use of closed network connection") {
			receiveBufferWarningOnce.Do(func() {
				if disable, _ := strconv.ParseBool(os.Getenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING")); disable {
					return
				}
				log.Printf("%s. See https://github.com/quic-go/quic-go/wiki/UDP-Receive-Buffer-Size for details.", err)
			})
		}
	}

	for {
		p, err := conn.ReadPacket()
		//nolint:staticcheck // SA1019 ignore this!
		// TODO: This code is used to ignore wsa errors on Windows.
		// Since net.Error.Temporary is deprecated as of Go 1.18, we should find a better solution.
		// See https://github.com/quic-go/quic-go/issues/1737 for details.
		if nerr, ok := err.(net.Error); ok && nerr.Temporary() {
			t.mutex.Lock()
			closed := t.closed
			t.mutex.Unlock()
			if closed {
				return
			}
			t.logger.Debugf("Temporary error reading from conn: %w", err)
			continue
		}
		if err != nil {
			t.close(err)
			return
		}
		t.handlePacket(p)
	}
}

func (t *Transport) handlePacket(p *receivedPacket) {
	connID, err := wire.ParseConnectionID(p.data, t.connIDLen)
	if err != nil {
		t.logger.Debugf("error parsing connection ID on packet from %s: %s", p.remoteAddr, err)
		if t.Tracer != nil {
			t.Tracer.DroppedPacket(p.remoteAddr, logging.PacketTypeNotDetermined, p.Size(), logging.PacketDropHeaderParseError)
		}
		p.buffer.MaybeRelease()
		return
	}

	if isStatelessReset := t.maybeHandleStatelessReset(p.data); isStatelessReset {
		return
	}
	if handler, ok := t.handlerMap.Get(connID); ok {
		handler.handlePacket(p)
		return
	}
	if !wire.IsLongHeaderPacket(p.data[0]) {
		go t.maybeSendStatelessReset(p, connID)
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.server == nil { // no server set
		t.logger.Debugf("received a packet with an unexpected connection ID %s", connID)
		return
	}
	t.server.handlePacket(p)
}

func (t *Transport) maybeSendStatelessReset(p *receivedPacket, connID protocol.ConnectionID) {
	defer p.buffer.Release()
	if t.StatelessResetKey == nil {
		return
	}
	// Don't send a stateless reset in response to very small packets.
	// This includes packets that could be stateless resets.
	if len(p.data) <= protocol.MinStatelessResetSize {
		return
	}
	token := t.handlerMap.GetStatelessResetToken(connID)
	t.logger.Debugf("Sending stateless reset to %s (connection ID: %s). Token: %#x", p.remoteAddr, connID, token)
	data := make([]byte, protocol.MinStatelessResetSize-16, protocol.MinStatelessResetSize)
	rand.Read(data)
	data[0] = (data[0] & 0x7f) | 0x40
	data = append(data, token[:]...)
	if _, err := t.conn.WritePacket(data, p.remoteAddr, p.info.OOB()); err != nil {
		t.logger.Debugf("Error sending Stateless Reset: %s", err)
	}
}

func (t *Transport) maybeHandleStatelessReset(data []byte) bool {
	// stateless resets are always short header packets
	if wire.IsLongHeaderPacket(data[0]) {
		return false
	}
	if len(data) < 17 /* type byte + 16 bytes for the reset token */ {
		return false
	}

	token := *(*protocol.StatelessResetToken)(data[len(data)-16:])
	if conn, ok := t.handlerMap.GetByResetToken(token); ok {
		t.logger.Debugf("Received a stateless reset with token %#x. Closing connection.", token)
		go conn.destroy(&StatelessResetError{Token: token})
		return true
	}
	return false
}
