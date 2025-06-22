//go:build windows && cgo

package ingress

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/netip"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/net/icmp"

	"github.com/stretchr/testify/require"
)

// TestParseEchoReply tests parsing raw bytes from icmpSendEcho into echoResp
func TestParseEchoReply(t *testing.T) {
	dst, err := inAddrV4(netip.MustParseAddr("192.168.10.20"))
	require.NoError(t, err)

	validReplyData := []byte(t.Name())
	validReply := echoReply{
		Address:       dst,
		Status:        success,
		RoundTripTime: uint32(20),
		DataSize:      uint16(len(validReplyData)),
		DataPointer:   &validReplyData[0],
		Options: ipOption{
			TTL: 59,
		},
	}

	destHostUnreachableReply := validReply
	destHostUnreachableReply.Status = destHostUnreachable

	tests := []struct {
		testCase      string
		replyBuf      []byte
		expectedReply *echoReply
		expectedData  []byte
	}{
		{
			testCase: "empty buffer",
		},
		{
			testCase: "status not success",
			replyBuf: destHostUnreachableReply.marshal(t, []byte{}),
		},
		{
			testCase:      "valid reply",
			replyBuf:      validReply.marshal(t, validReplyData),
			expectedReply: &validReply,
			expectedData:  validReplyData,
		},
	}

	for _, test := range tests {
		resp, err := newEchoV4Resp(test.replyBuf)
		if test.expectedReply == nil {
			require.Error(t, err)
			require.Nil(t, resp)
		} else {
			require.NoError(t, err)
			require.Equal(t, resp.reply, test.expectedReply)
			require.True(t, bytes.Equal(resp.data, test.expectedData))
		}
	}
}

// TestParseEchoV6Reply tests parsing raw bytes from icmp6SendEcho into echoV6Resp
func TestParseEchoV6Reply(t *testing.T) {
	dst := netip.MustParseAddr("2606:3600:4500::3333").As16()
	var addr [8]uint16
	for i := 0; i < 8; i++ {
		addr[i] = binary.BigEndian.Uint16(dst[i*2 : i*2+2])
	}

	validReplyData := []byte(t.Name())
	validReply := echoV6Reply{
		Address: ipv6AddrEx{
			addr: addr,
		},
		Status:        success,
		RoundTripTime: 25,
	}

	destHostUnreachableReply := validReply
	destHostUnreachableReply.Status = ipv6DestUnreachable

	tests := []struct {
		testCase      string
		replyBuf      []byte
		expectedReply *echoV6Reply
		expectedData  []byte
	}{
		{
			testCase: "empty buffer",
		},
		{
			testCase: "status not success",
			replyBuf: destHostUnreachableReply.marshal(t, []byte{}),
		},
		{
			testCase:      "valid reply",
			replyBuf:      validReply.marshal(t, validReplyData),
			expectedReply: &validReply,
			expectedData:  validReplyData,
		},
	}

	for _, test := range tests {
		resp, err := newEchoV6Resp(test.replyBuf, len(test.expectedData))
		if test.expectedReply == nil {
			require.Error(t, err)
			require.Nil(t, resp)
		} else {
			require.NoError(t, err)
			require.Equal(t, resp.reply, test.expectedReply)
			require.True(t, bytes.Equal(resp.data, test.expectedData))
		}
	}
}

// TestSendEchoErrors makes sure icmpSendEcho handles error cases
func TestSendEchoErrors(t *testing.T) {
	testSendEchoErrors(t, netip.IPv4Unspecified())
	testSendEchoErrors(t, netip.IPv6Unspecified())
}

func testSendEchoErrors(t *testing.T, listenIP netip.Addr) {
	proxy, err := newICMPProxy(listenIP, &noopLogger, time.Second)
	require.NoError(t, err)

	echo := icmp.Echo{
		ID:   6193,
		Seq:  25712,
		Data: []byte(t.Name()),
	}
	documentIP := netip.MustParseAddr("192.0.2.200")
	if listenIP.Is6() {
		documentIP = netip.MustParseAddr("2001:db8::1")
	}
	resp, err := proxy.icmpEchoRoundtrip(documentIP, &echo)
	require.Error(t, err)
	require.Nil(t, resp)
}

func (er *echoReply) marshal(t *testing.T, data []byte) []byte {
	buf := new(bytes.Buffer)

	for _, field := range []any{
		er.Address,
		er.Status,
		er.RoundTripTime,
		er.DataSize,
		er.Reserved,
	} {
		require.NoError(t, binary.Write(buf, endian, field))
	}

	require.NoError(t, marshalPointer(buf, uintptr(unsafe.Pointer(er.DataPointer))))

	for _, field := range []any{
		er.Options.TTL,
		er.Options.Tos,
		er.Options.Flags,
		er.Options.OptionsSize,
	} {
		require.NoError(t, binary.Write(buf, endian, field))
	}

	require.NoError(t, marshalPointer(buf, er.Options.OptionsData))

	padSize := buf.Len() % int(unsafe.Alignof(er))
	padding := make([]byte, padSize)
	n, err := buf.Write(padding)
	require.NoError(t, err)
	require.Equal(t, padSize, n)

	n, err = buf.Write(data)
	require.NoError(t, err)
	require.Equal(t, len(data), n)

	return buf.Bytes()
}

func marshalPointer(buf io.Writer, ptr uintptr) error {
	size := unsafe.Sizeof(ptr)
	switch size {
	case 4:
		return binary.Write(buf, endian, uint32(ptr))
	case 8:
		return binary.Write(buf, endian, uint64(ptr))
	default:
		return fmt.Errorf("unexpected pointer size %d", size)
	}
}

func (er *echoV6Reply) marshal(t *testing.T, data []byte) []byte {
	buf := new(bytes.Buffer)

	for _, field := range []any{
		er.Address.port,
		er.Address.flowInfoUpper,
		er.Address.flowInfoLower,
		er.Address.addr,
		er.Address.scopeID,
	} {
		require.NoError(t, binary.Write(buf, endian, field))
	}

	padSize := buf.Len() % int(unsafe.Alignof(er))
	padding := make([]byte, padSize)
	n, err := buf.Write(padding)
	require.NoError(t, err)
	require.Equal(t, padSize, n)

	for _, field := range []any{
		er.Status,
		er.RoundTripTime,
	} {
		require.NoError(t, binary.Write(buf, endian, field))
	}

	n, err = buf.Write(data)
	require.NoError(t, err)
	require.Equal(t, len(data), n)

	return buf.Bytes()
}
