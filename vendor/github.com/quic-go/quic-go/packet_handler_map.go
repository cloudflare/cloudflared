package quic

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"hash"
	"io"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
)

// rawConn is a connection that allow reading of a receivedPackeh.
type rawConn interface {
	ReadPacket() (*receivedPacket, error)
	WritePacket(b []byte, addr net.Addr, oob []byte) (int, error)
	LocalAddr() net.Addr
	SetReadDeadline(time.Time) error
	io.Closer
}

type closePacket struct {
	payload []byte
	addr    net.Addr
	info    *packetInfo
}

type unknownPacketHandler interface {
	handlePacket(*receivedPacket)
	setCloseError(error)
}

var errListenerAlreadySet = errors.New("listener already set")

type packetHandlerMap struct {
	mutex       sync.Mutex
	handlers    map[protocol.ConnectionID]packetHandler
	resetTokens map[protocol.StatelessResetToken] /* stateless reset token */ packetHandler

	closed    bool
	closeChan chan struct{}

	enqueueClosePacket func(closePacket)

	deleteRetiredConnsAfter time.Duration

	statelessResetMutex  sync.Mutex
	statelessResetHasher hash.Hash

	logger utils.Logger
}

var _ packetHandlerManager = &packetHandlerMap{}

func newPacketHandlerMap(key *StatelessResetKey, enqueueClosePacket func(closePacket), logger utils.Logger) *packetHandlerMap {
	h := &packetHandlerMap{
		closeChan:               make(chan struct{}),
		handlers:                make(map[protocol.ConnectionID]packetHandler),
		resetTokens:             make(map[protocol.StatelessResetToken]packetHandler),
		deleteRetiredConnsAfter: protocol.RetiredConnectionIDDeleteTimeout,
		enqueueClosePacket:      enqueueClosePacket,
		logger:                  logger,
	}
	if key != nil {
		h.statelessResetHasher = hmac.New(sha256.New, key[:])
	}
	if h.logger.Debug() {
		go h.logUsage()
	}
	return h
}

func (h *packetHandlerMap) logUsage() {
	ticker := time.NewTicker(2 * time.Second)
	var printedZero bool
	for {
		select {
		case <-h.closeChan:
			return
		case <-ticker.C:
		}

		h.mutex.Lock()
		numHandlers := len(h.handlers)
		numTokens := len(h.resetTokens)
		h.mutex.Unlock()
		// If the number tracked handlers and tokens is zero, only print it a single time.
		hasZero := numHandlers == 0 && numTokens == 0
		if !hasZero || (hasZero && !printedZero) {
			h.logger.Debugf("Tracking %d connection IDs and %d reset tokens.\n", numHandlers, numTokens)
			printedZero = false
			if hasZero {
				printedZero = true
			}
		}
	}
}

func (h *packetHandlerMap) Get(id protocol.ConnectionID) (packetHandler, bool) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	handler, ok := h.handlers[id]
	return handler, ok
}

func (h *packetHandlerMap) Add(id protocol.ConnectionID, handler packetHandler) bool /* was added */ {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if _, ok := h.handlers[id]; ok {
		h.logger.Debugf("Not adding connection ID %s, as it already exists.", id)
		return false
	}
	h.handlers[id] = handler
	h.logger.Debugf("Adding connection ID %s.", id)
	return true
}

func (h *packetHandlerMap) AddWithConnID(clientDestConnID, newConnID protocol.ConnectionID, fn func() (packetHandler, bool)) bool {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if _, ok := h.handlers[clientDestConnID]; ok {
		h.logger.Debugf("Not adding connection ID %s for a new connection, as it already exists.", clientDestConnID)
		return false
	}
	conn, ok := fn()
	if !ok {
		return false
	}
	h.handlers[clientDestConnID] = conn
	h.handlers[newConnID] = conn
	h.logger.Debugf("Adding connection IDs %s and %s for a new connection.", clientDestConnID, newConnID)
	return true
}

func (h *packetHandlerMap) Remove(id protocol.ConnectionID) {
	h.mutex.Lock()
	delete(h.handlers, id)
	h.mutex.Unlock()
	h.logger.Debugf("Removing connection ID %s.", id)
}

