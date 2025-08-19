package connection

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/client"
	cfdflow "github.com/cloudflare/cloudflared/flow"

	cfdquic "github.com/cloudflare/cloudflared/quic"
	"github.com/cloudflare/cloudflared/tracing"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	rpcquic "github.com/cloudflare/cloudflared/tunnelrpc/quic"
)

const (
	// HTTPHeaderKey is used to get or set http headers in QUIC ALPN if the underlying proxy connection type is HTTP.
	HTTPHeaderKey = "HttpHeader"
	// HTTPMethodKey is used to get or set http method in QUIC ALPN if the underlying proxy connection type is HTTP.
	HTTPMethodKey = "HttpMethod"
	// HTTPHostKey is used to get or set http host in QUIC ALPN if the underlying proxy connection type is HTTP.
	HTTPHostKey = "HttpHost"

	QUICMetadataFlowID = "FlowID"
)

// quicConnection represents the type that facilitates Proxying via QUIC streams.
type quicConnection struct {
	conn                 quic.Connection
	logger               *zerolog.Logger
	orchestrator         Orchestrator
	datagramHandler      DatagramSessionHandler
	controlStreamHandler ControlStreamHandler
	connOptions          *client.ConnectionOptionsSnapshot
	connIndex            uint8

	rpcTimeout         time.Duration
	streamWriteTimeout time.Duration
	gracePeriod        time.Duration
}

// NewTunnelConnection takes a [quic.Connection] to wrap it for use with cloudflared application logic.
func NewTunnelConnection(
	ctx context.Context,
	conn quic.Connection,
	connIndex uint8,
	orchestrator Orchestrator,
	datagramSessionHandler DatagramSessionHandler,
	controlStreamHandler ControlStreamHandler,
	connOptions *client.ConnectionOptionsSnapshot,
	rpcTimeout time.Duration,
	streamWriteTimeout time.Duration,
	gracePeriod time.Duration,
	logger *zerolog.Logger,
) TunnelConnection {
	return &quicConnection{
		conn:                 conn,
		logger:               logger,
		orchestrator:         orchestrator,
		datagramHandler:      datagramSessionHandler,
		controlStreamHandler: controlStreamHandler,
		connOptions:          connOptions,
		connIndex:            connIndex,
		rpcTimeout:           rpcTimeout,
		streamWriteTimeout:   streamWriteTimeout,
		gracePeriod:          gracePeriod,
	}
}

// Serve starts a QUIC connection that begins accepting streams.
// Returning a nil error means cloudflared will exit for good and will not attempt to reconnect.
func (q *quicConnection) Serve(ctx context.Context) error {
	// The edge assumes the first stream is used for the control plane
	controlStream, err := q.conn.OpenStream()
	if err != nil {
		return fmt.Errorf("failed to open a registration control stream: %w", err)
	}

	// If either goroutine returns a non nil error, then the error group cancels the context, thus also canceling the
	// other goroutines. We enforce returning a not-nil error for each function started in the errgroup by logging
	// the error returned and returning a custom error type instead.
	errGroup, ctx := errgroup.WithContext(ctx)

	// Close the quic connection if any of the following routines return from the errgroup (regardless of their error)
	// because they are no longer processing requests for the connection.
	defer q.Close()

	// Start the control stream routine
	errGroup.Go(func() error {
		// err is equal to nil if we exit due to unregistration. If that happens we want to wait the full
		// amount of the grace period, allowing requests to finish before we cancel the context, which will
		// make cloudflared exit.
		if err := q.serveControlStream(ctx, controlStream); err == nil {
			if q.gracePeriod > 0 {
				// In Go1.23 this can be removed and replaced with time.Ticker
				// see https://pkg.go.dev/time#Tick
				ticker := time.NewTicker(q.gracePeriod)
				defer ticker.Stop()
				select {
				case <-ctx.Done():
				case <-ticker.C:
				}
			}
		}
		if err != nil {
			q.logger.Error().Err(err).Msg("failed to serve the control stream")
		}
		return &ControlStreamError{}
	})
	// Start the accept stream loop routine
	errGroup.Go(func() error {
		err := q.acceptStream(ctx)
		if err != nil {
			q.logger.Error().Err(err).Msg("failed to accept incoming stream requests")
		}
		return &StreamListenerError{}
	})
	// Start the datagram handler routine
	errGroup.Go(func() error {
		err := q.datagramHandler.Serve(ctx)
		if err != nil {
			q.logger.Error().Err(err).Msg("failed to run the datagram handler")
		}
		return &DatagramManagerError{}
	})

	return errGroup.Wait()
}

