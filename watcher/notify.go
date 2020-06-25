package watcher

// Notification is the delegate methods from the Notifier
type Notification interface {
	WatcherItemDidChange(string)
	WatcherDidError(error)
}

// Notifier is the base interface for file watching
type Notifier interface {
	Start(Notification)
	Add(string) error
	Shutdown()
}
