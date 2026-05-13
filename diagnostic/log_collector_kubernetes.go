package diagnostic

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type KubernetesLogCollector struct {
	containerID string // This member identifies the container by identifier or name
	pod         string // This member identifies the pod where the container is deployed
}

func NewKubernetesLogCollector(containerID, pod string) *KubernetesLogCollector {
	return &KubernetesLogCollector{
		containerID,
		pod,
	}
}

func (collector *KubernetesLogCollector) Collect(ctx context.Context) (*LogInformation, error) {
	// nolint: gosec
	outputHandle, err := os.Create(filepath.Join(os.TempDir(), logFilename))
	if err != nil {
		return nil, fmt.Errorf("error opening output file: %w", err)
	}

	defer func() { _ = outputHandle.Close() }()

	var command *exec.Cmd
	// Calculate 2 weeks ago
	since := time.Now().Add(twoWeeksOffset).Format(time.RFC3339)
	if collector.containerID != "" {
		// nolint: gosec
		command = exec.CommandContext(
			ctx,
			"kubectl",
			"logs",
			collector.pod,
			"--since-time",
			since,
			"--tail",
			tailMaxNumberOfLines,
			"-c",
			collector.containerID,
		)
	} else {
		// nolint: gosec
		command = exec.CommandContext(
			ctx,
			"kubectl",
			"logs",
			collector.pod,
			"--since-time",
			since,
			"--tail",
			tailMaxNumberOfLines,
		)
	}

	return PipeCommandOutputToFile(command, outputHandle)
}
