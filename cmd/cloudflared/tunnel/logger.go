package tunnel

import (
	"fmt"
	"os"

	"github.com/cloudflare/cloudflared/log"
	"github.com/rifflock/lfshook"
	"github.com/sirupsen/logrus"
	"gopkg.in/urfave/cli.v2"

	"github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
)

const debugLevelWarning = "At debug level, request URL, method, protocol, content legnth and header will be logged. " +
	"Response status, content length and header will also be logged in debug level."

var logger = log.CreateLogger()

func configMainLogger(c *cli.Context) error {
	logLevel, err := logrus.ParseLevel(c.String("loglevel"))
	if err != nil {
		logger.WithError(err).Error("Unknown logging level specified")
		return errors.Wrap(err, "Unknown logging level specified")
	}
	logger.SetLevel(logLevel)
	if logLevel == logrus.DebugLevel {
		logger.Warn(debugLevelWarning)
	}
	return nil
}

func configTransportLogger(c *cli.Context) (*logrus.Logger, error) {
	transportLogLevel, err := logrus.ParseLevel(c.String("transport-loglevel"))
	if err != nil {
		logger.WithError(err).Fatal("Unknown transport logging level specified")
		return nil, errors.Wrap(err, "Unknown transport logging level specified")
	}
	transportLogger := logrus.New()
	transportLogger.Level = transportLogLevel
	return transportLogger, nil
}

func initLogFile(c *cli.Context, loggers ...*logrus.Logger) error {
	filePath, err := homedir.Expand(c.String("logfile"))
	if err != nil {
		return errors.Wrap(err, "Cannot resolve logfile path")
	}

	fileMode := os.O_WRONLY | os.O_APPEND | os.O_CREATE | os.O_TRUNC
	// do not truncate log file if the client has been autoupdated
	if c.Bool("is-autoupdated") {
		fileMode = os.O_WRONLY | os.O_APPEND | os.O_CREATE
	}
	f, err := os.OpenFile(filePath, fileMode, 0664)
	if err != nil {
		errors.Wrap(err, fmt.Sprintf("Cannot open file %s", filePath))
	}
	defer f.Close()
	pathMap := lfshook.PathMap{
		logrus.DebugLevel: filePath,
		logrus.WarnLevel:  filePath,
		logrus.InfoLevel:  filePath,
		logrus.ErrorLevel: filePath,
		logrus.FatalLevel: filePath,
		logrus.PanicLevel: filePath,
	}

	for _, l := range loggers {
		l.Hooks.Add(lfshook.NewHook(pathMap, &logrus.JSONFormatter{}))
	}

	return nil
}
