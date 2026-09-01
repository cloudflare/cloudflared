package access

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/carrier"
	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/stream"
	"github.com/cloudflare/cloudflared/validation"
)

const (
	LogFieldHost               = "host"
	cfAccessClientIDHeader     = "Cf-Access-Client-Id"
	cfAccessClientSecretHeader = "Cf-Access-Client-Secret"
)

// StartForwarder starts a client side websocket forward
func StartForwarder(forwarder config.Forwarder, shutdown <-chan struct{}, log *zerolog.Logger) error {
	validURL, err := validation.ValidateUrl(forwarder.Listener)
	if err != nil {
		return errors.Wrap(err, "error validating origin URL")
	}

	// get the headers from the config file and add to the request
	headers := make(http.Header)
	if forwarder.TokenClientID != "" {
		headers.Set(cfAccessClientIDHeader, forwarder.TokenClientID)
	}

	if forwarder.TokenSecret != "" {
		headers.Set(cfAccessClientSecretHeader, forwarder.TokenSecret)
	}
	headers.Set("User-Agent", userAgent)

	carrier.SetBastionDest(headers, forwarder.Destination)

	options := &carrier.StartOptions{
		OriginURL: forwarder.URL,
		Headers:   headers, //TODO: TUN-2688 support custom headers from config file
		IsFedramp: forwarder.IsFedramp,
	}

	// we could add a cmd line variable for this bool if we want the SOCK5 server to be on the client side
	wsConn := carrier.NewWSConnection(log)

	log.Info().Str(LogFieldHost, validURL.Host).Msg("Start Websocket listener")
	return carrier.StartForwarder(wsConn, validURL.Host, shutdown, options)
}

// ssh will start a WS proxy server for server mode
// or copy from stdin/stdout for client mode
// useful for proxying other protocols (like ssh) over websockets
// (which you can put Access in front of)
func ssh(c *cli.Context) error {
	// If not running as a forwarder, disable terminal logs as it collides with the stdin/stdout of the parent process
	outputTerminal := logger.DisableTerminalLog
	if c.IsSet(sshURLFlag) {
		outputTerminal = logger.EnableTerminalLog
	}
	log := logger.CreateSSHLoggerFromContext(c, outputTerminal)

	if c.IsSet(sshPidFileFlag) {
		pidFile := c.String(sshPidFileFlag)
		writePidFile(pidFile, log)
		defer removePidFile(pidFile, log)

		// Trap SIGTERM/SIGINT to clean up the PID file before exiting.
		// Without this, signals kill the process before defers can run.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() {
			<-sigCh
			removePidFile(pidFile, log)
			signal.Reset(syscall.SIGTERM, syscall.SIGINT)
			// Re-raise so the process exits with the default signal behavior
			if p, err := os.FindProcess(os.Getpid()); err == nil {
				_ = p.Signal(syscall.SIGTERM)
			}
		}()
	}

	// get the hostname from the cmdline and error out if its not provided
	rawHostName := c.String(sshHostnameFlag)
	url, err := parseURL(rawHostName)
	if err != nil {
		log.Err(err).Send()
		return cli.ShowCommandHelp(c, "ssh")
	}

	// get the headers from the cmdline and add them
	headers := parseRequestHeaders(c.StringSlice(sshHeaderFlag))
	if c.IsSet(sshTokenIDFlag) {
		headers.Set(cfAccessClientIDHeader, c.String(sshTokenIDFlag))
	}
	if c.IsSet(sshTokenSecretFlag) {
		headers.Set(cfAccessClientSecretHeader, c.String(sshTokenSecretFlag))
	}
	headers.Set("User-Agent", userAgent)

	carrier.SetBastionDest(headers, c.String(sshDestinationFlag))

	options := &carrier.StartOptions{
		OriginURL: url.String(),
		Headers:   headers,
		Host:      url.Host,
		IsFedramp: c.Bool(fedrampFlag),
	}

	if connectTo := c.String(sshConnectTo); connectTo != "" {
		parts := strings.Split(connectTo, ":")
		switch len(parts) {
		case 1:
			options.OriginURL = fmt.Sprintf("https://%s", parts[0])
		case 2:
			options.OriginURL = fmt.Sprintf("https://%s:%s", parts[0], parts[1])
		case 3:
			options.OriginURL = fmt.Sprintf("https://%s:%s", parts[2], parts[1])
			options.TLSClientConfig = &tls.Config{
				InsecureSkipVerify: true, // #nosec G402
				ServerName:         parts[0],
			}
			log.Warn().Msgf("Using insecure SSL connection because SNI overridden to %s", parts[0])
		default:
			return fmt.Errorf("invalid connection override: %s", connectTo)
		}
	}

	// we could add a cmd line variable for this bool if we want the SOCK5 server to be on the client side
	wsConn := carrier.NewWSConnection(log)

	if c.NArg() > 0 || c.IsSet(sshURLFlag) {
		forwarder, err := config.ValidateUrl(c, true)
		if err != nil {
			log.Err(err).Msg("Error validating origin URL")
			return errors.Wrap(err, "error validating origin URL")
		}
		log.Info().Str(LogFieldHost, forwarder.Host).Msg("Start Websocket listener")
		err = carrier.StartForwarder(wsConn, forwarder.Host, shutdownC, options)
		if err != nil {
			log.Err(err).Msg("Error on Websocket listener")
		}
		return err
	}

	var s io.ReadWriter
	s = &carrier.StdinoutStream{}
	if c.IsSet(sshDebugStream) {
		maxMessages := c.Uint64(sshDebugStream)
		if maxMessages == 0 {
			// default to 10 if provided but unset
			maxMessages = 10
		}
		logger := log.With().Str("host", url.Host).Logger()
		s = stream.NewDebugStream(s, &logger, maxMessages)
	}
	return carrier.StartClient(wsConn, s, options)
}

// writePidFile writes the current process ID to a given file path.
// It expands ~ in paths using go-homedir.
func writePidFile(pidPathname string, log *zerolog.Logger) {
	expandedPath, err := homedir.Expand(pidPathname)
	if err != nil {
		log.Err(err).Str("pidPath", pidPathname).Msg("Unable to expand the path, try to use absolute path in --pidfile")
		return
	}
	file, err := os.Create(expandedPath)
	if err != nil {
		log.Err(err).Str("pidPath", expandedPath).Msg("Unable to write pid")
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "%d", os.Getpid())
}

// removePidFile removes the PID file at the given path.
// Errors are logged but do not cause a failure.
func removePidFile(pidPathname string, log *zerolog.Logger) {
	expandedPath, err := homedir.Expand(pidPathname)
	if err != nil {
		return
	}
	if err := os.Remove(expandedPath); err != nil && !os.IsNotExist(err) {
		log.Err(err).Str("pidPath", expandedPath).Msg("Unable to remove pid file")
	}
}
