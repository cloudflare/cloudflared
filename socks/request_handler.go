package socks

import (
	"fmt"
	"io"
	"strings"
)

// RequestHandler is the functions needed to handle a SOCKS5 command
type RequestHandler interface {
	Handle(*Request, io.ReadWriter) error
}

// StandardRequestHandler implements the base socks5 command processing
type StandardRequestHandler struct {
	dialer Dialer
}

// NewRequestHandler creates a standard SOCKS5 request handler
// This handles the SOCKS5 commands and proxies them to their destination
func NewRequestHandler(dialer Dialer) RequestHandler {
	return &StandardRequestHandler{
		dialer: dialer,
	}
}

// Handle processes and responds to socks5 commands
func (h *StandardRequestHandler) Handle(req *Request, conn io.ReadWriter) error {
	switch req.Command {
	case connectCommand:
		return h.handleConnect(conn, req)
	case bindCommand:
		return h.handleBind(conn, req)
	case associateCommand:
		return h.handleAssociate(conn, req)
	default:
		if err := sendReply(conn, commandNotSupported, nil); err != nil {
			return fmt.Errorf("Failed to send reply: %v", err)
		}
		return fmt.Errorf("Unsupported command: %v", req.Command)
	}
}

// handleConnect is used to handle a connect command
func (h *StandardRequestHandler) handleConnect(conn io.ReadWriter, req *Request) error {
	target, localAddr, err := h.dialer.Dial(req.DestAddr.Address())
	if err != nil {
		msg := err.Error()
		resp := hostUnreachable
		if strings.Contains(msg, "refused") {
			resp = connectionRefused
		} else if strings.Contains(msg, "network is unreachable") {
			resp = networkUnreachable
		}
		if err := sendReply(conn, resp, nil); err != nil {
			return fmt.Errorf("Failed to send reply: %v", err)
		}
		return fmt.Errorf("Connect to %v failed: %v", req.DestAddr, err)
	}
	defer target.Close()

	// Send success
	if err := sendReply(conn, successReply, localAddr); err != nil {
		return fmt.Errorf("Failed to send reply: %v", err)
	}

	// Start proxying
	proxyDone := make(chan error, 2)

	go func() {
		_, e := io.Copy(target, req.bufConn)
		proxyDone <- e
	}()

	go func() {
		_, e := io.Copy(conn, target)
		proxyDone <- e
	}()

	// Wait for both
	for i := 0; i < 2; i++ {
		e := <-proxyDone
		if e != nil {
			return e
		}
	}
	return nil
}

// handleBind is used to handle a bind command
// TODO: Support bind command
func (h *StandardRequestHandler) handleBind(conn io.ReadWriter, req *Request) error {
	if err := sendReply(conn, commandNotSupported, nil); err != nil {
		return fmt.Errorf("Failed to send reply: %v", err)
	}
	return nil
}

// handleAssociate is used to handle a connect command
// TODO: Support associate command
func (h *StandardRequestHandler) handleAssociate(conn io.ReadWriter, req *Request) error {
	if err := sendReply(conn, commandNotSupported, nil); err != nil {
		return fmt.Errorf("Failed to send reply: %v", err)
	}
	return nil
}
