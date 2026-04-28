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
	namespace   string // This member identifies the namespace where the pod is deployed
}

func NewKubernetesLogCollector(containerID, pod string, namespace ...string) *KubernetesLogCollector {
    ns := "default"

	if len(namespace) > 0 && namespace[0] != "" {
		ns = namespace[0]
	}

	return &KubernetesLogCollector{
		containerID: containerID,
		pod:         pod,
		namespace:   ns
	}
}

func (collector *KubernetesLogCollector) Collect(ctx context.Context) (*LogInformation, error) {
	tmp := os.TempDir()
	outputHandle, err := os.Create(filepath.Join(tmp, logFilename))
	if err != nil {
		return nil, fmt.Errorf("error opening output file: %w", err)
	}

	defer outputHandle.Close()

	var command *exec.Cmd
	// Calculate 2 weeks ago
	since := time.Now().Add(twoWeeksOffset).Format(time.RFC3339)
	if collector.containerID != "" {
		command = exec.CommandContext(
			ctx,
			"kubectl",
			"logs",
			"-n",
			collector.namespace,
			collector.pod,
			"--since-time",
			since,
			"--tail",
			tailMaxNumberOfLines,
			"-c",
			collector.containerID,
		)
	} else {
		command = exec.CommandContext(
			ctx,
			"kubectl",
			"logs",
			"-n",
			collector.namespace,
			collector.pod,
			"--since-time",
			since,
			"--tail",
			tailMaxNumberOfLines,
		)
	}

	return PipeCommandOutputToFile(command, outputHandle)
}
