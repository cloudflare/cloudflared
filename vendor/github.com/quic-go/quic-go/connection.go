package quic

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/flowcontrol"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/utils/ringbuffer"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/logging"
)

type unpacker interface {
	UnpackLongHeader(hdr *wire.Header, data []byte) (*unpackedPacket, error)
	UnpackShortHeader(rcvTime time.Time, data []byte) (protocol.PacketNumber, protocol.PacketNumberLen, protocol.KeyPhaseBit, []byte, error)
}

type streamManager interface {
	GetOrOpenSendStream(protocol.StreamID) (sendStreamI, error)
	GetOrOpenReceiveStream(protocol.StreamID) (receiveStreamI, error)
	OpenStream() (Stream, error)
	OpenUniStream() (SendStream, error)
	OpenStreamSync(context.Context) (Stream, error)
	OpenUniStreamSync(context.Context) (SendStream, error)
	AcceptStream(context.Context) (Stream, error)
	AcceptUniStream(context.Context) (ReceiveStream, error)
	DeleteStream(protocol.StreamID) error
	UpdateLimits(*wire.TransportParameters)
	HandleMaxStreamsFrame(*wire.MaxStreamsFrame)
	CloseWithError(error)
	ResetFor0RTT()
	UseResetMaps()
}

type cryptoStreamHandler interface {
	StartHandshake(context.Context) error
	ChangeConnectionID(protocol.ConnectionID)
	SetLargest1RTTAcked(protocol.PacketNumber) error
	SetHandshakeConfirmed()
	GetSessionTicket() ([]byte, error)
	NextEvent() handshake.Event
	DiscardInitialKeys()
	HandleMessage([]byte, protocol.EncryptionLevel) error
	io.Closer
	ConnectionState() handshake.ConnectionState
}

type receivedPacket struct {
	buffer *packetBuffer

	remoteAddr net.Addr
	rcvTime    time.Time
	data       []byte

	ecn protocol.ECN

	info packetInfo // only valid if the contained IP address is valid
}

func (p *receivedPacket) Size() protocol.ByteCount { return protocol.ByteCount(len(p.data)) }

func (p *receivedPacket) Clone() *receivedPacket {
	return &receivedPacket{
		remoteAddr: p.remoteAddr,
		rcvTime:    p.rcvTime,
		data:       p.data,
		buffer:     p.buffer,
		ecn:        p.ecn,
		info:       p.info,
	}
}

type connRunner interface {
	Add(protocol.ConnectionID, packetHandler) bool
	Retire(protocol.ConnectionID)
	Remove(protocol.ConnectionID)
	ReplaceWithClosed([]protocol.ConnectionID, []byte)
	AddResetToken(protocol.StatelessResetToken, packetHandler)
	RemoveResetToken(protocol.StatelessResetToken)
}

type closeError struct {
	err       error
	immediate bool
}

type errCloseForRecreating struct {
	nextPacketNumber protocol.PacketNumber
	nextVersion      protocol.Version
}

func (e *errCloseForRecreating) Error() string {
	return "closing connection in order to recreate it"
}

var connTracingID atomic.Uint64              // to be accessed atomically
func nextConnTracingID() ConnectionTracingID { return ConnectionTracingID(connTracingID.Add(1)) }

// A Connection is a QUIC connection
type connection struct {
	tr *Transport

	// Destination connection ID used during the handshake.
	// Used to check source connection ID on incoming packets.
	handshakeDestConnID protocol.ConnectionID
	// Set for the client. Destination connection ID used on the first Initial sent.
	origDestConnID protocol.ConnectionID
	retrySrcConnID *protocol.ConnectionID // only set for the client (and if a Retry was performed)

	srcConnIDLen int

	perspective protocol.Perspective
	version     protocol.Version
	config      *Config

	conn      sendConn
	sendQueue sender

	// lazily initialzed: most connections never migrate
	pathManager         *pathManager
	largestRcvdAppData  protocol.PacketNumber
	pathManagerOutgoing atomic.Pointer[pathManagerOutgoing]

	streamsMap      streamManager
	connIDManager   *connIDManager
	connIDGenerator *connIDGenerator

	rttStats *utils.RTTStats

	cryptoStreamManager   *cryptoStreamManager
	sentPacketHandler     ackhandler.SentPacketHandler
	receivedPacketHandler ackhandler.ReceivedPacketHandler
	retransmissionQueue   *retransmissionQueue
	framer                *framer
	connFlowController    flowcontrol.ConnectionFlowController
	tokenStoreKey         string                    // only set for the client
	tokenGenerator        *handshake.TokenGenerator // only set for the server

	unpacker      unpacker
	frameParser   wire.FrameParser
	packer        packer
	mtuDiscoverer mtuDiscoverer // initialized when the transport parameters are received

	currentMTUEstimate atomic.Uint32

	initialStream       *cryptoStream
	handshakeStream     *cryptoStream
	oneRTTStream        *cryptoStream // only set for the server
	cryptoStreamHandler cryptoStreamHandler

	notifyReceivedPacket chan struct{}
	sendingScheduled     chan struct{}
	receivedPacketMx     sync.Mutex
	receivedPackets      ringbuffer.RingBuffer[receivedPacket]

	// closeChan is used to notify the run loop that it should terminate
	closeChan chan struct{}
	closeErr  atomic.Pointer[closeError]

	ctx                   context.Context
	ctxCancel             context.CancelCauseFunc
	handshakeCompleteChan chan struct{}

	undecryptablePackets          []receivedPacket // undecryptable packets, waiting for a change in encryption level
	undecryptablePacketsToProcess []receivedPacket

	earlyConnReadyChan chan struct{}
	sentFirstPacket    bool
	droppedInitialKeys bool
	handshakeComplete  bool
	handshakeConfirmed bool

	receivedRetry       bool
	versionNegotiated   bool
	receivedFirstPacket bool

	// the minimum of the max_idle_timeout values advertised by both endpoints
	idleTimeout  time.Duration
	creationTime time.Time
	// The idle timeout is set based on the max of the time we received the last packet...
	lastPacketReceivedTime time.Time
	// ... and the time we sent a new ack-eliciting packet after receiving a packet.
	firstAckElicitingPacketAfterIdleSentTime time.Time
	// pacingDeadline is the time when the next packet should be sent
	pacingDeadline time.Time

	peerParams *wire.TransportParameters

	timer connectionTimer
	// keepAlivePingSent stores whether a keep alive PING is in flight.
	// It is reset as soon as we receive a packet from the peer.
	keepAlivePingSent bool
	keepAliveInterval time.Duration

	datagramQueue *datagramQueue

	connStateMutex sync.Mutex
	connState      ConnectionState

	logID  string
	tracer *logging.ConnectionTracer
	logger utils.Logger
}

var (
	_ Connection      = &connection{}
	_ EarlyConnection = &connection{}
	_ streamSender    = &connection{}
)

var newConnection = func(
	ctx context.Context,
	ctxCancel context.CancelCauseFunc,
	conn sendConn,
	tr *Transport,
	origDestConnID protocol.ConnectionID,
	retrySrcConnID *protocol.ConnectionID,
	clientDestConnID protocol.ConnectionID,
	destConnID protocol.ConnectionID,
	srcConnID protocol.ConnectionID,
	connIDGenerator ConnectionIDGenerator,
	statelessResetter *statelessResetter,
	conf *Config,
	tlsConf *tls.Config,
	tokenGenerator *handshake.TokenGenerator,
	clientAddressValidated bool,
	tracer *logging.ConnectionTracer,
	logger utils.Logger,
	v protocol.Version,
) quicConn {
	s := &connection{
		ctx:                 ctx,
		ctxCancel:           ctxCancel,
		tr:                  tr,
		conn:                conn,
		config:              conf,
		handshakeDestConnID: destConnID,
		srcConnIDLen:        srcConnID.Len(),
		tokenGenerator:      tokenGenerator,
		oneRTTStream:        newCryptoStream(),
		perspective:         protocol.PerspectiveServer,
		tracer:              tracer,
		logger:              logger,
		version:             v,
	}
	if origDestConnID.Len() > 0 {
		s.logID = origDestConnID.String()
	} else {
		s.logID = destConnID.String()
	}
	runner := tr.connRunner()
	s.connIDManager = newConnIDManager(
		destConnID,
		func(token protocol.StatelessResetToken) { runner.AddResetToken(token, s) },
		runner.RemoveResetToken,
		s.queueControlFrame,
	)
	s.connIDGenerator = newConnIDGenerator(
		tr.id(),
		srcConnID,
		&clientDestConnID,
		statelessResetter,
		connRunnerCallbacks{
			AddConnectionID:    func(connID protocol.ConnectionID) { runner.Add(connID, s) },
			RemoveConnectionID: runner.Remove,
			RetireConnectionID: runner.Retire,
			ReplaceWithClosed:  runner.ReplaceWithClosed,
		},
		s.queueControlFrame,
		connIDGenerator,
	)
	s.preSetup()
	s.sentPacketHandler, s.receivedPacketHandler = ackhandler.NewAckHandler(
		0,
		protocol.ByteCount(s.config.InitialPacketSize),
		s.rttStats,
		clientAddressValidated,
		s.conn.capabilities().ECN,
		s.perspective,
		s.tracer,
		s.logger,
	)
	s.currentMTUEstimate.Store(uint32(estimateMaxPayloadSize(protocol.ByteCount(s.config.InitialPacketSize))))
	statelessResetToken := statelessResetter.GetStatelessResetToken(srcConnID)
	// Allow server to define custom MaxUDPPayloadSize
	maxUDPPayloadSize := protocol.MaxPacketBufferSize
	if maxPacketSize := os.Getenv("TUNNEL_MAX_QUIC_PACKET_SIZE"); maxPacketSize != "" {
		if customMaxPacketSize, err := strconv.ParseUint(maxPacketSize, 10, 64); err == nil {
			maxUDPPayloadSize = int(customMaxPacketSize)
		} else {
			utils.DefaultLogger.Errorf("failed to parse TUNNEL_MAX_QUIC_PACKET_SIZE: %v", err)
		}
	}

	params := &wire.TransportParameters{
		InitialMaxStreamDataBidiLocal:   protocol.ByteCount(s.config.InitialStreamReceiveWindow),
		InitialMaxStreamDataBidiRemote:  protocol.ByteCount(s.config.InitialStreamReceiveWindow),
		InitialMaxStreamDataUni:         protocol.ByteCount(s.config.InitialStreamReceiveWindow),
		InitialMaxData:                  protocol.ByteCount(s.config.InitialConnectionReceiveWindow),
		MaxIdleTimeout:                  s.config.MaxIdleTimeout,
		MaxBidiStreamNum:                protocol.StreamNum(s.config.MaxIncomingStreams),
		MaxUniStreamNum:                 protocol.StreamNum(s.config.MaxIncomingUniStreams),
		MaxAckDelay:                     protocol.MaxAckDelayInclGranularity,
		AckDelayExponent:                protocol.AckDelayExponent,
		MaxUDPPayloadSize:               protocol.ByteCount(maxUDPPayloadSize),
		StatelessResetToken:             &statelessResetToken,
		OriginalDestinationConnectionID: origDestConnID,
		// For interoperability with quic-go versions before May 2023, this value must be set to a value
		// different from protocol.DefaultActiveConnectionIDLimit.
		// If set to the default value, it will be omitted from the transport parameters, which will make
		// old quic-go versions interpret it as 0, instead of the default value of 2.
		// See https://github.com/quic-go/quic-go/pull/3806.
		ActiveConnectionIDLimit:   protocol.MaxActiveConnectionIDs,
		InitialSourceConnectionID: srcConnID,
		RetrySourceConnectionID:   retrySrcConnID,
	}
	if s.config.EnableDatagrams {
		params.MaxDatagramFrameSize = wire.MaxDatagramSize
	} else {
		params.MaxDatagramFrameSize = protocol.InvalidByteCount
	}
	if s.tracer != nil && s.tracer.SentTransportParameters != nil {
		s.tracer.SentTransportParameters(params)
	}
	cs := handshake.NewCryptoSetupServer(
		clientDestConnID,
		conn.LocalAddr(),
		conn.RemoteAddr(),
		params,
		tlsConf,
		conf.Allow0RTT,
		s.rttStats,
		tracer,
		logger,
		s.version,
	)
	s.cryptoStreamHandler = cs
	s.packer = newPacketPacker(srcConnID, s.connIDManager.Get, s.initialStream, s.handshakeStream, s.sentPacketHandler, s.retransmissionQueue, cs, s.framer, s.receivedPacketHandler, s.datagramQueue, s.perspective)
	s.unpacker = newPacketUnpacker(cs, s.srcConnIDLen)
	s.cryptoStreamManager = newCryptoStreamManager(s.initialStream, s.handshakeStream, s.oneRTTStream)
	return s
}

