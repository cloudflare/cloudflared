package servertiming

import (
	"net/http"

	"github.com/felixge/httpsnoop"
)

// MiddlewareOpts are options for the Middleware.
type MiddlewareOpts struct {
	// Nothing currently, reserved for the future.
}

// Middleware wraps an http.Handler and provides a *Header in the request
// context that can be used to set Server-Timing headers. The *Header can be
// extracted from the context using FromContext.
//
// The options supplied to this can be nil to use defaults.
//
// The Server-Timing header will be written when the status is written
// only if there are non-empty number of metrics.
//
// To control when Server-Timing is sent, the easiest approach is to wrap
// this middleware and only call it if the request should send server timings.
// For examples, see the README.
func Middleware(next http.Handler, _ *MiddlewareOpts) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var (
			// Create the Server-Timing headers struct
			h Header
			// Remember if the timing header were added to the response headers
			headerWritten bool
		)

		// This places the *Header value into the request context. This
		// can be extracted again with FromContext.
		r = r.WithContext(NewContext(r.Context(), &h))

		// Get the header map. This is a reference and shouldn't change.
		headers := w.Header()

		// Hook the response writer we pass upstream so we can modify headers
		// before they write them to the wire, but after we know what status
		// they are writing.
		hooks := httpsnoop.Hooks{
			WriteHeader: func(original httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
				// Return a function with same signature as
				// http.ResponseWriter.WriteHeader to be called in it's place
				return func(code int) {
					// Write the headers
					writeHeader(headers, &h)

					// Remember that headers were written
					headerWritten = true

					// Call the original WriteHeader function
					original(code)
				}
			},
		}

		w = httpsnoop.Wrap(w, hooks)
		next.ServeHTTP(w, r)

		// In case that next did not called WriteHeader function, add timing header to the response headers
		if !headerWritten {
			writeHeader(headers, &h)
		}
	})
}

func writeHeader(headers http.Header, h *Header) {
	// Grab the lock just in case there is any ongoing concurrency that
	// still has a reference and may be modifying the value.
	h.Lock()
	defer h.Unlock()

	// If there are no metrics set, do nothing
	if len(h.Metrics) == 0 {
		return
	}

	headers.Set(HeaderKey, h.String())
}
