//go:build windows

package diagnostic

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type NetworkCollectorImpl struct{}

func (tracer *NetworkCollectorImpl) Collect(ctx context.Context, options TraceOptions) ([]*Hop, string, error) {
	ipversion := "-4"
	if !options.useV4 {
		ipversion = "-6"
	}

	args := []string{
		ipversion,
		"-w",
		strconv.FormatInt(int64(options.timeout.Seconds()), 10),
		"-h",
		strconv.FormatUint(options.ttl, 10),
		// Do not resolve host names (can add 30+ seconds to run time)
		"-d",
		options.address,
	}
	command := exec.CommandContext(ctx, "tracert.exe", args...)

	return decodeNetworkOutputToFile(command, DecodeLine)
}

func DecodeLine(text string) (*Hop, error) {
	const requestTimedOut = "Request timed out."

	fields := strings.Fields(text)
	parts := []string{}
	filter := func(s string) bool { return s != "*" && s != "ms" }

	for _, field := range fields {
		if filter(field) {
			parts = append(parts, field)
		}
	}

	index, err := strconv.ParseUint(parts[0], 10, 8)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse index from timeout hop: %w", err)
	}

	domain := ""
	rtts := []time.Duration{}

	for _, part := range parts[1:] {

		rtt, err := strconv.ParseFloat(strings.TrimLeft(part, "<"), 64)

		if err != nil {
			domain += part + " "
		} else {
			rtts = append(rtts, time.Duration(rtt*MicrosecondsFactor))
		}
	}

	domain, _ = strings.CutSuffix(domain, " ")
	// If the domain is equal to "Request timed out." then we build a
	// timeout hop.
	if domain == requestTimedOut {
		return NewTimeoutHop(uint8(index)), nil
	}

	if domain == "" {
		return nil, ErrEmptyDomain
	}

	return NewHop(uint8(index), domain, rtts), nil
}
