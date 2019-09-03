package sshlog

import (
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	capnp "zombiezen.com/go/capnproto2"
)

const sessionLogFileName = "test-session-logger.log"

func createSessionLogger(t *testing.T) *SessionLogger {
	os.Remove(sessionLogFileName)
	l := logrus.New()
	logger, err := NewSessionLogger(sessionLogFileName, l, time.Millisecond, 1024)
	if err != nil {
		t.Fatal("couldn't create the logger!", err)
	}
	return logger
}

func TestSessionLogWrite(t *testing.T) {
	testStr := "hi"
	logger := createSessionLogger(t)
	defer func() {
		logger.Close()
		os.Remove(sessionLogFileName)
	}()

	logger.Write([]byte(testStr))
	time.Sleep(2 * time.Millisecond)
	f, err := os.Open(sessionLogFileName)
	if err != nil {
		t.Fatal("couldn't read the log file!", err)
	}
	defer f.Close()

	msg, err := capnp.NewDecoder(f).Decode()
	if err != nil {
		t.Fatal("couldn't read the capnp msg file!", err)
	}

	sessionLog, err := ReadRootSessionLog(msg)
	if err != nil {
		t.Fatal("couldn't read the session log from the msg!", err)
	}

	timeStr, err := sessionLog.Timestamp()
	if err != nil {
		t.Fatal("couldn't read the Timestamp field!", err)
	}

	_, terr := time.Parse(time.RFC3339, timeStr)
	if terr != nil {
		t.Fatal("couldn't parse the Timestamp into the expected RFC3339 format", terr)
	}

	data, err := sessionLog.Content()
	if err != nil {
		t.Fatal("couldn't read the Content field!", err)
	}

	checkStr := string(data)
	if checkStr != testStr {
		t.Fatal("file data doesn't match!")
	}
}
