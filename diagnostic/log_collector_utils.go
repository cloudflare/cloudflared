package diagnostic

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

func PipeCommandOutputToFile(command *exec.Cmd, outputHandle *os.File) (*LogInformation, error) {
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
