package access

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"

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

	// get the hostname from the cmdline and error out if its not provided
	rawHostName := c.String(sshHostnameFlag)
	hostname, err := validation.ValidateHostname(rawHostName)
	if err != nil || rawHostName == "" {
		return cli.ShowCommandHelp(c, "ssh")
	}
	originURL := ensureURLScheme(hostname)

	// get the headers from the cmdline and add them
	headers := buildRequestHeaders(c.StringSlice(sshHeaderFlag))
	if c.IsSet(sshTokenIDFlag) {
		headers.Set(cfAccessClientIDHeader, c.String(sshTokenIDFlag))
	}
	if c.IsSet(sshTokenSecretFlag) {
		headers.Set(cfAccessClientSecretHeader, c.String(sshTokenSecretFlag))
	}
	headers.Set("User-Agent", userAgent)

	carrier.SetBastionDest(headers, c.String(sshDestinationFlag))

	options := &carrier.StartOptions{
		OriginURL: originURL,
		Headers:   headers,
		Host:      hostname,
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
				InsecureSkipVerify: true,
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
		logger := log.With().Str("host", hostname).Logger()
		s = stream.NewDebugStream(s, &logger, maxMessages)
	}
	carrier.StartClient(wsConn, s, options)
	return nil
}

func buildRequestHeaders(values []string) http.Header {
	headers := make(http.Header)
	for _, valuePair := range values {
		header, value, found := strings.Cut(valuePair, ":")
		if found {
			headers.Add(strings.TrimSpace(header), strings.TrimSpace(value))
		}
	}
	return headers
}
