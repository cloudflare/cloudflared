package h2mux

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/rs/zerolog"
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
	// The max size for the write buffer of a stream
	streamWriteBufferMaxLen int
	// r is a reference to the underlying connection used when shutting down.
	r io.Closer
	// metricsUpdater is used to report metrics
	metricsUpdater muxMetricsUpdater
	// bytesRead is the amount of bytes read from data frames since the last time we called metricsUpdater.updateInBoundBytes()
	bytesRead *AtomicCounter
	// dictionaries holds the h2 cross-stream compression dictionaries
	dictionaries h2Dictionaries
}

// Shutdown blocks new streams from being created.
// It returns a channel that is closed once the last stream has closed.
func (r *MuxReader) Shutdown() <-chan struct{} {
	done, alreadyInProgress := r.streams.Shutdown()
	if alreadyInProgress {
		return done
	}
	r.sendGoAway(http2.ErrCodeNo)
	go func() {
		// close reader side when last stream ends; this will cause the writer to abort
		<-done
		r.r.Close()
	}()
	return done
}

func (r *MuxReader) run(log *zerolog.Logger) error {
	defer log.Debug().Msg("mux - read: event loop finished")

	// routine to periodically update bytesRead
	go func() {
		ticker := time.NewTicker(updateFreq)
		defer ticker.Stop()
		for {
			select {
			case <-r.abortChan:
				return
			case <-ticker.C:
				r.metricsUpdater.updateInBoundBytes(r.bytesRead.Count())
			}
		}
	}()

	for {
		frame, err := r.f.ReadFrame()
		if err != nil {
			errorString := fmt.Sprintf("mux - read: %s", err)
			if errorDetail := r.f.ErrorDetail(); errorDetail != nil {
				errorString = fmt.Sprintf("%s: errorDetail: %s", errorString, errorDetail)
			}
			switch e := err.(type) {
			case http2.StreamError:
				log.Info().Msgf("%s: stream error", errorString)
				// Ideally we wouldn't return here, since that aborts the muxer.
				// We should communicate the error to the relevant MuxedStream
				// data structure, so that callers of MuxedStream.Read() and
				// MuxedStream.Write() would see it. Then we could `continue`
				// and keep the muxer going.
				return r.streamError(e.StreamID, e.Code)
			case http2.ConnectionError:
				log.Info().Msgf("%s: stream error", errorString)
				return r.connectionError(err)
			default:
				if isConnectionClosedError(err) {
					if r.streams.Len() == 0 {
						// don't log the error here -- that would just be extra noise
						log.Debug().Msg("mux - read: shutting down")
						return nil
					}
					log.Info().Msgf("%s: connection closed unexpectedly", errorString)
					return err
				} else {
					log.Info().Msgf("%s: frame read error", errorString)
					return r.connectionError(err)
				}
			}
		}
		r.connActive.Signal()
		log.Debug().Msgf("mux - read: read frame: data %v", frame)
		switch f := frame.(type) {
		case *http2.DataFrame:
			err = r.receiveFrameData(f, log)
		case *http2.MetaHeadersFrame:
			err = r.receiveHeaderData(f)
		case *http2.RSTStreamFrame:
			streamID := f.Header().StreamID
			if streamID == 0 {
				return ErrInvalidStream
			}
			if stream, ok := r.streams.Get(streamID); ok {
				stream.Close()
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
		case *http2.UnknownFrame:
			switch f.Header().Type {
			case FrameUseDictionary:
				err = r.receiveUseDictionary(f)
			case FrameSetDictionary:
				err = r.receiveSetDictionary(f)
			default:
				err = ErrUnexpectedFrameType
			}
		default:
			err = ErrUnexpectedFrameType
		}
		if err != nil {
			log.Debug().Msgf("mux - read: read error: data %v", frame)
			return r.connectionError(err)
		}
	}
}

func (r *MuxReader) newMuxedStream(streamID uint32) *MuxedStream {
	return &MuxedStream{
		streamID:                streamID,
		readBuffer:              NewSharedBuffer(),
		writeBuffer:             &bytes.Buffer{},
		writeBufferMaxLen:       r.streamWriteBufferMaxLen,
		writeBufferHasSpace:     make(chan struct{}, 1),
		receiveWindow:           r.initialStreamWindow,
		receiveWindowCurrentMax: r.initialStreamWindow,
		receiveWindowMax:        r.streamWindowMax,
		sendWindow:              r.initialStreamWindow,
		readyList:               r.readyList,
		dictionaries:            r.dictionaries,
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
	headers := make([]Header, 0, len(frame.Fields))
	for _, header := range frame.Fields {
		switch header.Name {
		case ":method":
			stream.method = header.Value
		case ":path":
			u, err := url.Parse(header.Value)
			if err == nil {
				stream.path = u.Path
			}
		case "accept-encoding":
			// remove accept-encoding if dictionaries are enabled
			if r.dictionaries.write != nil {
				continue
			}
		}
		headers = append(headers, Header{Name: header.Name, Value: header.Value})
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
func (r *MuxReader) receiveFrameData(frame *http2.DataFrame, log *zerolog.Logger) error {
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
			log.Debug().Msgf("mux - read: stream closed: streamID: %d", frame.Header().StreamID)
		} else {
			log.Debug().Msgf("mux - read: shutdown receive side: streamID: %d", frame.Header().StreamID)
		}
		return nil
	}
	if !stream.consumeReceiveWindow(uint32(len(data))) {
		return r.streamError(stream.streamID, http2.ErrCodeFlowControl)
	}
	r.metricsUpdater.updateReceiveWindow(stream.getReceiveWindow())
	return nil
}

// Receive a PING from the peer. Update RTT and send/receive window metrics if it's an ACK.
func (r *MuxReader) receivePingData(frame *http2.PingFrame) {
	ts := int64(binary.LittleEndian.Uint64(frame.Data[:]))
	if !frame.IsAck() {
		r.pingTimestamp.Set(ts)
		return
	}

	// Update the computed RTT aggregations with a new measurement.
	// `ts` is the time that the probe was sent.
	// We assume that `time.Now()` is the time we received that probe.
	r.metricsUpdater.updateRTT(&roundTripMeasurement{
		receiveTime: time.Now(),
		sendTime:    time.Unix(0, ts),
	})
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

// Receive a USE_DICTIONARY from the peer. Setup dictionary for stream.
func (r *MuxReader) receiveUseDictionary(frame *http2.UnknownFrame) error {
	payload := frame.Payload()
	streamID := frame.StreamID

	// Check frame is formatted properly
	if len(payload) != 1 {
		return r.streamError(streamID, http2.ErrCodeProtocol)
	}

	stream, err := r.getStreamForFrame(frame)
	if err != nil {
		return err
	}

	if stream.receivedUseDict == true || stream.dictionaries.read == nil {
		return r.streamError(streamID, http2.ErrCodeInternal)
	}

	stream.receivedUseDict = true
	dictID := payload[0]

	dictReader := stream.dictionaries.read.newReader(stream.readBuffer.(*SharedBuffer), dictID)
	if dictReader == nil {
		return r.streamError(streamID, http2.ErrCodeInternal)
	}

	stream.readBufferLock.Lock()
	stream.readBuffer = dictReader
	stream.readBufferLock.Unlock()

	return nil
}

// Receive a SET_DICTIONARY from the peer. Update dictionaries accordingly.
func (r *MuxReader) receiveSetDictionary(frame *http2.UnknownFrame) (err error) {

	payload := frame.Payload()
	flags := frame.Flags

	stream, err := r.getStreamForFrame(frame)
	if err != nil && err != ErrClosedStream {
		return err
	}
	reader, ok := stream.readBuffer.(*h2DictionaryReader)
	if !ok {
		return r.streamError(frame.StreamID, http2.ErrCodeProtocol)
	}

	// A SetDictionary frame consists of several
	// Dictionary-Entries that specify how existing dictionaries
	// are to be updated using the current stream data
	// +---------------+---------------+
	// |   Dictionary-Entry (+)    ...
	// +---------------+---------------+

	for {
		// Each Dictionary-Entry is formatted as follows:
		// +-------------------------------+
		// |       Dictionary-ID (8)       |
		// +---+---------------------------+
		// | P |        Size (7+)          |
		// +---+---------------------------+
		// | E?| D?|  Truncate? (6+)       |
		// +---+---------------------------+
		// |           Offset? (8+)        |
		// +-------------------------------+

		var size, truncate, offset uint64
		var p, e, d bool

		// Parse a single Dictionary-Entry
		if len(payload) < 2 { // Must have at least id and size
			return MuxerStreamError{"unexpected EOF", http2.ErrCodeProtocol}
		}

		dictID := uint8(payload[0])
		p = (uint8(payload[1]) >> 7) == 1
		payload, size, err = http2ReadVarInt(7, payload[1:])
		if err != nil {
			return
		}

		if flags.Has(FlagSetDictionaryAppend) {
			// Presence of FlagSetDictionaryAppend means we expect e, d and truncate
			if len(payload) < 1 {
				return MuxerStreamError{"unexpected EOF", http2.ErrCodeProtocol}
			}
			e = (uint8(payload[0]) >> 7) == 1
			d = (uint8((payload[0])>>6) & 1) == 1
			payload, truncate, err = http2ReadVarInt(6, payload)
			if err != nil {
				return
			}
		}

		if flags.Has(FlagSetDictionaryOffset) {
			// Presence of FlagSetDictionaryOffset means we expect offset
			if len(payload) < 1 {
				return MuxerStreamError{"unexpected EOF", http2.ErrCodeProtocol}
			}
			payload, offset, err = http2ReadVarInt(8, payload)
			if err != nil {
				return
			}
		}

		setdict := setDictRequest{streamID: stream.streamID,
			dictID:   dictID,
			dictSZ:   size,
			truncate: truncate,
			offset:   offset,
			P:        p,
			E:        e,
			D:        d}

		// Find the right dictionary
		dict, err := r.dictionaries.read.getDictByID(dictID)
		if err != nil {
			return err
		}

		// Register a dictionary update order for the dictionary and reader
		updateEntry := &dictUpdate{reader: reader, dictionary: dict, s: setdict}
		dict.queue = append(dict.queue, updateEntry)
		reader.queue = append(reader.queue, updateEntry)
		// End of frame
		if len(payload) == 0 {
			break
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
	r.metricsUpdater.updateSendWindow(stream.getSendWindow())
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
