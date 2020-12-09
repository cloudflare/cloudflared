package websocket

import (
	"context"
	"github.com/rs/zerolog"
	"io"
	"time"

	gobwas "github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/gorilla/websocket"
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
	log *zerolog.Logger
}

// Read will read messages from the websocket connection
func (c *GorillaConn) Read(p []byte) (int, error) {
	_, message, err := c.Conn.ReadMessage()
	if err != nil {
		return 0, err
	}

	return copy(p, message), nil

}

// Write will write messages to the websocket connection
func (c *GorillaConn) Write(p []byte) (int, error) {
	if err := c.Conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}

	return len(p), nil
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

func NewConn(rw io.ReadWriter, log *zerolog.Logger) *Conn {
	return &Conn{
		rw:  rw,
		log: log,
	}
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

func (c *Conn) Pinger(ctx context.Context) {
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
