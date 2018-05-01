package h2mux

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"golang.org/x/sync/errgroup"
)

const (
	defaultFrameSize  uint32        = 1 << 14 // Minimum frame size in http2 spec
	defaultWindowSize uint32        = 65535
	maxWindowSize     uint32        = (1 << 31) - 1 // 2^31-1 = 2147483647, max window size specified in http2 spec
	defaultTimeout    time.Duration = 5 * time.Second
	defaultRetries    uint64        = 5

	SettingMuxerMagic http2.SettingID = 0x42db
	MuxerMagicOrigin  uint32          = 0xa2e43c8b
	MuxerMagicEdge    uint32          = 0x1088ebf9
)

type MuxedStreamHandler interface {
	ServeStream(*MuxedStream) error
}

type MuxedStreamFunc func(stream *MuxedStream) error

func (f MuxedStreamFunc) ServeStream(stream *MuxedStream) error {
	return f(stream)
}

type MuxerConfig struct {
	Timeout  time.Duration
	Handler  MuxedStreamHandler
	IsClient bool
	// Name is used to identify this muxer instance when logging.
	Name string
	// The minimum time this connection can be idle before sending a heartbeat.
	HeartbeatInterval time.Duration
	// The minimum number of heartbeats to send before terminating the connection.
	MaxHeartbeats uint64
	// Logger to use
	Logger             *log.Entry
	CompressionQuality CompressionSetting
}

type Muxer struct {
	// f is used to read and write HTTP2 frames on the wire.
	f *http2.Framer
	// config is the MuxerConfig given in Handshake.
	config MuxerConfig
	// w, r are references to the underlying connection used.
	w io.WriteCloser
	r io.ReadCloser
	// muxReader is the read process.
	muxReader *MuxReader
	// muxWriter is the write process.
	muxWriter *MuxWriter
	// muxMetricsUpdater is the process to update metrics
	muxMetricsUpdater *muxMetricsUpdater
	// newStreamChan is used to create new streams on the writer thread.
	// The writer will assign the next available stream ID.
	newStreamChan chan MuxedStreamRequest
	// abortChan is used to abort the writer event loop.
	abortChan chan struct{}
	// abortOnce is used to ensure abortChan is closed once only.
	abortOnce sync.Once
	// readyList is used to signal writable streams.
	readyList *ReadyList
	// streams tracks currently-open streams.
	streams *activeStreamMap
	// explicitShutdown records whether the Muxer is closing because Shutdown was called, or due to another
	// error.
	explicitShutdown *BooleanFuse

	compressionQuality CompressionPreset
}

type Header struct {
	Name, Value string
}

