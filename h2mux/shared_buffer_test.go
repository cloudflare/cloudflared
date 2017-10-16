package h2mux

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"
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
	testData := []byte("Hello world")
	result := make(chan []byte)
	go func() {
		bytesRead := make([]byte, len(testData))
		AssertIOReturnIsGood(t, len(testData))(b.Read(bytesRead))
		result <- bytesRead
	}()
	select {
	case <-result:
		t.Fatalf("read returned early")
	default:
	}
	AssertIOReturnIsGood(t, 5)(b.Write(testData[:5]))
	select {
	case <-result:
		t.Fatalf("read returned early")
	default:
	}
	AssertIOReturnIsGood(t, len(testData)-5)(b.Write(testData[5:]))
	select {
	case r := <-result:
		if string(r) != string(testData) {
			t.Fatalf("expected read to return %s, got %s", testData, r)
		}
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
					t.Fatalf("write error: %d %s", n, err)
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
				n, err := b.Read(block[:blockSize])
				if n != blockSize || err != nil {
					t.Fatalf("read error: %d %s", n, err)
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