// declare this as a variable, such that we can it mock it in the tests
var newClientConnection = func(
	ctx context.Context,
	conn sendConn,
	tr *Transport,
	destConnID protocol.ConnectionID,
	srcConnID protocol.ConnectionID,
	connIDGenerator ConnectionIDGenerator,
	statelessResetter *statelessResetter,
	conf *Config,
	tlsConf *tls.Config,
	initialPacketNumber protocol.PacketNumber,
	enable0RTT bool,
	hasNegotiatedVersion bool,
	tracer *logging.ConnectionTracer,
	logger utils.Logger,
	v protocol.Version,
) quicConn {
	s := &connection{
		tr:                  tr,
		conn:                conn,
		config:              conf,
		origDestConnID:      destConnID,
		handshakeDestConnID: destConnID,
		srcConnIDLen:        srcConnID.Len(),
		perspective:         protocol.PerspectiveClient,
		logID:               destConnID.String(),
		logger:              logger,
		tracer:              tracer,
		versionNegotiated:   hasNegotiatedVersion,
		version:             v,
	}
	runner := tr.connRunner()
	s.connIDManager = newConnIDManager(
		destConnID,
		func(token protocol.StatelessResetToken) { runner.AddResetToken(token, s) },
		runner.RemoveResetToken,
		s.queueControlFrame,
	)
	s.connIDGenerator = newConnIDGenerator(
		tr.id(),
		srcConnID,
		nil,
		statelessResetter,
		connRunnerCallbacks{
			AddConnectionID:    func(connID protocol.ConnectionID) { runner.Add(connID, s) },
			RemoveConnectionID: runner.Remove,
			RetireConnectionID: runner.Retire,
			ReplaceWithClosed:  runner.ReplaceWithClosed,
		},
		s.queueControlFrame,
		connIDGenerator,
	)
	s.ctx, s.ctxCancel = context.WithCancelCause(ctx)
	s.preSetup()
	s.sentPacketHandler, s.receivedPacketHandler = ackhandler.NewAckHandler(
		initialPacketNumber,
		protocol.ByteCount(s.config.InitialPacketSize),
		s.rttStats,
		false, // has no effect
		s.conn.capabilities().ECN,
		s.perspective,
		s.tracer,
		s.logger,
	)
	s.currentMTUEstimate.Store(uint32(estimateMaxPayloadSize(protocol.ByteCount(s.config.InitialPacketSize))))
	oneRTTStream := newCryptoStream()
	params := &wire.TransportParameters{
		InitialMaxStreamDataBidiRemote: protocol.ByteCount(s.config.InitialStreamReceiveWindow),
		InitialMaxStreamDataBidiLocal:  protocol.ByteCount(s.config.InitialStreamReceiveWindow),
		InitialMaxStreamDataUni:        protocol.ByteCount(s.config.InitialStreamReceiveWindow),
		InitialMaxData:                 protocol.ByteCount(s.config.InitialConnectionReceiveWindow),
		MaxIdleTimeout:                 s.config.MaxIdleTimeout,
		MaxBidiStreamNum:               protocol.StreamNum(s.config.MaxIncomingStreams),
		MaxUniStreamNum:                protocol.StreamNum(s.config.MaxIncomingUniStreams),
		MaxAckDelay:                    protocol.MaxAckDelayInclGranularity,
		MaxUDPPayloadSize:              protocol.MaxPacketBufferSize,
		AckDelayExponent:               protocol.AckDelayExponent,
		// For interoperability with quic-go versions before May 2023, this value must be set to a value
		// different from protocol.DefaultActiveConnectionIDLimit.
		// If set to the default value, it will be omitted from the transport parameters, which will make
		// old quic-go versions interpret it as 0, instead of the default value of 2.
		// See https://github.com/quic-go/quic-go/pull/3806.
		ActiveConnectionIDLimit:   protocol.MaxActiveConnectionIDs,
		InitialSourceConnectionID: srcConnID,
	}
	if s.config.EnableDatagrams {
		params.MaxDatagramFrameSize = wire.MaxDatagramSize
	} else {
		params.MaxDatagramFrameSize = protocol.InvalidByteCount
	}
	if s.tracer != nil && s.tracer.SentTransportParameters != nil {
		s.tracer.SentTransportParameters(params)
	}
	cs := handshake.NewCryptoSetupClient(
		destConnID,
		params,
		tlsConf,
		enable0RTT,
		s.rttStats,
		tracer,
		logger,
		s.version,
	)
	s.cryptoStreamHandler = cs
	s.cryptoStreamManager = newCryptoStreamManager(s.initialStream, s.handshakeStream, oneRTTStream)
	s.unpacker = newPacketUnpacker(cs, s.srcConnIDLen)
	s.packer = newPacketPacker(srcConnID, s.connIDManager.Get, s.initialStream, s.handshakeStream, s.sentPacketHandler, s.retransmissionQueue, cs, s.framer, s.receivedPacketHandler, s.datagramQueue, s.perspective)
	if len(tlsConf.ServerName) > 0 {
		s.tokenStoreKey = tlsConf.ServerName
	} else {
		s.tokenStoreKey = conn.RemoteAddr().String()
	}
	if s.config.TokenStore != nil {
		if token := s.config.TokenStore.Pop(s.tokenStoreKey); token != nil {
			s.packer.SetToken(token.data)
		}
	}
	return s
}

func (s *connection) preSetup() {
	s.largestRcvdAppData = protocol.InvalidPacketNumber
	s.initialStream = newCryptoStream()
	s.handshakeStream = newCryptoStream()
	s.sendQueue = newSendQueue(s.conn)
	s.retransmissionQueue = newRetransmissionQueue()
	s.frameParser = *wire.NewFrameParser(s.config.EnableDatagrams)
	s.rttStats = &utils.RTTStats{}
	s.connFlowController = flowcontrol.NewConnectionFlowController(
		protocol.ByteCount(s.config.InitialConnectionReceiveWindow),
		protocol.ByteCount(s.config.MaxConnectionReceiveWindow),
		func(size protocol.ByteCount) bool {
			if s.config.AllowConnectionWindowIncrease == nil {
				return true
			}
			return s.config.AllowConnectionWindowIncrease(s, uint64(size))
		},
		s.rttStats,
		s.logger,
	)
	s.earlyConnReadyChan = make(chan struct{})
	s.streamsMap = newStreamsMap(
		s.ctx,
		s,
		s.queueControlFrame,
		s.newFlowController,
		uint64(s.config.MaxIncomingStreams),
		uint64(s.config.MaxIncomingUniStreams),
		s.perspective,
	)
	s.framer = newFramer(s.connFlowController)
	s.receivedPackets.Init(8)
	s.notifyReceivedPacket = make(chan struct{}, 1)
	s.closeChan = make(chan struct{}, 1)
	s.sendingScheduled = make(chan struct{}, 1)
	s.handshakeCompleteChan = make(chan struct{})

	now := time.Now()
	s.lastPacketReceivedTime = now
	s.creationTime = now

	s.datagramQueue = newDatagramQueue(s.scheduleSending, s.logger)
	s.connState.Version = s.version
}

// run the connection main loop
func (s *connection) run() (err error) {
	defer func() { s.ctxCancel(err) }()

	defer func() {
		// drain queued packets that will never be processed
		s.receivedPacketMx.Lock()
		defer s.receivedPacketMx.Unlock()

		for !s.receivedPackets.Empty() {
			p := s.receivedPackets.PopFront()
			p.buffer.Decrement()
			p.buffer.MaybeRelease()
		}
	}()

	s.timer = *newTimer()

	if err := s.cryptoStreamHandler.StartHandshake(s.ctx); err != nil {
		return err
	}
	if err := s.handleHandshakeEvents(time.Now()); err != nil {
		return err
	}
	go func() {
		if err := s.sendQueue.Run(); err != nil {
			s.destroyImpl(err)
		}
	}()

	if s.perspective == protocol.PerspectiveClient {
		s.scheduleSending() // so the ClientHello actually gets sent
	}

	var sendQueueAvailable <-chan struct{}

runLoop:
	for {
		if s.framer.QueuedTooManyControlFrames() {
			s.setCloseError(&closeError{err: &qerr.TransportError{ErrorCode: InternalError}})
			break runLoop
		}
		// Close immediately if requested
		select {
		case <-s.closeChan:
			break runLoop
		default:
		}

		// no need to set a timer if we can send packets immediately
		if s.pacingDeadline != deadlineSendImmediately {
			s.maybeResetTimer()
		}

		// 1st: handle undecryptable packets, if any.
		// This can only occur before completion of the handshake.
		if len(s.undecryptablePacketsToProcess) > 0 {
			var processedUndecryptablePacket bool
			queue := s.undecryptablePacketsToProcess
			s.undecryptablePacketsToProcess = nil
			for _, p := range queue {
				processed, err := s.handleOnePacket(p)
				if err != nil {
					s.setCloseError(&closeError{err: err})
					break runLoop
				}
				if processed {
					processedUndecryptablePacket = true
				}
			}
			if processedUndecryptablePacket {
				// if we processed any undecryptable packets, jump to the resetting of the timers directly
				continue
			}
		}

		// 2nd: receive packets.
		processed, err := s.handlePackets() // don't check receivedPackets.Len() in the run loop to avoid locking the mutex
		if err != nil {
			s.setCloseError(&closeError{err: err})
			break runLoop
		}

		// We don't need to wait for new events if:
		// * we processed packets: we probably need to send an ACK, and potentially more data
		// * the pacer allows us to send more packets immediately
		shouldProceedImmediately := sendQueueAvailable == nil && (processed || s.pacingDeadline.Equal(deadlineSendImmediately))
		if !shouldProceedImmediately {
			// 3rd: wait for something to happen:
			// * closing of the connection
			// * timer firing
			// * sending scheduled
			// * send queue available
			// * received packets
			select {
			case <-s.closeChan:
				break runLoop
			case <-s.timer.Chan():
				s.timer.SetRead()
			case <-s.sendingScheduled:
			case <-sendQueueAvailable:
			case <-s.notifyReceivedPacket:
				wasProcessed, err := s.handlePackets()
				if err != nil {
					s.setCloseError(&closeError{err: err})
					break runLoop
				}
				// if we processed any undecryptable packets, jump to the resetting of the timers directly
				if !wasProcessed {
					continue
				}
			}
		}

		// Check for loss detection timeout.
		// This could cause packets to be declared lost, and retransmissions to be enqueued.
		now := time.Now()
		if timeout := s.sentPacketHandler.GetLossDetectionTimeout(); !timeout.IsZero() && timeout.Before(now) {
			if err := s.sentPacketHandler.OnLossDetectionTimeout(now); err != nil {
				s.setCloseError(&closeError{err: err})
				break runLoop
			}
		}

		if keepAliveTime := s.nextKeepAliveTime(); !keepAliveTime.IsZero() && !now.Before(keepAliveTime) {
			// send a PING frame since there is no activity in the connection
			s.logger.Debugf("Sending a keep-alive PING to keep the connection alive.")
			s.framer.QueueControlFrame(&wire.PingFrame{})
			s.keepAlivePingSent = true
		} else if !s.handshakeComplete && now.Sub(s.creationTime) >= s.config.handshakeTimeout() {
			s.destroyImpl(qerr.ErrHandshakeTimeout)
			break runLoop
		} else {
			idleTimeoutStartTime := s.idleTimeoutStartTime()
			if (!s.handshakeComplete && now.Sub(idleTimeoutStartTime) >= s.config.HandshakeIdleTimeout) ||
				(s.handshakeComplete && now.After(s.nextIdleTimeoutTime())) {
				s.destroyImpl(qerr.ErrIdleTimeout)
				break runLoop
			}
		}

		if s.perspective == protocol.PerspectiveClient {
			pm := s.pathManagerOutgoing.Load()
			if pm != nil {
				tr, ok := pm.ShouldSwitchPath()
				if ok {
					s.switchToNewPath(tr, now)
				}
			}
		}

		if s.sendQueue.WouldBlock() {
			// The send queue is still busy sending out packets. Wait until there's space to enqueue new packets.
			sendQueueAvailable = s.sendQueue.Available()
			// Cancel the pacing timer, as we can't send any more packets until the send queue is available again.
			s.pacingDeadline = time.Time{}
			continue
		}

		if s.closeErr.Load() != nil {
			break runLoop
		}

		if err := s.triggerSending(now); err != nil {
			s.setCloseError(&closeError{err: err})
			break runLoop
		}
		if s.sendQueue.WouldBlock() {
			// The send queue is still busy sending out packets. Wait until there's space to enqueue new packets.
			sendQueueAvailable = s.sendQueue.Available()
			// Cancel the pacing timer, as we can't send any more packets until the send queue is available again.
			s.pacingDeadline = time.Time{}
		} else {
			sendQueueAvailable = nil
		}
	}

	closeErr := s.closeErr.Load()
	s.cryptoStreamHandler.Close()
	s.sendQueue.Close() // close the send queue before sending the CONNECTION_CLOSE
	s.handleCloseError(closeErr)
	if s.tracer != nil && s.tracer.Close != nil {
		if e := (&errCloseForRecreating{}); !errors.As(closeErr.err, &e) {
			s.tracer.Close()
		}
	}
	s.logger.Infof("Connection %s closed.", s.logID)
	s.timer.Stop()
	return closeErr.err
}

