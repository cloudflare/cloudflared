package h2mux

import (
	"bytes"
	"io"
	"sync"
)

type MuxedStream struct {
	Headers []Header

	streamID uint32

	responseHeadersReceived chan struct{}

	readBuffer    *SharedBuffer
	receiveWindow uint32
	// current window size limit. Exponentially increase it when it's exhausted
	receiveWindowCurrentMax uint32
	// limit set in http2 spec. 2^31-1
	receiveWindowMax uint32

	// nonzero if a WINDOW_UPDATE frame for a stream needs to be sent
	windowUpdate uint32

	writeLock sync.Mutex
	// The zero value for Buffer is an empty buffer ready to use.
	writeBuffer bytes.Buffer

	sendWindow uint32

	readyList    *ReadyList
	headersSent  bool
	writeHeaders []Header
	// true if the write end of this stream has been closed
	writeEOF bool
	// true if we have sent EOF to the peer
	sentEOF bool
	// true if the peer sent us an EOF
	receivedEOF bool
}

func (s *MuxedStream) Read(p []byte) (n int, err error) {
	return s.readBuffer.Read(p)
}

func (s *MuxedStream) Write(p []byte) (n int, err error) {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	if s.writeEOF {
		return 0, io.EOF
	}
	n, err = s.writeBuffer.Write(p)
	if n != len(p) || err != nil {
		return n, err
	}
	s.writeNotify()
	return n, nil
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
	s.writeNotify()
	return nil
}

func (s *MuxedStream) WriteHeaders(headers []Header) error {
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	if s.writeHeaders != nil {
		return ErrStreamHeadersSent
	}
	s.writeHeaders = headers
	s.headersSent = false
	s.writeNotify()
	return nil
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
	s.sendWindow += bytes
	s.writeNotify()
	s.writeLock.Unlock()
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
	if s.receiveWindow < s.receiveWindowCurrentMax/2 {
		// exhausting client send window (how much data client can send)
		if s.receiveWindowCurrentMax < s.receiveWindowMax {
			s.receiveWindowCurrentMax <<= 1
		}
		s.windowUpdate += s.receiveWindowCurrentMax - s.receiveWindow
		s.writeNotify()
	}
	return true
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
	// nonzero if a WINDOW_UPDATE frame should be sent
	windowUpdate uint32
	// true if data frames should be sent
	sendData bool
	eof      bool
	buffer   bytes.Buffer
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

	// Copies at most s.sendWindow bytes
	writeLen, _ := io.CopyN(&chunk.buffer, &s.writeBuffer, int64(s.sendWindow))
	s.sendWindow -= uint32(writeLen)
	s.receiveWindow += s.windowUpdate
	s.windowUpdate = 0
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
	payload = c.buffer.Next(frameSize)
	if c.buffer.Len() == 0 {
		// this is the last data frame in this chunk
		c.sendData = false
		if c.eof {
			endStream = true
		}
	}
	return
}
