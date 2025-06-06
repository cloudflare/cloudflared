package quic

import (
	"context"
	"net"
	"os"
	"sync"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/flowcontrol"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

type deadlineError struct{}

func (deadlineError) Error() string   { return "deadline exceeded" }
func (deadlineError) Temporary() bool { return true }
func (deadlineError) Timeout() bool   { return true }
func (deadlineError) Unwrap() error   { return os.ErrDeadlineExceeded }

var errDeadline net.Error = &deadlineError{}

// The streamSender is notified by the stream about various events.
type streamSender interface {
	onHasConnectionData()
	onHasStreamData(protocol.StreamID, sendStreamI)
	onHasStreamControlFrame(protocol.StreamID, streamControlFrameGetter)
	// must be called without holding the mutex that is acquired by closeForShutdown
	onStreamCompleted(protocol.StreamID)
}

// Each of the both stream halves gets its own uniStreamSender.
// This is necessary in order to keep track when both halves have been completed.
type uniStreamSender struct {
	streamSender
	onStreamCompletedImpl       func()
	onHasStreamControlFrameImpl func(protocol.StreamID, streamControlFrameGetter)
}

func (s *uniStreamSender) onHasStreamData(id protocol.StreamID, str sendStreamI) {
	s.streamSender.onHasStreamData(id, str)
}
func (s *uniStreamSender) onStreamCompleted(protocol.StreamID) { s.onStreamCompletedImpl() }
func (s *uniStreamSender) onHasStreamControlFrame(id protocol.StreamID, str streamControlFrameGetter) {
	s.onHasStreamControlFrameImpl(id, str)
}

var _ streamSender = &uniStreamSender{}

type streamI interface {
	Stream
	closeForShutdown(error)
	// for receiving
	handleStreamFrame(*wire.StreamFrame, time.Time) error
	handleResetStreamFrame(*wire.ResetStreamFrame, time.Time) error
	// for sending
	hasData() bool
	handleStopSendingFrame(*wire.StopSendingFrame)
	popStreamFrame(protocol.ByteCount, protocol.Version) (_ ackhandler.StreamFrame, _ *wire.StreamDataBlockedFrame, hasMore bool)
	updateSendWindow(protocol.ByteCount)
}

var (
	_ receiveStreamI = (streamI)(nil)
	_ sendStreamI    = (streamI)(nil)
)

// A Stream assembles the data from StreamFrames and provides a super-convenient Read-Interface
//
// Read() and Write() may be called concurrently, but multiple calls to Read() or Write() individually must be synchronized manually.
type stream struct {
	receiveStream
	sendStream

	completedMutex         sync.Mutex
	sender                 streamSender
	receiveStreamCompleted bool
	sendStreamCompleted    bool
}

var (
	_ Stream                   = &stream{}
	_ streamControlFrameGetter = &receiveStream{}
)

// newStream creates a new Stream
func newStream(
	ctx context.Context,
	streamID protocol.StreamID,
	sender streamSender,
	flowController flowcontrol.StreamFlowController,
) *stream {
	s := &stream{sender: sender}
	senderForSendStream := &uniStreamSender{
		streamSender: sender,
		onStreamCompletedImpl: func() {
			s.completedMutex.Lock()
			s.sendStreamCompleted = true
			s.checkIfCompleted()
			s.completedMutex.Unlock()
		},
		onHasStreamControlFrameImpl: func(id protocol.StreamID, str streamControlFrameGetter) {
			sender.onHasStreamControlFrame(streamID, s)
		},
	}
	s.sendStream = *newSendStream(ctx, streamID, senderForSendStream, flowController)
	senderForReceiveStream := &uniStreamSender{
		streamSender: sender,
		onStreamCompletedImpl: func() {
			s.completedMutex.Lock()
			s.receiveStreamCompleted = true
			s.checkIfCompleted()
			s.completedMutex.Unlock()
		},
		onHasStreamControlFrameImpl: func(id protocol.StreamID, str streamControlFrameGetter) {
			sender.onHasStreamControlFrame(streamID, s)
		},
	}
	s.receiveStream = *newReceiveStream(streamID, senderForReceiveStream, flowController)
	return s
}

// need to define StreamID() here, since both receiveStream and readStream have a StreamID()
func (s *stream) StreamID() protocol.StreamID {
	// the result is same for receiveStream and sendStream
	return s.sendStream.StreamID()
}

func (s *stream) Close() error {
	return s.sendStream.Close()
}

func (s *stream) getControlFrame(now time.Time) (_ ackhandler.Frame, ok, hasMore bool) {
	f, ok, _ := s.sendStream.getControlFrame(now)
	if ok {
		return f, true, true
	}
	return s.receiveStream.getControlFrame(now)
}

func (s *stream) SetDeadline(t time.Time) error {
	_ = s.SetReadDeadline(t)  // SetReadDeadline never errors
	_ = s.SetWriteDeadline(t) // SetWriteDeadline never errors
	return nil
}

// CloseForShutdown closes a stream abruptly.
// It makes Read and Write unblock (and return the error) immediately.
// The peer will NOT be informed about this: the stream is closed without sending a FIN or RST.
func (s *stream) closeForShutdown(err error) {
	s.sendStream.closeForShutdown(err)
	s.receiveStream.closeForShutdown(err)
}

// checkIfCompleted is called from the uniStreamSender, when one of the stream halves is completed.
// It makes sure that the onStreamCompleted callback is only called if both receive and send side have completed.
func (s *stream) checkIfCompleted() {
	if s.sendStreamCompleted && s.receiveStreamCompleted {
		s.sender.onStreamCompleted(s.StreamID())
	}
}
