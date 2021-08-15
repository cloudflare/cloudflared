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

	"github.com/lucas-clemente/quic-go"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	quicpogs "github.com/cloudflare/cloudflared/quic"
)

const (
	// HTTPHeaderKey is used to get or set http headers in QUIC ALPN if the underlying proxy connection type is HTTP.
	HTTPHeaderKey = "HttpHeader"
	// HTTPMethodKey is used to get or set http method in QUIC ALPN if the underlying proxy connection type is HTTP.
	HTTPMethodKey = "HttpMethod"
	// HTTPHostKey is used to get or set http Method in QUIC ALPN if the underlying proxy connection type is HTTP.
	HTTPHostKey = "HttpHost"
)

// QUICConnection represents the type that facilitates Proxying via QUIC streams.
type QUICConnection struct {
	session   quic.Session
	logger    zerolog.Logger
	httpProxy OriginProxy
}

// NewQUICConnection returns a new instance of QUICConnection.
func NewQUICConnection(
	ctx context.Context,
	quicConfig *quic.Config,
	edgeAddr net.Addr,
	tlsConfig *tls.Config,
	httpProxy OriginProxy,
	logger zerolog.Logger,
) (*QUICConnection, error) {
	session, err := quic.DialAddr(edgeAddr.String(), tlsConfig, quicConfig)
	if err != nil {
		return nil, errors.Wrap(err, "failed to dial to edge")
	}

	//TODO: RegisterConnectionRPC here.

	return &QUICConnection{
		session:   session,
		httpProxy: httpProxy,
		logger:    logger,
	}, nil
}

// Serve starts a QUIC session that begins accepting streams.
func (q *QUICConnection) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for {
		stream, err := q.session.AcceptStream(ctx)
		if err != nil {
			return errors.Wrap(err, "failed to accept QUIC stream")
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
	connectRequest, err := quicpogs.ReadConnectRequestData(stream)
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
		return errors.New("not implemented")
	}
	return nil
}

// httpResponseAdapter translates responses written by the HTTP Proxy into ones that can be used in QUIC.
type httpResponseAdapter struct {
	io.Writer
}

func newHTTPResponseAdapter(w io.Writer) httpResponseAdapter {
	return httpResponseAdapter{w}
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
	return quicpogs.WriteConnectResponseData(hrw, nil, metadata...)
}

func (hrw httpResponseAdapter) WriteErrorResponse(err error) {
	quicpogs.WriteConnectResponseData(hrw, err, quicpogs.Metadata{Key: "HttpStatus", Val: strconv.Itoa(http.StatusBadGateway)})
}

func buildHTTPRequest(connectRequest *quicpogs.ConnectRequest, body io.Reader) (*http.Request, error) {
	metadata := connectRequest.MetadataMap()
	dest := connectRequest.Dest
	method := metadata[HTTPMethodKey]
	host := metadata[HTTPHostKey]

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
				return nil, fmt.Errorf("Header Key: %s malformed", metadata.Key)
			}
			req.Header.Add(httpHeaderKey[1], metadata.Val)
		}
	}
	stripWebsocketUpgradeHeader(req)
	return req, err
}
