package connection

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lucas-clemente/quic-go"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/datagramsession"
	"github.com/cloudflare/cloudflared/ingress"
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
)

// QUICConnection represents the type that facilitates Proxying via QUIC streams.
type QUICConnection struct {
	session              quic.Connection
	logger               *zerolog.Logger
	orchestrator         Orchestrator
	sessionManager       datagramsession.Manager
	controlStreamHandler ControlStreamHandler
	connOptions          *tunnelpogs.ConnectionOptions
}

// NewQUICConnection returns a new instance of QUICConnection.
func NewQUICConnection(
	quicConfig *quic.Config,
	edgeAddr net.Addr,
	tlsConfig *tls.Config,
	orchestrator Orchestrator,
	connOptions *tunnelpogs.ConnectionOptions,
	controlStreamHandler ControlStreamHandler,
	logger *zerolog.Logger,
) (*QUICConnection, error) {
	session, err := quic.DialAddr(edgeAddr.String(), tlsConfig, quicConfig)
	if err != nil {
		return nil, &EdgeQuicDialError{Cause: err}
	}

	datagramMuxer, err := quicpogs.NewDatagramMuxer(session, logger)
	if err != nil {
		return nil, err
	}

	sessionManager := datagramsession.NewManager(datagramMuxer, logger)

	return &QUICConnection{
		session:              session,
		orchestrator:         orchestrator,
		logger:               logger,
		sessionManager:       sessionManager,
		controlStreamHandler: controlStreamHandler,
		connOptions:          connOptions,
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
	stream := quicpogs.NewSafeStreamCloser(quicStream)
	defer stream.Close()

	if err := q.handleStream(stream); err != nil {
		q.logger.Err(err).Msg("Failed to handle QUIC stream")
	}
}

func (q *QUICConnection) handleStream(stream io.ReadWriteCloser) error {
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
		return q.handleDataStream(reqServerStream)
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

func (q *QUICConnection) handleDataStream(stream *quicpogs.RequestServerStream) error {
	request, err := stream.ReadConnectRequestData()
	if err != nil {
		return err
	}

	if err := q.dispatchRequest(stream, err, request); err != nil {
		_ = stream.WriteConnectResponseData(err)
		q.logger.Err(err).Str("type", request.Type.String()).Str("dest", request.Dest).Msg("Request failed")
	}

	return nil
}

func (q *QUICConnection) dispatchRequest(stream *quicpogs.RequestServerStream, err error, request *quicpogs.ConnectRequest) error {
	originProxy, err := q.orchestrator.GetOriginProxy()
	if err != nil {
		return err
	}

	switch request.Type {
	case quicpogs.ConnectionTypeHTTP, quicpogs.ConnectionTypeWebsocket:
		tracedReq, err := buildHTTPRequest(request, stream)
		if err != nil {
			return err
		}
		w := newHTTPResponseAdapter(stream)
		return originProxy.ProxyHTTP(w, tracedReq, request.Type == quicpogs.ConnectionTypeWebsocket)

	case quicpogs.ConnectionTypeTCP:
		rwa := &streamReadWriteAcker{stream}
		metadata := request.MetadataMap()
		return originProxy.ProxyTCP(context.Background(), rwa, &TCPRequest{
			Dest:   request.Dest,
			FlowID: metadata[QUICMetadataFlowID],
		})
	}
	return nil
}

func (q *QUICConnection) handleRPCStream(rpcStream *quicpogs.RPCServerStream) error {
	return rpcStream.Serve(q, q, q.logger)
}

// RegisterUdpSession is the RPC method invoked by edge to register and run a session
func (q *QUICConnection) RegisterUdpSession(ctx context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16, closeAfterIdleHint time.Duration) error {
	// Each session is a series of datagram from an eyeball to a dstIP:dstPort.
	// (src port, dst IP, dst port) uniquely identifies a session, so it needs a dedicated connected socket.
	originProxy, err := ingress.DialUDP(dstIP, dstPort)
	if err != nil {
		q.logger.Err(err).Msgf("Failed to create udp proxy to %s:%d", dstIP, dstPort)
		return err
	}
	session, err := q.sessionManager.RegisterSession(ctx, sessionID, originProxy)
	if err != nil {
		q.logger.Err(err).Str("sessionID", sessionID.String()).Msgf("Failed to register udp session")
		return err
	}

	go q.serveUDPSession(session, closeAfterIdleHint)

	q.logger.Debug().Str("sessionID", sessionID.String()).Str("src", originProxy.LocalAddr().String()).Str("dst", fmt.Sprintf("%s:%d", dstIP, dstPort)).Msgf("Registered session")
	return nil
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
	q.logger.Debug().Err(err).Str("sessionID", session.ID.String()).Msg("Session terminated")
}

// closeUDPSession first unregisters the session from session manager, then it tries to unregister from edge
func (q *QUICConnection) closeUDPSession(ctx context.Context, sessionID uuid.UUID, message string) {
	q.sessionManager.UnregisterSession(ctx, sessionID, message, false)
	stream, err := q.session.OpenStream()
	if err != nil {
		// Log this at debug because this is not an error if session was closed due to lost connection
		// with edge
		q.logger.Debug().Err(err).Str("sessionID", sessionID.String()).
			Msgf("Failed to open quic stream to unregister udp session with edge")
		return
	}
	rpcClientStream, err := quicpogs.NewRPCClientStream(ctx, stream, q.logger)
	if err != nil {
		// Log this at debug because this is not an error if session was closed due to lost connection
		// with edge
		q.logger.Err(err).Str("sessionID", sessionID.String()).
			Msgf("Failed to open rpc stream to unregister udp session with edge")
		return
	}
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
}

// AckConnection acks response back to the proxy.
func (s *streamReadWriteAcker) AckConnection() error {
	return s.WriteConnectResponseData(nil)
}

// httpResponseAdapter translates responses written by the HTTP Proxy into ones that can be used in QUIC.
type httpResponseAdapter struct {
	*quicpogs.RequestServerStream
}

func newHTTPResponseAdapter(s *quicpogs.RequestServerStream) httpResponseAdapter {
	return httpResponseAdapter{s}
}

func (hrw httpResponseAdapter) WriteRespHeaders(status int, header http.Header) error {
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

func (hrw httpResponseAdapter) WriteErrorResponse(err error) {
	hrw.WriteConnectResponseData(err, quicpogs.Metadata{Key: "HttpStatus", Val: strconv.Itoa(http.StatusBadGateway)})
}

func buildHTTPRequest(connectRequest *quicpogs.ConnectRequest, body io.ReadCloser) (*tracing.TracedRequest, error) {
	metadata := connectRequest.MetadataMap()
	dest := connectRequest.Dest
	method := metadata[HTTPMethodKey]
	host := metadata[HTTPHostKey]
	isWebsocket := connectRequest.Type == quicpogs.ConnectionTypeWebsocket

	req, err := http.NewRequest(method, dest, body)
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
	tracedReq := tracing.NewTracedRequest(req)
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
