package tracing

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

const (
	MaxTraceAmount = 20
)

var (
	errNoTraces   = errors.New("no traces recorded to be exported")
	errNoopTracer = errors.New("noop tracer has no traces")
)

type InMemoryClient interface {
	// Spans returns a copy of the list of in-memory stored spans as a base64
	// encoded otlp protobuf string.
	Spans() (string, error)
	// ProtoSpans returns a copy of the list of in-memory stored spans as otlp
	// protobuf byte array.
	ProtoSpans() ([]byte, error)
	// Clear spans removes all in-memory spans
	ClearSpans()
}

// InMemoryOtlpClient is a client implementation for otlptrace.Client
type InMemoryOtlpClient struct {
	mu    sync.Mutex
	spans []*tracepb.ResourceSpans
}

func (mc *InMemoryOtlpClient) Start(_ context.Context) error {
	return nil
}

func (mc *InMemoryOtlpClient) Stop(_ context.Context) error {
	return nil
}

// UploadTraces adds the provided list of spans to the in-memory list.
func (mc *InMemoryOtlpClient) UploadTraces(_ context.Context, protoSpans []*tracepb.ResourceSpans) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	// Catch to make sure too many traces aren't being added to response header.
	// Returning nil makes sure we don't fail to send the traces we already recorded.
	if len(mc.spans)+len(protoSpans) > MaxTraceAmount {
		return nil
	}
	mc.spans = append(mc.spans, protoSpans...)
	return nil
}

// Spans returns the list of in-memory stored spans as a base64 encoded otlp protobuf string.
func (mc *InMemoryOtlpClient) Spans() (string, error) {
	data, err := mc.ProtoSpans()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// ProtoSpans returns the list of in-memory stored spans as the protobuf byte array.
func (mc *InMemoryOtlpClient) ProtoSpans() ([]byte, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(mc.spans) <= 0 {
		return nil, errNoTraces
	}
	pbRequest := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: mc.spans,
	}
	return proto.Marshal(pbRequest)
}

func (mc *InMemoryOtlpClient) ClearSpans() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.spans = make([]*tracepb.ResourceSpans, 0)
}

// NoopOtlpClient is a client implementation for otlptrace.Client that does nothing
type NoopOtlpClient struct{}

func (mc *NoopOtlpClient) Start(_ context.Context) error {
	return nil
}

func (mc *NoopOtlpClient) Stop(_ context.Context) error {
	return nil
}

func (mc *NoopOtlpClient) UploadTraces(_ context.Context, _ []*tracepb.ResourceSpans) error {
	return nil
}

// Spans always returns no traces error
func (mc *NoopOtlpClient) Spans() (string, error) {
	return "", errNoopTracer
}

// Spans always returns no traces error
func (mc *NoopOtlpClient) ProtoSpans() ([]byte, error) {
	return nil, errNoopTracer
}

func (mc *NoopOtlpClient) ClearSpans() {}
