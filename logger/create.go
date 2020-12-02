package logger

import (
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
		defaultConfig.RollingConfig.Filename,
		logFile,
	)

	return newZerolog(loggerConfig)
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

func createRollingLogger(config RollingConfig) (io.Writer, error) {
	if err := os.MkdirAll(config.Directory, 0744); err != nil {
		return nil, err
	}

	return &lumberjack.Logger{
		Filename:   path.Join(config.Directory, config.Filename),
		MaxBackups: config.maxBackups,
		MaxSize:    config.maxSize,
		MaxAge:     config.maxAge,
	}, nil
}
