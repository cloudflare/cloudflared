package tunnel

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/origin"
	"github.com/cloudflare/cloudflared/tlsconfig"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/validation"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/sirupsen/logrus"
	"gopkg.in/urfave/cli.v2"

	"github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
)

var (
	developerPortal = "https://developers.cloudflare.com/argo-tunnel"
	quickStartUrl   = developerPortal + "/quickstart/quickstart/"
	serviceUrl      = developerPortal + "/reference/service/"
	argumentsUrl    = developerPortal + "/reference/arguments/"
)

// returns the first path that contains a cert.pem file. If none of the DefaultConfigDirs
// contains a cert.pem file, return empty string
func findDefaultOriginCertPath() string {
	for _, defaultConfigDir := range config.DefaultConfigDirs {
		originCertPath, _ := homedir.Expand(filepath.Join(defaultConfigDir, config.DefaultCredentialFile))
		if ok, _ := config.FileExists(originCertPath); ok {
			return originCertPath
		}
	}
	return ""
}

func generateRandomClientID() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	id := make([]byte, 32)
	r.Read(id)
	return hex.EncodeToString(id)
}

func handleDeprecatedOptions(c *cli.Context) error {
	// Fail if the user provided an old authentication method
	if c.IsSet("api-key") || c.IsSet("api-email") || c.IsSet("api-ca-key") {
		logger.Error("You don't need to give us your api-key anymore. Please use the new login method. Just run cloudflared login")
		return fmt.Errorf("Client provided deprecated options")
	}
	return nil
}

func logClientOptions(c *cli.Context) {
	flags := make(map[string]interface{})
	for _, flag := range c.LocalFlagNames() {
		flags[flag] = c.Generic(flag)
	}

	sliceFlags := []string{"header", "tag", "proxy-dns-upstream", "upstream", "edge"}
	for _, sliceFlag := range sliceFlags {
		if len(c.StringSlice(sliceFlag)) > 0 {
			flags[sliceFlag] = strings.Join(c.StringSlice(sliceFlag), ", ")
		}
	}

	if len(flags) > 0 {
		logger.WithFields(flags).Info("Flags")
	}

	envs := make(map[string]string)
	// Find env variables for Argo Tunnel
	for _, env := range os.Environ() {
		// All Argo Tunnel env variables start with TUNNEL_
		if strings.Contains(env, "TUNNEL_") {
			vars := strings.Split(env, "=")
			if len(vars) == 2 {
				envs[vars[0]] = vars[1]
			}
		}
	}
	if len(envs) > 0 {
		logger.Infof("Environmental variables %v", envs)
	}
}

func dnsProxyStandAlone(c *cli.Context) bool {
	return c.IsSet("proxy-dns") && (!c.IsSet("hostname") && !c.IsSet("tag") && !c.IsSet("hello-world"))
}

func getOriginCert(c *cli.Context) ([]byte, error) {
	if c.String("origincert") == "" {
		logger.Warnf("Cannot determine default origin certificate path. No file %s in %v", config.DefaultCredentialFile, config.DefaultConfigDirs)
		if isRunningFromTerminal() {
			logger.Errorf("You need to specify the origin certificate path with --origincert option, or set TUNNEL_ORIGIN_CERT environment variable. See %s for more information.", argumentsUrl)
			return nil, fmt.Errorf("Client didn't specify origincert path when running from terminal")
		} else {
			logger.Errorf("You need to specify the origin certificate path by specifying the origincert option in the configuration file, or set TUNNEL_ORIGIN_CERT environment variable. See %s for more information.", serviceUrl)
			return nil, fmt.Errorf("Client didn't specify origincert path")
		}
	}
	// Check that the user has acquired a certificate using the login command
	originCertPath, err := homedir.Expand(c.String("origincert"))
	if err != nil {
		logger.WithError(err).Errorf("Cannot resolve path %s", c.String("origincert"))
		return nil, fmt.Errorf("Cannot resolve path %s", c.String("origincert"))
	}
	ok, err := config.FileExists(originCertPath)
	if err != nil {
		logger.Errorf("Cannot check if origin cert exists at path %s", c.String("origincert"))
		return nil, fmt.Errorf("Cannot check if origin cert exists at path %s", c.String("origincert"))
	}
	if !ok {
		logger.Errorf(`Cannot find a valid certificate for your origin at the path:

    %s

If the path above is wrong, specify the path with the -origincert option.
If you don't have a certificate signed by Cloudflare, run the command:

	%s login
`, originCertPath, os.Args[0])
		return nil, fmt.Errorf("Cannot find a valid certificate at the path %s", originCertPath)
	}
	// Easier to send the certificate as []byte via RPC than decoding it at this point
	originCert, err := ioutil.ReadFile(originCertPath)
	if err != nil {
		logger.WithError(err).Errorf("Cannot read %s to load origin certificate", originCertPath)
		return nil, fmt.Errorf("Cannot read %s to load origin certificate", originCertPath)
	}
	return originCert, nil
}

