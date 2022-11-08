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
	// Updates the last time traffic went through this funnel
	UpdateLastActive()
	// LastActive returns the last time there is traffic through this funnel
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

type ActivityTracker struct {
	// last active unix time. Unit is seconds
	lastActive int64
}

func NewActivityTracker() *ActivityTracker {
	return &ActivityTracker{
		lastActive: time.Now().Unix(),
	}
}

func (at *ActivityTracker) UpdateLastActive() {
	atomic.StoreInt64(&at.lastActive, time.Now().Unix())
}

func (at *ActivityTracker) LastActive() time.Time {
	lastActive := atomic.LoadInt64(&at.lastActive)
	return time.Unix(lastActive, 0)
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

// Registers a funnel. If the `id` is already registered and `shouldReplaceFunc` returns true, it closes and replaces
// the current funnel. If `newFunnelFunc` returns an error, the `id` will remain unregistered, even if it was registered
// when calling this function.
func (ft *FunnelTracker) GetOrRegister(
	id FunnelID,
	shouldReplaceFunc func(Funnel) bool,
	newFunnelFunc func() (Funnel, error),
) (funnel Funnel, new bool, err error) {
	ft.lock.Lock()
	defer ft.lock.Unlock()
	currentFunnel, exists := ft.funnels[id]
	if exists {
		if !shouldReplaceFunc(currentFunnel) {
			return currentFunnel, false, nil
		}
		currentFunnel.Close()
		delete(ft.funnels, id)
	}
	newFunnel, err := newFunnelFunc()
	if err != nil {
		return nil, false, err
	}
	ft.funnels[id] = newFunnel
	return newFunnel, true, nil
}

// Unregisters and closes a funnel if the funnel equals to the current funnel
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
