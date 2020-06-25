package h2mux

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/quick"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ByName []Header

func (a ByName) Len() int      { return len(a) }
func (a ByName) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByName) Less(i, j int) bool {
	if a[i].Name == a[j].Name {
		return a[i].Value < a[j].Value
	}

	return a[i].Name < a[j].Name
}

func TestH2RequestHeadersToH1Request_RegularHeaders(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	assert.NoError(t, err)

	mockHeaders := http.Header{
		"Mock header 1": {"Mock value 1"},
		"Mock header 2": {"Mock value 2"},
	}

	headersConversionErr := H2RequestHeadersToH1Request(CreateSerializedHeaders(RequestUserHeadersField, mockHeaders), request)

	assert.True(t, reflect.DeepEqual(mockHeaders, request.Header))
	assert.NoError(t, headersConversionErr)
}

func TestH2RequestHeadersToH1Request_NoHeaders(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	assert.NoError(t, err)

	headersConversionErr := H2RequestHeadersToH1Request(
		[]Header{{
			RequestUserHeadersField,
			SerializeHeaders(http.Header{}),
		}},
		request,
	)

	assert.True(t, reflect.DeepEqual(http.Header{}, request.Header))
	assert.NoError(t, headersConversionErr)
}

func TestH2RequestHeadersToH1Request_InvalidHostPath(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	assert.NoError(t, err)

	mockRequestHeaders := []Header{
		{Name: ":path", Value: "//bad_path/"},
		{Name: RequestUserHeadersField, Value: SerializeHeaders(http.Header{"Mock header": {"Mock value"}})},
	}

	headersConversionErr := H2RequestHeadersToH1Request(mockRequestHeaders, request)

	assert.Equal(t, http.Header{
		"Mock header": []string{"Mock value"},
	}, request.Header)

	assert.Equal(t, "http://example.com//bad_path/", request.URL.String())

	assert.NoError(t, headersConversionErr)
}

func TestH2RequestHeadersToH1Request_HostPathWithQuery(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	assert.NoError(t, err)

	mockRequestHeaders := []Header{
		{Name: ":path", Value: "/?query=mock%20value"},
		{Name: RequestUserHeadersField, Value: SerializeHeaders(http.Header{"Mock header": {"Mock value"}})},
	}

	headersConversionErr := H2RequestHeadersToH1Request(mockRequestHeaders, request)

	assert.Equal(t, http.Header{
		"Mock header": []string{"Mock value"},
	}, request.Header)

	assert.Equal(t, "http://example.com/?query=mock%20value", request.URL.String())

	assert.NoError(t, headersConversionErr)
}

func TestH2RequestHeadersToH1Request_HostPathWithURLEncoding(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	assert.NoError(t, err)

	mockRequestHeaders := []Header{
		{Name: ":path", Value: "/mock%20path"},
		{Name: RequestUserHeadersField, Value: SerializeHeaders(http.Header{"Mock header": {"Mock value"}})},
	}

	headersConversionErr := H2RequestHeadersToH1Request(mockRequestHeaders, request)

	assert.Equal(t, http.Header{
		"Mock header": []string{"Mock value"},
	}, request.Header)

	assert.Equal(t, "http://example.com/mock%20path", request.URL.String())

	assert.NoError(t, headersConversionErr)
}

