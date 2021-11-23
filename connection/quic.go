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

	"github.com/google/uuid"
	"github.com/lucas-clemente/quic-go"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/datagramsession"
	quicpogs "github.com/cloudflare/cloudflared/quic"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const (
	// HTTPHeaderKey is used to get or set http headers in QUIC ALPN if the underlying proxy connection type is HTTP.
	HTTPHeaderKey = "HttpHeader"
	// HTTPMethodKey is used to get or set http method in QUIC ALPN if the underlying proxy connection type is HTTP.
	HTTPMethodKey = "HttpMethod"
	// HTTPHostKey is used to get or set http Method in QUIC ALPN if the underlying proxy connection type is HTTP.
	HTTPHostKey          = "HttpHost"
	MaxDatagramFrameSize = 1220
)

// QUICConnection represents the type that facilitates Proxying via QUIC streams.
type QUICConnection struct {
	session        quic.Session
	logger         *zerolog.Logger
	httpProxy      OriginProxy
	sessionManager datagramsession.Manager
	localIP        net.IP
}

// NewQUICConnection returns a new instance of QUICConnection.
func NewQUICConnection(
	ctx context.Context,
	quicConfig *quic.Config,
	edgeAddr net.Addr,
	tlsConfig *tls.Config,
	httpProxy OriginProxy,
	connOptions *tunnelpogs.ConnectionOptions,
	controlStreamHandler ControlStreamHandler,
	observer *Observer,
) (*QUICConnection, error) {
	session, err := quic.DialAddr(edgeAddr.String(), tlsConfig, quicConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to dial to edge: %w", err)
	}

	registrationStream, err := session.OpenStream()
	if err != nil {
		return nil, fmt.Errorf("failed to open a registration stream: %w", err)
	}

	err = controlStreamHandler.ServeControlStream(ctx, registrationStream, connOptions, false)
	if err != nil {
		// Not wrapping error here to be consistent with the http2 message.
		return nil, err
	}

	datagramMuxer, err := quicpogs.NewDatagramMuxer(session)
	if err != nil {
		return nil, err
	}

	sessionManager := datagramsession.NewManager(datagramMuxer, observer.log)

	localIP, err := getLocalIP()
	if err != nil {
		return nil, err
	}

	return &QUICConnection{
		session:        session,
		httpProxy:      httpProxy,
		logger:         observer.log,
		sessionManager: sessionManager,
		localIP:        localIP,
	}, nil
}

// Serve starts a QUIC session that begins accepting streams.
func (q *QUICConnection) Serve(ctx context.Context) error {
	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		return q.acceptStream(ctx)
	})
	errGroup.Go(func() error {
		return q.sessionManager.Serve(ctx)
	})
	return errGroup.Wait()
}

func (q *QUICConnection) acceptStream(ctx context.Context) error {
	for {
		stream, err := q.session.AcceptStream(ctx)
		if err != nil {
			// context.Canceled is usually a user ctrl+c. We don't want to log an error here as it's intentional.
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("failed to accept QUIC stream: %w", err)
		}
		go func() {
			defer stream.Close()
			if err = q.handleStream(stream); err != nil {
				q.logger.Err(err).Msg("Failed to handle QUIC stream")
			}
		}()
	}
}

// Close closes the session with no errors specified.
func (q *QUICConnection) Close() {
	q.session.CloseWithError(0, "")
}

func (q *QUICConnection) handleStream(stream quic.Stream) error {
	signature, err := quicpogs.DetermineProtocol(stream)
	if err != nil {
		return err
	}
	switch signature {
	case quicpogs.DataStreamProtocolSignature:
		reqServerStream, err := quicpogs.NewRequestServerStream(stream, signature)
		if err != nil {
			return nil
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
	connectRequest, err := stream.ReadConnectRequestData()
	if err != nil {
		return err
	}

	switch connectRequest.Type {
	case quicpogs.ConnectionTypeHTTP, quicpogs.ConnectionTypeWebsocket:
		req, err := buildHTTPRequest(connectRequest, stream)
		if err != nil {
			return err
		}

		w := newHTTPResponseAdapter(stream)
		return q.httpProxy.ProxyHTTP(w, req, connectRequest.Type == quicpogs.ConnectionTypeWebsocket)
	case quicpogs.ConnectionTypeTCP:
		rwa := &streamReadWriteAcker{stream}
		return q.httpProxy.ProxyTCP(context.Background(), rwa, &TCPRequest{Dest: connectRequest.Dest})
	}
	return nil
}

func (q *QUICConnection) handleRPCStream(rpcStream *quicpogs.RPCServerStream) error {
	return rpcStream.Serve(q, q.logger)
}

func (q *QUICConnection) RegisterUdpSession(ctx context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16) error {
	// Each session is a series of datagram from an eyeball to a dstIP:dstPort.
	// (src port, dst IP, dst port) uniquely identifies a session, so it needs a dedicated connected socket.
	originProxy, err := q.newUDPProxy(dstIP, dstPort)
	if err != nil {
		q.logger.Err(err).Msgf("Failed to create udp proxy to %s:%d", dstIP, dstPort)
		return err
	}
	session, err := q.sessionManager.RegisterSession(ctx, sessionID, originProxy)
	if err != nil {
		q.logger.Err(err).Msgf("Failed to register udp session %s", sessionID)
		return err
	}
	go func() {
		defer q.sessionManager.UnregisterSession(q.session.Context(), sessionID)
		if err := session.Serve(q.session.Context()); err != nil {
			q.logger.Debug().Err(err).Str("sessionID", sessionID.String()).Msg("session terminated")
		}
	}()
	q.logger.Debug().Msgf("Registered session %v, %v, %v", sessionID, dstIP, dstPort)
	return nil
}

// TODO: TUN-5422 Implement UnregisterUdpSession RPC

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

func buildHTTPRequest(connectRequest *quicpogs.ConnectRequest, body io.ReadCloser) (*http.Request, error) {
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
		req.Body = nil
	}
	stripWebsocketUpgradeHeader(req)
	return req, err
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

// TODO: TUN-5303: Define an UDPProxy in ingress package
func (q *QUICConnection) newUDPProxy(dstIP net.IP, dstPort uint16) (*net.UDPConn, error) {
	dstAddr := &net.UDPAddr{
		IP:   dstIP,
		Port: int(dstPort),
	}
	return net.DialUDP("udp", nil, dstAddr)
}

// TODO: TUN-5303: Find the local IP once in ingress package
// TODO: TUN-5421 allow user to specify which IP to bind to
func getLocalIP() (net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		// Find the IP that is not loop back
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if !ip.IsLoopback() {
			return ip, nil
		}
	}
	return nil, fmt.Errorf("cannot determine IP to bind to")
}
