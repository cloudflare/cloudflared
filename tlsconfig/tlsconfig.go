// Package tlsconfig provides convenience functions for configuring TLS connections from the
// command line.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net"

	"github.com/cloudflare/cloudflared/log"
	"github.com/pkg/errors"
	"gopkg.in/urfave/cli.v2"
	"runtime"
)

var logger = log.CreateLogger()

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
			logger.WithError(err).Fatal("Error parsing X509 key pair")
		}
		config.Certificates = []tls.Certificate{cert}
		config.BuildNameToCertificate()
	}
	return f.finishGettingConfig(c, config)
}

func (f CLIFlags) GetConfigReloadableCert(c *cli.Context, cr *CertReloader) *tls.Config {
	config := &tls.Config{
		GetCertificate: cr.Cert,
	}
	config.BuildNameToCertificate()
	return f.finishGettingConfig(c, config)
}

func (f CLIFlags) finishGettingConfig(c *cli.Context, config *tls.Config) *tls.Config {
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
	// we optimize CurveP256
	config.CurvePreferences = []tls.CurveID{tls.CurveP256}
	return config
}

// LoadCert creates a CertPool containing all certificates in a PEM-format file.
func LoadCert(certPath string) *x509.CertPool {
	caCert, err := ioutil.ReadFile(certPath)
	if err != nil {
		logger.WithError(err).Fatalf("Error reading certificate %s", certPath)
	}
	ca := x509.NewCertPool()
	if !ca.AppendCertsFromPEM(caCert) {
		logger.WithError(err).Fatalf("Error parsing certificate %s", certPath)
	}
	return ca
}

func LoadGlobalCertPool() (*x509.CertPool, error) {
	success := false

	// First, obtain the system certificate pool
	certPool, systemCertPoolErr := x509.SystemCertPool()
	if systemCertPoolErr != nil {
		if runtime.GOOS != "windows" {
			logger.Warnf("error obtaining the system certificates: %s", systemCertPoolErr)
		}
		certPool = x509.NewCertPool()
	} else {
		success = true
	}

	// Next, append the Cloudflare CA pool into the system pool
	if !certPool.AppendCertsFromPEM(cloudflareRootCA) {
		logger.Warn("could not append the CF certificate to the cloudflared certificate pool")
	} else {
		success = true
	}

	if success != true { // Obtaining any of the CAs has failed; this is a fatal error
		return nil, errors.New("error loading any of the CAs into the global certificate pool")
	}

	// Finally, add the Hello certificate into the pool (since it's self-signed)
	helloCertificate, err := GetHelloCertificateX509()
	if err != nil {
		logger.Warn("error obtaining the Hello server certificate")
	}

	certPool.AddCert(helloCertificate)

	return certPool, nil
}

func LoadOriginCertPool(originCAPoolPEM []byte) (*x509.CertPool, error) {
	success := false

	// Get the global pool
	certPool, globalPoolErr := LoadGlobalCertPool()
	if globalPoolErr != nil {
		certPool = x509.NewCertPool()
	} else {
		success = true
	}

	// Then, add any custom origin CA pool the user may have passed
	if originCAPoolPEM != nil {
		if !certPool.AppendCertsFromPEM(originCAPoolPEM) {
			logger.Warn("could not append the provided origin CA to the cloudflared certificate pool")
		} else {
			success = true
		}
	}

	if success != true {
		return nil, errors.New("error loading any of the CAs into the origin certificate pool")
	}

	return certPool, nil
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
