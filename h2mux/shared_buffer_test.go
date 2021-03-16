package h2mux

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func AssertIOReturnIsGood(t *testing.T, expected int) func(int, error) {
	return func(actual int, err error) {
		if expected != actual {
			t.Fatalf("Expected %d bytes, got %d", expected, actual)
		}
		if err != nil {
			t.Fatalf("Unexpected error %s", err)
		}
	}
}

func TestSharedBuffer(t *testing.T) {
	b := NewSharedBuffer()
	testData := []byte("Hello world")
	AssertIOReturnIsGood(t, len(testData))(b.Write(testData))
	bytesRead := make([]byte, len(testData))
	AssertIOReturnIsGood(t, len(testData))(b.Read(bytesRead))
}

func TestSharedBufferBlockingRead(t *testing.T) {
	b := NewSharedBuffer()
	testData1 := []byte("Hello")
	testData2 := []byte(" world")
	result := make(chan []byte)
	go func() {
		bytesRead := make([]byte, len(testData1)+len(testData2))
		nRead, err := b.Read(bytesRead)
		AssertIOReturnIsGood(t, len(testData1))(nRead, err)
		result <- bytesRead[:nRead]
		nRead, err = b.Read(bytesRead)
		AssertIOReturnIsGood(t, len(testData2))(nRead, err)
		result <- bytesRead[:nRead]
	}()
	time.Sleep(time.Millisecond * 250)
	select {
	case <-result:
		t.Fatalf("read returned early")
	default:
	}
	AssertIOReturnIsGood(t, len(testData1))(b.Write([]byte(testData1)))
	select {
	case r := <-result:
		assert.Equal(t, testData1, r)
	case <-time.After(time.Second):
		t.Fatalf("read timed out")
	}
	AssertIOReturnIsGood(t, len(testData2))(b.Write([]byte(testData2)))
	select {
	case r := <-result:
		assert.Equal(t, testData2, r)
	case <-time.After(time.Second):
		t.Fatalf("read timed out")
	}
}

// This is quite slow under the race detector
func TestSharedBufferConcurrentReadWrite(t *testing.T) {
	b := NewSharedBuffer()
	var expectedResult, actualResult bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		block := make([]byte, 256)
		for i := range block {
			block[i] = byte(i)
		}
		for blockSize := 1; blockSize <= 256; blockSize++ {
			for i := 0; i < 256; i++ {
				expectedResult.Write(block[:blockSize])
				n, err := b.Write(block[:blockSize])
				if n != blockSize || err != nil {
					t.Errorf("write error: %d %s", n, err)
					return
				}
			}
		}
		wg.Done()
	}()
	go func() {
		block := make([]byte, 256)
		// Change block sizes in opposition to the write thread, to test blocking for new data.
		for blockSize := 256; blockSize > 0; blockSize-- {
			for i := 0; i < 256; i++ {
				n, err := io.ReadFull(b, block[:blockSize])
				if n != blockSize || err != nil {
					t.Errorf("read error: %d %s", n, err)
					return
				}
				actualResult.Write(block[:blockSize])
			}
		}
		wg.Done()
	}()
	wg.Wait()
	if bytes.Compare(expectedResult.Bytes(), actualResult.Bytes()) != 0 {
		t.Fatal("Result diverged")
	}
}

func TestSharedBufferClose(t *testing.T) {
	b := NewSharedBuffer()
	testData := []byte("Hello world")
	AssertIOReturnIsGood(t, len(testData))(b.Write(testData))
	err := b.Close()
	if err != nil {
		t.Fatalf("unexpected error from Close: %s", err)
	}
	bytesRead := make([]byte, len(testData))
	AssertIOReturnIsGood(t, len(testData))(b.Read(bytesRead))
	n, err := b.Read(bytesRead)
	if n != 0 {
		t.Fatalf("extra bytes received: %d", n)
	}
	if err != io.EOF {
		t.Fatalf("expected EOF, got %s", err)
	}
}
