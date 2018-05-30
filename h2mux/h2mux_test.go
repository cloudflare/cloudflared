package h2mux

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

func TestMain(m *testing.M) {
	if os.Getenv("VERBOSE") == "1" {
		log.SetLevel(log.DebugLevel)
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

func NewDefaultMuxerPair() *DefaultMuxerPair {
	origin, edge := net.Pipe()
	return &DefaultMuxerPair{
		OriginMuxConfig: MuxerConfig{Timeout: time.Second, IsClient: true, Name: "origin"},
		OriginConn:      origin,
		EdgeMuxConfig:   MuxerConfig{Timeout: time.Second, IsClient: false, Name: "edge"},
		EdgeConn:        edge,
		doneC:           make(chan struct{}),
	}
}

func (p *DefaultMuxerPair) Handshake(t *testing.T) {
	edgeErrC := make(chan error)
	originErrC := make(chan error)
	go func() {
		var err error
		p.EdgeMux, err = Handshake(p.EdgeConn, p.EdgeConn, p.EdgeMuxConfig)
		edgeErrC <- err
	}()
	go func() {
		var err error
		p.OriginMux, err = Handshake(p.OriginConn, p.OriginConn, p.OriginMuxConfig)
		originErrC <- err
	}()

	select {
	case err := <-edgeErrC:
		if err != nil {
			t.Fatalf("edge handshake failure: %s", err)
		}
	case <-time.After(time.Second * 5):
		t.Fatalf("edge handshake timeout")
	}

	select {
	case err := <-originErrC:
		if err != nil {
			t.Fatalf("origin handshake failure: %s", err)
		}
	case <-time.After(time.Second * 5):
		t.Fatalf("origin handshake timeout")
	}
}

func (p *DefaultMuxerPair) HandshakeAndServe(t *testing.T) {
	ctx := context.Background()
	p.Handshake(t)
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

func TestHandshake(t *testing.T) {
	muxPair := NewDefaultMuxerPair()
	muxPair.Handshake(t)
	AssertIfPipeReadable(t, muxPair.OriginConn)
	AssertIfPipeReadable(t, muxPair.EdgeConn)
}

func TestSingleStream(t *testing.T) {
	closeC := make(chan struct{})
	muxPair := NewDefaultMuxerPair()
	muxPair.OriginMuxConfig.Handler = MuxedStreamFunc(func(stream *MuxedStream) error {
		defer close(closeC)
		if len(stream.Headers) != 1 {
			t.Fatalf("expected %d headers, got %d", 1, len(stream.Headers))
		}
		if stream.Headers[0].Name != "test-header" {
			t.Fatalf("expected header name %s, got %s", "test-header", stream.Headers[0].Name)
		}
		if stream.Headers[0].Value != "headerValue" {
			t.Fatalf("expected header value %s, got %s", "headerValue", stream.Headers[0].Value)
		}
		stream.WriteHeaders([]Header{
			{Name: "response-header", Value: "responseValue"},
		})
		buf := []byte("Hello world")
		stream.Write(buf)
		// after this receive, the edge closed the stream
		<-closeC
		n, err := io.ReadFull(stream, buf)
		if n > 0 {
			t.Fatalf("read %d bytes after EOF", n)
		}
		if err != io.EOF {
			t.Fatalf("expected EOF, got %s", err)
		}
		return nil
	})
	muxPair.HandshakeAndServe(t)

	stream, err := muxPair.EdgeMux.OpenStream(
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
	stream.Close()
	closeC <- struct{}{}
	n, err = stream.Write([]byte("aaaaa"))
	if n > 0 {
		t.Fatalf("wrote %d bytes after EOF", n)
	}
	if err != io.EOF {
		t.Fatalf("expected EOF, got %s", err)
	}
	<-closeC
}

func TestSingleStreamLargeResponseBody(t *testing.T) {
	muxPair := NewDefaultMuxerPair()
	bodySize := 1 << 24
	streamReady := make(chan struct{})
	muxPair.OriginMuxConfig.Handler = MuxedStreamFunc(func(stream *MuxedStream) error {
		if len(stream.Headers) != 1 {
			t.Fatalf("expected %d headers, got %d", 1, len(stream.Headers))
		}
		if stream.Headers[0].Name != "test-header" {
			t.Fatalf("expected header name %s, got %s", "test-header", stream.Headers[0].Name)
		}
		if stream.Headers[0].Value != "headerValue" {
			t.Fatalf("expected header value %s, got %s", "headerValue", stream.Headers[0].Value)
		}
		stream.WriteHeaders([]Header{
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
		t.Log("Payload written; signaling that the stream is ready")
		streamReady <- struct{}{}

		return nil
	})
	muxPair.HandshakeAndServe(t)

	stream, err := muxPair.EdgeMux.OpenStream(
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

	<-streamReady
	t.Log("Received stream ready signal; resuming the test")

	n, err := io.ReadFull(stream, responseBody)
	if err != nil {
		t.Fatalf("error from (*MuxedStream).Read: %s", err)
	}
	if n != len(responseBody) {
		t.Fatalf("expected response body to have %d bytes, got %d", len(responseBody), n)
	}
}

func TestMultipleStreams(t *testing.T) {
	muxPair := NewDefaultMuxerPair()
	maxStreams := 64
	errorsC := make(chan error, maxStreams)
	muxPair.OriginMuxConfig.Handler = MuxedStreamFunc(func(stream *MuxedStream) error {
		if len(stream.Headers) != 1 {
			t.Fatalf("expected %d headers, got %d", 1, len(stream.Headers))
		}
		if stream.Headers[0].Name != "client-token" {
			t.Fatalf("expected header name %s, got %s", "client-token", stream.Headers[0].Name)
		}
		log.Debugf("Got request for stream %s", stream.Headers[0].Value)
		stream.WriteHeaders([]Header{
			{Name: "response-token", Value: stream.Headers[0].Value},
		})
		log.Debugf("Wrote headers for stream %s", stream.Headers[0].Value)
		stream.Write([]byte("OK"))
		log.Debugf("Wrote body for stream %s", stream.Headers[0].Value)
		return nil
	})
	muxPair.HandshakeAndServe(t)

	var wg sync.WaitGroup
	wg.Add(maxStreams)
	for i := 0; i < maxStreams; i++ {
		go func(tokenId int) {
			defer wg.Done()
			tokenString := fmt.Sprintf("%d", tokenId)
			stream, err := muxPair.EdgeMux.OpenStream(
				[]Header{{Name: "client-token", Value: tokenString}},
				nil,
			)
			log.Debugf("Got headers for stream %d", tokenId)
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
		log.Error(err)
	}
	if testFail {
		t.Fatalf("TestMultipleStreamsFlowControl failed")
	}
}

func TestMultipleStreamsFlowControl(t *testing.T) {
	maxStreams := 32
	errorsC := make(chan error, maxStreams)
	streamReady := make(chan struct{})
	responseSizes := make([]int32, maxStreams)
	for i := 0; i < maxStreams; i++ {
		responseSizes[i] = rand.Int31n(int32(defaultWindowSize << 4))
	}
	muxPair := NewDefaultMuxerPair()
	muxPair.OriginMuxConfig.Handler = MuxedStreamFunc(func(stream *MuxedStream) error {
		if len(stream.Headers) != 1 {
			t.Fatalf("expected %d headers, got %d", 1, len(stream.Headers))
		}
		if stream.Headers[0].Name != "test-header" {
			t.Fatalf("expected header name %s, got %s", "test-header", stream.Headers[0].Name)
		}
		if stream.Headers[0].Value != "headerValue" {
			t.Fatalf("expected header value %s, got %s", "headerValue", stream.Headers[0].Value)
		}
		stream.WriteHeaders([]Header{
			{Name: "response-header", Value: "responseValue"},
		})
		payload := make([]byte, responseSizes[(stream.streamID-2)/2])
		for i := range payload {
			payload[i] = byte(i % 256)
		}
		n, err := stream.Write(payload)
		streamReady <- struct{}{}
		if err != nil {
			t.Fatalf("origin write error: %s", err)
		}
		if n != len(payload) {
			t.Fatalf("origin short write: %d/%d bytes", n, len(payload))
		}
		return nil
	})
	muxPair.HandshakeAndServe(t)

	var wg sync.WaitGroup
	wg.Add(maxStreams)
	for i := 0; i < maxStreams; i++ {
		go func(tokenId int) {
			defer wg.Done()
			stream, err := muxPair.EdgeMux.OpenStream(
				[]Header{{Name: "test-header", Value: "headerValue"}},
				nil,
			)
			if err != nil {
				errorsC <- fmt.Errorf("stream %d error in OpenStream: %s", stream.streamID, err)
				return
			}
			if len(stream.Headers) != 1 {
				errorsC <- fmt.Errorf("stream %d expected %d headers, got %d", stream.streamID, 1, len(stream.Headers))
				return
			}
			if stream.Headers[0].Name != "response-header" {
				errorsC <- fmt.Errorf("stream %d expected header name %s, got %s", stream.streamID, "response-header", stream.Headers[0].Name)
				return
			}
			if stream.Headers[0].Value != "responseValue" {
				errorsC <- fmt.Errorf("stream %d expected header value %s, got %s", stream.streamID, "responseValue", stream.Headers[0].Value)
				return
			}

			<-streamReady
			responseBody := make([]byte, responseSizes[(stream.streamID-2)/2])
			n, err := io.ReadFull(stream, responseBody)
			if err != nil {
				errorsC <- fmt.Errorf("stream %d error from (*MuxedStream).Read: %s", stream.streamID, err)
				return
			}
			if n != len(responseBody) {
				errorsC <- fmt.Errorf("stream %d expected response body to have %d bytes, got %d", stream.streamID, len(responseBody), n)
				return
			}
		}(i)
	}
	wg.Wait()
	close(errorsC)
	testFail := false
	for err := range errorsC {
		testFail = true
		log.Error(err)
	}
	if testFail {
		t.Fatalf("TestMultipleStreamsFlowControl failed")
	}
}

func TestGracefulShutdown(t *testing.T) {
	sendC := make(chan struct{})
	responseBuf := bytes.Repeat([]byte("Hello world"), 65536)
	muxPair := NewDefaultMuxerPair()
	muxPair.OriginMuxConfig.Handler = MuxedStreamFunc(func(stream *MuxedStream) error {
		stream.WriteHeaders([]Header{
			{Name: "response-header", Value: "responseValue"},
		})
		<-sendC
		log.Debugf("Writing %d bytes", len(responseBuf))
		stream.Write(responseBuf)
		stream.CloseWrite()
		log.Debugf("Wrote %d bytes", len(responseBuf))
		// Reading from the stream will block until the edge closes its end of the stream.
		// Otherwise, we'll close the whole connection before receiving the 'stream closed'
		// message from the edge.
		// Graceful shutdown works if you omit this, it just gives spurious errors for now -
		// TODO ignore errors when writing 'stream closed' and we're shutting down.
		stream.Read([]byte{0})
		log.Debugf("Handler ends")
		return nil
	})
	muxPair.HandshakeAndServe(t)

	stream, err := muxPair.EdgeMux.OpenStream(
		[]Header{{Name: "test-header", Value: "headerValue"}},
		nil,
	)
	// Start graceful shutdown of the edge mux - this should also close the origin mux when done
	muxPair.EdgeMux.Shutdown()
	close(sendC)
	if err != nil {
		t.Fatalf("error in OpenStream: %s", err)
	}
	responseBody := make([]byte, len(responseBuf))
	log.Debugf("Waiting for %d bytes", len(responseBuf))
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
	stream.Close()
	muxPair.Wait(t)
}

func TestUnexpectedShutdown(t *testing.T) {
	sendC := make(chan struct{})
	handlerFinishC := make(chan struct{})
	responseBuf := bytes.Repeat([]byte("Hello world"), 65536)
	muxPair := NewDefaultMuxerPair()
	muxPair.OriginMuxConfig.Handler = MuxedStreamFunc(func(stream *MuxedStream) error {
		defer close(handlerFinishC)
		stream.WriteHeaders([]Header{
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
	muxPair.HandshakeAndServe(t)

	stream, err := muxPair.EdgeMux.OpenStream(
		[]Header{{Name: "test-header", Value: "headerValue"}},
		nil,
	)
	// Close the underlying connection before telling the origin to write.
	muxPair.EdgeConn.Close()
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
	fmt.Fprintf(&buf, "Hello, world!\n\n# REQUEST HEADERS:\n\n")
	for _, header := range stream.Headers {
		fmt.Fprintf(&buf, "[%s] = %s\n", header.Name, header.Value)
	}
	stream.WriteHeaders([]Header{
		{Name: ":status", Value: "200"},
		{Name: "server", Value: "Echo-server/1.0"},
		{Name: "date", Value: time.Now().Format(time.RFC850)},
		{Name: "content-type", Value: "text/html; charset=utf-8"},
		{Name: "content-length", Value: strconv.Itoa(buf.Len())},
	})
	buf.WriteTo(stream)
	return nil
}

func TestOpenAfterDisconnect(t *testing.T) {
	for i := 0; i < 3; i++ {
		muxPair := NewDefaultMuxerPair()
		muxPair.OriginMuxConfig.Handler = MuxedStreamFunc(EchoHandler)
		muxPair.HandshakeAndServe(t)

		switch i {
		case 0:
			// Close both directions of the connection to cause EOF on both peers.
			muxPair.OriginConn.Close()
			muxPair.EdgeConn.Close()
		case 1:
			// Close origin conn to cause EOF on origin first.
			muxPair.OriginConn.Close()
		case 2:
			// Close edge conn to cause EOF on edge first.
			muxPair.EdgeConn.Close()
		}

		_, err := muxPair.EdgeMux.OpenStream(
			[]Header{{Name: "test-header", Value: "headerValue"}},
			nil,
		)
		if err != ErrConnectionClosed {
			t.Fatalf("unexpected error in OpenStream: %s", err)
		}
	}
}

func TestHPACK(t *testing.T) {
	muxPair := NewDefaultMuxerPair()
	muxPair.OriginMuxConfig.Handler = MuxedStreamFunc(EchoHandler)
	muxPair.HandshakeAndServe(t)

	stream, err := muxPair.EdgeMux.OpenStream(
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
	stream.Close()

	for i := 0; i < 3; i++ {
		stream, err := muxPair.EdgeMux.OpenStream(
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
		ioutil.ReadAll(stream)
		stream.Close()
	}
}

func AssertIfPipeReadable(t *testing.T, pipe io.ReadCloser) {
	errC := make(chan error)
	go func() {
		b := []byte{0}
		n, err := pipe.Read(b)
		if n > 0 {
			t.Fatalf("read pipe was not empty")
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
