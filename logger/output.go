package logger

import (
	"fmt"
	"io"
	"os"
	"time"
)

// provided for testing
var osExit = os.Exit

// OutputManager is used to sync data of Output
type OutputManager interface {
	Append([]byte, func([]byte))
	Shutdown()
}

// Service is the logging service that is either a group or single log writer
type Service interface {
	Error(message string)
	Info(message string)
	Debug(message string)
	Fatal(message string)

	Errorf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Debugf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
}

type sourceGroup struct {
	writer          io.Writer
	formatter       Formatter
	levelsSupported []Level
}

// OutputWriter is the standard logging implementation
type OutputWriter struct {
	groups     []sourceGroup
	syncWriter OutputManager
}

// NewOutputWriter create a new logger
func NewOutputWriter(syncWriter OutputManager) *OutputWriter {
	return &OutputWriter{
		syncWriter: syncWriter,
		groups:     make([]sourceGroup, 0),
	}
}

// Add a writer and formatter to output to
func (s *OutputWriter) Add(writer io.Writer, formatter Formatter, levels ...Level) {
	s.groups = append(s.groups, sourceGroup{writer: writer, formatter: formatter, levelsSupported: levels})
}

// Error writes an error to the logging sources
func (s *OutputWriter) Error(message string) {
	s.output(ErrorLevel, message)
}

// Info writes an info string to the logging sources
func (s *OutputWriter) Info(message string) {
	s.output(InfoLevel, message)
}

// Debug writes a debug string to the logging sources
func (s *OutputWriter) Debug(message string) {
	s.output(DebugLevel, message)
}

// Fatal writes a error string to the logging sources and runs does an os.exit()
func (s *OutputWriter) Fatal(message string) {
	s.output(FatalLevel, message)
	s.syncWriter.Shutdown() // waits for the pending logging to finish
	osExit(1)
}

// Errorf writes a formatted error to the logging sources
func (s *OutputWriter) Errorf(format string, args ...interface{}) {
	s.output(ErrorLevel, fmt.Sprintf(format, args...))
}

// Infof writes a formatted info statement to the logging sources
func (s *OutputWriter) Infof(format string, args ...interface{}) {
	s.output(InfoLevel, fmt.Sprintf(format, args...))
}

// Debugf writes a formatted debug statement to the logging sources
func (s *OutputWriter) Debugf(format string, args ...interface{}) {
	s.output(DebugLevel, fmt.Sprintf(format, args...))
}

// Fatalf writes a  writes a formatted error statement and runs does an os.exit()
func (s *OutputWriter) Fatalf(format string, args ...interface{}) {
	s.output(FatalLevel, fmt.Sprintf(format, args...))
	s.syncWriter.Shutdown() // waits for the pending logging to finish
	osExit(1)
}

// output does the actual write to the sync manager
func (s *OutputWriter) output(l Level, content string) {
	for _, group := range s.groups {
		if isSupported(group, l) {
			logLine := fmt.Sprintf("%s%s\n", group.formatter.Timestamp(l, time.Now()),
				group.formatter.Content(l, content))
			s.append(group, []byte(logLine))
		}
	}
}

func (s *OutputWriter) append(group sourceGroup, logLine []byte) {
	s.syncWriter.Append(logLine, func(b []byte) {
		group.writer.Write(b)
	})
}

// isSupported checks if the log level is supported
func isSupported(group sourceGroup, l Level) bool {
	for _, level := range group.levelsSupported {
		if l == level {
			return true
		}
	}
	return false
}

// Write implements io.Writer to support SetOutput of the log package
func (s *OutputWriter) Write(p []byte) (n int, err error) {
	s.Info(string(p))
	return len(p), nil
}
