package logger

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
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
	dirName := "testdir"
	err := os.Mkdir(dirName, 0755)
	assert.NoError(t, err)

	fileName := "test_file"
	firstFile := filepath.Join(dirName, fileName+".log")
	secondFile := filepath.Join(dirName, fileName+"-1.log")
	thirdFile := filepath.Join(dirName, fileName+"-2.log")

	defer func() {
		os.RemoveAll(dirName)
		os.Remove(firstFile)
		os.Remove(secondFile)
		os.Remove(thirdFile)
	}()

	w := NewFileRollingWriter(dirName, fileName, 1000, 2)
	defer w.Close()

	for i := 99; i >= 1; i-- {
		testData := []byte(fmt.Sprintf("%d bottles of beer on the wall...", i))
		w.Write(testData)
	}
	assert.FileExists(t, firstFile, "first file doesn't exist as expected")
	assert.FileExists(t, secondFile, "second file doesn't exist as expected")
	assert.FileExists(t, thirdFile, "third file doesn't exist as expected")
	assert.False(t, exists(filepath.Join(dirName, fileName+"-3.log")), "limited to two files and there is more")
}

func TestSingleFile(t *testing.T) {
	fileName := "test_file"
	testData := []byte(string("hello Dalton, how are you doing?"))
	defer func() {
		os.Remove(fileName)
	}()

	w := NewFileRollingWriter(fileName, fileName, 1000, 2)
	defer w.Close()

	l, err := w.Write(testData)

	assert.NoError(t, err)
	assert.Equal(t, l, len(testData), "expected write length and data length to match")

	d, err := ioutil.ReadFile(fileName)
	assert.FileExists(t, fileName, "file doesn't exist at expected path")
	assert.Equal(t, d, testData, "expected data in file to match test data")
}

func TestSingleFileInDirectory(t *testing.T) {
	dirName := "testdir"
	err := os.Mkdir(dirName, 0755)
	assert.NoError(t, err)

	fileName := "test_file"
	fullPath := filepath.Join(dirName, fileName+".log")
	testData := []byte(string("hello Dalton, how are you doing?"))
	defer func() {
		os.Remove(fullPath)
		os.RemoveAll(dirName)
	}()

	w := NewFileRollingWriter(fullPath, fileName, 1000, 2)
	defer w.Close()

	l, err := w.Write(testData)

	assert.NoError(t, err)
	assert.Equal(t, l, len(testData), "expected write length and data length to match")

	d, err := ioutil.ReadFile(fullPath)
	assert.FileExists(t, fullPath, "file doesn't exist at expected path")
	assert.Equal(t, d, testData, "expected data in file to match test data")
}
