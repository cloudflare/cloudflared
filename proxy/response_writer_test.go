package proxy

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func filterTestHeaders(headers http.Header) {
	headers.Del("X-Filtered-Trailer")
}

type recordingTrailerResponseWriter struct {
	*mockHTTPRespWriter
	trailers http.Header
}

func (w *recordingTrailerResponseWriter) AddTrailer(name, value string) {
	w.trailers.Add(name, value)
}

func TestResponseWriterFiltersDirectHeadersOnce(t *testing.T) {
	t.Parallel()

	filterCalls := 0
	filter := func(headers http.Header) {
		filterCalls++
		headers.Set("Cache-Control", "private, no-store")
	}
	responseWriter := newMockHTTPRespWriter()
	filteredWriter := newResponseWriterWithHeaderFilter(responseWriter, filter)

	_, err := filteredWriter.Write([]byte("first"))
	require.NoError(t, err)
	_, err = filteredWriter.Write([]byte("second"))
	require.NoError(t, err)

	assert.Equal(t, 1, filterCalls)
	assert.Equal(t, "private, no-store", responseWriter.Header().Get("Cache-Control"))
}

func TestResponseWriterFiltersTrailers(t *testing.T) {
	t.Parallel()

	responseWriter := &recordingTrailerResponseWriter{
		mockHTTPRespWriter: newMockHTTPRespWriter(),
		trailers:           make(http.Header),
	}
	filteredWriter := newResponseWriterWithHeaderFilter(responseWriter, filterTestHeaders)

	filteredWriter.AddTrailer("X-Filtered-Trailer", "filtered")
	filteredWriter.AddTrailer("X-Preserved-Trailer", "preserved")

	assert.Empty(t, responseWriter.trailers.Values("X-Filtered-Trailer"))
	assert.Equal(t, []string{"preserved"}, responseWriter.trailers.Values("X-Preserved-Trailer"))
}
