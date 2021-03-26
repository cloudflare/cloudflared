package h2mux

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var (
	methodHeader = Header{
		Name:  ":method",
		Value: "GET",
	}
	schemeHeader = Header{
		Name:  ":scheme",
		Value: "https",
	}
	pathHeader = Header{
		Name:  ":path",
		Value: "/api/tunnels",
	}
	respStatusHeader = Header{
		Name:  ":status",
		Value: "200",
	}
)

type mockOriginStreamHandler struct {
	stream *MuxedStream
}

func (mosh *mockOriginStreamHandler) ServeStream(stream *MuxedStream) error {
	mosh.stream = stream
	// Echo tunnel hostname in header
	stream.WriteHeaders([]Header{respStatusHeader})
	return nil
}

func assertOpenStreamSucceed(t *testing.T, stream *MuxedStream, err error) {
	assert.NoError(t, err)
	assert.Len(t, stream.Headers, 1)
	assert.Equal(t, respStatusHeader, stream.Headers[0])
}

func TestMissingHeaders(t *testing.T) {
	originHandler := &mockOriginStreamHandler{}
	muxPair := NewDefaultMuxerPair(t, t.Name(), originHandler.ServeStream)
	muxPair.Serve(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	reqHeaders := []Header{
		{
			Name:  "content-type",
			Value: "application/json",
		},
	}

	stream, err := muxPair.EdgeMux.OpenStream(ctx, reqHeaders, nil)
	assertOpenStreamSucceed(t, stream, err)

	assert.Empty(t, originHandler.stream.method)
	assert.Empty(t, originHandler.stream.path)
}

func TestReceiveHeaderData(t *testing.T) {
	originHandler := &mockOriginStreamHandler{}
	muxPair := NewDefaultMuxerPair(t, t.Name(), originHandler.ServeStream)
	muxPair.Serve(t)

	reqHeaders := []Header{
		methodHeader,
		schemeHeader,
		pathHeader,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	stream, err := muxPair.EdgeMux.OpenStream(ctx, reqHeaders, nil)
	assertOpenStreamSucceed(t, stream, err)

	assert.Equal(t, methodHeader.Value, originHandler.stream.method)
	assert.Equal(t, pathHeader.Value, originHandler.stream.path)
}
