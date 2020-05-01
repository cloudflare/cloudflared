// Package log implements a wrapper around the Go standard library's
// logging package. Clients should set the current log level; only
// messages below that level will actually be logged. For example, if
// Level is set to LevelWarning, only log messages at the Warning,
// Error, and Critical levels will be logged.
package log

import (
	"fmt"
	golog "log"
)

// The following constants represent logging levels in increasing levels of seriousness.
const (
	LevelDebug = iota
	LevelInfo
	LevelWarning
	LevelError
	LevelCritical
)

var levelPrefix = [...]string{
	LevelDebug:    "[DEBUG] ",
	LevelInfo:     "[INFO] ",
	LevelWarning:  "[WARNING] ",
	LevelError:    "[ERROR] ",
	LevelCritical: "[CRITICAL] ",
}

// Level stores the current logging level.
var Level = LevelDebug

func outputf(l int, format string, v []interface{}) {
	if l >= Level {
		golog.Printf(fmt.Sprint(levelPrefix[l], format), v...)
	}
}

func output(l int, v []interface{}) {
	if l >= Level {
		golog.Print(levelPrefix[l], fmt.Sprint(v...))
	}
}

// Criticalf logs a formatted message at the "critical" level. The
// arguments are handled in the same manner as fmt.Printf.
func Criticalf(format string, v ...interface{}) {
	outputf(LevelCritical, format, v)
}

// Critical logs its arguments at the "critical" level.
func Critical(v ...interface{}) {
	output(LevelCritical, v)
}

// Errorf logs a formatted message at the "error" level. The arguments
// are handled in the same manner as fmt.Printf.
func Errorf(format string, v ...interface{}) {
	outputf(LevelError, format, v)
}

// Error logs its arguments at the "error" level.
func Error(v ...interface{}) {
	output(LevelError, v)
}

// Warningf logs a formatted message at the "warning" level. The
// arguments are handled in the same manner as fmt.Printf.
func Warningf(format string, v ...interface{}) {
	outputf(LevelWarning, format, v)
}

// Warning logs its arguments at the "warning" level.
func Warning(v ...interface{}) {
	output(LevelWarning, v)
}

// Infof logs a formatted message at the "info" level. The arguments
// are handled in the same manner as fmt.Printf.
func Infof(format string, v ...interface{}) {
	outputf(LevelInfo, format, v)
}

// Info logs its arguments at the "info" level.
func Info(v ...interface{}) {
	output(LevelInfo, v)
}

// Debugf logs a formatted message at the "debug" level. The arguments
// are handled in the same manner as fmt.Printf.
func Debugf(format string, v ...interface{}) {
	outputf(LevelDebug, format, v)
}

// Debug logs its arguments at the "debug" level.
func Debug(v ...interface{}) {
	output(LevelDebug, v)
}
