package logger

import (
	"fmt"
	"time"

	"github.com/acmacalister/skittles"
)

// Level of logging
type Level int

const (
	// InfoLevel is for standard log messages
	InfoLevel Level = iota

	// DebugLevel is for messages that are intended for purposes debugging only
	DebugLevel

	// ErrorLevel is for error message to indicte something has gone wrong
	ErrorLevel

	// FatalLevel is for error message that log and kill the program with an os.exit(1)
	FatalLevel
)

// Formatter is the base interface for formatting logging messages before writing them out
type Formatter interface {
	Timestamp(Level, time.Time) string // format the timestamp string
	Content(Level, string) string      // format content string (color for terminal, etc)
}

// DefaultFormatter writes a simple structure timestamp and the message per log line
type DefaultFormatter struct {
	format string
}

// NewDefaultFormatter creates the standard log formatter
// format is the time format to use for timestamp formatting
func NewDefaultFormatter(format string) Formatter {
	return &DefaultFormatter{
		format: format,
	}
}

// Timestamp formats a log line timestamp with a brackets around them
func (f *DefaultFormatter) Timestamp(l Level, d time.Time) string {
	if f.format == "" {
		return ""
	}
	return fmt.Sprintf("[%s]: ", d.Format(f.format))
}

// Content just writes the log line straight to the sources
func (f *DefaultFormatter) Content(l Level, c string) string {
	return c
}

// TerminalFormatter is setup for colored output
type TerminalFormatter struct {
	format string
}

// NewTerminalFormatter creates a Terminal formatter for colored output
// format is the time format to use for timestamp formatting
func NewTerminalFormatter(format string) Formatter {
	return &TerminalFormatter{
		format: format,
	}
}

// Timestamp returns the log level with a matching color to the log type
func (f *TerminalFormatter) Timestamp(l Level, d time.Time) string {
	t := ""
	switch l {
	case InfoLevel:
		t = skittles.Cyan("[INFO] ")
	case ErrorLevel:
		t = skittles.Red("[ERROR] ")
	case DebugLevel:
		t = skittles.Yellow("[DEBUG] ")
	case FatalLevel:
		t = skittles.Red("[FATAL] ")
	}
	return t
}

// Content just writes the log line straight to the sources
func (f *TerminalFormatter) Content(l Level, c string) string {
	return c
}