// blocks until the early connection can be used
func (s *connection) earlyConnReady() <-chan struct{} {
	return s.earlyConnReadyChan
}

func (s *connection) HandshakeComplete() <-chan struct{} {
	return s.handshakeCompleteChan
}

func (s *connection) Context() context.Context {
	return s.ctx
}

func (s *connection) supportsDatagrams() bool {
	return s.peerParams.MaxDatagramFrameSize > 0
}

func (s *connection) ConnectionState() ConnectionState {
	s.connStateMutex.Lock()
	defer s.connStateMutex.Unlock()
	cs := s.cryptoStreamHandler.ConnectionState()
	s.connState.TLS = cs.ConnectionState
	s.connState.Used0RTT = cs.Used0RTT
	s.connState.GSO = s.conn.capabilities().GSO
	return s.connState
}

// Time when the connection should time out
func (s *connection) nextIdleTimeoutTime() time.Time {
	idleTimeout := max(s.idleTimeout, s.rttStats.PTO(true)*3)
	return s.idleTimeoutStartTime().Add(idleTimeout)
}

// Time when the next keep-alive packet should be sent.
// It returns a zero time if no keep-alive should be sent.
func (s *connection) nextKeepAliveTime() time.Time {
	if s.config.KeepAlivePeriod == 0 || s.keepAlivePingSent {
		return time.Time{}
	}
	keepAliveInterval := max(s.keepAliveInterval, s.rttStats.PTO(true)*3/2)
	return s.lastPacketReceivedTime.Add(keepAliveInterval)
}

func (s *connection) maybeResetTimer() {
	var deadline time.Time
	if !s.handshakeComplete {
		deadline = s.creationTime.Add(s.config.handshakeTimeout())
		if t := s.idleTimeoutStartTime().Add(s.config.HandshakeIdleTimeout); t.Before(deadline) {
			deadline = t
		}
	} else {
		if keepAliveTime := s.nextKeepAliveTime(); !keepAliveTime.IsZero() {
			deadline = keepAliveTime
		} else {
			deadline = s.nextIdleTimeoutTime()
		}
	}

	s.timer.SetTimer(
		deadline,
		s.receivedPacketHandler.GetAlarmTimeout(),
		s.sentPacketHandler.GetLossDetectionTimeout(),
		s.pacingDeadline,
	)
}

func (s *connection) idleTimeoutStartTime() time.Time {
	startTime := s.lastPacketReceivedTime
	if t := s.firstAckElicitingPacketAfterIdleSentTime; t.After(startTime) {
		startTime = t
	}
	return startTime
}

func (s *connection) switchToNewPath(tr *Transport, now time.Time) {
	initialPacketSize := protocol.ByteCount(s.config.InitialPacketSize)
	s.sentPacketHandler.MigratedPath(now, initialPacketSize)
	maxPacketSize := protocol.ByteCount(protocol.MaxPacketBufferSize)
	if s.peerParams.MaxUDPPayloadSize > 0 && s.peerParams.MaxUDPPayloadSize < maxPacketSize {
		maxPacketSize = s.peerParams.MaxUDPPayloadSize
	}
	s.mtuDiscoverer.Reset(now, initialPacketSize, maxPacketSize)
	s.conn = newSendConn(tr.conn, s.conn.RemoteAddr(), packetInfo{}, utils.DefaultLogger) // TODO: find a better way
	s.sendQueue.Close()
	s.sendQueue = newSendQueue(s.conn)
	go func() {
		if err := s.sendQueue.Run(); err != nil {
			s.destroyImpl(err)
		}
	}()
}

func (s *connection) handleHandshakeComplete(now time.Time) error {
	defer close(s.handshakeCompleteChan)
	// Once the handshake completes, we have derived 1-RTT keys.
	// There's no point in queueing undecryptable packets for later decryption anymore.
	s.undecryptablePackets = nil

	s.connIDManager.SetHandshakeComplete()
	s.connIDGenerator.SetHandshakeComplete()

	if s.tracer != nil && s.tracer.ChoseALPN != nil {
		s.tracer.ChoseALPN(s.cryptoStreamHandler.ConnectionState().NegotiatedProtocol)
	}

	// The server applies transport parameters right away, but the client side has to wait for handshake completion.
	// During a 0-RTT connection, the client is only allowed to use the new transport parameters for 1-RTT packets.
	if s.perspective == protocol.PerspectiveClient {
		s.applyTransportParameters()
		return nil
	}

	// All these only apply to the server side.
	if err := s.handleHandshakeConfirmed(now); err != nil {
		return err
	}

	ticket, err := s.cryptoStreamHandler.GetSessionTicket()
	if err != nil {
		return err
	}
	if ticket != nil { // may be nil if session tickets are disabled via tls.Config.SessionTicketsDisabled
		s.oneRTTStream.Write(ticket)
		for s.oneRTTStream.HasData() {
			s.queueControlFrame(s.oneRTTStream.PopCryptoFrame(protocol.MaxPostHandshakeCryptoFrameSize))
		}
	}
	token, err := s.tokenGenerator.NewToken(s.conn.RemoteAddr())
	if err != nil {
		return err
	}
	s.queueControlFrame(&wire.NewTokenFrame{Token: token})
	s.queueControlFrame(&wire.HandshakeDoneFrame{})
	return nil
}

func (s *connection) handleHandshakeConfirmed(now time.Time) error {
	if err := s.dropEncryptionLevel(protocol.EncryptionHandshake, now); err != nil {
		return err
	}

	s.handshakeConfirmed = true
	s.cryptoStreamHandler.SetHandshakeConfirmed()

	if !s.config.DisablePathMTUDiscovery && s.conn.capabilities().DF {
		s.mtuDiscoverer.Start(now)
	}
	return nil
}

func (s *connection) handlePackets() (wasProcessed bool, _ error) {
	// Now process all packets in the receivedPackets channel.
	// Limit the number of packets to the length of the receivedPackets channel,
	// so we eventually get a chance to send out an ACK when receiving a lot of packets.
	s.receivedPacketMx.Lock()
	numPackets := s.receivedPackets.Len()
	if numPackets == 0 {
		s.receivedPacketMx.Unlock()
		return false, nil
	}

	var hasMorePackets bool
	for i := 0; i < numPackets; i++ {
		if i > 0 {
			s.receivedPacketMx.Lock()
		}
		p := s.receivedPackets.PopFront()
		hasMorePackets = !s.receivedPackets.Empty()
		s.receivedPacketMx.Unlock()

		processed, err := s.handleOnePacket(p)
		if err != nil {
			return false, err
		}
		if processed {
			wasProcessed = true
		}
		if !hasMorePackets {
			break
		}
		// only process a single packet at a time before handshake completion
		if !s.handshakeComplete {
			break
		}
	}
	if hasMorePackets {
		select {
		case s.notifyReceivedPacket <- struct{}{}:
		default:
		}
	}
	return wasProcessed, nil
}

func (s *connection) handleOnePacket(rp receivedPacket) (wasProcessed bool, _ error) {
	s.sentPacketHandler.ReceivedBytes(rp.Size(), rp.rcvTime)

	if wire.IsVersionNegotiationPacket(rp.data) {
		s.handleVersionNegotiationPacket(rp)
		return false, nil
	}

	var counter uint8
	var lastConnID protocol.ConnectionID
	data := rp.data
	p := rp
	for len(data) > 0 {
		if counter > 0 {
			p = *(p.Clone())
			p.data = data

			destConnID, err := wire.ParseConnectionID(p.data, s.srcConnIDLen)
			if err != nil {
				if s.tracer != nil && s.tracer.DroppedPacket != nil {
					s.tracer.DroppedPacket(logging.PacketTypeNotDetermined, protocol.InvalidPacketNumber, protocol.ByteCount(len(data)), logging.PacketDropHeaderParseError)
				}
				s.logger.Debugf("error parsing packet, couldn't parse connection ID: %s", err)
				break
			}
			if destConnID != lastConnID {
				if s.tracer != nil && s.tracer.DroppedPacket != nil {
					s.tracer.DroppedPacket(logging.PacketTypeNotDetermined, protocol.InvalidPacketNumber, protocol.ByteCount(len(data)), logging.PacketDropUnknownConnectionID)
				}
				s.logger.Debugf("coalesced packet has different destination connection ID: %s, expected %s", destConnID, lastConnID)
				break
			}
		}

		if wire.IsLongHeaderPacket(p.data[0]) {
			hdr, packetData, rest, err := wire.ParsePacket(p.data)
			if err != nil {
				if s.tracer != nil && s.tracer.DroppedPacket != nil {
					dropReason := logging.PacketDropHeaderParseError
					if err == wire.ErrUnsupportedVersion {
						dropReason = logging.PacketDropUnsupportedVersion
					}
					s.tracer.DroppedPacket(logging.PacketTypeNotDetermined, protocol.InvalidPacketNumber, protocol.ByteCount(len(data)), dropReason)
				}
				s.logger.Debugf("error parsing packet: %s", err)
				break
			}
			lastConnID = hdr.DestConnectionID

			if hdr.Version != s.version {
				if s.tracer != nil && s.tracer.DroppedPacket != nil {
					s.tracer.DroppedPacket(logging.PacketTypeFromHeader(hdr), protocol.InvalidPacketNumber, protocol.ByteCount(len(data)), logging.PacketDropUnexpectedVersion)
				}
				s.logger.Debugf("Dropping packet with version %x. Expected %x.", hdr.Version, s.version)
				break
			}

			if counter > 0 {
				p.buffer.Split()
			}
			counter++

			// only log if this actually a coalesced packet
			if s.logger.Debug() && (counter > 1 || len(rest) > 0) {
				s.logger.Debugf("Parsed a coalesced packet. Part %d: %d bytes. Remaining: %d bytes.", counter, len(packetData), len(rest))
			}

			p.data = packetData

			processed, err := s.handleLongHeaderPacket(p, hdr)
			if err != nil {
				return false, err
			}
			if processed {
				wasProcessed = true
			}
			data = rest
		} else {
			if counter > 0 {
				p.buffer.Split()
			}
			processed, err := s.handleShortHeaderPacket(p, counter > 0)
			if err != nil {
				return false, err
			}
			if processed {
				wasProcessed = true
			}
			break
		}
	}

	p.buffer.MaybeRelease()
	return wasProcessed, nil
}

func (s *connection) handleShortHeaderPacket(p receivedPacket, isCoalesced bool) (wasProcessed bool, _ error) {
	var wasQueued bool

	defer func() {
		// Put back the packet buffer if the packet wasn't queued for later decryption.
		if !wasQueued {
			p.buffer.Decrement()
		}
	}()

	destConnID, err := wire.ParseConnectionID(p.data, s.srcConnIDLen)
	if err != nil {
		s.tracer.DroppedPacket(logging.PacketType1RTT, protocol.InvalidPacketNumber, protocol.ByteCount(len(p.data)), logging.PacketDropHeaderParseError)
		return false, nil
	}
	pn, pnLen, keyPhase, data, err := s.unpacker.UnpackShortHeader(p.rcvTime, p.data)
	if err != nil {
		// Stateless reset packets (see RFC 9000, section 10.3):
		// * fill the entire UDP datagram (i.e. they cannot be part of a coalesced packet)
		// * are short header packets (first bit is 0)
		// * have the QUIC bit set (second bit is 1)
		// * are at least 21 bytes long
		if !isCoalesced && len(p.data) >= protocol.MinReceivedStatelessResetSize && p.data[0]&0b11000000 == 0b01000000 {
			token := protocol.StatelessResetToken(p.data[len(p.data)-16:])
			if s.connIDManager.IsActiveStatelessResetToken(token) {
				return false, &StatelessResetError{}
			}
		}
		wasQueued, err = s.handleUnpackError(err, p, logging.PacketType1RTT)
		return false, err
	}
	s.largestRcvdAppData = max(s.largestRcvdAppData, pn)

	if s.logger.Debug() {
		s.logger.Debugf("<- Reading packet %d (%d bytes) for connection %s, 1-RTT", pn, p.Size(), destConnID)
		wire.LogShortHeader(s.logger, destConnID, pn, pnLen, keyPhase)
	}

	if s.receivedPacketHandler.IsPotentiallyDuplicate(pn, protocol.Encryption1RTT) {
		s.logger.Debugf("Dropping (potentially) duplicate packet.")
		if s.tracer != nil && s.tracer.DroppedPacket != nil {
			s.tracer.DroppedPacket(logging.PacketType1RTT, pn, p.Size(), logging.PacketDropDuplicate)
		}
		return false, nil
	}

	var log func([]logging.Frame)
	if s.tracer != nil && s.tracer.ReceivedShortHeaderPacket != nil {
		log = func(frames []logging.Frame) {
			s.tracer.ReceivedShortHeaderPacket(
				&logging.ShortHeader{
					DestConnectionID: destConnID,
					PacketNumber:     pn,
					PacketNumberLen:  pnLen,
					KeyPhase:         keyPhase,
				},
				p.Size(),
				p.ecn,
				frames,
			)
		}
	}
	isNonProbing, pathChallenge, err := s.handleUnpackedShortHeaderPacket(destConnID, pn, data, p.ecn, p.rcvTime, log)
	if err != nil {
		return false, err
	}

	// In RFC 9000, only the client can migrate between paths.
	if s.perspective == protocol.PerspectiveClient {
		return true, nil
	}
	if addrsEqual(p.remoteAddr, s.RemoteAddr()) {
		return true, nil
	}

	var shouldSwitchPath bool
	if s.pathManager == nil {
		s.pathManager = newPathManager(
			s.connIDManager.GetConnIDForPath,
			s.connIDManager.RetireConnIDForPath,
			s.logger,
		)
	}
	destConnID, frames, shouldSwitchPath := s.pathManager.HandlePacket(p.remoteAddr, p.rcvTime, pathChallenge, isNonProbing)
	if len(frames) > 0 {
		probe, buf, err := s.packer.PackPathProbePacket(destConnID, frames, s.version)
		if err != nil {
			return true, err
		}
		s.logger.Debugf("sending path probe packet to %s", p.remoteAddr)
		s.logShortHeaderPacket(probe.DestConnID, probe.Ack, probe.Frames, probe.StreamFrames, probe.PacketNumber, probe.PacketNumberLen, probe.KeyPhase, protocol.ECNNon, buf.Len(), false)
		s.registerPackedShortHeaderPacket(probe, protocol.ECNNon, p.rcvTime)
		s.sendQueue.SendProbe(buf, p.remoteAddr)
	}
	// We only switch paths in response to the highest-numbered non-probing packet,
	// see section 9.3 of RFC 9000.
	if !shouldSwitchPath || pn != s.largestRcvdAppData {
		return true, nil
	}
	s.pathManager.SwitchToPath(p.remoteAddr)
	s.sentPacketHandler.MigratedPath(p.rcvTime, protocol.ByteCount(s.config.InitialPacketSize))
	maxPacketSize := protocol.ByteCount(protocol.MaxPacketBufferSize)
	if s.peerParams.MaxUDPPayloadSize > 0 && s.peerParams.MaxUDPPayloadSize < maxPacketSize {
		maxPacketSize = s.peerParams.MaxUDPPayloadSize
	}
	s.mtuDiscoverer.Reset(
		p.rcvTime,
		protocol.ByteCount(s.config.InitialPacketSize),
		maxPacketSize,
	)
	s.conn.ChangeRemoteAddr(p.remoteAddr, p.info)
	return true, nil
}