// serveControlStream will serve the RPC; blocking until the control plane is done.
func (q *quicConnection) serveControlStream(ctx context.Context, controlStream quic.Stream) error {
	return q.controlStreamHandler.ServeControlStream(ctx, controlStream, q.connOptions.ConnectionOptions(), q.orchestrator)
}

// Close the connection with no errors specified.
func (q *quicConnection) Close() {
	_ = q.conn.CloseWithError(0, "")
}

func (q *quicConnection) acceptStream(ctx context.Context) error {
	for {
		quicStream, err := q.conn.AcceptStream(ctx)
		if err != nil {
			// context.Canceled is usually a user ctrl+c. We don't want to log an error here as it's intentional.
			if errors.Is(err, context.Canceled) || q.controlStreamHandler.IsStopped() {
				return nil
			}
			return fmt.Errorf("failed to accept QUIC stream: %w", err)
		}
		go q.runStream(quicStream)
	}
}

func (q *quicConnection) runStream(quicStream quic.Stream) {
	ctx := quicStream.Context()
	stream := cfdquic.NewSafeStreamCloser(quicStream, q.streamWriteTimeout, q.logger)
	defer stream.Close()

	// we are going to fuse readers/writers from stream <- cloudflared -> origin, and we want to guarantee that
	// code executed in the code path of handleStream don't trigger an earlier close to the downstream write stream.
	// So, we wrap the stream with a no-op write closer and only this method can actually close write side of the stream.
	// A call to close will simulate a close to the read-side, which will fail subsequent reads.
	noCloseStream := &nopCloserReadWriter{ReadWriteCloser: stream}
	ss := rpcquic.NewCloudflaredServer(q.handleDataStream, q.datagramHandler, q, q.rpcTimeout)
	if err := ss.Serve(ctx, noCloseStream); err != nil {
		q.logger.Debug().Err(err).Msg("Failed to handle QUIC stream")

		// if we received an error at this level, then close write side of stream with an error, which will result in
		// RST_STREAM frame.
		quicStream.CancelWrite(0)
	}
}

func (q *quicConnection) handleDataStream(ctx context.Context, stream *rpcquic.RequestServerStream) error {
	request, err := stream.ReadConnectRequestData()
	if err != nil {
		return err
	}

	if err, connectResponseSent := q.dispatchRequest(ctx, stream, request); err != nil {
		q.logger.Err(err).Str("type", request.Type.String()).Str("dest", request.Dest).Msg("Request failed")

		// if the connectResponse was already sent and we had an error, we need to propagate it up, so that the stream is
		// closed with an RST_STREAM frame
		if connectResponseSent {
			return err
		}

		var metadata []pogs.Metadata
		// Check the type of error that was throw and add metadata that will help identify it on OTD.
		if errors.Is(err, cfdflow.ErrTooManyActiveFlows) {
			metadata = append(metadata, pogs.ErrorFlowConnectRateLimitedMetadata)
		}

		if writeRespErr := stream.WriteConnectResponseData(err, metadata...); writeRespErr != nil {
			return writeRespErr
		}
	}

	return nil
}

