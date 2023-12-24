package tunnel

import (
	"os"
)

// Abstract away details of reading files, so that SubcommandContext can read
// from either the real filesystem, or a mock (when running unit tests).
type fileSystem interface {
	readFile(filePath string) ([]byte, error)
	validFilePath(path string) bool
}

type realFileSystem struct{}

func (fs realFileSystem) validFilePath(path string) bool {
	fileStat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !fileStat.IsDir()
}

func (fs realFileSystem) readFile(filePath string) ([]byte, error) {
	return os.ReadFile(filePath)
}
