package main

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

var logger = log.CreateLogger()

func configMainLogger(c *cli.Context) {
	logLevel, err := logrus.ParseLevel(c.String("loglevel"))
	if err != nil {
		logger.WithError(err).Fatal("Unknown logging level specified")
	}
	logger.SetLevel(logLevel)
}

func configProtoLogger(c *cli.Context) *logrus.Logger {
	protoLogLevel, err := logrus.ParseLevel(c.String("proto-loglevel"))
	if err != nil {
		logger.WithError(err).Fatal("Unknown protocol logging level specified")
	}
	protoLogger := logrus.New()
	protoLogger.Level = protoLogLevel
	return protoLogger
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
