package diagnostic

import (
	"context"
)

// Represents the path of the log file or log directory.
// This struct is meant to give some ergonimics regarding
// the logging information.
type LogInformation struct {
	path        string // path to a file or directory
	wasCreated  bool   // denotes if `path` was created
	isDirectory bool   // denotes if `path` is a directory
}

func NewLogInformation(
	path string,
	wasCreated bool,
	isDirectory bool,
) *LogInformation {
	return &LogInformation{
		path,
		wasCreated,
		isDirectory,
	}
}

type LogCollector interface {
	// This function is responsible for returning a path to a single file
	// whose contents are the logs of a cloudflared instance.
	// A new file may be create by a LogCollector, thus, its the caller
	// responsibility to remove the newly create file.
	Collect(ctx context.Context) (*LogInformation, error)
}