func (s *connection) handleLongHeaderPacket(p receivedPacket, hdr *wire.Header) (wasProcessed bool, _ error) {
	var wasQueued bool

	defer func() {
		// Put back the packet buffer if the packet wasn't queued for later decryption.
		if !wasQueued {
			p.buffer.Decrement()
		}
	}()

	if hdr.Type == protocol.PacketTypeRetry {
		return s.handleRetryPacket(hdr, p.data, p.rcvTime), nil
	}

	// The server can change the source connection ID with the first Handshake packet.
	// After this, all packets with a different source connection have to be ignored.
	if s.receivedFirstPacket && hdr.Type == protocol.PacketTypeInitial && hdr.SrcConnectionID != s.handshakeDestConnID {
		if s.tracer != nil && s.tracer.DroppedPacket != nil {
			s.tracer.DroppedPacket(logging.PacketTypeInitial, protocol.InvalidPacketNumber, p.Size(), logging.PacketDropUnknownConnectionID)
		}
		s.logger.Debugf("Dropping Initial packet (%d bytes) with unexpected source connection ID: %s (expected %s)", p.Size(), hdr.SrcConnectionID, s.handshakeDestConnID)
		return false, nil
	}
	// drop 0-RTT packets, if we are a client
	if s.perspective == protocol.PerspectiveClient && hdr.Type == protocol.PacketType0RTT {
		if s.tracer != nil && s.tracer.DroppedPacket != nil {
			s.tracer.DroppedPacket(logging.PacketType0RTT, protocol.InvalidPacketNumber, p.Size(), logging.PacketDropUnexpectedPacket)
		}
		return false, nil
	}

	packet, err := s.unpacker.UnpackLongHeader(hdr, p.data)
	if err != nil {
		wasQueued, err = s.handleUnpackError(err, p, logging.PacketTypeFromHeader(hdr))
		return false, err
	}

	if s.logger.Debug() {
		s.logger.Debugf("<- Reading packet %d (%d bytes) for connection %s, %s", packet.hdr.PacketNumber, p.Size(), hdr.DestConnectionID, packet.encryptionLevel)
		packet.hdr.Log(s.logger)
	}

	if pn := packet.hdr.PacketNumber; s.receivedPacketHandler.IsPotentiallyDuplicate(pn, packet.encryptionLevel) {
		s.logger.Debugf("Dropping (potentially) duplicate packet.")
		if s.tracer != nil && s.tracer.DroppedPacket != nil {
			s.tracer.DroppedPacket(logging.PacketTypeFromHeader(hdr), pn, p.Size(), logging.PacketDropDuplicate)
		}
		return false, nil
	}

	if err := s.handleUnpackedLongHeaderPacket(packet, p.ecn, p.rcvTime, p.Size()); err != nil {
		return false, err
	}
	return true, nil
}

func (s *connection) handleUnpackError(err error, p receivedPacket, pt logging.PacketType) (wasQueued bool, _ error) {
	switch err {
	case handshake.ErrKeysDropped:
		if s.tracer != nil && s.tracer.DroppedPacket != nil {
			s.tracer.DroppedPacket(pt, protocol.InvalidPacketNumber, p.Size(), logging.PacketDropKeyUnavailable)
		}
		s.logger.Debugf("Dropping %s packet (%d bytes) because we already dropped the keys.", pt, p.Size())
		return false, nil
	case handshake.ErrKeysNotYetAvailable:
		// Sealer for this encryption level not yet available.
		// Try again later.
		s.tryQueueingUndecryptablePacket(p, pt)
		return true, nil
	case wire.ErrInvalidReservedBits:
		return false, &qerr.TransportError{
			ErrorCode:    qerr.ProtocolViolation,
			ErrorMessage: err.Error(),
		}
	case handshake.ErrDecryptionFailed:
		// This might be a packet injected by an attacker. Drop it.
		if s.tracer != nil && s.tracer.DroppedPacket != nil {
			s.tracer.DroppedPacket(pt, protocol.InvalidPacketNumber, p.Size(), logging.PacketDropPayloadDecryptError)
		}
		s.logger.Debugf("Dropping %s packet (%d bytes) that could not be unpacked. Error: %s", pt, p.Size(), err)
		return false, nil
	default:
		var headerErr *headerParseError
		if errors.As(err, &headerErr) {
			// This might be a packet injected by an attacker. Drop it.
			if s.tracer != nil && s.tracer.DroppedPacket != nil {
				s.tracer.DroppedPacket(pt, protocol.InvalidPacketNumber, p.Size(), logging.PacketDropHeaderParseError)
			}
			s.logger.Debugf("Dropping %s packet (%d bytes) for which we couldn't unpack the header. Error: %s", pt, p.Size(), err)
			return false, nil
		}
		// This is an error returned by the AEAD (other than ErrDecryptionFailed).
		// For example, a PROTOCOL_VIOLATION due to key updates.
		return false, err
	}
}

func (s *connection) handleRetryPacket(hdr *wire.Header, data []byte, rcvTime time.Time) bool /* was this a valid Retry */ {
	if s.perspective == protocol.PerspectiveServer {
		if s.tracer != nil && s.tracer.DroppedPacket != nil {
			s.tracer.DroppedPacket(logging.PacketTypeRetry, protocol.InvalidPacketNumber, protocol.ByteCount(len(data)), logging.PacketDropUnexpectedPacket)
		}
		s.logger.Debugf("Ignoring Retry.")
		return false
	}
	if s.receivedFirstPacket {
		if s.tracer != nil && s.tracer.DroppedPacket != nil {
			s.tracer.DroppedPacket(logging.PacketTypeRetry, protocol.InvalidPacketNumber, protocol.ByteCount(len(data)), logging.PacketDropUnexpectedPacket)
		}
		s.logger.Debugf("Ignoring Retry, since we already received a packet.")
		return false
	}
	destConnID := s.connIDManager.Get()
	if hdr.SrcConnectionID == destConnID {
		if s.tracer != nil && s.tracer.DroppedPacket != nil {
			s.tracer.DroppedPacket(logging.PacketTypeRetry, protocol.InvalidPacketNumber, protocol.ByteCount(len(data)), logging.PacketDropUnexpectedPacket)
		}
		s.logger.Debugf("Ignoring Retry, since the server didn't change the Source Connection ID.")
		return false
	}
	// If a token is already set, this means that we already received a Retry from the server.
	// Ignore this Retry packet.
	if s.receivedRetry {
		s.logger.Debugf("Ignoring Retry, since a Retry was already received.")
		return false
	}

	tag := handshake.GetRetryIntegrityTag(data[:len(data)-16], destConnID, hdr.Version)
	if !bytes.Equal(data[len(data)-16:], tag[:]) {
		if s.tracer != nil && s.tracer.DroppedPacket != nil {
			s.tracer.DroppedPacket(logging.PacketTypeRetry, protocol.InvalidPacketNumber, protocol.ByteCount(len(data)), logging.PacketDropPayloadDecryptError)
		}
		s.logger.Debugf("Ignoring spoofed Retry. Integrity Tag doesn't match.")
		return false
	}

	newDestConnID := hdr.SrcConnectionID
	s.receivedRetry = true
	s.sentPacketHandler.ResetForRetry(rcvTime)
	s.handshakeDestConnID = newDestConnID
	s.retrySrcConnID = &newDestConnID
	s.cryptoStreamHandler.ChangeConnectionID(newDestConnID)
	s.packer.SetToken(hdr.Token)
	s.connIDManager.ChangeInitialConnID(newDestConnID)

	if s.logger.Debug() {
		s.logger.Debugf("<- Received Retry:")
		(&wire.ExtendedHeader{Header: *hdr}).Log(s.logger)
		s.logger.Debugf("Switching destination connection ID to: %s", hdr.SrcConnectionID)
	}
	if s.tracer != nil && s.tracer.ReceivedRetry != nil {
		s.tracer.ReceivedRetry(hdr)
	}

	s.scheduleSending()
	return true
}

func (s *connection) handleVersionNegotiationPacket(p receivedPacket) {
	if s.perspective == protocol.PerspectiveServer || // servers never receive version negotiation packets
		s.receivedFirstPacket || s.versionNegotiated { // ignore delayed / duplicated version negotiation packets
		if s.tracer != nil && s.tracer.DroppedPacket != nil {
			s.tracer.DroppedPacket(logging.PacketTypeVersionNegotiation, protocol.InvalidPacketNumber, p.Size(), logging.PacketDropUnexpectedPacket)
		}
		return
	}

	src, dest, supportedVersions, err := wire.ParseVersionNegotiationPacket(p.data)
	if err != nil {
		if s.tracer != nil && s.tracer.DroppedPacket != nil {
			s.tracer.DroppedPacket(logging.PacketTypeVersionNegotiation, protocol.InvalidPacketNumber, p.Size(), logging.PacketDropHeaderParseError)
		}
		s.logger.Debugf("Error parsing Version Negotiation packet: %s", err)
		return
	}

	for _, v := range supportedVersions {
		if v == s.version {
			if s.tracer != nil && s.tracer.DroppedPacket != nil {
				s.tracer.DroppedPacket(logging.PacketTypeVersionNegotiation, protocol.InvalidPacketNumber, p.Size(), logging.PacketDropUnexpectedVersion)
			}
			// The Version Negotiation packet contains the version that we offered.
			// This might be a packet sent by an attacker, or it was corrupted.
			return
		}
	}

	s.logger.Infof("Received a Version Negotiation packet. Supported Versions: %s", supportedVersions)
	if s.tracer != nil && s.tracer.ReceivedVersionNegotiationPacket != nil {
		s.tracer.ReceivedVersionNegotiationPacket(dest, src, supportedVersions)
	}
	newVersion, ok := protocol.ChooseSupportedVersion(s.config.Versions, supportedVersions)
	if !ok {
		s.destroyImpl(&VersionNegotiationError{
			Ours:   s.config.Versions,
			Theirs: supportedVersions,
		})
		s.logger.Infof("No compatible QUIC version found.")
		return
	}
	if s.tracer != nil && s.tracer.NegotiatedVersion != nil {
		s.tracer.NegotiatedVersion(newVersion, s.config.Versions, supportedVersions)
	}

	s.logger.Infof("Switching to QUIC version %s.", newVersion)
	nextPN, _ := s.sentPacketHandler.PeekPacketNumber(protocol.EncryptionInitial)
	s.destroyImpl(&errCloseForRecreating{
		nextPacketNumber: nextPN,
		nextVersion:      newVersion,
	})
}

