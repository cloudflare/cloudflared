package diagnostic

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

func PipeCommandOutputToFile(command *exec.Cmd, outputHandle *os.File) (*LogInformation, error) {
	stdoutReader, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf(
			"error retrieving stdout from command '%s': %w",
			command.String(),
			err,
		)
	}

	stderrReader, err := command.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf(
			"error retrieving stderr from command '%s': %w",
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
			"error copying stdout from %s to file %s: %w",
			command.String(),
			outputHandle.Name(),
			err,
		)
	}

	_, err = io.Copy(outputHandle, stderrReader)
	if err != nil {
		return nil, fmt.Errorf(
			"error copying stderr from %s to file %s: %w",
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

func CopyFilesFromDirectory(path string) (string, error) {
	// rolling logs have as suffix the current date thus
	// when iterating the path files they are already in
	// chronological order
	files, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("error reading directory %s: %w", path, err)
	}

	outputHandle, err := os.Create(filepath.Join(os.TempDir(), logFilename))
	if err != nil {
		return "", fmt.Errorf("creating file %s: %w", outputHandle.Name(), err)
	}
	defer outputHandle.Close()

	for _, file := range files {
		logHandle, err := os.Open(filepath.Join(path, file.Name()))
		if err != nil {
			return "", fmt.Errorf("error opening file %s:%w", file.Name(), err)
		}
		defer logHandle.Close()

		_, err = io.Copy(outputHandle, logHandle)
		if err != nil {
			return "", fmt.Errorf("error copying file %s:%w", logHandle.Name(), err)
		}
	}

	logHandle, err := os.Open(filepath.Join(path, "cloudflared.log"))
	if err != nil {
		return "", fmt.Errorf("error opening file %s:%w", logHandle.Name(), err)
	}
	defer logHandle.Close()

	_, err = io.Copy(outputHandle, logHandle)
	if err != nil {
		return "", fmt.Errorf("error copying file %s:%w", logHandle.Name(), err)
	}

	return outputHandle.Name(), nil
}
