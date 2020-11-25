package logger

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
}

type FileConfig struct {
	Filepath string
}

type RollingConfig struct {
	Directory string
	Filename  string

	maxSize    int // megabytes
	maxBackups int // files
	maxAge     int // days
}

func createDefaultConfig() Config {
	const minLevel = "info"

	const RollingMaxSize = 1    // Mb
	const RollingMaxBackups = 5 // files
	const RollingMaxAge = 0     // Keep forever
	const rollingLogFilename = "cloudflared.log"

	return Config{
		ConsoleConfig: &ConsoleConfig{
			noColor: false,
		},
		FileConfig: &FileConfig{
			Filepath: "",
		},
		RollingConfig: &RollingConfig{
			Directory:  "",
			Filename:   rollingLogFilename,
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
	rollingLogPath, rollingLogFilename, nonRollingLogFilePath string,
) *Config {
	var console *ConsoleConfig
	if !disableTerminal {
		console = createConsoleConfig()
	}

	var file *FileConfig
	if nonRollingLogFilePath != "" {
		file = createFileConfig(nonRollingLogFilePath)
	}

	var rolling *RollingConfig
	if rollingLogPath != "" {
		rolling = createRollingConfig(rollingLogPath, rollingLogFilename)
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

func createConsoleConfig() *ConsoleConfig {
	return &ConsoleConfig{
		noColor: false,
	}
}

func createFileConfig(filepath string) *FileConfig {
	if filepath == "" {
		filepath = defaultConfig.FileConfig.Filepath
	}

	return &FileConfig{
		Filepath: filepath,
	}
}

func createRollingConfig(directory, filename string) *RollingConfig {
	if directory == "" {
		directory = defaultConfig.RollingConfig.Directory
	}

	return &RollingConfig{
		Directory:  directory,
		Filename:   filename,
		maxSize:    defaultConfig.RollingConfig.maxSize,
		maxBackups: defaultConfig.RollingConfig.maxBackups,
		maxAge:     defaultConfig.RollingConfig.maxAge,
	}
}
