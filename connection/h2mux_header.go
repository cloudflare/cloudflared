package connection

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/pkg/errors"

	"github.com/cloudflare/cloudflared/h2mux"
)

// H2RequestHeadersToH1Request converts the HTTP/2 headers coming from origintunneld
// to an HTTP/1 Request object destined for the local origin web service.
// This operation includes conversion of the pseudo-headers into their closest
// HTTP/1 equivalents. See https://tools.ietf.org/html/rfc7540#section-8.1.2.3
func H2RequestHeadersToH1Request(h2 []h2mux.Header, h1 *http.Request) error {
	for _, header := range h2 {
		name := strings.ToLower(header.Name)
		if !IsH2muxControlRequestHeader(name) {
			continue
		}

		switch name {
		case ":method":
			h1.Method = header.Value
		case ":scheme":
			// noop - use the preexisting scheme from h1.URL
		case ":authority":
			// Otherwise the host header will be based on the origin URL
			h1.Host = header.Value
		case ":path":
			// We don't want to be an "opinionated" proxy, so ideally we would use :path as-is.
			// However, this HTTP/1 Request object belongs to the Go standard library,
			// whose URL package makes some opinionated decisions about the encoding of
			// URL characters: see the docs of https://godoc.org/net/url#URL,
			// in particular the EscapedPath method https://godoc.org/net/url#URL.EscapedPath,
			// which is always used when computing url.URL.String(), whether we'd like it or not.
			//
			// Well, not *always*. We could circumvent this by using url.URL.Opaque. But
			// that would present unusual difficulties when using an HTTP proxy: url.URL.Opaque
			// is treated differently when HTTP_PROXY is set!
			// See https://github.com/golang/go/issues/5684#issuecomment-66080888
			//
			// This means we are subject to the behavior of net/url's function `shouldEscape`
			// (as invoked with mode=encodePath): https://github.com/golang/go/blob/go1.12.7/src/net/url/url.go#L101

			if header.Value == "*" {
				h1.URL.Path = "*"
				continue
			}
			// Due to the behavior of validation.ValidateUrl, h1.URL may
			// already have a partial value, with or without a trailing slash.
			base := h1.URL.String()
			base = strings.TrimRight(base, "/")
			// But we know :path begins with '/', because we handled '*' above - see RFC7540
			requestURL, err := url.Parse(base + header.Value)
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("invalid path '%v'", header.Value))
			}
			h1.URL = requestURL
		case "content-length":
			contentLength, err := strconv.ParseInt(header.Value, 10, 64)
			if err != nil {
				return fmt.Errorf("unparseable content length")
			}
			h1.ContentLength = contentLength
		case RequestUserHeaders:
			// Do not forward the serialized headers to the origin -- deserialize them, and ditch the serialized version
			// Find and parse user headers serialized into a single one
			userHeaders, err := DeserializeHeaders(header.Value)
			if err != nil {
				return errors.Wrap(err, "Unable to parse user headers")
			}
			for _, userHeader := range userHeaders {
				h1.Header.Add(userHeader.Name, userHeader.Value)
			}
		default:
			// All other control headers shall just be proxied transparently
			h1.Header.Add(header.Name, header.Value)
		}
	}

	return nil
}

func H1ResponseToH2ResponseHeaders(status int, h1 http.Header) (h2 []h2mux.Header) {
	h2 = []h2mux.Header{
		{Name: ":status", Value: strconv.Itoa(status)},
	}
	userHeaders := make(http.Header, len(h1))
	for header, values := range h1 {
		h2name := strings.ToLower(header)
		if h2name == "content-length" {
			// This header has meaning in HTTP/2 and will be used by the edge,
			// so it should be sent as an HTTP/2 response header.

			// Since these are http2 headers, they're required to be lowercase
			h2 = append(h2, h2mux.Header{Name: "content-length", Value: values[0]})
		} else if !IsH2muxControlResponseHeader(h2name) || IsWebsocketClientHeader(h2name) {
			// User headers, on the other hand, must all be serialized so that
			// HTTP/2 header validation won't be applied to HTTP/1 header values
			userHeaders[header] = values
		}
	}

	// Perform user header serialization and set them in the single header
	h2 = append(h2, h2mux.Header{Name: ResponseUserHeaders, Value: SerializeHeaders(userHeaders)})
	return h2
}

// IsH2muxControlRequestHeader is called in the direction of eyeball -> origin.
func IsH2muxControlRequestHeader(headerName string) bool {
	return headerName == "content-length" ||
		headerName == "connection" || headerName == "upgrade" || // Websocket request headers
		strings.HasPrefix(headerName, ":") ||
		strings.HasPrefix(headerName, "cf-")
}

// IsH2muxControlResponseHeader is called in the direction of eyeball <- origin.
func IsH2muxControlResponseHeader(headerName string) bool {
	return headerName == "content-length" ||
		strings.HasPrefix(headerName, ":") ||
		strings.HasPrefix(headerName, "cf-int-") ||
		strings.HasPrefix(headerName, "cf-cloudflared-")
}
