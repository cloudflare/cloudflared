package logger

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFileWrite(t *testing.T) {
	fileName := "test_file"
	fileLog := fileName + ".log"
	testData := []byte(string("hello Dalton, how are you doing?"))
	defer func() {
		os.Remove(fileLog)
	}()

	w := NewFileRollingWriter("", fileName, 1000, 2)
	defer w.Close()

	l, err := w.Write(testData)

	assert.NoError(t, err)
	assert.Equal(t, l, len(testData), "expected write length and data length to match")

	d, err := ioutil.ReadFile(fileLog)
	assert.FileExists(t, fileLog, "file doesn't exist at expected path")
	assert.Equal(t, d, testData, "expected data in file to match test data")
}

func TestRolling(t *testing.T) {
	fileName := "test_file"
	firstFile := fileName + ".log"
	secondFile := fileName + "-1.log"
	thirdFile := fileName + "-2.log"

	defer func() {
		os.Remove(firstFile)
		os.Remove(secondFile)
		os.Remove(thirdFile)
	}()

	w := NewFileRollingWriter("", fileName, 1000, 2)
	defer w.Close()

	for i := 99; i >= 1; i-- {
		testData := []byte(fmt.Sprintf("%d bottles of beer on the wall...", i))
		w.Write(testData)
	}
	assert.FileExists(t, firstFile, "first file doesn't exist as expected")
	assert.FileExists(t, secondFile, "second file doesn't exist as expected")
	assert.FileExists(t, thirdFile, "third file doesn't exist as expected")
	assert.False(t, exists(fileName+"-3.log"), "limited to two files and there is more")
}
