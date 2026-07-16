package wire

import (
	"fmt"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/quicvarint"
)

// A StreamsBlockedFrame is a STREAMS_BLOCKED frame
type StreamsBlockedFrame struct {
	Type        protocol.StreamType
	StreamLimit protocol.StreamNum
}

func parseStreamsBlockedFrame(b []byte, typ uint64, _ protocol.Version) (*StreamsBlockedFrame, int, error) {
	f := &StreamsBlockedFrame{}
	switch typ {
	case bidiStreamBlockedFrameType:
		f.Type = protocol.StreamTypeBidi
	case uniStreamBlockedFrameType:
		f.Type = protocol.StreamTypeUni
	}
	streamLimit, l, err := quicvarint.Parse(b)
	if err != nil {
		return nil, 0, replaceUnexpectedEOF(err)
	}
	f.StreamLimit = protocol.StreamNum(streamLimit)
	if f.StreamLimit > protocol.MaxStreamCount {
		return nil, 0, fmt.Errorf("%d exceeds the maximum stream count", f.StreamLimit)
	}
	return f, l, nil
}

func (f *StreamsBlockedFrame) Append(b []byte, _ protocol.Version) ([]byte, error) {
	switch f.Type {
	case protocol.StreamTypeBidi:
		b = append(b, bidiStreamBlockedFrameType)
	case protocol.StreamTypeUni:
		b = append(b, uniStreamBlockedFrameType)
	}
	b = quicvarint.Append(b, uint64(f.StreamLimit))
	return b, nil
}

// Length of a written frame
func (f *StreamsBlockedFrame) Length(_ protocol.Version) protocol.ByteCount {
	return 1 + protocol.ByteCount(quicvarint.Len(uint64(f.StreamLimit)))
}
