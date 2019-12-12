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
	tunnelHostnameHeader = Header{
		Name:  CloudflaredProxyTunnelHostnameHeader,
		Value: "tunnel.example.com",
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

func getCloudflaredProxyTunnelHostnameHeader(stream *MuxedStream) string {
	for _, header := range stream.Headers {
		if header.Name == CloudflaredProxyTunnelHostnameHeader {
			return header.Value
		}
	}
	return ""
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

	// Request doesn't contain CloudflaredProxyTunnelHostnameHeader
	stream, err := muxPair.EdgeMux.OpenStream(ctx, reqHeaders, nil)
	assertOpenStreamSucceed(t, stream, err)

	assert.Empty(t, originHandler.stream.method)
	assert.Empty(t, originHandler.stream.path)
	assert.False(t, originHandler.stream.TunnelHostname().IsSet())
}

func TestReceiveHeaderData(t *testing.T) {
	originHandler := &mockOriginStreamHandler{}
	muxPair := NewDefaultMuxerPair(t, t.Name(), originHandler.ServeStream)
	muxPair.Serve(t)

	reqHeaders := []Header{
		methodHeader,
		schemeHeader,
		pathHeader,
		tunnelHostnameHeader,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	reqHeaders = append(reqHeaders, tunnelHostnameHeader)
	stream, err := muxPair.EdgeMux.OpenStream(ctx, reqHeaders, nil)
	assertOpenStreamSucceed(t, stream, err)

	assert.Equal(t, methodHeader.Value, originHandler.stream.method)
	assert.Equal(t, pathHeader.Value, originHandler.stream.path)
	assert.True(t, originHandler.stream.TunnelHostname().IsSet())
	assert.Equal(t, tunnelHostnameHeader.Value, originHandler.stream.TunnelHostname().String())
}
