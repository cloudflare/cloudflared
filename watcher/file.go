package watcher

import (
	"github.com/fsnotify/fsnotify"
)

// File is a file watcher that notifies when a file has been changed
type File struct {
	watcher  *fsnotify.Watcher
	shutdown chan struct{}
}

// NewFile is a standard constructor
func NewFile() (*File, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	f := &File{
		watcher:  watcher,
		shutdown: make(chan struct{}),
	}
	return f, nil
}

// Add adds a file to start watching
func (f *File) Add(filepath string) error {
	return f.watcher.Add(filepath)
}

// Shutdown stop the file watching run loop
func (f *File) Shutdown() {
	// don't block if Start quit early
	select {
	case f.shutdown <- struct{}{}:
	default:
	}
}

// Start is a runloop to watch for files changes from the file paths added from Add()
func (f *File) Start(notifier Notification) {
	for {
		select {
		case event, ok := <-f.watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				notifier.WatcherItemDidChange(event.Name)
			}
		case err, ok := <-f.watcher.Errors:
			if !ok {
				return
			}
			notifier.WatcherDidError(err)

		case <-f.shutdown:
			f.watcher.Close()
			return
		}
	}
}
