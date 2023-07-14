package h2mux

import (
	"io"
)

func CompressionIsSupported() bool {
	return false
}

func newDecompressor(src io.Reader) decompressor {
	return nil
}

func newCompressor(dst io.Writer, quality, lgwin int) compressor {
	return nil
}
