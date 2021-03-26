package connection

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/pkg/errors"

	"github.com/cloudflare/cloudflared/h2mux"
)

var (
	// h2mux-style special headers
	RequestUserHeaders  = "cf-cloudflared-request-headers"
	ResponseUserHeaders = "cf-cloudflared-response-headers"
	ResponseMetaHeader  = "cf-cloudflared-response-meta"

	// h2mux-style special headers
	CanonicalResponseUserHeaders = http.CanonicalHeaderKey(ResponseUserHeaders)
	CanonicalResponseMetaHeader  = http.CanonicalHeaderKey(ResponseMetaHeader)
)

var (
	// pre-generate possible values for res
	responseMetaHeaderCfd    = mustInitRespMetaHeader("cloudflared")
	responseMetaHeaderOrigin = mustInitRespMetaHeader("origin")
)

type responseMetaHeader struct {
	Source string `json:"src"`
}

func mustInitRespMetaHeader(src string) string {
	header, err := json.Marshal(responseMetaHeader{Source: src})
	if err != nil {
		panic(fmt.Sprintf("Failed to serialize response meta header = %s, err: %v", src, err))
	}
	return string(header)
}

var headerEncoding = base64.RawStdEncoding

// note: all h2mux headers should be lower-case (http/2 style)
const ()

// H2RequestHeadersToH1Request converts the HTTP/2 headers coming from origintunneld
// to an HTTP/1 Request object destined for the local origin web service.
// This operation includes conversion of the pseudo-headers into their closest
// HTTP/1 equivalents. See https://tools.ietf.org/html/rfc7540#section-8.1.2.3
func H2RequestHeadersToH1Request(h2 []h2mux.Header, h1 *http.Request) error {
	for _, header := range h2 {
		name := strings.ToLower(header.Name)
		if !IsControlHeader(name) {
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

func IsControlHeader(headerName string) bool {
	return headerName == "content-length" ||
		headerName == "connection" || headerName == "upgrade" || // Websocket headers
		strings.HasPrefix(headerName, ":") ||
		strings.HasPrefix(headerName, "cf-")
}

// isWebsocketClientHeader returns true if the header name is required by the client to upgrade properly
func IsWebsocketClientHeader(headerName string) bool {
	return headerName == "sec-websocket-accept" ||
		headerName == "connection" ||
		headerName == "upgrade"
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
		} else if !IsControlHeader(h2name) || IsWebsocketClientHeader(h2name) {
			// User headers, on the other hand, must all be serialized so that
			// HTTP/2 header validation won't be applied to HTTP/1 header values
			userHeaders[header] = values
		}
	}

	// Perform user header serialization and set them in the single header
	h2 = append(h2, h2mux.Header{Name: ResponseUserHeaders, Value: SerializeHeaders(userHeaders)})
	return h2
}

// Serialize HTTP1.x headers by base64-encoding each header name and value,
// and then joining them in the format of [key:value;]
func SerializeHeaders(h1Headers http.Header) string {
	// compute size of the fully serialized value and largest temp buffer we will need
	serializedLen := 0
	maxTempLen := 0
	for headerName, headerValues := range h1Headers {
		for _, headerValue := range headerValues {
			nameLen := headerEncoding.EncodedLen(len(headerName))
			valueLen := headerEncoding.EncodedLen(len(headerValue))
			const delims = 2
			serializedLen += delims + nameLen + valueLen
			if nameLen > maxTempLen {
				maxTempLen = nameLen
			}
			if valueLen > maxTempLen {
				maxTempLen = valueLen
			}
		}
	}
	var buf strings.Builder
	buf.Grow(serializedLen)

	temp := make([]byte, maxTempLen)
	writeB64 := func(s string) {
		n := headerEncoding.EncodedLen(len(s))
		if n > len(temp) {
			temp = make([]byte, n)
		}
		headerEncoding.Encode(temp[:n], []byte(s))
		buf.Write(temp[:n])
	}

	for headerName, headerValues := range h1Headers {
		for _, headerValue := range headerValues {
			if buf.Len() > 0 {
				buf.WriteByte(';')
			}
			writeB64(headerName)
			buf.WriteByte(':')
			writeB64(headerValue)
		}
	}

	return buf.String()
}

// Deserialize headers serialized by `SerializeHeader`
func DeserializeHeaders(serializedHeaders string) ([]h2mux.Header, error) {
	const unableToDeserializeErr = "Unable to deserialize headers"

	var deserialized []h2mux.Header
	for _, serializedPair := range strings.Split(serializedHeaders, ";") {
		if len(serializedPair) == 0 {
			continue
		}

		serializedHeaderParts := strings.Split(serializedPair, ":")
		if len(serializedHeaderParts) != 2 {
			return nil, errors.New(unableToDeserializeErr)
		}

		serializedName := serializedHeaderParts[0]
		serializedValue := serializedHeaderParts[1]
		deserializedName := make([]byte, headerEncoding.DecodedLen(len(serializedName)))
		deserializedValue := make([]byte, headerEncoding.DecodedLen(len(serializedValue)))

		if _, err := headerEncoding.Decode(deserializedName, []byte(serializedName)); err != nil {
			return nil, errors.Wrap(err, unableToDeserializeErr)
		}
		if _, err := headerEncoding.Decode(deserializedValue, []byte(serializedValue)); err != nil {
			return nil, errors.Wrap(err, unableToDeserializeErr)
		}

		deserialized = append(deserialized, h2mux.Header{
			Name:  string(deserializedName),
			Value: string(deserializedValue),
		})
	}

	return deserialized, nil
}
