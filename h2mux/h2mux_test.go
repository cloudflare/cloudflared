package h2mux

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"
)

const (
	testOpenStreamTimeout = time.Millisecond * 5000
	testHandshakeTimeout  = time.Millisecond * 1000
)

var log = zerolog.Nop()

func TestMain(m *testing.M) {
	if os.Getenv("VERBOSE") == "1" {
		//TODO: set log level
	}
	os.Exit(m.Run())
}

type DefaultMuxerPair struct {
	OriginMuxConfig MuxerConfig
	OriginMux       *Muxer
	OriginConn      net.Conn
	EdgeMuxConfig   MuxerConfig
	EdgeMux         *Muxer
	EdgeConn        net.Conn
	doneC           chan struct{}
}

func NewDefaultMuxerPair(t assert.TestingT, testName string, f MuxedStreamFunc) *DefaultMuxerPair {
	origin, edge := net.Pipe()
	p := &DefaultMuxerPair{
		OriginMuxConfig: MuxerConfig{
			Timeout:                 testHandshakeTimeout,
			Handler:                 f,
			IsClient:                true,
			Name:                    "origin",
			Log:                     &log,
			DefaultWindowSize:       (1 << 8) - 1,
			MaxWindowSize:           (1 << 15) - 1,
			StreamWriteBufferMaxLen: 1024,
			HeartbeatInterval:       defaultTimeout,
			MaxHeartbeats:           defaultRetries,
		},
		OriginConn: origin,
		EdgeMuxConfig: MuxerConfig{
			Timeout:                 testHandshakeTimeout,
			IsClient:                false,
			Name:                    "edge",
			Log:                     &log,
			DefaultWindowSize:       (1 << 8) - 1,
			MaxWindowSize:           (1 << 15) - 1,
			StreamWriteBufferMaxLen: 1024,
			HeartbeatInterval:       defaultTimeout,
			MaxHeartbeats:           defaultRetries,
		},
		EdgeConn: edge,
		doneC:    make(chan struct{}),
	}
	assert.NoError(t, p.Handshake(testName))
	return p
}

func NewCompressedMuxerPair(t assert.TestingT, testName string, quality CompressionSetting, f MuxedStreamFunc) *DefaultMuxerPair {
	origin, edge := net.Pipe()
	p := &DefaultMuxerPair{
		OriginMuxConfig: MuxerConfig{
			Timeout:            time.Second,
			Handler:            f,
			IsClient:           true,
			Name:               "origin",
			CompressionQuality: quality,
			Log:                &log,
			HeartbeatInterval:  defaultTimeout,
			MaxHeartbeats:      defaultRetries,
		},
		OriginConn: origin,
		EdgeMuxConfig: MuxerConfig{
			Timeout:            time.Second,
			IsClient:           false,
			Name:               "edge",
			CompressionQuality: quality,
			Log:                &log,
			HeartbeatInterval:  defaultTimeout,
			MaxHeartbeats:      defaultRetries,
		},
		EdgeConn: edge,
		doneC:    make(chan struct{}),
	}
	assert.NoError(t, p.Handshake(testName))
	return p
}

