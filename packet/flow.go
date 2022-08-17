package packet

import (
	"errors"
	"net"
	"net/netip"
	"sync"
)

type flowID string

var (
	ErrFlowNotFound = errors.New("flow not found")
)

func newFlowID(ip net.IP) flowID {
	return flowID(ip.String())
}

type Flow struct {
	Src       netip.Addr
	Dst       netip.Addr
	Responder FlowResponder
}

func isSameFlow(f1, f2 *Flow) bool {
	if f1 == nil || f2 == nil {
		return false
	}
	return *f1 == *f2
}

// FlowResponder sends response packets to the flow
type FlowResponder interface {
	// SendPacket returns a packet to the flow. It must not modify the packet,
	// and after return it must not read the packet
	SendPacket(pk RawPacket) error
}

// SrcFlowTracker tracks flow from the perspective of eyeball to origin
// flowID is the source IP
type SrcFlowTracker struct {
	lock  sync.RWMutex
	flows map[flowID]*Flow
}

func NewSrcFlowTracker() *SrcFlowTracker {
	return &SrcFlowTracker{
		flows: make(map[flowID]*Flow),
	}
}

func (sft *SrcFlowTracker) Get(srcIP net.IP) (*Flow, bool) {
	sft.lock.RLock()
	defer sft.lock.RUnlock()
	id := newFlowID(srcIP)
	flow, ok := sft.flows[id]
	return flow, ok
}

// Registers a flow. If shouldReplace = true, replace the current flow
func (sft *SrcFlowTracker) Register(flow *Flow, shouldReplace bool) (replaced bool) {
	sft.lock.Lock()
	defer sft.lock.Unlock()
	id := flowID(flow.Src.String())
	currentFlow, ok := sft.flows[id]
	if !ok {
		sft.flows[id] = flow
		return false
	}

	if shouldReplace && isSameFlow(currentFlow, flow) {
		sft.flows[id] = flow
		return true
	}
	return false
}

// Unregisters a flow. If force = true, delete it even if it maps to a different flow
func (sft *SrcFlowTracker) Unregister(flow *Flow, force bool) (forceDeleted bool) {
	sft.lock.Lock()
	defer sft.lock.Unlock()
	id := flowID(flow.Src.String())
	currentFlow, ok := sft.flows[id]
	if !ok {
		return false
	}
	if isSameFlow(currentFlow, flow) {
		delete(sft.flows, id)
		return false
	}
	if force {
		delete(sft.flows, id)
		return true
	}
	return false
}
