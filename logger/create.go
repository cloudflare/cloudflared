package logger

import (
	"fmt"
	"io"
	"os"
	"path"

	"github.com/rs/zerolog"
	fallbacklog "github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	EnableTerminalLog  = false
	DisableTerminalLog = true

	LogLevelFlag          = "loglevel"
	LogFileFlag           = "logfile"
	LogDirectoryFlag      = "log-directory"
	LogTransportLevelFlag = "transport-loglevel"

	LogSSHDirectoryFlag = "log-directory"
	LogSSHLevelFlag     = "log-level"

	dirPermMode  = 0744 // rwxr--r--
	filePermMode = 0644 // rw-r--r--
)

func fallbackLogger(err error) *zerolog.Logger {
	failLog := fallbacklog.With().Logger()
	fallbacklog.Error().Msgf("Falling back to a default logger due to logger setup failure: %s", err)

	return &failLog
}

func newZerolog(loggerConfig *Config) *zerolog.Logger {
	var writers []io.Writer

	if loggerConfig.ConsoleConfig != nil {
		writers = append(writers, createConsoleLogger(*loggerConfig.ConsoleConfig))
	}

	if loggerConfig.FileConfig != nil {
		fileLogger, err := createFileLogger(*loggerConfig.FileConfig)
		if err != nil {
			return fallbackLogger(err)
		}

		writers = append(writers, fileLogger)
	}

	if loggerConfig.RollingConfig != nil {
		rollingLogger, err := createRollingLogger(*loggerConfig.RollingConfig)
		if err != nil {
			return fallbackLogger(err)
		}

		writers = append(writers, rollingLogger)
	}

	multi := zerolog.MultiLevelWriter(writers...)

	level, err := zerolog.ParseLevel(loggerConfig.MinLevel)
	if err != nil {
		return fallbackLogger(err)
	}
	log := zerolog.New(multi).With().Timestamp().Logger().Level(level)

	return &log
}

func CreateTransportLoggerFromContext(c *cli.Context, disableTerminal bool) *zerolog.Logger {
	return createFromContext(c, LogTransportLevelFlag, LogDirectoryFlag, disableTerminal)
}

func CreateLoggerFromContext(c *cli.Context, disableTerminal bool) *zerolog.Logger {
	return createFromContext(c, LogLevelFlag, LogDirectoryFlag, disableTerminal)
}

func CreateSSHLoggerFromContext(c *cli.Context, disableTerminal bool) *zerolog.Logger {
	return createFromContext(c, LogSSHLevelFlag, LogSSHDirectoryFlag, disableTerminal)
}

func createFromContext(
	c *cli.Context,
	logLevelFlagName,
	logDirectoryFlagName string,
	disableTerminal bool,
) *zerolog.Logger {
	logLevel := c.String(logLevelFlagName)
	logFile := c.String(LogFileFlag)
	logDirectory := c.String(logDirectoryFlagName)

	loggerConfig := CreateConfig(
		logLevel,
		disableTerminal,
		logDirectory,
		logFile,
	)

	log := newZerolog(loggerConfig)
	if incompatibleFlagsSet := logFile != "" && logDirectory != ""; incompatibleFlagsSet {
		log.Error().Msgf("Your config includes values for both %s and %s, but they are incompatible. %s takes precedence.", LogFileFlag, logDirectoryFlagName, LogFileFlag)
	}
	return log
}

func Create(loggerConfig *Config) *zerolog.Logger {
	if loggerConfig == nil {
		loggerConfig = &defaultConfig
	}

	return newZerolog(loggerConfig)
}

func createConsoleLogger(config ConsoleConfig) io.Writer {
	return zerolog.ConsoleWriter{
		Out:     os.Stderr,
		NoColor: config.noColor,
	}
}

func createFileLogger(config FileConfig) (io.Writer, error) {
	var logFile io.Writer
	fullpath := config.Fullpath()

	// Try to open the existing file
	logFile, err := os.OpenFile(fullpath, os.O_APPEND|os.O_WRONLY, filePermMode)
	if err != nil {
		// If the existing file wasn't found, or couldn't be opened, just ignore
		// it and recreate a new one.
		logFile, err = createLogFile(config)
		// If creating a new logfile fails, then we have no choice but to error out.
		if err != nil {
			return nil, err
		}
	}

	fileLogger := zerolog.New(logFile).With().Logger()

	return fileLogger, nil
}

func createLogFile(config FileConfig) (io.Writer, error) {
	if config.Dirname != "" {
		err := os.MkdirAll(config.Dirname, dirPermMode)

		if err != nil {
			return nil, fmt.Errorf("unable to create directories for new logfile: %s", err)
		}
	}

	mode := os.FileMode(filePermMode)

	logFile, err := os.OpenFile(config.Filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return nil, fmt.Errorf("unable to create a new logfile: %s", err)
	}

	return logFile, nil
}

func createRollingLogger(config RollingConfig) (io.Writer, error) {
	if err := os.MkdirAll(config.Dirname, dirPermMode); err != nil {
		return nil, err
	}

	return &lumberjack.Logger{
		Filename:   path.Join(config.Dirname, config.Filename),
		MaxBackups: config.maxBackups,
		MaxSize:    config.maxSize,
		MaxAge:     config.maxAge,
	}, nil
}
