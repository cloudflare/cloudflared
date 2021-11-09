//go:build cgo
// +build cgo

package h2mux

import (
	"io"

	"github.com/cloudflare/brotli-go"
)

func CompressionIsSupported() bool {
	return true
}

func newDecompressor(src io.Reader) *brotli.Reader {
	return brotli.NewReader(src)
}

func newCompressor(dst io.Writer, quality, lgwin int) *brotli.Writer {
	return brotli.NewWriter(dst, brotli.WriterOptions{Quality: quality, LGWin: lgwin})
}
