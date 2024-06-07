package quic

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/quic-go/quic-go/internal/flowcontrol"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
)

type receiveStreamI interface {
	ReceiveStream

	handleStreamFrame(*wire.StreamFrame) error
	handleResetStreamFrame(*wire.ResetStreamFrame) error
	closeForShutdown(error)
	getWindowUpdate() protocol.ByteCount
}

type receiveStream struct {
	mutex sync.Mutex

	streamID protocol.StreamID

	sender streamSender

	frameQueue  *frameSorter
	finalOffset protocol.ByteCount

	currentFrame       []byte
	currentFrameDone   func()
	readPosInFrame     int
	currentFrameIsLast bool // is the currentFrame the last frame on this stream

	// Set once we read the io.EOF or the cancellation error.
	// Note that for local cancellations, this doesn't necessarily mean that we know the final offset yet.
	errorRead           bool
	completed           bool // set once we've called streamSender.onStreamCompleted
	cancelledRemotely   bool
	cancelledLocally    bool
	cancelErr           *StreamError
	closeForShutdownErr error

	readChan chan struct{}
	readOnce chan struct{} // cap: 1, to protect against concurrent use of Read
	deadline time.Time

	flowController flowcontrol.StreamFlowController
}

var (
	_ ReceiveStream  = &receiveStream{}
	_ receiveStreamI = &receiveStream{}
)

func newReceiveStream(
	streamID protocol.StreamID,
	sender streamSender,
	flowController flowcontrol.StreamFlowController,
) *receiveStream {
	return &receiveStream{
		streamID:       streamID,
		sender:         sender,
		flowController: flowController,
		frameQueue:     newFrameSorter(),
		readChan:       make(chan struct{}, 1),
		readOnce:       make(chan struct{}, 1),
		finalOffset:    protocol.MaxByteCount,
	}
}

func (s *receiveStream) StreamID() protocol.StreamID {
	return s.streamID
}

// Read implements io.Reader. It is not thread safe!
func (s *receiveStream) Read(p []byte) (int, error) {
	// Concurrent use of Read is not permitted (and doesn't make any sense),
	// but sometimes people do it anyway.
	// Make sure that we only execute one call at any given time to avoid hard to debug failures.
	s.readOnce <- struct{}{}
	defer func() { <-s.readOnce }()

	s.mutex.Lock()
	n, err := s.readImpl(p)
	completed := s.isNewlyCompleted()
	s.mutex.Unlock()

	if completed {
		s.sender.onStreamCompleted(s.streamID)
	}
	return n, err
}

func (s *receiveStream) isNewlyCompleted() bool {
	if s.completed {
		return false
	}
	// We need to know the final offset (either via FIN or RESET_STREAM) for flow control accounting.
	if s.finalOffset == protocol.MaxByteCount {
		return false
	}
	// We're done with the stream if it was cancelled locally...
	if s.cancelledLocally {
		s.completed = true
		return true
	}
	// ... or if the error (either io.EOF or the reset error) was read
	if s.errorRead {
		s.completed = true
		return true
	}
	return false
}

func (s *receiveStream) readImpl(p []byte) (int, error) {
	if s.currentFrameIsLast && s.currentFrame == nil {
		s.errorRead = true
		return 0, io.EOF
	}
	if s.cancelledRemotely || s.cancelledLocally {
		s.errorRead = true
		return 0, s.cancelErr
	}
	if s.closeForShutdownErr != nil {
		return 0, s.closeForShutdownErr
	}

	var bytesRead int
	var deadlineTimer *utils.Timer
	for bytesRead < len(p) {
		if s.currentFrame == nil || s.readPosInFrame >= len(s.currentFrame) {
			s.dequeueNextFrame()
		}
		if s.currentFrame == nil && bytesRead > 0 {
			return bytesRead, s.closeForShutdownErr
		}

		for {
			// Stop waiting on errors
			if s.closeForShutdownErr != nil {
				return bytesRead, s.closeForShutdownErr
			}
			if s.cancelledRemotely || s.cancelledLocally {
				s.errorRead = true
				return 0, s.cancelErr
			}

			deadline := s.deadline
			if !deadline.IsZero() {
				if !time.Now().Before(deadline) {
					return bytesRead, errDeadline
				}
				if deadlineTimer == nil {
					deadlineTimer = utils.NewTimer()
					defer deadlineTimer.Stop()
				}
				deadlineTimer.Reset(deadline)
			}

			if s.currentFrame != nil || s.currentFrameIsLast {
				break
			}

			s.mutex.Unlock()
			if deadline.IsZero() {
				<-s.readChan
			} else {
				select {
				case <-s.readChan:
				case <-deadlineTimer.Chan():
					deadlineTimer.SetRead()
				}
			}
			s.mutex.Lock()
			if s.currentFrame == nil {
				s.dequeueNextFrame()
			}
		}

		if bytesRead > len(p) {
			return bytesRead, fmt.Errorf("BUG: bytesRead (%d) > len(p) (%d) in stream.Read", bytesRead, len(p))
		}
		if s.readPosInFrame > len(s.currentFrame) {
			return bytesRead, fmt.Errorf("BUG: readPosInFrame (%d) > frame.DataLen (%d) in stream.Read", s.readPosInFrame, len(s.currentFrame))
		}

		m := copy(p[bytesRead:], s.currentFrame[s.readPosInFrame:])
		s.readPosInFrame += m
		bytesRead += m

		// when a RESET_STREAM was received, the flow controller was already
		// informed about the final byteOffset for this stream
		if !s.cancelledRemotely {
			s.flowController.AddBytesRead(protocol.ByteCount(m))
		}

		if s.readPosInFrame >= len(s.currentFrame) && s.currentFrameIsLast {
			s.currentFrame = nil
			if s.currentFrameDone != nil {
				s.currentFrameDone()
			}
			s.errorRead = true
			return bytesRead, io.EOF
		}
	}
	return bytesRead, nil
}