func (p *DefaultMuxerPair) Handshake(testName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), testHandshakeTimeout)
	defer cancel()
	errGroup, _ := errgroup.WithContext(ctx)
	errGroup.Go(func() (err error) {
		p.EdgeMux, err = Handshake(p.EdgeConn, p.EdgeConn, p.EdgeMuxConfig, ActiveStreams)
		return errors.Wrap(err, "edge handshake failure")
	})
	errGroup.Go(func() (err error) {
		p.OriginMux, err = Handshake(p.OriginConn, p.OriginConn, p.OriginMuxConfig, ActiveStreams)
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

func (p *DefaultMuxerPair) Wait(t *testing.T) {
	select {
	case <-p.doneC:
		return
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for shutdown")
	}
}

func (p *DefaultMuxerPair) OpenEdgeMuxStream(headers []Header, body io.Reader) (*MuxedStream, error) {
	ctx, cancel := context.WithTimeout(context.Background(), testOpenStreamTimeout)
	defer cancel()
	return p.EdgeMux.OpenStream(ctx, headers, body)
}

func TestHandshake(t *testing.T) {
	f := func(stream *MuxedStream) error {
		return nil
	}
	muxPair := NewDefaultMuxerPair(t, t.Name(), f)
	AssertIfPipeReadable(t, muxPair.OriginConn)
	AssertIfPipeReadable(t, muxPair.EdgeConn)
}

func TestSingleStream(t *testing.T) {
	f := MuxedStreamFunc(func(stream *MuxedStream) error {
		if len(stream.Headers) != 1 {
			t.Fatalf("expected %d headers, got %d", 1, len(stream.Headers))
		}
		if stream.Headers[0].Name != "test-header" {
			t.Fatalf("expected header name %s, got %s", "test-header", stream.Headers[0].Name)
		}
		if stream.Headers[0].Value != "headerValue" {
			t.Fatalf("expected header value %s, got %s", "headerValue", stream.Headers[0].Value)
		}
		_ = stream.WriteHeaders([]Header{
			{Name: "response-header", Value: "responseValue"},
		})
		buf := []byte("Hello world")
		_, _ = stream.Write(buf)
		n, err := io.ReadFull(stream, buf)
		if n > 0 {
			t.Fatalf("read %d bytes after EOF", n)
		}
		if err != io.EOF {
			t.Fatalf("expected EOF, got %s", err)
		}
		return nil
	})
	muxPair := NewDefaultMuxerPair(t, t.Name(), f)
	muxPair.Serve(t)

	stream, err := muxPair.OpenEdgeMuxStream(
		[]Header{{Name: "test-header", Value: "headerValue"}},
		nil,
	)
	if err != nil {
		t.Fatalf("error in OpenStream: %s", err)
	}
	if len(stream.Headers) != 1 {
		t.Fatalf("expected %d headers, got %d", 1, len(stream.Headers))
	}
	if stream.Headers[0].Name != "response-header" {
		t.Fatalf("expected header name %s, got %s", "response-header", stream.Headers[0].Name)
	}
	if stream.Headers[0].Value != "responseValue" {
		t.Fatalf("expected header value %s, got %s", "responseValue", stream.Headers[0].Value)
	}
	responseBody := make([]byte, 11)
	n, err := io.ReadFull(stream, responseBody)
	if err != nil {
		t.Fatalf("error from (*MuxedStream).Read: %s", err)
	}
	if n != len(responseBody) {
		t.Fatalf("expected response body to have %d bytes, got %d", len(responseBody), n)
	}
	if string(responseBody) != "Hello world" {
		t.Fatalf("expected response body %s, got %s", "Hello world", responseBody)
	}
	_ = stream.Close()
	n, err = stream.Write([]byte("aaaaa"))
	if n > 0 {
		t.Fatalf("wrote %d bytes after EOF", n)
	}
	if err != io.EOF {
		t.Fatalf("expected EOF, got %s", err)
	}
}

func TestSingleStreamLargeResponseBody(t *testing.T) {
	bodySize := 1 << 24
	f := MuxedStreamFunc(func(stream *MuxedStream) error {
		if len(stream.Headers) != 1 {
			t.Fatalf("expected %d headers, got %d", 1, len(stream.Headers))
		}
		if stream.Headers[0].Name != "test-header" {
			t.Fatalf("expected header name %s, got %s", "test-header", stream.Headers[0].Name)
		}
		if stream.Headers[0].Value != "headerValue" {
			t.Fatalf("expected header value %s, got %s", "headerValue", stream.Headers[0].Value)
		}
		_ = stream.WriteHeaders([]Header{
			{Name: "response-header", Value: "responseValue"},
		})
		payload := make([]byte, bodySize)
		for i := range payload {
			payload[i] = byte(i % 256)
		}
		t.Log("Writing payload...")
		n, err := stream.Write(payload)
		t.Logf("Wrote %d bytes into the stream", n)
		if err != nil {
			t.Fatalf("origin write error: %s", err)
		}
		if n != len(payload) {
			t.Fatalf("origin short write: %d/%d bytes", n, len(payload))
		}

		return nil
	})
	muxPair := NewDefaultMuxerPair(t, t.Name(), f)
	muxPair.Serve(t)

	stream, err := muxPair.OpenEdgeMuxStream(
		[]Header{{Name: "test-header", Value: "headerValue"}},
		nil,
	)
	if err != nil {
		t.Fatalf("error in OpenStream: %s", err)
	}
	if len(stream.Headers) != 1 {
		t.Fatalf("expected %d headers, got %d", 1, len(stream.Headers))
	}
	if stream.Headers[0].Name != "response-header" {
		t.Fatalf("expected header name %s, got %s", "response-header", stream.Headers[0].Name)
	}
	if stream.Headers[0].Value != "responseValue" {
		t.Fatalf("expected header value %s, got %s", "responseValue", stream.Headers[0].Value)
	}
	responseBody := make([]byte, bodySize)

	n, err := io.ReadFull(stream, responseBody)
	if err != nil {
		t.Fatalf("error from (*MuxedStream).Read: %s", err)
	}
	if n != len(responseBody) {
		t.Fatalf("expected response body to have %d bytes, got %d", len(responseBody), n)
	}
}

func TestMultipleStreams(t *testing.T) {
	f := MuxedStreamFunc(func(stream *MuxedStream) error {
		if len(stream.Headers) != 1 {
			t.Fatalf("expected %d headers, got %d", 1, len(stream.Headers))
		}
		if stream.Headers[0].Name != "client-token" {
			t.Fatalf("expected header name %s, got %s", "client-token", stream.Headers[0].Name)
		}
		log.Debug().Msgf("Got request for stream %s", stream.Headers[0].Value)
		_ = stream.WriteHeaders([]Header{
			{Name: "response-token", Value: stream.Headers[0].Value},
		})
		log.Debug().Msgf("Wrote headers for stream %s", stream.Headers[0].Value)
		_, _ = stream.Write([]byte("OK"))
		log.Debug().Msgf("Wrote body for stream %s", stream.Headers[0].Value)
		return nil
	})
	muxPair := NewDefaultMuxerPair(t, t.Name(), f)
	muxPair.Serve(t)

	maxStreams := 64
	errorsC := make(chan error, maxStreams)
	var wg sync.WaitGroup
	wg.Add(maxStreams)
	for i := 0; i < maxStreams; i++ {
		go func(tokenId int) {
			defer wg.Done()
			tokenString := fmt.Sprintf("%d", tokenId)
			stream, err := muxPair.OpenEdgeMuxStream(
				[]Header{{Name: "client-token", Value: tokenString}},
				nil,
			)
			log.Debug().Msgf("Got headers for stream %d", tokenId)
			if err != nil {
				errorsC <- err
				return
			}
			if len(stream.Headers) != 1 {
				errorsC <- fmt.Errorf("stream %d has error: expected %d headers, got %d", stream.streamID, 1, len(stream.Headers))
				return
			}
			if stream.Headers[0].Name != "response-token" {
				errorsC <- fmt.Errorf("stream %d has error: expected header name %s, got %s", stream.streamID, "response-token", stream.Headers[0].Name)
				return
			}
			if stream.Headers[0].Value != tokenString {
				errorsC <- fmt.Errorf("stream %d has error: expected header value %s, got %s", stream.streamID, tokenString, stream.Headers[0].Value)
				return
			}
			responseBody := make([]byte, 2)
			n, err := io.ReadFull(stream, responseBody)
			if err != nil {
				errorsC <- fmt.Errorf("stream %d has error: error from (*MuxedStream).Read: %s", stream.streamID, err)
				return
			}
			if n != len(responseBody) {
				errorsC <- fmt.Errorf("stream %d has error: expected response body to have %d bytes, got %d", stream.streamID, len(responseBody), n)
				return
			}
			if string(responseBody) != "OK" {
				errorsC <- fmt.Errorf("stream %d has error: expected response body %s, got %s", stream.streamID, "OK", responseBody)
				return
			}
		}(i)
	}
	wg.Wait()
	close(errorsC)
	testFail := false
	for err := range errorsC {
		testFail = true
		log.Error().Msgf("%s", err)
	}
	if testFail {
		t.Fatalf("TestMultipleStreams failed")
	}
}

func TestMultipleStreamsFlowControl(t *testing.T) {
	maxStreams := 32
	responseSizes := make([]int32, maxStreams)
	for i := 0; i < maxStreams; i++ {
		responseSizes[i] = rand.Int31n(int32(defaultWindowSize << 4))
	}

	f := MuxedStreamFunc(func(stream *MuxedStream) error {
		if len(stream.Headers) != 1 {
			t.Fatalf("expected %d headers, got %d", 1, len(stream.Headers))
		}
		if stream.Headers[0].Name != "test-header" {
			t.Fatalf("expected header name %s, got %s", "test-header", stream.Headers[0].Name)
		}
		if stream.Headers[0].Value != "headerValue" {
			t.Fatalf("expected header value %s, got %s", "headerValue", stream.Headers[0].Value)
		}
		_ = stream.WriteHeaders([]Header{
			{Name: "response-header", Value: "responseValue"},
		})
		payload := make([]byte, responseSizes[(stream.streamID-2)/2])
		for i := range payload {
			payload[i] = byte(i % 256)
		}
		n, err := stream.Write(payload)
		if err != nil {
			t.Fatalf("origin write error: %s", err)
		}
		if n != len(payload) {
			t.Fatalf("origin short write: %d/%d bytes", n, len(payload))
		}
		return nil
	})
	muxPair := NewDefaultMuxerPair(t, t.Name(), f)
	muxPair.Serve(t)

	errGroup, _ := errgroup.WithContext(context.Background())
	for i := 0; i < maxStreams; i++ {
		errGroup.Go(func() error {
			stream, err := muxPair.OpenEdgeMuxStream(
				[]Header{{Name: "test-header", Value: "headerValue"}},
				nil,
			)
			if err != nil {
				return fmt.Errorf("error in OpenStream: %d %s", stream.streamID, err)
			}
			if len(stream.Headers) != 1 {
				return fmt.Errorf("stream %d expected %d headers, got %d", stream.streamID, 1, len(stream.Headers))
			}
			if stream.Headers[0].Name != "response-header" {
				return fmt.Errorf("stream %d expected header name %s, got %s", stream.streamID, "response-header", stream.Headers[0].Name)
			}
			if stream.Headers[0].Value != "responseValue" {
				return fmt.Errorf("stream %d expected header value %s, got %s", stream.streamID, "responseValue", stream.Headers[0].Value)
			}

			responseBody := make([]byte, responseSizes[(stream.streamID-2)/2])
			n, err := io.ReadFull(stream, responseBody)
			if err != nil {
				return fmt.Errorf("stream %d error from (*MuxedStream).Read: %s", stream.streamID, err)
			}
			if n != len(responseBody) {
				return fmt.Errorf("stream %d expected response body to have %d bytes, got %d", stream.streamID, len(responseBody), n)
			}
			return nil
		})
	}
	assert.NoError(t, errGroup.Wait())
}

func TestGracefulShutdown(t *testing.T) {
	sendC := make(chan struct{})
	responseBuf := bytes.Repeat([]byte("Hello world"), 65536)

	f := MuxedStreamFunc(func(stream *MuxedStream) error {
		_ = stream.WriteHeaders([]Header{
			{Name: "response-header", Value: "responseValue"},
		})
		<-sendC
		log.Debug().Msgf("Writing %d bytes", len(responseBuf))
		_, _ = stream.Write(responseBuf)
		_ = stream.CloseWrite()
		log.Debug().Msgf("Wrote %d bytes", len(responseBuf))
		// Reading from the stream will block until the edge closes its end of the stream.
		// Otherwise, we'll close the whole connection before receiving the 'stream closed'
		// message from the edge.
		// Graceful shutdown works if you omit this, it just gives spurious errors for now -
		// TODO ignore errors when writing 'stream closed' and we're shutting down.
		_, _ = stream.Read([]byte{0})
		log.Debug().Msgf("Handler ends")
		return nil
	})
	muxPair := NewDefaultMuxerPair(t, t.Name(), f)
	muxPair.Serve(t)

	stream, err := muxPair.OpenEdgeMuxStream(
		[]Header{{Name: "test-header", Value: "headerValue"}},
		nil,
	)
	if err != nil {
		t.Fatalf("error in OpenStream: %s", err)
	}
	// Start graceful shutdown of the edge mux - this should also close the origin mux when done
	muxPair.EdgeMux.Shutdown()
	close(sendC)
	responseBody := make([]byte, len(responseBuf))
	log.Debug().Msgf("Waiting for %d bytes", len(responseBuf))
	n, err := io.ReadFull(stream, responseBody)
	if err != nil {
		t.Fatalf("error from (*MuxedStream).Read with %d bytes read: %s", n, err)
	}
	if n != len(responseBody) {
		t.Fatalf("expected response body to have %d bytes, got %d", len(responseBody), n)
	}
	if !bytes.Equal(responseBuf, responseBody) {
		t.Fatalf("response body mismatch")
	}
	_ = stream.Close()
	muxPair.Wait(t)
}

func TestUnexpectedShutdown(t *testing.T) {
	sendC := make(chan struct{})
	handlerFinishC := make(chan struct{})
	responseBuf := bytes.Repeat([]byte("Hello world"), 65536)

	f := MuxedStreamFunc(func(stream *MuxedStream) error {
		defer close(handlerFinishC)
		_ = stream.WriteHeaders([]Header{
			{Name: "response-header", Value: "responseValue"},
		})
		<-sendC
		n, err := stream.Read([]byte{0})
		if err != io.EOF {
			t.Fatalf("unexpected error from (*MuxedStream).Read: %s", err)
		}
		if n != 0 {
			t.Fatalf("expected empty read, got %d bytes", n)
		}
		// Write comes after read, because write buffers data before it is flushed. It wouldn't know about EOF
		// until some time later. Calling read first forces it to know about EOF now.
		_, err = stream.Write(responseBuf)
		if err != io.EOF {
			t.Fatalf("unexpected error from (*MuxedStream).Write: %s", err)
		}
		return nil
	})
	muxPair := NewDefaultMuxerPair(t, t.Name(), f)
	muxPair.Serve(t)

	stream, err := muxPair.OpenEdgeMuxStream(
		[]Header{{Name: "test-header", Value: "headerValue"}},
		nil,
	)
	// Close the underlying connection before telling the origin to write.
	_ = muxPair.EdgeConn.Close()
	close(sendC)
	if err != nil {
		t.Fatalf("error in OpenStream: %s", err)
	}
	responseBody := make([]byte, len(responseBuf))
	n, err := io.ReadFull(stream, responseBody)
	if err != io.EOF {
		t.Fatalf("unexpected error from (*MuxedStream).Read: %s", err)
	}
	if n != 0 {
		t.Fatalf("expected response body to have %d bytes, got %d", 0, n)
	}
	// The write ordering requirement explained in the origin handler applies here too.
	_, err = stream.Write(responseBuf)
	if err != io.EOF {
		t.Fatalf("unexpected error from (*MuxedStream).Write: %s", err)
	}
	<-handlerFinishC
}

func EchoHandler(stream *MuxedStream) error {
	var buf bytes.Buffer
	_, _ = fmt.Fprintf(&buf, "Hello, world!\n\n# REQUEST HEADERS:\n\n")
	for _, header := range stream.Headers {
		_, _ = fmt.Fprintf(&buf, "[%s] = %s\n", header.Name, header.Value)
	}
	_ = stream.WriteHeaders([]Header{
		{Name: ":status", Value: "200"},
		{Name: "server", Value: "Echo-server/1.0"},
		{Name: "date", Value: time.Now().Format(time.RFC850)},
		{Name: "content-type", Value: "text/html; charset=utf-8"},
		{Name: "content-length", Value: strconv.Itoa(buf.Len())},
	})
	_, _ = buf.WriteTo(stream)
	return nil
}

func TestOpenAfterDisconnect(t *testing.T) {
	for i := 0; i < 3; i++ {
		muxPair := NewDefaultMuxerPair(t, fmt.Sprintf("%s_%d", t.Name(), i), EchoHandler)
		muxPair.Serve(t)

		switch i {
		case 0:
			// Close both directions of the connection to cause EOF on both peers.
			_ = muxPair.OriginConn.Close()
			_ = muxPair.EdgeConn.Close()
		case 1:
			// Close origin conn to cause EOF on origin first.
			_ = muxPair.OriginConn.Close()
		case 2:
			// Close edge conn to cause EOF on edge first.
			_ = muxPair.EdgeConn.Close()
		}

		_, err := muxPair.OpenEdgeMuxStream(
			[]Header{{Name: "test-header", Value: "headerValue"}},
			nil,
		)
		if err != ErrStreamRequestConnectionClosed && err != ErrResponseHeadersConnectionClosed {
			t.Fatalf("case %v: unexpected error in OpenStream: %v", i, err)
		}
	}
}

func TestHPACK(t *testing.T) {
	muxPair := NewDefaultMuxerPair(t, t.Name(), EchoHandler)
	muxPair.Serve(t)

	stream, err := muxPair.OpenEdgeMuxStream(
		[]Header{
			{Name: ":method", Value: "RPC"},
			{Name: ":scheme", Value: "capnp"},
			{Name: ":path", Value: "*"},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("error in OpenStream: %s", err)
	}
	_ = stream.Close()

	for i := 0; i < 3; i++ {
		stream, err := muxPair.OpenEdgeMuxStream(
			[]Header{
				{Name: ":method", Value: "GET"},
				{Name: ":scheme", Value: "https"},
				{Name: ":authority", Value: "tunnel.otterlyadorable.co.uk"},
				{Name: ":path", Value: "/get"},
				{Name: "accept-encoding", Value: "gzip"},
				{Name: "cf-ray", Value: "378948953f044408-SFO-DOG"},
				{Name: "cf-visitor", Value: "{\"scheme\":\"https\"}"},
				{Name: "cf-connecting-ip", Value: "2400:cb00:0025:010d:0000:0000:0000:0001"},
				{Name: "x-forwarded-for", Value: "2400:cb00:0025:010d:0000:0000:0000:0001"},
				{Name: "x-forwarded-proto", Value: "https"},
				{Name: "accept-language", Value: "en-gb"},
				{Name: "referer", Value: "https://tunnel.otterlyadorable.co.uk/"},
				{Name: "cookie", Value: "__cfduid=d4555095065f92daedc059490771967d81493032162"},
				{Name: "connection", Value: "Keep-Alive"},
				{Name: "cf-ipcountry", Value: "US"},
				{Name: "accept", Value: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"},
				{Name: "user-agent", Value: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_12_5) AppleWebKit/603.2.4 (KHTML, like Gecko) Version/10.1.1 Safari/603.2.4"},
			},
			nil,
		)
		if err != nil {
			t.Fatalf("error in OpenStream: %s", err)
		}
		if len(stream.Headers) == 0 {
			t.Fatal("response has no headers")
		}
		if stream.Headers[0].Name != ":status" {
			t.Fatalf("first header should be status, found %s instead", stream.Headers[0].Name)
		}
		if stream.Headers[0].Value != "200" {
			t.Fatalf("expected status 200, got %s", stream.Headers[0].Value)
		}
		_, _ = io.ReadAll(stream)
		_ = stream.Close()
	}
}

func AssertIfPipeReadable(t *testing.T, pipe io.ReadCloser) {
	errC := make(chan error)
	go func() {
		b := []byte{0}
		n, err := pipe.Read(b)
		if n > 0 {
			t.Errorf("read pipe was not empty")
			return
		}
		errC <- err
	}()
	select {
	case err := <-errC:
		if err != nil {
			t.Fatalf("read error: %s", err)
		}
	case <-time.After(100 * time.Millisecond):
		// nothing to read
	}
}

func TestMultipleStreamsWithDictionaries(t *testing.T) {
	l := zerolog.Nop()

	for q := CompressionNone; q <= CompressionMax; q++ {
		htmlBody := `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN"` +
			`"http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd">` +
			`<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="en">` +
			`<head>` +
			`  <title>Your page title here</title>` +
			`</head>` +
			`<body>` +
			`<h1>Your major heading here</h1>` +
			`<p>` +
			`This is a regular text paragraph.` +
			`</p>` +
			`<ul>` +
			`  <li>` +
			`  First bullet of a bullet list.` +
			`  </li>` +
			`  <li>` +
			`  This is the <em>second</em> bullet.` +
			`  </li>` +
			`</ul>` +
			`</body>` +
			`</html>`

		f := MuxedStreamFunc(func(stream *MuxedStream) error {
			var contentType string
			var pathHeader Header

			for _, h := range stream.Headers {
				if h.Name == ":path" {
					pathHeader = h
					break
				}
			}

			if pathHeader.Name != ":path" {
				panic("Couldn't find :path header in test")
			}

			if strings.Contains(pathHeader.Value, "html") {
				contentType = "text/html; charset=utf-8"
			} else if strings.Contains(pathHeader.Value, "js") {
				contentType = "application/javascript"
			} else if strings.Contains(pathHeader.Value, "css") {
				contentType = "text/css"
			} else {
				contentType = "img/gif"
			}

			_ = stream.WriteHeaders([]Header{
				{Name: "content-type", Value: contentType},
			})
			_, _ = stream.Write([]byte(strings.Replace(htmlBody, "paragraph", pathHeader.Value, 1) + stream.Headers[5].Value))

			return nil
		})
		muxPair := NewCompressedMuxerPair(t, fmt.Sprintf("%s_%d", t.Name(), q), q, f)
		muxPair.Serve(t)

		var wg sync.WaitGroup

		paths := []string{
			"/html1",
			"/html2?sa:ds",
			"/html3",
			"/css1",
			"/html1",
			"/html2?sa:ds",
			"/html3",
			"/css1",
			"/css2",
			"/css3",
			"/js",
			"/js",
			"/js",
			"/js2",
			"/img2",
			"/html1",
			"/html2?sa:ds",
			"/html3",
			"/css1",
			"/css2",
			"/css3",
			"/js",
			"/js",
			"/js",
			"/js2",
			"/img1",
		}

		wg.Add(len(paths))
		errorsC := make(chan error, len(paths))

		for i, s := range paths {
			go func(index int, path string) {
				defer wg.Done()
				stream, err := muxPair.OpenEdgeMuxStream(
					[]Header{
						{Name: ":method", Value: "GET"},
						{Name: ":scheme", Value: "https"},
						{Name: ":authority", Value: "tunnel.otterlyadorable.co.uk"},
						{Name: ":path", Value: path},
						{Name: "cf-ray", Value: "378948953f044408-SFO-DOG"},
						{Name: "idx", Value: strconv.Itoa(index)},
						{Name: "accept-encoding", Value: "gzip, br"},
					},
					nil,
				)
				if err != nil {
					errorsC <- fmt.Errorf("error in OpenStream: %v", err)
					return
				}

				expectBody := strings.Replace(htmlBody, "paragraph", path, 1) + strconv.Itoa(index)
				responseBody := make([]byte, len(expectBody)*2)
				n, err := stream.Read(responseBody)
				if err != nil {
					errorsC <- fmt.Errorf("stream %d error from (*MuxedStream).Read: %s", stream.streamID, err)
					return
				}
				if n != len(expectBody) {
					errorsC <- fmt.Errorf("stream %d expected response body to have %d bytes, got %d", stream.streamID, len(expectBody), n)
					return
				}
				if string(responseBody[:n]) != expectBody {
					errorsC <- fmt.Errorf("stream %d expected response body %s, got %s", stream.streamID, expectBody, responseBody[:n])
					return
				}
			}(i, s)
		}

		wg.Wait()
		close(errorsC)
		testFail := false
		for err := range errorsC {
			testFail = true
			l.Error().Msgf("%s", err)
		}
		if testFail {
			t.Fatalf("TestMultipleStreams failed")
		}

		originMuxMetrics := muxPair.OriginMux.Metrics()
		if q > CompressionNone && originMuxMetrics.CompBytesBefore.Value() <= 10*originMuxMetrics.CompBytesAfter.Value() {
			t.Fatalf("Cross-stream compression is expected to give a better compression ratio")
		}
	}
}

func sampleSiteHandler(files map[string][]byte) MuxedStreamFunc {
	return func(stream *MuxedStream) error {
		var contentType string
		var pathHeader Header

		for _, h := range stream.Headers {
			if h.Name == ":path" {
				pathHeader = h
				break
			}
		}

		if pathHeader.Name != ":path" {
			return fmt.Errorf("Couldn't find :path header in test")
		}

		if strings.Contains(pathHeader.Value, "html") {
			contentType = "text/html; charset=utf-8"
		} else if strings.Contains(pathHeader.Value, "js") {
			contentType = "application/javascript"
		} else if strings.Contains(pathHeader.Value, "css") {
			contentType = "text/css"
		} else {
			contentType = "img/gif"
		}
		_ = stream.WriteHeaders([]Header{
			{Name: "content-type", Value: contentType},
		})
		log.Debug().Msgf("Wrote headers for stream %s", pathHeader.Value)
		file, ok := files[pathHeader.Value]
		if !ok {
			return fmt.Errorf("%s content is not preloaded", pathHeader.Value)
		}
		_, _ = stream.Write(file)
		log.Debug().Msgf("Wrote body for stream %s", pathHeader.Value)
		return nil
	}
}

func sampleSiteTest(muxPair *DefaultMuxerPair, path string, files map[string][]byte) error {
	stream, err := muxPair.OpenEdgeMuxStream(
		[]Header{
			{Name: ":method", Value: "GET"},
			{Name: ":scheme", Value: "https"},
			{Name: ":authority", Value: "tunnel.otterlyadorable.co.uk"},
			{Name: ":path", Value: path},
			{Name: "accept-encoding", Value: "br, gzip"},
			{Name: "cf-ray", Value: "378948953f044408-SFO-DOG"},
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("error in OpenStream: %v", err)
	}
	file, ok := files[path]
	if !ok {
		return fmt.Errorf("%s content is not preloaded", path)
	}
	responseBody := make([]byte, len(file))
	n, err := io.ReadFull(stream, responseBody)
	if err != nil {
		return fmt.Errorf("error from (*MuxedStream).Read: %v", err)
	}
	if n != len(file) {
		return fmt.Errorf("expected response body to have %d bytes, got %d", len(file), n)
	}
	if string(responseBody[:n]) != string(file) {
		return fmt.Errorf("expected response body %s, got %s", file, responseBody[:n])
	}
	return nil
}

func loadSampleFiles(paths []string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	for _, path := range paths {
		if _, ok := files[path]; !ok {
			expectBody, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			files[path] = expectBody
		}
	}
	return files, nil
}

func TestSampleSiteWithDictionaries(t *testing.T) {
	paths := []string{
		"./sample/index.html",
		"./sample/index2.html",
		"./sample/index1.html",
		"./sample/ghost-url.min.js",
		"./sample/jquery.fitvids.js",
		"./sample/index1.html",
		"./sample/index2.html",
		"./sample/index.html",
	}
	files, err := loadSampleFiles(paths)
	assert.NoError(t, err)

	for q := CompressionNone; q <= CompressionMax; q++ {
		muxPair := NewCompressedMuxerPair(t, fmt.Sprintf("%s_%d", t.Name(), q), q, sampleSiteHandler(files))
		muxPair.Serve(t)

		var wg sync.WaitGroup
		errC := make(chan error, len(paths))

		wg.Add(len(paths))
		for _, s := range paths {
			go func(path string) {
				defer wg.Done()
				errC <- sampleSiteTest(muxPair, path, files)
			}(s)
		}

		wg.Wait()
		close(errC)

		for err := range errC {
			assert.NoError(t, err)
		}

		originMuxMetrics := muxPair.OriginMux.Metrics()
		if q > CompressionNone && originMuxMetrics.CompBytesBefore.Value() <= 10*originMuxMetrics.CompBytesAfter.Value() {
			t.Fatalf("Cross-stream compression is expected to give a better compression ratio")
		}
	}
}

func TestLongSiteWithDictionaries(t *testing.T) {
	paths := []string{
		"./sample/index.html",
		"./sample/index1.html",
		"./sample/index2.html",
		"./sample/ghost-url.min.js",
		"./sample/jquery.fitvids.js",
	}
	files, err := loadSampleFiles(paths)
	assert.NoError(t, err)
	for q := CompressionNone; q <= CompressionMedium; q++ {
		muxPair := NewCompressedMuxerPair(t, fmt.Sprintf("%s_%d", t.Name(), q), q, sampleSiteHandler(files))
		muxPair.Serve(t)

		rand.Seed(time.Now().Unix())

		tstLen := 500
		errGroup, _ := errgroup.WithContext(context.Background())
		for i := 0; i < tstLen; i++ {
			errGroup.Go(func() error {
				path := paths[rand.Int()%len(paths)]
				return sampleSiteTest(muxPair, path, files)
			})
		}
		assert.NoError(t, errGroup.Wait())

		originMuxMetrics := muxPair.OriginMux.Metrics()
		if q > CompressionNone && originMuxMetrics.CompBytesBefore.Value() <= 10*originMuxMetrics.CompBytesAfter.Value() {
			t.Fatalf("Cross-stream compression is expected to give a better compression ratio")
		}
	}
}

func BenchmarkOpenStream(b *testing.B) {
	const streams = 5000
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		f := MuxedStreamFunc(func(stream *MuxedStream) error {
			if len(stream.Headers) != 1 {
				b.Fatalf("expected %d headers, got %d", 1, len(stream.Headers))
			}
			if stream.Headers[0].Name != "test-header" {
				b.Fatalf("expected header name %s, got %s", "test-header", stream.Headers[0].Name)
			}
			if stream.Headers[0].Value != "headerValue" {
				b.Fatalf("expected header value %s, got %s", "headerValue", stream.Headers[0].Value)
			}
			_ = stream.WriteHeaders([]Header{
				{Name: "response-header", Value: "responseValue"},
			})
			return nil
		})
		muxPair := NewDefaultMuxerPair(b, fmt.Sprintf("%s_%d", b.Name(), i), f)
		muxPair.Serve(b)
		b.StartTimer()
		openStreams(b, muxPair, streams)
	}
}

func openStreams(b *testing.B, muxPair *DefaultMuxerPair, n int) {
	errGroup, _ := errgroup.WithContext(context.Background())
	for i := 0; i < n; i++ {
		errGroup.Go(func() error {
			_, err := muxPair.OpenEdgeMuxStream(
				[]Header{{Name: "test-header", Value: "headerValue"}},
				nil,
			)
			return err
		})
	}
	assert.NoError(b, errGroup.Wait())
}

func BenchmarkSingleStreamLargeResponseBody(b *testing.B) {
	const bodySize = 1 << 24

	const writeBufferSize = 16 << 10
	const writeN = bodySize / writeBufferSize
	payload := make([]byte, writeBufferSize)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	const readBufferSize = 16 << 10
	const readN = bodySize / readBufferSize
	responseBody := make([]byte, readBufferSize)

	f := MuxedStreamFunc(func(stream *MuxedStream) error {
		if len(stream.Headers) != 1 {
			b.Fatalf("expected %d headers, got %d", 1, len(stream.Headers))
		}
		if stream.Headers[0].Name != "test-header" {
			b.Fatalf("expected header name %s, got %s", "test-header", stream.Headers[0].Name)
		}
		if stream.Headers[0].Value != "headerValue" {
			b.Fatalf("expected header value %s, got %s", "headerValue", stream.Headers[0].Value)
		}
		_ = stream.WriteHeaders([]Header{
			{Name: "response-header", Value: "responseValue"},
		})
		for i := 0; i < writeN; i++ {
			n, err := stream.Write(payload)
			if err != nil {
				b.Fatalf("origin write error: %s", err)
			}
			if n != len(payload) {
				b.Fatalf("origin short write: %d/%d bytes", n, len(payload))
			}
		}

		return nil
	})

	name := fmt.Sprintf("%s_%d", b.Name(), rand.Int())
	origin, edge := net.Pipe()

	muxPair := &DefaultMuxerPair{
		OriginMuxConfig: MuxerConfig{
			Timeout:                 testHandshakeTimeout,
			Handler:                 f,
			IsClient:                true,
			Name:                    "origin",
			Log:                     &log,
			DefaultWindowSize:       defaultWindowSize,
			MaxWindowSize:           maxWindowSize,
			StreamWriteBufferMaxLen: defaultWriteBufferMaxLen,
			HeartbeatInterval:       defaultTimeout,
			MaxHeartbeats:           defaultRetries,
		},
		OriginConn: origin,
		EdgeMuxConfig: MuxerConfig{
			Timeout:                 testHandshakeTimeout,
			IsClient:                false,
			Name:                    "edge",
			Log:                     &log,
			DefaultWindowSize:       defaultWindowSize,
			MaxWindowSize:           maxWindowSize,
			StreamWriteBufferMaxLen: defaultWriteBufferMaxLen,
			HeartbeatInterval:       defaultTimeout,
			MaxHeartbeats:           defaultRetries,
		},
		EdgeConn: edge,
		doneC:    make(chan struct{}),
	}
	assert.NoError(b, muxPair.Handshake(name))
	muxPair.Serve(b)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		stream, err := muxPair.OpenEdgeMuxStream(
			[]Header{{Name: "test-header", Value: "headerValue"}},
			nil,
		)
		if err != nil {
			b.Fatalf("error in OpenStream: %s", err)
		}
		if len(stream.Headers) != 1 {
			b.Fatalf("expected %d headers, got %d", 1, len(stream.Headers))
		}
		if stream.Headers[0].Name != "response-header" {
			b.Fatalf("expected header name %s, got %s", "response-header", stream.Headers[0].Name)
		}
		if stream.Headers[0].Value != "responseValue" {
			b.Fatalf("expected header value %s, got %s", "responseValue", stream.Headers[0].Value)
		}

		for k := 0; k < readN; k++ {
			n, err := io.ReadFull(stream, responseBody)
			if err != nil {
				b.Fatalf("error from (*MuxedStream).Read: %s", err)
			}
			if n != len(responseBody) {
				b.Fatalf("expected response body to have %d bytes, got %d", len(responseBody), n)
			}
		}
	}
}
