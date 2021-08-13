// +build go1.15
// +build !go1.16

package qtls

import (
	"crypto"
	"crypto/cipher"
	"crypto/tls"
	"net"
	"unsafe"

	qtls "github.com/marten-seemann/qtls-go1-15"
)

type (
	// Alert is a TLS alert
	Alert = qtls.Alert
	// A Certificate is qtls.Certificate.
	Certificate = qtls.Certificate
	// CertificateRequestInfo contains inforamtion about a certificate request.
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

// CipherSuiteName gets the name of a cipher suite.
func CipherSuiteName(id uint16) string {
	return qtls.CipherSuiteName(id)
}

// HkdfExtract generates a pseudorandom key for use with Expand from an input secret and an optional independent salt.
func HkdfExtract(hash crypto.Hash, newSecret, currentSecret []byte) []byte {
	return qtls.HkdfExtract(hash, newSecret, currentSecret)
}

// HkdfExpandLabel HKDF expands a label
func HkdfExpandLabel(hash crypto.Hash, secret, hashValue []byte, label string, L int) []byte {
	return qtls.HkdfExpandLabel(hash, secret, hashValue, label, L)
}

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

//go:linkname cipherSuiteTLS13ByID github.com/marten-seemann/qtls-go1-15.cipherSuiteTLS13ByID
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
