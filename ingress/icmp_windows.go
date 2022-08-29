//go:build windows

package ingress

/*
#include <iphlpapi.h>
#include <icmpapi.h>
*/
import "C"
import (
	"context"
	"encoding/binary"
	"fmt"
	"net/netip"
	"runtime/debug"
	"sync"
	"syscall"
	"unsafe"

	"github.com/google/gopacket/layers"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/cloudflare/cloudflared/packet"
)

const (
	icmpEchoReplyCode = 0
)

var (
	Iphlpapi            = syscall.NewLazyDLL("Iphlpapi.dll")
	IcmpCreateFile_proc = Iphlpapi.NewProc("IcmpCreateFile")
	IcmpSendEcho_proc   = Iphlpapi.NewProc("IcmpSendEcho")
	echoReplySize       = unsafe.Sizeof(echoReply{})
	endian              = binary.LittleEndian
)

// IP_STATUS code, see https://docs.microsoft.com/en-us/windows/win32/api/ipexport/ns-ipexport-icmp_echo_reply32#members
// for possible values
type ipStatus uint32

const (
	success     ipStatus = 0
	bufTooSmall          = iota + 11000
	destNetUnreachable
	destHostUnreachable
	destProtocolUnreachable
	destPortUnreachable
	noResources
	badOption
	hwError
	packetTooBig
	reqTimedOut
	badReq
	badRoute
	ttlExpiredTransit
	ttlExpiredReassembly
	paramProblem
	sourceQuench
	optionTooBig
	badDestination
	// Can be returned for malformed ICMP packets
	generalFailure = 11050
)

func (is ipStatus) String() string {
	switch is {
	case success:
		return "Success"
	case bufTooSmall:
		return "The reply buffer too small"
	case destNetUnreachable:
		return "The destination network was unreachable"
	case destHostUnreachable:
		return "The destination host was unreachable"
	case destProtocolUnreachable:
		return "The destination protocol was unreachable"
	case destPortUnreachable:
		return "The destination port was unreachable"
	case noResources:
		return "Insufficient IP resources were available"
	case badOption:
		return "A bad IP option was specified"
	case hwError:
		return "A hardware error occurred"
	case packetTooBig:
		return "The packet was too big"
	case reqTimedOut:
		return "The request timed out"
	case badReq:
		return "Bad request"
	case badRoute:
		return "Bad route"
	case ttlExpiredTransit:
		return "The TTL expired in transit"
	case ttlExpiredReassembly:
		return "The TTL expired during fragment reassembly"
	case paramProblem:
		return "A parameter problem"
	case sourceQuench:
		return "Datagrams are arriving too fast to be processed and datagrams may have been discarded"
	case optionTooBig:
		return "The IP option was too big"
	case badDestination:
		return "Bad destination"
	case generalFailure:
		return "The ICMP packet might be malformed"
	default:
		return fmt.Sprintf("Unknown ip status %d", is)
	}
}

// https://docs.microsoft.com/en-us/windows/win32/api/ipexport/ns-ipexport-ip_option_information
type ipOption struct {
	TTL         uint8
	Tos         uint8
	Flags       uint8
	OptionsSize uint8
	OptionsData uintptr
}

// https://docs.microsoft.com/en-us/windows/win32/api/ipexport/ns-ipexport-icmp_echo_reply
type echoReply struct {
	Address       uint32
	Status        ipStatus
	RoundTripTime uint32
	DataSize      uint16
	Reserved      uint16
	// The pointer size defers between 32-bit and 64-bit platforms
	DataPointer *byte
	Options     ipOption
}

type icmpProxy struct {
	// An open handle that can send ICMP requests https://docs.microsoft.com/en-us/windows/win32/api/icmpapi/nf-icmpapi-icmpcreatefile
	handle uintptr
	logger *zerolog.Logger
	// A pool of reusable *packet.Encoder
	encoderPool sync.Pool
}

func newICMPProxy(listenIP netip.Addr, logger *zerolog.Logger) (ICMPProxy, error) {
	handle, _, err := IcmpCreateFile_proc.Call()
	// Windows procedure calls always return non-nil error constructed from the result of GetLastError.
	// Caller need to inspect the primary returned value
	if syscall.Handle(handle) == syscall.InvalidHandle {
		return nil, errors.Wrap(err, "invalid ICMP handle")
	}
	return &icmpProxy{
		handle: handle,
		logger: logger,
		encoderPool: sync.Pool{
			New: func() any {
				return packet.NewEncoder()
			},
		},
	}, nil
}

func (ip *icmpProxy) Serve(ctx context.Context) error {
	<-ctx.Done()
	syscall.CloseHandle(syscall.Handle(ip.handle))
	return ctx.Err()
}

func (ip *icmpProxy) Request(pk *packet.ICMP, responder packet.FlowResponder) error {
	if pk == nil {
		return errPacketNil
	}
	defer func() {
		if r := recover(); r != nil {
			ip.logger.Error().Interface("error", r).Msgf("Recover panic from sending icmp request/response, error %s", debug.Stack())
		}
	}()
	echo, err := getICMPEcho(pk)
	if err != nil {
		return err
	}

	resp, err := ip.icmpSendEcho(pk.Dst, echo)
	if err != nil {
		return errors.Wrap(err, "failed to send/receive ICMP echo")
	}

	err = ip.handleEchoResponse(pk, echo, resp, responder)
	if err != nil {
		return errors.Wrap(err, "failed to handle ICMP echo reply")
	}
	return nil
}

