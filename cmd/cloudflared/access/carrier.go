package access

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudflare/cloudflared/carrier"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/validation"
	"github.com/pkg/errors"
	cli "gopkg.in/urfave/cli.v2"
)

// ssh will start a WS proxy server for server mode
// or copy from stdin/stdout for client mode
// useful for proxying other protocols (like ssh) over websockets
// (which you can put Access in front of)
func ssh(c *cli.Context) error {
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
		headers.Add("CF-Access-Client-Id", c.String(sshTokenIDFlag))
	}
	if c.IsSet(sshTokenSecretFlag) {
		headers.Add("CF-Access-Client-Secret", c.String(sshTokenSecretFlag))
	}

	destination := c.String(sshDestinationFlag)
	if destination != "" {
		headers.Add("CF-Access-SSH-Destination", destination)
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
			logger.WithError(err).Error("Error validating origin URL")
			return errors.Wrap(err, "error validating origin URL")
		}
		forwarder, err := url.Parse(localForwarder)
		if err != nil {
			logger.WithError(err).Error("Error validating origin URL")
			return errors.Wrap(err, "error validating origin URL")
		}

		logger.Infof("Start Websocket listener on: %s", forwarder.Host)
		return carrier.StartForwarder(wsConn, forwarder.Host, shutdownC, options)
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
