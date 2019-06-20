package streamhandler

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"
)

const (
	testOpenStreamTimeout = time.Millisecond * 5000
	testHandshakeTimeout  = time.Millisecond * 1000
)

var (
	testTunnelHostname = h2mux.TunnelHostname("123.cftunnel.com")
	baseHeaders        = []h2mux.Header{
		{Name: ":method", Value: "GET"},
		{Name: ":scheme", Value: "http"},
		{Name: ":authority", Value: "example.com"},
		{Name: ":path", Value: "/"},
	}
	tunnelHostnameHeader = h2mux.Header{
		Name:  h2mux.CloudflaredProxyTunnelHostnameHeader,
		Value: testTunnelHostname.String(),
	}
)

func TestServeRequest(t *testing.T) {
	configChan := make(chan *pogs.ClientConfig)
	useConfigResultChan := make(chan *pogs.UseConfigurationResult)
	streamHandler := NewStreamHandler(configChan, useConfigResultChan, logrus.New())

	message := []byte("Hello cloudflared")
	httpServer := httptest.NewServer(&mockHTTPHandler{message})

	reverseProxyConfigs := []*pogs.ReverseProxyConfig{
		{
			TunnelHostname: testTunnelHostname,
			Origin: &pogs.HTTPOriginConfig{
				URLString: httpServer.URL,
			},
		},
	}
	streamHandler.UpdateConfig(reverseProxyConfigs)

	muxPair := NewDefaultMuxerPair(t, streamHandler)
	muxPair.Serve(t)

	ctx, cancel := context.WithTimeout(context.Background(), testOpenStreamTimeout)
	defer cancel()

	headers := append(baseHeaders, tunnelHostnameHeader)
	stream, err := muxPair.EdgeMux.OpenStream(ctx, headers, nil)
	assert.NoError(t, err)
	assertStatusHeader(t, http.StatusOK, stream.Headers)
	assertRespBody(t, message, stream)
}

func TestServeBadRequest(t *testing.T) {
	configChan := make(chan *pogs.ClientConfig)
	useConfigResultChan := make(chan *pogs.UseConfigurationResult)
	streamHandler := NewStreamHandler(configChan, useConfigResultChan, logrus.New())

	muxPair := NewDefaultMuxerPair(t, streamHandler)
	muxPair.Serve(t)

	ctx, cancel := context.WithTimeout(context.Background(), testOpenStreamTimeout)
	defer cancel()

	// No tunnel hostname header, expect to get 400 Bad Request
	stream, err := muxPair.EdgeMux.OpenStream(ctx, baseHeaders, nil)
	assert.NoError(t, err)
	assertStatusHeader(t, http.StatusBadRequest, stream.Headers)
	assertRespBody(t, statusBadRequest.text, stream)

	// No mapping for the tunnel hostname, expect to get 404 Not Found
	headers := append(baseHeaders, tunnelHostnameHeader)
	stream, err = muxPair.EdgeMux.OpenStream(ctx, headers, nil)
	assert.NoError(t, err)
	assertStatusHeader(t, http.StatusNotFound, stream.Headers)
	assertRespBody(t, statusNotFound.text, stream)

	// Nothing listening on empty url, so proxy would fail. Expect to get 502 Bad Gateway
	reverseProxyConfigs := []*pogs.ReverseProxyConfig{
		{
			TunnelHostname: testTunnelHostname,
			Origin: &pogs.HTTPOriginConfig{
				URLString: "",
			},
		},
	}
	streamHandler.UpdateConfig(reverseProxyConfigs)
	stream, err = muxPair.EdgeMux.OpenStream(ctx, headers, nil)
	assert.NoError(t, err)
	assertStatusHeader(t, http.StatusBadGateway, stream.Headers)
	assertRespBody(t, statusBadGateway.text, stream)

	// Invalid content-length, wouldn't not be able to create a request. Expect to get 400 Bad Request
	headers = append(headers, h2mux.Header{
		Name:  "content-length",
		Value: "x",
	})
	stream, err = muxPair.EdgeMux.OpenStream(ctx, headers, nil)
	assert.NoError(t, err)
	assertStatusHeader(t, http.StatusBadRequest, stream.Headers)
	assertRespBody(t, statusBadRequest.text, stream)
}

