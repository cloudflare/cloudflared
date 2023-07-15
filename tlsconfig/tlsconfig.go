// Package tlsconfig provides convenience functions for configuring TLS connections from the
// command line.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"os"

	"github.com/pkg/errors"
)

// Config is the user provided parameters to create a tls.Config
type TLSParameters struct {
	Cert                 string
	Key                  string
	GetCertificate       *CertReloader
	GetClientCertificate *CertReloader
	ClientCAs            []string
	RootCAs              []string
	ServerName           string
	CurvePreferences     []tls.CurveID
	MinVersion           uint16 // min tls version. If zero, TLS1.0 is defined as minimum.
	MaxVersion           uint16 // max tls version. If zero, last TLS version is used defined as limit (currently TLS1.3)
}

// GetConfig returns a TLS configuration according to the Config set by the user.
func GetConfig(p *TLSParameters) (*tls.Config, error) {
	tlsconfig := &tls.Config{}
	if p.Cert != "" && p.Key != "" {
		cert, err := tls.LoadX509KeyPair(p.Cert, p.Key)
		if err != nil {
			return nil, errors.Wrap(err, "Error parsing X509 key pair")
		}
		tlsconfig.Certificates = []tls.Certificate{cert}
		// BuildNameToCertificate parses Certificates and builds NameToCertificate from common name
		// and SAN fields of leaf certificates
		tlsconfig.BuildNameToCertificate()
	}

	if p.GetCertificate != nil {
		// GetCertificate is called when client supplies SNI info or Certificates is empty.
		// Order of retrieving certificate is GetCertificate, NameToCertificate and lastly first element of Certificates
		tlsconfig.GetCertificate = p.GetCertificate.Cert
	}

	if p.GetClientCertificate != nil {
		// GetClientCertificate is called when using an HTTP client library and mTLS is required.
		tlsconfig.GetClientCertificate = p.GetClientCertificate.ClientCert
	}

	if len(p.ClientCAs) > 0 {
		// set of root certificate authorities that servers use if required to verify a client certificate
		// by the policy in ClientAuth
		clientCAs, err := LoadCert(p.ClientCAs)
		if err != nil {
			return nil, errors.Wrap(err, "Error loading client CAs")
		}
		tlsconfig.ClientCAs = clientCAs
		// server's policy for TLS Client Authentication. Default is no client cert
		tlsconfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	if len(p.RootCAs) > 0 {
		rootCAs, err := LoadCert(p.RootCAs)
		if err != nil {
			return nil, errors.Wrap(err, "Error loading root CAs")
		}
		tlsconfig.RootCAs = rootCAs
	}

	if p.ServerName != "" {
		tlsconfig.ServerName = p.ServerName
	}

	if len(p.CurvePreferences) > 0 {
		tlsconfig.CurvePreferences = p.CurvePreferences
	} else {
		// Cloudflare optimize CurveP256
		tlsconfig.CurvePreferences = []tls.CurveID{tls.CurveP256}
	}

	tlsconfig.MinVersion = p.MinVersion
	tlsconfig.MaxVersion = p.MaxVersion

	return tlsconfig, nil
}

// LoadCert creates a CertPool containing all certificates in a PEM-format file.
func LoadCert(certPaths []string) (*x509.CertPool, error) {
	ca := x509.NewCertPool()
	for _, certPath := range certPaths {
		caCert, err := os.ReadFile(certPath)
		if err != nil {
			return nil, errors.Wrapf(err, "Error reading certificate %s", certPath)
		}
		if !ca.AppendCertsFromPEM(caCert) {
			return nil, errors.Wrapf(err, "Error parsing certificate %s", certPath)
		}
	}
	return ca, nil
}
