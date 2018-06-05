package tlsconfig

import (
	"crypto/tls"
	"errors"
	"fmt"
	"sync"

	tunnellog "github.com/cloudflare/cloudflared/log"
	"github.com/getsentry/raven-go"
	log "github.com/sirupsen/logrus"
	"gopkg.in/urfave/cli.v2"
)

// CertReloader can load and reload a TLS certificate from a particular filepath.
// Hooks into tls.Config's GetCertificate to allow a TLS server to update its certificate without restarting.
type CertReloader struct {
	sync.Mutex
	certificate *tls.Certificate
	certPath    string
	keyPath     string
}

// NewCertReloader makes a CertReloader, memorizing the filepaths in the context/flags.
func NewCertReloader(c *cli.Context, f CLIFlags) (*CertReloader, error) {
	if !c.IsSet(f.Cert) {
		return nil, errors.New("CertReloader: cert not provided")
	}
	if !c.IsSet(f.Key) {
		return nil, errors.New("CertReloader: key not provided")
	}
	cr := new(CertReloader)
	cr.certPath = c.String(f.Cert)
	cr.keyPath = c.String(f.Key)
	cr.LoadCert()
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
func (cr *CertReloader) LoadCert() {
	cr.Lock()
	defer cr.Unlock()

	log.SetFormatter(&tunnellog.JSONFormatter{})
	log.Info("Reloading certificate")
	cert, err := tls.LoadX509KeyPair(cr.certPath, cr.keyPath)

	// Keep the old certificate if there's a problem reading the new one.
	if err != nil {
		raven.CaptureErrorAndWait(fmt.Errorf("Error parsing X509 key pair: %v", err), nil)
		return
	}
	cr.certificate = &cert
}