func prepareTunnelConfig(
	c *cli.Context,
	buildInfo *origin.BuildInfo,
	version string, logger,
	transportLogger *logrus.Logger,
) (*origin.TunnelConfig, error) {
	hostname, err := validation.ValidateHostname(c.String("hostname"))
	if err != nil {
		logger.WithError(err).Error("Invalid hostname")
		return nil, errors.Wrap(err, "Invalid hostname")
	}
	isFreeTunnel := hostname == ""
	clientID := c.String("id")
	if !c.IsSet("id") {
		clientID = generateRandomClientID()
	}

	tags, err := NewTagSliceFromCLI(c.StringSlice("tag"))
	if err != nil {
		logger.WithError(err).Error("Tag parse failure")
		return nil, errors.Wrap(err, "Tag parse failure")
	}

	tags = append(tags, tunnelpogs.Tag{Name: "ID", Value: clientID})

	originURL, err := config.ValidateUrl(c)
	if err != nil {
		logger.WithError(err).Error("Error validating origin URL")
		return nil, errors.Wrap(err, "Error validating origin URL")
	}

	var originCert []byte
	if !isFreeTunnel {
		originCert, err = getOriginCert(c)
		if err != nil {
			return nil, errors.Wrap(err, "Error getting origin cert")
		}
	}

	originCertPool, err := loadCertPool(c, logger)
	if err != nil {
		logger.WithError(err).Error("Error loading cert pool")
		return nil, errors.Wrap(err, "Error loading cert pool")
	}

	tunnelMetrics := origin.NewTunnelMetrics()
	httpTransport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          c.Int("proxy-keepalive-connections"),
		IdleConnTimeout:       c.Duration("proxy-keepalive-timeout"),
		TLSHandshakeTimeout:   c.Duration("proxy-tls-timeout"),
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{RootCAs: originCertPool, InsecureSkipVerify: c.IsSet("no-tls-verify")},
	}

	dialContext := (&net.Dialer{
		Timeout:   c.Duration("proxy-connect-timeout"),
		KeepAlive: c.Duration("proxy-tcp-keepalive"),
		DualStack: !c.Bool("proxy-no-happy-eyeballs"),
	}).DialContext

	if c.IsSet("unix-socket") {
		unixSocket, err := config.ValidateUnixSocket(c)
		if err != nil {
			logger.WithError(err).Error("Error validating --unix-socket")
			return nil, errors.Wrap(err, "Error validating --unix-socket")
		}

		logger.Infof("Proxying tunnel requests to unix:%s", unixSocket)
		httpTransport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			// if --unix-socket specified, enforce network type "unix"
			return dialContext(ctx, "unix", unixSocket)
		}
	} else {
		logger.Infof("Proxying tunnel requests to %s", originURL)
		httpTransport.DialContext = dialContext
	}

	if !c.IsSet("hello-world") && c.IsSet("origin-server-name") {
		httpTransport.TLSClientConfig.ServerName = c.String("origin-server-name")
	}

	err = validation.ValidateHTTPService(originURL, hostname, httpTransport)
	if err != nil {
		logger.WithError(err).Error("unable to connect to the origin")
		return nil, errors.Wrap(err, "unable to connect to the origin")
	}

	toEdgeTLSConfig, err := createTunnelConfig(c)
	if err != nil {
		logger.WithError(err).Error("unable to create TLS config to connect with edge")
		return nil, errors.Wrap(err, "unable to create TLS config to connect with edge")
	}

	return &origin.TunnelConfig{
		EdgeAddrs:          c.StringSlice("edge"),
		OriginUrl:          originURL,
		Hostname:           hostname,
		OriginCert:         originCert,
		TlsConfig:          toEdgeTLSConfig,
		ClientTlsConfig:    httpTransport.TLSClientConfig,
		Retries:            c.Uint("retries"),
		HeartbeatInterval:  c.Duration("heartbeat-interval"),
		MaxHeartbeats:      c.Uint64("heartbeat-count"),
		ClientID:           clientID,
		BuildInfo:          buildInfo,
		ReportedVersion:    version,
		LBPool:             c.String("lb-pool"),
		Tags:               tags,
		HAConnections:      c.Int("ha-connections"),
		HTTPTransport:      httpTransport,
		Metrics:            tunnelMetrics,
		MetricsUpdateFreq:  c.Duration("metrics-update-freq"),
		TransportLogger:    transportLogger,
		Logger:             logger,
		IsAutoupdated:      c.Bool("is-autoupdated"),
		GracePeriod:        c.Duration("grace-period"),
		RunFromTerminal:    isRunningFromTerminal(),
		NoChunkedEncoding:  c.Bool("no-chunked-encoding"),
		CompressionQuality: c.Uint64("compression-quality"),
		IncidentLookup:     origin.NewIncidentLookup(),
		IsFreeTunnel:       isFreeTunnel,
	}, nil
}

