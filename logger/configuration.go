package logger

import (
	"path/filepath"
)

var defaultConfig = createDefaultConfig()

// Logging configuration
type Config struct {
	ConsoleConfig *ConsoleConfig // If nil, the logger will not log into the console
	FileConfig    *FileConfig    // If nil, the logger will not use an individual log file
	RollingConfig *RollingConfig // If nil, the logger will not use a rolling log

	MinLevel string // debug | info | error | fatal
}

type ConsoleConfig struct {
	noColor bool
	asJSON  bool
}

type FileConfig struct {
	Dirname  string
	Filename string
}

func (fc *FileConfig) Fullpath() string {
	return filepath.Join(fc.Dirname, fc.Filename)
}

type RollingConfig struct {
	Dirname  string
	Filename string

	maxSize    int // megabytes
	maxBackups int // files
	maxAge     int // days
}

func createDefaultConfig() Config {
	const minLevel = "info"

	const RollingMaxSize = 1    // Mb
	const RollingMaxBackups = 5 // files
	const RollingMaxAge = 0     // Keep forever
	const defaultLogFilename = "cloudflared.log"

	return Config{
		ConsoleConfig: &ConsoleConfig{
			noColor: false,
			asJSON:  false,
		},
		FileConfig: &FileConfig{
			Dirname:  "",
			Filename: defaultLogFilename,
		},
		RollingConfig: &RollingConfig{
			Dirname:    "",
			Filename:   defaultLogFilename,
			maxSize:    RollingMaxSize,
			maxBackups: RollingMaxBackups,
			maxAge:     RollingMaxAge,
		},
		MinLevel: minLevel,
	}
}

func CreateConfig(
	minLevel string,
	disableTerminal bool,
	formatJSON bool,
	rollingLogPath, nonRollingLogFilePath string,
) *Config {
	var console *ConsoleConfig
	if !disableTerminal {
		console = createConsoleConfig(formatJSON)
	}

	var file *FileConfig
	var rolling *RollingConfig
	if nonRollingLogFilePath != "" {
		file = createFileConfig(nonRollingLogFilePath)
	} else if rollingLogPath != "" {
		rolling = createRollingConfig(rollingLogPath)
	}

	if minLevel == "" {
		minLevel = defaultConfig.MinLevel
	}

	return &Config{
		ConsoleConfig: console,
		FileConfig:    file,
		RollingConfig: rolling,

		MinLevel: minLevel,
	}
}

func createConsoleConfig(formatJSON bool) *ConsoleConfig {
	return &ConsoleConfig{
		noColor: false,
		asJSON:  formatJSON,
	}
}

func createFileConfig(fullpath string) *FileConfig {
	if fullpath == "" {
		return defaultConfig.FileConfig
	}

	dirname, filename := filepath.Split(fullpath)

	return &FileConfig{
		Dirname:  dirname,
		Filename: filename,
	}
}

func createRollingConfig(directory string) *RollingConfig {
	if directory == "" {
		directory = defaultConfig.RollingConfig.Dirname
	}

	return &RollingConfig{
		Dirname:    directory,
		Filename:   defaultConfig.RollingConfig.Filename,
		maxSize:    defaultConfig.RollingConfig.maxSize,
		maxBackups: defaultConfig.RollingConfig.maxBackups,
		maxAge:     defaultConfig.RollingConfig.maxAge,
	}
}
