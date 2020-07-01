package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"

	homedir "github.com/mitchellh/go-homedir"
	"gopkg.in/urfave/cli.v2"
	"gopkg.in/urfave/cli.v2/altsrc"
	"gopkg.in/yaml.v2"

	"github.com/cloudflare/cloudflared/validation"
)

var (
	// DefaultConfigFiles is the file names from which we attempt to read configuration.
	DefaultConfigFiles = []string{"config.yml", "config.yaml"}

	// DefaultUnixConfigLocation is the primary location to find a config file
	DefaultUnixConfigLocation = "/usr/local/etc/cloudflared"

	// DefaultUnixLogLocation is the primary location to find log files
	DefaultUnixLogLocation = "/var/log/cloudflared"

	// Launchd doesn't set root env variables, so there is default
	// Windows default config dir was ~/cloudflare-warp in documentation; let's keep it compatible
	DefaultConfigDirs = []string{"~/.cloudflared", "~/.cloudflare-warp", "~/cloudflare-warp", "/etc/cloudflared", DefaultUnixConfigLocation}
)

const DefaultCredentialFile = "cert.pem"

// DefaultConfigDirectory returns the default directory of the config file
func DefaultConfigDirectory() string {
	if runtime.GOOS == "windows" {
		path := os.Getenv("CFDPATH")
		if path == "" {
			path = filepath.Join(os.Getenv("ProgramFiles(x86)"), "cloudflared")
			if _, err := os.Stat(path); os.IsNotExist(err) { //doesn't exist, so return an empty failure string
				return ""
			}
		}
		return path
	}
	return DefaultUnixConfigLocation
}

// DefaultLogDirectory returns the default directory for log files
func DefaultLogDirectory() string {
	if runtime.GOOS == "windows" {
		return DefaultConfigDirectory()
	}
	return DefaultUnixLogLocation
}

// DefaultConfigPath returns the default location of a config file
func DefaultConfigPath() string {
	dir := DefaultConfigDirectory()
	if dir == "" {
		return DefaultConfigFiles[0]
	}
	return filepath.Join(dir, DefaultConfigFiles[0])
}

// FileExists checks to see if a file exist at the provided path.
func FileExists(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// ignore missing files
			return false, nil
		}
		return false, err
	}
	f.Close()
	return true, nil
}

// FindInputSourceContext pulls the input source from the config flag.
func FindInputSourceContext(context *cli.Context) (altsrc.InputSourceContext, error) {
	if context.String("config") != "" {
		return altsrc.NewYamlSourceFromFile(context.String("config"))
	}
	return nil, nil
}

// FindDefaultConfigPath returns the first path that contains a config file.
// If none of the combination of DefaultConfigDirs and DefaultConfigFiles
// contains a config file, return empty string.
func FindDefaultConfigPath() string {
	for _, configDir := range DefaultConfigDirs {
		for _, configFile := range DefaultConfigFiles {
			dirPath, err := homedir.Expand(configDir)
			if err != nil {
				continue
			}
			path := filepath.Join(dirPath, configFile)
			if ok, _ := FileExists(path); ok {
				return path
			}
		}
	}
	return ""
}

// FindOrCreateConfigPath returns the first path that contains a config file
// or creates one in the primary default path if it doesn't exist
func FindOrCreateConfigPath() string {
	path := FindDefaultConfigPath()

	if path == "" {
		// create the default directory if it doesn't exist
		path = DefaultConfigPath()
		if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
			return ""
		}

		// write a new config file out
		file, err := os.Create(path)
		if err != nil {
			return ""
		}
		defer file.Close()

		logDir := DefaultLogDirectory()
		os.MkdirAll(logDir, os.ModePerm) //try and create it. Doesn't matter if it succeed or not, only byproduct will be no logs

		c := Root{
			LogDirectory: logDir,
		}
		if err := yaml.NewEncoder(file).Encode(&c); err != nil {
			return ""
		}
	}

	return path
}

// FindLogSettings gets the log directory and level from the config file
func FindLogSettings() (string, string) {
	configPath := FindOrCreateConfigPath()
	defaultDirectory := DefaultLogDirectory()
	defaultLevel := "info"

	file, err := os.Open(configPath)
	if err != nil {
		return defaultDirectory, defaultLevel
	}
	defer file.Close()

	var config Root
	if err := yaml.NewDecoder(file).Decode(&config); err != nil {
		return defaultDirectory, defaultLevel
	}

	directory := defaultDirectory
	if config.LogDirectory != "" {
		directory = config.LogDirectory
	}

	level := defaultLevel
	if config.LogLevel != "" {
		level = config.LogLevel
	}
	return directory, level
}

// ValidateUnixSocket ensures --unix-socket param is used exclusively
// i.e. it fails if a user specifies both --url and --unix-socket
func ValidateUnixSocket(c *cli.Context) (string, error) {
	if c.IsSet("unix-socket") && (c.IsSet("url") || c.NArg() > 0) {
		return "", errors.New("--unix-socket must be used exclusively.")
	}
	return c.String("unix-socket"), nil
}

// ValidateUrl will validate url flag correctness. It can be either from --url or argument
// Notice ValidateUnixSocket, it will enforce --unix-socket is not used with --url or argument
func ValidateUrl(c *cli.Context, allowFromArgs bool) (string, error) {
	var url = c.String("url")
	if allowFromArgs && c.NArg() > 0 {
		if c.IsSet("url") {
			return "", errors.New("Specified origin urls using both --url and argument. Decide which one you want, I can only support one.")
		}
		url = c.Args().Get(0)
	}
	validUrl, err := validation.ValidateUrl(url)
	return validUrl, err
}
