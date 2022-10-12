package config

import (
	"io"
	"os"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	yaml "gopkg.in/yaml.v3"

	"github.com/cloudflare/cloudflared/watcher"
)

// Notifier sends out config updates
type Notifier interface {
	ConfigDidUpdate(Root)
}

// Manager is the base functions of the config manager
type Manager interface {
	Start(Notifier) error
	Shutdown()
}

// FileManager watches the yaml config for changes
// sends updates to the service to reconfigure to match the updated config
type FileManager struct {
	watcher    watcher.Notifier
	notifier   Notifier
	configPath string
	log        *zerolog.Logger
	ReadConfig func(string, *zerolog.Logger) (Root, error)
}

// NewFileManager creates a config manager
func NewFileManager(watcher watcher.Notifier, configPath string, log *zerolog.Logger) (*FileManager, error) {
	m := &FileManager{
		watcher:    watcher,
		configPath: configPath,
		log:        log,
		ReadConfig: readConfigFromPath,
	}
	err := watcher.Add(configPath)
	return m, err
}

// Start starts the runloop to watch for config changes
func (m *FileManager) Start(notifier Notifier) error {
	m.notifier = notifier

	// update the notifier with a fresh config on start
	config, err := m.GetConfig()
	if err != nil {
		return err
	}
	notifier.ConfigDidUpdate(config)

	m.watcher.Start(m)
	return nil
}

// GetConfig reads the yaml file from the disk
func (m *FileManager) GetConfig() (Root, error) {
	return m.ReadConfig(m.configPath, m.log)
}

// Shutdown stops the watcher
func (m *FileManager) Shutdown() {
	m.watcher.Shutdown()
}

func readConfigFromPath(configPath string, log *zerolog.Logger) (Root, error) {
	if configPath == "" {
		return Root{}, errors.New("unable to find config file")
	}

	file, err := os.Open(configPath)
	if err != nil {
		return Root{}, err
	}
	defer file.Close()

	var config Root
	if err := yaml.NewDecoder(file).Decode(&config); err != nil {
		if err == io.EOF {
			log.Error().Msgf("Configuration file %s was empty", configPath)
			return Root{}, nil
		}
		return Root{}, errors.Wrap(err, "error parsing YAML in config file at "+configPath)
	}

	return config, nil
}

// File change notifications from the watcher

// WatcherItemDidChange triggers when the yaml config is updated
// sends the updated config to the service to reload its state
func (m *FileManager) WatcherItemDidChange(filepath string) {
	config, err := m.GetConfig()
	if err != nil {
		m.log.Err(err).Msg("Failed to read new config")
		return
	}
	m.log.Info().Msg("Config file has been updated")
	m.notifier.ConfigDidUpdate(config)
}

// WatcherDidError notifies of errors with the file watcher
func (m *FileManager) WatcherDidError(err error) {
	m.log.Err(err).Msg("Config watcher encountered an error")
}
