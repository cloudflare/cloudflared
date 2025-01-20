package main

import (
	"flag"
	"log"
	"os"
)

const (
	deprecatedFlagProgOnly = "prog_only"
	deprecatedFlagExecOnly = "exec_only"
)

var (
	_ = flag.Bool("prog_only", false, "DEPRECATED (reflect mode) Only generate the reflection program; write it to stdout and exit.")
	_ = flag.String("exec_only", "", "DEPRECATED (reflect mode) If set, execute this reflection program.")
)

// notifyAboutDeprecatedFlags prints a warning message for a deprecated flags if they are set.
func notifyAboutDeprecatedFlags() {
	const resetColorPostfix = "\033[0m"
	logger := initWarningLogger()

	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case deprecatedFlagProgOnly:
			logger.Println("The -prog_only flag is deprecated and has no effect.", resetColorPostfix)
		case deprecatedFlagExecOnly:
			logger.Println("The -exec_only flag is deprecated and has no effect.", resetColorPostfix)
		}
	})
}

func initWarningLogger() *log.Logger {
	const (
		yellowColor   = "\033[33m"
		warningPrefix = yellowColor + "WARNING: "
	)

	return log.New(os.Stdout, warningPrefix, log.Ldate|log.Ltime)
}
