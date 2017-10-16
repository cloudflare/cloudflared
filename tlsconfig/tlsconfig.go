// Package tlsconfig provides convenience functions for configuring TLS connections from the
// command line.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"

	log "github.com/Sirupsen/logrus"
	cli "gopkg.in/urfave/cli.v2"
)

// CLIFlags names the flags used to configure TLS for a command or subsystem.
// The nil value for a field means the flag is ignored.
type CLIFlags struct {
	Cert       string
	Key        string
	ClientCert string
	RootCA     string
}

// GetConfig returns a TLS configuration according to the flags defined in f and
// set by the user.
func (f CLIFlags) GetConfig(c *cli.Context) *tls.Config {
	config := &tls.Config{}

	if c.IsSet(f.Cert) && c.IsSet(f.Key) {
		cert, err := tls.LoadX509KeyPair(c.String(f.Cert), c.String(f.Key))
		if err != nil {
			log.WithError(err).Fatal("Error parsing X509 key pair")
		}
		config.Certificates = []tls.Certificate{cert}
		config.BuildNameToCertificate()
	}
	if c.IsSet(f.ClientCert) {
		// set of root certificate authorities that servers use if required to verify a client certificate
		// by the policy in ClientAuth
		config.ClientCAs = LoadCert(c.String(f.ClientCert))
		// server's policy for TLS Client Authentication. Default is no client cert
		config.ClientAuth = tls.RequireAndVerifyClientCert
	}
	// set of root certificate authorities that clients use when verifying server certificates
	if c.IsSet(f.RootCA) {
		config.RootCAs = LoadCert(c.String(f.RootCA))
	}

	return config
}

// LoadCert creates a CertPool containing all certificates in a PEM-format file.
func LoadCert(certPath string) *x509.CertPool {
	caCert, err := ioutil.ReadFile(certPath)
	if err != nil {
		log.WithError(err).Fatalf("Error reading certificate %s", certPath)
	}
	ca := x509.NewCertPool()
	if !ca.AppendCertsFromPEM(caCert) {
		log.WithError(err).Fatalf("Error parsing certificate %s", certPath)
	}
	return ca
}
