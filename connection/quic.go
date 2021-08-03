package connection

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/lucas-clemente/quic-go"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	quicpogs "github.com/cloudflare/cloudflared/quic"
)

// QUICConnection represents the type that facilitates Proxying via QUIC streams.
type QUICConnection struct {
	session quic.Session
	logger  zerolog.Logger
}

// NewQUICConnection returns a new instance of QUICConnection.
func NewQUICConnection(
	ctx context.Context,
	quicConfig *quic.Config,
	edgeAddr net.Addr,
	tlsConfig *tls.Config,
	logger zerolog.Logger,
) (*QUICConnection, error) {
	session, err := quic.DialAddr(edgeAddr.String(), tlsConfig, quicConfig)
	if err != nil {
		return nil, errors.Wrap(err, "failed to dial to edge")
	}

	//TODO: RegisterConnectionRPC here.

	return &QUICConnection{
		session: session,
		logger:  logger,
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

// Close calls this to close the QuicConnection stream.
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
		// Temporary dummy code for the unit test.
		if err := quicpogs.WriteConnectResponseData(stream, nil, quicpogs.Metadata{Key: "HTTPStatus", Val: "200"}); err != nil {
			return err
		}

		stream.Write([]byte("OK"))
	case quicpogs.ConnectionTypeTCP:

	}
	return nil
}
