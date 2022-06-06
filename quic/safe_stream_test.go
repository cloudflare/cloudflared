package quic

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/stretchr/testify/require"
)

var (
	testTLSServerConfig = GenerateTLSConfig()
	testQUICConfig      = &quic.Config{
		KeepAlivePeriod: 5 * time.Second,
		EnableDatagrams: true,
	}
	exchanges       = 1000
	msgsPerExchange = 10
	testMsg         = "Ok message"
)

func TestSafeStreamClose(t *testing.T) {
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	udpListener, err := net.ListenUDP(udpAddr.Network(), udpAddr)
	require.NoError(t, err)
	defer udpListener.Close()

	var serverReady sync.WaitGroup
	serverReady.Add(1)

	var done sync.WaitGroup
	done.Add(1)
	go func() {
		defer done.Done()
		quicServer(t, &serverReady, udpListener)
	}()

	done.Add(1)
	go func() {
		serverReady.Wait()
		defer done.Done()
		quicClient(t, udpListener.LocalAddr())
	}()

	done.Wait()
}

func quicClient(t *testing.T, addr net.Addr) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"argotunnel"},
	}
	session, err := quic.DialAddr(addr.String(), tlsConf, testQUICConfig)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for exchange := 0; exchange < exchanges; exchange++ {
		quicStream, err := session.AcceptStream(context.Background())
		require.NoError(t, err)
		wg.Add(1)

		go func(iter int) {
			defer wg.Done()

			stream := NewSafeStreamCloser(quicStream)
			defer stream.Close()

			// Do a bunch of round trips over this stream that should work.
			for msg := 0; msg < msgsPerExchange; msg++ {
				clientRoundTrip(t, stream, true)
			}
			// And one that won't work necessarily, but shouldn't break other streams in the session.
			if iter%2 == 0 {
				clientRoundTrip(t, stream, false)
			}
		}(exchange)
	}

	wg.Wait()
}

func quicServer(t *testing.T, serverReady *sync.WaitGroup, conn net.PacketConn) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	earlyListener, err := quic.Listen(conn, testTLSServerConfig, testQUICConfig)
	require.NoError(t, err)

	serverReady.Done()
	session, err := earlyListener.Accept(ctx)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for exchange := 0; exchange < exchanges; exchange++ {
		quicStream, err := session.OpenStreamSync(context.Background())
		require.NoError(t, err)
		wg.Add(1)

		go func(iter int) {
			defer wg.Done()

			stream := NewSafeStreamCloser(quicStream)
			defer stream.Close()

			// Do a bunch of round trips over this stream that should work.
			for msg := 0; msg < msgsPerExchange; msg++ {
				serverRoundTrip(t, stream, true)
			}
			// And one that won't work necessarily, but shouldn't break other streams in the session.
			if iter%2 == 1 {
				serverRoundTrip(t, stream, false)
			}
		}(exchange)
	}

	wg.Wait()
}

func clientRoundTrip(t *testing.T, stream io.ReadWriteCloser, mustWork bool) {
	response := make([]byte, len(testMsg))
	_, err := stream.Read(response)
	if !mustWork {
		return
	}
	if err != io.EOF {
		require.NoError(t, err)
	}
	require.Equal(t, testMsg, string(response))
}

func serverRoundTrip(t *testing.T, stream io.ReadWriteCloser, mustWork bool) {
	_, err := stream.Write([]byte(testMsg))
	if !mustWork {
		return
	}
	require.NoError(t, err)
}
