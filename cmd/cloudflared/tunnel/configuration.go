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
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/origin"
	"github.com/cloudflare/cloudflared/tlsconfig"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/validation"

	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	"golang.org/x/crypto/ssh/terminal"
)

var (
	developerPortal = "https://developers.cloudflare.com/argo-tunnel"
	quickStartUrl   = developerPortal + "/quickstart/quickstart/"
	serviceUrl      = developerPortal + "/reference/service/"
	argumentsUrl    = developerPortal + "/reference/arguments/"
)

// returns the first path that contains a cert.pem file. If none of the DefaultConfigSearchDirectories
// contains a cert.pem file, return empty string
func findDefaultOriginCertPath() string {
	for _, defaultConfigDir := range config.DefaultConfigSearchDirectories() {
		originCertPath, _ := homedir.Expand(filepath.Join(defaultConfigDir, config.DefaultCredentialFile))
		if ok, _ := config.FileExists(originCertPath); ok {
			return originCertPath
		}
	}
	return ""
}

func generateRandomClientID(logger logger.Service) (string, error) {
	u, err := uuid.NewRandom()
	if err != nil {
		logger.Errorf("couldn't create UUID for client ID %s", err)
		return "", err
	}
	return u.String(), nil
}

func logClientOptions(c *cli.Context, logger logger.Service) {
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
		logger.Infof("Environment variables %v", flags)
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

func findOriginCert(c *cli.Context, logger logger.Service) (string, error) {
	originCertPath := c.String("origincert")
	if originCertPath == "" {
		logger.Infof(config.ErrNoConfigFile.Error())
		if isRunningFromTerminal() {
			logger.Errorf("You need to specify the origin certificate path with --origincert option, or set TUNNEL_ORIGIN_CERT environment variable. See %s for more information.", argumentsUrl)
			return "", fmt.Errorf("Client didn't specify origincert path when running from terminal")
		} else {
			logger.Errorf("You need to specify the origin certificate path by specifying the origincert option in the configuration file, or set TUNNEL_ORIGIN_CERT environment variable. See %s for more information.", serviceUrl)
			return "", fmt.Errorf("Client didn't specify origincert path")
		}
	}
	var err error
	originCertPath, err = homedir.Expand(originCertPath)
	if err != nil {
		logger.Errorf("Cannot resolve path %s: %s", originCertPath, err)
		return "", fmt.Errorf("Cannot resolve path %s", originCertPath)
	}
	// Check that the user has acquired a certificate using the login command
	ok, err := config.FileExists(originCertPath)
	if err != nil {
		logger.Errorf("Cannot check if origin cert exists at path %s", originCertPath)
		return "", fmt.Errorf("Cannot check if origin cert exists at path %s", originCertPath)
	}
	if !ok {
		logger.Errorf(`Cannot find a valid certificate for your origin at the path:

    %s

If the path above is wrong, specify the path with the -origincert option.
If you don't have a certificate signed by Cloudflare, run the command:

	%s login
`, originCertPath, os.Args[0])
		return "", fmt.Errorf("Cannot find a valid certificate at the path %s", originCertPath)
	}

	return originCertPath, nil
}

func readOriginCert(originCertPath string, logger logger.Service) ([]byte, error) {
	logger.Debugf("Reading origin cert from %s", originCertPath)

	// Easier to send the certificate as []byte via RPC than decoding it at this point
	originCert, err := ioutil.ReadFile(originCertPath)
	if err != nil {
		logger.Errorf("Cannot read %s to load origin certificate: %s", originCertPath, err)
		return nil, fmt.Errorf("Cannot read %s to load origin certificate", originCertPath)
	}
	return originCert, nil
}

func getOriginCert(c *cli.Context, logger logger.Service) ([]byte, error) {
	if originCertPath, err := findOriginCert(c, logger); err != nil {
		return nil, err
	} else {
		return readOriginCert(originCertPath, logger)
	}
}

func prepareTunnelConfig(
	c *cli.Context,
	buildInfo *buildinfo.BuildInfo,
	version string,
	logger logger.Service,
	transportLogger logger.Service,
	namedTunnel *origin.NamedTunnelConfig,
) (*origin.TunnelConfig, error) {
	compatibilityMode := namedTunnel == nil

	hostname, err := validation.ValidateHostname(c.String("hostname"))
	if err != nil {
		logger.Errorf("Invalid hostname: %s", err)
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
		logger.Errorf("Tag parse failure: %s", err)
		return nil, errors.Wrap(err, "Tag parse failure")
	}

	tags = append(tags, tunnelpogs.Tag{Name: "ID", Value: clientID})

	var originCert []byte
	if !isFreeTunnel {
		originCert, err = getOriginCert(c, logger)
		if err != nil {
			return nil, errors.Wrap(err, "Error getting origin cert")
		}
	}

	originCertPool, err := tlsconfig.LoadOriginCA(c, logger)
	if err != nil {
		logger.Errorf("Error loading cert pool: %s", err)
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

	var ingressRules []ingress.Rule
	if namedTunnel != nil {
		clientUUID, err := uuid.NewRandom()
		if err != nil {
			return nil, errors.Wrap(err, "can't generate clientUUID")
		}
		namedTunnel.Client = tunnelpogs.ClientInfo{
			ClientID: clientUUID[:],
			Features: []string{origin.FeatureSerializedHeaders},
			Version:  version,
			Arch:     fmt.Sprintf("%s_%s", buildInfo.GoOS, buildInfo.GoArch),
		}
		ingressRules, err = config.ReadRules(c)
		if err != nil && err != ingress.ErrNoIngressRules {
			return nil, err
		}
		if len(ingressRules) > 0 && c.IsSet("url") {
			return nil, ingress.ErrURLIncompatibleWithIngress
		}
	}

	var originURL string
	isUsingMultipleOrigins := len(ingressRules) > 0
	if !isUsingMultipleOrigins {
		originURL, err = config.ValidateUrl(c, compatibilityMode)
		if err != nil {
			logger.Errorf("Error validating origin URL: %s", err)
			return nil, errors.Wrap(err, "Error validating origin URL")
		}
	}

	if c.IsSet("unix-socket") {
		unixSocket, err := config.ValidateUnixSocket(c)
		if err != nil {
			logger.Errorf("Error validating --unix-socket: %s", err)
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
	// If tunnel running in bastion mode, a connection to origin will not exist until initiated by the client.
	if !c.IsSet(bastionFlag) {

		// List all origin URLs that require validation
		var originURLs []string
		if !isUsingMultipleOrigins {
			originURLs = append(originURLs, originURL)
		} else {
			for _, rule := range ingressRules {
				originURLs = append(originURLs, rule.Service.String())
			}
		}

		// Validate each origin URL
		for _, u := range originURLs {
			if err = validation.ValidateHTTPService(u, hostname, httpTransport); err != nil {
				logger.Errorf("unable to connect to the origin: %s", err)
			}
		}
	}

	toEdgeTLSConfig, err := tlsconfig.CreateTunnelConfig(c)
	if err != nil {
		logger.Errorf("unable to create TLS config to connect with edge: %s", err)
		return nil, errors.Wrap(err, "unable to create TLS config to connect with edge")
	}
	return &origin.TunnelConfig{
		BuildInfo:          buildInfo,
		ClientID:           clientID,
		ClientTlsConfig:    httpTransport.TLSClientConfig,
		CompressionQuality: c.Uint64("compression-quality"),
		EdgeAddrs:          c.StringSlice("edge"),
		GracePeriod:        c.Duration("grace-period"),
		HAConnections:      c.Int("ha-connections"),
		HTTPTransport:      httpTransport,
		HeartbeatInterval:  c.Duration("heartbeat-interval"),
		Hostname:           hostname,
		HTTPHostHeader:     c.String("http-host-header"),
		IncidentLookup:     origin.NewIncidentLookup(),
		IsAutoupdated:      c.Bool("is-autoupdated"),
		IsFreeTunnel:       isFreeTunnel,
		LBPool:             c.String("lb-pool"),
		Logger:             logger,
		TransportLogger:    transportLogger,
		MaxHeartbeats:      c.Uint64("heartbeat-count"),
		Metrics:            tunnelMetrics,
		MetricsUpdateFreq:  c.Duration("metrics-update-freq"),
		NoChunkedEncoding:  c.Bool("no-chunked-encoding"),
		OriginCert:         originCert,
		OriginUrl:          originURL,
		ReportedVersion:    version,
		Retries:            c.Uint("retries"),
		RunFromTerminal:    isRunningFromTerminal(),
		Tags:               tags,
		TlsConfig:          toEdgeTLSConfig,
		NamedTunnel:        namedTunnel,
		ReplaceExisting:    c.Bool("force"),
		IngressRules:       ingressRules,
		// turn off use of reconnect token and auth refresh when using named tunnels
		UseReconnectToken: compatibilityMode && c.Bool("use-reconnect-token"),
	}, nil
}

func isRunningFromTerminal() bool {
	return terminal.IsTerminal(int(os.Stdout.Fd()))
}
