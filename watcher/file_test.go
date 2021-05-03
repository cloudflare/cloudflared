// +build !windows

package watcher

import (
	"bufio"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockNotifier struct {
	eventPath string
}

func (n *mockNotifier) WatcherItemDidChange(path string) {
	n.eventPath = path
}

func (n *mockNotifier) WatcherDidError(err error) {
}

func TestFileChanged(t *testing.T) {
	filePath := "test_file"
	f, err := os.Create(filePath)
	assert.NoError(t, err)
	defer func() {
		f.Close()
		os.Remove(filePath)
	}()

	service, err := NewFile()
	assert.NoError(t, err)

	err = service.Add(filePath)
	assert.NoError(t, err)

	n := &mockNotifier{}
	go service.Start(n)

	f.Sync()

	w := bufio.NewWriter(f)
	_, err = w.WriteString("hello Austin, do you like my file watcher?\n")
	assert.NoError(t, err)
	err = w.Flush()
	assert.NoError(t, err)

	// give it time to trigger
	time.Sleep(20 * time.Millisecond)
	service.Shutdown()

	assert.Equal(t, filePath, n.eventPath, "notifier didn't get an new file write event")
}