// Handshake establishes a muxed connection with the peer.
// After the handshake completes, it is possible to open and accept streams.
func Handshake(
	w io.WriteCloser,
	r io.ReadCloser,
	config MuxerConfig,
) (*Muxer, error) {
	// Set default config values
	if config.Timeout == 0 {
		config.Timeout = defaultTimeout
	}
	// Initialise connection state fields
	m := &Muxer{
		f:             http2.NewFramer(w, r), // A framer that writes to w and reads from r
		config:        config,
		w:             w,
		r:             r,
		newStreamChan: make(chan MuxedStreamRequest),
		abortChan:     make(chan struct{}),
		readyList:     NewReadyList(),
		streams:       newActiveStreamMap(config.IsClient),
	}

	m.f.ReadMetaHeaders = hpack.NewDecoder(4096, func(hpack.HeaderField) {})
	// Initialise the settings to identify this connection and confirm the other end is sane.
	handshakeSetting := http2.Setting{ID: SettingMuxerMagic, Val: MuxerMagicEdge}
	compressionSetting := http2.Setting{ID: SettingCompression, Val: config.CompressionQuality.toH2Setting()}
	if CompressionIsSupported() {
		log.Debug("Compression is supported")
		m.compressionQuality = config.CompressionQuality.getPreset()
	} else {
		log.Debug("Compression is not supported")
		compressionSetting = http2.Setting{ID: SettingCompression, Val: 0}
	}

	expectedMagic := MuxerMagicOrigin
	if config.IsClient {
		handshakeSetting.Val = MuxerMagicOrigin
		expectedMagic = MuxerMagicEdge
	}
	errChan := make(chan error, 2)
	// Simultaneously send our settings and verify the peer's settings.
	go func() { errChan <- m.f.WriteSettings(handshakeSetting, compressionSetting) }()
	go func() { errChan <- m.readPeerSettings(expectedMagic) }()
	err := joinErrorsWithTimeout(errChan, 2, config.Timeout, ErrHandshakeTimeout)
	if err != nil {
		return nil, err
	}
	// Confirm sanity by ACKing the frame and expecting an ACK for our frame.
	// Not strictly necessary, but let's pretend to be H2-like.
	go func() { errChan <- m.f.WriteSettingsAck() }()
	go func() { errChan <- m.readPeerSettingsAck() }()
	err = joinErrorsWithTimeout(errChan, 2, config.Timeout, ErrHandshakeTimeout)
	if err != nil {
		return nil, err
	}

	// set up reader/writer pair ready for serve
	streamErrors := NewStreamErrorMap()
	goAwayChan := make(chan http2.ErrCode, 1)
	updateRTTChan := make(chan *roundTripMeasurement, 1)
	updateReceiveWindowChan := make(chan uint32, 1)
	updateSendWindowChan := make(chan uint32, 1)
	updateInBoundBytesChan := make(chan uint64)
	updateOutBoundBytesChan := make(chan uint64)
	inBoundCounter := NewAtomicCounter(0)
	outBoundCounter := NewAtomicCounter(0)
	pingTimestamp := NewPingTimestamp()
	connActive := NewSignal()
	idleDuration := config.HeartbeatInterval
	// Sanity check to enusre idelDuration is sane
	if idleDuration == 0 || idleDuration < defaultTimeout {
		idleDuration = defaultTimeout
		config.Logger.Warn("Minimum idle time has been adjusted to ", defaultTimeout)
	}
	maxRetries := config.MaxHeartbeats
	if maxRetries == 0 {
		maxRetries = defaultRetries
		config.Logger.Warn("Minimum number of unacked heartbeats to send before closing the connection has been adjusted to ", maxRetries)
	}

	m.explicitShutdown = NewBooleanFuse()
	m.muxReader = &MuxReader{
		f:                       m.f,
		handler:                 m.config.Handler,
		streams:                 m.streams,
		readyList:               m.readyList,
		streamErrors:            streamErrors,
		goAwayChan:              goAwayChan,
		abortChan:               m.abortChan,
		pingTimestamp:           pingTimestamp,
		connActive:              connActive,
		initialStreamWindow:     defaultWindowSize,
		streamWindowMax:         maxWindowSize,
		r:                       m.r,
		updateRTTChan:           updateRTTChan,
		updateReceiveWindowChan: updateReceiveWindowChan,
		updateSendWindowChan:    updateSendWindowChan,
		bytesRead:               inBoundCounter,
		updateInBoundBytesChan:  updateInBoundBytesChan,
	}
	m.muxWriter = &MuxWriter{
		f:                       m.f,
		streams:                 m.streams,
		streamErrors:            streamErrors,
		readyStreamChan:         m.readyList.ReadyChannel(),
		newStreamChan:           m.newStreamChan,
		goAwayChan:              goAwayChan,
		abortChan:               m.abortChan,
		pingTimestamp:           pingTimestamp,
		idleTimer:               NewIdleTimer(idleDuration, maxRetries),
		connActiveChan:          connActive.WaitChannel(),
		maxFrameSize:            defaultFrameSize,
		updateReceiveWindowChan: updateReceiveWindowChan,
		updateSendWindowChan:    updateSendWindowChan,
		bytesWrote:              outBoundCounter,
		updateOutBoundBytesChan: updateOutBoundBytesChan,
	}
	m.muxWriter.headerEncoder = hpack.NewEncoder(&m.muxWriter.headerBuffer)

	compBytesBefore, compBytesAfter := NewAtomicCounter(0), NewAtomicCounter(0)

	m.muxMetricsUpdater = newMuxMetricsUpdater(
		updateRTTChan,
		updateReceiveWindowChan,
		updateSendWindowChan,
		updateInBoundBytesChan,
		updateOutBoundBytesChan,
		m.abortChan,
		compBytesBefore,
		compBytesAfter,
	)

	if m.compressionQuality.dictSize > 0 && m.compressionQuality.nDicts > 0 {
		nd, sz := m.compressionQuality.nDicts, m.compressionQuality.dictSize
		writeDicts, dictChan := newH2WriteDictionaries(
			nd,
			sz,
			m.compressionQuality.quality,
			compBytesBefore,
			compBytesAfter,
		)
		readDicts := newH2ReadDictionaries(nd, sz)
		m.muxReader.dictionaries = h2Dictionaries{read: &readDicts, write: writeDicts}
		m.muxWriter.useDictChan = dictChan
	}

	return m, nil
}

