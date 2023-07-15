package socks

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/net/proxy"
)

type successResponse struct {
	Status string `json:"status"`
}

func sendSocksRequest(t *testing.T) []byte {
	dialer, err := proxy.SOCKS5("tcp", "127.0.0.1:8086", nil, proxy.Direct)
	assert.NoError(t, err)

	httpTransport := &http.Transport{}
	httpClient := &http.Client{Transport: httpTransport}
	// set our socks5 as the dialer
	httpTransport.Dial = dialer.Dial

	req, err := http.NewRequest("GET", "http://127.0.0.1:8085", nil)
	assert.NoError(t, err)

	resp, err := httpClient.Do(req)
	assert.NoError(t, err)
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)

	return b
}

func startTestServer(t *testing.T, httpHandler func(w http.ResponseWriter, r *http.Request)) {
	// create a socks server
	requestHandler := NewRequestHandler(NewNetDialer(), nil)
	socksServer := NewConnectionHandler(requestHandler)
	listener, err := net.Listen("tcp", "localhost:8086")
	assert.NoError(t, err)

	go func() {
		defer listener.Close()
		for {
			conn, _ := listener.Accept()
			go socksServer.Serve(conn)
		}
	}()

	// create an http server
	mux := http.NewServeMux()
	mux.HandleFunc("/", httpHandler)

	// start the servers
	go http.ListenAndServe("localhost:8085", mux)
}

func respondWithJSON(w http.ResponseWriter, v interface{}, status int) {
	data, _ := json.Marshal(v)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(data)
}

func OkJSONResponseHandler(w http.ResponseWriter, r *http.Request) {
	resp := successResponse{
		Status: "ok",
	}
	respondWithJSON(w, resp, http.StatusOK)
}

func TestSocksConnection(t *testing.T) {
	startTestServer(t, OkJSONResponseHandler)
	time.Sleep(100 * time.Millisecond)
	b := sendSocksRequest(t)
	assert.True(t, len(b) > 0, "no data returned!")

	var resp successResponse
	json.Unmarshal(b, &resp)

	assert.True(t, resp.Status == "ok", "response didn't return ok")
}
