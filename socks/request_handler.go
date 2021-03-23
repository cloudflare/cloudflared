package socks

import (
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/ipaccess"
)

// RequestHandler is the functions needed to handle a SOCKS5 command
type RequestHandler interface {
	Handle(*Request, io.ReadWriter) error
}

// StandardRequestHandler implements the base socks5 command processing
type StandardRequestHandler struct {
	dialer       Dialer
	accessPolicy *ipaccess.Policy
}

// NewRequestHandler creates a standard SOCKS5 request handler
// This handles the SOCKS5 commands and proxies them to their destination
func NewRequestHandler(dialer Dialer, accessPolicy *ipaccess.Policy) RequestHandler {
	return &StandardRequestHandler{
		dialer:       dialer,
		accessPolicy: accessPolicy,
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
	if h.accessPolicy != nil {
		if req.DestAddr.IP == nil {
			addr, err := net.ResolveIPAddr("ip", req.DestAddr.FQDN)
			if err != nil {
				_ = sendReply(conn, ruleFailure, req.DestAddr)
				return fmt.Errorf("unable to resolve host to confirm acceess")
			}

			req.DestAddr.IP = addr.IP
		}
		if allowed, rule := h.accessPolicy.Allowed(req.DestAddr.IP, req.DestAddr.Port); !allowed {
			_ = sendReply(conn, ruleFailure, req.DestAddr)
			if rule != nil {
				return fmt.Errorf("Connect to %v denied due to iprule: %s", req.DestAddr, rule.String())
			}
			return fmt.Errorf("Connect to %v denied", req.DestAddr)
		}
	}

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

func StreamHandler(tunnelConn io.ReadWriter, originConn net.Conn, log *zerolog.Logger) {
	dialer := NewConnDialer(originConn)
	requestHandler := NewRequestHandler(dialer, nil)
	socksServer := NewConnectionHandler(requestHandler)

	if err := socksServer.Serve(tunnelConn); err != nil {
		log.Debug().Err(err).Msg("Socks stream handler error")
	}
}

func StreamNetHandler(tunnelConn io.ReadWriter, accessPolicy *ipaccess.Policy, log *zerolog.Logger) {
	dialer := NewNetDialer()
	requestHandler := NewRequestHandler(dialer, accessPolicy)
	socksServer := NewConnectionHandler(requestHandler)

	if err := socksServer.Serve(tunnelConn); err != nil {
		log.Debug().Err(err).Msg("Socks stream handler error")
	}
}
