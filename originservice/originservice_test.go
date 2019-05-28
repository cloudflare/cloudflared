package originservice

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsEventStream(t *testing.T) {
	tests := []struct {
		resp          *http.Response
		isEventStream bool
	}{
		{
			resp:          &http.Response{},
			isEventStream: false,
		},
		{
			// isEventStream checks all headers
			resp: &http.Response{
				Header: http.Header{
					"accept":       []string{"text/html"},
					"content-type": []string{"text/event-stream"},
				},
			},
			isEventStream: true,
		},
		{
			// Content-Type and text/event-stream are case-insensitive. text/event-stream can be followed by OWS parameter
			resp: &http.Response{
				Header: http.Header{
					"content-type": []string{"Text/event-stream;charset=utf-8"},
				},
			},
			isEventStream: true,
		},
		{
			// Content-Type and text/event-stream are case-insensitive. text/event-stream can be followed by OWS parameter
			resp: &http.Response{
				Header: http.Header{
					"content-type": []string{"appication/json", "text/html", "Text/event-stream;charset=utf-8"},
				},
			},
			isEventStream: true,
		},
		{
			// Not an event stream because the content-type value doesn't start with text/event-stream
			resp: &http.Response{
				Header: http.Header{
					"content-type": []string{" text/event-stream"},
				},
			},
			isEventStream: false,
		},
	}
	for _, test := range tests {
		assert.Equal(t, test.isEventStream, isEventStream(test.resp), "Header: %v", test.resp.Header)
	}
}