func (s *connection) handleUnpackedLongHeaderPacket(
	packet *unpackedPacket,
	ecn protocol.ECN,
	rcvTime time.Time,
	packetSize protocol.ByteCount, // only for logging
) error {
	if !s.receivedFirstPacket {
		s.receivedFirstPacket = true
		if !s.versionNegotiated && s.tracer != nil && s.tracer.NegotiatedVersion != nil {
			var clientVersions, serverVersions []protocol.Version
			switch s.perspective {
			case protocol.PerspectiveClient:
				clientVersions = s.config.Versions
			case protocol.PerspectiveServer:
				serverVersions = s.config.Versions
			}
			s.tracer.NegotiatedVersion(s.version, clientVersions, serverVersions)
		}
		// The server can change the source connection ID with the first Handshake packet.
		if s.perspective == protocol.PerspectiveClient && packet.hdr.SrcConnectionID != s.handshakeDestConnID {
			cid := packet.hdr.SrcConnectionID
			s.logger.Debugf("Received first packet. Switching destination connection ID to: %s", cid)
			s.handshakeDestConnID = cid
			s.connIDManager.ChangeInitialConnID(cid)
		}
		// We create the connection as soon as we receive the first packet from the client.
		// We do that before authenticating the packet.
		// That means that if the source connection ID was corrupted,
		// we might have created a connection with an incorrect source connection ID.
		// Once we authenticate the first packet, we need to update it.
		if s.perspective == protocol.PerspectiveServer {
			if packet.hdr.SrcConnectionID != s.handshakeDestConnID {
				s.handshakeDestConnID = packet.hdr.SrcConnectionID
				s.connIDManager.ChangeInitialConnID(packet.hdr.SrcConnectionID)
			}
			if s.tracer != nil && s.tracer.StartedConnection != nil {
				s.tracer.StartedConnection(
					s.conn.LocalAddr(),
					s.conn.RemoteAddr(),
					packet.hdr.SrcConnectionID,
					packet.hdr.DestConnectionID,
				)
			}
		}
	}

	if s.perspective == protocol.PerspectiveServer && packet.encryptionLevel == protocol.EncryptionHandshake &&
		!s.droppedInitialKeys {
		// On the server side, Initial keys are dropped as soon as the first Handshake packet is received.
		// See Section 4.9.1 of RFC 9001.
		if err := s.dropEncryptionLevel(protocol.EncryptionInitial, rcvTime); err != nil {
			return err
		}
	}

	s.lastPacketReceivedTime = rcvTime
	s.firstAckElicitingPacketAfterIdleSentTime = time.Time{}
	s.keepAlivePingSent = false

	if packet.hdr.Type == protocol.PacketType0RTT {
		s.largestRcvdAppData = max(s.largestRcvdAppData, packet.hdr.PacketNumber)
	}

	var log func([]logging.Frame)
	if s.tracer != nil && s.tracer.ReceivedLongHeaderPacket != nil {
		log = func(frames []logging.Frame) {
			s.tracer.ReceivedLongHeaderPacket(packet.hdr, packetSize, ecn, frames)
		}
	}
	isAckEliciting, _, _, err := s.handleFrames(packet.data, packet.hdr.DestConnectionID, packet.encryptionLevel, log, rcvTime)
	if err != nil {
		return err
	}
	return s.receivedPacketHandler.ReceivedPacket(packet.hdr.PacketNumber, ecn, packet.encryptionLevel, rcvTime, isAckEliciting)
}

func (s *connection) handleUnpackedShortHeaderPacket(
	destConnID protocol.ConnectionID,
	pn protocol.PacketNumber,
	data []byte,
	ecn protocol.ECN,
	rcvTime time.Time,
	log func([]logging.Frame),
) (isNonProbing bool, pathChallenge *wire.PathChallengeFrame, _ error) {
	s.lastPacketReceivedTime = rcvTime
	s.firstAckElicitingPacketAfterIdleSentTime = time.Time{}
	s.keepAlivePingSent = false

	isAckEliciting, isNonProbing, pathChallenge, err := s.handleFrames(data, destConnID, protocol.Encryption1RTT, log, rcvTime)
	if err != nil {
		return false, nil, err
	}
	if err := s.receivedPacketHandler.ReceivedPacket(pn, ecn, protocol.Encryption1RTT, rcvTime, isAckEliciting); err != nil {
		return false, nil, err
	}
	return isNonProbing, pathChallenge, nil
}

// handleFrames parses the frames, one after the other, and handles them.
// It returns the last PATH_CHALLENGE frame contained in the packet, if any.
func (s *connection) handleFrames(
	data []byte,
	destConnID protocol.ConnectionID,
	encLevel protocol.EncryptionLevel,
	log func([]logging.Frame),
	rcvTime time.Time,
) (isAckEliciting, isNonProbing bool, pathChallenge *wire.PathChallengeFrame, _ error) {
	// Only used for tracing.
	// If we're not tracing, this slice will always remain empty.
	var frames []logging.Frame
	if log != nil {
		frames = make([]logging.Frame, 0, 4)
	}
	handshakeWasComplete := s.handshakeComplete
	var handleErr error
	for len(data) > 0 {
		l, frame, err := s.frameParser.ParseNext(data, encLevel, s.version)
		if err != nil {
			return false, false, nil, err
		}
		data = data[l:]
		if frame == nil {
			break
		}
		if ackhandler.IsFrameAckEliciting(frame) {
			isAckEliciting = true
		}
		if !wire.IsProbingFrame(frame) {
			isNonProbing = true
		}
		if log != nil {
			frames = append(frames, toLoggingFrame(frame))
		}
		// An error occurred handling a previous frame.
		// Don't handle the current frame.
		if handleErr != nil {
			continue
		}
		pc, err := s.handleFrame(frame, encLevel, destConnID, rcvTime)
		if err != nil {
			if log == nil {
				return false, false, nil, err
			}
			// If we're logging, we need to keep parsing (but not handling) all frames.
			handleErr = err
		}
		if pc != nil {
			pathChallenge = pc
		}
	}

	if log != nil {
		log(frames)
		if handleErr != nil {
			return false, false, nil, handleErr
		}
	}

	// Handle completion of the handshake after processing all the frames.
	// This ensures that we correctly handle the following case on the server side:
	// We receive a Handshake packet that contains the CRYPTO frame that allows us to complete the handshake,
	// and an ACK serialized after that CRYPTO frame. In this case, we still want to process the ACK frame.
	if !handshakeWasComplete && s.handshakeComplete {
		if err := s.handleHandshakeComplete(rcvTime); err != nil {
			return false, false, nil, err
		}
	}
	return
}

func (s *connection) handleFrame(
	f wire.Frame,
	encLevel protocol.EncryptionLevel,
	destConnID protocol.ConnectionID,
	rcvTime time.Time,
) (pathChallenge *wire.PathChallengeFrame, _ error) {
	var err error
	wire.LogFrame(s.logger, f, false)
	switch frame := f.(type) {
	case *wire.CryptoFrame:
		err = s.handleCryptoFrame(frame, encLevel, rcvTime)
	case *wire.StreamFrame:
		err = s.handleStreamFrame(frame, rcvTime)
	case *wire.AckFrame:
		err = s.handleAckFrame(frame, encLevel, rcvTime)
	case *wire.ConnectionCloseFrame:
		err = s.handleConnectionCloseFrame(frame)
	case *wire.ResetStreamFrame:
		err = s.handleResetStreamFrame(frame, rcvTime)
	case *wire.MaxDataFrame:
		s.handleMaxDataFrame(frame)
	case *wire.MaxStreamDataFrame:
		err = s.handleMaxStreamDataFrame(frame)
	case *wire.MaxStreamsFrame:
		s.handleMaxStreamsFrame(frame)
	case *wire.DataBlockedFrame:
	case *wire.StreamDataBlockedFrame:
		err = s.handleStreamDataBlockedFrame(frame)
	case *wire.StreamsBlockedFrame:
	case *wire.StopSendingFrame:
		err = s.handleStopSendingFrame(frame)
	case *wire.PingFrame:
	case *wire.PathChallengeFrame:
		s.handlePathChallengeFrame(frame)
		pathChallenge = frame
	case *wire.PathResponseFrame:
		err = s.handlePathResponseFrame(frame)
	case *wire.NewTokenFrame:
		err = s.handleNewTokenFrame(frame)
	case *wire.NewConnectionIDFrame:
		err = s.handleNewConnectionIDFrame(frame)
	case *wire.RetireConnectionIDFrame:
		err = s.handleRetireConnectionIDFrame(frame, destConnID)
	case *wire.HandshakeDoneFrame:
		err = s.handleHandshakeDoneFrame(rcvTime)
	case *wire.DatagramFrame:
		err = s.handleDatagramFrame(frame)
	default:
		err = fmt.Errorf("unexpected frame type: %s", reflect.ValueOf(&frame).Elem().Type().Name())
	}
	return pathChallenge, err
}

// handlePacket is called by the server with a new packet
func (s *connection) handlePacket(p receivedPacket) {
	s.receivedPacketMx.Lock()
	// Discard packets once the amount of queued packets is larger than
	// the channel size, protocol.MaxConnUnprocessedPackets
	if s.receivedPackets.Len() >= protocol.MaxConnUnprocessedPackets {
		if s.tracer != nil && s.tracer.DroppedPacket != nil {
			s.tracer.DroppedPacket(logging.PacketTypeNotDetermined, protocol.InvalidPacketNumber, p.Size(), logging.PacketDropDOSPrevention)
		}
		s.receivedPacketMx.Unlock()
		return
	}
	s.receivedPackets.PushBack(p)
	s.receivedPacketMx.Unlock()

	select {
	case s.notifyReceivedPacket <- struct{}{}:
	default:
	}
}

func (s *connection) handleConnectionCloseFrame(frame *wire.ConnectionCloseFrame) error {
	if frame.IsApplicationError {
		return &qerr.ApplicationError{
			Remote:       true,
			ErrorCode:    qerr.ApplicationErrorCode(frame.ErrorCode),
			ErrorMessage: frame.ReasonPhrase,
		}
	}
	return &qerr.TransportError{
		Remote:       true,
		ErrorCode:    qerr.TransportErrorCode(frame.ErrorCode),
		FrameType:    frame.FrameType,
		ErrorMessage: frame.ReasonPhrase,
	}
}

func (s *connection) handleCryptoFrame(frame *wire.CryptoFrame, encLevel protocol.EncryptionLevel, rcvTime time.Time) error {
	if err := s.cryptoStreamManager.HandleCryptoFrame(frame, encLevel); err != nil {
		return err
	}
	for {
		data := s.cryptoStreamManager.GetCryptoData(encLevel)
		if data == nil {
			break
		}
		if err := s.cryptoStreamHandler.HandleMessage(data, encLevel); err != nil {
			return err
		}
	}
	return s.handleHandshakeEvents(rcvTime)
}

func (s *connection) handleHandshakeEvents(now time.Time) error {
	for {
		ev := s.cryptoStreamHandler.NextEvent()
		var err error
		switch ev.Kind {
		case handshake.EventNoEvent:
			return nil
		case handshake.EventHandshakeComplete:
			// Don't call handleHandshakeComplete yet.
			// It's advantageous to process ACK frames that might be serialized after the CRYPTO frame first.
			s.handshakeComplete = true
		case handshake.EventReceivedTransportParameters:
			err = s.handleTransportParameters(ev.TransportParameters)
		case handshake.EventRestoredTransportParameters:
			s.restoreTransportParameters(ev.TransportParameters)
			close(s.earlyConnReadyChan)
		case handshake.EventReceivedReadKeys:
			// queue all previously undecryptable packets
			s.undecryptablePacketsToProcess = append(s.undecryptablePacketsToProcess, s.undecryptablePackets...)
			s.undecryptablePackets = nil
		case handshake.EventDiscard0RTTKeys:
			err = s.dropEncryptionLevel(protocol.Encryption0RTT, now)
		case handshake.EventWriteInitialData:
			_, err = s.initialStream.Write(ev.Data)
		case handshake.EventWriteHandshakeData:
			_, err = s.handshakeStream.Write(ev.Data)
		}
		if err != nil {
			return err
		}
	}
}

func (s *connection) handleStreamFrame(frame *wire.StreamFrame, rcvTime time.Time) error {
	str, err := s.streamsMap.GetOrOpenReceiveStream(frame.StreamID)
	if err != nil {
		return err
	}
	if str == nil { // stream was already closed and garbage collected
		return nil
	}
	return str.handleStreamFrame(frame, rcvTime)
}

func (s *connection) handleMaxDataFrame(frame *wire.MaxDataFrame) {
	s.connFlowController.UpdateSendWindow(frame.MaximumData)
}

func (s *connection) handleMaxStreamDataFrame(frame *wire.MaxStreamDataFrame) error {
	str, err := s.streamsMap.GetOrOpenSendStream(frame.StreamID)
	if err != nil {
		return err
	}
	if str == nil {
		// stream is closed and already garbage collected
		return nil
	}
	str.updateSendWindow(frame.MaximumStreamData)
	return nil
}

