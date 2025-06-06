package quic

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/flowcontrol"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
)

type sendStreamI interface {
	SendStream
	handleStopSendingFrame(*wire.StopSendingFrame)
	hasData() bool
	popStreamFrame(protocol.ByteCount, protocol.Version) (_ ackhandler.StreamFrame, _ *wire.StreamDataBlockedFrame, hasMore bool)
	closeForShutdown(error)
	updateSendWindow(protocol.ByteCount)
}

type sendStream struct {
	mutex sync.Mutex

	numOutstandingFrames int64 // outstanding STREAM and RESET_STREAM frames
	retransmissionQueue  []*wire.StreamFrame

	ctx       context.Context
	ctxCancel context.CancelCauseFunc

	streamID protocol.StreamID
	sender   streamSender

	writeOffset protocol.ByteCount

	// finalError is the error that is returned by Write.
	// It can either be a cancellation error or the shutdown error.
	finalError             error
	queuedResetStreamFrame *wire.ResetStreamFrame

	finishedWriting bool // set once Close() is called
	finSent         bool // set when a STREAM_FRAME with FIN bit has been sent
	// Set when the application knows about the cancellation.
	// This can happen because the application called CancelWrite,
	// or because Write returned the error (for remote cancellations).
	cancellationFlagged bool
	cancelled           bool // both local and remote cancellations
	closedForShutdown   bool // set by closeForShutdown
	completed           bool // set when this stream has been reported to the streamSender as completed

	dataForWriting []byte // during a Write() call, this slice is the part of p that still needs to be sent out
	nextFrame      *wire.StreamFrame

	writeChan chan struct{}
	writeOnce chan struct{}
	deadline  time.Time

	flowController flowcontrol.StreamFlowController
}

var (
	_ SendStream               = &sendStream{}
	_ sendStreamI              = &sendStream{}
	_ streamControlFrameGetter = &sendStream{}
)

func newSendStream(
	ctx context.Context,
	streamID protocol.StreamID,
	sender streamSender,
	flowController flowcontrol.StreamFlowController,
) *sendStream {
	s := &sendStream{
		streamID:       streamID,
		sender:         sender,
		flowController: flowController,
		writeChan:      make(chan struct{}, 1),
		writeOnce:      make(chan struct{}, 1), // cap: 1, to protect against concurrent use of Write
	}
	s.ctx, s.ctxCancel = context.WithCancelCause(ctx)
	return s
}

func (s *sendStream) StreamID() protocol.StreamID {
	return s.streamID // same for receiveStream and sendStream
}

func (s *sendStream) Write(p []byte) (int, error) {
	// Concurrent use of Write is not permitted (and doesn't make any sense),
	// but sometimes people do it anyway.
	// Make sure that we only execute one call at any given time to avoid hard to debug failures.
	s.writeOnce <- struct{}{}
	defer func() { <-s.writeOnce }()

	isNewlyCompleted, n, err := s.write(p)
	if isNewlyCompleted {
		s.sender.onStreamCompleted(s.streamID)
	}
	return n, err
}

