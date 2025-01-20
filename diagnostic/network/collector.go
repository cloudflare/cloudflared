package diagnostic

import (
	"context"
	"errors"
	"time"
)

const MicrosecondsFactor = 1000.0

var ErrEmptyDomain = errors.New("domain must not be empty")

// For now only support ICMP is provided.
type IPVersion int

const (
	V4 IPVersion = iota
	V6 IPVersion = iota
)

type Hop struct {
	Hop    uint8           `json:"hop,omitempty"`    // hop number along the route
	Domain string          `json:"domain,omitempty"` // domain and/or ip of the hop, this field will be '*' if the hop is a timeout
	Rtts   []time.Duration `json:"rtts,omitempty"`   // RTT measurements in microseconds
}

type TraceOptions struct {
	ttl     uint64        // number of hops to perform
	timeout time.Duration // wait timeout for each response
	address string        // address to trace
	useV4   bool
}

func NewTimeoutHop(
	hop uint8,
) *Hop {
	// Whenever there is a hop in the format of 'N * * *'
	// it means that the hop in the path didn't answer to
	// any probe.
	return NewHop(
		hop,
		"*",
		nil,
	)
}

func NewHop(hop uint8, domain string, rtts []time.Duration) *Hop {
	return &Hop{
		hop,
		domain,
		rtts,
	}
}

func NewTraceOptions(
	ttl uint64,
	timeout time.Duration,
	address string,
	useV4 bool,
) TraceOptions {
	return TraceOptions{
		ttl,
		timeout,
		address,
		useV4,
	}
}

type NetworkCollector interface {
	// Performs a trace route operation with the specified options.
	// In case the trace fails, it will return a non-nil error and
	// it may return a string which represents the raw information
	// obtained.
	// In case it is successful it will only return an array of Hops
	// an empty string and a nil error.
	Collect(ctx context.Context, options TraceOptions) ([]*Hop, string, error)
}
