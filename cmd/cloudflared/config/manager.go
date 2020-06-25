package config

import (
	"errors"
	"os"

	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/watcher"
	"gopkg.in/yaml.v2"
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
	logger     logger.Service
	ReadConfig func(string) (Root, error)
}

// NewFileManager creates a config manager
func NewFileManager(watcher watcher.Notifier, configPath string, logger logger.Service) (*FileManager, error) {
	m := &FileManager{
		watcher:    watcher,
		configPath: configPath,
		logger:     logger,
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
	return m.ReadConfig(m.configPath)
}

// Shutdown stops the watcher
func (m *FileManager) Shutdown() {
	m.watcher.Shutdown()
}

func readConfigFromPath(configPath string) (Root, error) {
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
		return Root{}, err
	}

	return config, nil
}

// File change notifications from the watcher

// WatcherItemDidChange triggers when the yaml config is updated
// sends the updated config to the service to reload its state
func (m *FileManager) WatcherItemDidChange(filepath string) {
	config, err := m.GetConfig()
	if err != nil {
		m.logger.Errorf("Failed to read new config: %s", err)
		return
	}
	m.logger.Info("Config file has been updated")
	m.notifier.ConfigDidUpdate(config)
}

// WatcherDidError notifies of errors with the file watcher
func (m *FileManager) WatcherDidError(err error) {
	m.logger.Errorf("Config watcher encountered an error: %s", err)
}
