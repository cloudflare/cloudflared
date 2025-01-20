package config

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/watcher"
)

type mockNotifier struct {
	configs []Root
}

func (n *mockNotifier) ConfigDidUpdate(c Root) {
	n.configs = append(n.configs, c)
}

type mockFileWatcher struct {
	path     string
	notifier watcher.Notification
	ready    chan struct{}
}

func (w *mockFileWatcher) Start(n watcher.Notification) {
	w.notifier = n
	w.ready <- struct{}{}
}

func (w *mockFileWatcher) Add(string) error {
	return nil
}

func (w *mockFileWatcher) Shutdown() {

}

func (w *mockFileWatcher) TriggerChange() {
	w.notifier.WatcherItemDidChange(w.path)
}

func TestConfigChanged(t *testing.T) {
	filePath := "config.yaml"
	f, err := os.Create(filePath)
	assert.NoError(t, err)
	defer func() {
		_ = f.Close()
		_ = os.Remove(filePath)
	}()
	c := &Root{
		Forwarders: []Forwarder{
			{
				URL:      "test.daltoniam.com",
				Listener: "127.0.0.1:8080",
			},
		},
	}
	configRead := func(configPath string, log *zerolog.Logger) (Root, error) {
		return *c, nil
	}
	wait := make(chan struct{})
	w := &mockFileWatcher{path: filePath, ready: wait}

	log := zerolog.Nop()

	service, err := NewFileManager(w, filePath, &log)
	service.ReadConfig = configRead
	assert.NoError(t, err)

	n := &mockNotifier{}
	go service.Start(n)

	<-wait
	c.Forwarders = append(c.Forwarders, Forwarder{URL: "add.daltoniam.com", Listener: "127.0.0.1:8081"})
	w.TriggerChange()

	service.Shutdown()

	assert.Len(t, n.configs, 2, "did not get 2 config updates as expected")
	assert.Len(t, n.configs[0].Forwarders, 1, "not the amount of forwarders expected")
	assert.Len(t, n.configs[1].Forwarders, 2, "not the amount of forwarders expected")

	assert.Equal(t, n.configs[0].Forwarders[0].Hash(), c.Forwarders[0].Hash(), "forwarder hashes don't match")
	assert.Equal(t, n.configs[1].Forwarders[0].Hash(), c.Forwarders[0].Hash(), "forwarder hashes don't match")
	assert.Equal(t, n.configs[1].Forwarders[1].Hash(), c.Forwarders[1].Hash(), "forwarder hashes don't match")
}