// dispatchRequest will dispatch the request to the origin depending on the type and returns an error if it occurs.
// Also returns if the connect response was sent to the downstream during processing of the origin request.
func (q *quicConnection) dispatchRequest(ctx context.Context, stream *rpcquic.RequestServerStream, request *pogs.ConnectRequest) (err error, connectResponseSent bool) {
	originProxy, err := q.orchestrator.GetOriginProxy()
	if err != nil {
		return err, false
	}

	switch request.Type {
	case pogs.ConnectionTypeHTTP, pogs.ConnectionTypeWebsocket:
		tracedReq, err := buildHTTPRequest(ctx, request, stream, q.connIndex, q.logger)
		if err != nil {
			return err, false
		}
		w := newHTTPResponseAdapter(stream)
		return originProxy.ProxyHTTP(&w, tracedReq, request.Type == pogs.ConnectionTypeWebsocket), w.connectResponseSent

	case pogs.ConnectionTypeTCP:
		rwa := &streamReadWriteAcker{RequestServerStream: stream}
		metadata := request.MetadataMap()
		return originProxy.ProxyTCP(ctx, rwa, &TCPRequest{
			Dest:      request.Dest,
			FlowID:    metadata[QUICMetadataFlowID],
			CfTraceID: metadata[tracing.TracerContextName],
			ConnIndex: q.connIndex,
		}), rwa.connectResponseSent
	default:
		return fmt.Errorf("unsupported error type: %s", request.Type), false
	}
}

// UpdateConfiguration is the RPC method invoked by edge when there is a new configuration
func (q *quicConnection) UpdateConfiguration(ctx context.Context, version int32, config []byte) *pogs.UpdateConfigurationResponse {
	return q.orchestrator.UpdateConfig(version, config)
}

// streamReadWriteAcker is a light wrapper over QUIC streams with a callback to send response back to
// the client.
type streamReadWriteAcker struct {
	*rpcquic.RequestServerStream
	connectResponseSent bool
}

// AckConnection acks response back to the proxy.
func (s *streamReadWriteAcker) AckConnection(tracePropagation string) error {
	metadata := []pogs.Metadata{}
	// Only add tracing if provided by the edge request
	if tracePropagation != "" {
		metadata = append(metadata, pogs.Metadata{
			Key: tracing.CanonicalCloudflaredTracingHeader,
			Val: tracePropagation,
		})
	}
	s.connectResponseSent = true
	return s.WriteConnectResponseData(nil, metadata...)
}

// httpResponseAdapter translates responses written by the HTTP Proxy into ones that can be used in QUIC.
type httpResponseAdapter struct {
	*rpcquic.RequestServerStream
	headers             http.Header
	connectResponseSent bool
}

func newHTTPResponseAdapter(s *rpcquic.RequestServerStream) httpResponseAdapter {
	return httpResponseAdapter{RequestServerStream: s, headers: make(http.Header)}
}

func (hrw *httpResponseAdapter) AddTrailer(trailerName, trailerValue string) {
	// we do not support trailers over QUIC
}

func (hrw *httpResponseAdapter) WriteRespHeaders(status int, header http.Header) error {
	metadata := make([]pogs.Metadata, 0)
	metadata = append(metadata, pogs.Metadata{Key: "HttpStatus", Val: strconv.Itoa(status)})
	for k, vv := range header {
		for _, v := range vv {
			httpHeaderKey := fmt.Sprintf("%s:%s", HTTPHeaderKey, k)
			metadata = append(metadata, pogs.Metadata{Key: httpHeaderKey, Val: v})
		}
	}

	return hrw.WriteConnectResponseData(nil, metadata...)
}

func (hrw *httpResponseAdapter) Write(p []byte) (int, error) {
	// Make sure to send WriteHeader response if not called yet
	if !hrw.connectResponseSent {
		_ = hrw.WriteRespHeaders(http.StatusOK, hrw.headers)
	}
	return hrw.RequestServerStream.Write(p)
}

func (hrw *httpResponseAdapter) Header() http.Header {
	return hrw.headers
}

// This is a no-op Flush because this adapter is over a quic.Stream and we don't need Flush here.
func (hrw *httpResponseAdapter) Flush() {}

func (hrw *httpResponseAdapter) WriteHeader(status int) {
	_ = hrw.WriteRespHeaders(status, hrw.headers)
}

func (hrw *httpResponseAdapter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn := &localProxyConnection{hrw.ReadWriteCloser}
	readWriter := bufio.NewReadWriter(
		bufio.NewReader(hrw.ReadWriteCloser),
		bufio.NewWriter(hrw.ReadWriteCloser),
	)
	return conn, readWriter, nil
}