func TestH2RequestHeadersToH1Request_WeirdURLs(t *testing.T) {
	type testCase struct {
		path string
		want string
	}
	testCases := []testCase{
		{
			path: "",
			want: "",
		},
		{
			path: "/",
			want: "/",
		},
		{
			path: "//",
			want: "//",
		},
		{
			path: "/test",
			want: "/test",
		},
		{
			path: "//test",
			want: "//test",
		},
		{
			// https://github.com/cloudflare/cloudflared/issues/81
			path: "//test/",
			want: "//test/",
		},
		{
			path: "/%2Ftest",
			want: "/%2Ftest",
		},
		{
			path: "//%20test",
			want: "//%20test",
		},
		{
			// https://github.com/cloudflare/cloudflared/issues/124
			path: "/test?get=somthing%20a",
			want: "/test?get=somthing%20a",
		},
		{
			path: "/%20",
			want: "/%20",
		},
		{
			// stdlib's EscapedPath() will always percent-encode ' '
			path: "/ ",
			want: "/%20",
		},
		{
			path: "/ a ",
			want: "/%20a%20",
		},
		{
			path: "/a%20b",
			want: "/a%20b",
		},
		{
			path: "/foo/bar;param?query#frag",
			want: "/foo/bar;param?query#frag",
		},
		{
			// stdlib's EscapedPath() will always percent-encode non-ASCII chars
			path: "/a␠b",
			want: "/a%E2%90%A0b",
		},
		{
			path: "/a-umlaut-ä",
			want: "/a-umlaut-%C3%A4",
		},
		{
			path: "/a-umlaut-%C3%A4",
			want: "/a-umlaut-%C3%A4",
		},
		{
			path: "/a-umlaut-%c3%a4",
			want: "/a-umlaut-%c3%a4",
		},
		{
			// here the second '#' is treated as part of the fragment
			path: "/a#b#c",
			want: "/a#b%23c",
		},
		{
			path: "/a#b␠c",
			want: "/a#b%E2%90%A0c",
		},
		{
			path: "/a#b%20c",
			want: "/a#b%20c",
		},
		{
			path: "/a#b c",
			want: "/a#b%20c",
		},
		{
			// stdlib's EscapedPath() will always percent-encode '\'
			path: "/\\",
			want: "/%5C",
		},
		{
			path: "/a\\",
			want: "/a%5C",
		},
		{
			path: "/a,b.c.",
			want: "/a,b.c.",
		},
		{
			path: "/.",
			want: "/.",
		},
		{
			// stdlib's EscapedPath() will always percent-encode '`'
			path: "/a`",
			want: "/a%60",
		},
		{
			path: "/a[0]",
			want: "/a[0]",
		},
		{
			path: "/?a[0]=5 &b[]=",
			want: "/?a[0]=5 &b[]=",
		},
		{
			path: "/?a=%22b%20%22",
			want: "/?a=%22b%20%22",
		},
	}

	for index, testCase := range testCases {
		requestURL := "https://example.com"

		request, err := http.NewRequest(http.MethodGet, requestURL, nil)
		assert.NoError(t, err)

		mockRequestHeaders := []Header{
			{Name: ":path", Value: testCase.path},
			{Name: RequestUserHeadersField, Value: SerializeHeaders(http.Header{"Mock header": {"Mock value"}})},
		}

		headersConversionErr := H2RequestHeadersToH1Request(mockRequestHeaders, request)
		assert.NoError(t, headersConversionErr)

		assert.Equal(t,
			http.Header{
				"Mock header": []string{"Mock value"},
			},
			request.Header)

		assert.Equal(t,
			"https://example.com"+testCase.want,
			request.URL.String(),
			"Failed URL index: %v %#v", index, testCase)
	}
}

func TestH2RequestHeadersToH1Request_QuickCheck(t *testing.T) {
	config := &quick.Config{
		Values: func(args []reflect.Value, rand *rand.Rand) {
			args[0] = reflect.ValueOf(randomHTTP2Path(t, rand))
		},
	}

	type testOrigin struct {
		url string

		expectedScheme   string
		expectedBasePath string
	}
	testOrigins := []testOrigin{
		{
			url:              "http://origin.hostname.example.com:8080",
			expectedScheme:   "http",
			expectedBasePath: "http://origin.hostname.example.com:8080",
		},
		{
			url:              "http://origin.hostname.example.com:8080/",
			expectedScheme:   "http",
			expectedBasePath: "http://origin.hostname.example.com:8080",
		},
		{
			url:              "http://origin.hostname.example.com:8080/api",
			expectedScheme:   "http",
			expectedBasePath: "http://origin.hostname.example.com:8080/api",
		},
		{
			url:              "http://origin.hostname.example.com:8080/api/",
			expectedScheme:   "http",
			expectedBasePath: "http://origin.hostname.example.com:8080/api",
		},
		{
			url:              "https://origin.hostname.example.com:8080/api",
			expectedScheme:   "https",
			expectedBasePath: "https://origin.hostname.example.com:8080/api",
		},
	}

	// use multiple schemes to demonstrate that the URL is based on the
	// origin's scheme, not the :scheme header
	for _, testScheme := range []string{"http", "https"} {
		for _, testOrigin := range testOrigins {
			assertion := func(testPath string) bool {
				const expectedMethod = "POST"
				const expectedHostname = "request.hostname.example.com"

				h2 := []Header{
					{Name: ":method", Value: expectedMethod},
					{Name: ":scheme", Value: testScheme},
					{Name: ":authority", Value: expectedHostname},
					{Name: ":path", Value: testPath},
					{Name: RequestUserHeadersField, Value: ""},
				}
				h1, err := http.NewRequest("GET", testOrigin.url, nil)
				require.NoError(t, err)

				err = H2RequestHeadersToH1Request(h2, h1)
				return assert.NoError(t, err) &&
					assert.Equal(t, expectedMethod, h1.Method) &&
					assert.Equal(t, expectedHostname, h1.Host) &&
					assert.Equal(t, testOrigin.expectedScheme, h1.URL.Scheme) &&
					assert.Equal(t, testOrigin.expectedBasePath+testPath, h1.URL.String())
			}
			err := quick.Check(assertion, config)
			assert.NoError(t, err)
		}
	}
}