func (s *sendStream) write(p []byte) (bool /* is newly completed */, int, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.finalError != nil {
		if s.cancelled {
			s.cancellationFlagged = true
		}
		return s.isNewlyCompleted(), 0, s.finalError
	}
	if s.finishedWriting {
		return false, 0, fmt.Errorf("write on closed stream %d", s.streamID)
	}
	if !s.deadline.IsZero() && !time.Now().Before(s.deadline) {
		return false, 0, errDeadline
	}
	if len(p) == 0 {
		return false, 0, nil
	}

	s.dataForWriting = p

	var (
		deadlineTimer  *utils.Timer
		bytesWritten   int
		notifiedSender bool
	)
	for {
		var copied bool
		var deadline time.Time
		// As soon as dataForWriting becomes smaller than a certain size x, we copy all the data to a STREAM frame (s.nextFrame),
		// which can then be popped the next time we assemble a packet.
		// This allows us to return Write() when all data but x bytes have been sent out.
		// When the user now calls Close(), this is much more likely to happen before we popped that last STREAM frame,
		// allowing us to set the FIN bit on that frame (instead of sending an empty STREAM frame with FIN).
		if s.canBufferStreamFrame() && len(s.dataForWriting) > 0 {
			if s.nextFrame == nil {
				f := wire.GetStreamFrame()
				f.Offset = s.writeOffset
				f.StreamID = s.streamID
				f.DataLenPresent = true
				f.Data = f.Data[:len(s.dataForWriting)]
				copy(f.Data, s.dataForWriting)
				s.nextFrame = f
			} else {
				l := len(s.nextFrame.Data)
				s.nextFrame.Data = s.nextFrame.Data[:l+len(s.dataForWriting)]
				copy(s.nextFrame.Data[l:], s.dataForWriting)
			}
			s.dataForWriting = nil
			bytesWritten = len(p)
			copied = true
		} else {
			bytesWritten = len(p) - len(s.dataForWriting)
			deadline = s.deadline
			if !deadline.IsZero() {
				if !time.Now().Before(deadline) {
					s.dataForWriting = nil
					return false, bytesWritten, errDeadline
				}
				if deadlineTimer == nil {
					deadlineTimer = utils.NewTimer()
					defer deadlineTimer.Stop()
				}
				deadlineTimer.Reset(deadline)
			}
			if s.dataForWriting == nil || s.finalError != nil {
				break
			}
		}

		s.mutex.Unlock()
		if !notifiedSender {
			s.sender.onHasStreamData(s.streamID, s) // must be called without holding the mutex
			notifiedSender = true
		}
		if copied {
			s.mutex.Lock()
			break
		}
		if deadline.IsZero() {
			<-s.writeChan
		} else {
			select {
			case <-s.writeChan:
			case <-deadlineTimer.Chan():
				deadlineTimer.SetRead()
			}
		}
		s.mutex.Lock()
	}

	if bytesWritten == len(p) {
		return false, bytesWritten, nil
	}
	if s.finalError != nil {
		if s.cancelled {
			s.cancellationFlagged = true
		}
		return s.isNewlyCompleted(), bytesWritten, s.finalError
	}
	return false, bytesWritten, nil
}

func (s *sendStream) canBufferStreamFrame() bool {
	var l protocol.ByteCount
	if s.nextFrame != nil {
		l = s.nextFrame.DataLen()
	}
	return l+protocol.ByteCount(len(s.dataForWriting)) <= protocol.MaxPacketBufferSize
}

// popStreamFrame returns the next STREAM frame that is supposed to be sent on this stream
// maxBytes is the maximum length this frame (including frame header) will have.
func (s *sendStream) popStreamFrame(maxBytes protocol.ByteCount, v protocol.Version) (_ ackhandler.StreamFrame, _ *wire.StreamDataBlockedFrame, hasMore bool) {
	s.mutex.Lock()
	f, blocked, hasMoreData := s.popNewOrRetransmittedStreamFrame(maxBytes, v)
	if f != nil {
		s.numOutstandingFrames++
	}
	s.mutex.Unlock()

	if f == nil {
		return ackhandler.StreamFrame{}, blocked, hasMoreData
	}
	return ackhandler.StreamFrame{
		Frame:   f,
		Handler: (*sendStreamAckHandler)(s),
	}, blocked, hasMoreData
}

