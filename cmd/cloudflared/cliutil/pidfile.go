package cliutil

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
	"github.com/rs/zerolog"
)

const (
	LogFieldPIDPathname  = "pidPathname"
	LogFieldExpandedPath = "expandedPath"
)

// WritePidFile writes the current process ID into the specified path.
func WritePidFile(pidPathname string, log *zerolog.Logger) error {
	expandedPath, err := homedir.Expand(pidPathname)
	if err != nil {
		log.Err(err).Str(LogFieldPIDPathname, pidPathname).Msg("Unable to expand the path, try to use absolute path in --pidfile")
		return err
	}
	cleanPath := filepath.Clean(expandedPath)
	file, err := os.Create(cleanPath)
	if err != nil {
		log.Err(err).Str(LogFieldExpandedPath, expandedPath).Msg("Unable to write pid")
		return err
	}
	defer func() { _ = file.Close() }()
	_, err = fmt.Fprintf(file, "%d", os.Getpid())
	return err
}
