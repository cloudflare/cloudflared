package streamhandler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/cloudflare/cloudflared/h2mux"

	"github.com/stretchr/testify/assert"
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
	expectedPaths := []string{
		"",
		"/",
		"//",
		"/%20",
		"/ ",
		"/ a ",
		"/a%20b",
		"/foo/bar;param?query#frag",
		"/a␠b",
		"/a-umlaut-ä",
		"/a-umlaut-%C3%A4",
		"/a-umlaut-%c3%a4",
		"/a#b#c",
		"/a#b␠c",
		"/a#b%20c",
		"/a#b c",
		"/\\",
		"/a\\",
		"/a\\b",
		"/a,b.c.",
		"/.",
		"/a`",
		"/a[0]",
		"/?a[0]=5 &b[]=",
		"/?a=%22b%20%22",
	}

	for index, expectedPath := range expectedPaths {
		requestURL := "https://example.com"
		expectedURL := fmt.Sprintf("https://example.com%v", expectedPath)

		request, err := http.NewRequest(http.MethodGet, requestURL, nil)
		assert.NoError(t, err)

		headersConversionErr := H2RequestHeadersToH1Request(
			[]h2mux.Header{
				h2mux.Header{
					Name:  ":path",
					Value: expectedPath,
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

		assert.Equal(t, expectedURL, request.URL.String(), fmt.Sprintf("Failed URL index: %v", index))

		assert.NoError(t, headersConversionErr)
	}
}
