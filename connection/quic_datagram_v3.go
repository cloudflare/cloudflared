package connection

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/management"
	cfdquic "github.com/cloudflare/cloudflared/quic/v3"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

type datagramV3Connection struct {
	conn quic.Connection
	// datagramMuxer mux/demux datagrams from quic connection
	datagramMuxer cfdquic.DatagramConn
	logger        *zerolog.Logger
}

func NewDatagramV3Connection(ctx context.Context,
	conn quic.Connection,
	sessionManager cfdquic.SessionManager,
	icmpRouter ingress.ICMPRouter,
	index uint8,
	metrics cfdquic.Metrics,
	logger *zerolog.Logger,
) DatagramSessionHandler {
	log := logger.
		With().
		Int(management.EventTypeKey, int(management.UDP)).
		Uint8(LogFieldConnIndex, index).
		Logger()
	datagramMuxer := cfdquic.NewDatagramConn(conn, sessionManager, icmpRouter, index, metrics, &log)

	return &datagramV3Connection{
		conn,
		datagramMuxer,
		logger,
	}
}

func (d *datagramV3Connection) Serve(ctx context.Context) error {
	return d.datagramMuxer.Serve(ctx)
}

func (d *datagramV3Connection) RegisterUdpSession(ctx context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16, closeAfterIdleHint time.Duration, traceContext string) (*pogs.RegisterUdpSessionResponse, error) {
	return nil, fmt.Errorf("datagram v3 does not support RegisterUdpSession RPC")
}

func (d *datagramV3Connection) UnregisterUdpSession(ctx context.Context, sessionID uuid.UUID, message string) error {
	return fmt.Errorf("datagram v3 does not support UnregisterUdpSession RPC")
}