func assertStatusHeader(t *testing.T, expectedStatus int, headers []h2mux.Header) {
	assert.Equal(t, statusPseudoHeader, headers[0].Name)
	assert.Equal(t, strconv.Itoa(expectedStatus), headers[0].Value)
}

func assertRespBody(t *testing.T, expectedRespBody []byte, stream *h2mux.MuxedStream) {
	respBody := make([]byte, len(expectedRespBody))
	_, err := stream.Read(respBody)
	assert.NoError(t, err)
	assert.Equal(t, expectedRespBody, respBody)
}

type DefaultMuxerPair struct {
	OriginMuxConfig h2mux.MuxerConfig
	OriginMux       *h2mux.Muxer
	OriginConn      net.Conn
	EdgeMuxConfig   h2mux.MuxerConfig
	EdgeMux         *h2mux.Muxer
	EdgeConn        net.Conn
	doneC           chan struct{}
}

func NewDefaultMuxerPair(t assert.TestingT, h h2mux.MuxedStreamHandler) *DefaultMuxerPair {
	origin, edge := net.Pipe()
	p := &DefaultMuxerPair{
		OriginMuxConfig: h2mux.MuxerConfig{
			Timeout:                 testHandshakeTimeout,
			Handler:                 h,
			IsClient:                true,
			Name:                    "origin",
			Logger:                  logrus.NewEntry(logrus.New()),
			DefaultWindowSize:       (1 << 8) - 1,
			MaxWindowSize:           (1 << 15) - 1,
			StreamWriteBufferMaxLen: 1024,
		},
		OriginConn: origin,
		EdgeMuxConfig: h2mux.MuxerConfig{
			Timeout:                 testHandshakeTimeout,
			IsClient:                false,
			Name:                    "edge",
			Logger:                  logrus.NewEntry(logrus.New()),
			DefaultWindowSize:       (1 << 8) - 1,
			MaxWindowSize:           (1 << 15) - 1,
			StreamWriteBufferMaxLen: 1024,
		},
		EdgeConn: edge,
		doneC:    make(chan struct{}),
	}
	assert.NoError(t, p.Handshake())
	return p
}

func (p *DefaultMuxerPair) Handshake() error {
	ctx, cancel := context.WithTimeout(context.Background(), testHandshakeTimeout)
	defer cancel()
	errGroup, _ := errgroup.WithContext(ctx)
	errGroup.Go(func() (err error) {
		p.EdgeMux, err = h2mux.Handshake(p.EdgeConn, p.EdgeConn, p.EdgeMuxConfig)
		return errors.Wrap(err, "edge handshake failure")
	})
	errGroup.Go(func() (err error) {
		p.OriginMux, err = h2mux.Handshake(p.OriginConn, p.OriginConn, p.OriginMuxConfig)
		return errors.Wrap(err, "origin handshake failure")
	})

	return errGroup.Wait()
}

func (p *DefaultMuxerPair) Serve(t assert.TestingT) {
	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		err := p.EdgeMux.Serve(ctx)
		if err != nil && err != io.EOF && err != io.ErrClosedPipe {
			t.Errorf("error in edge muxer Serve(): %s", err)
		}
		p.OriginMux.Shutdown()
		wg.Done()
	}()
	go func() {
		err := p.OriginMux.Serve(ctx)
		if err != nil && err != io.EOF && err != io.ErrClosedPipe {
			t.Errorf("error in origin muxer Serve(): %s", err)
		}
		p.EdgeMux.Shutdown()
		wg.Done()
	}()
	go func() {
		// notify when both muxes have stopped serving
		wg.Wait()
		close(p.doneC)
	}()
}

type mockHTTPHandler struct {
	message []byte
}

func (mth *mockHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Write(mth.message)
}
