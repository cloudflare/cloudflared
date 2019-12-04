package tunnel

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/buildinfo"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/origin"
	"github.com/cloudflare/cloudflared/tlsconfig"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/validation"

	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/urfave/cli.v2"
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

func generateRandomClientID(logger *logrus.Logger) (string, error) {
	u, err := uuid.NewRandom()
	if err != nil {
		logger.WithError(err).Error("couldn't create UUID for client ID")
		return "", err
	}
	return u.String(), nil
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
	buildInfo *buildinfo.BuildInfo,
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
		clientID, err = generateRandomClientID(logger)
		if err != nil {
			return nil, err
		}
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

	originCertPool, err := tlsconfig.LoadOriginCA(c, logger)
	if err != nil {
		logger.WithError(err).Error("Error loading cert pool")
		return nil, errors.Wrap(err, "Error loading cert pool")
	}

	tunnelMetrics := origin.NewTunnelMetrics()
	httpTransport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          c.Int("proxy-keepalive-connections"),
		MaxIdleConnsPerHost:   c.Int("proxy-keepalive-connections"),
		IdleConnTimeout:       c.Duration("proxy-keepalive-timeout"),
		TLSHandshakeTimeout:   c.Duration("proxy-tls-timeout"),
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{RootCAs: originCertPool, InsecureSkipVerify: c.IsSet("no-tls-verify")},
	}

	dialer := &net.Dialer{
		Timeout:   c.Duration("proxy-connect-timeout"),
		KeepAlive: c.Duration("proxy-tcp-keepalive"),
	}
	if c.Bool("proxy-no-happy-eyeballs") {
		dialer.FallbackDelay = -1 // As of Golang 1.12, a negative delay disables "happy eyeballs"
	}
	dialContext := dialer.DialContext

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

	toEdgeTLSConfig, err := tlsconfig.CreateTunnelConfig(c)
	if err != nil {
		logger.WithError(err).Error("unable to create TLS config to connect with edge")
		return nil, errors.Wrap(err, "unable to create TLS config to connect with edge")
	}

	return &origin.TunnelConfig{
		BuildInfo:            buildInfo,
		ClientID:             clientID,
		ClientTlsConfig:      httpTransport.TLSClientConfig,
		CompressionQuality:   c.Uint64("compression-quality"),
		EdgeAddrs:            c.StringSlice("edge"),
		GracePeriod:          c.Duration("grace-period"),
		HAConnections:        c.Int("ha-connections"),
		HTTPTransport:        httpTransport,
		HeartbeatInterval:    c.Duration("heartbeat-interval"),
		Hostname:             hostname,
		HTTPHostHeader:       c.String("http-host-header"),
		IncidentLookup:       origin.NewIncidentLookup(),
		IsAutoupdated:        c.Bool("is-autoupdated"),
		IsFreeTunnel:         isFreeTunnel,
		LBPool:               c.String("lb-pool"),
		Logger:               logger,
		MaxHeartbeats:        c.Uint64("heartbeat-count"),
		Metrics:              tunnelMetrics,
		MetricsUpdateFreq:    c.Duration("metrics-update-freq"),
		NoChunkedEncoding:    c.Bool("no-chunked-encoding"),
		OriginCert:           originCert,
		OriginUrl:            originURL,
		ReportedVersion:      version,
		Retries:              c.Uint("retries"),
		RunFromTerminal:      isRunningFromTerminal(),
		Tags:                 tags,
		TlsConfig:            toEdgeTLSConfig,
		TransportLogger:      transportLogger,
		UseDeclarativeTunnel: c.Bool("use-declarative-tunnels"),
	}, nil
}

func serviceDiscoverer(c *cli.Context, logger *logrus.Logger) (connection.EdgeServiceDiscoverer, error) {
	// If --edge is specfied, resolve edge server addresses
	if len(c.StringSlice("edge")) > 0 {
		return connection.NewEdgeHostnameResolver(c.StringSlice("edge"))
	}
	// Otherwise lookup edge server addresses through service discovery
	return connection.NewEdgeAddrResolver(logger)
}

func isRunningFromTerminal() bool {
	return terminal.IsTerminal(int(os.Stdout.Fd()))
}
