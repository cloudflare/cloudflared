package v3_test

import (
	"crypto/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	v3 "github.com/cloudflare/cloudflared/quic/v3"
)

var (
	testRequestIDBytes = [16]byte{
		0x00, 0x11, 0x22, 0x33,
		0x44, 0x55, 0x66, 0x77,
		0x88, 0x99, 0xaa, 0xbb,
		0xcc, 0xdd, 0xee, 0xff,
	}
	testRequestID = mustRequestID(testRequestIDBytes)
)

func mustRequestID(data [16]byte) v3.RequestID {
	id, err := v3.RequestIDFromSlice(data[:])
	if err != nil {
		panic(err)
	}
	return id
}

func TestRequestIDParsing(t *testing.T) {
	buf1 := make([]byte, 16)
	n, err := rand.Read(buf1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 16 {
		t.Fatalf("did not read 16 bytes: %d", n)
	}
	id, err := v3.RequestIDFromSlice(buf1)
	if err != nil {
		t.Fatal(err)
	}
	buf2 := make([]byte, 16)
	err = id.MarshalBinaryTo(buf2)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(buf1, buf2) {
		t.Fatalf("buf1 != buf2: %+v %+v", buf1, buf2)
	}
}

func TestRequestID_MarshalBinary(t *testing.T) {
	buf := make([]byte, 16)
	err := testRequestID.MarshalBinaryTo(buf)
	require.NoError(t, err)
	require.Len(t, buf, 16)

	parsed := v3.RequestID{}
	err = parsed.UnmarshalBinary(buf)
	require.NoError(t, err)
	require.Equal(t, testRequestID, parsed)
}
