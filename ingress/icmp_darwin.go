//go:build darwin

package ingress

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/netip"
	"strconv"
	"sync"

	"github.com/google/gopacket/layers"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/net/icmp"

	"github.com/cloudflare/cloudflared/packet"
)

// TODO: TUN-6654 Extend support to IPv6
// On Darwin, a non-privileged ICMP socket can read messages from all echo IDs, so we use it for all sources.
type icmpProxy struct {
	// TODO: TUN-6588 clean up flows
	srcFlowTracker *packet.FlowTracker
	echoIDTracker  *echoIDTracker
	conn           *icmp.PacketConn
	logger         *zerolog.Logger
	encoder        *packet.Encoder
}

// echoIDTracker tracks which ID has been assigned. It first loops through assignment from lastAssignment to then end,
// then from the beginning to lastAssignment.
// ICMP echo are short lived. By the time an ID is revisited, it should have been released.
type echoIDTracker struct {
	lock sync.RWMutex
	// maps the source IP to an echo ID obtained from assignment
	srcIPMapping map[netip.Addr]uint16
	// assignment tracks if an ID is assigned using index as the ID
	// The size of the array is math.MaxUint16 because echo ID is 2 bytes
	assignment [math.MaxUint16]bool
	// nextAssignment is the next number to check for assigment
	nextAssignment uint16
}

func newEchoIDTracker() *echoIDTracker {
	return &echoIDTracker{
		srcIPMapping: make(map[netip.Addr]uint16),
	}
}

func (eit *echoIDTracker) get(srcIP netip.Addr) (uint16, bool) {
	eit.lock.RLock()
	defer eit.lock.RUnlock()
	id, ok := eit.srcIPMapping[srcIP]
	return id, ok
}

func (eit *echoIDTracker) assign(srcIP netip.Addr) (uint16, bool) {
	eit.lock.Lock()
	defer eit.lock.Unlock()

	if eit.nextAssignment == math.MaxUint16 {
		eit.nextAssignment = 0
	}

	for i, assigned := range eit.assignment[eit.nextAssignment:] {
		if !assigned {
			echoID := uint16(i) + eit.nextAssignment
			eit.set(srcIP, echoID)
			return echoID, true
		}
	}
	for i, assigned := range eit.assignment[0:eit.nextAssignment] {
		if !assigned {
			echoID := uint16(i)
			eit.set(srcIP, echoID)
			return echoID, true
		}
	}
	return 0, false
}

// Caller should hold the lock
func (eit *echoIDTracker) set(srcIP netip.Addr, echoID uint16) {
	eit.assignment[echoID] = true
	eit.srcIPMapping[srcIP] = echoID
	eit.nextAssignment = echoID + 1
}

func (eit *echoIDTracker) release(srcIP netip.Addr, id uint16) bool {
	eit.lock.Lock()
	defer eit.lock.Unlock()

	currentID, ok := eit.srcIPMapping[srcIP]
	if ok && id == currentID {
		delete(eit.srcIPMapping, srcIP)
		eit.assignment[id] = false
		return true
	}
	return false
}

type echoFlowID uint16

func (snf echoFlowID) Type() string {
	return "echoID"
}

func (snf echoFlowID) String() string {
	return strconv.FormatUint(uint64(snf), 10)
}

func newICMPProxy(listenIP net.IP, logger *zerolog.Logger) (ICMPProxy, error) {
	network := "udp6"
	if listenIP.To4() != nil {
		network = "udp4"
	}
	// Opens a non-privileged ICMP socket
	conn, err := icmp.ListenPacket(network, listenIP.String())
	if err != nil {
		return nil, err
	}
	return &icmpProxy{
		srcFlowTracker: packet.NewFlowTracker(),
		echoIDTracker:  newEchoIDTracker(),
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
				ip.logger.Error().Err(err).
					Str("src", src.String()).
					Str("flowID", echoFlowID(body.ID).String()).
					Msg("Failed to handle ICMP response")
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
	echoID, ok := ip.echoIDTracker.get(pk.Src)
	if !ok {
		echoID, ok = ip.echoIDTracker.assign(pk.Src)
		if !ok {
			return fmt.Errorf("failed to assign unique echo ID")
		}
		flowID := echoFlowID(echoID)
		flow := packet.Flow{
			Src:       pk.Src,
			Dst:       pk.Dst,
			Responder: responder,
		}
		if replaced := ip.srcFlowTracker.Register(flowID, &flow, true); replaced {
			ip.logger.Info().Str("src", flow.Src.String()).Str("dst", flow.Dst.String()).Msg("Replaced flow")
		}
	}

	echo.ID = int(echoID)
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
	flowID := echoFlowID(echo.ID)
	flow, ok := ip.srcFlowTracker.Get(flowID)
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
