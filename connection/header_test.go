package connection

import (
	"net/http"
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSerializeHeaders(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

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
	require.NotEqual(t, "", serializedHeaders)

	// Deserialize back, and ensure we get the same set of headers
	deserializedHeaders, err := DeserializeHeaders(serializedHeaders)
	require.NoError(t, err)

	require.Len(t, deserializedHeaders, 13)
	expectedHeaders := headerToReqHeader(mockHeaders)

	sort.Sort(ByName(deserializedHeaders))
	sort.Sort(ByName(expectedHeaders))

	require.True(
		t,
		reflect.DeepEqual(expectedHeaders, deserializedHeaders),
		"got = %#v, want = %#v\n", deserializedHeaders, expectedHeaders,
	)
}

type ByName []HTTPHeader

func (a ByName) Len() int      { return len(a) }
func (a ByName) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByName) Less(i, j int) bool {
	if a[i].Name == a[j].Name {
		return a[i].Value < a[j].Value
	}

	return a[i].Name < a[j].Name
}

func headerToReqHeader(headers http.Header) (reqHeaders []HTTPHeader) {
	for name, values := range headers {
		for _, value := range values {
			reqHeaders = append(reqHeaders, HTTPHeader{Name: name, Value: value})
		}
	}

	return reqHeaders
}

func TestSerializeNoHeaders(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	serializedHeaders := SerializeHeaders(request.Header)
	deserializedHeaders, err := DeserializeHeaders(serializedHeaders)
	require.NoError(t, err)
	require.Empty(t, deserializedHeaders)
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
		require.Error(t, err)
	}
}

func TestIsControlResponseHeader(t *testing.T) {
	controlResponseHeaders := []string{
		// Anything that begins with cf-int-, cf-cloudflared- or cf-proxy-
		"cf-int-sample-header",
		"cf-cloudflared-sample-header",
		"cf-proxy-sample-header",
		// Any http2 pseudoheader
		":sample-pseudo-header",
	}

	for _, header := range controlResponseHeaders {
		require.True(t, IsControlResponseHeader(header))
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
		require.False(t, IsControlResponseHeader(header))
	}
}
