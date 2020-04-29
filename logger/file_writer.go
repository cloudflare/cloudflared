package logger

import (
	"fmt"
	"os"
	"path/filepath"
)

// FileRollingWriter maintains a set of log files numbered in order
// to keep a subset of log data to ensure it doesn't grow pass defined limits
type FileRollingWriter struct {
	baseFileName string
	directory    string
	maxFileSize  int64
	maxFileCount uint
	fileHandle   *os.File
}

// NewFileRollingWriter creates a new rolling file writer.
// directory is the working directory for the files
// baseFileName is the log file name. This writer appends .log to the name for the file name
// maxFileSize is the size in bytes of how large each file can be. Not a hard limit, general limit based after each write
// maxFileCount is the number of rolled files to keep.
func NewFileRollingWriter(directory, baseFileName string, maxFileSize int64, maxFileCount uint) *FileRollingWriter {
	return &FileRollingWriter{
		directory:    directory,
		baseFileName: baseFileName,
		maxFileSize:  maxFileSize,
		maxFileCount: maxFileCount,
	}
}

// Write is an implementation of io.writer the rolls the file once it reaches its max size
// It is expected the caller to Write is doing so in a thread safe manner (as WriteManager does).
func (w *FileRollingWriter) Write(p []byte) (n int, err error) {
	logFile := buildPath(w.directory, w.baseFileName)
	if w.fileHandle == nil {
		h, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0664)
		if err != nil {
			return 0, err
		}
		w.fileHandle = h
	}

	// get size for rolling check
	info, err := w.fileHandle.Stat()
	if err != nil {
		// failed to stat the file. Close the file handle and attempt to open a new handle on the next write
		w.Close()
		w.fileHandle = nil
		return 0, err
	}

	// write to the file
	written, err := w.fileHandle.Write(p)

	// check if the file needs to be rolled
	if err == nil && info.Size()+int64(written) > w.maxFileSize {
		// close the file handle than do the renaming. A new one will be opened on the next write
		w.Close()
		w.rename(logFile, 1)
	}

	return written, err
}

// Close closes the file handle if it is open
func (w *FileRollingWriter) Close() {
	if w.fileHandle != nil {
		w.fileHandle.Close()
		w.fileHandle = nil
	}
}

// rename is how the files are rolled. It works recursively to move the base log file to the rolled ones
// e.g. cloudflared.log -> cloudflared-1.log,
// but if cloudflared-1.log already exists, it is renamed to cloudflared-2.log,
// then the other files move in to their postion
func (w *FileRollingWriter) rename(sourcePath string, index uint) {
	destinationPath := buildPath(w.directory, fmt.Sprintf("%s-%d", w.baseFileName, index))

	// rolled to the max amount of files allowed on disk
	if index >= w.maxFileCount {
		os.Remove(destinationPath)
	}

	// if the rolled path already exist, rename it to cloudflared-2.log, then do this one.
	// recursive call since the oldest one needs to be renamed, before the newer ones can be moved
	if exists(destinationPath) {
		w.rename(destinationPath, index+1)
	}

	os.Rename(sourcePath, destinationPath)
}

func buildPath(directory, fileName string) string {
	return filepath.Join(directory, fileName+".log")
}

func exists(filePath string) bool {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return false
	}
	return true
}
