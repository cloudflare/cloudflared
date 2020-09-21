package h2mux

import (
	"encoding/base64"
	"encoding/json"
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

	ResponseMetaHeaderField   = "cf-cloudflared-response-meta"
	ResponseSourceCloudflared = "cloudflared"
	ResponseSourceOrigin      = "origin"

	CFAccessTokenHeader        = "cf-access-token"
	CFJumpDestinationHeader    = "CF-Access-Jump-Destination"
	CFAccessClientIDHeader     = "CF-Access-Client-Id"
	CFAccessClientSecretHeader = "CF-Access-Client-Secret"
)

// H2RequestHeadersToH1Request converts the HTTP/2 headers coming from origintunneld
// to an HTTP/1 Request object destined for the local origin web service.
// This operation includes conversion of the pseudo-headers into their closest
// HTTP/1 equivalents. See https://tools.ietf.org/html/rfc7540#section-8.1.2.3
func H2RequestHeadersToH1Request(h2 []Header, h1 *http.Request) error {
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
		case RequestUserHeadersField:
			// Do not forward the serialized headers to the origin -- deserialize them, and ditch the serialized version
			// Find and parse user headers serialized into a single one
			userHeaders, err := ParseUserHeaders(RequestUserHeadersField, h2)
			if err != nil {
				return errors.Wrap(err, "Unable to parse user headers")
			}
			for _, userHeader := range userHeaders {
				h1.Header.Add(http.CanonicalHeaderKey(userHeader.Name), userHeader.Value)
			}
		default:
			// All other control headers shall just be proxied transparently
			h1.Header.Add(http.CanonicalHeaderKey(header.Name), header.Value)
		}
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

func H1ResponseToH2ResponseHeaders(h1 *http.Response) (h2 []Header) {
	h2 = []Header{
		{Name: ":status", Value: strconv.Itoa(h1.StatusCode)},
	}
	userHeaders := make(http.Header, len(h1.Header))
	for header, values := range h1.Header {
		h2name := strings.ToLower(header)
		if h2name == "content-length" {
			// This header has meaning in HTTP/2 and will be used by the edge,
			// so it should be sent as an HTTP/2 response header.

			// Since these are http2 headers, they're required to be lowercase
			h2 = append(h2, Header{Name: "content-length", Value: values[0]})
		} else if !IsControlHeader(h2name) || IsWebsocketClientHeader(h2name) {
			// User headers, on the other hand, must all be serialized so that
			// HTTP/2 header validation won't be applied to HTTP/1 header values
			userHeaders[header] = values
		}
	}

	// Perform user header serialization and set them in the single header
	h2 = append(h2, Header{ResponseUserHeadersField, SerializeHeaders(userHeaders)})

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

type ResponseMetaHeader struct {
	Source string `json:"src"`
}

func CreateResponseMetaHeader(headerName, source string) Header {
	jsonResponseMetaHeader, err := json.Marshal(ResponseMetaHeader{Source: source})
	if err != nil {
		panic(err)
	}

	return Header{
		Name:  headerName,
		Value: string(jsonResponseMetaHeader),
	}
}
