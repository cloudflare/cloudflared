package warp

import (
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/cloudflare/cloudflare-warp/origin"
	"github.com/cloudflare/cloudflare-warp/tlsconfig"
	tunnelpogs "github.com/cloudflare/cloudflare-warp/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflare-warp/validation"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/sirupsen/logrus"
)

// StartServer starts a warp proxy server with the given configuration.
// It blocks indefinitely.
func StartServer(cfg ServerConfig) error {
	hostname, err := validation.ValidateHostname(cfg.Hostname)
	if err != nil {
		return err
	}
	if cfg.ClientID == "" {
		cfg.ClientID = generateRandomClientID()
	}

	cfg.Tags = append(cfg.Tags, tunnelpogs.Tag{Name: "ID", Value: cfg.ClientID})

	cfg.ServerURL, err = validation.ValidateUrl(cfg.ServerURL)
	if err != nil {
		return fmt.Errorf("validating server URL: %v", err)
	}

	// Check that the user has acquired a certificate using the log in command
	originCertPath, err := homedir.Expand(cfg.OriginCert)
	if err != nil {
		return fmt.Errorf("cannot resolve path %s: %v", cfg.OriginCert, err)
	}
	ok, err := fileExists(originCertPath)
	if !ok {
		return fmt.Errorf(`Cannot find a valid certificate for your origin at the path:

    %s

If the path above is wrong, specify the path with the -origincert option.
If you don't have a certificate signed by Cloudflare, run the command:

    %s login
`, originCertPath, os.Args[0]) // TODO - we need to improve how this is handled
	}
	// Easier to send the certificate as []byte via RPC than decoding it at this point
	originCert, err := ioutil.ReadFile(originCertPath)
	if err != nil {
		return fmt.Errorf("cannot read %s to load origin certificate: %v", originCertPath, err)
	}

	tunnelMetrics := origin.NewTunnelMetrics()

	httpTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   cfg.Timeout,
			KeepAlive: cfg.KeepAlive,
			DualStack: cfg.DualStack,
		}).DialContext,
		MaxIdleConns:          cfg.MaxIdleConns,
		IdleConnTimeout:       cfg.IdleConnTimeout,
		TLSHandshakeTimeout:   cfg.TLSHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{RootCAs: tlsconfig.LoadOriginCertsPool()},
	}

	tunnelConfig := &origin.TunnelConfig{
		EdgeAddrs:         cfg.EdgeAddrs,
		OriginUrl:         cfg.ServerURL,
		Hostname:          hostname,
		OriginCert:        originCert,
		TlsConfig:         tlsconfig.CreateTunnelConfig(cfg.TLSConfig, cfg.EdgeAddrs),
		ClientTlsConfig:   httpTransport.TLSClientConfig,
		Retries:           cfg.Retries,
		HeartbeatInterval: cfg.HeartbeatInterval,
		MaxHeartbeats:     cfg.MaxHeartbeats,
		ClientID:          cfg.ClientID,
		ReportedVersion:   cfg.ReportedVersion,
		LBPool:            cfg.LBPool,
		Tags:              cfg.Tags,
		HAConnections:     cfg.HAConnections,
		HTTPTransport:     httpTransport,
		Metrics:           tunnelMetrics,
		MetricsUpdateFreq: cfg.MetricsUpdateFreq,
		ProtocolLogger:    cfg.ProtoLogger,
		Logger:            cfg.Logger,
		IsAutoupdated:     cfg.IsAutoupdated,
	}

	// blocking
	return origin.StartTunnelDaemon(tunnelConfig, cfg.ShutdownChan, cfg.ConnectedChan)
}

func fileExists(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// ignore missing files
			return false, nil
		}
		return false, err
	}
	f.Close()
	return true, nil
}

func generateRandomClientID() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	id := make([]byte, 32)
	r.Read(id)
	return hex.EncodeToString(id)
}

// ServerConfig specifies a warp proxy-server configuration.
type ServerConfig struct {
	// The hostname on a Cloudflare zone with which route
	// traffic through this tunnel.
	// Required.
	Hostname string

	// The URL of the local web server.
	// Required.
	ServerURL string

	// The tunnel ID; leave blank to use a random ID.
	ClientID string

	// Custom tags to identify this tunnel
	Tags []tunnelpogs.Tag

	// Specifies the Warp certificate for one of your zones,
	// authorizing the client to serve as an origin for that zone.
	// A certificate is required to use Warp. You can obtain a
	// certificate by using the login command or by visiting
	// https://www.cloudflare.com/a/warp.
	OriginCert string

	// The channel to close when the tunnel is connected.
	ConnectedChan chan struct{}

	// The channel to close when shutting down.
	ShutdownChan chan struct{}

	Timeout   time.Duration // proxy-connect-timeout
	KeepAlive time.Duration // proxy-tcp-keepalive
	DualStack bool          // proxy-no-happy-eyeballs

	MaxIdleConns        int           // proxy-keepalive-connections
	IdleConnTimeout     time.Duration // proxy-keepalive-timeout
	TLSHandshakeTimeout time.Duration // proxy-tls-timeout

	EdgeAddrs         []string      // edge
	Retries           uint          // retries
	HeartbeatInterval time.Duration // heartbeat-interval
	MaxHeartbeats     uint64        // heartbeat-count
	LBPool            string        // lb-pool
	HAConnections     int           // ha-connections
	MetricsUpdateFreq time.Duration // metrics-update-freq
	IsAutoupdated     bool          // is-autoupdated

	// The TLS client config used when making the tunnel.
	// If not set, a sane default config will be created.
	TLSConfig *tls.Config

	// The version of the client to report
	ReportedVersion string

	ProtoLogger *logrus.Logger
	Logger      *logrus.Logger
}
