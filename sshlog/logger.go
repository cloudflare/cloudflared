package sshlog

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	logTimeFormat = "2006-01-02T15-04-05.000"
	megabyte      = 1024 * 1024
)

// Logger will buffer and write events to disk
type Logger struct {
	sync.Mutex
	filename    string
	file        *os.File
	writeBuffer *bufio.Writer
	logger      *logrus.Logger
	done        chan struct{}
	once        sync.Once
}

// NewLogger creates a Logger instance. A buffer is created that needs to be
// drained and closed when the caller is finished, so instances should call
// Close when finished with this Logger instance. Writes will be flushed to disk
// every second (fsync). filename is the name of the logfile to be created. The
// logger variable is a logrus that will log all i/o, filesystem error etc, that
// that shouldn't end execution of the logger, but are useful to report to the
// caller.
func NewLogger(filename string, logger *logrus.Logger) (*Logger, error) {
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(0600))
	if err != nil {
		return nil, err
	}
	l := &Logger{filename: filename,
		file:        f,
		writeBuffer: bufio.NewWriter(f),
		logger:      logger,
		done:        make(chan struct{})}
	go l.writer()
	return l, nil
}

// Writes to a log buffer. Implements the io.Writer interface.
func (l *Logger) Write(p []byte) (n int, err error) {
	l.Lock()
	defer l.Unlock()
	return l.writeBuffer.Write(p)
}

// Close drains anything left in the buffer and cleans up any resources still
// in use.
func (l *Logger) Close() error {
	l.once.Do(func() {
		close(l.done)
	})
	if err := l.write(); err != nil {
		return err
	}
	return l.file.Close()
}

// writer is the run loop that handles draining the write buffer and syncing
// data to disk.
func (l *Logger) writer() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := l.write(); err != nil {
				l.logger.Errorln(err)
			}
		case <-l.done:
			return
		}
	}
}

// write does the actual system write calls to disk and does a rotation if the
// file size limit has been reached. Since the rotation happens at the end,
// the rotation is a soft limit (aka the file can be bigger than the max limit
// because of the final buffer flush)
func (l *Logger) write() error {
	l.Lock()
	defer l.Unlock()

	if l.writeBuffer.Buffered() <= 0 {
		return nil
	}

	if err := l.writeBuffer.Flush(); err != nil {
		return err
	}

	if err := l.file.Sync(); err != nil {
		return err
	}

	if l.shouldRotate() {
		return l.rotate()
	}
	return nil
}

// shouldRotate checks to see if the current file should be rotated to a new
// logfile.
func (l *Logger) shouldRotate() bool {
	info, err := l.file.Stat()
	if err != nil {
		return false
	}

	return info.Size() >= 100*megabyte
}

// rotate creates a new logfile with the existing filename and renames the
// existing file with a current timestamp.
func (l *Logger) rotate() error {
	if err := l.file.Close(); err != nil {
		return err
	}

	// move the existing file
	newname := rotationName(l.filename)
	if err := os.Rename(l.filename, newname); err != nil {
		return fmt.Errorf("can't rename log file: %s", err)
	}

	f, err := os.OpenFile(l.filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(0600))
	if err != nil {
		return fmt.Errorf("failed to open new logfile %s", err)
	}
	l.file = f
	l.writeBuffer = bufio.NewWriter(f)
	return nil
}

// rotationName creates a new filename from the given name, inserting a timestamp
// between the filename and the extension.
func rotationName(name string) string {
	dir := filepath.Dir(name)
	filename := filepath.Base(name)
	ext := filepath.Ext(filename)
	prefix := filename[:len(filename)-len(ext)]
	t := time.Now()
	timestamp := t.Format(logTimeFormat)
	return filepath.Join(dir, fmt.Sprintf("%s-%s%s", prefix, timestamp, ext))
}