func (s *sendStream) popNewOrRetransmittedStreamFrame(maxBytes protocol.ByteCount, v protocol.Version) (_ *wire.StreamFrame, _ *wire.StreamDataBlockedFrame, hasMoreData bool) {
	if s.finalError != nil {
		return nil, nil, false
	}

	if len(s.retransmissionQueue) > 0 {
		f, hasMoreRetransmissions := s.maybeGetRetransmission(maxBytes, v)
		if f != nil || hasMoreRetransmissions {
			if f == nil {
				return nil, nil, true
			}
			// We always claim that we have more data to send.
			// This might be incorrect, in which case there'll be a spurious call to popStreamFrame in the future.
			return f, nil, true
		}
	}

	if len(s.dataForWriting) == 0 && s.nextFrame == nil {
		if s.finishedWriting && !s.finSent {
			s.finSent = true
			return &wire.StreamFrame{
				StreamID:       s.streamID,
				Offset:         s.writeOffset,
				DataLenPresent: true,
				Fin:            true,
			}, nil, false
		}
		return nil, nil, false
	}

	sendWindow := s.flowController.SendWindowSize()
	if sendWindow == 0 {
		return nil, nil, true
	}

	f, hasMoreData := s.popNewStreamFrame(maxBytes, sendWindow, v)
	if f == nil {
		return nil, nil, hasMoreData
	}
	if f.DataLen() > 0 {
		s.writeOffset += f.DataLen()
		s.flowController.AddBytesSent(f.DataLen())
	}
	var blocked *wire.StreamDataBlockedFrame
	// If the entire send window is used, the stream might have become blocked on stream-level flow control.
	// This is not guaranteed though, because the stream might also have been blocked on connection-level flow control.
	if f.DataLen() == sendWindow && s.flowController.IsNewlyBlocked() {
		blocked = &wire.StreamDataBlockedFrame{StreamID: s.streamID, MaximumStreamData: s.writeOffset}
	}
	f.Fin = s.finishedWriting && s.dataForWriting == nil && s.nextFrame == nil && !s.finSent
	if f.Fin {
		s.finSent = true
	}
	return f, blocked, hasMoreData
}

func (s *sendStream) popNewStreamFrame(maxBytes, sendWindow protocol.ByteCount, v protocol.Version) (*wire.StreamFrame, bool) {
	if s.nextFrame != nil {
		maxDataLen := min(sendWindow, s.nextFrame.MaxDataLen(maxBytes, v))
		if maxDataLen == 0 {
			return nil, true
		}
		nextFrame := s.nextFrame
		s.nextFrame = nil
		if nextFrame.DataLen() > maxDataLen {
			s.nextFrame = wire.GetStreamFrame()
			s.nextFrame.StreamID = s.streamID
			s.nextFrame.Offset = s.writeOffset + maxDataLen
			s.nextFrame.Data = s.nextFrame.Data[:nextFrame.DataLen()-maxDataLen]
			s.nextFrame.DataLenPresent = true
			copy(s.nextFrame.Data, nextFrame.Data[maxDataLen:])
			nextFrame.Data = nextFrame.Data[:maxDataLen]
		} else {
			s.signalWrite()
		}
		return nextFrame, s.nextFrame != nil || s.dataForWriting != nil
	}

	f := wire.GetStreamFrame()
	f.Fin = false
	f.StreamID = s.streamID
	f.Offset = s.writeOffset
	f.DataLenPresent = true
	f.Data = f.Data[:0]

	hasMoreData := s.popNewStreamFrameWithoutBuffer(f, maxBytes, sendWindow, v)
	if len(f.Data) == 0 && !f.Fin {
		f.PutBack()
		return nil, hasMoreData
	}
	return f, hasMoreData
}

func (s *sendStream) popNewStreamFrameWithoutBuffer(f *wire.StreamFrame, maxBytes, sendWindow protocol.ByteCount, v protocol.Version) bool {
	maxDataLen := f.MaxDataLen(maxBytes, v)
	if maxDataLen == 0 { // a STREAM frame must have at least one byte of data
		return s.dataForWriting != nil || s.nextFrame != nil || s.finishedWriting
	}
	s.getDataForWriting(f, min(maxDataLen, sendWindow))

	return s.dataForWriting != nil || s.nextFrame != nil || s.finishedWriting
}

func (s *sendStream) maybeGetRetransmission(maxBytes protocol.ByteCount, v protocol.Version) (*wire.StreamFrame, bool /* has more retransmissions */) {
	f := s.retransmissionQueue[0]
	newFrame, needsSplit := f.MaybeSplitOffFrame(maxBytes, v)
	if needsSplit {
		return newFrame, true
	}
	s.retransmissionQueue = s.retransmissionQueue[1:]
	return f, len(s.retransmissionQueue) > 0
}