func (s *receiveStream) dequeueNextFrame() {
	var offset protocol.ByteCount
	// We're done with the last frame. Release the buffer.
	if s.currentFrameDone != nil {
		s.currentFrameDone()
	}
	offset, s.currentFrame, s.currentFrameDone = s.frameQueue.Pop()
	s.currentFrameIsLast = offset+protocol.ByteCount(len(s.currentFrame)) >= s.finalOffset
	s.readPosInFrame = 0
}

func (s *receiveStream) CancelRead(errorCode StreamErrorCode) {
	s.mutex.Lock()
	s.cancelReadImpl(errorCode)
	completed := s.isNewlyCompleted()
	s.mutex.Unlock()

	if completed {
		s.flowController.Abandon()
		s.sender.onStreamCompleted(s.streamID)
	}
}

func (s *receiveStream) cancelReadImpl(errorCode qerr.StreamErrorCode) {
	if s.cancelledLocally { // duplicate call to CancelRead
		return
	}
	s.cancelledLocally = true
	if s.errorRead || s.cancelledRemotely {
		return
	}
	s.cancelErr = &StreamError{StreamID: s.streamID, ErrorCode: errorCode, Remote: false}
	s.signalRead()
	s.sender.queueControlFrame(&wire.StopSendingFrame{
		StreamID:  s.streamID,
		ErrorCode: errorCode,
	})
}

func (s *receiveStream) handleStreamFrame(frame *wire.StreamFrame) error {
	s.mutex.Lock()
	err := s.handleStreamFrameImpl(frame)
	completed := s.isNewlyCompleted()
	s.mutex.Unlock()

	if completed {
		s.flowController.Abandon()
		s.sender.onStreamCompleted(s.streamID)
	}
	return err
}

func (s *receiveStream) handleStreamFrameImpl(frame *wire.StreamFrame) error {
	maxOffset := frame.Offset + frame.DataLen()
	if err := s.flowController.UpdateHighestReceived(maxOffset, frame.Fin); err != nil {
		return err
	}
	if frame.Fin {
		s.finalOffset = maxOffset
	}
	if s.cancelledLocally {
		return nil
	}
	if err := s.frameQueue.Push(frame.Data, frame.Offset, frame.PutBack); err != nil {
		return err
	}
	s.signalRead()
	return nil
}

func (s *receiveStream) handleResetStreamFrame(frame *wire.ResetStreamFrame) error {
	s.mutex.Lock()
	err := s.handleResetStreamFrameImpl(frame)
	completed := s.isNewlyCompleted()
	s.mutex.Unlock()

	if completed {
		s.sender.onStreamCompleted(s.streamID)
	}
	return err
}

func (s *receiveStream) handleResetStreamFrameImpl(frame *wire.ResetStreamFrame) error {
	if s.closeForShutdownErr != nil {
		return nil
	}
	if err := s.flowController.UpdateHighestReceived(frame.FinalSize, true); err != nil {
		return err
	}
	s.finalOffset = frame.FinalSize

	// ignore duplicate RESET_STREAM frames for this stream (after checking their final offset)
	if s.cancelledRemotely {
		return nil
	}
	s.flowController.Abandon()
	// don't save the error if the RESET_STREAM frames was received after CancelRead was called
	if s.cancelledLocally {
		return nil
	}
	s.cancelledRemotely = true
	s.cancelErr = &StreamError{StreamID: s.streamID, ErrorCode: frame.ErrorCode, Remote: true}
	s.signalRead()
	return nil
}

func (s *receiveStream) SetReadDeadline(t time.Time) error {
	s.mutex.Lock()
	s.deadline = t
	s.mutex.Unlock()
	s.signalRead()
	return nil
}

// CloseForShutdown closes a stream abruptly.
// It makes Read unblock (and return the error) immediately.
// The peer will NOT be informed about this: the stream is closed without sending a FIN or RESET.
func (s *receiveStream) closeForShutdown(err error) {
	s.mutex.Lock()
	s.closeForShutdownErr = err
	s.mutex.Unlock()
	s.signalRead()
}

func (s *receiveStream) getWindowUpdate() protocol.ByteCount {
	return s.flowController.GetWindowUpdate()
}

// signalRead performs a non-blocking send on the readChan
func (s *receiveStream) signalRead() {
	select {
	case s.readChan <- struct{}{}:
	default:
	}
}
