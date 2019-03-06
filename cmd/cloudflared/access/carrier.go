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
	hostname, err := validation.ValidateHostname(c.String("hostname"))
	if err != nil || c.String("hostname") == "" {
		return cli.ShowCommandHelp(c, "ssh")
	}
	headers := buildRequestHeaders(c.StringSlice("header"))
	if c.IsSet("service-token-id") {
		headers.Add("CF-Access-Client-Id", c.String("service-token-id"))
	}
	if c.IsSet("service-token-secret") {
		headers.Add("CF-Access-Client-Secret", c.String("service-token-secret"))
	}

	if c.NArg() > 0 || c.IsSet("url") {
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
		return carrier.StartServer(logger, forwarder.Host, "https://"+hostname, shutdownC, headers)
	}

	return carrier.StartClient(logger, "https://"+hostname, &carrier.StdinoutStream{}, headers)
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
