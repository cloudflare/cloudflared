package h2mux

import (
	"bytes"
	"io"
	"sync"
)

type ReadWriteLengther interface {
	io.ReadWriter
	Reset()
	Len() int
}

type ReadWriteClosedCloser interface {
	io.ReadWriteCloser
	Closed() bool
}

// MuxedStreamDataSignaller is a write-only *ReadyList
type MuxedStreamDataSignaller interface {
	// Non-blocking: call this when data is ready to be sent for the given stream ID.
	Signal(ID uint32)
}

type Header struct {
	Name, Value string
}

// MuxedStream is logically an HTTP/2 stream, with an additional buffer for outgoing data.
type MuxedStream struct {
	streamID uint32

	// The "Receive" end of the stream
	readBufferLock sync.RWMutex
	readBuffer     ReadWriteClosedCloser
	// This is the amount of bytes that are in our receive window
	// (how much data we can receive into this stream).
	receiveWindow uint32
	// current receive window size limit. Exponentially increase it when it's exhausted
	receiveWindowCurrentMax uint32
	// hard limit set in http2 spec. 2^31-1
	receiveWindowMax uint32
	// The desired size increment for receiveWindow.
	// If this is nonzero, a WINDOW_UPDATE frame needs to be sent.
	windowUpdate uint32
	// The headers that were most recently received.
	// Particularly:
	//     * for an eyeball-initiated stream (as passed to TunnelHandler::ServeStream),
	//       these are the request headers
	//     * for a cloudflared-initiated stream (as created by Register/UnregisterTunnel),
	//       these are the response headers.
	// They are useful in both of these contexts; hence `Headers` is public.
	Headers []Header
	// For use in the context of a cloudflared-initiated stream.
	responseHeadersReceived chan struct{}

	// The "Send" end of the stream
	writeLock   sync.Mutex
	writeBuffer ReadWriteLengther
	// The maximum capacity that the send buffer should grow to.
	writeBufferMaxLen int
	// A channel to be notified when the send buffer is not full.
	writeBufferHasSpace chan struct{}
	// This is the amount of bytes that are in the peer's receive window
	// (how much data we can send from this stream).
	sendWindow uint32
	// The muxer's readyList
	readyList MuxedStreamDataSignaller
	// The headers that should be sent, and a flag so we only send them once.
	headersSent  bool
	writeHeaders []Header

	// EOF-related fields
	// true if the write end of this stream has been closed
	writeEOF bool
	// true if we have sent EOF to the peer
	sentEOF bool
	// true if the peer sent us an EOF
	receivedEOF bool
	// Compression-related fields
	receivedUseDict bool
	method          string
	contentType     string
	path            string
	dictionaries    h2Dictionaries
}

type TunnelHostname string

func (th TunnelHostname) String() string {
	return string(th)
}

func (th TunnelHostname) IsSet() bool {
	return th != ""
}

func NewStream(config MuxerConfig, writeHeaders []Header, readyList MuxedStreamDataSignaller, dictionaries h2Dictionaries) *MuxedStream {
	return &MuxedStream{
		responseHeadersReceived: make(chan struct{}),
		readBuffer:              NewSharedBuffer(),
		writeBuffer:             &bytes.Buffer{},
		writeBufferMaxLen:       config.StreamWriteBufferMaxLen,
		writeBufferHasSpace:     make(chan struct{}, 1),
		receiveWindow:           config.DefaultWindowSize,
		receiveWindowCurrentMax: config.DefaultWindowSize,
		receiveWindowMax:        config.MaxWindowSize,
		sendWindow:              config.DefaultWindowSize,
		readyList:               readyList,
		writeHeaders:            writeHeaders,
		dictionaries:            dictionaries,
	}
}

func (s *MuxedStream) Read(p []byte) (n int, err error) {
	var readBuffer ReadWriteClosedCloser
	if s.dictionaries.read != nil {
		s.readBufferLock.RLock()
		readBuffer = s.readBuffer
		s.readBufferLock.RUnlock()
	} else {
		readBuffer = s.readBuffer
	}
	n, err = readBuffer.Read(p)
	s.replenishReceiveWindow(uint32(n))
	return
}

