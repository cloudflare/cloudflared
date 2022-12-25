package websocket

import (
	"crypto/sha1"
	"encoding/base64"
	"net/http"

	"github.com/gorilla/websocket"
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

// from RFC-6455
var keyGUID = []byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11")

func generateAcceptKey(challengeKey string) string {
	h := sha1.New()
	h.Write([]byte(challengeKey))
	h.Write(keyGUID)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
