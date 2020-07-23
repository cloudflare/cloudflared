package logger

import "sync"

// SharedWriteManager is a package level variable to allows multiple loggers to use the same write manager.
// This is useful when multiple loggers will write to the same file to ensure they don't clobber each other.
var SharedWriteManager = NewWriteManager()

type writeData struct {
	target LogOutput
	data   []byte
}

// WriteManager is a logging service that handles managing multiple writing streams
type WriteManager struct {
	shutdown  chan struct{}
	writeChan chan writeData
	writers   map[string]Service
	wg        sync.WaitGroup
}

// NewWriteManager creates a write manager that implements OutputManager
func NewWriteManager() OutputManager {
	m := &WriteManager{
		shutdown:  make(chan struct{}),
		writeChan: make(chan writeData, 1000),
	}

	go m.run()
	return m
}

// Append adds a message to the writer runloop
func (m *WriteManager) Append(data []byte, target LogOutput) {
	m.wg.Add(1)
	m.writeChan <- writeData{data: data, target: target}
}

// Shutdown stops the sync manager service
func (m *WriteManager) Shutdown() {
	m.wg.Wait()
	close(m.shutdown)
	close(m.writeChan)
}

// run is the main runloop that schedules log messages
func (m *WriteManager) run() {
	for {
		select {
		case event, ok := <-m.writeChan:
			if ok {
				event.target.WriteLogLine(event.data)
				m.wg.Done()
			}
		case <-m.shutdown:
			return
		}
	}
}
