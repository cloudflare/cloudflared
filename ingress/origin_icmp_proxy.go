package ingress

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/google/gopacket/layers"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/net/icmp"

	"github.com/cloudflare/cloudflared/packet"
)

// ICMPProxy sends ICMP messages and listens for their responses
type ICMPProxy interface {
	// Request sends an ICMP message
	Request(pk *packet.ICMP, responder packet.FlowResponder) error
	// ListenResponse listens for responses to the requests until context is done
	ListenResponse(ctx context.Context) error
}

// TODO: TUN-6654 Extend support to IPv6
type icmpProxy struct {
	srcFlowTracker *packet.FlowTracker
	conn           *icmp.PacketConn
	logger         *zerolog.Logger
	encoder        *packet.Encoder
}

// TODO: TUN-6586: Use echo ID as FlowID
type seqNumFlowID int

func (snf seqNumFlowID) ID() string {
	return strconv.FormatInt(int64(snf), 10)
}

func NewICMPProxy(network string, listenIP net.IP, logger *zerolog.Logger) (*icmpProxy, error) {
	conn, err := icmp.ListenPacket(network, listenIP.String())
	if err != nil {
		return nil, err
	}
	return &icmpProxy{
		srcFlowTracker: packet.NewFlowTracker(),
		conn:           conn,
		logger:         logger,
		encoder:        packet.NewEncoder(),
	}, nil
}

func (ip *icmpProxy) Request(pk *packet.ICMP, responder packet.FlowResponder) error {
	switch body := pk.Message.Body.(type) {
	case *icmp.Echo:
		return ip.sendICMPEchoRequest(pk, body, responder)
	default:
		return fmt.Errorf("sending ICMP %s is not implemented", pk.Type)
	}
}

func (ip *icmpProxy) ListenResponse(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		ip.conn.Close()
	}()
	buf := make([]byte, 1500)
	for {
		n, src, err := ip.conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		// TODO: TUN-6654 Check for IPv6
		msg, err := icmp.ParseMessage(int(layers.IPProtocolICMPv4), buf[:n])
		if err != nil {
			ip.logger.Error().Err(err).Str("src", src.String()).Msg("Failed to parse ICMP message")
			continue
		}
		switch body := msg.Body.(type) {
		case *icmp.Echo:
			if err := ip.handleEchoResponse(msg, body); err != nil {
				ip.logger.Error().Err(err).Str("src", src.String()).Msg("Failed to handle ICMP response")
				continue
			}
		default:
			ip.logger.Warn().
				Str("icmpType", fmt.Sprintf("%s", msg.Type)).
				Msgf("Responding to this type of ICMP is not implemented")
			continue
		}
	}
}

func (ip *icmpProxy) sendICMPEchoRequest(pk *packet.ICMP, echo *icmp.Echo, responder packet.FlowResponder) error {
	flow := packet.Flow{
		Src:       pk.Src,
		Dst:       pk.Dst,
		Responder: responder,
	}
	// TODO: TUN-6586 rewrite ICMP echo request identifier and use it to track flows
	flowID := seqNumFlowID(echo.Seq)
	// TODO: TUN-6588 clean up flows
	if replaced := ip.srcFlowTracker.Register(flowID, &flow, true); replaced {
		ip.logger.Info().Str("src", flow.Src.String()).Str("dst", flow.Dst.String()).Msg("Replaced flow")
	}
	var pseudoHeader []byte = nil
	serializedMsg, err := pk.Marshal(pseudoHeader)
	if err != nil {
		return errors.Wrap(err, "Failed to encode ICMP message")
	}
	// The address needs to be of type UDPAddr when conn is created without priviledge
	_, err = ip.conn.WriteTo(serializedMsg, &net.UDPAddr{
		IP: pk.Dst.AsSlice(),
	})
	return err
}

func (ip *icmpProxy) handleEchoResponse(msg *icmp.Message, echo *icmp.Echo) error {
	flow, ok := ip.srcFlowTracker.Get(seqNumFlowID(echo.Seq))
	if !ok {
		return fmt.Errorf("flow not found")
	}
	icmpPacket := packet.ICMP{
		IP: &packet.IP{
			Src:      flow.Dst,
			Dst:      flow.Src,
			Protocol: layers.IPProtocol(msg.Type.Protocol()),
		},
		Message: msg,
	}
	serializedPacket, err := ip.encoder.Encode(&icmpPacket)
	if err != nil {
		return errors.Wrap(err, "Failed to encode ICMP message")
	}
	if err := flow.Responder.SendPacket(serializedPacket); err != nil {
		return errors.Wrap(err, "Failed to send packet to the edge")
	}
	return nil
}
