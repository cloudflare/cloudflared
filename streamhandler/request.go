package streamhandler

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/pkg/errors"
)

const (
	lbProbeUserAgentPrefix = "Mozilla/5.0 (compatible; Cloudflare-Traffic-Manager/1.0; +https://www.cloudflare.com/traffic-manager/;"
)

func FindCfRayHeader(h1 *http.Request) string {
	return h1.Header.Get("Cf-Ray")
}

func IsLBProbeRequest(req *http.Request) bool {
	return strings.HasPrefix(req.UserAgent(), lbProbeUserAgentPrefix)
}

func createRequest(stream *h2mux.MuxedStream, url *url.URL) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url.String(), h2mux.MuxedStreamReader{MuxedStream: stream})
	if err != nil {
		return nil, errors.Wrap(err, "unexpected error from http.NewRequest")
	}
	err = h2mux.H2RequestHeadersToH1Request(stream.Headers, req)
	if err != nil {
		return nil, errors.Wrap(err, "invalid request received")
	}
	return req, nil
}
