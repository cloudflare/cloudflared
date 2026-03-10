package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Envelope represents a Sentry envelope containing headers and items.
type Envelope struct {
	Header *EnvelopeHeader `json:"-"`
	Items  []*EnvelopeItem `json:"-"`
}

// EnvelopeHeader represents the header of a Sentry envelope.
type EnvelopeHeader struct {
	// EventID is the unique identifier for this event
	EventID string `json:"event_id"`

	// SentAt is the timestamp when the event was sent from the SDK as string in RFC 3339 format.
	// Used for clock drift correction of the event timestamp. The time zone must be UTC.
	SentAt time.Time `json:"sent_at,omitzero"`

	// Dsn can be used for self-authenticated envelopes.
	// This means that the envelope has all the information necessary to be sent to sentry.
	// In this case the full DSN must be stored in this key.
	Dsn string `json:"dsn,omitempty"`

	// Sdk carries the same payload as the sdk interface in the event payload but can be carried for all events.
	// This means that SDK information can be carried for minidumps, session data and other submissions.
	Sdk *SdkInfo `json:"sdk,omitempty"`

	// Trace contains the [Dynamic Sampling Context](https://develop.sentry.dev/sdk/telemetry/traces/dynamic-sampling-context/)
	Trace map[string]string `json:"trace,omitempty"`
}

// EnvelopeItemType represents the type of envelope item.
type EnvelopeItemType string

// Constants for envelope item types as defined in the Sentry documentation.
const (
	EnvelopeItemTypeEvent       EnvelopeItemType = "event"
	EnvelopeItemTypeTransaction EnvelopeItemType = "transaction"
	EnvelopeItemTypeCheckIn     EnvelopeItemType = "check_in"
	EnvelopeItemTypeAttachment  EnvelopeItemType = "attachment"
	EnvelopeItemTypeLog         EnvelopeItemType = "log"
	EnvelopeItemTypeTraceMetric EnvelopeItemType = "trace_metric"
)

// EnvelopeItemHeader represents the header of an envelope item.
type EnvelopeItemHeader struct {
	// Type specifies the type of this Item and its contents.
	// Based on the Item type, more headers may be required.
	Type EnvelopeItemType `json:"type"`

	// Length is the length of the payload in bytes.
	// If no length is specified, the payload implicitly goes to the next newline.
	// For payloads containing newline characters, the length must be specified.
	Length *int `json:"length,omitempty"`

	// Filename is the name of the attachment file (used for attachments)
	Filename string `json:"filename,omitempty"`

	// ContentType is the MIME type of the item payload (used for attachments and some other item types)
	ContentType string `json:"content_type,omitempty"`

	// ItemCount is the number of items in a batch (used for logs)
	ItemCount *int `json:"item_count,omitempty"`
}

// EnvelopeItem represents a single item or batch within an envelope.
type EnvelopeItem struct {
	Header  *EnvelopeItemHeader `json:"-"`
	Payload []byte              `json:"-"`
}

// NewEnvelope creates a new envelope with the given header.
func NewEnvelope(header *EnvelopeHeader) *Envelope {
	return &Envelope{
		Header: header,
		Items:  make([]*EnvelopeItem, 0),
	}
}

// AddItem adds an item to the envelope.
func (e *Envelope) AddItem(item *EnvelopeItem) {
	if item == nil {
		return
	}
	e.Items = append(e.Items, item)
}

// Serialize serializes the envelope to the Sentry envelope format.
//
// Format: Headers "\n" { Item } [ "\n" ]
// Item: Headers "\n" Payload "\n".
func (e *Envelope) Serialize() ([]byte, error) {
	var buf bytes.Buffer

	headerBytes, err := json.Marshal(e.Header)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal envelope header: %w", err)
	}

	if _, err := buf.Write(headerBytes); err != nil {
		return nil, fmt.Errorf("failed to write envelope header: %w", err)
	}

	if _, err := buf.WriteString("\n"); err != nil {
		return nil, fmt.Errorf("failed to write newline after envelope header: %w", err)
	}

	for _, item := range e.Items {
		if err := e.writeItem(&buf, item); err != nil {
			return nil, fmt.Errorf("failed to write envelope item: %w", err)
		}
	}

	return buf.Bytes(), nil
}

// WriteTo writes the envelope to the given writer in the Sentry envelope format.
func (e *Envelope) WriteTo(w io.Writer) (int64, error) {
	data, err := e.Serialize()
	if err != nil {
		return 0, err
	}

	n, err := w.Write(data)
	return int64(n), err
}

// writeItem writes a single envelope item to the buffer.
func (e *Envelope) writeItem(buf *bytes.Buffer, item *EnvelopeItem) error {
	headerBytes, err := json.Marshal(item.Header)
	if err != nil {
		return fmt.Errorf("failed to marshal item header: %w", err)
	}

	if _, err := buf.Write(headerBytes); err != nil {
		return fmt.Errorf("failed to write item header: %w", err)
	}

	if _, err := buf.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline after item header: %w", err)
	}

	if len(item.Payload) > 0 {
		if _, err := buf.Write(item.Payload); err != nil {
			return fmt.Errorf("failed to write item payload: %w", err)
		}
	}

	if _, err := buf.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline after item payload: %w", err)
	}

	return nil
}

// Size returns the total size of the envelope when serialized.
func (e *Envelope) Size() (int, error) {
	data, err := e.Serialize()
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

// NewEnvelopeItem creates a new envelope item with the specified type and payload.
func NewEnvelopeItem(itemType EnvelopeItemType, payload []byte) *EnvelopeItem {
	length := len(payload)
	return &EnvelopeItem{
		Header: &EnvelopeItemHeader{
			Type:   itemType,
			Length: &length,
		},
		Payload: payload,
	}
}

// NewAttachmentItem creates a new envelope item for an attachment.
// Parameters: filename, contentType, payload.
func NewAttachmentItem(filename, contentType string, payload []byte) *EnvelopeItem {
	length := len(payload)
	return &EnvelopeItem{
		Header: &EnvelopeItemHeader{
			Type:        EnvelopeItemTypeAttachment,
			Length:      &length,
			ContentType: contentType,
			Filename:    filename,
		},
		Payload: payload,
	}
}

// NewLogItem creates a new envelope item for logs.
func NewLogItem(itemCount int, payload []byte) *EnvelopeItem {
	length := len(payload)
	return &EnvelopeItem{
		Header: &EnvelopeItemHeader{
			Type:        EnvelopeItemTypeLog,
			Length:      &length,
			ItemCount:   &itemCount,
			ContentType: "application/vnd.sentry.items.log+json",
		},
		Payload: payload,
	}
}

// NewTraceMetricItem creates a new envelope item for trace metrics.
func NewTraceMetricItem(itemCount int, payload []byte) *EnvelopeItem {
	length := len(payload)
	return &EnvelopeItem{
		Header: &EnvelopeItemHeader{
			Type:        EnvelopeItemTypeTraceMetric,
			Length:      &length,
			ItemCount:   &itemCount,
			ContentType: "application/vnd.sentry.items.trace-metric+json",
		},
		Payload: payload,
	}
}
