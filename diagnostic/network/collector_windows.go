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
	args := []string{
		"-w",
		strconv.FormatInt(int64(options.timeout.Seconds()), 10),
		"-h",
		strconv.FormatUint(options.ttl, 10),
		// Do not resolve host names (can add 30+ seconds to run time)
		"-d",
		options.address,
	}
	if options.useV4 {
		args = append(args, "-4")
	} else {
		args = append(args, "-6")
	}
	command := exec.CommandContext(ctx, "tracert.exe", args...)

	return decodeNetworkOutputToFile(command, DecodeLine)
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

		rtt, err := strconv.ParseFloat(strings.TrimLeft(part, "<"), 64)

		if err != nil {
			domain += part + " "
		} else {
			rtts = append(rtts, time.Duration(rtt*MicrosecondsFactor))
		}
	}
	domain, _ = strings.CutSuffix(domain, " ")
	return NewHop(uint8(index), domain, rtts), nil
}
