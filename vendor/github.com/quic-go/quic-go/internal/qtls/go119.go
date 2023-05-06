//go:build go1.19 && !go1.20

package qtls

import (
	"crypto"
	"crypto/cipher"
	"crypto/tls"
	"fmt"
	"net"
	"unsafe"

	"github.com/quic-go/qtls-go1-19"
)

type (
	// Alert is a TLS alert
	Alert = qtls.Alert
	// A Certificate is qtls.Certificate.
	Certificate = qtls.Certificate
	// CertificateRequestInfo contains information about a certificate request.
	CertificateRequestInfo = qtls.CertificateRequestInfo
	// A CipherSuiteTLS13 is a cipher suite for TLS 1.3
	CipherSuiteTLS13 = qtls.CipherSuiteTLS13
	// ClientHelloInfo contains information about a ClientHello.
	ClientHelloInfo = qtls.ClientHelloInfo
	// ClientSessionCache is a cache used for session resumption.
	ClientSessionCache = qtls.ClientSessionCache
	// ClientSessionState is a state needed for session resumption.
	ClientSessionState = qtls.ClientSessionState
	// A Config is a qtls.Config.
	Config = qtls.Config
	// A Conn is a qtls.Conn.
	Conn = qtls.Conn
	// ConnectionState contains information about the state of the connection.
	ConnectionState = qtls.ConnectionStateWith0RTT
	// EncryptionLevel is the encryption level of a message.
	EncryptionLevel = qtls.EncryptionLevel
	// Extension is a TLS extension
	Extension = qtls.Extension
	// ExtraConfig is the qtls.ExtraConfig
	ExtraConfig = qtls.ExtraConfig
	// RecordLayer is a qtls RecordLayer.
	RecordLayer = qtls.RecordLayer
)

const (
	// EncryptionHandshake is the Handshake encryption level
	EncryptionHandshake = qtls.EncryptionHandshake
	// Encryption0RTT is the 0-RTT encryption level
	Encryption0RTT = qtls.Encryption0RTT
	// EncryptionApplication is the application data encryption level
	EncryptionApplication = qtls.EncryptionApplication
)

// AEADAESGCMTLS13 creates a new AES-GCM AEAD for TLS 1.3
func AEADAESGCMTLS13(key, fixedNonce []byte) cipher.AEAD {
	return qtls.AEADAESGCMTLS13(key, fixedNonce)
}

// Client returns a new TLS client side connection.
func Client(conn net.Conn, config *Config, extraConfig *ExtraConfig) *Conn {
	return qtls.Client(conn, config, extraConfig)
}

// Server returns a new TLS server side connection.
func Server(conn net.Conn, config *Config, extraConfig *ExtraConfig) *Conn {
	return qtls.Server(conn, config, extraConfig)
}

func GetConnectionState(conn *Conn) ConnectionState {
	return conn.ConnectionStateWith0RTT()
}

// ToTLSConnectionState extracts the tls.ConnectionState
func ToTLSConnectionState(cs ConnectionState) tls.ConnectionState {
	return cs.ConnectionState
}

type cipherSuiteTLS13 struct {
	ID     uint16
	KeyLen int
	AEAD   func(key, fixedNonce []byte) cipher.AEAD
	Hash   crypto.Hash
}

//go:linkname cipherSuiteTLS13ByID github.com/quic-go/qtls-go1-19.cipherSuiteTLS13ByID
func cipherSuiteTLS13ByID(id uint16) *cipherSuiteTLS13

// CipherSuiteTLS13ByID gets a TLS 1.3 cipher suite.
func CipherSuiteTLS13ByID(id uint16) *CipherSuiteTLS13 {
	val := cipherSuiteTLS13ByID(id)
	cs := (*cipherSuiteTLS13)(unsafe.Pointer(val))
	return &qtls.CipherSuiteTLS13{
		ID:     cs.ID,
		KeyLen: cs.KeyLen,
		AEAD:   cs.AEAD,
		Hash:   cs.Hash,
	}
}

//go:linkname cipherSuitesTLS13 github.com/quic-go/qtls-go1-19.cipherSuitesTLS13
var cipherSuitesTLS13 []unsafe.Pointer

//go:linkname defaultCipherSuitesTLS13 github.com/quic-go/qtls-go1-19.defaultCipherSuitesTLS13
var defaultCipherSuitesTLS13 []uint16

//go:linkname defaultCipherSuitesTLS13NoAES github.com/quic-go/qtls-go1-19.defaultCipherSuitesTLS13NoAES
var defaultCipherSuitesTLS13NoAES []uint16

var cipherSuitesModified bool

// SetCipherSuite modifies the cipherSuiteTLS13 slice of cipher suites inside qtls
// such that it only contains the cipher suite with the chosen id.
// The reset function returned resets them back to the original value.
func SetCipherSuite(id uint16) (reset func()) {
	if cipherSuitesModified {
		panic("cipher suites modified multiple times without resetting")
	}
	cipherSuitesModified = true

	origCipherSuitesTLS13 := append([]unsafe.Pointer{}, cipherSuitesTLS13...)
	origDefaultCipherSuitesTLS13 := append([]uint16{}, defaultCipherSuitesTLS13...)
	origDefaultCipherSuitesTLS13NoAES := append([]uint16{}, defaultCipherSuitesTLS13NoAES...)
	// The order is given by the order of the slice elements in cipherSuitesTLS13 in qtls.
	switch id {
	case tls.TLS_AES_128_GCM_SHA256:
		cipherSuitesTLS13 = cipherSuitesTLS13[:1]
	case tls.TLS_CHACHA20_POLY1305_SHA256:
		cipherSuitesTLS13 = cipherSuitesTLS13[1:2]
	case tls.TLS_AES_256_GCM_SHA384:
		cipherSuitesTLS13 = cipherSuitesTLS13[2:]
	default:
		panic(fmt.Sprintf("unexpected cipher suite: %d", id))
	}
	defaultCipherSuitesTLS13 = []uint16{id}
	defaultCipherSuitesTLS13NoAES = []uint16{id}

	return func() {
		cipherSuitesTLS13 = origCipherSuitesTLS13
		defaultCipherSuitesTLS13 = origDefaultCipherSuitesTLS13
		defaultCipherSuitesTLS13NoAES = origDefaultCipherSuitesTLS13NoAES
		cipherSuitesModified = false
	}
}
