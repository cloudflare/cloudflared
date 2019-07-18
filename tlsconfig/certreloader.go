package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"runtime"
	"sync"

	"github.com/getsentry/raven-go"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/urfave/cli.v2"
)

const (
	OriginCAPoolFlag = "origin-ca-pool"
	CaCertFlag       = "cacert"
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

func LoadOriginCA(c *cli.Context, logger *logrus.Logger) (*x509.CertPool, error) {
	var originCustomCAPool []byte

	originCAPoolFilename := c.String(OriginCAPoolFlag)
	if originCAPoolFilename != "" {
		var err error
		originCustomCAPool, err = ioutil.ReadFile(originCAPoolFilename)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("unable to read the file %s for --%s", originCAPoolFilename, OriginCAPoolFlag))
		}
	}

	originCertPool, err := loadOriginCertPool(originCustomCAPool, logger)
	if err != nil {
		return nil, errors.Wrap(err, "error loading the certificate pool")
	}

	// Windows users should be notified that they can use the flag
	if runtime.GOOS == "windows" && originCAPoolFilename == "" {
		logger.Infof("cloudflared does not support loading the system root certificate pool on Windows. Please use the --%s to specify it", OriginCAPoolFlag)
	}

	return originCertPool, nil
}

func LoadCustomOriginCA(originCAFilename string) (*x509.CertPool, error) {
	// First, obtain the system certificate pool
	certPool, err := x509.SystemCertPool()
	if err != nil {
		certPool = x509.NewCertPool()
	}

	// Next, append the Cloudflare CAs into the system pool
	cfRootCA, err := GetCloudflareRootCA()
	if err != nil {
		return nil, errors.Wrap(err, "could not append Cloudflare Root CAs to cloudflared certificate pool")
	}
	for _, cert := range cfRootCA {
		certPool.AddCert(cert)
	}

	if originCAFilename == "" {
		return certPool, nil
	}

	customOriginCA, err := ioutil.ReadFile(originCAFilename)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("unable to read the file %s", originCAFilename))
	}

	if !certPool.AppendCertsFromPEM(customOriginCA) {
		return nil, fmt.Errorf("error appending custom CA to cert pool")
	}
	return certPool, nil
}

func CreateTunnelConfig(c *cli.Context) (*tls.Config, error) {
	var rootCAs []string
	if c.String(CaCertFlag) != "" {
		rootCAs = append(rootCAs, c.String(CaCertFlag))
	}

	userConfig := &TLSParameters{RootCAs: rootCAs}
	tlsConfig, err := GetConfig(userConfig)
	if err != nil {
		return nil, err
	}

	if tlsConfig.RootCAs == nil {
		rootCAPool := x509.NewCertPool()
		cfRootCA, err := GetCloudflareRootCA()
		if err != nil {
			return nil, errors.Wrap(err, "could not append Cloudflare Root CAs to cloudflared certificate pool")
		}
		for _, cert := range cfRootCA {
			rootCAPool.AddCert(cert)
		}
		tlsConfig.RootCAs = rootCAPool
		tlsConfig.ServerName = "cftunnel.com"
	} else if edgeAddrs := c.StringSlice("edge"); len(edgeAddrs) > 0 {
		// Set for development environments and for testing specific origintunneld instances
		tlsConfig.ServerName, _, _ = net.SplitHostPort(edgeAddrs[0])
	}

	if tlsConfig.ServerName == "" && !tlsConfig.InsecureSkipVerify {
		return nil, fmt.Errorf("either ServerName or InsecureSkipVerify must be specified in the tls.Config")
	}
	return tlsConfig, nil
}

func loadOriginCertPool(originCAPoolPEM []byte, logger *logrus.Logger) (*x509.CertPool, error) {
	// Get the global pool
	certPool, err := loadGlobalCertPool(logger)
	if err != nil {
		return nil, err
	}

	// Then, add any custom origin CA pool the user may have passed
	if originCAPoolPEM != nil {
		if !certPool.AppendCertsFromPEM(originCAPoolPEM) {
			logger.Warn("could not append the provided origin CA to the cloudflared certificate pool")
		}
	}

	return certPool, nil
}

func loadGlobalCertPool(logger *logrus.Logger) (*x509.CertPool, error) {
	// First, obtain the system certificate pool
	certPool, err := x509.SystemCertPool()
	if err != nil {
		if runtime.GOOS != "windows" { // See https://github.com/golang/go/issues/16736
			logger.WithError(err).Warn("error obtaining the system certificates")
		}
		certPool = x509.NewCertPool()
	}

	// Next, append the Cloudflare CAs into the system pool
	cfRootCA, err := GetCloudflareRootCA()
	if err != nil {
		return nil, errors.Wrap(err, "could not append Cloudflare Root CAs to cloudflared certificate pool")
	}
	for _, cert := range cfRootCA {
		certPool.AddCert(cert)
	}

	// Finally, add the Hello certificate into the pool (since it's self-signed)
	helloCert, err := GetHelloCertificateX509()
	if err != nil {
		return nil, errors.Wrap(err, "could not append Hello server certificate to cloudflared certificate pool")
	}
	certPool.AddCert(helloCert)

	return certPool, nil
}
