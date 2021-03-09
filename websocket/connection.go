package websocket

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	gobwas "github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
)

// GorillaConn is a wrapper around the standard gorilla websocket but implements a ReadWriter
// This is still used by access carrier
type GorillaConn struct {
	*websocket.Conn
	log     *zerolog.Logger
	readBuf bytes.Buffer
}

// Read will read messages from the websocket connection
func (c *GorillaConn) Read(p []byte) (int, error) {
	// Intermediate buffer may contain unread bytes from the last read, start there before blocking on a new frame
	if c.readBuf.Len() > 0 {
		return c.readBuf.Read(p)
	}

	_, message, err := c.Conn.ReadMessage()
	if err != nil {
		return 0, err
	}

	copied := copy(p, message)

	// Write unread bytes to readBuf; if everything was read this is a no-op
	// Write returns a nil error always and grows the buffer; everything is always written or panic
	c.readBuf.Write(message[copied:])

	return copied, nil
}

// Write will write messages to the websocket connection
func (c *GorillaConn) Write(p []byte) (int, error) {
	if err := c.Conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}

	return len(p), nil
}

// SetDeadline sets both read and write deadlines, as per net.Conn interface docs:
// "It is equivalent to calling both SetReadDeadline and SetWriteDeadline."
// Note there is no synchronization here, but the gorilla implementation isn't thread safe anyway
func (c *GorillaConn) SetDeadline(t time.Time) error {
	if err := c.Conn.SetReadDeadline(t); err != nil {
		return fmt.Errorf("error setting read deadline: %w", err)
	}
	if err := c.Conn.SetWriteDeadline(t); err != nil {
		return fmt.Errorf("error setting write deadline: %w", err)
	}
	return nil
}

// pinger simulates the websocket connection to keep it alive
func (c *GorillaConn) pinger(ctx context.Context) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := c.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(writeWait)); err != nil {
				c.log.Debug().Msgf("failed to send ping message: %s", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

type Conn struct {
	rw  io.ReadWriter
	log *zerolog.Logger
}

func NewConn(ctx context.Context, rw io.ReadWriter, log *zerolog.Logger) *Conn {
	c := &Conn{
		rw:  rw,
		log: log,
	}
	go c.pinger(ctx)
	return c
}

// Read will read messages from the websocket connection
func (c *Conn) Read(reader []byte) (int, error) {
	data, err := wsutil.ReadClientBinary(c.rw)
	if err != nil {
		return 0, err
	}
	return copy(reader, data), nil
}

// Write will write messages to the websocket connection
func (c *Conn) Write(p []byte) (int, error) {
	if err := wsutil.WriteServerBinary(c.rw, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *Conn) pinger(ctx context.Context) {
	pongMessge := wsutil.Message{
		OpCode:  gobwas.OpPong,
		Payload: []byte{},
	}
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := wsutil.WriteServerMessage(c.rw, gobwas.OpPing, []byte{}); err != nil {
				c.log.Err(err).Msgf("failed to write ping message")
			}
			if err := wsutil.HandleClientControlMessage(c.rw, pongMessge); err != nil {
				c.log.Err(err).Msgf("failed to write pong message")
			}
		case <-ctx.Done():
			return
		}
	}
}
