package util

import (
	"fmt"
	"io"
	"net/http"

	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/protocol"
)

// MaxDrainResponseBytes is the maximum number of bytes that transport
// implementations will read from response bodies when draining them.
const MaxDrainResponseBytes = 16 << 10

// HandleHTTPResponse is a helper method that reads the HTTP response and handles debug output.
func HandleHTTPResponse(response *http.Response, identifier string) bool {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return true
	}

	if response.StatusCode >= 400 && response.StatusCode <= 599 {
		body, err := io.ReadAll(io.LimitReader(response.Body, MaxDrainResponseBytes))
		if err != nil {
			debuglog.Printf("Error while reading response body: %v", err)
			return false
		}

		switch {
		case response.StatusCode == http.StatusRequestEntityTooLarge:
			debuglog.Printf("Sending %s failed because the request was too large: %s", identifier, string(body))
		case response.StatusCode >= 500:
			debuglog.Printf("Sending %s failed with server error %d: %s", identifier, response.StatusCode, string(body))
		default:
			debuglog.Printf("Sending %s failed with client error %d: %s", identifier, response.StatusCode, string(body))
		}
		return false
	}

	debuglog.Printf("Unexpected status code %d for event %s", response.StatusCode, identifier)
	return false
}

// EnvelopeIdentifier returns a human-readable identifier for the event to be used in log messages.
// Format: "<description> [<event-id>]".
func EnvelopeIdentifier(envelope *protocol.Envelope) string {
	if envelope == nil || len(envelope.Items) == 0 {
		return "empty envelope"
	}

	var description string
	// we don't currently support mixed envelope types, so all event types would have the same type.
	itemType := envelope.Items[0].Header.Type

	switch itemType {
	case protocol.EnvelopeItemTypeEvent:
		description = "error"
	case protocol.EnvelopeItemTypeTransaction:
		description = "transaction"
	case protocol.EnvelopeItemTypeCheckIn:
		description = "check-in"
	case protocol.EnvelopeItemTypeLog:
		logCount := 0
		for _, item := range envelope.Items {
			if item != nil && item.Header != nil && item.Header.Type == protocol.EnvelopeItemTypeLog && item.Header.ItemCount != nil {
				logCount += *item.Header.ItemCount
			}
		}
		description = fmt.Sprintf("%d log events", logCount)
	case protocol.EnvelopeItemTypeTraceMetric:
		metricCount := 0
		for _, item := range envelope.Items {
			if item != nil && item.Header != nil && item.Header.Type == protocol.EnvelopeItemTypeTraceMetric && item.Header.ItemCount != nil {
				metricCount += *item.Header.ItemCount
			}
		}
		description = fmt.Sprintf("%d metric events", metricCount)
	default:
		description = fmt.Sprintf("%s event", itemType)
	}

	return fmt.Sprintf("%s [%s]", description, envelope.Header.EventID)
}
