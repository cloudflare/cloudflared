package wire

import (
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/quicvarint"
)

const (
	pingFrameType               = 0x1
	ackFrameType                = 0x2
	ackECNFrameType             = 0x3
	resetStreamFrameType        = 0x4
	stopSendingFrameType        = 0x5
	cryptoFrameType             = 0x6
	newTokenFrameType           = 0x7
	maxDataFrameType            = 0x10
	maxStreamDataFrameType      = 0x11
	bidiMaxStreamsFrameType     = 0x12
	uniMaxStreamsFrameType      = 0x13
	dataBlockedFrameType        = 0x14
	streamDataBlockedFrameType  = 0x15
	bidiStreamBlockedFrameType  = 0x16
	uniStreamBlockedFrameType   = 0x17
	newConnectionIDFrameType    = 0x18
	retireConnectionIDFrameType = 0x19
	pathChallengeFrameType      = 0x1a
	pathResponseFrameType       = 0x1b
	connectionCloseFrameType    = 0x1c
	applicationCloseFrameType   = 0x1d
	handshakeDoneFrameType      = 0x1e
)

// The FrameParser parses QUIC frames, one by one.
type FrameParser struct {
	ackDelayExponent  uint8
	supportsDatagrams bool

	// To avoid allocating when parsing, keep a single ACK frame struct.
	// It is used over and over again.
	ackFrame *AckFrame
}

// NewFrameParser creates a new frame parser.
func NewFrameParser(supportsDatagrams bool) *FrameParser {
	return &FrameParser{
		supportsDatagrams: supportsDatagrams,
		ackFrame:          &AckFrame{},
	}
}

// ParseNext parses the next frame.
// It skips PADDING frames.
func (p *FrameParser) ParseNext(data []byte, encLevel protocol.EncryptionLevel, v protocol.Version) (int, Frame, error) {
	frame, l, err := p.parseNext(data, encLevel, v)
	return l, frame, err
}

func (p *FrameParser) parseNext(b []byte, encLevel protocol.EncryptionLevel, v protocol.Version) (Frame, int, error) {
	var parsed int
	for len(b) != 0 {
		typ, l, err := quicvarint.Parse(b)
		parsed += l
		if err != nil {
			return nil, parsed, &qerr.TransportError{
				ErrorCode:    qerr.FrameEncodingError,
				ErrorMessage: err.Error(),
			}
		}
		b = b[l:]
		if typ == 0x0 { // skip PADDING frames
			continue
		}

		f, l, err := p.parseFrame(b, typ, encLevel, v)
		parsed += l
		if err != nil {
			return nil, parsed, &qerr.TransportError{
				FrameType:    typ,
				ErrorCode:    qerr.FrameEncodingError,
				ErrorMessage: err.Error(),
			}
		}
		return f, parsed, nil
	}
	return nil, parsed, nil
}

func (p *FrameParser) parseFrame(b []byte, typ uint64, encLevel protocol.EncryptionLevel, v protocol.Version) (Frame, int, error) {
	var frame Frame
	var err error
	var l int
	if typ&0xf8 == 0x8 {
		frame, l, err = parseStreamFrame(b, typ, v)
	} else {
		switch typ {
		case pingFrameType:
			frame = &PingFrame{}
		case ackFrameType, ackECNFrameType:
			ackDelayExponent := p.ackDelayExponent
			if encLevel != protocol.Encryption1RTT {
				ackDelayExponent = protocol.DefaultAckDelayExponent
			}
			p.ackFrame.Reset()
			l, err = parseAckFrame(p.ackFrame, b, typ, ackDelayExponent, v)
			frame = p.ackFrame
		case resetStreamFrameType:
			frame, l, err = parseResetStreamFrame(b, v)
		case stopSendingFrameType:
			frame, l, err = parseStopSendingFrame(b, v)
		case cryptoFrameType:
			frame, l, err = parseCryptoFrame(b, v)
		case newTokenFrameType:
			frame, l, err = parseNewTokenFrame(b, v)
		case maxDataFrameType:
			frame, l, err = parseMaxDataFrame(b, v)
		case maxStreamDataFrameType:
			frame, l, err = parseMaxStreamDataFrame(b, v)
		case bidiMaxStreamsFrameType, uniMaxStreamsFrameType:
			frame, l, err = parseMaxStreamsFrame(b, typ, v)
		case dataBlockedFrameType:
			frame, l, err = parseDataBlockedFrame(b, v)
		case streamDataBlockedFrameType:
			frame, l, err = parseStreamDataBlockedFrame(b, v)
		case bidiStreamBlockedFrameType, uniStreamBlockedFrameType:
			frame, l, err = parseStreamsBlockedFrame(b, typ, v)
		case newConnectionIDFrameType:
			frame, l, err = parseNewConnectionIDFrame(b, v)
		case retireConnectionIDFrameType:
			frame, l, err = parseRetireConnectionIDFrame(b, v)
		case pathChallengeFrameType:
			frame, l, err = parsePathChallengeFrame(b, v)
		case pathResponseFrameType:
			frame, l, err = parsePathResponseFrame(b, v)
		case connectionCloseFrameType, applicationCloseFrameType:
			frame, l, err = parseConnectionCloseFrame(b, typ, v)
		case handshakeDoneFrameType:
			frame = &HandshakeDoneFrame{}
		case 0x30, 0x31:
			if p.supportsDatagrams {
				frame, l, err = parseDatagramFrame(b, typ, v)
				break
			}
			fallthrough
		default:
			err = errors.New("unknown frame type")
		}
	}
	if err != nil {
		return nil, 0, err
	}
	if !p.isAllowedAtEncLevel(frame, encLevel) {
		return nil, l, fmt.Errorf("%s not allowed at encryption level %s", reflect.TypeOf(frame).Elem().Name(), encLevel)
	}
	return frame, l, nil
}

func (p *FrameParser) isAllowedAtEncLevel(f Frame, encLevel protocol.EncryptionLevel) bool {
	switch encLevel {
	case protocol.EncryptionInitial, protocol.EncryptionHandshake:
		switch f.(type) {
		case *CryptoFrame, *AckFrame, *ConnectionCloseFrame, *PingFrame:
			return true
		default:
			return false
		}
	case protocol.Encryption0RTT:
		switch f.(type) {
		case *CryptoFrame, *AckFrame, *ConnectionCloseFrame, *NewTokenFrame, *PathResponseFrame, *RetireConnectionIDFrame:
			return false
		default:
			return true
		}
	case protocol.Encryption1RTT:
		return true
	default:
		panic("unknown encryption level")
	}
}

// SetAckDelayExponent sets the acknowledgment delay exponent (sent in the transport parameters).
// This value is used to scale the ACK Delay field in the ACK frame.
func (p *FrameParser) SetAckDelayExponent(exp uint8) {
	p.ackDelayExponent = exp
}

func replaceUnexpectedEOF(e error) error {
	if e == io.ErrUnexpectedEOF {
		return io.EOF
	}
	return e
}
