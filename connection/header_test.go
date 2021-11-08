package connection

import (
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSerializeHeaders(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	assert.NoError(t, err)

	mockHeaders := http.Header{
		"Mock-Header-One":        {"Mock header one value", "three"},
		"Mock-Header-Two-Long":   {"Mock header two value\nlong"},
		":;":                     {":;", ";:"},
		":":                      {":"},
		";":                      {";"},
		";;":                     {";;"},
		"Empty values":           {"", ""},
		"":                       {"Empty key"},
		"control\tcharacter\b\n": {"value\n\b\t"},
		";\v:":                   {":\v;"},
	}

	for header, values := range mockHeaders {
		for _, value := range values {
			// Note that Golang's http library is opinionated;
			// at this point every header name will be title-cased in order to comply with the HTTP RFC
			// This means our proxy is not completely transparent when it comes to proxying headers
			request.Header.Add(header, value)
		}
	}

	serializedHeaders := SerializeHeaders(request.Header)

	// Sanity check: the headers serialized to something that's not an empty string
	assert.NotEqual(t, "", serializedHeaders)

	// Deserialize back, and ensure we get the same set of headers
	deserializedHeaders, err := DeserializeHeaders(serializedHeaders)
	assert.NoError(t, err)

	assert.Equal(t, 13, len(deserializedHeaders))
	h2muxExpectedHeaders := stdlibHeaderToH2muxHeader(mockHeaders)

	sort.Sort(ByName(deserializedHeaders))
	sort.Sort(ByName(h2muxExpectedHeaders))

	assert.True(
		t,
		reflect.DeepEqual(h2muxExpectedHeaders, deserializedHeaders),
		fmt.Sprintf("got = %#v, want = %#v\n", deserializedHeaders, h2muxExpectedHeaders),
	)
}

func TestSerializeNoHeaders(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	assert.NoError(t, err)

	serializedHeaders := SerializeHeaders(request.Header)
	deserializedHeaders, err := DeserializeHeaders(serializedHeaders)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(deserializedHeaders))
}

func TestDeserializeMalformed(t *testing.T) {
	var err error

	malformedData := []string{
		"malformed data",
		"bW9jawo=",                   // "mock"
		"bW9jawo=:ZGF0YQo=:bW9jawo=", // "mock:data:mock"
		"::",
	}

	for _, malformedValue := range malformedData {
		_, err = DeserializeHeaders(malformedValue)
		assert.Error(t, err)
	}
}

func TestIsControlResponseHeader(t *testing.T) {
	controlResponseHeaders := []string{
		// Anything that begins with cf-int- or cf-cloudflared-
		"cf-int-sample-header",
		"cf-cloudflared-sample-header",
		// Any http2 pseudoheader
		":sample-pseudo-header",
	}

	for _, header := range controlResponseHeaders {
		assert.True(t, IsControlResponseHeader(header))
	}
}

func TestIsNotControlResponseHeader(t *testing.T) {
	notControlResponseHeaders := []string{
		"mock-header",
		"another-sample-header",
		"upgrade",
		"connection",
		"cf-whatever", // On the response path, we only want to filter cf-int- and cf-cloudflared-
	}

	for _, header := range notControlResponseHeaders {
		assert.False(t, IsControlResponseHeader(header))
	}
}
