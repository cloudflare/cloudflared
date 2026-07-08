package wire

import (
	"fmt"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/quicvarint"
)

// A MaxStreamsFrame is a MAX_STREAMS frame
type MaxStreamsFrame struct {
	Type         protocol.StreamType
	MaxStreamNum protocol.StreamNum
}

func parseMaxStreamsFrame(b []byte, typ uint64, _ protocol.Version) (*MaxStreamsFrame, int, error) {
	f := &MaxStreamsFrame{}
	switch typ {
	case bidiMaxStreamsFrameType:
		f.Type = protocol.StreamTypeBidi
	case uniMaxStreamsFrameType:
		f.Type = protocol.StreamTypeUni
	}
	streamID, l, err := quicvarint.Parse(b)
	if err != nil {
		return nil, 0, replaceUnexpectedEOF(err)
	}
	f.MaxStreamNum = protocol.StreamNum(streamID)
	if f.MaxStreamNum > protocol.MaxStreamCount {
		return nil, 0, fmt.Errorf("%d exceeds the maximum stream count", f.MaxStreamNum)
	}
	return f, l, nil
}

func (f *MaxStreamsFrame) Append(b []byte, _ protocol.Version) ([]byte, error) {
	switch f.Type {
	case protocol.StreamTypeBidi:
		b = append(b, bidiMaxStreamsFrameType)
	case protocol.StreamTypeUni:
		b = append(b, uniMaxStreamsFrameType)
	}
	b = quicvarint.Append(b, uint64(f.MaxStreamNum))
	return b, nil
}

// Length of a written frame
func (f *MaxStreamsFrame) Length(protocol.Version) protocol.ByteCount {
	return 1 + protocol.ByteCount(quicvarint.Len(uint64(f.MaxStreamNum)))
}
