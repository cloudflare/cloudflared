package datagramsession

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/packet"
)

const (
	defaultCloseIdleAfter = time.Second * 210
)

func SessionIdleErr(timeout time.Duration) error {
	return fmt.Errorf("session idle for %v", timeout)
}

type transportSender func(session *packet.Session) error

// ErrVithVariableSeverity are errors that have variable severity
type ErrVithVariableSeverity interface {
	error
	// LogLevel return the severity of this error
	LogLevel() zerolog.Level
}

// Session is a bidirectional pipe of datagrams between transport and dstConn
// Destination can be a connection with origin or with eyeball
// When the destination is origin:
// - Manager receives datagrams from receiveChan and calls the transportToDst method of the Session to send to origin
// - Datagrams from origin are read from conn and Send to transport using the transportSender callback. Transport will return them to eyeball
// When the destination is eyeball:
// - Datagrams from eyeball are read from conn and Send to transport. Transport will send them to cloudflared using the transportSender callback.
// - Manager receives datagrams from receiveChan and calls the transportToDst method of the Session to send to the eyeball
type Session struct {
	ID       uuid.UUID
	sendFunc transportSender
	dstConn  io.ReadWriteCloser
	// activeAtChan is used to communicate the last read/write time
	activeAtChan chan time.Time
	closeChan    chan error
	log          *zerolog.Logger
}

func (s *Session) Serve(ctx context.Context, closeAfterIdle time.Duration) (closedByRemote bool, err error) {
	go func() {
		// QUIC implementation copies data to another buffer before returning https://github.com/quic-go/quic-go/blob/v0.24.0/session.go#L1967-L1975
		// This makes it safe to share readBuffer between iterations
		const maxPacketSize = 1500
		readBuffer := make([]byte, maxPacketSize)
		for {
			if closeSession, err := s.dstToTransport(readBuffer); err != nil {
				if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
					s.log.Debug().Msg("Destination connection closed")
				} else {
					level := zerolog.ErrorLevel
					if variableErr, ok := err.(ErrVithVariableSeverity); ok {
						level = variableErr.LogLevel()
					}
					s.log.WithLevel(level).Err(err).Msg("Failed to send session payload from destination to transport")
				}
				if closeSession {
					s.closeChan <- err
					return
				}
			}
		}
	}()
	err = s.waitForCloseCondition(ctx, closeAfterIdle)
	if closeSession, ok := err.(*errClosedSession); ok {
		closedByRemote = closeSession.byRemote
	}
	return closedByRemote, err
}

func (s *Session) waitForCloseCondition(ctx context.Context, closeAfterIdle time.Duration) error {
	// Closing dstConn cancels read so dstToTransport routine in Serve() can return
	defer s.dstConn.Close()
	if closeAfterIdle == 0 {
		// provide default is caller doesn't specify one
		closeAfterIdle = defaultCloseIdleAfter
	}

	checkIdleFreq := closeAfterIdle / 8
	checkIdleTicker := time.NewTicker(checkIdleFreq)
	defer checkIdleTicker.Stop()

	activeAt := time.Now()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case reason := <-s.closeChan:
			return reason
		// TODO: TUN-5423 evaluate if using atomic is more efficient
		case now := <-checkIdleTicker.C:
			// The session is considered inactive if current time is after (last active time + allowed idle time)
			if now.After(activeAt.Add(closeAfterIdle)) {
				return SessionIdleErr(closeAfterIdle)
			}
		case activeAt = <-s.activeAtChan: // Update last active time
		}
	}
}

func (s *Session) dstToTransport(buffer []byte) (closeSession bool, err error) {
	n, err := s.dstConn.Read(buffer)
	s.markActive()
	// https://pkg.go.dev/io#Reader suggests caller should always process n > 0 bytes
	if n > 0 || err == nil {
		session := packet.Session{
			ID:      s.ID,
			Payload: buffer[:n],
		}
		if sendErr := s.sendFunc(&session); sendErr != nil {
			return false, sendErr
		}
	}
	return err != nil, err
}

func (s *Session) transportToDst(payload []byte) (int, error) {
	s.markActive()
	n, err := s.dstConn.Write(payload)
	if err != nil {
		s.log.Err(err).Msg("Failed to write payload to session")
	}
	return n, err
}

// Sends the last active time to the idle checker loop without blocking. activeAtChan will only be full when there
// are many concurrent read/write. It is fine to lose some precision
func (s *Session) markActive() {
	select {
	case s.activeAtChan <- time.Now():
	default:
	}
}

func (s *Session) close(err *errClosedSession) {
	s.closeChan <- err
}