func (s *connection) handleStreamDataBlockedFrame(frame *wire.StreamDataBlockedFrame) error {
	// We don't need to do anything in response to a STREAM_DATA_BLOCKED frame,
	// but we need to make sure that the stream ID is valid.
	_, err := s.streamsMap.GetOrOpenReceiveStream(frame.StreamID)
	return err
}

func (s *connection) handleMaxStreamsFrame(frame *wire.MaxStreamsFrame) {
	s.streamsMap.HandleMaxStreamsFrame(frame)
}

func (s *connection) handleResetStreamFrame(frame *wire.ResetStreamFrame, rcvTime time.Time) error {
	str, err := s.streamsMap.GetOrOpenReceiveStream(frame.StreamID)
	if err != nil {
		return err
	}
	if str == nil {
		// stream is closed and already garbage collected
		return nil
	}
	return str.handleResetStreamFrame(frame, rcvTime)
}

func (s *connection) handleStopSendingFrame(frame *wire.StopSendingFrame) error {
	str, err := s.streamsMap.GetOrOpenSendStream(frame.StreamID)
	if err != nil {
		return err
	}
	if str == nil {
		// stream is closed and already garbage collected
		return nil
	}
	str.handleStopSendingFrame(frame)
	return nil
}

func (s *connection) handlePathChallengeFrame(f *wire.PathChallengeFrame) {
	if s.perspective == protocol.PerspectiveClient {
		s.queueControlFrame(&wire.PathResponseFrame{Data: f.Data})
	}
}

func (s *connection) handlePathResponseFrame(f *wire.PathResponseFrame) error {
	switch s.perspective {
	case protocol.PerspectiveClient:
		return s.handlePathResponseFrameClient(f)
	case protocol.PerspectiveServer:
		return s.handlePathResponseFrameServer(f)
	default:
		panic("unreachable")
	}
}

func (s *connection) handlePathResponseFrameClient(f *wire.PathResponseFrame) error {
	pm := s.pathManagerOutgoing.Load()
	if pm == nil {
		return &qerr.TransportError{
			ErrorCode:    qerr.ProtocolViolation,
			ErrorMessage: "unexpected PATH_RESPONSE frame",
		}
	}
	pm.HandlePathResponseFrame(f)
	return nil
}

func (s *connection) handlePathResponseFrameServer(f *wire.PathResponseFrame) error {
	if s.pathManager == nil {
		// since we didn't send PATH_CHALLENGEs yet, we don't expect PATH_RESPONSEs
		return &qerr.TransportError{
			ErrorCode:    qerr.ProtocolViolation,
			ErrorMessage: "unexpected PATH_RESPONSE frame",
		}
	}
	s.pathManager.HandlePathResponseFrame(f)
	return nil
}

func (s *connection) handleNewTokenFrame(frame *wire.NewTokenFrame) error {
	if s.perspective == protocol.PerspectiveServer {
		return &qerr.TransportError{
			ErrorCode:    qerr.ProtocolViolation,
			ErrorMessage: "received NEW_TOKEN frame from the client",
		}
	}
	if s.config.TokenStore != nil {
		s.config.TokenStore.Put(s.tokenStoreKey, &ClientToken{data: frame.Token})
	}
	return nil
}

func (s *connection) handleNewConnectionIDFrame(f *wire.NewConnectionIDFrame) error {
	return s.connIDManager.Add(f)
}

func (s *connection) handleRetireConnectionIDFrame(f *wire.RetireConnectionIDFrame, destConnID protocol.ConnectionID) error {
	return s.connIDGenerator.Retire(f.SequenceNumber, destConnID)
}

func (s *connection) handleHandshakeDoneFrame(rcvTime time.Time) error {
	if s.perspective == protocol.PerspectiveServer {
		return &qerr.TransportError{
			ErrorCode:    qerr.ProtocolViolation,
			ErrorMessage: "received a HANDSHAKE_DONE frame",
		}
	}
	if !s.handshakeConfirmed {
		return s.handleHandshakeConfirmed(rcvTime)
	}
	return nil
}

func (s *connection) handleAckFrame(frame *wire.AckFrame, encLevel protocol.EncryptionLevel, rcvTime time.Time) error {
	acked1RTTPacket, err := s.sentPacketHandler.ReceivedAck(frame, encLevel, s.lastPacketReceivedTime)
	if err != nil {
		return err
	}
	if !acked1RTTPacket {
		return nil
	}
	// On the client side: If the packet acknowledged a 1-RTT packet, this confirms the handshake.
	// This is only possible if the ACK was sent in a 1-RTT packet.
	// This is an optimization over simply waiting for a HANDSHAKE_DONE frame, see section 4.1.2 of RFC 9001.
	if s.perspective == protocol.PerspectiveClient && !s.handshakeConfirmed {
		if err := s.handleHandshakeConfirmed(rcvTime); err != nil {
			return err
		}
	}
	// If one of the acknowledged packets was a Path MTU probe packet, this might have increased the Path MTU estimate.
	if s.mtuDiscoverer != nil {
		if mtu := s.mtuDiscoverer.CurrentSize(); mtu > protocol.ByteCount(s.currentMTUEstimate.Load()) {
			s.currentMTUEstimate.Store(uint32(mtu))
			s.sentPacketHandler.SetMaxDatagramSize(mtu)
		}
	}
	return s.cryptoStreamHandler.SetLargest1RTTAcked(frame.LargestAcked())
}

func (s *connection) handleDatagramFrame(f *wire.DatagramFrame) error {
	if f.Length(s.version) > wire.MaxDatagramSize {
		return &qerr.TransportError{
			ErrorCode:    qerr.ProtocolViolation,
			ErrorMessage: "DATAGRAM frame too large",
		}
	}
	s.datagramQueue.HandleDatagramFrame(f)
	return nil
}

func (s *connection) setCloseError(e *closeError) {
	s.closeErr.CompareAndSwap(nil, e)
	select {
	case s.closeChan <- struct{}{}:
	default:
	}
}

// closeLocal closes the connection and send a CONNECTION_CLOSE containing the error
func (s *connection) closeLocal(e error) {
	s.setCloseError(&closeError{err: e, immediate: false})
}

// destroy closes the connection without sending the error on the wire
func (s *connection) destroy(e error) {
	s.destroyImpl(e)
	<-s.ctx.Done()
}

func (s *connection) destroyImpl(e error) {
	s.setCloseError(&closeError{err: e, immediate: true})
}

func (s *connection) CloseWithError(code ApplicationErrorCode, desc string) error {
	s.closeLocal(&qerr.ApplicationError{
		ErrorCode:    code,
		ErrorMessage: desc,
	})
	<-s.ctx.Done()
	return nil
}

func (s *connection) closeWithTransportError(code TransportErrorCode) {
	s.closeLocal(&qerr.TransportError{ErrorCode: code})
	<-s.ctx.Done()
}

func (s *connection) handleCloseError(closeErr *closeError) {
	if closeErr.immediate {
		if nerr, ok := closeErr.err.(net.Error); ok && nerr.Timeout() {
			s.logger.Errorf("Destroying connection: %s", closeErr.err)
		} else {
			s.logger.Errorf("Destroying connection with error: %s", closeErr.err)
		}
	} else {
		if closeErr.err == nil {
			s.logger.Infof("Closing connection.")
		} else {
			s.logger.Errorf("Closing connection with error: %s", closeErr.err)
		}
	}

	e := closeErr.err
	if e == nil {
		e = &qerr.ApplicationError{}
	} else {
		defer func() { closeErr.err = e }()
	}

	var (
		statelessResetErr     *StatelessResetError
		versionNegotiationErr *VersionNegotiationError
		recreateErr           *errCloseForRecreating
		applicationErr        *ApplicationError
		transportErr          *TransportError
	)
	var isRemoteClose bool
	switch {
	case errors.Is(e, qerr.ErrIdleTimeout),
		errors.Is(e, qerr.ErrHandshakeTimeout),
		errors.As(e, &statelessResetErr),
		errors.As(e, &versionNegotiationErr),
		errors.As(e, &recreateErr):
	case errors.As(e, &applicationErr):
		isRemoteClose = applicationErr.Remote
	case errors.As(e, &transportErr):
		isRemoteClose = transportErr.Remote
	case closeErr.immediate:
		e = closeErr.err
	default:
		e = &qerr.TransportError{
			ErrorCode:    qerr.InternalError,
			ErrorMessage: e.Error(),
		}
	}

	s.streamsMap.CloseWithError(e)
	if s.datagramQueue != nil {
		s.datagramQueue.CloseWithError(e)
	}

	// In rare instances, the connection ID manager might switch to a new connection ID
	// when sending the CONNECTION_CLOSE frame.
	// The connection ID manager removes the active stateless reset token from the packet
	// handler map when it is closed, so we need to make sure that this happens last.
	defer s.connIDManager.Close()

	if s.tracer != nil && s.tracer.ClosedConnection != nil && !errors.As(e, &recreateErr) {
		s.tracer.ClosedConnection(e)
	}

	// If this is a remote close we're done here
	if isRemoteClose {
		s.connIDGenerator.ReplaceWithClosed(nil)
		return
	}
	if closeErr.immediate {
		s.connIDGenerator.RemoveAll()
		return
	}
	// Don't send out any CONNECTION_CLOSE if this is an error that occurred
	// before we even sent out the first packet.
	if s.perspective == protocol.PerspectiveClient && !s.sentFirstPacket {
		s.connIDGenerator.RemoveAll()
		return
	}
	connClosePacket, err := s.sendConnectionClose(e)
	if err != nil {
		s.logger.Debugf("Error sending CONNECTION_CLOSE: %s", err)
	}
	s.connIDGenerator.ReplaceWithClosed(connClosePacket)
}

func (s *connection) dropEncryptionLevel(encLevel protocol.EncryptionLevel, now time.Time) error {
	if s.tracer != nil && s.tracer.DroppedEncryptionLevel != nil {
		s.tracer.DroppedEncryptionLevel(encLevel)
	}
	s.sentPacketHandler.DropPackets(encLevel, now)
	s.receivedPacketHandler.DropPackets(encLevel)
	//nolint:exhaustive // only Initial and 0-RTT need special treatment
	switch encLevel {
	case protocol.EncryptionInitial:
		s.droppedInitialKeys = true
		s.cryptoStreamHandler.DiscardInitialKeys()
	case protocol.Encryption0RTT:
		s.streamsMap.ResetFor0RTT()
		s.framer.Handle0RTTRejection()
		return s.connFlowController.Reset()
	}
	return s.cryptoStreamManager.Drop(encLevel)
}

// is called for the client, when restoring transport parameters saved for 0-RTT
func (s *connection) restoreTransportParameters(params *wire.TransportParameters) {
	if s.logger.Debug() {
		s.logger.Debugf("Restoring Transport Parameters: %s", params)
	}

	s.peerParams = params
	s.connIDGenerator.SetMaxActiveConnIDs(params.ActiveConnectionIDLimit)
	s.connFlowController.UpdateSendWindow(params.InitialMaxData)
	s.streamsMap.UpdateLimits(params)
	s.connStateMutex.Lock()
	s.connState.SupportsDatagrams = s.supportsDatagrams()
	s.connStateMutex.Unlock()
}

func (s *connection) handleTransportParameters(params *wire.TransportParameters) error {
	if s.tracer != nil && s.tracer.ReceivedTransportParameters != nil {
		s.tracer.ReceivedTransportParameters(params)
	}
	if err := s.checkTransportParameters(params); err != nil {
		return &qerr.TransportError{
			ErrorCode:    qerr.TransportParameterError,
			ErrorMessage: err.Error(),
		}
	}

	if s.perspective == protocol.PerspectiveClient && s.peerParams != nil && s.ConnectionState().Used0RTT && !params.ValidForUpdate(s.peerParams) {
		return &qerr.TransportError{
			ErrorCode:    qerr.ProtocolViolation,
			ErrorMessage: "server sent reduced limits after accepting 0-RTT data",
		}
	}

	s.peerParams = params
	// On the client side we have to wait for handshake completion.
	// During a 0-RTT connection, we are only allowed to use the new transport parameters for 1-RTT packets.
	if s.perspective == protocol.PerspectiveServer {
		s.applyTransportParameters()
		// On the server side, the early connection is ready as soon as we processed
		// the client's transport parameters.
		close(s.earlyConnReadyChan)
	}

	s.connStateMutex.Lock()
	s.connState.SupportsDatagrams = s.supportsDatagrams()
	s.connStateMutex.Unlock()
	return nil
}

