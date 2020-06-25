package sshlog

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/logger"
)

const logFileName = "test-logger.log"

func createLogger(t *testing.T) *Logger {
	os.Remove(logFileName)
	l := logger.NewOutputWriter(logger.NewMockWriteManager())
	logger, err := NewLogger(logFileName, l, time.Millisecond, 1024)
	if err != nil {
		t.Fatal("couldn't create the logger!", err)
	}
	return logger
}

// AUTH-2115 TODO: fix this test
//func TestWrite(t *testing.T) {
//	testStr := "hi"
//	logger := createLogger(t)
//	defer func() {
//		logger.Close()
//		os.Remove(logFileName)
//	}()
//
//	logger.Write([]byte(testStr))
//	time.DelayBeforeReconnect(2 * time.Millisecond)
//	data, err := ioutil.ReadFile(logFileName)
//	if err != nil {
//		t.Fatal("couldn't read the log file!", err)
//	}
//	checkStr := string(data)
//	if checkStr != testStr {
//		t.Fatal("file data doesn't match!")
//	}
//}

func TestFilenameRotation(t *testing.T) {
	newName := rotationName("dir/bob/acoolloggername.log")

	dir := filepath.Dir(newName)
	if dir != "dir/bob" {
		t.Fatal("rotation name doesn't respect the directory filepath:", newName)
	}

	filename := filepath.Base(newName)
	if !strings.HasPrefix(filename, "acoolloggername") {
		t.Fatal("rotation filename is wrong:", filename)
	}

	ext := filepath.Ext(newName)
	if ext != ".log" {
		t.Fatal("rotation file extension is wrong:", ext)
	}
}

func TestRotation(t *testing.T) {
	logger := createLogger(t)

	for i := 0; i < 2000; i++ {
		logger.Write([]byte("a string for testing rotation\n"))
	}
	logger.Close()

	count := 0
	filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasPrefix(info.Name(), "test-logger") {
			log.Println("deleting: ", path)
			os.Remove(path)
			count++
		}
		return nil
	})
	if count < 2 {
		t.Fatal("rotation didn't roll files:", count)
	}

}
