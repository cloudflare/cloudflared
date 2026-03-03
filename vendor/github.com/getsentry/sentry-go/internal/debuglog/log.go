package debuglog

import (
	"io"
	"log"
)

// logger is the global debug logger instance.
var logger = log.New(io.Discard, "[Sentry] ", log.LstdFlags)

// SetOutput changes the output destination of the logger.
func SetOutput(w io.Writer) {
	logger.SetOutput(w)
}

// GetLogger returns the current logger instance.
// This function is thread-safe and can be called concurrently.
func GetLogger() *log.Logger {
	return logger
}

// Printf calls Printf on the underlying logger.
func Printf(format string, args ...interface{}) {
	logger.Printf(format, args...)
}

// Println calls Println on the underlying logger.
func Println(args ...interface{}) {
	logger.Println(args...)
}

// Print calls Print on the underlying logger.
func Print(args ...interface{}) {
	logger.Print(args...)
}
