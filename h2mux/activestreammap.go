package h2mux

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/http2"
)

var (
	ActiveStreams = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cloudflared",
		Subsystem: "tunnel",
		Name:      "active_streams",
		Help:      "Number of active streams created by all muxers.",
	})
)

func init() {
	prometheus.MustRegister(ActiveStreams)
}

// activeStreamMap is used to moderate access to active streams between the read and write
// threads, and deny access to new peer streams while shutting down.
type activeStreamMap struct {
	sync.RWMutex
	// streams tracks open streams.
	streams map[uint32]*MuxedStream
	// nextStreamID is the next ID to use on our side of the connection.
	// This is odd for clients, even for servers.
	nextStreamID uint32
	// maxPeerStreamID is the ID of the most recent stream opened by the peer.
	maxPeerStreamID uint32
	// activeStreams is a gauge shared by all muxers of this process to expose the total number of active streams
	activeStreams prometheus.Gauge

	// ignoreNewStreams is true when the connection is being shut down. New streams
	// cannot be registered.
	ignoreNewStreams bool
	// streamsEmpty is a chan that will be closed when no more streams are open.
	streamsEmptyChan chan struct{}
	closeOnce        sync.Once
}

func newActiveStreamMap(useClientStreamNumbers bool, activeStreams prometheus.Gauge) *activeStreamMap {
	m := &activeStreamMap{
		streams:          make(map[uint32]*MuxedStream),
		streamsEmptyChan: make(chan struct{}),
		nextStreamID:     1,
		activeStreams:    activeStreams,
	}
	// Client initiated stream uses odd stream ID, server initiated stream uses even stream ID
	if !useClientStreamNumbers {
		m.nextStreamID = 2
	}
	return m
}

// This function should be called while `m` is locked.
func (m *activeStreamMap) notifyStreamsEmpty() {
	m.closeOnce.Do(func() {
		close(m.streamsEmptyChan)
	})
}

// Len returns the number of active streams.
func (m *activeStreamMap) Len() int {
	m.RLock()
	defer m.RUnlock()
	return len(m.streams)
}

func (m *activeStreamMap) Get(streamID uint32) (*MuxedStream, bool) {
	m.RLock()
	defer m.RUnlock()
	stream, ok := m.streams[streamID]
	return stream, ok
}

// Set returns true if the stream was assigned successfully. If a stream
// already existed with that ID or we are shutting down, return false.
func (m *activeStreamMap) Set(newStream *MuxedStream) bool {
	m.Lock()
	defer m.Unlock()
	if _, ok := m.streams[newStream.streamID]; ok {
		return false
	}
	if m.ignoreNewStreams {
		return false
	}
	m.streams[newStream.streamID] = newStream
	m.activeStreams.Inc()
	return true
}

// Delete stops tracking the stream. It should be called only after it is closed and reset.
func (m *activeStreamMap) Delete(streamID uint32) {
	m.Lock()
	defer m.Unlock()
	if _, ok := m.streams[streamID]; ok {
		delete(m.streams, streamID)
		m.activeStreams.Dec()
	}

	// shutting down, and now the map is empty
	if m.ignoreNewStreams && len(m.streams) == 0 {
		m.notifyStreamsEmpty()
	}
}

// Shutdown blocks new streams from being created.
// It returns `done`, a channel that is closed once the last stream has closed
// and `progress`, whether a shutdown was already in progress
func (m *activeStreamMap) Shutdown() (done <-chan struct{}, alreadyInProgress bool) {
	m.Lock()
	defer m.Unlock()
	if m.ignoreNewStreams {
		// already shutting down
		return m.streamsEmptyChan, true
	}
	m.ignoreNewStreams = true
	if len(m.streams) == 0 {
		// there are no streams to wait for
		m.notifyStreamsEmpty()
	}
	return m.streamsEmptyChan, false
}

// AcquireLocalID acquires a new stream ID for a stream you're opening.
func (m *activeStreamMap) AcquireLocalID() uint32 {
	m.Lock()
	defer m.Unlock()
	x := m.nextStreamID
	m.nextStreamID += 2
	return x
}

// ObservePeerID observes the ID of a stream opened by the peer. It returns true if we should accept
// the new stream, or false to reject it. The ErrCode gives the reason why.
func (m *activeStreamMap) AcquirePeerID(streamID uint32) (bool, http2.ErrCode) {
	m.Lock()
	defer m.Unlock()
	switch {
	case m.ignoreNewStreams:
		return false, http2.ErrCodeStreamClosed
	case streamID > m.maxPeerStreamID:
		m.maxPeerStreamID = streamID
		return true, http2.ErrCodeNo
	default:
		return false, http2.ErrCodeStreamClosed
	}
}

// IsPeerStreamID is true if the stream ID belongs to the peer.
func (m *activeStreamMap) IsPeerStreamID(streamID uint32) bool {
	m.RLock()
	defer m.RUnlock()
	return (streamID % 2) != (m.nextStreamID % 2)
}

// IsLocalStreamID is true if it is a stream we have opened, even if it is now closed.
func (m *activeStreamMap) IsLocalStreamID(streamID uint32) bool {
	m.RLock()
	defer m.RUnlock()
	return (streamID%2) == (m.nextStreamID%2) && streamID < m.nextStreamID
}

// LastPeerStreamID returns the most recently opened peer stream ID.
func (m *activeStreamMap) LastPeerStreamID() uint32 {
	m.RLock()
	defer m.RUnlock()
	return m.maxPeerStreamID
}

// LastLocalStreamID returns the most recently opened local stream ID.
func (m *activeStreamMap) LastLocalStreamID() uint32 {
	m.RLock()
	defer m.RUnlock()
	if m.nextStreamID > 1 {
		return m.nextStreamID - 2
	}
	return 0
}

// Abort closes every active stream and prevents new ones being created. This should be used to
// return errors in pending read/writes when the underlying connection goes away.
func (m *activeStreamMap) Abort() {
	m.Lock()
	defer m.Unlock()
	for _, stream := range m.streams {
		stream.Close()
	}
	m.ignoreNewStreams = true
	m.notifyStreamsEmpty()
}
