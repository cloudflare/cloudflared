package socks

import (
	"fmt"
	"io"
)

const (
	// NoAuth means no authentication is used when connecting
	NoAuth = uint8(0)

	// UserPassAuth means a user/password is used when connecting
	UserPassAuth = uint8(2)

	noAcceptable    = uint8(255)
	userAuthVersion = uint8(1)
	authSuccess     = uint8(0)
	authFailure     = uint8(1)
)

// AuthHandler handles socks authentication requests
type AuthHandler interface {
	Handle(io.Reader, io.Writer) error
	Register(uint8, Authenticator)
}

// StandardAuthHandler loads the default authenticators
type StandardAuthHandler struct {
	authenticators map[uint8]Authenticator
}

// NewAuthHandler creates a default auth handler
func NewAuthHandler() AuthHandler {
	defaults := make(map[uint8]Authenticator)
	defaults[NoAuth] = NewNoAuthAuthenticator()
	return &StandardAuthHandler{
		authenticators: defaults,
	}
}

// Register adds/replaces an Authenticator to use when handling Authentication requests
func (h *StandardAuthHandler) Register(method uint8, a Authenticator) {
	h.authenticators[method] = a
}

// Handle gets the methods from the SOCKS5 client and authenticates with the first supported method
func (h *StandardAuthHandler) Handle(bufConn io.Reader, conn io.Writer) error {
	methods, err := readMethods(bufConn)
	if err != nil {
		return fmt.Errorf("Failed to read auth methods: %v", err)
	}

	// first supported method is used
	for _, method := range methods {
		authenticator := h.authenticators[method]
		if authenticator != nil {
			return authenticator.Handle(bufConn, conn)
		}
	}

	// failed to authenticate. No supported authentication type found
	conn.Write([]byte{socks5Version, noAcceptable})
	return fmt.Errorf("unknown authentication type")
}

// readMethods is used to read the number and type of methods
func readMethods(r io.Reader) ([]byte, error) {
	header := []byte{0}
	if _, err := r.Read(header); err != nil {
		return nil, err
	}

	numMethods := int(header[0])
	methods := make([]byte, numMethods)
	_, err := io.ReadAtLeast(r, methods, numMethods)
	return methods, err
}
