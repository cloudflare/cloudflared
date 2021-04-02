package websocket

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// IsWebSocketUpgrade checks to see if the request is a WebSocket connection.
func IsWebSocketUpgrade(req *http.Request) bool {
	return websocket.IsWebSocketUpgrade(req)
}

// NewResponseHeader returns headers needed to return to origin for completing handshake
func NewResponseHeader(req *http.Request) http.Header {
	header := http.Header{}
	header.Add("Connection", "Upgrade")
	header.Add("Sec-Websocket-Accept", generateAcceptKey(req.Header.Get("Sec-WebSocket-Key")))
	header.Add("Upgrade", "websocket")
	return header
}

// Stream copies copy data to & from provided io.ReadWriters.
func Stream(tunnelConn, originConn io.ReadWriter, log *zerolog.Logger) {
	proxyDone := make(chan struct{}, 2)

	go func() {
		_, err := copyData(tunnelConn, originConn, "origin->tunnel")
		if err != nil {
			log.Debug().Msgf("origin to tunnel copy: %v", err)
		}
		proxyDone <- struct{}{}
	}()

	go func() {
		_, err := copyData(originConn, tunnelConn, "tunnel->origin")
		if err != nil {
			log.Debug().Msgf("tunnel to origin copy: %v", err)
		}
		proxyDone <- struct{}{}
	}()

	// If one side is done, we are done.
	<-proxyDone
}

// when set to true, enables logging of content copied to/from origin and tunnel
const debugCopy = false

func copyData(dst io.Writer, src io.Reader, dir string) (written int64, err error) {
	if debugCopy {
		// copyBuffer is based on stdio Copy implementation but shows copied data
		copyBuffer := func(dst io.Writer, src io.Reader, dir string) (written int64, err error) {
			var buf []byte
			size := 32 * 1024
			buf = make([]byte, size)
			for {
				t := time.Now()
				nr, er := src.Read(buf)
				if nr > 0 {
					fmt.Println(dir, t.UnixNano(), "\n"+hex.Dump(buf[0:nr]))
					nw, ew := dst.Write(buf[0:nr])
					if nw < 0 || nr < nw {
						nw = 0
						if ew == nil {
							ew = errors.New("invalid write")
						}
					}
					written += int64(nw)
					if ew != nil {
						err = ew
						break
					}
					if nr != nw {
						err = io.ErrShortWrite
						break
					}
				}
				if er != nil {
					if er != io.EOF {
						err = er
					}
					break
				}
			}
			return written, err
		}
		return copyBuffer(dst, src, dir)
	} else {
		return io.Copy(dst, src)
	}
}

// from RFC-6455
var keyGUID = []byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11")

func generateAcceptKey(challengeKey string) string {
	h := sha1.New()
	h.Write([]byte(challengeKey))
	h.Write(keyGUID)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
