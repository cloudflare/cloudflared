package wire

import (
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/quicvarint"
)

// A ResetStreamFrame is a RESET_STREAM frame in QUIC
type ResetStreamFrame struct {
	StreamID  protocol.StreamID
	ErrorCode qerr.StreamErrorCode
	FinalSize protocol.ByteCount
}

func parseResetStreamFrame(b []byte, _ protocol.Version) (*ResetStreamFrame, int, error) {
	startLen := len(b)
	var streamID protocol.StreamID
	var byteOffset protocol.ByteCount
	sid, l, err := quicvarint.Parse(b)
	if err != nil {
		return nil, 0, replaceUnexpectedEOF(err)
	}
	b = b[l:]
	streamID = protocol.StreamID(sid)
	errorCode, l, err := quicvarint.Parse(b)
	if err != nil {
		return nil, 0, replaceUnexpectedEOF(err)
	}
	b = b[l:]
	bo, l, err := quicvarint.Parse(b)
	if err != nil {
		return nil, 0, replaceUnexpectedEOF(err)
	}
	byteOffset = protocol.ByteCount(bo)

	return &ResetStreamFrame{
		StreamID:  streamID,
		ErrorCode: qerr.StreamErrorCode(errorCode),
		FinalSize: byteOffset,
	}, startLen - len(b) + l, nil
}

func (f *ResetStreamFrame) Append(b []byte, _ protocol.Version) ([]byte, error) {
	b = append(b, resetStreamFrameType)
	b = quicvarint.Append(b, uint64(f.StreamID))
	b = quicvarint.Append(b, uint64(f.ErrorCode))
	b = quicvarint.Append(b, uint64(f.FinalSize))
	return b, nil
}

// Length of a written frame
func (f *ResetStreamFrame) Length(protocol.Version) protocol.ByteCount {
	return 1 + protocol.ByteCount(quicvarint.Len(uint64(f.StreamID))+quicvarint.Len(uint64(f.ErrorCode))+quicvarint.Len(uint64(f.FinalSize)))
}
