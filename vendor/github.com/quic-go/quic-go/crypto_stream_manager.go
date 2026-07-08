package quic

import (
	"fmt"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

type cryptoStreamManager struct {
	initialStream   *cryptoStream
	handshakeStream *cryptoStream
	oneRTTStream    *cryptoStream
}

func newCryptoStreamManager(
	initialStream *cryptoStream,
	handshakeStream *cryptoStream,
	oneRTTStream *cryptoStream,
) *cryptoStreamManager {
	return &cryptoStreamManager{
		initialStream:   initialStream,
		handshakeStream: handshakeStream,
		oneRTTStream:    oneRTTStream,
	}
}

func (m *cryptoStreamManager) HandleCryptoFrame(frame *wire.CryptoFrame, encLevel protocol.EncryptionLevel) error {
	var str *cryptoStream
	//nolint:exhaustive // CRYPTO frames cannot be sent in 0-RTT packets.
	switch encLevel {
	case protocol.EncryptionInitial:
		str = m.initialStream
	case protocol.EncryptionHandshake:
		str = m.handshakeStream
	case protocol.Encryption1RTT:
		str = m.oneRTTStream
	default:
		return fmt.Errorf("received CRYPTO frame with unexpected encryption level: %s", encLevel)
	}
	return str.HandleCryptoFrame(frame)
}

func (m *cryptoStreamManager) GetCryptoData(encLevel protocol.EncryptionLevel) []byte {
	var str *cryptoStream
	//nolint:exhaustive // CRYPTO frames cannot be sent in 0-RTT packets.
	switch encLevel {
	case protocol.EncryptionInitial:
		str = m.initialStream
	case protocol.EncryptionHandshake:
		str = m.handshakeStream
	case protocol.Encryption1RTT:
		str = m.oneRTTStream
	default:
		panic(fmt.Sprintf("received CRYPTO frame with unexpected encryption level: %s", encLevel))
	}
	return str.GetCryptoData()
}

func (m *cryptoStreamManager) GetPostHandshakeData(maxSize protocol.ByteCount) *wire.CryptoFrame {
	if !m.oneRTTStream.HasData() {
		return nil
	}
	return m.oneRTTStream.PopCryptoFrame(maxSize)
}

func (m *cryptoStreamManager) Drop(encLevel protocol.EncryptionLevel) error {
	//nolint:exhaustive // 1-RTT keys should never get dropped.
	switch encLevel {
	case protocol.EncryptionInitial:
		return m.initialStream.Finish()
	case protocol.EncryptionHandshake:
		return m.handshakeStream.Finish()
	default:
		panic(fmt.Sprintf("dropped unexpected encryption level: %s", encLevel))
	}
}
