package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriteManger(t *testing.T) {
	testData := []byte(string("hello Austin, how are you doing?"))
	waitChan := make(chan []byte)
	m := NewWriteManager()
	m.Append(testData, func(b []byte) {
		waitChan <- b
	})
	resp := <-waitChan
	assert.Equal(t, testData, resp)
}
