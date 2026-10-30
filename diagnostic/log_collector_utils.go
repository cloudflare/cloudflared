package diagnostic

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

func PipeCommandOutputToFile(command *exec.Cmd, outputHandle *os.File) (*LogInformation, error) {
	// Share the output file so neither stream can block on an unread pipe.
	command.Stdout = outputHandle
	command.Stderr = outputHandle

	if err := command.Start(); err != nil {
		return nil, fmt.Errorf(
			"error running command '%s': %w",
			command.String(),
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

func CopyFilesFromDirectory(path string) (string, error) {
	const defaultLogFilename = "cloudflared.log"

	// rolling logs have as suffix the current date thus
	// when iterating the path files they are already in
	// chronological order
	files, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("error reading directory %s: %w", path, err)
	}

	// nolint: gosec
	outputHandle, err := os.Create(filepath.Join(os.TempDir(), logFilename))
	if err != nil {
		return "", fmt.Errorf("creating temporary log file %s: %w", logFilename, err)
	}
	defer func() { _ = outputHandle.Close() }()

	for _, file := range files {
		// nolint: gosec
		logHandle, err := os.Open(filepath.Join(path, file.Name()))
		if err != nil {
			return "", fmt.Errorf("error opening file %s: %w", file.Name(), err)
		}
		_, err = io.Copy(outputHandle, logHandle)
		_ = logHandle.Close()
		if err != nil {
			return "", fmt.Errorf("error copying file %s: %w", file.Name(), err)
		}
	}

	// nolint: gosec
	logHandle, err := os.Open(filepath.Join(path, defaultLogFilename))
	if err != nil {
		return "", fmt.Errorf("error opening file %s:%w", defaultLogFilename, err)
	}
	defer func() { _ = logHandle.Close() }()

	_, err = io.Copy(outputHandle, logHandle)
	if err != nil {
		return "", fmt.Errorf("error copying file %s:%w", logHandle.Name(), err)
	}

	return outputHandle.Name(), nil
}
