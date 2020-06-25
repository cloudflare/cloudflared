package socks

import (
	"bufio"
	"fmt"
	"io"
)

// ConnectionHandler is the Serve method to handle connections
// from a local TCP listener of the standard library (net.Listener)
type ConnectionHandler interface {
	Serve(io.ReadWriter) error
}

// StandardConnectionHandler is the base implementation of handling SOCKS5 requests
type StandardConnectionHandler struct {
	requestHandler RequestHandler
	authHandler    AuthHandler
}

// NewConnectionHandler creates a standard SOCKS5 connection handler
// This process connections from a generic TCP listener from the standard library
func NewConnectionHandler(requestHandler RequestHandler) ConnectionHandler {
	return &StandardConnectionHandler{
		requestHandler: requestHandler,
		authHandler:    NewAuthHandler(),
	}
}

// Serve process new connection created after calling `Accept()` in the standard library
func (h *StandardConnectionHandler) Serve(c io.ReadWriter) error {
	bufConn := bufio.NewReader(c)

	// read the version byte
	version := []byte{0}
	if _, err := bufConn.Read(version); err != nil {
		return err
	}

	// ensure compatibility
	if version[0] != socks5Version {
		return fmt.Errorf("Unsupported SOCKS version: %v", version)
	}

	// handle auth
	if err := h.authHandler.Handle(bufConn, c); err != nil {
		return err
	}

	// process command/request
	req, err := NewRequest(bufConn)
	if err != nil {
		return err
	}

	return h.requestHandler.Handle(req, c)
}
