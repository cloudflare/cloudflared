package config

import (
	"bufio"
	"os"
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/log"
	"github.com/cloudflare/cloudflared/watcher"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

type mockNotifier struct {
	configs []Root
}

func (n *mockNotifier) ConfigDidUpdate(c Root) {
	n.configs = append(n.configs, c)
}

func writeConfig(t *testing.T, f *os.File, c *Root) {
	f.Sync()
	b, err := yaml.Marshal(c)
	assert.NoError(t, err)

	w := bufio.NewWriter(f)
	_, err = w.Write(b)
	assert.NoError(t, err)

	err = w.Flush()
	assert.NoError(t, err)
}

func TestConfigChanged(t *testing.T) {
	filePath := "config.yaml"
	f, err := os.Create(filePath)
	assert.NoError(t, err)
	defer func() {
		f.Close()
		os.Remove(filePath)
	}()
	c := &Root{
		OrgKey:          "abcd",
		ConfigType:      "mytype",
		CheckinInterval: 1,
		Forwarders: []Forwarder{
			{
				URL:      "test.daltoniam.com",
				Listener: "127.0.0.1:8080",
			},
		},
	}
	writeConfig(t, f, c)

	w, err := watcher.NewFile()
	assert.NoError(t, err)
	logger := log.CreateLogger()
	service, err := NewFileManager(w, filePath, logger)
	assert.NoError(t, err)

	n := &mockNotifier{}
	go service.Start(n)

	c.Forwarders = append(c.Forwarders, Forwarder{URL: "add.daltoniam.com", Listener: "127.0.0.1:8081"})
	writeConfig(t, f, c)

	// give it time to trigger
	time.Sleep(10 * time.Millisecond)
	service.Shutdown()

	assert.Len(t, n.configs, 2, "did not get 2 config updates as expected")
	assert.Len(t, n.configs[0].Forwarders, 1, "not the amount of forwarders expected")
	assert.Len(t, n.configs[1].Forwarders, 2, "not the amount of forwarders expected")

	assert.Equal(t, n.configs[0].Forwarders[0].Hash(), c.Forwarders[0].Hash(), "forwarder hashes don't match")
	assert.Equal(t, n.configs[1].Forwarders[0].Hash(), c.Forwarders[0].Hash(), "forwarder hashes don't match")
	assert.Equal(t, n.configs[1].Forwarders[1].Hash(), c.Forwarders[1].Hash(), "forwarder hashes don't match")
}
