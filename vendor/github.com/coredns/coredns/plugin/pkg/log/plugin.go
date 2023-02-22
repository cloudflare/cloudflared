package log

import (
	"fmt"
	"os"
)

// P is a logger that includes the plugin doing the logging.
type P struct {
	plugin string
}

// NewWithPlugin returns a logger that includes "plugin/name: " in the log message.
// I.e [INFO] plugin/<name>: message.
func NewWithPlugin(name string) P { return P{"plugin/" + name + ": "} }

func (p P) logf(level, format string, v ...interface{}) {
	log(level, p.plugin, fmt.Sprintf(format, v...))
}

func (p P) log(level string, v ...interface{}) {
	log(level+p.plugin, v...)
}

// Debug logs as log.Debug.
func (p P) Debug(v ...interface{}) {
	if !D.Value() {
		return
	}
	ls.debug(p.plugin, v...)
	p.log(debug, v...)
}

// Debugf logs as log.Debugf.
func (p P) Debugf(format string, v ...interface{}) {
	if !D.Value() {
		return
	}
	ls.debugf(p.plugin, format, v...)
	p.logf(debug, format, v...)
}

// Info logs as log.Info.
func (p P) Info(v ...interface{}) {
	ls.info(p.plugin, v...)
	p.log(info, v...)
}

// Infof logs as log.Infof.
func (p P) Infof(format string, v ...interface{}) {
	ls.infof(p.plugin, format, v...)
	p.logf(info, format, v...)
}

// Warning logs as log.Warning.
func (p P) Warning(v ...interface{}) {
	ls.warning(p.plugin, v...)
	p.log(warning, v...)
}

// Warningf logs as log.Warningf.
func (p P) Warningf(format string, v ...interface{}) {
	ls.warningf(p.plugin, format, v...)
	p.logf(warning, format, v...)
}

// Error logs as log.Error.
func (p P) Error(v ...interface{}) {
	ls.error(p.plugin, v...)
	p.log(err, v...)
}

// Errorf logs as log.Errorf.
func (p P) Errorf(format string, v ...interface{}) {
	ls.errorf(p.plugin, format, v...)
	p.logf(err, format, v...)
}

// Fatal logs as log.Fatal and calls os.Exit(1).
func (p P) Fatal(v ...interface{}) {
	ls.fatal(p.plugin, v...)
	p.log(fatal, v...)
	os.Exit(1)
}

// Fatalf logs as log.Fatalf and calls os.Exit(1).
func (p P) Fatalf(format string, v ...interface{}) {
	ls.fatalf(p.plugin, format, v...)
	p.logf(fatal, format, v...)
	os.Exit(1)
}
