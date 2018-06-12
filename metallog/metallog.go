package metallog

import (
	tunnellog "github.com/cloudflare/cloudflared/log"
	log "github.com/sirupsen/logrus"
)

// New creates a logger formatted for JSON output
func New() *log.Logger {
	logger := log.New()
	logger.Formatter = &tunnellog.JSONFormatter{}
	return logger
}
