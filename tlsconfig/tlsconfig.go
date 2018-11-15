// Package tlsconfig provides convenience functions for configuring TLS connections from the
// command line.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"

	"github.com/pkg/errors"
)

// Config is the user provided parameters to create a tls.Config
type TLSParameters struct {
	Cert             string
	Key              string
	GetCertificate   *CertReloader
	ClientCAs        []string
	RootCAs          []string
	ServerName       string
	CurvePreferences []tls.CurveID
}

// GetConfig returns a TLS configuration according to the Config set by the user.
func GetConfig(p *TLSParameters) (*tls.Config, error) {
	tlsconfig := &tls.Config{}
	if p.GetCertificate != nil {
		tlsconfig.GetCertificate = p.GetCertificate.Cert
		tlsconfig.BuildNameToCertificate()
	} else if p.Cert != "" && p.Key != "" {
		cert, err := tls.LoadX509KeyPair(p.Cert, p.Key)
		if err != nil {
			return nil, errors.Wrap(err, "Error parsing X509 key pair")
		}
		tlsconfig.Certificates = []tls.Certificate{cert}
		tlsconfig.BuildNameToCertificate()
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

	return tlsconfig, nil
}

// LoadCert creates a CertPool containing all certificates in a PEM-format file.
func LoadCert(certPaths []string) (*x509.CertPool, error) {
	ca := x509.NewCertPool()
	for _, certPath := range certPaths {
		caCert, err := ioutil.ReadFile(certPath)
		if err != nil {
			return nil, errors.Wrapf(err, "Error reading certificate %s", certPath)
		}
		if !ca.AppendCertsFromPEM(caCert) {
			return nil, errors.Wrapf(err, "Error parsing certificate %s", certPath)
		}
	}
	return ca, nil
}
