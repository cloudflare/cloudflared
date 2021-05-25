package h2mux

import (
	"bytes"
	"encoding/binary"
	"io"
	"time"

	"github.com/rs/zerolog"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

type MuxWriter struct {
	// f is used to write HTTP2 frames.
	f *http2.Framer
	// streams tracks currently-open streams.
	streams *activeStreamMap
	// streamErrors receives stream errors raised by the MuxReader.
	streamErrors *StreamErrorMap
	// readyStreamChan is used to multiplex writable streams onto the single connection.
	// When a stream becomes writable its ID is sent on this channel.
	readyStreamChan <-chan uint32
	// newStreamChan is used to create new streams with a given set of headers.
	newStreamChan <-chan MuxedStreamRequest
	// goAwayChan is used to send a single GOAWAY message to the peer. The element received
	// is the HTTP/2 error code to send.
	goAwayChan <-chan http2.ErrCode
	// abortChan is used when shutting down ungracefully. When this becomes readable, all activity should stop.
	abortChan <-chan struct{}
	// pingTimestamp is an atomic value containing the latest received ping timestamp.
	pingTimestamp *PingTimestamp
	// A timer used to measure idle connection time. Reset after sending data.
	idleTimer *IdleTimer
	// connActiveChan receives a signal that the connection received some (read) activity.
	connActiveChan <-chan struct{}
	// Maximum size of all frames that can be sent on this connection.
	maxFrameSize uint32
	// headerEncoder is the stateful header encoder for this connection
	headerEncoder *hpack.Encoder
	// headerBuffer is the temporary buffer used by headerEncoder.
	headerBuffer bytes.Buffer

	// metricsUpdater is used to report metrics
	metricsUpdater muxMetricsUpdater
	// bytesWrote is the amount of bytes written to data frames since the last time we called metricsUpdater.updateOutBoundBytes()
	bytesWrote *AtomicCounter

	useDictChan <-chan useDictRequest
}

type MuxedStreamRequest struct {
	stream *MuxedStream
	body   io.Reader
}

func NewMuxedStreamRequest(stream *MuxedStream, body io.Reader) MuxedStreamRequest {
	return MuxedStreamRequest{
		stream: stream,
		body:   body,
	}
}

func (r *MuxedStreamRequest) flushBody() {
	io.Copy(r.stream, r.body)
	r.stream.CloseWrite()
}

func tsToPingData(ts int64) [8]byte {
	pingData := [8]byte{}
	binary.LittleEndian.PutUint64(pingData[:], uint64(ts))
	return pingData
}

func (w *MuxWriter) run(log *zerolog.Logger) error {
	defer log.Debug().Msg("mux - write: event loop finished")

	// routine to periodically communicate bytesWrote
	go func() {
		ticker := time.NewTicker(updateFreq)
		defer ticker.Stop()
		for {
			select {
			case <-w.abortChan:
				return
			case <-ticker.C:
				w.metricsUpdater.updateOutBoundBytes(w.bytesWrote.Count())
			}
		}
	}()

	for {
		select {
		case <-w.abortChan:
			log.Debug().Msg("mux - write: aborting writer thread")
			return nil
		case errCode := <-w.goAwayChan:
			log.Debug().Msgf("mux - write: sending GOAWAY code %v", errCode)
			err := w.f.WriteGoAway(w.streams.LastPeerStreamID(), errCode, []byte{})
			if err != nil {
				return err
			}
			w.idleTimer.MarkActive()
		case <-w.pingTimestamp.GetUpdateChan():
			log.Debug().Msg("mux - write: sending PING ACK")
			err := w.f.WritePing(true, tsToPingData(w.pingTimestamp.Get()))
			if err != nil {
				return err
			}
			w.idleTimer.MarkActive()
		case <-w.idleTimer.C:
			if !w.idleTimer.Retry() {
				return ErrConnectionDropped
			}
			log.Debug().Msg("mux - write: sending PING")
			err := w.f.WritePing(false, tsToPingData(time.Now().UnixNano()))
			if err != nil {
				return err
			}
			w.idleTimer.ResetTimer()
		case <-w.connActiveChan:
			w.idleTimer.MarkActive()
		case <-w.streamErrors.GetSignalChan():
			for streamID, errCode := range w.streamErrors.GetErrors() {
				log.Debug().Msgf("mux - write: resetting stream with code: %v streamID: %d", errCode, streamID)
				err := w.f.WriteRSTStream(streamID, errCode)
				if err != nil {
					return err
				}
			}
			w.idleTimer.MarkActive()
		case streamRequest := <-w.newStreamChan:
			streamID := w.streams.AcquireLocalID()
			streamRequest.stream.streamID = streamID
			if !w.streams.Set(streamRequest.stream) {
				// Race between OpenStream and Shutdown, and Shutdown won. Let Shutdown (and the eventual abort) take
				// care of this stream. Ideally we'd pass the error directly to the stream object somehow so the
				// caller can be unblocked sooner, but the value of that optimisation is minimal for most of the
				// reasons why you'd call Shutdown anyway.
				continue
			}
			if streamRequest.body != nil {
				go streamRequest.flushBody()
			}
			err := w.writeStreamData(streamRequest.stream, log)
			if err != nil {
				return err
			}
			w.idleTimer.MarkActive()
		case streamID := <-w.readyStreamChan:
			stream, ok := w.streams.Get(streamID)
			if !ok {
				continue
			}
			err := w.writeStreamData(stream, log)
			if err != nil {
				return err
			}
			w.idleTimer.MarkActive()
		case useDict := <-w.useDictChan:
			err := w.writeUseDictionary(useDict)
			if err != nil {
				log.Error().Msgf("mux - write: error writing use dictionary: %s", err)
				return err
			}
			w.idleTimer.MarkActive()
		}
	}
}

func (w *MuxWriter) writeStreamData(stream *MuxedStream, log *zerolog.Logger) error {
	log.Debug().Msgf("mux - write: writable: streamID: %d", stream.streamID)
	chunk := stream.getChunk()
	w.metricsUpdater.updateReceiveWindow(stream.getReceiveWindow())
	w.metricsUpdater.updateSendWindow(stream.getSendWindow())
	if chunk.sendHeadersFrame() {
		err := w.writeHeaders(chunk.streamID, chunk.headers)
		if err != nil {
			log.Error().Msgf("mux - write: error writing headers: %s: streamID: %d", err, stream.streamID)
			return err
		}
		log.Debug().Msgf("mux - write: output headers: streamID: %d", stream.streamID)
	}

	if chunk.sendWindowUpdateFrame() {
		// Send a WINDOW_UPDATE frame to update our receive window.
		// If the Stream ID is zero, the window update applies to the connection as a whole
		// RFC7540 section-6.9.1 "A receiver that receives a flow-controlled frame MUST
		// always account for  its contribution against the connection flow-control
		// window, unless the receiver treats this as a connection error"
		err := w.f.WriteWindowUpdate(chunk.streamID, chunk.windowUpdate)
		if err != nil {
			log.Error().Msgf("mux - write: error writing window update: %s: streamID: %d", err, stream.streamID)
			return err
		}
		log.Debug().Msgf("mux - write: increment receive window by %d streamID: %d", chunk.windowUpdate, stream.streamID)
	}

	for chunk.sendDataFrame() {
		payload, sentEOF := chunk.nextDataFrame(int(w.maxFrameSize))
		err := w.f.WriteData(chunk.streamID, sentEOF, payload)
		if err != nil {
			log.Error().Msgf("mux - write: error writing data: %s: streamID: %d", err, stream.streamID)
			return err
		}
		// update the amount of data wrote
		w.bytesWrote.IncrementBy(uint64(len(payload)))
		log.Debug().Msgf("mux - write: output data: %d: streamID: %d", len(payload), stream.streamID)

		if sentEOF {
			if stream.readBuffer.Closed() {
				// transition into closed state
				if !stream.gotReceiveEOF() {
					// the peer may send data that we no longer want to receive. Force them into the
					// closed state.
					log.Debug().Msgf("mux - write: resetting stream: streamID: %d", stream.streamID)
					w.f.WriteRSTStream(chunk.streamID, http2.ErrCodeNo)
				} else {
					// Half-open stream transitioned into closed
					log.Debug().Msgf("mux - write: closing stream: streamID: %d", stream.streamID)
				}
				w.streams.Delete(chunk.streamID)
			} else {
				log.Debug().Msgf("mux - write: closing stream write side: streamID: %d", stream.streamID)
			}
		}
	}
	return nil
}

func (w *MuxWriter) encodeHeaders(headers []Header) ([]byte, error) {
	w.headerBuffer.Reset()
	for _, header := range headers {
		err := w.headerEncoder.WriteField(hpack.HeaderField{
			Name:  header.Name,
			Value: header.Value,
		})
		if err != nil {
			return nil, err
		}
	}
	return w.headerBuffer.Bytes(), nil
}

// writeHeaders writes a block of encoded headers, splitting it into multiple frames if necessary.
func (w *MuxWriter) writeHeaders(streamID uint32, headers []Header) error {
	encodedHeaders, err := w.encodeHeaders(headers)
	if err != nil || len(encodedHeaders) == 0 {
		return err
	}

	blockSize := int(w.maxFrameSize)
	// CONTINUATION is unnecessary; the headers fit within the blockSize
	if len(encodedHeaders) < blockSize {
		return w.f.WriteHeaders(http2.HeadersFrameParam{
			StreamID:      streamID,
			EndHeaders:    true,
			BlockFragment: encodedHeaders,
		})
	}

	choppedHeaders := chopEncodedHeaders(encodedHeaders, blockSize)
	// len(choppedHeaders) is at least 2
	if err := w.f.WriteHeaders(http2.HeadersFrameParam{StreamID: streamID, EndHeaders: false, BlockFragment: choppedHeaders[0]}); err != nil {
		return err
	}
	for i := 1; i < len(choppedHeaders)-1; i++ {
		if err := w.f.WriteContinuation(streamID, false, choppedHeaders[i]); err != nil {
			return err
		}
	}
	if err := w.f.WriteContinuation(streamID, true, choppedHeaders[len(choppedHeaders)-1]); err != nil {
		return err
	}

	return nil
}

// Partition a slice of bytes into `len(slice) / blockSize` slices of length `blockSize`
func chopEncodedHeaders(headers []byte, chunkSize int) [][]byte {
	var divided [][]byte

	for i := 0; i < len(headers); i += chunkSize {
		end := i + chunkSize

		if end > len(headers) {
			end = len(headers)
		}

		divided = append(divided, headers[i:end])
	}

	return divided
}

func (w *MuxWriter) writeUseDictionary(dictRequest useDictRequest) error {
	err := w.f.WriteRawFrame(FrameUseDictionary, 0, dictRequest.streamID, []byte{byte(dictRequest.dictID)})
	if err != nil {
		return err
	}
	payload := make([]byte, 0, 64)
	for _, set := range dictRequest.setDict {
		payload = append(payload, byte(set.dictID))
		payload = appendVarInt(payload, 7, uint64(set.dictSZ))
		payload = append(payload, 0x80) // E = 1, D = 0, Truncate = 0
	}

	err = w.f.WriteRawFrame(FrameSetDictionary, FlagSetDictionaryAppend, dictRequest.streamID, payload)
	return err
}
