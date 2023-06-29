package logger

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/mattn/go-colorable"
	"github.com/rs/zerolog"
	fallbacklog "github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"golang.org/x/term"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/cloudflare/cloudflared/features"
	"github.com/cloudflare/cloudflared/management"
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

	consoleTimeFormat = time.RFC3339
)

var (
	ManagementLogger *management.Logger
)

func init() {
	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.TimestampFunc = utcNow

	if features.Contains(features.FeatureManagementLogs) {
		// Management logger needs to be initialized before any of the other loggers as to not capture
		// it's own logging events.
		ManagementLogger = management.NewLogger()
	}
}

func utcNow() time.Time {
	return time.Now().UTC()
}

func fallbackLogger(err error) *zerolog.Logger {
	failLog := fallbacklog.With().Logger()
	fallbacklog.Error().Msgf("Falling back to a default logger due to logger setup failure: %s", err)

	return &failLog
}

// resilientMultiWriter is an alternative to zerolog's so that we can make it resilient to individual
// writer's errors. E.g., when running as a Windows service, the console writer fails, but we don't want to
// allow that to prevent all logging to fail due to breaking the for loop upon an error.
type resilientMultiWriter struct {
	level            zerolog.Level
	writers          []io.Writer
	managementWriter zerolog.LevelWriter
}

func (t resilientMultiWriter) Write(p []byte) (n int, err error) {
	for _, w := range t.writers {
		_, _ = w.Write(p)
	}
	if t.managementWriter != nil {
		_, _ = t.managementWriter.Write(p)
	}
	return len(p), nil
}

func (t resilientMultiWriter) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	// Only write the event to normal writers if it exceeds the level, but always write to the
	// management logger and let it decided with the provided level of the log event.
	if t.level <= level {
		for _, w := range t.writers {
			_, _ = w.Write(p)
		}
	}
	if t.managementWriter != nil {
		_, _ = t.managementWriter.WriteLevel(level, p)
	}
	return len(p), nil
}

var levelErrorLogged = false

func newZerolog(loggerConfig *Config) *zerolog.Logger {
	var writers []io.Writer

	if loggerConfig.ConsoleConfig != nil {
		writers = append(writers, createConsoleLogger(*loggerConfig.ConsoleConfig))
	}

	if loggerConfig.FileConfig != nil {
		fileLogger, err := createFileWriter(*loggerConfig.FileConfig)
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

	var managementWriter zerolog.LevelWriter
	if features.Contains(features.FeatureManagementLogs) {
		managementWriter = ManagementLogger
	}

	level, levelErr := zerolog.ParseLevel(loggerConfig.MinLevel)
	if levelErr != nil {
		level = zerolog.InfoLevel
	}

	multi := resilientMultiWriter{level, writers, managementWriter}
	log := zerolog.New(multi).With().Timestamp().Logger()
	if !levelErrorLogged && levelErr != nil {
		log.Error().Msgf("Failed to parse log level %q, using %q instead", loggerConfig.MinLevel, level)
		levelErrorLogged = true
	}

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
		log.Error().Msgf("Your config includes values for both %s (%s) and %s (%s), but they are incompatible. %s takes precedence.", LogFileFlag, logFile, logDirectoryFlagName, logDirectory, LogFileFlag)
	}
	return log
}

func Create(loggerConfig *Config) *zerolog.Logger {
	if loggerConfig == nil {
		loggerConfig = &Config{
			defaultConfig.ConsoleConfig,
			nil,
			nil,
			defaultConfig.MinLevel,
		}
	}
	return newZerolog(loggerConfig)
}

func createConsoleLogger(config ConsoleConfig) io.Writer {
	consoleOut := os.Stderr
	return zerolog.ConsoleWriter{
		Out:        colorable.NewColorable(consoleOut),
		NoColor:    config.noColor || !term.IsTerminal(int(consoleOut.Fd())),
		TimeFormat: consoleTimeFormat,
	}
}

type fileInitializer struct {
	once          sync.Once
	writer        io.Writer
	creationError error
}

var (
	singleFileInit   fileInitializer
	rotatingFileInit fileInitializer
)

func createFileWriter(config FileConfig) (io.Writer, error) {
	singleFileInit.once.Do(func() {

		var logFile io.Writer
		fullpath := config.Fullpath()

		// Try to open the existing file
		logFile, err := os.OpenFile(fullpath, os.O_APPEND|os.O_WRONLY, filePermMode)
		if err != nil {
			// If the existing file wasn't found, or couldn't be opened, just ignore
			// it and recreate a new one.
			logFile, err = createDirFile(config)
			// If creating a new logfile fails, then we have no choice but to error out.
			if err != nil {
				singleFileInit.creationError = err
				return
			}
		}

		singleFileInit.writer = logFile
	})

	return singleFileInit.writer, singleFileInit.creationError
}

func createDirFile(config FileConfig) (io.Writer, error) {
	if config.Dirname != "" {
		err := os.MkdirAll(config.Dirname, dirPermMode)

		if err != nil {
			return nil, fmt.Errorf("unable to create directories for new logfile: %s", err)
		}
	}

	mode := os.FileMode(filePermMode)

	fullPath := filepath.Join(config.Dirname, config.Filename)
	logFile, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, mode)
	if err != nil {
		return nil, fmt.Errorf("unable to create a new logfile: %s", err)
	}

	return logFile, nil
}

func createRollingLogger(config RollingConfig) (io.Writer, error) {
	rotatingFileInit.once.Do(func() {
		if err := os.MkdirAll(config.Dirname, dirPermMode); err != nil {
			rotatingFileInit.creationError = err
			return
		}

		rotatingFileInit.writer = &lumberjack.Logger{
			Filename:   path.Join(config.Dirname, config.Filename),
			MaxBackups: config.maxBackups,
			MaxSize:    config.maxSize,
			MaxAge:     config.maxAge,
		}
	})

	return rotatingFileInit.writer, rotatingFileInit.creationError
}
