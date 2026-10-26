package diagnostic

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type DockerLogCollector struct {
	containerID string // This member identifies the container by identifier or name
}

func NewDockerLogCollector(containerID string) *DockerLogCollector {
	return &DockerLogCollector{
		containerID,
	}
}

func (collector *DockerLogCollector) Collect(ctx context.Context) (*LogInformation, error) {
	// nolint: gosec
	outputHandle, err := os.Create(filepath.Join(os.TempDir(), logFilename))
	if err != nil {
		return nil, fmt.Errorf("error opening output file: %w", err)
	}

	defer func() { _ = outputHandle.Close() }()

	// Calculate 2 weeks ago
	since := time.Now().Add(twoWeeksOffset).Format(time.RFC3339)

	// nolint: gosec
	command := exec.CommandContext(
		ctx,
		"docker",
		"logs",
		"--tail",
		tailMaxNumberOfLines,
		"--since",
		since,
		collector.containerID,
	)

	return PipeCommandOutputToFile(command, outputHandle)
}
