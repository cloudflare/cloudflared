// Package tlsconfig provides convenience functions for configuring TLS connections from the
// command line.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net"

	log "github.com/sirupsen/logrus"
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

func LoadOriginCertsPool() *x509.CertPool {
	// First, obtain the system certificate pool
	certPool, systemCertPoolErr := x509.SystemCertPool()
	if systemCertPoolErr != nil {
		log.Warn("error obtaining the system certificates: %s", systemCertPoolErr)
		certPool = x509.NewCertPool()
	}

	// Next, append the Cloudflare CA pool into the system pool
	if !certPool.AppendCertsFromPEM([]byte(cloudflareRootCA)) {
		log.Warn("could not append the CF certificate to the system certificate pool")

		if systemCertPoolErr != nil { // Obtaining both certificates failed; this is a fatal error
			log.WithError(systemCertPoolErr).Fatalf("Error loading the certificate pool")
		}
	}

	// Finally, add the Hello certificate into the pool (since it's self-signed)
	helloCertificate, err := GetHelloCertificateX509()
	if err != nil {
		log.Warn("error obtaining the Hello server certificate")
	}

	certPool.AddCert(helloCertificate)

	return certPool
}

func CreateTunnelConfig(c *cli.Context, addrs []string) *tls.Config {
	tlsConfig := CLIFlags{RootCA: "cacert"}.GetConfig(c)
	if tlsConfig.RootCAs == nil {
		tlsConfig.RootCAs = GetCloudflareRootCA()
		tlsConfig.ServerName = "cftunnel.com"
	} else if len(addrs) > 0 {
		// Set for development environments and for testing specific origintunneld instances
		tlsConfig.ServerName, _, _ = net.SplitHostPort(addrs[0])
	}
	return tlsConfig
}