func (m *Muxer) readPeerSettings(magic uint32) error {
	frame, err := m.f.ReadFrame()
	if err != nil {
		return err
	}
	settingsFrame, ok := frame.(*http2.SettingsFrame)
	if !ok {
		return ErrBadHandshakeNotSettings
	}
	if settingsFrame.Header().Flags != 0 {
		return ErrBadHandshakeUnexpectedAck
	}
	peerMagic, ok := settingsFrame.Value(SettingMuxerMagic)
	if !ok {
		return ErrBadHandshakeNoMagic
	}
	if magic != peerMagic {
		return ErrBadHandshakeWrongMagic
	}
	peerCompression, ok := settingsFrame.Value(SettingCompression)
	if !ok {
		m.compressionQuality = compressionPresets[CompressionNone]
		return nil
	}
	ver, fmt, sz, nd := parseCompressionSettingVal(peerCompression)
	if ver != compressionVersion || fmt != compressionFormat || sz == 0 || nd == 0 {
		m.compressionQuality = compressionPresets[CompressionNone]
		return nil
	}
	// Values used for compression are the mimimum between the two peers
	if sz < m.compressionQuality.dictSize {
		m.compressionQuality.dictSize = sz
	}
	if nd < m.compressionQuality.nDicts {
		m.compressionQuality.nDicts = nd
	}
	return nil
}

func (m *Muxer) readPeerSettingsAck() error {
	frame, err := m.f.ReadFrame()
	if err != nil {
		return err
	}
	settingsFrame, ok := frame.(*http2.SettingsFrame)
	if !ok {
		return ErrBadHandshakeNotSettingsAck
	}
	if settingsFrame.Header().Flags != http2.FlagSettingsAck {
		return ErrBadHandshakeUnexpectedSettings
	}
	return nil
}

func joinErrorsWithTimeout(errChan <-chan error, receiveCount int, timeout time.Duration, timeoutError error) error {
	for i := 0; i < receiveCount; i++ {
		select {
		case err := <-errChan:
			if err != nil {
				return err
			}
		case <-time.After(timeout):
			return timeoutError
		}
	}
	return nil
}

func (m *Muxer) Serve(ctx context.Context) error {
	errGroup, _ := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		err := m.muxReader.run(m.config.Logger)
		m.explicitShutdown.Fuse(false)
		m.r.Close()
		m.abort()
		return err
	})

	errGroup.Go(func() error {
		err := m.muxWriter.run(m.config.Logger)
		m.explicitShutdown.Fuse(false)
		m.w.Close()
		m.abort()
		return err
	})

	errGroup.Go(func() error {
		err := m.muxMetricsUpdater.run(m.config.Logger)
		return err
	})

	err := errGroup.Wait()
	if isUnexpectedTunnelError(err, m.explicitShutdown.Value()) {
		return err
	}
	return nil
}

func (m *Muxer) Shutdown() {
	m.explicitShutdown.Fuse(true)
	m.muxReader.Shutdown()
}

// IsUnexpectedTunnelError identifies errors that are expected when shutting down the h2mux tunnel.
// The set of expected errors change depending on whether we initiated shutdown or not.
func isUnexpectedTunnelError(err error, expectedShutdown bool) bool {
	if err == nil {
		return false
	}
	if !expectedShutdown {
		return true
	}
	return !isConnectionClosedError(err)
}

func isConnectionClosedError(err error) bool {
	if err == io.EOF {
		return true
	}
	if err == io.ErrClosedPipe {
		return true
	}
	if err.Error() == "tls: use of closed connection" {
		return true
	}
	if strings.HasSuffix(err.Error(), "use of closed network connection") {
		return true
	}
	return false
}

// OpenStream opens a new data stream with the given headers.
// Called by proxy server and tunnel
func (m *Muxer) OpenStream(headers []Header, body io.Reader) (*MuxedStream, error) {
	stream := &MuxedStream{
		responseHeadersReceived: make(chan struct{}),
		readBuffer:              NewSharedBuffer(),
		writeBuffer:             &bytes.Buffer{},
		receiveWindow:           defaultWindowSize,
		receiveWindowCurrentMax: defaultWindowSize, // Initial window size limit. exponentially increase it when receiveWindow is exhausted
		receiveWindowMax:        maxWindowSize,
		sendWindow:              defaultWindowSize,
		readyList:               m.readyList,
		writeHeaders:            headers,
		dictionaries:            m.muxReader.dictionaries,
	}

	select {
	// Will be received by mux writer
	case m.newStreamChan <- MuxedStreamRequest{stream: stream, body: body}:
	case <-m.abortChan:
		return nil, ErrConnectionClosed
	}
	select {
	case <-stream.responseHeadersReceived:
		return stream, nil
	case <-m.abortChan:
		return nil, ErrConnectionClosed
	}
}

func (m *Muxer) Metrics() *MuxerMetrics {
	return m.muxMetricsUpdater.Metrics()
}

func (m *Muxer) abort() {
	m.abortOnce.Do(func() {
		close(m.abortChan)
		m.streams.Abort()
	})
}

// Return how many retries/ticks since the connection was last marked active
func (m *Muxer) TimerRetries() uint64 {
	return m.muxWriter.idleTimer.RetryCount()
}