func (s *connection) checkTransportParameters(params *wire.TransportParameters) error {
	if s.logger.Debug() {
		s.logger.Debugf("Processed Transport Parameters: %s", params)
	}

	// check the initial_source_connection_id
	if params.InitialSourceConnectionID != s.handshakeDestConnID {
		return fmt.Errorf("expected initial_source_connection_id to equal %s, is %s", s.handshakeDestConnID, params.InitialSourceConnectionID)
	}

	if s.perspective == protocol.PerspectiveServer {
		return nil
	}
	// check the original_destination_connection_id
	if params.OriginalDestinationConnectionID != s.origDestConnID {
		return fmt.Errorf("expected original_destination_connection_id to equal %s, is %s", s.origDestConnID, params.OriginalDestinationConnectionID)
	}
	if s.retrySrcConnID != nil { // a Retry was performed
		if params.RetrySourceConnectionID == nil {
			return errors.New("missing retry_source_connection_id")
		}
		if *params.RetrySourceConnectionID != *s.retrySrcConnID {
			return fmt.Errorf("expected retry_source_connection_id to equal %s, is %s", s.retrySrcConnID, *params.RetrySourceConnectionID)
		}
	} else if params.RetrySourceConnectionID != nil {
		return errors.New("received retry_source_connection_id, although no Retry was performed")
	}
	return nil
}

func (s *connection) applyTransportParameters() {
	params := s.peerParams
	// Our local idle timeout will always be > 0.
	s.idleTimeout = s.config.MaxIdleTimeout
	// If the peer advertised an idle timeout, take the minimum of the values.
	if params.MaxIdleTimeout > 0 {
		s.idleTimeout = min(s.idleTimeout, params.MaxIdleTimeout)
	}
	s.keepAliveInterval = min(s.config.KeepAlivePeriod, s.idleTimeout/2)
	s.streamsMap.UpdateLimits(params)
	s.frameParser.SetAckDelayExponent(params.AckDelayExponent)
	s.connFlowController.UpdateSendWindow(params.InitialMaxData)
	s.rttStats.SetMaxAckDelay(params.MaxAckDelay)
	s.connIDGenerator.SetMaxActiveConnIDs(params.ActiveConnectionIDLimit)
	if params.StatelessResetToken != nil {
		s.connIDManager.SetStatelessResetToken(*params.StatelessResetToken)
	}
	// We don't support connection migration yet, so we don't have any use for the preferred_address.
	if params.PreferredAddress != nil {
		// Retire the connection ID.
		s.connIDManager.AddFromPreferredAddress(params.PreferredAddress.ConnectionID, params.PreferredAddress.StatelessResetToken)
	}
	maxPacketSize := protocol.ByteCount(protocol.MaxPacketBufferSize)
	if params.MaxUDPPayloadSize > 0 && params.MaxUDPPayloadSize < maxPacketSize {
		maxPacketSize = params.MaxUDPPayloadSize
	}
	s.mtuDiscoverer = newMTUDiscoverer(
		s.rttStats,
		protocol.ByteCount(s.config.InitialPacketSize),
		maxPacketSize,
		s.tracer,
	)
}

func (s *connection) triggerSending(now time.Time) error {
	s.pacingDeadline = time.Time{}

	sendMode := s.sentPacketHandler.SendMode(now)
	//nolint:exhaustive // No need to handle pacing limited here.
	switch sendMode {
	case ackhandler.SendAny:
		return s.sendPackets(now)
	case ackhandler.SendNone:
		return nil
	case ackhandler.SendPacingLimited:
		deadline := s.sentPacketHandler.TimeUntilSend()
		if deadline.IsZero() {
			deadline = deadlineSendImmediately
		}
		s.pacingDeadline = deadline
		// Allow sending of an ACK if we're pacing limit.
		// This makes sure that a peer that is mostly receiving data (and thus has an inaccurate cwnd estimate)
		// sends enough ACKs to allow its peer to utilize the bandwidth.
		fallthrough
	case ackhandler.SendAck:
		// We can at most send a single ACK only packet.
		// There will only be a new ACK after receiving new packets.
		// SendAck is only returned when we're congestion limited, so we don't need to set the pacing timer.
		return s.maybeSendAckOnlyPacket(now)
	case ackhandler.SendPTOInitial, ackhandler.SendPTOHandshake, ackhandler.SendPTOAppData:
		if err := s.sendProbePacket(sendMode, now); err != nil {
			return err
		}
		if s.sendQueue.WouldBlock() {
			s.scheduleSending()
			return nil
		}
		return s.triggerSending(now)
	default:
		return fmt.Errorf("BUG: invalid send mode %d", sendMode)
	}
}

func (s *connection) sendPackets(now time.Time) error {
	if s.perspective == protocol.PerspectiveClient && s.handshakeConfirmed {
		if pm := s.pathManagerOutgoing.Load(); pm != nil {
			connID, frame, tr, ok := pm.NextPathToProbe()
			if ok {
				probe, buf, err := s.packer.PackPathProbePacket(connID, []ackhandler.Frame{frame}, s.version)
				if err != nil {
					return err
				}
				s.logger.Debugf("sending path probe packet from %s", s.LocalAddr())
				s.logShortHeaderPacket(probe.DestConnID, probe.Ack, probe.Frames, probe.StreamFrames, probe.PacketNumber, probe.PacketNumberLen, probe.KeyPhase, protocol.ECNNon, buf.Len(), false)
				s.registerPackedShortHeaderPacket(probe, protocol.ECNNon, now)
				tr.WriteTo(buf.Data, s.conn.RemoteAddr())
				// There's (likely) more data to send. Loop around again.
				s.scheduleSending()
				return nil
			}
		}
	}

	// Path MTU Discovery
	// Can't use GSO, since we need to send a single packet that's larger than our current maximum size.
	// Performance-wise, this doesn't matter, since we only send a very small (<10) number of
	// MTU probe packets per connection.
	if s.handshakeConfirmed && s.mtuDiscoverer != nil && s.mtuDiscoverer.ShouldSendProbe(now) {
		ping, size := s.mtuDiscoverer.GetPing(now)
		p, buf, err := s.packer.PackMTUProbePacket(ping, size, s.version)
		if err != nil {
			return err
		}
		ecn := s.sentPacketHandler.ECNMode(true)
		s.logShortHeaderPacket(p.DestConnID, p.Ack, p.Frames, p.StreamFrames, p.PacketNumber, p.PacketNumberLen, p.KeyPhase, ecn, buf.Len(), false)
		s.registerPackedShortHeaderPacket(p, ecn, now)
		s.sendQueue.Send(buf, 0, ecn)
		// There's (likely) more data to send. Loop around again.
		s.scheduleSending()
		return nil
	}

	if offset := s.connFlowController.GetWindowUpdate(now); offset > 0 {
		s.framer.QueueControlFrame(&wire.MaxDataFrame{MaximumData: offset})
	}
	if cf := s.cryptoStreamManager.GetPostHandshakeData(protocol.MaxPostHandshakeCryptoFrameSize); cf != nil {
		s.queueControlFrame(cf)
	}

	if !s.handshakeConfirmed {
		packet, err := s.packer.PackCoalescedPacket(false, s.maxPacketSize(), now, s.version)
		if err != nil || packet == nil {
			return err
		}
		s.sentFirstPacket = true
		if err := s.sendPackedCoalescedPacket(packet, s.sentPacketHandler.ECNMode(packet.IsOnlyShortHeaderPacket()), now); err != nil {
			return err
		}
		//nolint:exhaustive // only need to handle pacing-related events here
		switch s.sentPacketHandler.SendMode(now) {
		case ackhandler.SendPacingLimited:
			s.resetPacingDeadline()
		case ackhandler.SendAny:
			s.pacingDeadline = deadlineSendImmediately
		}
		return nil
	}

	if s.conn.capabilities().GSO {
		return s.sendPacketsWithGSO(now)
	}
	return s.sendPacketsWithoutGSO(now)
}

func (s *connection) sendPacketsWithoutGSO(now time.Time) error {
	for {
		buf := getPacketBuffer()
		ecn := s.sentPacketHandler.ECNMode(true)
		if _, err := s.appendOneShortHeaderPacket(buf, s.maxPacketSize(), ecn, now); err != nil {
			if err == errNothingToPack {
				buf.Release()
				return nil
			}
			return err
		}

		s.sendQueue.Send(buf, 0, ecn)

		if s.sendQueue.WouldBlock() {
			return nil
		}
		sendMode := s.sentPacketHandler.SendMode(now)
		if sendMode == ackhandler.SendPacingLimited {
			s.resetPacingDeadline()
			return nil
		}
		if sendMode != ackhandler.SendAny {
			return nil
		}
		// Prioritize receiving of packets over sending out more packets.
		s.receivedPacketMx.Lock()
		hasPackets := !s.receivedPackets.Empty()
		s.receivedPacketMx.Unlock()
		if hasPackets {
			s.pacingDeadline = deadlineSendImmediately
			return nil
		}
	}
}

func (s *connection) sendPacketsWithGSO(now time.Time) error {
	buf := getLargePacketBuffer()
	maxSize := s.maxPacketSize()

	ecn := s.sentPacketHandler.ECNMode(true)
	for {
		var dontSendMore bool
		size, err := s.appendOneShortHeaderPacket(buf, maxSize, ecn, now)
		if err != nil {
			if err != errNothingToPack {
				return err
			}
			if buf.Len() == 0 {
				buf.Release()
				return nil
			}
			dontSendMore = true
		}

		if !dontSendMore {
			sendMode := s.sentPacketHandler.SendMode(now)
			if sendMode == ackhandler.SendPacingLimited {
				s.resetPacingDeadline()
			}
			if sendMode != ackhandler.SendAny {
				dontSendMore = true
			}
		}

		// Don't send more packets in this batch if they require a different ECN marking than the previous ones.
		nextECN := s.sentPacketHandler.ECNMode(true)

		// Append another packet if
		// 1. The congestion controller and pacer allow sending more
		// 2. The last packet appended was a full-size packet
		// 3. The next packet will have the same ECN marking
		// 4. We still have enough space for another full-size packet in the buffer
		if !dontSendMore && size == maxSize && nextECN == ecn && buf.Len()+maxSize <= buf.Cap() {
			continue
		}

		s.sendQueue.Send(buf, uint16(maxSize), ecn)

		if dontSendMore {
			return nil
		}
		if s.sendQueue.WouldBlock() {
			return nil
		}

		// Prioritize receiving of packets over sending out more packets.
		s.receivedPacketMx.Lock()
		hasPackets := !s.receivedPackets.Empty()
		s.receivedPacketMx.Unlock()
		if hasPackets {
			s.pacingDeadline = deadlineSendImmediately
			return nil
		}

		ecn = nextECN
		buf = getLargePacketBuffer()
	}
}

func (s *connection) resetPacingDeadline() {
	deadline := s.sentPacketHandler.TimeUntilSend()
	if deadline.IsZero() {
		deadline = deadlineSendImmediately
	}
	s.pacingDeadline = deadline
}

func (s *connection) maybeSendAckOnlyPacket(now time.Time) error {
	if !s.handshakeConfirmed {
		ecn := s.sentPacketHandler.ECNMode(false)
		packet, err := s.packer.PackCoalescedPacket(true, s.maxPacketSize(), now, s.version)
		if err != nil {
			return err
		}
		if packet == nil {
			return nil
		}
		return s.sendPackedCoalescedPacket(packet, ecn, now)
	}

	ecn := s.sentPacketHandler.ECNMode(true)
	p, buf, err := s.packer.PackAckOnlyPacket(s.maxPacketSize(), now, s.version)
	if err != nil {
		if err == errNothingToPack {
			return nil
		}
		return err
	}
	s.logShortHeaderPacket(p.DestConnID, p.Ack, p.Frames, p.StreamFrames, p.PacketNumber, p.PacketNumberLen, p.KeyPhase, ecn, buf.Len(), false)
	s.registerPackedShortHeaderPacket(p, ecn, now)
	s.sendQueue.Send(buf, 0, ecn)
	return nil
}

func (s *connection) sendProbePacket(sendMode ackhandler.SendMode, now time.Time) error {
	var encLevel protocol.EncryptionLevel
	//nolint:exhaustive // We only need to handle the PTO send modes here.
	switch sendMode {
	case ackhandler.SendPTOInitial:
		encLevel = protocol.EncryptionInitial
	case ackhandler.SendPTOHandshake:
		encLevel = protocol.EncryptionHandshake
	case ackhandler.SendPTOAppData:
		encLevel = protocol.Encryption1RTT
	default:
		return fmt.Errorf("connection BUG: unexpected send mode: %d", sendMode)
	}
	// Queue probe packets until we actually send out a packet,
	// or until there are no more packets to queue.
	var packet *coalescedPacket
	for packet == nil {
		if wasQueued := s.sentPacketHandler.QueueProbePacket(encLevel); !wasQueued {
			break
		}
		var err error
		packet, err = s.packer.PackPTOProbePacket(encLevel, s.maxPacketSize(), false, now, s.version)
		if err != nil {
			return err
		}
	}
	if packet == nil {
		var err error
		packet, err = s.packer.PackPTOProbePacket(encLevel, s.maxPacketSize(), true, now, s.version)
		if err != nil {
			return err
		}
	}
	if packet == nil || (len(packet.longHdrPackets) == 0 && packet.shortHdrPacket == nil) {
		return fmt.Errorf("connection BUG: couldn't pack %s probe packet: %v", encLevel, packet)
	}
	return s.sendPackedCoalescedPacket(packet, s.sentPacketHandler.ECNMode(packet.IsOnlyShortHeaderPacket()), now)
}

