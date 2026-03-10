package protocol

import (
	"encoding/json"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

type Metrics []TelemetryItem

func (ms Metrics) ToEnvelopeItem() (*EnvelopeItem, error) {
	// Convert each metric to its JSON representation
	items := make([]json.RawMessage, 0, len(ms))
	for _, metric := range ms {
		metricPayload, err := json.Marshal(metric)
		if err != nil {
			continue
		}
		items = append(items, metricPayload)
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

	return NewTraceMetricItem(len(items), payload), nil
}

func (Metrics) GetCategory() ratelimit.Category              { return ratelimit.CategoryTraceMetric }
func (Metrics) GetEventID() string                           { return "" }
func (Metrics) GetSdkInfo() *SdkInfo                         { return nil }
func (Metrics) GetDynamicSamplingContext() map[string]string { return nil }
