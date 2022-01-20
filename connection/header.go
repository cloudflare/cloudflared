package connection

import (
	"encoding/base64"
	"fmt"
	"net/http"
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

// IsControlResponseHeader is called in the direction of eyeball <- origin.
func IsControlResponseHeader(headerName string) bool {
	return strings.HasPrefix(headerName, ":") ||
		strings.HasPrefix(headerName, "cf-int-") ||
		strings.HasPrefix(headerName, "cf-cloudflared-")
}

// isWebsocketClientHeader returns true if the header name is required by the client to upgrade properly
func IsWebsocketClientHeader(headerName string) bool {
	return headerName == "sec-websocket-accept" ||
		headerName == "connection" ||
		headerName == "upgrade"
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
