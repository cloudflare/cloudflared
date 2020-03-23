package h2mux

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

type Header struct {
	Name, Value string
}

var headerEncoding = base64.RawStdEncoding

const (
	RequestUserHeadersField  = "cf-cloudflared-request-headers"
	ResponseUserHeadersField = "cf-cloudflared-response-headers"
)

// H2RequestHeadersToH1Request converts the HTTP/2 headers coming from origintunneld
// to an HTTP/1 Request object destined for the local origin web service.
// This operation includes conversion of the pseudo-headers into their closest
// HTTP/1 equivalents. See https://tools.ietf.org/html/rfc7540#section-8.1.2.3
func H2RequestHeadersToH1Request(h2 []Header, h1 *http.Request) error {
	for _, header := range h2 {
		switch strings.ToLower(header.Name) {
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
		case "connection", "upgrade":
			// for websocket header support
			h1.Header.Add(http.CanonicalHeaderKey(header.Name), header.Value)
		default:
			// Ignore any other header;
			// User headers will be read from `RequestUserHeadersField`
			continue
		}
	}

	// Find and parse user headers serialized into a single one
	userHeaders, err := ParseUserHeaders(RequestUserHeadersField, h2)
	if err != nil {
		return errors.Wrap(err, "Unable to parse user headers")
	}
	for _, userHeader := range userHeaders {
		h1.Header.Add(http.CanonicalHeaderKey(userHeader.Name), userHeader.Value)
	}

	return nil
}

func ParseUserHeaders(headerNameToParseFrom string, headers []Header) ([]Header, error) {
	for _, header := range headers {
		if header.Name == headerNameToParseFrom {
			return DeserializeHeaders(header.Value)
		}
	}

	return nil, fmt.Errorf("%v header not found", RequestUserHeadersField)
}

func IsControlHeader(headerName string) bool {
	headerName = strings.ToLower(headerName)

	return headerName == "content-length" ||
		headerName == "connection" || headerName == "upgrade" || // Websocket headers
		strings.HasPrefix(headerName, ":") ||
		strings.HasPrefix(headerName, "cf-")
}

// IsWebsocketClientHeader returns true if the header name is required by the client to upgrade properly
func IsWebsocketClientHeader(headerName string) bool {
	headerName = strings.ToLower(headerName)

	return headerName == "sec-websocket-accept" ||
		headerName == "connection" ||
		headerName == "upgrade"
}

func H1ResponseToH2ResponseHeaders(h1 *http.Response) (h2 []Header) {
	h2 = []Header{
		{Name: ":status", Value: strconv.Itoa(h1.StatusCode)},
	}
	userHeaders := http.Header{}
	for header, values := range h1.Header {
		for _, value := range values {
			if strings.ToLower(header) == "content-length" {
				// This header has meaning in HTTP/2 and will be used by the edge,
				// so it should be sent as an HTTP/2 response header.

				// Since these are http2 headers, they're required to be lowercase
				h2 = append(h2, Header{Name: strings.ToLower(header), Value: value})
			} else if !IsControlHeader(header) || IsWebsocketClientHeader(header) {
				// User headers, on the other hand, must all be serialized so that
				// HTTP/2 header validation won't be applied to HTTP/1 header values
				if _, ok := userHeaders[header]; ok {
					userHeaders[header] = append(userHeaders[header], value)
				} else {
					userHeaders[header] = []string{value}
				}
			}
		}
	}

	// Perform user header serialization and set them in the single header
	h2 = append(h2, CreateSerializedHeaders(ResponseUserHeadersField, userHeaders)...)

	return h2
}

// Serialize HTTP1.x headers by base64-encoding each header name and value,
// and then joining them in the format of [key:value;]
func SerializeHeaders(h1Headers http.Header) string {
	var serializedHeaders []string
	for headerName, headerValues := range h1Headers {
		for _, headerValue := range headerValues {
			encodedName := make([]byte, headerEncoding.EncodedLen(len(headerName)))
			headerEncoding.Encode(encodedName, []byte(headerName))

			encodedValue := make([]byte, headerEncoding.EncodedLen(len(headerValue)))
			headerEncoding.Encode(encodedValue, []byte(headerValue))

			serializedHeaders = append(
				serializedHeaders,
				strings.Join(
					[]string{string(encodedName), string(encodedValue)},
					":",
				),
			)
		}
	}

	return strings.Join(serializedHeaders, ";")
}

// Deserialize headers serialized by `SerializeHeader`
func DeserializeHeaders(serializedHeaders string) ([]Header, error) {
	const unableToDeserializeErr = "Unable to deserialize headers"

	var deserialized []Header
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

		deserialized = append(deserialized, Header{
			Name:  string(deserializedName),
			Value: string(deserializedValue),
		})
	}

	return deserialized, nil
}

func CreateSerializedHeaders(headersField string, headers ...http.Header) []Header {
	var serializedHeaderChunks []string
	for _, headerChunk := range headers {
		serializedHeaderChunks = append(serializedHeaderChunks, SerializeHeaders(headerChunk))
	}

	return []Header{{
		headersField,
		strings.Join(serializedHeaderChunks, ";"),
	}}
}
