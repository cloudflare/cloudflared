package h2mux

import (
	"encoding/binary"
	"io"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
)

type MuxReader struct {
	// f is used to read HTTP2 frames.
	f *http2.Framer
	// handler provides a callback to receive new streams. if nil, new streams cannot be accepted.
	handler MuxedStreamHandler
	// streams tracks currently-open streams.
	streams *activeStreamMap
	// readyList is used to signal writable streams.
	readyList *ReadyList
	// streamErrors lets us report stream errors to the MuxWriter.
	streamErrors *StreamErrorMap
	// goAwayChan is used to tell the writer to send a GOAWAY message.
	goAwayChan chan<- http2.ErrCode
	// abortChan is used when shutting down ungracefully. When this becomes readable, all activity should stop.
	abortChan <-chan struct{}
	// pingTimestamp is an atomic value containing the latest received ping timestamp.
	pingTimestamp *PingTimestamp
	// connActive is used to signal to the writer that something happened on the connection.
	// This is used to clear idle timeout disconnection deadlines.
	connActive Signal
	// The initial value for the send and receive window of a new stream.
	initialStreamWindow uint32
	// The max value for the send window of a stream.
	streamWindowMax uint32
	// r is a reference to the underlying connection used when shutting down.
	r io.Closer
	// updateRTTChan is the channel to send new RTT measurement to muxerMetricsUpdater
	updateRTTChan chan<- *roundTripMeasurement
	// updateReceiveWindowChan is the channel to update receiveWindow size to muxerMetricsUpdater
	updateReceiveWindowChan chan<- uint32
	// updateSendWindowChan is the channel to update sendWindow size to muxerMetricsUpdater
	updateSendWindowChan chan<- uint32
	// bytesRead is the amount of bytes read from data frame since the last time we send bytes read to metrics
	bytesRead *AtomicCounter
	// updateOutBoundBytesChan is the channel to send bytesWrote to muxerMetricsUpdater
	updateInBoundBytesChan chan<- uint64
}

func (r *MuxReader) Shutdown() {
	done := r.streams.Shutdown()
	if done == nil {
		return
	}
	r.sendGoAway(http2.ErrCodeNo)
	go func() {
		// close reader side when last stream ends; this will cause the writer to abort
		<-done
		r.r.Close()
	}()
}

func (r *MuxReader) run(parentLogger *log.Entry) error {
	logger := parentLogger.WithFields(log.Fields{
		"subsystem": "mux",
		"dir":       "read",
	})
	defer logger.Debug("event loop finished")

	// routine to periodically update bytesRead
	go func() {
		tickC := time.Tick(updateFreq)
		for {
			select {
			case <-r.abortChan:
				return
			case <-tickC:
				r.updateInBoundBytesChan <- r.bytesRead.Count()
			}
		}
	}()

	for {
		frame, err := r.f.ReadFrame()
		if err != nil {
			switch e := err.(type) {
			case http2.StreamError:
				logger.WithError(err).Warn("stream error")
				r.streamError(e.StreamID, e.Code)
			case http2.ConnectionError:
				logger.WithError(err).Warn("connection error")
				return r.connectionError(err)
			default:
				if isConnectionClosedError(err) {
					if r.streams.Len() == 0 {
						logger.Debug("shutting down")
						return nil
					}
					logger.Warn("connection closed unexpectedly")
					return err
				} else {
					logger.WithError(err).Warn("frame read error")
					return r.connectionError(err)
				}
			}
		}
		r.connActive.Signal()
		logger.WithField("data", frame).Debug("read frame")
		switch f := frame.(type) {
		case *http2.DataFrame:
			err = r.receiveFrameData(f, logger)
		case *http2.MetaHeadersFrame:
			err = r.receiveHeaderData(f)
		case *http2.RSTStreamFrame:
			streamID := f.Header().StreamID
			if streamID == 0 {
				return ErrInvalidStream
			}
			r.streams.Delete(streamID)
		case *http2.PingFrame:
			r.receivePingData(f)
		case *http2.GoAwayFrame:
			err = r.receiveGoAway(f)
		// The receiver of a flow-controlled frame sends a WINDOW_UPDATE frame as it
		// consumes data and frees up space in flow-control windows
		case *http2.WindowUpdateFrame:
			err = r.updateStreamWindow(f)
		default:
			err = ErrUnexpectedFrameType
		}
		if err != nil {
			logger.WithField("data", frame).WithError(err).Debug("frame error")
			return r.connectionError(err)
		}
	}
}

func (r *MuxReader) newMuxedStream(streamID uint32) *MuxedStream {
	return &MuxedStream{
		streamID:                streamID,
		readBuffer:              NewSharedBuffer(),
		receiveWindow:           r.initialStreamWindow,
		receiveWindowCurrentMax: r.initialStreamWindow,
		receiveWindowMax:        r.streamWindowMax,
		sendWindow:              r.initialStreamWindow,
		readyList:               r.readyList,
	}
}

// getStreamForFrame returns a stream if valid, or an error describing why the stream could not be returned.
func (r *MuxReader) getStreamForFrame(frame http2.Frame) (*MuxedStream, error) {
	sid := frame.Header().StreamID
	if sid == 0 {
		return nil, ErrUnexpectedFrameType
	}
	if stream, ok := r.streams.Get(sid); ok {
		return stream, nil
	}
	if r.streams.IsLocalStreamID(sid) {
		// no stream available, but no error
		return nil, ErrClosedStream
	}
	if sid < r.streams.LastPeerStreamID() {
		// no stream available, stream closed error
		return nil, ErrClosedStream
	}
	return nil, ErrUnknownStream
}

