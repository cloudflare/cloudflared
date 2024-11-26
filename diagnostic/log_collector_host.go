package diagnostic

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	linuxManagedLogsPath  = "/var/log/cloudflared.err"
	darwinManagedLogsPath = "/Library/Logs/com.cloudflare.cloudflared.err.log"
)

type HostLogCollector struct {
	client HTTPClient
}

func NewHostLogCollector(client HTTPClient) *HostLogCollector {
	return &HostLogCollector{
		client,
	}
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

	return nil, ErrMustNotBeEmpty
}