func (hrw *httpResponseAdapter) WriteErrorResponse(err error) {
	_ = hrw.WriteConnectResponseData(err, pogs.Metadata{Key: "HttpStatus", Val: strconv.Itoa(http.StatusBadGateway)})
}

func (hrw *httpResponseAdapter) WriteConnectResponseData(respErr error, metadata ...pogs.Metadata) error {
	hrw.connectResponseSent = true
	return hrw.RequestServerStream.WriteConnectResponseData(respErr, metadata...)
}

func buildHTTPRequest(
	ctx context.Context,
	connectRequest *pogs.ConnectRequest,
	body io.ReadCloser,
	connIndex uint8,
	log *zerolog.Logger,
) (*tracing.TracedHTTPRequest, error) {
	metadata := connectRequest.MetadataMap()
	dest := connectRequest.Dest
	method := metadata[HTTPMethodKey]
	host := metadata[HTTPHostKey]
	isWebsocket := connectRequest.Type == pogs.ConnectionTypeWebsocket

	req, err := http.NewRequestWithContext(ctx, method, dest, body)
	if err != nil {
		return nil, err
	}

	req.Host = host
	for _, metadata := range connectRequest.Metadata {
		if strings.Contains(metadata.Key, HTTPHeaderKey) {
			// metadata.Key is off the format httpHeaderKey:<HTTPHeader>
			httpHeaderKey := strings.Split(metadata.Key, ":")
			if len(httpHeaderKey) != 2 {
				return nil, fmt.Errorf("header Key: %s malformed", metadata.Key)
			}
			req.Header.Add(httpHeaderKey[1], metadata.Val)
		}
	}
	// Go's http.Client automatically sends chunked request body if this value is not set on the
	// *http.Request struct regardless of header:
	// https://go.googlesource.com/go/+/go1.8rc2/src/net/http/transfer.go#154.
	if err := setContentLength(req); err != nil {
		return nil, fmt.Errorf("Error setting content-length: %w", err)
	}

	// Go's client defaults to chunked encoding after a 200ms delay if the following cases are true:
	//   * the request body blocks
	//   * the content length is not set (or set to -1)
	//   * the method doesn't usually have a body (GET, HEAD, DELETE, ...)
	//   * there is no transfer-encoding=chunked already set.
	// So, if transfer cannot be chunked and content length is 0, we dont set a request body.
	if !isWebsocket && !isTransferEncodingChunked(req) && req.ContentLength == 0 {
		req.Body = http.NoBody
	}
	stripWebsocketUpgradeHeader(req)

	// Check for tracing on request
	tracedReq := tracing.NewTracedHTTPRequest(req, connIndex, log)
	return tracedReq, err
}

func setContentLength(req *http.Request) error {
	var err error
	if contentLengthStr := req.Header.Get("Content-Length"); contentLengthStr != "" {
		req.ContentLength, err = strconv.ParseInt(contentLengthStr, 10, 64)
	}
	return err
}

func isTransferEncodingChunked(req *http.Request) bool {
	transferEncodingVal := req.Header.Get("Transfer-Encoding")
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Transfer-Encoding suggests that this can be a comma
	// separated value as well.
	return strings.Contains(strings.ToLower(transferEncodingVal), "chunked")
}

// A helper struct that guarantees a call to close only affects read side, but not write side.
type nopCloserReadWriter struct {
	io.ReadWriteCloser

	// for use by Read only
	// we don't need a memory barrier here because there is an implicit assumption that
	// Read calls can't happen concurrently by different go-routines.
	sawEOF bool
	// should be updated and read using atomic primitives.
	// value is read in Read method and written in Close method, which could be done by different
	// go-routines.
	closed uint32
}

func (np *nopCloserReadWriter) Read(p []byte) (n int, err error) {
	if np.sawEOF {
		return 0, io.EOF
	}

	if atomic.LoadUint32(&np.closed) > 0 {
		return 0, fmt.Errorf("closed by handler")
	}

	n, err = np.ReadWriteCloser.Read(p)
	if err == io.EOF {
		np.sawEOF = true
	}

	return
}

func (np *nopCloserReadWriter) Close() error {
	atomic.StoreUint32(&np.closed, 1)

	return nil
}
