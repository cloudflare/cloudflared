//go:build windows && cgo

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
	"time"
	"unsafe"

	"github.com/google/gopacket/layers"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/cloudflare/cloudflared/packet"
	"github.com/cloudflare/cloudflared/tracing"
)

const (
	// Value defined in https://docs.microsoft.com/en-us/windows/win32/api/winsock2/nf-winsock2-wsasocketw
	AF_INET6          = 23
	icmpEchoReplyCode = 0
	nullParameter     = uintptr(0)
)

var (
	Iphlpapi             = syscall.NewLazyDLL("Iphlpapi.dll")
	IcmpCreateFile_proc  = Iphlpapi.NewProc("IcmpCreateFile")
	Icmp6CreateFile_proc = Iphlpapi.NewProc("Icmp6CreateFile")
	IcmpSendEcho_proc    = Iphlpapi.NewProc("IcmpSendEcho")
	Icmp6SendEcho_proc   = Iphlpapi.NewProc("Icmp6SendEcho2")
	echoReplySize        = unsafe.Sizeof(echoReply{})
	echoV6ReplySize      = unsafe.Sizeof(echoV6Reply{})
	icmpv6ErrMessageSize = 8
	ioStatusBlockSize    = unsafe.Sizeof(ioStatusBlock{})
	endian               = binary.LittleEndian
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

// Additional IP_STATUS codes for ICMPv6  https://docs.microsoft.com/en-us/windows/win32/api/ipexport/ns-ipexport-icmpv6_echo_reply_lh#members
const (
	ipv6DestUnreachable ipStatus = iota + 11040
	ipv6TimeExceeded
	ipv6BadHeader
	ipv6UnrecognizedNextHeader
	ipv6ICMPError
	ipv6DestScopeMismatch
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
	case ipv6DestUnreachable:
		return "IPv6 destination unreachable"
	case ipv6TimeExceeded:
		return "IPv6 time exceeded"
	case ipv6BadHeader:
		return "IPv6 bad IP header"
	case ipv6UnrecognizedNextHeader:
		return "IPv6 unrecognized next header"
	case ipv6ICMPError:
		return "IPv6 ICMP error"
	case ipv6DestScopeMismatch:
		return "IPv6 destination scope ID mismatch"
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

type echoV6Reply struct {
	Address       ipv6AddrEx
	Status        ipStatus
	RoundTripTime uint32
}

// https://docs.microsoft.com/en-us/windows/win32/api/ipexport/ns-ipexport-ipv6_address_ex
// All the fields are in network byte order. The memory alignment is 4 bytes
type ipv6AddrEx struct {
	port uint16
	// flowInfo is uint32. Because of field alignment, when we cast reply buffer to ipv6AddrEx, it starts at the 5th byte
	// But looking at the raw bytes, flowInfo starts at the 3rd byte. We device flowInfo into 2 uint16 so it's aligned
	flowInfoUpper uint16
	flowInfoLower uint16
	addr          [8]uint16
	scopeID       uint32
}

// https://docs.microsoft.com/en-us/windows/win32/winsock/sockaddr-2
type sockAddrIn6 struct {
	family int16
	// Can't embed ipv6AddrEx, that changes the memory alignment
	port     uint16
	flowInfo uint32
	addr     [16]byte
	scopeID  uint32
}

func newSockAddrIn6(addr netip.Addr) (*sockAddrIn6, error) {
	if !addr.Is6() {
		return nil, fmt.Errorf("%s is not IPv6", addr)
	}
	return &sockAddrIn6{
		family: AF_INET6,
		port:   10,
		addr:   addr.As16(),
	}, nil
}

// https://docs.microsoft.com/en-us/windows-hardware/drivers/ddi/wdm/ns-wdm-_io_status_block#syntax
type ioStatusBlock struct {
	// The first field is an union of NTSTATUS and PVOID. NTSTATUS is int32 while PVOID depends on the platform.
	// We model it as uintptr whose size depends on if the platform is 32-bit or 64-bit
	// https://docs.microsoft.com/en-us/openspecs/windows_protocols/ms-erref/596a1078-e883-4972-9bbc-49e60bebca55
	statusOrPointer uintptr
	information     uintptr
}

type icmpProxy struct {
	// An open handle that can send ICMP requests https://docs.microsoft.com/en-us/windows/win32/api/icmpapi/nf-icmpapi-icmpcreatefile
	handle uintptr
	// This is a ICMPv6 if srcSocketAddr is not nil
	srcSocketAddr *sockAddrIn6
	logger        *zerolog.Logger
	// A pool of reusable *packet.Encoder
	encoderPool sync.Pool
}

func newICMPProxy(listenIP netip.Addr, zone string, logger *zerolog.Logger, idleTimeout time.Duration) (*icmpProxy, error) {
	var (
		srcSocketAddr *sockAddrIn6
		handle        uintptr
		err           error
	)
	if listenIP.Is4() {
		handle, _, err = IcmpCreateFile_proc.Call()
	} else {
		srcSocketAddr, err = newSockAddrIn6(listenIP)
		if err != nil {
			return nil, err
		}
		handle, _, err = Icmp6CreateFile_proc.Call()
	}
	// Windows procedure calls always return non-nil error constructed from the result of GetLastError.
	// Caller need to inspect the primary returned value
	if syscall.Handle(handle) == syscall.InvalidHandle {
		return nil, errors.Wrap(err, "invalid ICMP handle")
	}
	return &icmpProxy{
		handle:        handle,
		srcSocketAddr: srcSocketAddr,
		logger:        logger,
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

// Request sends an ICMP echo request and wait for a reply or timeout.
// The async version of Win32 APIs take a callback whose memory is not garbage collected, so we use the synchronous version.
// It's possible that a slow request will block other requests, so we set the timeout to only 1s.
func (ip *icmpProxy) Request(ctx context.Context, pk *packet.ICMP, responder *packetResponder) error {
	defer func() {
		if r := recover(); r != nil {
			ip.logger.Error().Interface("error", r).Msgf("Recover panic from sending icmp request/response, error %s", debug.Stack())
		}
	}()

	_, requestSpan := responder.requestSpan(ctx, pk)
	defer responder.exportSpan()

	echo, err := getICMPEcho(pk.Message)
	if err != nil {
		return err
	}
	observeICMPRequest(ip.logger, requestSpan, pk.Src.String(), pk.Dst.String(), echo.ID, echo.Seq)

	resp, err := ip.icmpEchoRoundtrip(pk.Dst, echo)
	if err != nil {
		ip.logger.Err(err).Msg("ICMP echo roundtrip failed")
		tracing.EndWithErrorStatus(requestSpan, err)
		return err
	}
	tracing.End(requestSpan)
	responder.exportSpan()

	_, replySpan := responder.replySpan(ctx, ip.logger)
	err = ip.handleEchoReply(pk, echo, resp, responder)
	if err != nil {
		ip.logger.Err(err).Msg("Failed to send ICMP reply")
		tracing.EndWithErrorStatus(replySpan, err)
		return errors.Wrap(err, "failed to handle ICMP echo reply")
	}
	observeICMPReply(ip.logger, replySpan, pk.Dst.String(), echo.ID, echo.Seq)
	replySpan.SetAttributes(
		attribute.Int64("rtt", int64(resp.rtt())),
		attribute.String("status", resp.status().String()),
	)
	tracing.End(replySpan)
	return nil
}

func (ip *icmpProxy) handleEchoReply(request *packet.ICMP, echoReq *icmp.Echo, resp echoResp, responder *packetResponder) error {
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
			TTL:      packet.DefaultTTL,
		},
		Message: &icmp.Message{
			Type: replyType,
			Code: icmpEchoReplyCode,
			Body: &icmp.Echo{
				ID:   echoReq.ID,
				Seq:  echoReq.Seq,
				Data: resp.payload(),
			},
		},
	}

	cachedEncoder := ip.encoderPool.Get()
	// The encoded packet is a slice to of the encoder, so we shouldn't return the encoder back to the pool until
	// the encoded packet is sent.
	defer ip.encoderPool.Put(cachedEncoder)
	encoder, ok := cachedEncoder.(*packet.Encoder)
	if !ok {
		return fmt.Errorf("encoderPool returned %T, expect *packet.Encoder", cachedEncoder)
	}

	serializedPacket, err := encoder.Encode(&pk)
	if err != nil {
		return err
	}
	return responder.returnPacket(serializedPacket)
}

func (ip *icmpProxy) icmpEchoRoundtrip(dst netip.Addr, echo *icmp.Echo) (echoResp, error) {
	if dst.Is6() {
		if ip.srcSocketAddr == nil {
			return nil, fmt.Errorf("cannot send ICMPv6 using ICMPv4 proxy")
		}
		resp, err := ip.icmp6SendEcho(dst, echo)
		if err != nil {

			return nil, errors.Wrap(err, "failed to send/receive ICMPv6 echo")
		}
		return resp, nil
	}
	if ip.srcSocketAddr != nil {
		return nil, fmt.Errorf("cannot send ICMPv4 using ICMPv6 proxy")
	}
	resp, err := ip.icmpSendEcho(dst, echo)
	if err != nil {
		return nil, errors.Wrap(err, "failed to send/receive ICMPv4 echo")
	}
	return resp, nil
}

/*
Wrapper to call https://docs.microsoft.com/en-us/windows/win32/api/icmpapi/nf-icmpapi-icmpsendecho
Parameters:
- IcmpHandle: Handle created by IcmpCreateFile
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
func (ip *icmpProxy) icmpSendEcho(dst netip.Addr, echo *icmp.Echo) (*echoV4Resp, error) {
	dataSize := len(echo.Data)
	replySize := echoReplySize + uintptr(dataSize)
	replyBuf := make([]byte, replySize)
	noIPHeaderOption := nullParameter
	inAddr, err := inAddrV4(dst)
	if err != nil {
		return nil, err
	}
	replyCount, _, err := IcmpSendEcho_proc.Call(
		ip.handle,
		uintptr(inAddr),
		uintptr(unsafe.Pointer(&echo.Data[0])),
		uintptr(dataSize),
		noIPHeaderOption,
		uintptr(unsafe.Pointer(&replyBuf[0])),
		replySize,
		icmpRequestTimeoutMs,
	)
	if replyCount == 0 {
		// status is returned in 5th to 8th byte of reply buffer
		if status, parseErr := unmarshalIPStatus(replyBuf[4:8]); parseErr == nil && status != success {
			return nil, errors.Wrapf(err, "received ip status: %s", status)
		}
		return nil, errors.Wrap(err, "did not receive ICMP echo reply")
	} else if replyCount > 1 {
		ip.logger.Warn().Msgf("Received %d ICMP echo replies, only sending 1 back", replyCount)
	}
	return newEchoV4Resp(replyBuf)
}

// Third definition of https://docs.microsoft.com/en-us/windows/win32/api/inaddr/ns-inaddr-in_addr#syntax is address in uint32
func inAddrV4(ip netip.Addr) (uint32, error) {
	if !ip.Is4() {
		return 0, fmt.Errorf("%s is not IPv4", ip)
	}
	v4 := ip.As4()
	return endian.Uint32(v4[:]), nil
}

type echoResp interface {
	status() ipStatus
	rtt() uint32
	payload() []byte
}

type echoV4Resp struct {
	reply *echoReply
	data  []byte
}

func (r *echoV4Resp) status() ipStatus {
	return r.reply.Status
}

func (r *echoV4Resp) rtt() uint32 {
	return r.reply.RoundTripTime
}

func (r *echoV4Resp) payload() []byte {
	return r.data
}

func newEchoV4Resp(replyBuf []byte) (*echoV4Resp, error) {
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
	return &echoV4Resp{
		reply: &reply,
		data:  replyBuf[dataBufStart:],
	}, nil
}

/*
	Wrapper to call https://docs.microsoft.com/en-us/windows/win32/api/icmpapi/nf-icmpapi-icmp6sendecho2
	Parameters:
	- IcmpHandle: Handle created by Icmp6CreateFile
	- Event (optional): Event object to be signaled when a reply arrives
	- ApcRoutine (optional): Routine to call when the calling thread is in an alertable thread and a reply arrives
	- ApcContext (optional): Optional parameter to ApcRoutine
	- SourceAddress: Source address of the request
	- DestinationAddress: Destination address of the request
	- RequestData: A pointer to echo data
	- RequestSize: Number of bytes in buffer pointed by echo data
	- RequestOptions (optional): A pointer to the IPv6 header options
	- ReplyBuffer: A pointer to the buffer for echoReply, options and data
	- ReplySize: Number of bytes allocated for ReplyBuffer
	- Timeout: Timeout in milliseconds to wait for a reply
	Returns:
	- the number of replies in uint32
	To retain the reference allocated objects, conversion from pointer to uintptr must happen as arguments to the
	syscall function
*/

func (ip *icmpProxy) icmp6SendEcho(dst netip.Addr, echo *icmp.Echo) (*echoV6Resp, error) {
	dstAddr, err := newSockAddrIn6(dst)
	if err != nil {
		return nil, err
	}
	dataSize := len(echo.Data)
	// Reply buffer needs to be big enough to hold an echoV6Reply, echo data, 8 bytes for ICMP error message
	// and ioStatusBlock
	replySize := echoV6ReplySize + uintptr(dataSize) + uintptr(icmpv6ErrMessageSize) + ioStatusBlockSize
	replyBuf := make([]byte, replySize)
	noEvent := nullParameter
	noApcRoutine := nullParameter
	noAppCtx := nullParameter
	noIPHeaderOption := nullParameter
	replyCount, _, err := Icmp6SendEcho_proc.Call(
		ip.handle,
		noEvent,
		noApcRoutine,
		noAppCtx,
		uintptr(unsafe.Pointer(ip.srcSocketAddr)),
		uintptr(unsafe.Pointer(dstAddr)),
		uintptr(unsafe.Pointer(&echo.Data[0])),
		uintptr(dataSize),
		noIPHeaderOption,
		uintptr(unsafe.Pointer(&replyBuf[0])),
		replySize,
		icmpRequestTimeoutMs,
	)
	if replyCount == 0 {
		// status is in the 4 bytes after ipv6AddrEx. The reply buffer size is at least size of ipv6AddrEx + 4
		if status, parseErr := unmarshalIPStatus(replyBuf[unsafe.Sizeof(ipv6AddrEx{}) : unsafe.Sizeof(ipv6AddrEx{})+4]); parseErr == nil && status != success {
			return nil, fmt.Errorf("received ip status: %s", status)
		}
		return nil, errors.Wrap(err, "did not receive ICMP echo reply")
	} else if replyCount > 1 {
		ip.logger.Warn().Msgf("Received %d ICMP echo replies, only sending 1 back", replyCount)
	}
	return newEchoV6Resp(replyBuf, dataSize)
}

type echoV6Resp struct {
	reply *echoV6Reply
	data  []byte
}

func (r *echoV6Resp) status() ipStatus {
	return r.reply.Status
}

func (r *echoV6Resp) rtt() uint32 {
	return r.reply.RoundTripTime
}

func (r *echoV6Resp) payload() []byte {
	return r.data
}

func newEchoV6Resp(replyBuf []byte, dataSize int) (*echoV6Resp, error) {
	if len(replyBuf) == 0 {
		return nil, fmt.Errorf("reply buffer is empty")
	}
	reply := *(*echoV6Reply)(unsafe.Pointer(&replyBuf[0]))
	if reply.Status != success {
		return nil, fmt.Errorf("status %d", reply.Status)
	}
	if uintptr(len(replyBuf)) < unsafe.Sizeof(reply)+uintptr(dataSize) {
		return nil, fmt.Errorf("reply buffer size %d is too small to hold reply size %d + data size %d", len(replyBuf), echoV6ReplySize, dataSize)
	}
	return &echoV6Resp{
		reply: &reply,
		data:  replyBuf[echoV6ReplySize : echoV6ReplySize+uintptr(dataSize)],
	}, nil
}

func unmarshalIPStatus(replyBuf []byte) (ipStatus, error) {
	if len(replyBuf) != 4 {
		return 0, fmt.Errorf("ipStatus needs to be 4 bytes, got %d", len(replyBuf))
	}
	return ipStatus(endian.Uint32(replyBuf)), nil
}
