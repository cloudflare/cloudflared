package servertiming

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/golang/gddo/httputil/header"
)

// HeaderKey is the specified key for the Server-Timing header.
const HeaderKey = "Server-Timing"

// Header represents a collection of metrics that can be encoded as
// a Server-Timing header value.
//
// The functions for working with metrics are concurrency-safe to make
// it easy to record metrics from goroutines. If you want to avoid the
// lock overhead, you can access the Metrics field directly.
//
// The functions for working with metrics are also usable on a nil
// Header pointer. This allows functions that use FromContext to get the
// *Header value to skip nil-checking and use it as normal. On a nil
// *Header, Metrics are not recorded.
type Header struct {
	// Metrics is the list of metrics in the header.
	Metrics []*Metric

	// The lock that is held when Metrics is being modified. This
	// ONLY NEEDS TO BE SET WHEN working with Metrics directly. If using
	// the functions on the struct, the lock is managed automatically.
	sync.Mutex
}

// ParseHeader parses a Server-Timing header value.
func ParseHeader(input string) (*Header, error) {
	// Split the comma-separated list of metrics
	rawMetrics := header.ParseList(headerParams(input))

	// Parse the list of metrics. We can pre-allocate the length of the
	// comma-separated list of metrics since at most it will be that and
	// most likely it will be that length.
	metrics := make([]*Metric, 0, len(rawMetrics))
	for _, raw := range rawMetrics {
		var m Metric
		m.Name, m.Extra = header.ParseValueAndParams(headerParams(raw))

		// Description
		if v, ok := m.Extra[paramNameDesc]; ok {
			m.Desc = v
			delete(m.Extra, paramNameDesc)
		}

		// Duration. This is treated as a millisecond value since that
		// is what modern browsers are treating it as. If the parsing of
		// an integer fails, the set value remains in the Extra field.
		if v, ok := m.Extra[paramNameDur]; ok {
			m.Duration, _ = time.ParseDuration(v + "ms")
			delete(m.Extra, paramNameDur)
		}

		metrics = append(metrics, &m)
	}

	return &Header{Metrics: metrics}, nil
}

// NewMetric creates a new Metric and adds it to this header.
func (h *Header) NewMetric(name string) *Metric {
	return h.Add(&Metric{Name: name})
}

// Add adds the given metric to the header.
//
// This function is safe to call concurrently.
func (h *Header) Add(m *Metric) *Metric {
	if h == nil {
		return m
	}

	h.Lock()
	defer h.Unlock()
	h.Metrics = append(h.Metrics, m)
	return m
}

// String returns the valid Server-Timing header value that can be
// sent in an HTTP response.
func (h *Header) String() string {
	parts := make([]string, 0, len(h.Metrics))
	for _, m := range h.Metrics {
		parts = append(parts, m.String())
	}

	return strings.Join(parts, ",")
}

// Specified server-timing-param-name values.
const (
	paramNameDesc = "desc"
	paramNameDur  = "dur"
)

// headerParams is a helper function that takes a header value and turns
// it into the expected argument format for the httputil/header library
// functions..
func headerParams(s string) (http.Header, string) {
	const key = "Key"
	return http.Header(map[string][]string{
		key: {s},
	}), key
}

var reNumber = regexp.MustCompile(`^\d+\.?\d*$`)

// headerEncodeParam encodes a key/value pair as a proper `key=value`
// syntax, using double-quotes if necessary.
func headerEncodeParam(key, value string) string {
	// The only case we currently don't quote is numbers. We can make this
	// smarter in the future.
	if reNumber.MatchString(value) {
		return fmt.Sprintf(`%s=%s`, key, value)
	}

	return fmt.Sprintf(`%s=%q`, key, value)
}
