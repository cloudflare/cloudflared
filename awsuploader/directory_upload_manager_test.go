package awsuploader

import (
	"errors"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/logger"
)

type MockUploader struct {
	shouldFail bool
}

func (m *MockUploader) Upload(filepath string) error {
	if m.shouldFail {
		return errors.New("upload set to fail")
	}
	return nil
}

func NewMockUploader(shouldFail bool) Uploader {
	return &MockUploader{shouldFail: shouldFail}
}

func getDirectoryPath(t *testing.T) string {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal("couldn't create the test directory!", err)
	}
	return filepath.Join(dir, "uploads")
}

func setupTestDirectory(t *testing.T) string {
	path := getDirectoryPath(t)
	os.RemoveAll(path)
	time.Sleep(100 * time.Millisecond) //short way to wait for the OS to delete the folder
	err := os.MkdirAll(path, os.ModePerm)
	if err != nil {
		t.Fatal("couldn't create the test directory!", err)
	}
	return path
}

func createUploadManager(t *testing.T, shouldFailUpload bool) *DirectoryUploadManager {
	rootDirectory := setupTestDirectory(t)
	uploader := NewMockUploader(shouldFailUpload)
	logger := logger.NewOutputWriter(logger.NewMockWriteManager())
	shutdownC := make(chan struct{})
	return NewDirectoryUploadManager(logger, uploader, rootDirectory, 1*time.Second, shutdownC)
}

func createFile(t *testing.T, fileName string) (*os.File, string) {
	path := filepath.Join(getDirectoryPath(t), fileName)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal("upload to create file for sweep test", err)
	}
	return f, path
}

func TestUploadSuccess(t *testing.T) {
	manager := createUploadManager(t, false)
	path := filepath.Join(getDirectoryPath(t), "test_file")
	if err := manager.Upload(path); err != nil {
		t.Fatal("the upload request method failed", err)
	}
}

func TestUploadFailure(t *testing.T) {
	manager := createUploadManager(t, true)
	path := filepath.Join(getDirectoryPath(t), "test_file")
	if err := manager.Upload(path); err == nil {
		t.Fatal("the upload request method should have failed and didn't", err)
	}
}

func TestSweepSuccess(t *testing.T) {
	manager := createUploadManager(t, false)
	f, path := createFile(t, "test_file")
	defer f.Close()

	manager.Start()
	time.Sleep(2 * time.Second)
	if _, err := os.Stat(path); os.IsExist(err) {
		//the file should have been deleted
		t.Fatal("the manager failed to delete the file", err)
	}
}

func TestSweepFailure(t *testing.T) {
	manager := createUploadManager(t, true)
	f, path := createFile(t, "test_file")
	defer f.Close()

	manager.Start()
	time.Sleep(2 * time.Second)
	_, serr := f.Stat()
	if serr != nil {
		//the file should still exist
		os.Remove(path)
		t.Fatal("the manager failed to delete the file", serr)
	}
}

func TestHighLoad(t *testing.T) {
	manager := createUploadManager(t, false)
	for i := 0; i < 30; i++ {
		f, _ := createFile(t, randomString(6))
		defer f.Close()
	}
	manager.Start()
	time.Sleep(4 * time.Second)

	directory := getDirectoryPath(t)
	files, err := ioutil.ReadDir(directory)
	if err != nil || len(files) > 0 {
		t.Fatalf("the manager failed to upload all the files: %s files left: %d", err, len(files))
	}
}

// LowerCase [a-z]
const randSet = "abcdefghijklmnopqrstuvwxyz"

// String returns a string of length 'n' from a set of letters 'lset'
func randomString(n int) string {
	b := make([]byte, n)
	lsetLen := len(randSet)
	for i := range b {
		b[i] = randSet[rand.Intn(lsetLen)]
	}
	return string(b)
}
