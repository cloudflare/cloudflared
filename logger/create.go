package logger

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/units"
)

// Option is to encaspulate actions that will be called by Parse and run later to build an Options struct
type Option func(*Options) error

// Options is use to set logging configuration data
type Options struct {
	logFileDirectory        string
	maxFileSize             units.Base2Bytes
	maxFileCount            uint
	terminalOutputDisabled  bool
	supportedFileLevels     []Level
	supportedTerminalLevels []Level
}

// DisableTerminal stops terminal output for the logger
func DisableTerminal(disable bool) Option {
	return func(c *Options) error {
		c.terminalOutputDisabled = disable
		return nil
	}
}

// File sets a custom file to log events
func File(path string, size units.Base2Bytes, count uint) Option {
	return func(c *Options) error {
		c.logFileDirectory = path
		c.maxFileSize = size
		c.maxFileCount = count
		return nil
	}
}

// DefaultFile configures the log options will the defaults
func DefaultFile(directoryPath string) Option {
	return func(c *Options) error {
		size, err := units.ParseBase2Bytes("1MB")
		if err != nil {
			return err
		}

		c.logFileDirectory = directoryPath
		c.maxFileSize = size
		c.maxFileCount = 5
		return nil
	}
}

// SupportedFileLevels sets the supported logging levels for the log file
func SupportedFileLevels(supported []Level) Option {
	return func(c *Options) error {
		c.supportedFileLevels = supported
		return nil
	}
}

// SupportedTerminalevels sets the supported logging levels for the terminal output
func SupportedTerminalevels(supported []Level) Option {
	return func(c *Options) error {
		c.supportedTerminalLevels = supported
		return nil
	}
}

// LogLevelString sets the supported logging levels from a command line flag
func LogLevelString(level string) Option {
	return func(c *Options) error {
		supported, err := ParseLevelString(level)
		if err != nil {
			return err
		}
		c.supportedFileLevels = supported
		c.supportedTerminalLevels = supported
		return nil
	}
}

// Parse builds the Options struct so the caller knows what actions should be run
func Parse(opts ...Option) (*Options, error) {
	options := &Options{}
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, err
		}
	}
	return options, nil
}

// New setups a new logger based on the options.
// The default behavior is to write to standard out
func New(opts ...Option) (Service, error) {
	config, err := Parse(opts...)

	if err != nil {
		return nil, err
	}

	l := NewOutputWriter(SharedWriteManager)
	if config.logFileDirectory != "" {
		l.Add(NewFileRollingWriter(config.logFileDirectory,
			"cloudflared",
			int64(config.maxFileSize),
			config.maxFileCount),
			NewDefaultFormatter(time.RFC3339Nano), config.supportedFileLevels...)
	}

	if !config.terminalOutputDisabled {
		terminalFormatter := NewTerminalFormatter(time.RFC3339)

		if len(config.supportedTerminalLevels) == 0 {
			l.Add(os.Stderr, terminalFormatter, InfoLevel, ErrorLevel, FatalLevel)
		} else {
			l.Add(os.Stderr, terminalFormatter, config.supportedTerminalLevels...)
		}
	}
	return l, nil
}

// ParseLevelString returns the expected log levels based on the cmd flag
func ParseLevelString(lvl string) ([]Level, error) {
	switch strings.ToLower(lvl) {
	case "fatal":
		return []Level{FatalLevel}, nil
	case "error":
		return []Level{FatalLevel, ErrorLevel}, nil
	case "info", "warn":
		return []Level{FatalLevel, ErrorLevel, InfoLevel}, nil
	case "debug":
		return []Level{FatalLevel, ErrorLevel, InfoLevel, DebugLevel}, nil
	}
	return []Level{}, fmt.Errorf("not a valid log level: %q", lvl)
}
