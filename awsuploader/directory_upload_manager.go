package awsuploader

import (
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
)

// DirectoryUploadManager is used to manage file uploads on an interval from a directory
type DirectoryUploadManager struct {
	logger        *logrus.Logger
	uploader      Uploader
	rootDirectory string
	sweepInterval time.Duration
	ticker        *time.Ticker
	shutdownC     chan struct{}
	workQueue     chan string
}

// NewDirectoryUploadManager create a new DirectoryUploadManager
// uploader is an Uploader to use as an actual uploading engine
// directory is the directory to sweep for files to upload
// sweepInterval is how often to iterate the directory and upload the files within
func NewDirectoryUploadManager(logger *logrus.Logger, uploader Uploader, directory string, sweepInterval time.Duration, shutdownC chan struct{}) *DirectoryUploadManager {
	workerCount := 10
	manager := &DirectoryUploadManager{
		logger:        logger,
		uploader:      uploader,
		rootDirectory: directory,
		sweepInterval: sweepInterval,
		shutdownC:     shutdownC,
		workQueue:     make(chan string, workerCount),
	}

	//start workers
	for i := 0; i < workerCount; i++ {
		go manager.worker()
	}

	return manager
}

// Upload a file using the uploader
// This is useful for "out of band" uploads that need to be triggered immediately instead of waiting for the sweep
func (m *DirectoryUploadManager) Upload(filepath string) error {
	return m.uploader.Upload(filepath)
}

// Start the upload ticker to walk the directories
func (m *DirectoryUploadManager) Start() {
	m.ticker = time.NewTicker(m.sweepInterval)
	go m.run()
}

func (m *DirectoryUploadManager) run() {
	for {
		select {
		case <-m.shutdownC:
			m.ticker.Stop()
			return
		case <-m.ticker.C:
			m.sweep()
		}
	}
}

// sweep the directory and kick off uploads
func (m *DirectoryUploadManager) sweep() {
	filepath.Walk(m.rootDirectory, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		//30 days ago
		retentionTime := 30 * (time.Hour * 24)
		checkTime := time.Now().Add(-time.Duration(retentionTime))

		//delete the file it is stale
		if info.ModTime().After(checkTime) {
			os.Remove(path)
			return nil
		}
		//add the upload to the work queue
		go func() {
			m.workQueue <- path
		}()
		return nil
	})
}

// worker handles upload requests
func (m *DirectoryUploadManager) worker() {
	for {
		select {
		case <-m.shutdownC:
			return
		case filepath := <-m.workQueue:
			if err := m.Upload(filepath); err != nil {
				m.logger.WithError(err).Error("Cannot upload file to s3 bucket")
			} else {
				os.Remove(filepath)
			}
		}
	}
}
