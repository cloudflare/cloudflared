package tlsconfig

import (
	"crypto/tls"
	"fmt"
	"sync"

	"github.com/getsentry/raven-go"
)

// CertReloader can load and reload a TLS certificate from a particular filepath.
// Hooks into tls.Config's GetCertificate to allow a TLS server to update its certificate without restarting.
type CertReloader struct {
	sync.Mutex
	certificate *tls.Certificate
	certPath    string
	keyPath     string
}

// NewCertReloader makes a CertReloader. It loads the cert during initialization to make sure certPath and keyPath are valid
func NewCertReloader(certPath, keyPath string) (*CertReloader, error) {
	cr := new(CertReloader)
	cr.certPath = certPath
	cr.keyPath = keyPath
	if err := cr.LoadCert(); err != nil {
		return nil, err
	}
	return cr, nil
}

// Cert returns the TLS certificate most recently read by the CertReloader.
func (cr *CertReloader) Cert(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cr.Lock()
	defer cr.Unlock()
	return cr.certificate, nil
}

// LoadCert loads a TLS certificate from the CertReloader's specified filepath.
// Call this after writing a new certificate to the disk (e.g. after renewing a certificate)
func (cr *CertReloader) LoadCert() error {
	cr.Lock()
	defer cr.Unlock()

	cert, err := tls.LoadX509KeyPair(cr.certPath, cr.keyPath)

	// Keep the old certificate if there's a problem reading the new one.
	if err != nil {
		raven.CaptureError(fmt.Errorf("Error parsing X509 key pair: %v", err), nil)
		return err
	}
	cr.certificate = &cert
	return nil
}
