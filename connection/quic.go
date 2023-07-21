package connection

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/datagramsession"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/management"
	"github.com/cloudflare/cloudflared/packet"
	quicpogs "github.com/cloudflare/cloudflared/quic"
	"github.com/cloudflare/cloudflared/tracing"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const (
	// HTTPHeaderKey is used to get or set http headers in QUIC ALPN if the underlying proxy connection type is HTTP.
	HTTPHeaderKey = "HttpHeader"
	// HTTPMethodKey is used to get or set http method in QUIC ALPN if the underlying proxy connection type is HTTP.
	HTTPMethodKey = "HttpMethod"
	// HTTPHostKey is used to get or set http Method in QUIC ALPN if the underlying proxy connection type is HTTP.
	HTTPHostKey = "HttpHost"

	QUICMetadataFlowID = "FlowID"
	// emperically this capacity has been working well
	demuxChanCapacity = 16
)

var (
	portForConnIndex = make(map[uint8]int, 0)
	portMapMutex     sync.Mutex
)

// QUICConnection represents the type that facilitates Proxying via QUIC streams.
type QUICConnection struct {
	session      quic.Connection
	logger       *zerolog.Logger
	orchestrator Orchestrator
	// sessionManager tracks active sessions. It receives datagrams from quic connection via datagramMuxer
	sessionManager datagramsession.Manager
	// datagramMuxer mux/demux datagrams from quic connection
	datagramMuxer        *quicpogs.DatagramMuxerV2
	packetRouter         *ingress.PacketRouter
	controlStreamHandler ControlStreamHandler
	connOptions          *tunnelpogs.ConnectionOptions
	connIndex            uint8

	udpUnregisterTimeout time.Duration
}

// NewQUICConnection returns a new instance of QUICConnection.
func NewQUICConnection(
	ctx context.Context,
	quicConfig *quic.Config,
	edgeAddr net.Addr,
	localAddr net.IP,
	connIndex uint8,
	tlsConfig *tls.Config,
	orchestrator Orchestrator,
	connOptions *tunnelpogs.ConnectionOptions,
	controlStreamHandler ControlStreamHandler,
	logger *zerolog.Logger,
	packetRouterConfig *ingress.GlobalRouterConfig,
	udpUnregisterTimeout time.Duration,
) (*QUICConnection, error) {
	udpConn, err := createUDPConnForConnIndex(connIndex, localAddr, logger)
	if err != nil {
		return nil, err
	}

	session, err := quic.Dial(ctx, udpConn, edgeAddr, tlsConfig, quicConfig)
	if err != nil {
		// close the udp server socket in case of error connecting to the edge
		udpConn.Close()
		return nil, &EdgeQuicDialError{Cause: err}
	}

	// wrap the session, so that the UDPConn is closed after session is closed.
	session = &wrapCloseableConnQuicConnection{
		session,
		udpConn,
	}

	sessionDemuxChan := make(chan *packet.Session, demuxChanCapacity)
	datagramMuxer := quicpogs.NewDatagramMuxerV2(session, logger, sessionDemuxChan)
	sessionManager := datagramsession.NewManager(logger, datagramMuxer.SendToSession, sessionDemuxChan)
	packetRouter := ingress.NewPacketRouter(packetRouterConfig, datagramMuxer, logger, orchestrator.WarpRoutingEnabled)

	return &QUICConnection{
		session:              session,
		orchestrator:         orchestrator,
		logger:               logger,
		sessionManager:       sessionManager,
		datagramMuxer:        datagramMuxer,
		packetRouter:         packetRouter,
		controlStreamHandler: controlStreamHandler,
		connOptions:          connOptions,
		connIndex:            connIndex,
		udpUnregisterTimeout: udpUnregisterTimeout,
	}, nil
}

