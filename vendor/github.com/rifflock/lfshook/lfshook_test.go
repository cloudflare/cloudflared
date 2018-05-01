package lfshook

import (
	"bytes"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
	"testing"
)

const expectedMsg = "This is the expected test message."
const unexpectedMsg = "This message should not be logged."

// Tests that writing to a tempfile log works.
// Matches the 'msg' of the output and deletes the tempfile.
func TestLogEntryWritten(t *testing.T) {
	log := logrus.New()
	// The colors were messing with the regexp so I turned them off.
	tmpfile, err := ioutil.TempFile("", "test_lfshook")
	if err != nil {
		t.Errorf("Unable to generate logfile due to err: %s", err)
	}
	fname := tmpfile.Name()
	defer func() {
		tmpfile.Close()
		os.Remove(fname)
	}()
	hook := NewHook(PathMap{
		logrus.InfoLevel: fname,
	}, nil)
	log.Hooks.Add(hook)

	log.Info(expectedMsg)
	log.Warn(unexpectedMsg)

	contents, err := ioutil.ReadAll(tmpfile)
	if err != nil {
		t.Errorf("Error while reading from tmpfile: %s", err)
	}

	if !bytes.Contains(contents, []byte("msg=\""+expectedMsg+"\"")) {
		t.Errorf("Message read (%s) doesnt match message written (%s) for file: %s", contents, expectedMsg, fname)
	}

	if bytes.Contains(contents, []byte("msg=\""+unexpectedMsg+"\"")) {
		t.Errorf("Message read (%s) contains message written (%s) for file: %s", contents, unexpectedMsg, fname)
	}

}
