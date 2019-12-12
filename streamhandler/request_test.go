package streamhandler

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"testing/quick"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestH2RequestHeadersToH1Request_RegularHeaders(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	assert.NoError(t, err)

	headersConversionErr := H2RequestHeadersToH1Request(
		[]h2mux.Header{
			h2mux.Header{
				Name:  "Mock header 1",
				Value: "Mock value 1",
			},
			h2mux.Header{
				Name:  "Mock header 2",
				Value: "Mock value 2",
			},
		},
		request,
	)

	assert.Equal(t, http.Header{
		"Mock header 1": []string{"Mock value 1"},
		"Mock header 2": []string{"Mock value 2"},
	}, request.Header)

	assert.NoError(t, headersConversionErr)
}

func TestH2RequestHeadersToH1Request_NoHeaders(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	assert.NoError(t, err)

	headersConversionErr := H2RequestHeadersToH1Request(
		[]h2mux.Header{},
		request,
	)

	assert.Equal(t, http.Header{}, request.Header)

	assert.NoError(t, headersConversionErr)
}

func TestH2RequestHeadersToH1Request_InvalidHostPath(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	assert.NoError(t, err)

	headersConversionErr := H2RequestHeadersToH1Request(
		[]h2mux.Header{
			h2mux.Header{
				Name:  ":path",
				Value: "//bad_path/",
			},
			h2mux.Header{
				Name:  "Mock header",
				Value: "Mock value",
			},
		},
		request,
	)

	assert.Equal(t, http.Header{
		"Mock header": []string{"Mock value"},
	}, request.Header)

	assert.Equal(t, "http://example.com//bad_path/", request.URL.String())

	assert.NoError(t, headersConversionErr)
}

func TestH2RequestHeadersToH1Request_HostPathWithQuery(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	assert.NoError(t, err)

	headersConversionErr := H2RequestHeadersToH1Request(
		[]h2mux.Header{
			h2mux.Header{
				Name:  ":path",
				Value: "/?query=mock%20value",
			},
			h2mux.Header{
				Name:  "Mock header",
				Value: "Mock value",
			},
		},
		request,
	)

	assert.Equal(t, http.Header{
		"Mock header": []string{"Mock value"},
	}, request.Header)

	assert.Equal(t, "http://example.com/?query=mock%20value", request.URL.String())

	assert.NoError(t, headersConversionErr)
}

func TestH2RequestHeadersToH1Request_HostPathWithURLEncoding(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	assert.NoError(t, err)

	headersConversionErr := H2RequestHeadersToH1Request(
		[]h2mux.Header{
			h2mux.Header{
				Name:  ":path",
				Value: "/mock%20path",
			},
			h2mux.Header{
				Name:  "Mock header",
				Value: "Mock value",
			},
		},
		request,
	)

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
		headersConversionErr := H2RequestHeadersToH1Request(
			[]h2mux.Header{
				h2mux.Header{
					Name:  ":path",
					Value: testCase.path,
				},
				h2mux.Header{
					Name:  "Mock header",
					Value: "Mock value",
				},
			},
			request,
		)
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

				h2 := []h2mux.Header{
					h2mux.Header{Name: ":method", Value: expectedMethod},
					h2mux.Header{Name: ":scheme", Value: testScheme},
					h2mux.Header{Name: ":authority", Value: expectedHostname},
					h2mux.Header{Name: ":path", Value: testPath},
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
	regexp, err := regexp.Compile("[^/;,]*")
	require.NoError(t, err)
	return "/" + regexp.ReplaceAllStringFunc(text, url.PathEscape)
}

// Calls `randomASCIIText` and ensures the result is a valid URL query,
// i.e. one that can pass unchanged through url.URL.String()
func randomHTTP1Query(t *testing.T, rand *rand.Rand, minLength int, maxLength int) string {
	text := randomASCIIText(rand, minLength, maxLength)
	return "?" + strings.ReplaceAll(text, "#", "%23")
}

// Calls `randomASCIIText` and ensures the result is a valid URL fragment,
// i.e. one that can pass unchanged through url.URL.String()
func randomHTTP1Fragment(t *testing.T, rand *rand.Rand, minLength int, maxLength int) string {
	text := randomASCIIText(rand, minLength, maxLength)
	url, err := url.Parse("#" + text)
	require.NoError(t, err)
	return url.String()
}

// Assemble a random :path pseudoheader that is legal by Go stdlib standards
// (i.e. all characters will satisfy "net/url".shouldEscape for their respective locations)
func randomHTTP2Path(t *testing.T, rand *rand.Rand) string {
	result := randomHTTP1Path(t, rand, 1, 64)
	if rand.Intn(2) == 1 {
		result += randomHTTP1Query(t, rand, 1, 32)
	}
	if rand.Intn(2) == 1 {
		result += randomHTTP1Fragment(t, rand, 1, 16)
	}
	return result
}