func (s *sendStream) hasData() bool {
	s.mutex.Lock()
	hasData := len(s.dataForWriting) > 0
	s.mutex.Unlock()
	return hasData
}

func (s *sendStream) getDataForWriting(f *wire.StreamFrame, maxBytes protocol.ByteCount) {
	if protocol.ByteCount(len(s.dataForWriting)) <= maxBytes {
		f.Data = f.Data[:len(s.dataForWriting)]
		copy(f.Data, s.dataForWriting)
		s.dataForWriting = nil
		s.signalWrite()
		return
	}
	f.Data = f.Data[:maxBytes]
	copy(f.Data, s.dataForWriting)
	s.dataForWriting = s.dataForWriting[maxBytes:]
	if s.canBufferStreamFrame() {
		s.signalWrite()
	}
}

func (s *sendStream) isNewlyCompleted() bool {
	if s.completed {
		return false
	}
	// We need to keep the stream around until all frames have been sent and acknowledged.
	if s.numOutstandingFrames > 0 || len(s.retransmissionQueue) > 0 || s.queuedResetStreamFrame != nil {
		return false
	}
	// The stream is completed if we sent the FIN.
	if s.finSent {
		s.completed = true
		return true
	}
	// The stream is also completed if:
	// 1. the application called CancelWrite, or
	// 2. we received a STOP_SENDING, and
	// 		* the application consumed the error via Write, or
	//		* the application called Close
	if s.cancelled && (s.cancellationFlagged || s.finishedWriting) {
		s.completed = true
		return true
	}
	return false
}

func (s *sendStream) Close() error {
	s.mutex.Lock()
	if s.closedForShutdown || s.finishedWriting {
		s.mutex.Unlock()
		return nil
	}
	s.finishedWriting = true
	cancelled := s.cancelled
	if cancelled {
		s.cancellationFlagged = true
	}
	completed := s.isNewlyCompleted()
	s.mutex.Unlock()

	if completed {
		s.sender.onStreamCompleted(s.streamID)
	}
	if cancelled {
		return fmt.Errorf("close called for canceled stream %d", s.streamID)
	}
	s.sender.onHasStreamData(s.streamID, s) // need to send the FIN, must be called without holding the mutex

	s.ctxCancel(nil)
	return nil
}

func (s *sendStream) CancelWrite(errorCode StreamErrorCode) {
	s.cancelWrite(errorCode, false)
}

// cancelWrite cancels the stream
// It is possible to cancel a stream after it has been closed, both locally and remotely.
// This is useful to prevent the retransmission of outstanding stream data.
func (s *sendStream) cancelWrite(errorCode qerr.StreamErrorCode, remote bool) {
	s.mutex.Lock()
	if s.closedForShutdown {
		s.mutex.Unlock()
		return
	}
	if !remote {
		s.cancellationFlagged = true
		if s.cancelled {
			completed := s.isNewlyCompleted()
			s.mutex.Unlock()
			// The user has called CancelWrite. If the previous cancellation was
			// because of a STOP_SENDING, we don't need to flag the error to the
			// user anymore.
			if completed {
				s.sender.onStreamCompleted(s.streamID)
			}
			return
		}
	}
	if s.cancelled {
		s.mutex.Unlock()
		return
	}
	s.cancelled = true
	s.finalError = &StreamError{StreamID: s.streamID, ErrorCode: errorCode, Remote: remote}
	s.ctxCancel(s.finalError)
	s.numOutstandingFrames = 0
	s.retransmissionQueue = nil
	s.queuedResetStreamFrame = &wire.ResetStreamFrame{
		StreamID:  s.streamID,
		FinalSize: s.writeOffset,
		ErrorCode: errorCode,
	}
	s.mutex.Unlock()

	s.signalWrite()
	s.sender.onHasStreamControlFrame(s.streamID, s)
}

