package tracing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
	"go.opentelemetry.io/otel/trace"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

const (
	resourceSchemaUrl   = "http://example.com/custom-resource-schema"
	instrumentSchemaUrl = semconv.SchemaURL
)

var (
	traceId      = []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F}
	spanId       = []byte{0xFF, 0xFE, 0xFD, 0xFC, 0xFB, 0xFA, 0xF9, 0xF8}
	parentSpanId = []byte{0x0F, 0x0E, 0x0D, 0x0C, 0x0B, 0x0A, 0x09, 0x08}
	startTime    = time.Date(2022, 4, 4, 0, 0, 0, 0, time.UTC)
	endTime      = startTime.Add(5 * time.Second)

	traceState, _ = trace.ParseTraceState("key1=val1,key2=val2")
	instrScope    = &commonpb.InstrumentationScope{Name: "go.opentelemetry.io/test/otel", Version: "v1.6.0"}
	otlpKeyValues = []*commonpb.KeyValue{
		{
			Key: "string_key",
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_StringValue{
					StringValue: "string value",
				},
			},
		},
		{
			Key: "bool_key",
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_BoolValue{
					BoolValue: true,
				},
			},
		},
	}
	otlpResource = &resourcepb.Resource{
		Attributes: []*commonpb.KeyValue{
			{
				Key: "service.name",
				Value: &commonpb.AnyValue{
					Value: &commonpb.AnyValue_StringValue{
						StringValue: "service-name",
					},
				},
			},
		},
	}
)

var _ otlptrace.Client = (*InMemoryOtlpClient)(nil)
var _ InMemoryClient = (*InMemoryOtlpClient)(nil)
var _ otlptrace.Client = (*NoopOtlpClient)(nil)
var _ InMemoryClient = (*NoopOtlpClient)(nil)

func TestUploadTraces(t *testing.T) {
	client := &InMemoryOtlpClient{}
	spans := createResourceSpans([]*tracepb.Span{createOtlpSpan(traceId)})
	spans2 := createResourceSpans([]*tracepb.Span{createOtlpSpan(traceId)})
	err := client.UploadTraces(context.Background(), spans)
	assert.NoError(t, err)
	err = client.UploadTraces(context.Background(), spans2)
	assert.NoError(t, err)
	assert.Len(t, client.spans, 2)
}

func TestSpans(t *testing.T) {
	client := &InMemoryOtlpClient{}
	spans := createResourceSpans([]*tracepb.Span{createOtlpSpan(traceId)})
	err := client.UploadTraces(context.Background(), spans)
	assert.NoError(t, err)
	assert.Len(t, client.spans, 1)
	enc, err := client.Spans()
	assert.NoError(t, err)
	expected := "CsECCiAKHgoMc2VydmljZS5uYW1lEg4KDHNlcnZpY2UtbmFtZRLxAQonCh1nby5vcGVudGVsZW1ldHJ5LmlvL3Rlc3Qvb3RlbBIGdjEuNi4wEp0BChAAAQIDBAUGBwgJCgsMDQ4PEgj//v38+/r5+BoTa2V5MT12YWwxLGtleTI9dmFsMiIIDw4NDAsKCQgqCnRyYWNlX25hbWUwATkAANJvaYjiFkEA8teZaojiFkocCgpzdHJpbmdfa2V5Eg4KDHN0cmluZyB2YWx1ZUoOCghib29sX2tleRICEAF6EhIOc3RhdHVzIG1lc3NhZ2UYARomaHR0cHM6Ly9vcGVudGVsZW1ldHJ5LmlvL3NjaGVtYXMvMS43LjAaKWh0dHA6Ly9leGFtcGxlLmNvbS9jdXN0b20tcmVzb3VyY2Utc2NoZW1h"
	assert.Equal(t, expected, enc)
}

func TestSpansEmpty(t *testing.T) {
	client := &InMemoryOtlpClient{}
	err := client.UploadTraces(context.Background(), []*tracepb.ResourceSpans{})
	assert.NoError(t, err)
	assert.Len(t, client.spans, 0)
	_, err = client.Spans()
	assert.ErrorIs(t, err, errNoTraces)
}

func TestSpansNil(t *testing.T) {
	client := &InMemoryOtlpClient{}
	err := client.UploadTraces(context.Background(), nil)
	assert.NoError(t, err)
	assert.Len(t, client.spans, 0)
	_, err = client.Spans()
	assert.ErrorIs(t, err, errNoTraces)
}

func TestSpansTooManySpans(t *testing.T) {
	client := &InMemoryOtlpClient{}
	for i := 0; i < MaxTraceAmount+1; i++ {
		spans := createResourceSpans([]*tracepb.Span{createOtlpSpan(traceId)})
		err := client.UploadTraces(context.Background(), spans)
		assert.NoError(t, err)
	}
	assert.Len(t, client.spans, MaxTraceAmount)
	_, err := client.Spans()
	assert.NoError(t, err)
}

func createResourceSpans(spans []*tracepb.Span) []*tracepb.ResourceSpans {
	return []*tracepb.ResourceSpans{createResourceSpan(spans)}
}

func createResourceSpan(spans []*tracepb.Span) *tracepb.ResourceSpans {
	return &tracepb.ResourceSpans{
		Resource: otlpResource,
		ScopeSpans: []*tracepb.ScopeSpans{
			{
				Scope:     instrScope,
				Spans:     spans,
				SchemaUrl: instrumentSchemaUrl,
			},
		},
		InstrumentationLibrarySpans: nil,
		SchemaUrl:                   resourceSchemaUrl,
	}
}

func createOtlpSpan(tid []byte) *tracepb.Span {
	return &tracepb.Span{
		TraceId:                tid,
		SpanId:                 spanId,
		TraceState:             traceState.String(),
		ParentSpanId:           parentSpanId,
		Name:                   "trace_name",
		Kind:                   tracepb.Span_SPAN_KIND_INTERNAL,
		StartTimeUnixNano:      uint64(startTime.UnixNano()),
		EndTimeUnixNano:        uint64(endTime.UnixNano()),
		Attributes:             otlpKeyValues,
		DroppedAttributesCount: 0,
		Events:                 nil,
		DroppedEventsCount:     0,
		Links:                  nil,
		DroppedLinksCount:      0,
		Status: &tracepb.Status{
			Message: "status message",
			Code:    tracepb.Status_STATUS_CODE_OK,
		},
	}
}
