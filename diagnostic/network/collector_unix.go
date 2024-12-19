//go:build darwin || linux

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
	args := []string{
		"-I",
		"-w",
		strconv.FormatInt(int64(options.timeout.Seconds()), 10),
		"-m",
		strconv.FormatUint(options.ttl, 10),
		options.address,
	}

	var command string

	switch options.useV4 {
	case false:
		command = "traceroute6"
	default:
		command = "traceroute"
	}

	process := exec.CommandContext(ctx, command, args...)

	return decodeNetworkOutputToFile(process, DecodeLine)
}

func DecodeLine(text string) (*Hop, error) {
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

	if len(parts) == 1 {
		return NewTimeoutHop(uint8(index)), nil
	}

	domain := ""
	rtts := []time.Duration{}

	for _, part := range parts[1:] {
		rtt, err := strconv.ParseFloat(part, 64)
		if err != nil {
			domain += part + " "
		} else {
			rtts = append(rtts, time.Duration(rtt*MicrosecondsFactor))
		}
	}

	domain, _ = strings.CutSuffix(domain, " ")
	if domain == "" {
		return nil, ErrEmptyDomain
	}

	return NewHop(uint8(index), domain, rtts), nil
}