func (ip *icmpProxy) handleEchoResponse(request *packet.ICMP, echoReq *icmp.Echo, resp *echoResp, responder packet.FlowResponder) error {
	var replyType icmp.Type
	if request.Dst.Is4() {
		replyType = ipv4.ICMPTypeEchoReply
	} else {
		replyType = ipv6.ICMPTypeEchoReply
	}

	pk := packet.ICMP{
		IP: &packet.IP{
			Src:      request.Dst,
			Dst:      request.Src,
			Protocol: layers.IPProtocol(request.Type.Protocol()),
		},
		Message: &icmp.Message{
			Type: replyType,
			Code: icmpEchoReplyCode,
			Body: &icmp.Echo{
				ID:   echoReq.ID,
				Seq:  echoReq.Seq,
				Data: resp.data,
			},
		},
	}

	serializedPacket, err := ip.encodeICMPReply(&pk)
	if err != nil {
		return err
	}
	return responder.SendPacket(serializedPacket)
}

func (ip *icmpProxy) encodeICMPReply(pk *packet.ICMP) (packet.RawPacket, error) {
	cachedEncoder := ip.encoderPool.Get()
	defer ip.encoderPool.Put(cachedEncoder)
	encoder, ok := cachedEncoder.(*packet.Encoder)
	if !ok {
		return packet.RawPacket{}, fmt.Errorf("encoderPool returned %T, expect *packet.Encoder", cachedEncoder)
	}
	return encoder.Encode(pk)
}

/*
	Wrapper to call https://docs.microsoft.com/en-us/windows/win32/api/icmpapi/nf-icmpapi-icmpsendecho
	Parameters:
	- IcmpHandle:
	- DestinationAddress: IPv4 in the form of https://docs.microsoft.com/en-us/windows/win32/api/inaddr/ns-inaddr-in_addr#syntax
	- RequestData: A pointer to echo data
	- RequestSize: Number of bytes in buffer pointed by echo data
	- RequestOptions: IP header options
	- ReplyBuffer: A pointer to the buffer for echoReply, options and data
	- ReplySize: Number of bytes allocated for ReplyBuffer
	- Timeout: Timeout in milliseconds to wait for a reply
	Returns:
	- the number of replies in uint32 https://docs.microsoft.com/en-us/windows/win32/api/icmpapi/nf-icmpapi-icmpsendecho#return-value
	To retain the reference allocated objects, conversion from pointer to uintptr must happen as arguments to the
	syscall function
*/
func (ip *icmpProxy) icmpSendEcho(dst netip.Addr, echo *icmp.Echo) (*echoResp, error) {
	dataSize := len(echo.Data)
	replySize := echoReplySize + uintptr(dataSize)
	replyBuf := make([]byte, replySize)
	noIPHeaderOption := uintptr(0)
	inAddr, err := inAddrV4(dst)
	if err != nil {
		return nil, err
	}
	replyCount, _, err := IcmpSendEcho_proc.Call(ip.handle, uintptr(inAddr), uintptr(unsafe.Pointer(&echo.Data[0])),
		uintptr(dataSize), noIPHeaderOption, uintptr(unsafe.Pointer(&replyBuf[0])),
		replySize, icmpTimeoutMs)
	if replyCount == 0 {
		// status is returned in 5th to 8th byte of reply buffer
		if status, err := unmarshalIPStatus(replyBuf[4:8]); err == nil {
			return nil, fmt.Errorf("received ip status: %s", status)
		}
		return nil, errors.Wrap(err, "did not receive ICMP echo reply")
	} else if replyCount > 1 {
		ip.logger.Warn().Msgf("Received %d ICMP echo replies, only sending 1 back", replyCount)
	}
	return newEchoResp(replyBuf)
}

type echoResp struct {
	reply *echoReply
	data  []byte
}

func newEchoResp(replyBuf []byte) (*echoResp, error) {
	if len(replyBuf) == 0 {
		return nil, fmt.Errorf("reply buffer is empty")
	}
	// This is pattern 1 of https://pkg.go.dev/unsafe@master#Pointer, conversion of *replyBuf to *echoReply
	// replyBuf size is larger than echoReply
	reply := *(*echoReply)(unsafe.Pointer(&replyBuf[0]))
	if reply.Status != success {
		return nil, fmt.Errorf("status %d", reply.Status)
	}
	dataBufStart := len(replyBuf) - int(reply.DataSize)
	if dataBufStart < int(echoReplySize) {
		return nil, fmt.Errorf("reply buffer size %d is too small to hold data of size %d", len(replyBuf), int(reply.DataSize))
	}
	return &echoResp{
		reply: &reply,
		data:  replyBuf[dataBufStart:],
	}, nil
}

// Third definition of https://docs.microsoft.com/en-us/windows/win32/api/inaddr/ns-inaddr-in_addr#syntax is address in uint32
func inAddrV4(ip netip.Addr) (uint32, error) {
	if !ip.Is4() {
		return 0, fmt.Errorf("%s is not IPv4", ip)
	}
	v4 := ip.As4()
	return endian.Uint32(v4[:]), nil
}

func unmarshalIPStatus(replyBuf []byte) (ipStatus, error) {
	if len(replyBuf) != 4 {
		return 0, fmt.Errorf("ipStatus needs to be 4 bytes, got %d", len(replyBuf))
	}
	return ipStatus(endian.Uint32(replyBuf)), nil
}
