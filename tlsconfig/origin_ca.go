// Package tlsconfig builds base TLS configuration for edge and origin connections.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"runtime"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

const (
	OriginCAPoolFlag = "origin-ca-pool"
)

func LoadOriginCA(originCAPoolFilename string, log *zerolog.Logger) (*x509.CertPool, error) {
	var originCustomCAPool []byte

	if originCAPoolFilename != "" {
		var err error
		// nolint:gosec
		originCustomCAPool, err = os.ReadFile(originCAPoolFilename)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("unable to read the file %s for --%s", originCAPoolFilename, OriginCAPoolFlag))
		}
	}

	originCertPool, err := loadOriginCertPool(originCustomCAPool, log)
	if err != nil {
		return nil, errors.Wrap(err, "error loading the certificate pool")
	}

	// Windows users should be notified that they can use the flag
	if runtime.GOOS == "windows" && originCAPoolFilename == "" {
		log.Info().Msgf("cloudflared does not support loading the system root certificate pool on Windows. Please use --%s <PATH> to specify the path to the certificate pool", OriginCAPoolFlag)
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

	// nolint: gosec
	customOriginCA, err := os.ReadFile(originCAFilename)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("unable to read the file %s", originCAFilename))
	}

	if !certPool.AppendCertsFromPEM(customOriginCA) {
		return nil, fmt.Errorf("error appending custom CA to cert pool")
	}
	return certPool, nil
}

func CreateTunnelConfig(caCert string, serverName string) (*tls.Config, error) {
	tlsConfig := &tls.Config{ServerName: serverName}
	if caCert != "" {
		caCertPEM, err := os.ReadFile(caCert) //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("read CA certificate %s: %w", caCert, err)
		}

		rootCAPool := x509.NewCertPool()
		if !rootCAPool.AppendCertsFromPEM(caCertPEM) {
			return nil, fmt.Errorf("parse CA certificate %s", caCert)
		}
		tlsConfig.RootCAs = rootCAPool
	}

	if tlsConfig.RootCAs == nil {
		rootCAPool, err := x509.SystemCertPool()
		if err != nil {
			return nil, errors.Wrap(err, "unable to get x509 system cert pool")
		}
		cfRootCA, err := GetCloudflareRootCA()
		if err != nil {
			return nil, errors.Wrap(err, "could not append Cloudflare Root CAs to cloudflared certificate pool")
		}
		for _, cert := range cfRootCA {
			rootCAPool.AddCert(cert)
		}
		tlsConfig.RootCAs = rootCAPool
	}

	if tlsConfig.ServerName == "" && !tlsConfig.InsecureSkipVerify {
		return nil, fmt.Errorf("either ServerName or InsecureSkipVerify must be specified in the tls.Config")
	}
	return tlsConfig, nil
}

func loadOriginCertPool(originCAPoolPEM []byte, log *zerolog.Logger) (*x509.CertPool, error) {
	// Get the global pool
	certPool, err := loadGlobalCertPool(log)
	if err != nil {
		return nil, err
	}

	// Then, add any custom origin CA pool the user may have passed
	if originCAPoolPEM != nil {
		if !certPool.AppendCertsFromPEM(originCAPoolPEM) {
			log.Info().Msg("could not append the provided origin CA to the cloudflared certificate pool")
		}
	}

	return certPool, nil
}

func loadGlobalCertPool(log *zerolog.Logger) (*x509.CertPool, error) {
	// First, obtain the system certificate pool
	certPool, err := x509.SystemCertPool()
	if err != nil {
		if runtime.GOOS != "windows" { // See https://github.com/golang/go/issues/16736
			log.Err(err).Msg("error obtaining the system certificates")
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
