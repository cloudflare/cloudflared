package socks

import (
	"fmt"
	"io"
)

// Authenticator is the connection passed in as a reader/writer to support different authentication types
type Authenticator interface {
	Handle(io.Reader, io.Writer) error
}

// NoAuthAuthenticator is used to handle the No Authentication mode
type NoAuthAuthenticator struct{}

// NewNoAuthAuthenticator creates a authless Authenticator
func NewNoAuthAuthenticator() Authenticator {
	return &NoAuthAuthenticator{}
}

// Handle writes back the version and NoAuth
func (a *NoAuthAuthenticator) Handle(reader io.Reader, writer io.Writer) error {
	_, err := writer.Write([]byte{socks5Version, NoAuth})
	return err
}

// UserPassAuthAuthenticator is used to handle the user/password mode
type UserPassAuthAuthenticator struct {
	IsValid func(string, string) bool
}

// NewUserPassAuthAuthenticator creates a new username/password validator Authenticator
func NewUserPassAuthAuthenticator(isValid func(string, string) bool) Authenticator {
	return &UserPassAuthAuthenticator{
		IsValid: isValid,
	}
}

// Handle writes back the version and NoAuth
func (a *UserPassAuthAuthenticator) Handle(reader io.Reader, writer io.Writer) error {
	if _, err := writer.Write([]byte{socks5Version, UserPassAuth}); err != nil {
		return err
	}

	// Get the version and username length
	header := []byte{0, 0}
	if _, err := io.ReadAtLeast(reader, header, 2); err != nil {
		return err
	}

	// Ensure compatibility. Someone call E-harmony
	if header[0] != userAuthVersion {
		return fmt.Errorf("Unsupported auth version: %v", header[0])
	}

	// Get the user name
	userLen := int(header[1])
	user := make([]byte, userLen)
	if _, err := io.ReadAtLeast(reader, user, userLen); err != nil {
		return err
	}

	// Get the password length
	if _, err := reader.Read(header[:1]); err != nil {
		return err
	}

	// Get the password
	passLen := int(header[0])
	pass := make([]byte, passLen)
	if _, err := io.ReadAtLeast(reader, pass, passLen); err != nil {
		return err
	}

	// Verify the password
	if a.IsValid(string(user), string(pass)) {
		_, err := writer.Write([]byte{userAuthVersion, authSuccess})
		return err
	}

	// password failed. Write back failure
	if _, err := writer.Write([]byte{userAuthVersion, authFailure}); err != nil {
		return err
	}

	return fmt.Errorf("User authentication failed")
}
