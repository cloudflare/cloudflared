package packet

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrFunnelNotFound = errors.New("funnel not found")
)

// Funnel is an abstraction to pipe from 1 src to 1 or more destinations
type Funnel interface {
	// SendToDst sends a raw packet to a destination
	SendToDst(dst netip.Addr, pk RawPacket) error
	// ReturnToSrc returns a raw packet to the source
	ReturnToSrc(pk RawPacket) error
	// LastActive returns the last time SendToDst or ReturnToSrc is called
	LastActive() time.Time
	// Close closes the funnel. Further call to SendToDst or ReturnToSrc should return an error
	Close() error
	// Equal compares if 2 funnels are equivalent
	Equal(other Funnel) bool
}

// FunnelUniPipe is a unidirectional pipe for sending raw packets
type FunnelUniPipe interface {
	// SendPacket sends a packet to/from the funnel. It must not modify the packet,
	// and after return it must not read the packet
	SendPacket(dst netip.Addr, pk RawPacket) error
	Close() error
}

// RawPacketFunnel is an implementation of Funnel that sends raw packets. It can be embedded in other structs to
// satisfy the Funnel interface.
type RawPacketFunnel struct {
	Src netip.Addr
	// last active unix time. Unit is seconds
	lastActive int64
	sendPipe   FunnelUniPipe
	returnPipe FunnelUniPipe
}

func NewRawPacketFunnel(src netip.Addr, sendPipe, returnPipe FunnelUniPipe) *RawPacketFunnel {
	return &RawPacketFunnel{
		Src:        src,
		lastActive: time.Now().Unix(),
		sendPipe:   sendPipe,
		returnPipe: returnPipe,
	}
}

func (rpf *RawPacketFunnel) SendToDst(dst netip.Addr, pk RawPacket) error {
	rpf.updateLastActive()
	return rpf.sendPipe.SendPacket(dst, pk)
}

func (rpf *RawPacketFunnel) ReturnToSrc(pk RawPacket) error {
	rpf.updateLastActive()
	return rpf.returnPipe.SendPacket(rpf.Src, pk)
}

func (rpf *RawPacketFunnel) updateLastActive() {
	atomic.StoreInt64(&rpf.lastActive, time.Now().Unix())
}

func (rpf *RawPacketFunnel) LastActive() time.Time {
	lastActive := atomic.LoadInt64(&rpf.lastActive)
	return time.Unix(lastActive, 0)
}

func (rpf *RawPacketFunnel) Close() error {
	sendPipeErr := rpf.sendPipe.Close()
	returnPipeErr := rpf.returnPipe.Close()
	if sendPipeErr != nil {
		return sendPipeErr
	}
	if returnPipeErr != nil {
		return returnPipeErr
	}
	return nil
}

func (rpf *RawPacketFunnel) Equal(other Funnel) bool {
	otherRawFunnel, ok := other.(*RawPacketFunnel)
	if !ok {
		return false
	}
	if rpf.Src != otherRawFunnel.Src {
		return false
	}
	if rpf.sendPipe != otherRawFunnel.sendPipe {
		return false
	}
	if rpf.returnPipe != otherRawFunnel.returnPipe {
		return false
	}
	return true
}

// FunnelID represents a key type that can be used by FunnelTracker
type FunnelID interface {
	// Type returns the name of the type that implements the FunnelID
	Type() string
	fmt.Stringer
}

// FunnelTracker tracks funnel from the perspective of eyeball to origin
type FunnelTracker struct {
	lock    sync.RWMutex
	funnels map[FunnelID]Funnel
}

func NewFunnelTracker() *FunnelTracker {
	return &FunnelTracker{
		funnels: make(map[FunnelID]Funnel),
	}
}

func (ft *FunnelTracker) ScheduleCleanup(ctx context.Context, idleTimeout time.Duration) {
	checkIdleTicker := time.NewTicker(idleTimeout)
	defer checkIdleTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-checkIdleTicker.C:
			ft.cleanup(idleTimeout)
		}
	}
}

func (ft *FunnelTracker) cleanup(idleTimeout time.Duration) {
	ft.lock.Lock()
	defer ft.lock.Unlock()

	now := time.Now()
	for id, funnel := range ft.funnels {
		lastActive := funnel.LastActive()
		if now.After(lastActive.Add(idleTimeout)) {
			funnel.Close()
			delete(ft.funnels, id)
		}
	}
}

func (ft *FunnelTracker) Get(id FunnelID) (Funnel, bool) {
	ft.lock.RLock()
	defer ft.lock.RUnlock()
	funnel, ok := ft.funnels[id]
	return funnel, ok
}

// Registers a funnel. It replaces the current funnel.
func (ft *FunnelTracker) GetOrRegister(id FunnelID, newFunnelFunc func() (Funnel, error)) (funnel Funnel, new bool, err error) {
	ft.lock.Lock()
	defer ft.lock.Unlock()
	currentFunnel, exists := ft.funnels[id]
	if exists {
		return currentFunnel, false, nil
	}
	newFunnel, err := newFunnelFunc()
	if err != nil {
		return nil, false, err
	}
	ft.funnels[id] = newFunnel
	return newFunnel, true, nil
}

// Unregisters a funnel if the funnel equals to the current funnel
func (ft *FunnelTracker) Unregister(id FunnelID, funnel Funnel) (deleted bool) {
	ft.lock.Lock()
	defer ft.lock.Unlock()
	currentFunnel, exists := ft.funnels[id]
	if !exists {
		return true
	}
	if currentFunnel.Equal(funnel) {
		currentFunnel.Close()
		delete(ft.funnels, id)
		return true
	}
	return false
}