// appendOneShortHeaderPacket appends a new packet to the given packetBuffer.
// If there was nothing to pack, the returned size is 0.
func (s *connection) appendOneShortHeaderPacket(buf *packetBuffer, maxSize protocol.ByteCount, ecn protocol.ECN, now time.Time) (protocol.ByteCount, error) {
	startLen := buf.Len()
	p, err := s.packer.AppendPacket(buf, maxSize, now, s.version)
	if err != nil {
		return 0, err
	}
	size := buf.Len() - startLen
	s.logShortHeaderPacket(p.DestConnID, p.Ack, p.Frames, p.StreamFrames, p.PacketNumber, p.PacketNumberLen, p.KeyPhase, ecn, size, false)
	s.registerPackedShortHeaderPacket(p, ecn, now)
	return size, nil
}

func (s *connection) registerPackedShortHeaderPacket(p shortHeaderPacket, ecn protocol.ECN, now time.Time) {
	if p.IsPathProbePacket {
		s.sentPacketHandler.SentPacket(
			now,
			p.PacketNumber,
			protocol.InvalidPacketNumber,
			p.StreamFrames,
			p.Frames,
			protocol.Encryption1RTT,
			ecn,
			p.Length,
			p.IsPathMTUProbePacket,
			true,
		)
		return
	}
	if s.firstAckElicitingPacketAfterIdleSentTime.IsZero() && (len(p.StreamFrames) > 0 || ackhandler.HasAckElicitingFrames(p.Frames)) {
		s.firstAckElicitingPacketAfterIdleSentTime = now
	}

	largestAcked := protocol.InvalidPacketNumber
	if p.Ack != nil {
		largestAcked = p.Ack.LargestAcked()
	}
	s.sentPacketHandler.SentPacket(
		now,
		p.PacketNumber,
		largestAcked,
		p.StreamFrames,
		p.Frames,
		protocol.Encryption1RTT,
		ecn,
		p.Length,
		p.IsPathMTUProbePacket,
		false,
	)
	s.connIDManager.SentPacket()
}

func (s *connection) sendPackedCoalescedPacket(packet *coalescedPacket, ecn protocol.ECN, now time.Time) error {
	s.logCoalescedPacket(packet, ecn)
	for _, p := range packet.longHdrPackets {
		if s.firstAckElicitingPacketAfterIdleSentTime.IsZero() && p.IsAckEliciting() {
			s.firstAckElicitingPacketAfterIdleSentTime = now
		}
		largestAcked := protocol.InvalidPacketNumber
		if p.ack != nil {
			largestAcked = p.ack.LargestAcked()
		}
		s.sentPacketHandler.SentPacket(
			now,
			p.header.PacketNumber,
			largestAcked,
			p.streamFrames,
			p.frames,
			p.EncryptionLevel(),
			ecn,
			p.length,
			false,
			false,
		)
		if s.perspective == protocol.PerspectiveClient && p.EncryptionLevel() == protocol.EncryptionHandshake &&
			!s.droppedInitialKeys {
			// On the client side, Initial keys are dropped as soon as the first Handshake packet is sent.
			// See Section 4.9.1 of RFC 9001.
			if err := s.dropEncryptionLevel(protocol.EncryptionInitial, now); err != nil {
				return err
			}
		}
	}
	if p := packet.shortHdrPacket; p != nil {
		if s.firstAckElicitingPacketAfterIdleSentTime.IsZero() && p.IsAckEliciting() {
			s.firstAckElicitingPacketAfterIdleSentTime = now
		}
		largestAcked := protocol.InvalidPacketNumber
		if p.Ack != nil {
			largestAcked = p.Ack.LargestAcked()
		}
		s.sentPacketHandler.SentPacket(
			now,
			p.PacketNumber,
			largestAcked,
			p.StreamFrames,
			p.Frames,
			protocol.Encryption1RTT,
			ecn,
			p.Length,
			p.IsPathMTUProbePacket,
			false,
		)
	}
	s.connIDManager.SentPacket()
	s.sendQueue.Send(packet.buffer, 0, ecn)
	return nil
}

func (s *connection) sendConnectionClose(e error) ([]byte, error) {
	var packet *coalescedPacket
	var err error
	var transportErr *qerr.TransportError
	var applicationErr *qerr.ApplicationError
	if errors.As(e, &transportErr) {
		packet, err = s.packer.PackConnectionClose(transportErr, s.maxPacketSize(), s.version)
	} else if errors.As(e, &applicationErr) {
		packet, err = s.packer.PackApplicationClose(applicationErr, s.maxPacketSize(), s.version)
	} else {
		packet, err = s.packer.PackConnectionClose(&qerr.TransportError{
			ErrorCode:    qerr.InternalError,
			ErrorMessage: fmt.Sprintf("connection BUG: unspecified error type (msg: %s)", e.Error()),
		}, s.maxPacketSize(), s.version)
	}
	if err != nil {
		return nil, err
	}
	ecn := s.sentPacketHandler.ECNMode(packet.IsOnlyShortHeaderPacket())
	s.logCoalescedPacket(packet, ecn)
	return packet.buffer.Data, s.conn.Write(packet.buffer.Data, 0, ecn)
}

func (s *connection) maxPacketSize() protocol.ByteCount {
	if s.mtuDiscoverer == nil {
		// Use the configured packet size on the client side.
		// If the server sends a max_udp_payload_size that's smaller than this size, we can ignore this:
		// Apparently the server still processed the (fully padded) Initial packet anyway.
		if s.perspective == protocol.PerspectiveClient {
			return protocol.ByteCount(s.config.InitialPacketSize)
		}
		// On the server side, there's no downside to using 1200 bytes until we received the client's transport
		// parameters:
		// * If the first packet didn't contain the entire ClientHello, all we can do is ACK that packet. We don't
		//   need a lot of bytes for that.
		// * If it did, we will have processed the transport parameters and initialized the MTU discoverer.
		return protocol.MinInitialPacketSize
	}
	return s.mtuDiscoverer.CurrentSize()
}

// AcceptStream returns the next stream openend by the peer
func (s *connection) AcceptStream(ctx context.Context) (Stream, error) {
	return s.streamsMap.AcceptStream(ctx)
}

func (s *connection) AcceptUniStream(ctx context.Context) (ReceiveStream, error) {
	return s.streamsMap.AcceptUniStream(ctx)
}

// OpenStream opens a stream
func (s *connection) OpenStream() (Stream, error) {
	return s.streamsMap.OpenStream()
}

func (s *connection) OpenStreamSync(ctx context.Context) (Stream, error) {
	return s.streamsMap.OpenStreamSync(ctx)
}

func (s *connection) OpenUniStream() (SendStream, error) {
	return s.streamsMap.OpenUniStream()
}

func (s *connection) OpenUniStreamSync(ctx context.Context) (SendStream, error) {
	return s.streamsMap.OpenUniStreamSync(ctx)
}

func (s *connection) newFlowController(id protocol.StreamID) flowcontrol.StreamFlowController {
	initialSendWindow := s.peerParams.InitialMaxStreamDataUni
	if id.Type() == protocol.StreamTypeBidi {
		if id.InitiatedBy() == s.perspective {
			initialSendWindow = s.peerParams.InitialMaxStreamDataBidiRemote
		} else {
			initialSendWindow = s.peerParams.InitialMaxStreamDataBidiLocal
		}
	}
	return flowcontrol.NewStreamFlowController(
		id,
		s.connFlowController,
		protocol.ByteCount(s.config.InitialStreamReceiveWindow),
		protocol.ByteCount(s.config.MaxStreamReceiveWindow),
		initialSendWindow,
		s.rttStats,
		s.logger,
	)
}

// scheduleSending signals that we have data for sending
func (s *connection) scheduleSending() {
	select {
	case s.sendingScheduled <- struct{}{}:
	default:
	}
}

// tryQueueingUndecryptablePacket queues a packet for which we're missing the decryption keys.
// The logging.PacketType is only used for logging purposes.
func (s *connection) tryQueueingUndecryptablePacket(p receivedPacket, pt logging.PacketType) {
	if s.handshakeComplete {
		panic("shouldn't queue undecryptable packets after handshake completion")
	}
	if len(s.undecryptablePackets)+1 > protocol.MaxUndecryptablePackets {
		if s.tracer != nil && s.tracer.DroppedPacket != nil {
			s.tracer.DroppedPacket(pt, protocol.InvalidPacketNumber, p.Size(), logging.PacketDropDOSPrevention)
		}
		s.logger.Infof("Dropping undecryptable packet (%d bytes). Undecryptable packet queue full.", p.Size())
		return
	}
	s.logger.Infof("Queueing packet (%d bytes) for later decryption", p.Size())
	if s.tracer != nil && s.tracer.BufferedPacket != nil {
		s.tracer.BufferedPacket(pt, p.Size())
	}
	s.undecryptablePackets = append(s.undecryptablePackets, p)
}

func (s *connection) queueControlFrame(f wire.Frame) {
	s.framer.QueueControlFrame(f)
	s.scheduleSending()
}

func (s *connection) onHasConnectionData() { s.scheduleSending() }

func (s *connection) onHasStreamData(id protocol.StreamID, str sendStreamI) {
	s.framer.AddActiveStream(id, str)
	s.scheduleSending()
}

func (s *connection) onHasStreamControlFrame(id protocol.StreamID, str streamControlFrameGetter) {
	s.framer.AddStreamWithControlFrames(id, str)
	s.scheduleSending()
}

func (s *connection) onStreamCompleted(id protocol.StreamID) {
	if err := s.streamsMap.DeleteStream(id); err != nil {
		s.closeLocal(err)
	}
	s.framer.RemoveActiveStream(id)
}

func (s *connection) SendDatagram(p []byte) error {
	if !s.supportsDatagrams() {
		return errors.New("datagram support disabled")
	}

	f := &wire.DatagramFrame{DataLenPresent: true}
	// The payload size estimate is conservative.
	// Under many circumstances we could send a few more bytes.
	maxDataLen := min(
		f.MaxDataLen(s.peerParams.MaxDatagramFrameSize, s.version),
		protocol.ByteCount(s.currentMTUEstimate.Load()),
	)
	if protocol.ByteCount(len(p)) > maxDataLen {
		return &DatagramTooLargeError{MaxDatagramPayloadSize: int64(maxDataLen)}
	}
	f.Data = make([]byte, len(p))
	copy(f.Data, p)
	return s.datagramQueue.Add(f)
}

func (s *connection) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	if !s.config.EnableDatagrams {
		return nil, errors.New("datagram support disabled")
	}
	return s.datagramQueue.Receive(ctx)
}

func (s *connection) LocalAddr() net.Addr  { return s.conn.LocalAddr() }
func (s *connection) RemoteAddr() net.Addr { return s.conn.RemoteAddr() }

func (s *connection) getPathManager() *pathManagerOutgoing {
	s.pathManagerOutgoing.CompareAndSwap(nil,
		func() *pathManagerOutgoing { // this function is only called if a swap is performed
			return newPathManagerOutgoing(
				s.connIDManager.GetConnIDForPath,
				s.connIDManager.RetireConnIDForPath,
				s.scheduleSending,
			)
		}(),
	)
	return s.pathManagerOutgoing.Load()
}

func (s *connection) AddPath(t *Transport) (*Path, error) {
	if s.perspective == protocol.PerspectiveServer {
		return nil, errors.New("server cannot initiate connection migration")
	}
	if s.peerParams.DisableActiveMigration {
		return nil, errors.New("server disabled connection migration")
	}
	if err := t.init(false); err != nil {
		return nil, err
	}
	return s.getPathManager().NewPath(
		t,
		200*time.Millisecond, // initial RTT estimate
		func() {
			runner := t.connRunner()
			s.connIDGenerator.AddConnRunner(
				t.id(),
				connRunnerCallbacks{
					AddConnectionID:    func(connID protocol.ConnectionID) { runner.Add(connID, s) },
					RemoveConnectionID: runner.Remove,
					RetireConnectionID: runner.Retire,
					ReplaceWithClosed:  runner.ReplaceWithClosed,
				},
			)
		},
	), nil
}

func (s *connection) NextConnection(ctx context.Context) (Connection, error) {
	// The handshake might fail after the server rejected 0-RTT.
	// This could happen if the Finished message is malformed or never received.
	select {
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case <-s.Context().Done():
	case <-s.HandshakeComplete():
		s.streamsMap.UseResetMaps()
	}
	return s, nil
}

// estimateMaxPayloadSize estimates the maximum payload size for short header packets.
// It is not very sophisticated: it just subtracts the size of header (assuming the maximum
// connection ID length), and the size of the encryption tag.
func estimateMaxPayloadSize(mtu protocol.ByteCount) protocol.ByteCount {
	return mtu - 1 /* type byte */ - 20 /* maximum connection ID length */ - 16 /* tag size */
}