func (r *MuxReader) defaultStreamErrorHandler(err error, header http2.FrameHeader) error {
	if header.Flags.Has(http2.FlagHeadersEndStream) {
		return nil
	} else if err == ErrUnknownStream || err == ErrClosedStream {
		return r.streamError(header.StreamID, http2.ErrCodeStreamClosed)
	} else {
		return err
	}
}

// Receives header frames from a stream. A non-nil error is a connection error.
func (r *MuxReader) receiveHeaderData(frame *http2.MetaHeadersFrame) error {
	var stream *MuxedStream
	sid := frame.Header().StreamID
	if sid == 0 {
		return ErrUnexpectedFrameType
	}
	newStream := r.streams.IsPeerStreamID(sid)
	if newStream {
		// header request
		// TODO support trailers (if stream exists)
		ok, err := r.streams.AcquirePeerID(sid)
		if !ok {
			// ignore new streams while shutting down
			return r.streamError(sid, err)
		}
		stream = r.newMuxedStream(sid)
		// Set stream. Returns false if a stream already existed with that ID or we are shutting down, return false.
		if !r.streams.Set(stream) {
			// got HEADERS frame for an existing stream
			// TODO support trailers
			return r.streamError(sid, http2.ErrCodeInternal)
		}
	} else {
		// header response
		var err error
		if stream, err = r.getStreamForFrame(frame); err != nil {
			return r.defaultStreamErrorHandler(err, frame.Header())
		}
	}
	headers := make([]Header, len(frame.Fields))
	for i, header := range frame.Fields {
		headers[i].Name = header.Name
		headers[i].Value = header.Value
	}
	stream.Headers = headers
	if frame.Header().Flags.Has(http2.FlagHeadersEndStream) {
		stream.receiveEOF()
		return nil
	}
	if newStream {
		go r.handleStream(stream)
	} else {
		close(stream.responseHeadersReceived)
	}
	return nil
}

func (r *MuxReader) handleStream(stream *MuxedStream) {
	defer stream.Close()
	r.handler.ServeStream(stream)
}

// Receives a data frame from a stream. A non-nil error is a connection error.
func (r *MuxReader) receiveFrameData(frame *http2.DataFrame, parentLogger *log.Entry) error {
	logger := parentLogger.WithField("stream", frame.Header().StreamID)
	stream, err := r.getStreamForFrame(frame)
	if err != nil {
		return r.defaultStreamErrorHandler(err, frame.Header())
	}
	data := frame.Data()
	if len(data) > 0 {
		n, err := stream.readBuffer.Write(data)
		if err != nil {
			return r.streamError(stream.streamID, http2.ErrCodeInternal)
		}
		r.bytesRead.IncrementBy(uint64(n))
	}
	if frame.Header().Flags.Has(http2.FlagDataEndStream) {
		if stream.receiveEOF() {
			r.streams.Delete(stream.streamID)
			logger.Debug("stream closed")
		} else {
			logger.Debug("shutdown receive side")
		}
		return nil
	}
	if !stream.consumeReceiveWindow(uint32(len(data))) {
		return r.streamError(stream.streamID, http2.ErrCodeFlowControl)
	}
	r.updateReceiveWindowChan <- stream.getReceiveWindow()
	return nil
}

// Receive a PING from the peer. Update RTT and send/receive window metrics if it's an ACK.
func (r *MuxReader) receivePingData(frame *http2.PingFrame) {
	ts := int64(binary.LittleEndian.Uint64(frame.Data[:]))
	if !frame.IsAck() {
		r.pingTimestamp.Set(ts)
		return
	}

	// Update updates the computed values with a new measurement.
	// outgoingTime is the time that the probe was sent.
	// We assume that time.Now() is the time we received that probe.
	r.updateRTTChan <- &roundTripMeasurement{
		receiveTime: time.Now(),
		sendTime:    time.Unix(0, ts),
	}
}

// Receive a GOAWAY from the peer. Gracefully shut down our connection.
func (r *MuxReader) receiveGoAway(frame *http2.GoAwayFrame) error {
	r.Shutdown()
	// Close all streams above the last processed stream
	lastStream := r.streams.LastLocalStreamID()
	for i := frame.LastStreamID + 2; i <= lastStream; i++ {
		if stream, ok := r.streams.Get(i); ok {
			stream.Close()
		}
	}
	return nil
}

// Receives header frames from a stream. A non-nil error is a connection error.
func (r *MuxReader) updateStreamWindow(frame *http2.WindowUpdateFrame) error {
	stream, err := r.getStreamForFrame(frame)
	if err != nil && err != ErrUnknownStream && err != ErrClosedStream {
		return err
	}
	if stream == nil {
		// ignore window updates on closed streams
		return nil
	}
	stream.replenishSendWindow(frame.Increment)
	r.updateSendWindowChan <- stream.getSendWindow()
	return nil
}

// Raise a stream processing error, closing the stream. Runs on the write thread.
func (r *MuxReader) streamError(streamID uint32, e http2.ErrCode) error {
	r.streamErrors.RaiseError(streamID, e)
	return nil
}

func (r *MuxReader) connectionError(err error) error {
	http2Code := http2.ErrCodeInternal
	switch e := err.(type) {
	case http2.ConnectionError:
		http2Code = http2.ErrCode(e)
	case MuxerProtocolError:
		http2Code = e.h2code
	}
	r.sendGoAway(http2Code)
	return err
}

// Instruct the writer to send a GOAWAY message if possible. This may fail in
// the case where an existing GOAWAY message is in flight or the writer event
// loop already ended.
func (r *MuxReader) sendGoAway(errCode http2.ErrCode) {
	select {
	case r.goAwayChan <- errCode:
	default:
	}
}