// Blocks until len(p) bytes have been written to the buffer
func (s *MuxedStream) Write(p []byte) (int, error) {
	// If assignDictToStream returns success, then it will have acquired the
	// writeLock. Otherwise we must acquire it ourselves.
	ok := assignDictToStream(s, p)
	if !ok {
		s.writeLock.Lock()
	}
	defer s.writeLock.Unlock()

	if s.writeEOF {
		return 0, io.EOF
	}

	// pre-allocate some space in the write buffer if possible
	if buffer, ok := s.writeBuffer.(*bytes.Buffer); ok {
		if buffer.Cap() == 0 {
			buffer.Grow(writeBufferInitialSize)
		}
	}

	totalWritten := 0
	for totalWritten < len(p) {
		// If the buffer is full, block till there is more room.
		// Use a loop to recheck the buffer size after the lock is reacquired.
		for s.writeBufferMaxLen <= s.writeBuffer.Len() {
			s.awaitWriteBufferHasSpace()
			if s.writeEOF {
				return totalWritten, io.EOF
			}
		}
		amountToWrite := len(p) - totalWritten
		spaceAvailable := s.writeBufferMaxLen - s.writeBuffer.Len()
		if spaceAvailable < amountToWrite {
			amountToWrite = spaceAvailable
		}
		amountWritten, err := s.writeBuffer.Write(p[totalWritten : totalWritten+amountToWrite])
		totalWritten += amountWritten
		if err != nil {
			return totalWritten, err
		}
		s.writeNotify()
	}
	return totalWritten, nil
}

func (s *MuxedStream) Close() error {
	// TUN-115: Close the write buffer before the read buffer.
	// In the case of shutdown, read will not get new data, but the write buffer can still receive
	// new data. Closing read before write allows application to race between a failed read and a
	// successful write, even though this close should appear to be atomic.
	// This can't happen the other way because reads may succeed after a failed write; if we read
	// past EOF the application will block until we close the buffer.
	err := s.CloseWrite()
	if err != nil {
		if s.CloseRead() == nil {
			// don't bother the caller with errors if at least one close succeeded
			return nil
		}
		return err
	}
	return s.CloseRead()
}

func (s *MuxedStream) CloseRead() error {
	return s.readBuffer.Close()
}

func (s *MuxedStream) CloseWrite() error {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	if s.writeEOF {
		return io.EOF
	}
	s.writeEOF = true
	if c, ok := s.writeBuffer.(io.Closer); ok {
		c.Close()
	}
	// Allow MuxedStream::Write() to terminate its loop with err=io.EOF, if needed
	s.notifyWriteBufferHasSpace()
	// We need to send something over the wire, even if it's an END_STREAM with no data
	s.writeNotify()
	return nil
}

func (s *MuxedStream) WriteClosed() bool {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	return s.writeEOF
}

func (s *MuxedStream) WriteHeaders(headers []Header) error {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	if s.writeHeaders != nil {
		return ErrStreamHeadersSent
	}

	if s.dictionaries.write != nil {
		dictWriter := s.dictionaries.write.getDictWriter(s, headers)
		if dictWriter != nil {
			s.writeBuffer = dictWriter
		}

	}

	s.writeHeaders = headers
	s.headersSent = false
	s.writeNotify()
	return nil
}

// IsRPCStream returns if the stream is used to transport RPC.
func (s *MuxedStream) IsRPCStream() bool {
	rpcHeaders := RPCHeaders()
	if len(s.Headers) != len(rpcHeaders) {
		return false
	}
	// The headers order matters, so RPC stream should be opened with OpenRPCStream method and let MuxWriter serializes the headers.
	for i, rpcHeader := range rpcHeaders {
		if s.Headers[i] != rpcHeader {
			return false
		}
	}
	return true
}

// Block until a value is sent on writeBufferHasSpace.
// Must be called while holding writeLock
func (s *MuxedStream) awaitWriteBufferHasSpace() {
	s.writeLock.Unlock()
	<-s.writeBufferHasSpace
	s.writeLock.Lock()
}

// Send a value on writeBufferHasSpace without blocking.
// Must be called while holding writeLock
func (s *MuxedStream) notifyWriteBufferHasSpace() {
	select {
	case s.writeBufferHasSpace <- struct{}{}:
	default:
	}
}

func (s *MuxedStream) getReceiveWindow() uint32 {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	return s.receiveWindow
}

func (s *MuxedStream) getSendWindow() uint32 {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	return s.sendWindow
}

// writeNotify must happen while holding writeLock.
func (s *MuxedStream) writeNotify() {
	s.readyList.Signal(s.streamID)
}

// Call by muxreader when it gets a WindowUpdateFrame. This is an update of the peer's
// receive window (how much data we can send).
func (s *MuxedStream) replenishSendWindow(bytes uint32) {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	s.sendWindow += bytes
	s.writeNotify()
}