func (h *packetHandlerMap) Retire(id protocol.ConnectionID) {
	h.logger.Debugf("Retiring connection ID %s in %s.", id, h.deleteRetiredConnsAfter)
	time.AfterFunc(h.deleteRetiredConnsAfter, func() {
		h.mutex.Lock()
		delete(h.handlers, id)
		h.mutex.Unlock()
		h.logger.Debugf("Removing connection ID %s after it has been retired.", id)
	})
}

// ReplaceWithClosed is called when a connection is closed.
// Depending on which side closed the connection, we need to:
// * remote close: absorb delayed packets
// * local close: retransmit the CONNECTION_CLOSE packet, in case it was lost
func (h *packetHandlerMap) ReplaceWithClosed(ids []protocol.ConnectionID, pers protocol.Perspective, connClosePacket []byte) {
	var handler packetHandler
	if connClosePacket != nil {
		handler = newClosedLocalConn(
			func(addr net.Addr, info *packetInfo) {
				h.enqueueClosePacket(closePacket{payload: connClosePacket, addr: addr, info: info})
			},
			pers,
			h.logger,
		)
	} else {
		handler = newClosedRemoteConn(pers)
	}

	h.mutex.Lock()
	for _, id := range ids {
		h.handlers[id] = handler
	}
	h.mutex.Unlock()
	h.logger.Debugf("Replacing connection for connection IDs %s with a closed connection.", ids)

	time.AfterFunc(h.deleteRetiredConnsAfter, func() {
		h.mutex.Lock()
		handler.shutdown()
		for _, id := range ids {
			delete(h.handlers, id)
		}
		h.mutex.Unlock()
		h.logger.Debugf("Removing connection IDs %s for a closed connection after it has been retired.", ids)
	})
}

func (h *packetHandlerMap) AddResetToken(token protocol.StatelessResetToken, handler packetHandler) {
	h.mutex.Lock()
	h.resetTokens[token] = handler
	h.mutex.Unlock()
}

func (h *packetHandlerMap) RemoveResetToken(token protocol.StatelessResetToken) {
	h.mutex.Lock()
	delete(h.resetTokens, token)
	h.mutex.Unlock()
}

func (h *packetHandlerMap) GetByResetToken(token protocol.StatelessResetToken) (packetHandler, bool) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	handler, ok := h.resetTokens[token]
	return handler, ok
}

func (h *packetHandlerMap) CloseServer() {
	h.mutex.Lock()
	var wg sync.WaitGroup
	for _, handler := range h.handlers {
		if handler.getPerspective() == protocol.PerspectiveServer {
			wg.Add(1)
			go func(handler packetHandler) {
				// blocks until the CONNECTION_CLOSE has been sent and the run-loop has stopped
				handler.shutdown()
				wg.Done()
			}(handler)
		}
	}
	h.mutex.Unlock()
	wg.Wait()
}

func (h *packetHandlerMap) Close(e error) {
	h.mutex.Lock()

	if h.closed {
		h.mutex.Unlock()
		return
	}

	close(h.closeChan)

	var wg sync.WaitGroup
	for _, handler := range h.handlers {
		wg.Add(1)
		go func(handler packetHandler) {
			handler.destroy(e)
			wg.Done()
		}(handler)
	}
	h.closed = true
	h.mutex.Unlock()
	wg.Wait()
}

func (h *packetHandlerMap) GetStatelessResetToken(connID protocol.ConnectionID) protocol.StatelessResetToken {
	var token protocol.StatelessResetToken
	if h.statelessResetHasher == nil {
		// Return a random stateless reset token.
		// This token will be sent in the server's transport parameters.
		// By using a random token, an off-path attacker won't be able to disrupt the connection.
		rand.Read(token[:])
		return token
	}
	h.statelessResetMutex.Lock()
	h.statelessResetHasher.Write(connID.Bytes())
	copy(token[:], h.statelessResetHasher.Sum(nil))
	h.statelessResetHasher.Reset()
	h.statelessResetMutex.Unlock()
	return token
}
