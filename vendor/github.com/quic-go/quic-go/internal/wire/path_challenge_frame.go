package wire

import (
	"bytes"
	"io"

	"github.com/quic-go/quic-go/internal/protocol"
)

// A PathChallengeFrame is a PATH_CHALLENGE frame
type PathChallengeFrame struct {
	Data [8]byte
}

func parsePathChallengeFrame(r *bytes.Reader, _ protocol.Version) (*PathChallengeFrame, error) {
	frame := &PathChallengeFrame{}
	if _, err := io.ReadFull(r, frame.Data[:]); err != nil {
		if err == io.ErrUnexpectedEOF {
			return nil, io.EOF
		}
		return nil, err
	}
	return frame, nil
}

func (f *PathChallengeFrame) Append(b []byte, _ protocol.Version) ([]byte, error) {
	b = append(b, pathChallengeFrameType)
	b = append(b, f.Data[:]...)
	return b, nil
}

// Length of a written frame
func (f *PathChallengeFrame) Length(_ protocol.Version) protocol.ByteCount {
	return 1 + 8
}