func randomASCIIPrintableChar(rand *rand.Rand) int {
	// smallest printable ASCII char is 32, largest is 126
	const startPrintable = 32
	const endPrintable = 127
	return startPrintable + rand.Intn(endPrintable-startPrintable)
}

// randomASCIIText generates an ASCII string, some of whose characters may be
// percent-encoded. Its "logical length" (ignoring percent-encoding) is
// between 1 and `maxLength`.
func randomASCIIText(rand *rand.Rand, minLength int, maxLength int) string {
	length := minLength + rand.Intn(maxLength)
	result := ""
	for i := 0; i < length; i++ {
		c := randomASCIIPrintableChar(rand)

		// 1/4 chance of using percent encoding when not necessary
		if c == '%' || rand.Intn(4) == 0 {
			result += fmt.Sprintf("%%%02X", c)
		} else {
			result += string(c)
		}
	}
	return result
}

// Calls `randomASCIIText` and ensures the result is a valid URL path,
// i.e. one that can pass unchanged through url.URL.String()
func randomHTTP1Path(t *testing.T, rand *rand.Rand, minLength int, maxLength int) string {
	text := randomASCIIText(rand, minLength, maxLength)
	re, err := regexp.Compile("[^/;,]*")
	require.NoError(t, err)
	return "/" + re.ReplaceAllStringFunc(text, url.PathEscape)
}

// Calls `randomASCIIText` and ensures the result is a valid URL query,
// i.e. one that can pass unchanged through url.URL.String()
func randomHTTP1Query(rand *rand.Rand, minLength int, maxLength int) string {
	text := randomASCIIText(rand, minLength, maxLength)
	return "?" + strings.ReplaceAll(text, "#", "%23")
}

// Calls `randomASCIIText` and ensures the result is a valid URL fragment,
// i.e. one that can pass unchanged through url.URL.String()
func randomHTTP1Fragment(t *testing.T, rand *rand.Rand, minLength int, maxLength int) string {
	text := randomASCIIText(rand, minLength, maxLength)
	u, err := url.Parse("#" + text)
	require.NoError(t, err)
	return u.String()
}

// Assemble a random :path pseudoheader that is legal by Go stdlib standards
// (i.e. all characters will satisfy "net/url".shouldEscape for their respective locations)
func randomHTTP2Path(t *testing.T, rand *rand.Rand) string {
	result := randomHTTP1Path(t, rand, 1, 64)
	if rand.Intn(2) == 1 {
		result += randomHTTP1Query(rand, 1, 32)
	}
	if rand.Intn(2) == 1 {
		result += randomHTTP1Fragment(t, rand, 1, 16)
	}
	return result
}

func stdlibHeaderToH2muxHeader(headers http.Header) (h2muxHeaders []Header) {
	for name, values := range headers {
		for _, value := range values {
			h2muxHeaders = append(h2muxHeaders, Header{name, value})
		}
	}

	return h2muxHeaders
}

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

func TestParseHeaders(t *testing.T) {
	mockUserHeadersToSerialize := http.Header{
		"Mock-Header-One":   {"1", "1.5"},
		"Mock-Header-Two":   {"2"},
		"Mock-Header-Three": {"3"},
	}

	mockHeaders := []Header{
		{Name: "One", Value: "1"},
		{Name: "Cf-Two", Value: "cf-value-1"},
		{Name: "Cf-Two", Value: "cf-value-2"},
		{Name: RequestUserHeadersField, Value: SerializeHeaders(mockUserHeadersToSerialize)},
	}

	expectedHeaders := []Header{
		{Name: "Mock-Header-One", Value: "1"},
		{Name: "Mock-Header-One", Value: "1.5"},
		{Name: "Mock-Header-Two", Value: "2"},
		{Name: "Mock-Header-Three", Value: "3"},
	}
	parsedHeaders, err := ParseUserHeaders(RequestUserHeadersField, mockHeaders)
	assert.NoError(t, err)
	assert.ElementsMatch(t, expectedHeaders, parsedHeaders)
}