// Serve starts a QUIC session that begins accepting streams.
func (q *QUICConnection) Serve(ctx context.Context) error {
	// origintunneld assumes the first stream is used for the control plane
	controlStream, err := q.session.OpenStream()
	if err != nil {
		return fmt.Errorf("failed to open a registration control stream: %w", err)
	}

	// If either goroutine returns nil error, we rely on this cancellation to make sure the other goroutine exits
	// as fast as possible as well. Nil error means we want to exit for good (caller code won't retry serving this
	// connection).
	// If either goroutine returns a non nil error, then the error group cancels the context, thus also canceling the
	// other goroutine as fast as possible.
	ctx, cancel := context.WithCancel(ctx)
	errGroup, ctx := errgroup.WithContext(ctx)

	// In the future, if cloudflared can autonomously push traffic to the edge, we have to make sure the control
	// stream is already fully registered before the other goroutines can proceed.
	errGroup.Go(func() error {
		defer cancel()
		return q.serveControlStream(ctx, controlStream)
	})
	errGroup.Go(func() error {
		defer cancel()
		return q.acceptStream(ctx)
	})
	errGroup.Go(func() error {
		defer cancel()
		return q.sessionManager.Serve(ctx)
	})
	errGroup.Go(func() error {
		defer cancel()
		return q.datagramMuxer.ServeReceive(ctx)
	})
	errGroup.Go(func() error {
		defer cancel()
		return q.packetRouter.Serve(ctx)
	})

	return errGroup.Wait()
}

func (q *QUICConnection) serveControlStream(ctx context.Context, controlStream quic.Stream) error {
	// This blocks until the control plane is done.
	err := q.controlStreamHandler.ServeControlStream(ctx, controlStream, q.connOptions, q.orchestrator)
	if err != nil {
		// Not wrapping error here to be consistent with the http2 message.
		return err
	}

	return nil
}

// Close closes the session with no errors specified.
func (q *QUICConnection) Close() {
	q.session.CloseWithError(0, "")
}

