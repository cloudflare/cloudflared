package streamhandler

import (
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
				Value: "/?query",
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

	assert.Equal(t, "http://example.com/?query", request.URL.String())

	assert.NoError(t, headersConversionErr)
}