func loadCertPool(c *cli.Context, logger *logrus.Logger) (*x509.CertPool, error) {
	const originCAPoolFlag = "origin-ca-pool"
	originCAPoolFilename := c.String(originCAPoolFlag)
	var originCustomCAPool []byte

	if originCAPoolFilename != "" {
		var err error
		originCustomCAPool, err = ioutil.ReadFile(originCAPoolFilename)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("unable to read the file %s for --%s", originCAPoolFilename, originCAPoolFlag))
		}
	}

	originCertPool, err := loadOriginCertPool(originCustomCAPool)
	if err != nil {
		return nil, errors.Wrap(err, "error loading the certificate pool")
	}

	// Windows users should be notified that they can use the flag
	if runtime.GOOS == "windows" && originCAPoolFilename == "" {
		logger.Infof("cloudflared does not support loading the system root certificate pool on Windows. Please use the --%s to specify it", originCAPoolFlag)
	}

	return originCertPool, nil
}

func loadOriginCertPool(originCAPoolPEM []byte) (*x509.CertPool, error) {
	// Get the global pool
	certPool, err := loadGlobalCertPool()
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

func loadGlobalCertPool() (*x509.CertPool, error) {
	// First, obtain the system certificate pool
	certPool, err := x509.SystemCertPool()
	if err != nil {
		if runtime.GOOS != "windows" {
			logger.WithError(err).Warn("error obtaining the system certificates")
		}
		certPool = x509.NewCertPool()
	}

	// Next, append the Cloudflare CAs into the system pool
	cfRootCA, err := tlsconfig.GetCloudflareRootCA()
	if err != nil {
		return nil, errors.Wrap(err, "could not append Cloudflare Root CAs to cloudflared certificate pool")
	}
	for _, cert := range cfRootCA {
		certPool.AddCert(cert)
	}

	// Finally, add the Hello certificate into the pool (since it's self-signed)
	helloCert, err := tlsconfig.GetHelloCertificateX509()
	if err != nil {
		return nil, errors.Wrap(err, "could not append Hello server certificate to cloudflared certificate pool")
	}
	certPool.AddCert(helloCert)

	return certPool, nil
}

func createTunnelConfig(c *cli.Context) (*tls.Config, error) {
	var rootCAs []string
	if c.String("cacert") != "" {
		rootCAs = append(rootCAs, c.String("cacert"))
	}
	edgeAddrs := c.StringSlice("edge")

	userConfig := &tlsconfig.TLSParameters{RootCAs: rootCAs}
	tlsConfig, err := tlsconfig.GetConfig(userConfig)
	if err != nil {
		return nil, err
	}
	if tlsConfig.RootCAs == nil {
		rootCAPool := x509.NewCertPool()
		cfRootCA, err := tlsconfig.GetCloudflareRootCA()
		if err != nil {
			return nil, errors.Wrap(err, "could not append Cloudflare Root CAs to cloudflared certificate pool")
		}
		for _, cert := range cfRootCA {
			rootCAPool.AddCert(cert)
		}
		tlsConfig.RootCAs = rootCAPool
		tlsConfig.ServerName = "cftunnel.com"
	} else if len(edgeAddrs) > 0 {
		// Set for development environments and for testing specific origintunneld instances
		tlsConfig.ServerName, _, _ = net.SplitHostPort(edgeAddrs[0])
	}

	if tlsConfig.ServerName == "" && !tlsConfig.InsecureSkipVerify {
		return nil, fmt.Errorf("either ServerName or InsecureSkipVerify must be specified in the tls.Config")
	}
	return tlsConfig, nil
}

func isRunningFromTerminal() bool {
	return terminal.IsTerminal(int(os.Stdout.Fd()))
}
