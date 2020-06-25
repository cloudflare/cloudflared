package logger

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLogLevel(t *testing.T) {
	timeFormat := "2006-01-02"
	f := NewDefaultFormatter(timeFormat)
	m := NewWriteManager()

	var testBuffer bytes.Buffer
	logger := NewOutputWriter(m)
	logger.Add(&testBuffer, f, InfoLevel, DebugLevel)

	testTime := f.Timestamp(InfoLevel, time.Now())

	testInfo := "hello Dalton, how are you doing?"
	logger.Info(testInfo)

	tesErr := "hello Austin, how did it break today?"
	logger.Error(tesErr)

	testDebug := "hello Bill, who are you?"
	logger.Debug(testDebug)

	m.Shutdown()

	lines := strings.Split(testBuffer.String(), "\n")
	assert.Len(t, lines, 3, "only expected two strings in the buffer")

	infoLine := lines[0]
	debugLine := lines[1]

	compareInfo := fmt.Sprintf("%s%s", testTime, testInfo)
	assert.Equal(t, compareInfo, infoLine, "expect the strings to match")

	compareDebug := fmt.Sprintf("%s%s", testTime, testDebug)
	assert.Equal(t, compareDebug, debugLine, "expect the strings to match")
}

func TestOutputWrite(t *testing.T) {
	timeFormat := "2006-01-02"
	f := NewDefaultFormatter(timeFormat)
	m := NewWriteManager()

	var testBuffer bytes.Buffer
	logger := NewOutputWriter(m)
	logger.Add(&testBuffer, f, InfoLevel)

	testData := "hello Bob Bork, how are you doing?"
	logger.Info(testData)
	testTime := f.Timestamp(InfoLevel, time.Now())

	m.Shutdown()

	scanner := bufio.NewScanner(&testBuffer)
	scanner.Scan()
	line := scanner.Text()
	assert.NoError(t, scanner.Err())

	compareLine := fmt.Sprintf("%s%s", testTime, testData)
	assert.Equal(t, compareLine, line, "expect the strings to match")
}

func TestFatalWrite(t *testing.T) {
	timeFormat := "2006-01-02"
	f := NewDefaultFormatter(timeFormat)
	m := NewWriteManager()

	var testBuffer bytes.Buffer
	logger := NewOutputWriter(m)
	logger.Add(&testBuffer, f, FatalLevel)

	oldOsExit := osExit
	defer func() { osExit = oldOsExit }()

	var got int
	myExit := func(code int) {
		got = code
	}

	osExit = myExit

	testData := "so long y'all"
	logger.Fatal(testData)
	testTime := f.Timestamp(FatalLevel, time.Now())

	scanner := bufio.NewScanner(&testBuffer)
	scanner.Scan()
	line := scanner.Text()
	assert.NoError(t, scanner.Err())

	compareLine := fmt.Sprintf("%s%s", testTime, testData)
	assert.Equal(t, compareLine, line, "expect the strings to match")
	assert.Equal(t, got, 1, "exit code should be one for a fatal log")
}
