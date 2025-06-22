package diagnostic

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	linuxManagedLogsPath          = "/var/log/cloudflared.err"
	darwinManagedLogsPath         = "/Library/Logs/com.cloudflare.cloudflared.err.log"
	linuxServiceConfigurationPath = "/etc/systemd/system/cloudflared.service"
	linuxSystemdPath              = "/run/systemd/system"
)

type HostLogCollector struct {
	client HTTPClient
}

func NewHostLogCollector(client HTTPClient) *HostLogCollector {
	return &HostLogCollector{
		client,
	}
}

func extractLogsFromJournalCtl(ctx context.Context) (*LogInformation, error) {
	tmp := os.TempDir()

	outputHandle, err := os.Create(filepath.Join(tmp, logFilename))
	if err != nil {
		return nil, fmt.Errorf("error opening output file: %w", err)
	}

	defer outputHandle.Close()

	command := exec.CommandContext(
		ctx,
		"journalctl",
		"--since",
		"2 weeks ago",
		"-u",
		"cloudflared.service",
	)

	return PipeCommandOutputToFile(command, outputHandle)
}

func getServiceLogPath() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		{
			path := darwinManagedLogsPath
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}

			userHomeDir, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("error getting user home: %w", err)
			}

			return filepath.Join(userHomeDir, darwinManagedLogsPath), nil
		}
	case "linux":
		{
			return linuxManagedLogsPath, nil
		}
	default:
		return "", ErrManagedLogNotFound
	}
}

func (collector *HostLogCollector) Collect(ctx context.Context) (*LogInformation, error) {
	logConfiguration, err := collector.client.GetLogConfiguration(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting log configuration: %w", err)
	}

	if logConfiguration.uid == 0 {
		_, statSystemdErr := os.Stat(linuxServiceConfigurationPath)

		_, statServiceConfigurationErr := os.Stat(linuxServiceConfigurationPath)
		if statSystemdErr == nil && statServiceConfigurationErr == nil && runtime.GOOS == "linux" {
			return extractLogsFromJournalCtl(ctx)
		}

		path, err := getServiceLogPath()
		if err != nil {
			return nil, err
		}

		return NewLogInformation(path, false, false), nil
	}

	if logConfiguration.logFile != "" {
		return NewLogInformation(logConfiguration.logFile, false, false), nil
	} else if logConfiguration.logDirectory != "" {
		return NewLogInformation(logConfiguration.logDirectory, false, true), nil
	}

	return nil, ErrLogConfigurationIsInvalid
}
