package diagnostic

import (
	"context"
	"fmt"
	"io"
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
	tmp := os.TempDir()

	outputHandle, err := os.Create(filepath.Join(tmp, logFilename))
	if err != nil {
		return nil, fmt.Errorf("error opening output file: %w", err)
	}

	defer outputHandle.Close()

	// Calculate 2 weeks ago
	since := time.Now().Add(twoWeeksOffset).Format(time.RFC3339)

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

	stdoutReader, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf(
			"error retrieving output from command '%s': %w",
			command.String(),
			err,
		)
	}

	if err := command.Start(); err != nil {
		return nil, fmt.Errorf(
			"error running command '%s': %w",
			command.String(),
			err,
		)
	}

	_, err = io.Copy(outputHandle, stdoutReader)
	if err != nil {
		return nil, fmt.Errorf(
			"error copying output from %s to file %s: %w",
			command.String(),
			outputHandle.Name(),
			err,
		)
	}

	if err := command.Wait(); err != nil {
		return nil, fmt.Errorf(
			"error waiting from command '%s': %w",
			command.String(),
			err,
		)
	}

	return NewLogInformation(outputHandle.Name(), true, false), nil
}