func (q *QUICConnection) acceptStream(ctx context.Context) error {
	defer q.Close()
	for {
		quicStream, err := q.session.AcceptStream(ctx)
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

func (q *QUICConnection) runStream(quicStream quic.Stream) {
	ctx := quicStream.Context()
	stream := quicpogs.NewSafeStreamCloser(quicStream)
	defer stream.Close()

	// we are going to fuse readers/writers from stream <- cloudflared -> origin, and we want to guarantee that
	// code executed in the code path of handleStream don't trigger an earlier close to the downstream write stream.
	// So, we wrap the stream with a no-op write closer and only this method can actually close write side of the stream.
	// A call to close will simulate a close to the read-side, which will fail subsequent reads.
	noCloseStream := &nopCloserReadWriter{ReadWriteCloser: stream}
	if err := q.handleStream(ctx, noCloseStream); err != nil {
		q.logger.Debug().Err(err).Msg("Failed to handle QUIC stream")

		// if we received an error at this level, then close write side of stream with an error, which will result in
		// RST_STREAM frame.
		quicStream.CancelWrite(0)
	}
}

func (q *QUICConnection) handleStream(ctx context.Context, stream io.ReadWriteCloser) error {
	signature, err := quicpogs.DetermineProtocol(stream)
	if err != nil {
		return err
	}
	switch signature {
	case quicpogs.DataStreamProtocolSignature:
		reqServerStream, err := quicpogs.NewRequestServerStream(stream, signature)
		if err != nil {
			return err
		}
		return q.handleDataStream(ctx, reqServerStream)
	case quicpogs.RPCStreamProtocolSignature:
		rpcStream, err := quicpogs.NewRPCServerStream(stream, signature)
		if err != nil {
			return err
		}
		return q.handleRPCStream(rpcStream)
	default:
		return fmt.Errorf("unknown protocol %v", signature)
	}
}

func (q *QUICConnection) handleDataStream(ctx context.Context, stream *quicpogs.RequestServerStream) error {
	request, err := stream.ReadConnectRequestData()
	if err != nil {
		return err
	}

	if err, connectResponseSent := q.dispatchRequest(ctx, stream, err, request); err != nil {
		q.logger.Err(err).Str("type", request.Type.String()).Str("dest", request.Dest).Msg("Request failed")

		// if the connectResponse was already sent and we had an error, we need to propagate it up, so that the stream is
		// closed with an RST_STREAM frame
		if connectResponseSent {
			return err
		}

		if writeRespErr := stream.WriteConnectResponseData(err); writeRespErr != nil {
			return writeRespErr
		}
	}

	return nil
}

// dispatchRequest will dispatch the request depending on the type and returns an error if it occurs.
// More importantly, it also tells if the during processing of the request the ConnectResponse metadata was sent downstream.
// This is important since it informs
func (q *QUICConnection) dispatchRequest(ctx context.Context, stream *quicpogs.RequestServerStream, err error, request *quicpogs.ConnectRequest) (error, bool) {
	originProxy, err := q.orchestrator.GetOriginProxy()
	if err != nil {
		return err, false
	}

	switch request.Type {
	case quicpogs.ConnectionTypeHTTP, quicpogs.ConnectionTypeWebsocket:
		tracedReq, err := buildHTTPRequest(ctx, request, stream, q.connIndex, q.logger)
		if err != nil {
			return err, false
		}
		w := newHTTPResponseAdapter(stream)
		return originProxy.ProxyHTTP(&w, tracedReq, request.Type == quicpogs.ConnectionTypeWebsocket), w.connectResponseSent

	case quicpogs.ConnectionTypeTCP:
		rwa := &streamReadWriteAcker{RequestServerStream: stream}
		metadata := request.MetadataMap()
		return originProxy.ProxyTCP(ctx, rwa, &TCPRequest{
			Dest:      request.Dest,
			FlowID:    metadata[QUICMetadataFlowID],
			CfTraceID: metadata[tracing.TracerContextName],
			ConnIndex: q.connIndex,
		}), rwa.connectResponseSent
	default:
		return errors.Errorf("unsupported error type: %s", request.Type), false
	}
}

func (q *QUICConnection) handleRPCStream(rpcStream *quicpogs.RPCServerStream) error {
	if err := rpcStream.Serve(q, q, q.logger); err != nil {
		q.logger.Err(err).Msg("failed handling RPC stream")
	}

	return nil
}

// RegisterUdpSession is the RPC method invoked by edge to register and run a session
func (q *QUICConnection) RegisterUdpSession(ctx context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16, closeAfterIdleHint time.Duration, traceContext string) (*tunnelpogs.RegisterUdpSessionResponse, error) {
	traceCtx := tracing.NewTracedContext(ctx, traceContext, q.logger)
	ctx, registerSpan := traceCtx.Tracer().Start(traceCtx, "register-session", trace.WithAttributes(
		attribute.String("session-id", sessionID.String()),
		attribute.String("dst", fmt.Sprintf("%s:%d", dstIP, dstPort)),
	))
	log := q.logger.With().Int(management.EventTypeKey, int(management.UDP)).Logger()
	// Each session is a series of datagram from an eyeball to a dstIP:dstPort.
	// (src port, dst IP, dst port) uniquely identifies a session, so it needs a dedicated connected socket.
	originProxy, err := ingress.DialUDP(dstIP, dstPort)
	if err != nil {
		log.Err(err).Msgf("Failed to create udp proxy to %s:%d", dstIP, dstPort)
		tracing.EndWithErrorStatus(registerSpan, err)
		return nil, err
	}
	registerSpan.SetAttributes(
		attribute.Bool("socket-bind-success", true),
		attribute.String("src", originProxy.LocalAddr().String()),
	)

	session, err := q.sessionManager.RegisterSession(ctx, sessionID, originProxy)
	if err != nil {
		log.Err(err).Str("sessionID", sessionID.String()).Msgf("Failed to register udp session")
		tracing.EndWithErrorStatus(registerSpan, err)
		return nil, err
	}

	go q.serveUDPSession(session, closeAfterIdleHint)

	log.Debug().
		Str("sessionID", sessionID.String()).
		Str("src", originProxy.LocalAddr().String()).
		Str("dst", fmt.Sprintf("%s:%d", dstIP, dstPort)).
		Msgf("Registered session")
	tracing.End(registerSpan)

	resp := tunnelpogs.RegisterUdpSessionResponse{
		Spans: traceCtx.GetProtoSpans(),
	}

	return &resp, nil
}

func (q *QUICConnection) serveUDPSession(session *datagramsession.Session, closeAfterIdleHint time.Duration) {
	ctx := q.session.Context()
	closedByRemote, err := session.Serve(ctx, closeAfterIdleHint)
	// If session is terminated by remote, then we know it has been unregistered from session manager and edge
	if !closedByRemote {
		if err != nil {
			q.closeUDPSession(ctx, session.ID, err.Error())
		} else {
			q.closeUDPSession(ctx, session.ID, "terminated without error")
		}
	}
	q.logger.Debug().Err(err).
		Int(management.EventTypeKey, int(management.UDP)).
		Str("sessionID", session.ID.String()).
		Msg("Session terminated")
}

// closeUDPSession first unregisters the session from session manager, then it tries to unregister from edge
func (q *QUICConnection) closeUDPSession(ctx context.Context, sessionID uuid.UUID, message string) {
	q.sessionManager.UnregisterSession(ctx, sessionID, message, false)
	quicStream, err := q.session.OpenStream()
	if err != nil {
		// Log this at debug because this is not an error if session was closed due to lost connection
		// with edge
		q.logger.Debug().Err(err).
			Int(management.EventTypeKey, int(management.UDP)).
			Str("sessionID", sessionID.String()).
			Msgf("Failed to open quic stream to unregister udp session with edge")
		return
	}

	stream := quicpogs.NewSafeStreamCloser(quicStream)
	defer stream.Close()
	rpcClientStream, err := quicpogs.NewRPCClientStream(ctx, stream, q.udpUnregisterTimeout, q.logger)
	if err != nil {
		// Log this at debug because this is not an error if session was closed due to lost connection
		// with edge
		q.logger.Err(err).Str("sessionID", sessionID.String()).
			Msgf("Failed to open rpc stream to unregister udp session with edge")
		return
	}
	defer rpcClientStream.Close()

	if err := rpcClientStream.UnregisterUdpSession(ctx, sessionID, message); err != nil {
		q.logger.Err(err).Str("sessionID", sessionID.String()).
			Msgf("Failed to unregister udp session with edge")
	}
}

// UnregisterUdpSession is the RPC method invoked by edge to unregister and terminate a sesssion
func (q *QUICConnection) UnregisterUdpSession(ctx context.Context, sessionID uuid.UUID, message string) error {
	return q.sessionManager.UnregisterSession(ctx, sessionID, message, true)
}

// UpdateConfiguration is the RPC method invoked by edge when there is a new configuration
func (q *QUICConnection) UpdateConfiguration(ctx context.Context, version int32, config []byte) *tunnelpogs.UpdateConfigurationResponse {
	return q.orchestrator.UpdateConfig(version, config)
}

// streamReadWriteAcker is a light wrapper over QUIC streams with a callback to send response back to
// the client.
type streamReadWriteAcker struct {
	*quicpogs.RequestServerStream
	connectResponseSent bool
}

// AckConnection acks response back to the proxy.
func (s *streamReadWriteAcker) AckConnection(tracePropagation string) error {
	metadata := []quicpogs.Metadata{}
	// Only add tracing if provided by origintunneld
	if tracePropagation != "" {
		metadata = append(metadata, quicpogs.Metadata{
			Key: tracing.CanonicalCloudflaredTracingHeader,
			Val: tracePropagation,
		})
	}
	s.connectResponseSent = true
	return s.WriteConnectResponseData(nil, metadata...)
}

// httpResponseAdapter translates responses written by the HTTP Proxy into ones that can be used in QUIC.
type httpResponseAdapter struct {
	*quicpogs.RequestServerStream
	headers             http.Header
	connectResponseSent bool
}

func newHTTPResponseAdapter(s *quicpogs.RequestServerStream) httpResponseAdapter {
	return httpResponseAdapter{RequestServerStream: s, headers: make(http.Header)}
}

func (hrw *httpResponseAdapter) AddTrailer(trailerName, trailerValue string) {
	// we do not support trailers over QUIC
}

func (hrw *httpResponseAdapter) WriteRespHeaders(status int, header http.Header) error {
	metadata := make([]quicpogs.Metadata, 0)
	metadata = append(metadata, quicpogs.Metadata{Key: "HttpStatus", Val: strconv.Itoa(status)})
	for k, vv := range header {
		for _, v := range vv {
			httpHeaderKey := fmt.Sprintf("%s:%s", HTTPHeaderKey, k)
			metadata = append(metadata, quicpogs.Metadata{Key: httpHeaderKey, Val: v})
		}
	}

	return hrw.WriteConnectResponseData(nil, metadata...)
}

func (hrw *httpResponseAdapter) Write(p []byte) (int, error) {
	// Make sure to send WriteHeader response if not called yet
	if !hrw.connectResponseSent {
		hrw.WriteRespHeaders(http.StatusOK, hrw.headers)
	}
	return hrw.RequestServerStream.Write(p)
}

func (hrw *httpResponseAdapter) Header() http.Header {
	return hrw.headers
}

// This is a no-op Flush because this adapter is over a quic.Stream and we don't need Flush here.
func (hrw *httpResponseAdapter) Flush() {}

func (hrw *httpResponseAdapter) WriteHeader(status int) {
	hrw.WriteRespHeaders(status, hrw.headers)
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
	hrw.WriteConnectResponseData(err, quicpogs.Metadata{Key: "HttpStatus", Val: strconv.Itoa(http.StatusBadGateway)})
}

func (hrw *httpResponseAdapter) WriteConnectResponseData(respErr error, metadata ...quicpogs.Metadata) error {
	hrw.connectResponseSent = true
	return hrw.RequestServerStream.WriteConnectResponseData(respErr, metadata...)
}

func buildHTTPRequest(
	ctx context.Context,
	connectRequest *quicpogs.ConnectRequest,
	body io.ReadCloser,
	connIndex uint8,
	log *zerolog.Logger,
) (*tracing.TracedHTTPRequest, error) {
	metadata := connectRequest.MetadataMap()
	dest := connectRequest.Dest
	method := metadata[HTTPMethodKey]
	host := metadata[HTTPHostKey]
	isWebsocket := connectRequest.Type == quicpogs.ConnectionTypeWebsocket

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

// muxerWrapper wraps DatagramMuxerV2 to satisfy the packet.FunnelUniPipe interface
type muxerWrapper struct {
	muxer *quicpogs.DatagramMuxerV2
}

func (rp *muxerWrapper) SendPacket(dst netip.Addr, pk packet.RawPacket) error {
	return rp.muxer.SendPacket(quicpogs.RawPacket(pk))
}

func (rp *muxerWrapper) ReceivePacket(ctx context.Context) (packet.RawPacket, error) {
	pk, err := rp.muxer.ReceivePacket(ctx)
	if err != nil {
		return packet.RawPacket{}, err
	}
	rawPacket, ok := pk.(quicpogs.RawPacket)
	if ok {
		return packet.RawPacket(rawPacket), nil
	}
	return packet.RawPacket{}, fmt.Errorf("unexpected packet type %+v", pk)
}

func (rp *muxerWrapper) Close() error {
	return nil
}

func createUDPConnForConnIndex(connIndex uint8, localIP net.IP, logger *zerolog.Logger) (*net.UDPConn, error) {
	portMapMutex.Lock()
	defer portMapMutex.Unlock()

	if localIP == nil {
		localIP = net.IPv4zero
	}

	// if port was not set yet, it will be zero, so bind will randomly allocate one.
	if port, ok := portForConnIndex[connIndex]; ok {
		udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: localIP, Port: port})
		// if there wasn't an error, or if port was 0 (independently of error or not, just return)
		if err == nil {
			return udpConn, nil
		} else {
			logger.Debug().Err(err).Msgf("Unable to reuse port %d for connIndex %d. Falling back to random allocation.", port, connIndex)
		}
	}

	// if we reached here, then there was an error or port as not been allocated it.
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: localIP, Port: 0})
	if err == nil {
		udpAddr, ok := (udpConn.LocalAddr()).(*net.UDPAddr)
		if !ok {
			return nil, fmt.Errorf("unable to cast to udpConn")
		}
		portForConnIndex[connIndex] = udpAddr.Port
	} else {
		delete(portForConnIndex, connIndex)
	}

	return udpConn, err
}

type wrapCloseableConnQuicConnection struct {
	quic.Connection
	udpConn *net.UDPConn
}

func (w *wrapCloseableConnQuicConnection) CloseWithError(errorCode quic.ApplicationErrorCode, reason string) error {
	err := w.Connection.CloseWithError(errorCode, reason)
	w.udpConn.Close()

	return err
}
