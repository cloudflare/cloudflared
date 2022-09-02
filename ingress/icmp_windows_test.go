//go:build windows

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
		resp, err := newEchoResp(test.replyBuf)
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

//  TestSendEchoErrors makes sure icmpSendEcho handles error cases
func TestSendEchoErrors(t *testing.T) {
	proxy, err := newICMPProxy(localhostIP, &noopLogger, time.Second)
	require.NoError(t, err)
	winProxy := proxy.(*icmpProxy)

	echo := icmp.Echo{
		ID:   6193,
		Seq:  25712,
		Data: []byte(t.Name()),
	}
	documentIP := netip.MustParseAddr("192.0.2.200")
	resp, err := winProxy.icmpSendEcho(documentIP, &echo)
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
