package connection

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"sync"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
)

var (
	portForConnIndex = make(map[uint8]int, 0)
	portMapMutex     sync.Mutex
)

func DialQuic(
	ctx context.Context,
	quicConfig *quic.Config,
	tlsConfig *tls.Config,
	edgeAddr netip.AddrPort,
	localAddr net.IP,
	connIndex uint8,
	logger *zerolog.Logger,
) (quic.Connection, error) {
	udpConn, err := createUDPConnForConnIndex(connIndex, localAddr, edgeAddr, logger)
	if err != nil {
		return nil, err
	}

	conn, err := quic.Dial(ctx, udpConn, net.UDPAddrFromAddrPort(edgeAddr), tlsConfig, quicConfig)
	if err != nil {
		// close the udp server socket in case of error connecting to the edge
		udpConn.Close()
		return nil, &EdgeQuicDialError{Cause: err}
	}

	// wrap the session, so that the UDPConn is closed after session is closed.
	conn = &wrapCloseableConnQuicConnection{
		conn,
		udpConn,
	}
	return conn, nil
}

func createUDPConnForConnIndex(connIndex uint8, localIP net.IP, edgeIP netip.AddrPort, logger *zerolog.Logger) (*net.UDPConn, error) {
	portMapMutex.Lock()
	defer portMapMutex.Unlock()

	listenNetwork := "udp"
	// https://github.com/quic-go/quic-go/issues/3793 DF bit cannot be set for dual stack listener ("udp") on macOS,
	// to set the DF bit properly, the network string needs to be specific to the IP family.
	if runtime.GOOS == "darwin" {
		if edgeIP.Addr().Is4() {
			listenNetwork = "udp4"
		} else {
			listenNetwork = "udp6"
		}
	}

	// if port was not set yet, it will be zero, so bind will randomly allocate one.
	if port, ok := portForConnIndex[connIndex]; ok {
		udpConn, err := net.ListenUDP(listenNetwork, &net.UDPAddr{IP: localIP, Port: port})
		// if there wasn't an error, or if port was 0 (independently of error or not, just return)
		if err == nil {
			return udpConn, nil
		} else {
			logger.Debug().Err(err).Msgf("Unable to reuse port %d for connIndex %d. Falling back to random allocation.", port, connIndex)
		}
	}

	// if we reached here, then there was an error or port as not been allocated it.
	udpConn, err := net.ListenUDP(listenNetwork, &net.UDPAddr{IP: localIP, Port: 0})
	if err == nil {
		udpAddr, ok := (udpConn.LocalAddr()).(*net.UDPAddr)
		if !ok {
			return nil, fmt.Errorf("unable to cast to udpConn")
		}
		portForConnIndex[connIndex] = udpAddr.Port
	} else {
		delete(portForConnIndex, connIndex)
	}

	return udpConn, err
}

type wrapCloseableConnQuicConnection struct {
	quic.Connection
	udpConn *net.UDPConn
}

func (w *wrapCloseableConnQuicConnection) CloseWithError(errorCode quic.ApplicationErrorCode, reason string) error {
	err := w.Connection.CloseWithError(errorCode, reason)
	w.udpConn.Close()

	return err
}
