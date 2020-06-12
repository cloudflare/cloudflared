package access

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudflare/cloudflared/carrier"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/validation"
	"github.com/pkg/errors"
	cli "gopkg.in/urfave/cli.v2"
)

// StartForwarder starts a client side websocket forward
func StartForwarder(forwarder config.Forwarder, shutdown <-chan struct{}, logger logger.Service) error {
	validURLString, err := validation.ValidateUrl(forwarder.Listener)
	if err != nil {
		return errors.Wrap(err, "error validating origin URL")
	}

	validURL, err := url.Parse(validURLString)
	if err != nil {
		return errors.Wrap(err, "error parsing origin URL")
	}

	// get the headers from the config file and add to the request
	headers := make(http.Header)
	if forwarder.TokenClientID != "" {
		headers.Set(h2mux.CFAccessClientIDHeader, forwarder.TokenClientID)
	}

	if forwarder.TokenSecret != "" {
		headers.Set(h2mux.CFAccessClientSecretHeader, forwarder.TokenSecret)
	}

	options := &carrier.StartOptions{
		OriginURL: forwarder.URL,
		Headers:   headers, //TODO: TUN-2688 support custom headers from config file
	}

	// we could add a cmd line variable for this bool if we want the SOCK5 server to be on the client side
	wsConn := carrier.NewWSConnection(logger, false)

	logger.Infof("Start Websocket listener on: %s", validURL.Host)
	return carrier.StartForwarder(wsConn, validURL.Host, shutdown, options)
}

// ssh will start a WS proxy server for server mode
// or copy from stdin/stdout for client mode
// useful for proxying other protocols (like ssh) over websockets
// (which you can put Access in front of)
func ssh(c *cli.Context) error {
	logDirectory, logLevel := config.FindLogSettings()

	flagLogDirectory := c.String(sshLogDirectoryFlag)
	if flagLogDirectory != "" {
		logDirectory = flagLogDirectory
	}

	flagLogLevel := c.String(sshLogLevelFlag)
	if flagLogLevel != "" {
		logLevel = flagLogLevel
	}

	logger, err := logger.New(logger.DefaultFile(logDirectory), logger.LogLevelString(logLevel))
	if err != nil {
		return cliutil.PrintLoggerSetupError("error setting up logger", err)
	}

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
		headers.Set(h2mux.CFAccessClientIDHeader, c.String(sshTokenIDFlag))
	}
	if c.IsSet(sshTokenSecretFlag) {
		headers.Set(h2mux.CFAccessClientSecretHeader, c.String(sshTokenSecretFlag))
	}

	destination := c.String(sshDestinationFlag)
	if destination != "" {
		headers.Add(h2mux.CFJumpDestinationHeader, destination)
	}

	options := &carrier.StartOptions{
		OriginURL: originURL,
		Headers:   headers,
	}

	// we could add a cmd line variable for this bool if we want the SOCK5 server to be on the client side
	wsConn := carrier.NewWSConnection(logger, false)

	if c.NArg() > 0 || c.IsSet(sshURLFlag) {
		localForwarder, err := config.ValidateUrl(c)
		if err != nil {
			logger.Errorf("Error validating origin URL: %s", err)
			return errors.Wrap(err, "error validating origin URL")
		}
		forwarder, err := url.Parse(localForwarder)
		if err != nil {
			logger.Errorf("Error validating origin URL: %s", err)
			return errors.Wrap(err, "error validating origin URL")
		}

		logger.Infof("Start Websocket listener on: %s", forwarder.Host)
		err = carrier.StartForwarder(wsConn, forwarder.Host, shutdownC, options)
		if err != nil {
			logger.Errorf("Error on Websocket listener: %s", err)
		}
		return err
	}

	return carrier.StartClient(wsConn, &carrier.StdinoutStream{}, options)
}

func buildRequestHeaders(values []string) http.Header {
	headers := make(http.Header)
	for _, valuePair := range values {
		split := strings.Split(valuePair, ":")
		if len(split) > 1 {
			headers.Add(strings.TrimSpace(split[0]), strings.TrimSpace(split[1]))
		}
	}
	return headers
}