func (s *sendStream) updateSendWindow(limit protocol.ByteCount) {
	updated := s.flowController.UpdateSendWindow(limit)
	if !updated { // duplicate or reordered MAX_STREAM_DATA frame
		return
	}
	s.mutex.Lock()
	hasStreamData := s.dataForWriting != nil || s.nextFrame != nil
	s.mutex.Unlock()
	if hasStreamData {
		s.sender.onHasStreamData(s.streamID, s)
	}
}

func (s *sendStream) handleStopSendingFrame(frame *wire.StopSendingFrame) {
	s.cancelWrite(frame.ErrorCode, true)
}

func (s *sendStream) getControlFrame(time.Time) (_ ackhandler.Frame, ok, hasMore bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.queuedResetStreamFrame == nil {
		return ackhandler.Frame{}, false, false
	}
	s.numOutstandingFrames++
	f := ackhandler.Frame{
		Frame:   s.queuedResetStreamFrame,
		Handler: (*sendStreamResetStreamHandler)(s),
	}
	s.queuedResetStreamFrame = nil
	return f, true, false
}

func (s *sendStream) Context() context.Context {
	return s.ctx
}

func (s *sendStream) SetWriteDeadline(t time.Time) error {
	s.mutex.Lock()
	s.deadline = t
	s.mutex.Unlock()
	s.signalWrite()
	return nil
}

// CloseForShutdown closes a stream abruptly.
// It makes Write unblock (and return the error) immediately.
// The peer will NOT be informed about this: the stream is closed without sending a FIN or RST.
func (s *sendStream) closeForShutdown(err error) {
	s.mutex.Lock()
	s.closedForShutdown = true
	if s.finalError == nil && !s.finishedWriting {
		s.finalError = err
	}
	s.mutex.Unlock()
	s.signalWrite()
}

// signalWrite performs a non-blocking send on the writeChan
func (s *sendStream) signalWrite() {
	select {
	case s.writeChan <- struct{}{}:
	default:
	}
}

type sendStreamAckHandler sendStream

var _ ackhandler.FrameHandler = &sendStreamAckHandler{}

func (s *sendStreamAckHandler) OnAcked(f wire.Frame) {
	sf := f.(*wire.StreamFrame)
	sf.PutBack()
	s.mutex.Lock()
	if s.cancelled {
		s.mutex.Unlock()
		return
	}
	s.numOutstandingFrames--
	if s.numOutstandingFrames < 0 {
		panic("numOutStandingFrames negative")
	}
	completed := (*sendStream)(s).isNewlyCompleted()
	s.mutex.Unlock()

	if completed {
		s.sender.onStreamCompleted(s.streamID)
	}
}

func (s *sendStreamAckHandler) OnLost(f wire.Frame) {
	sf := f.(*wire.StreamFrame)
	s.mutex.Lock()
	if s.cancelled {
		s.mutex.Unlock()
		return
	}
	sf.DataLenPresent = true
	s.retransmissionQueue = append(s.retransmissionQueue, sf)
	s.numOutstandingFrames--
	if s.numOutstandingFrames < 0 {
		panic("numOutStandingFrames negative")
	}
	s.mutex.Unlock()

	s.sender.onHasStreamData(s.streamID, (*sendStream)(s))
}

type sendStreamResetStreamHandler sendStream

var _ ackhandler.FrameHandler = &sendStreamResetStreamHandler{}

func (s *sendStreamResetStreamHandler) OnAcked(wire.Frame) {
	s.mutex.Lock()
	s.numOutstandingFrames--
	if s.numOutstandingFrames < 0 {
		panic("numOutStandingFrames negative")
	}
	completed := (*sendStream)(s).isNewlyCompleted()
	s.mutex.Unlock()

	if completed {
		s.sender.onStreamCompleted(s.streamID)
	}
}

func (s *sendStreamResetStreamHandler) OnLost(f wire.Frame) {
	s.mutex.Lock()
	s.queuedResetStreamFrame = f.(*wire.ResetStreamFrame)
	s.numOutstandingFrames--
	s.mutex.Unlock()
	s.sender.onHasStreamControlFrame(s.streamID, (*sendStream)(s))
}
