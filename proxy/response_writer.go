package proxy

import (
	"maps"
	"net/http"
	"sync"

	"github.com/cloudflare/cloudflared/connection"
)

type responseWriterWithHeaderFilter struct {
	connection.ResponseWriter
	filterHeaderFunc  func(http.Header)
	directHeadersOnce sync.Once
}

func newResponseWriterWithHeaderFilter(
	responseWriter connection.ResponseWriter,
	filterHeaderFunc func(http.Header),
) connection.ResponseWriter {
	return &responseWriterWithHeaderFilter{
		ResponseWriter:   responseWriter,
		filterHeaderFunc: filterHeaderFunc,
	}
}

func (w *responseWriterWithHeaderFilter) WriteRespHeaders(status int, headers http.Header) error {
	filteredHeaders := headers.Clone()
	if filteredHeaders == nil {
		filteredHeaders = make(http.Header)
	}
	maps.Copy(filteredHeaders, w.Header())
	w.filterHeaderFunc(filteredHeaders)
	if err := w.ResponseWriter.WriteRespHeaders(status, filteredHeaders); err != nil {
		return err
	}
	w.directHeadersOnce.Do(func() {})
	return nil
}

func (w *responseWriterWithHeaderFilter) WriteHeader(status int) {
	w.filterDirectResponseHeaders()
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriterWithHeaderFilter) Write(data []byte) (int, error) {
	w.filterDirectResponseHeaders()
	return w.ResponseWriter.Write(data)
}

func (w *responseWriterWithHeaderFilter) AddTrailer(trailerName, trailerValue string) {
	trailer := make(http.Header)
	trailer.Add(trailerName, trailerValue)
	w.filterHeaderFunc(trailer)
	for _, value := range trailer.Values(trailerName) {
		w.ResponseWriter.AddTrailer(trailerName, value)
	}
}

func (w *responseWriterWithHeaderFilter) filterDirectResponseHeaders() {
	w.directHeadersOnce.Do(func() {
		w.filterHeaderFunc(w.Header())
	})
}

func (w *responseWriterWithHeaderFilter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
