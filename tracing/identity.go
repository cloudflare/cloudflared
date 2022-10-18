package tracing

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

const (
	// 16 bytes for tracing ID, 8 bytes for span ID and 1 byte for flags
	IdentityLength = 16 + 8 + 1
)

type Identity struct {
	// Based on https://www.jaegertracing.io/docs/1.36/client-libraries/#value
	// parent span ID is always 0 for our case
	traceIDUpper uint64
	traceIDLower uint64
	spanID       uint64
	flags        uint8
}

// TODO: TUN-6604 Remove this. To reconstruct into Jaeger propagation format, convert tracingContext to tracing.Identity
func (tc *Identity) String() string {
	return fmt.Sprintf("%016x%016x:%x:0:%x", tc.traceIDUpper, tc.traceIDLower, tc.spanID, tc.flags)
}

func (tc *Identity) MarshalBinary() ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, IdentityLength))
	for _, field := range []interface{}{
		tc.traceIDUpper,
		tc.traceIDLower,
		tc.spanID,
		tc.flags,
	} {
		if err := binary.Write(buf, binary.BigEndian, field); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func (tc *Identity) UnmarshalBinary(data []byte) error {
	if len(data) < IdentityLength {
		return fmt.Errorf("expect tracingContext to have at least %d bytes, got %d", IdentityLength, len(data))
	}

	buf := bytes.NewBuffer(data)
	for _, field := range []interface{}{
		&tc.traceIDUpper,
		&tc.traceIDLower,
		&tc.spanID,
		&tc.flags,
	} {
		if err := binary.Read(buf, binary.BigEndian, field); err != nil {
			return err
		}
	}

	return nil
}

func NewIdentity(trace string) (*Identity, error) {
	parts := strings.Split(trace, separator)
	if len(parts) != 4 {
		return nil, fmt.Errorf("trace '%s' doesn't have exactly 4 parts separated by %s", trace, separator)
	}
	const base = 16
	tracingID, err := padTracingID(parts[0])
	if err != nil {
		return nil, err
	}
	traceIDUpper, err := strconv.ParseUint(tracingID[:16], base, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse first 16 bytes of tracing ID as uint64, err: %w", err)
	}
	traceIDLower, err := strconv.ParseUint(tracingID[16:], base, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse last 16 bytes of tracing ID as uint64, err: %w", err)
	}
	spanID, err := strconv.ParseUint(parts[1], base, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse span ID as uint64, err: %w", err)
	}
	flags, err := strconv.ParseUint(parts[3], base, 8)
	if err != nil {
		return nil, fmt.Errorf("failed to parse flag as uint8, err: %w", err)
	}
	return &Identity{
		traceIDUpper: traceIDUpper,
		traceIDLower: traceIDLower,
		spanID:       spanID,
		flags:        uint8(flags),
	}, nil
}

func padTracingID(tracingID string) (string, error) {
	if len(tracingID) == 0 {
		return "", fmt.Errorf("missing tracing ID")
	}
	if len(tracingID) == traceID128bitsWidth {
		return tracingID, nil
	}
	// Correctly left pad the trace to a length of 32
	left := traceID128bitsWidth - len(tracingID)
	paddedTracingID := strings.Repeat("0", left) + tracingID
	return paddedTracingID, nil
}
