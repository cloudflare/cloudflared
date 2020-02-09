package h2mux

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/pkg/errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Header struct {
	Name, Value string
}

var headerEncoding = base64.RawStdEncoding

// H2RequestHeadersToH1Request converts the HTTP/2 headers to an HTTP/1 Request
// object. This includes conversion of the pseudo-headers into their closest
// HTTP/1 equivalents. See https://tools.ietf.org/html/rfc7540#section-8.1.2.3
func H2RequestHeadersToH1Request(h2 []Header, h1 *http.Request) error {
	for _, header := range h2 {
		switch header.Name {
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
			url, err := url.Parse(base + header.Value)
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("invalid path '%v'", header.Value))
			}
			h1.URL = url
		case "content-length":
			contentLength, err := strconv.ParseInt(header.Value, 10, 64)
			if err != nil {
				return fmt.Errorf("unparseable content length")
			}
			h1.ContentLength = contentLength
		default:
			h1.Header.Add(http.CanonicalHeaderKey(header.Name), header.Value)
		}
	}
	return nil
}

func H1ResponseToH2ResponseHeaders(h1 *http.Response) (h2 []Header) {
	h2 = []Header{{Name: ":status", Value: fmt.Sprintf("%d", h1.StatusCode)}}
	for headerName, headerValues := range h1.Header {
		for _, headerValue := range headerValues {
			h2 = append(h2, Header{Name: strings.ToLower(headerName), Value: headerValue})
		}
	}

	return h2
}

// Serialize HTTP1.x headers by base64-encoding each header name and value,
// and then joining them in the format of [key:value;]
func SerializeHeaders(h1 *http.Request) []byte {
	var serializedHeaders [][]byte
	for headerName, headerValues := range h1.Header {
		for _, headerValue := range headerValues {
			encodedName := make([]byte, headerEncoding.EncodedLen(len(headerName)))
			headerEncoding.Encode(encodedName, []byte(headerName))

			encodedValue := make([]byte, headerEncoding.EncodedLen(len(headerValue)))
			headerEncoding.Encode(encodedValue, []byte(headerValue))

			serializedHeaders = append(
				serializedHeaders,
				bytes.Join(
					[][]byte{encodedName, encodedValue},
					[]byte(":"),
				),
			)
		}
	}

	return bytes.Join(serializedHeaders, []byte(";"))
}

// Deserialize headers serialized by `SerializeHeader`
func DeserializeHeaders(serializedHeaders []byte) (http.Header, error) {
	const unableToDeserializeErr = "Unable to deserialize headers"

	deserialized := http.Header{}
	for _, serializedPair := range bytes.Split(serializedHeaders, []byte(";")) {
		if len(serializedPair) == 0 {
			continue
		}

		serializedHeaderParts := bytes.Split(serializedPair, []byte(":"))
		if len(serializedHeaderParts) != 2 {
			return nil, errors.New(unableToDeserializeErr)
		}

		serializedName := serializedHeaderParts[0]
		serializedValue := serializedHeaderParts[1]
		deserializedName := make([]byte, headerEncoding.DecodedLen(len(serializedName)))
		deserializedValue := make([]byte, headerEncoding.DecodedLen(len(serializedValue)))

		if _, err := headerEncoding.Decode(deserializedName, serializedName); err != nil {
			return nil, errors.Wrap(err, unableToDeserializeErr)
		}
		if _, err := headerEncoding.Decode(deserializedValue, serializedValue); err != nil {
			return nil, errors.Wrap(err, unableToDeserializeErr)
		}

		deserialized.Add(string(deserializedName), string(deserializedValue))
	}

	return deserialized, nil
}