// Call by muxreader when it receives a data frame
func (s *MuxedStream) consumeReceiveWindow(bytes uint32) bool {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	// received data size is greater than receive window/buffer
	if s.receiveWindow < bytes {
		return false
	}
	s.receiveWindow -= bytes
	if s.receiveWindow < s.receiveWindowCurrentMax/2 && s.receiveWindowCurrentMax < s.receiveWindowMax {
		// exhausting client send window (how much data client can send)
		// and there is room to grow the receive window
		newMax := s.receiveWindowCurrentMax << 1
		if newMax > s.receiveWindowMax {
			newMax = s.receiveWindowMax
		}
		s.windowUpdate += newMax - s.receiveWindowCurrentMax
		s.receiveWindowCurrentMax = newMax
		// notify MuxWriter to write WINDOW_UPDATE frame
		s.writeNotify()
	}
	return true
}

// Arranges for the MuxWriter to send a WINDOW_UPDATE
// Called by MuxedStream::Read when data has left the read buffer.
func (s *MuxedStream) replenishReceiveWindow(bytes uint32) {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	s.windowUpdate += bytes
	s.writeNotify()
}

// receiveEOF should be called when the peer indicates no more data will be sent.
// Returns true if the socket is now closed (i.e. the write side is already closed).
func (s *MuxedStream) receiveEOF() (closed bool) {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	s.receivedEOF = true
	s.CloseRead()
	return s.writeEOF && s.writeBuffer.Len() == 0
}

func (s *MuxedStream) gotReceiveEOF() bool {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	return s.receivedEOF
}

// MuxedStreamReader implements io.ReadCloser for the read end of the stream.
// This is useful for passing to functions that close the object after it is done reading,
// but you still want to be able to write data afterwards (e.g. http.Client).
type MuxedStreamReader struct {
	*MuxedStream
}

func (s MuxedStreamReader) Read(p []byte) (n int, err error) {
	return s.MuxedStream.Read(p)
}

func (s MuxedStreamReader) Close() error {
	return s.MuxedStream.CloseRead()
}

// streamChunk represents a chunk of data to be written.
type streamChunk struct {
	streamID uint32
	// true if a HEADERS frame should be sent
	sendHeaders bool
	headers     []Header
	// nonzero if a WINDOW_UPDATE frame should be sent;
	// in that case, it is the increment value to use
	windowUpdate uint32
	// true if data frames should be sent
	sendData bool
	eof      bool

	buffer []byte
	offset int
}

// getChunk atomically extracts a chunk of data to be written by MuxWriter.
// The data returned will not exceed the send window for this stream.
func (s *MuxedStream) getChunk() *streamChunk {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()

	chunk := &streamChunk{
		streamID:     s.streamID,
		sendHeaders:  !s.headersSent,
		headers:      s.writeHeaders,
		windowUpdate: s.windowUpdate,
		sendData:     !s.sentEOF,
		eof:          s.writeEOF && uint32(s.writeBuffer.Len()) <= s.sendWindow,
	}
	// Copy at most s.sendWindow bytes, adjust the sendWindow accordingly
	toCopy := int(s.sendWindow)
	if toCopy > s.writeBuffer.Len() {
		toCopy = s.writeBuffer.Len()
	}

	if toCopy > 0 {
		buf := make([]byte, toCopy)
		writeLen, _ := s.writeBuffer.Read(buf)
		chunk.buffer = buf[:writeLen]
		s.sendWindow -= uint32(writeLen)
	}

	// Allow MuxedStream::Write() to continue, if needed
	if s.writeBuffer.Len() < s.writeBufferMaxLen {
		s.notifyWriteBufferHasSpace()
	}

	// When we write the chunk, we'll write the WINDOW_UPDATE frame if needed
	s.receiveWindow += s.windowUpdate
	s.windowUpdate = 0

	// When we write the chunk, we'll write the headers if needed
	s.headersSent = true

	// if this chunk contains the end of the stream, close the stream now
	if chunk.sendData && chunk.eof {
		s.sentEOF = true
	}

	return chunk
}

func (c *streamChunk) sendHeadersFrame() bool {
	return c.sendHeaders
}

func (c *streamChunk) sendWindowUpdateFrame() bool {
	return c.windowUpdate > 0
}

func (c *streamChunk) sendDataFrame() bool {
	return c.sendData
}

func (c *streamChunk) nextDataFrame(frameSize int) (payload []byte, endStream bool) {
	bytesLeft := len(c.buffer) - c.offset
	if frameSize > bytesLeft {
		frameSize = bytesLeft
	}
	nextOffset := c.offset + frameSize
	payload = c.buffer[c.offset:nextOffset]
	c.offset = nextOffset

	if c.offset == len(c.buffer) {
		// this is the last data frame in this chunk
		c.sendData = false
		if c.eof {
			endStream = true
		}
	}
	return
}
