package writebuffer

import (
	"io"

	"github.com/kshvakov/clickhouse/lib/leakypool"
)

const InitialSize = 256 * 1024

func New(initSize int) *WriteBuffer {
	wb := &WriteBuffer{}
	wb.addChunk(0, initSize)
	return wb
}

type WriteBuffer struct {
	chunks [][]byte
}

func (wb *WriteBuffer) Write(data []byte) (int, error) {
	var (
		chunkIdx    = len(wb.chunks) - 1
		dataSize    = len(data)
		writtenSize = 0
	)
	for {
		freeSize := cap(wb.chunks[chunkIdx]) - len(wb.chunks[chunkIdx])
		if freeSize >= len(data) {
			wb.chunks[chunkIdx] = append(wb.chunks[chunkIdx], data...)
			writtenSize += len(data)
			return writtenSize, nil
		}
		wb.chunks[chunkIdx] = append(wb.chunks[chunkIdx], data[:freeSize]...)
		writtenSize += freeSize
		data = data[freeSize:]
		dataSize = dataSize - freeSize
		wb.addChunk(0, wb.calcCap(len(data)))
		chunkIdx++
	}
}

func (wb *WriteBuffer) WriteTo(w io.Writer) (int64, error) {
	var size int64
	for _, chunk := range wb.chunks {
		ln, err := w.Write(chunk)
		if err != nil {
			wb.Reset()
			return 0, err
		}
		size += int64(ln)
	}
	wb.Reset()
	return size, nil
}

func (wb *WriteBuffer) Bytes() []byte {
	if len(wb.chunks) == 1 {
		return wb.chunks[0]
	}
	bytes := make([]byte, 0, wb.len())
	for _, chunk := range wb.chunks {
		bytes = append(bytes, chunk...)
	}
	return bytes
}

func (wb *WriteBuffer) addChunk(size, capacity int) {
	chunk := leakypool.GetBytes(size, capacity)
	if cap(chunk) >= size {
		chunk = chunk[:size]
	}
	wb.chunks = append(wb.chunks, chunk)
}

func (wb *WriteBuffer) len() int {
	var v int
	for _, chunk := range wb.chunks {
		v += len(chunk)
	}
	return v
}

func (wb *WriteBuffer) calcCap(dataSize int) int {
	dataSize = max(dataSize, 64)
	if len(wb.chunks) == 0 {
		return dataSize
	}
	// Always double the size of the last chunk
	return max(dataSize, cap(wb.chunks[len(wb.chunks)-1])*2)
}

func (wb *WriteBuffer) Reset() {
	if len(wb.chunks) == 0 {
		return
	}
	// Recycle all chunks except the last one
	chunkSizeThreshold := cap(wb.chunks[0])
	for _, chunk := range wb.chunks[:len(wb.chunks)-1] {
		// Drain chunks smaller than the initial size
		if cap(chunk) >= chunkSizeThreshold {
			leakypool.PutBytes(chunk[:0])
		} else {
			chunkSizeThreshold = cap(chunk)
		}
	}
	// Keep the largest chunk
	wb.chunks[0] = wb.chunks[len(wb.chunks)-1][:0]
	wb.chunks = wb.chunks[:1]
}

func max(a, b int) int {
	if b > a {
		return b
	}
	return a
}