func TestParseHeadersNoSerializedHeader(t *testing.T) {
	mockHeaders := []Header{
		{Name: "One", Value: "1"},
		{Name: "Cf-Two", Value: "cf-value-1"},
		{Name: "Cf-Two", Value: "cf-value-2"},
	}

	_, err := ParseUserHeaders(RequestUserHeadersField, mockHeaders)
	assert.EqualError(t, err, fmt.Sprintf("%s header not found", RequestUserHeadersField))
}

func TestIsControlHeader(t *testing.T) {
	controlHeaders := []string{
		// Anything that begins with cf-
		"cf-sample-header",
		"CF-SAMPLE-HEADER",
		"Cf-Sample-Header",

		// Any http2 pseudoheader
		":sample-pseudo-header",

		// content-length is a special case, it has to be there
		// for some requests to work (per the HTTP2 spec)
		"content-length",
	}

	for _, header := range controlHeaders {
		assert.True(t, IsControlHeader(header))
	}
}

func TestIsNotControlHeader(t *testing.T) {
	notControlHeaders := []string{
		"Mock-header",
		"Another-sample-header",
	}

	for _, header := range notControlHeaders {
		assert.False(t, IsControlHeader(header))
	}
}

func TestH1ResponseToH2ResponseHeaders(t *testing.T) {
	mockHeaders := http.Header{
		"User-header-one": {""},
		"User-header-two": {"1", "2"},
		"cf-header":       {"cf-value"},
		"Content-Length":  {"123"},
	}
	mockResponse := http.Response{
		StatusCode: 200,
		Header:     mockHeaders,
	}

	headers := H1ResponseToH2ResponseHeaders(&mockResponse)

	serializedHeadersIndex := -1
	for i, header := range headers {
		if header.Name == ResponseUserHeadersField {
			serializedHeadersIndex = i
			break
		}
	}
	assert.NotEqual(t, -1, serializedHeadersIndex)
	actualControlHeaders := append(
		headers[:serializedHeadersIndex],
		headers[serializedHeadersIndex+1:]...,
	)
	expectedControlHeaders := []Header{
		{Name: ":status", Value: "200"},
		{Name: "content-length", Value: "123"},
	}

	assert.ElementsMatch(t, expectedControlHeaders, actualControlHeaders)

	actualUserHeaders, err := DeserializeHeaders(headers[serializedHeadersIndex].Value)
	expectedUserHeaders := []Header{
		{Name: "User-header-one", Value: ""},
		{Name: "User-header-two", Value: "1"},
		{Name: "User-header-two", Value: "2"},
	}
	assert.NoError(t, err)
	assert.ElementsMatch(t, expectedUserHeaders, actualUserHeaders)
}

// The purpose of this test is to check that our code and the http.Header
// implementation don't throw validation errors about header size
func TestHeaderSize(t *testing.T) {
	largeValue := randSeq(5 * 1024 * 1024) // 5Mb
	largeHeaders := http.Header{
		"User-header": {largeValue},
	}
	mockResponse := http.Response{
		StatusCode: 200,
		Header:     largeHeaders,
	}

	serializedHeaders := H1ResponseToH2ResponseHeaders(&mockResponse)
	request, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	assert.NoError(t, err)
	for _, header := range serializedHeaders {
		request.Header.Set(header.Name, header.Value)
	}

	for _, header := range serializedHeaders {
		if header.Name != ResponseUserHeadersField {
			continue
		}

		deserializedHeaders, err := DeserializeHeaders(header.Value)
		assert.NoError(t, err)
		assert.Equal(t, largeValue, deserializedHeaders[0].Value)
	}
}

func randSeq(n int) string {
	randomizer := rand.New(rand.NewSource(17))
	var letters = []rune(":;,+/=abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[randomizer.Intn(len(letters))]
	}
	return string(b)
}
