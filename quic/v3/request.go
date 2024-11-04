package v3

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	datagramRequestIdLen = 16
)

var (
	// ErrInvalidRequestIDLen is returned when the provided request id can not be parsed from the provided byte slice.
	ErrInvalidRequestIDLen error = errors.New("invalid request id length provided")
	// ErrInvalidPayloadDestLen is returned when the provided destination byte slice cannot fit the whole request id.
	ErrInvalidPayloadDestLen error = errors.New("invalid payload size provided")
)

// RequestID is the request-id-v2 identifier, it is used to distinguish between specific flows or sessions proxied
// from the edge to cloudflared.
type RequestID uint128

type uint128 struct {
	hi uint64
	lo uint64
}

// RequestIDFromSlice reads a request ID from a byte slice.
func RequestIDFromSlice(data []byte) (RequestID, error) {
	if len(data) != datagramRequestIdLen {
		return RequestID{}, ErrInvalidRequestIDLen
	}

	return RequestID{
		hi: binary.BigEndian.Uint64(data[:8]),
		lo: binary.BigEndian.Uint64(data[8:]),
	}, nil
}

func (id RequestID) String() string {
	return fmt.Sprintf("%016x%016x", id.hi, id.lo)
}

// Compare returns an integer comparing two IPs.
// The result will be 0 if id == id2, -1 if id < id2, and +1 if id > id2.
// The definition of "less than" is the same as the [RequestID.Less] method.
func (id RequestID) Compare(id2 RequestID) int {
	hi1, hi2 := id.hi, id2.hi
	if hi1 < hi2 {
		return -1
	}
	if hi1 > hi2 {
		return 1
	}
	lo1, lo2 := id.lo, id2.lo
	if lo1 < lo2 {
		return -1
	}
	if lo1 > lo2 {
		return 1
	}
	return 0
}

// Less reports whether id sorts before id2.
func (id RequestID) Less(id2 RequestID) bool { return id.Compare(id2) == -1 }

// MarshalBinaryTo writes the id to the provided destination byte slice; the byte slice must be of at least size 16.
func (id RequestID) MarshalBinaryTo(data []byte) error {
	if len(data) < datagramRequestIdLen {
		return ErrInvalidPayloadDestLen
	}
	binary.BigEndian.PutUint64(data[:8], id.hi)
	binary.BigEndian.PutUint64(data[8:], id.lo)
	return nil
}

func (id *RequestID) UnmarshalBinary(data []byte) error {
	if len(data) != 16 {
		return fmt.Errorf("invalid length slice provided to unmarshal: %d (expected 16)", len(data))
	}

	*id = RequestID{
		binary.BigEndian.Uint64(data[:8]),
		binary.BigEndian.Uint64(data[8:]),
	}
	return nil
}
