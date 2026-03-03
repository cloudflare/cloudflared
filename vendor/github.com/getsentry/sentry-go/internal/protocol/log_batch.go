package protocol

import (
	"encoding/json"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

// LogAttribute is the JSON representation for a single log attribute value.
type LogAttribute struct {
	Value any    `json:"value"`
	Type  string `json:"type"`
}

// Logs is a container for multiple log items which knows how to convert
// itself into a single batched log envelope item.
type Logs []TelemetryItem

func (ls Logs) ToEnvelopeItem() (*EnvelopeItem, error) {
	// Convert each log to its JSON representation
	items := make([]json.RawMessage, 0, len(ls))
	for _, log := range ls {
		logPayload, err := json.Marshal(log)
		if err != nil {
			continue
		}
		items = append(items, logPayload)
	}

	if len(items) == 0 {
		return nil, nil
	}

	wrapper := struct {
		Items []json.RawMessage `json:"items"`
	}{Items: items}

	payload, err := json.Marshal(wrapper)
	if err != nil {
		return nil, err
	}
	return NewLogItem(len(ls), payload), nil
}

func (Logs) GetCategory() ratelimit.Category              { return ratelimit.CategoryLog }
func (Logs) GetEventID() string                           { return "" }
func (Logs) GetSdkInfo() *SdkInfo                         { return nil }
func (Logs) GetDynamicSamplingContext() map[string]string { return nil }
