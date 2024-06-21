package quic

import (
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/logging"
)

type mtuDiscoverer interface {
	// Start starts the MTU discovery process.
	// It's unnecessary to call ShouldSendProbe before that.
	Start()
	ShouldSendProbe(now time.Time) bool
	CurrentSize() protocol.ByteCount
	GetPing() (ping ackhandler.Frame, datagramSize protocol.ByteCount)
}

const (
	// At some point, we have to stop searching for a higher MTU.
	// We're happy to send a packet that's 10 bytes smaller than the actual MTU.
	maxMTUDiff = 20
	// send a probe packet every mtuProbeDelay RTTs
	mtuProbeDelay = 5
	// Once maxLostMTUProbes MTU probe packets larger than a certain size are lost,
	// MTU discovery won't probe for larger MTUs than this size.
	// The algorithm used here is resilient to packet loss of (maxLostMTUProbes - 1) packets.
	maxLostMTUProbes = 3
)

// The Path MTU is found by sending a larger packet every now and then.
// If the packet is acknowledged, we conclude that the path supports this larger packet size.
// If the packet is lost, this can mean one of two things:
//   1. The path doesn't support this larger packet size, or
//   2. The packet was lost due to packet loss, independent of its size.
// The algorithm used here is resilient to packet loss of (maxLostMTUProbes - 1) packets.
// For simplicty, the following example use maxLostMTUProbes = 2.
//
// Initialization:
//    |------------------------------------------------------------------------------|
//   min                                                                            max
//
// The first MTU probe packet will have size (min+max)/2.
// Assume that this packet is acknowledged. We can now move the min marker,
// and continue the search in the resulting interval.
//
// If 1st probe packet acknowledged:
//    |---------------------------------------|--------------------------------------|
//                                           min                                    max
//
// If 1st probe packet lost:
//    |---------------------------------------|--------------------------------------|
//   min                                    lost[0]                                 max
//
// We can't conclude that the path doesn't support this packet size, since the loss of the probe
// packet could have been unrelated to the packet size.  A larger probe packet will be sent later on.
// After a loss, the next probe packet has size (min+lost[0])/2.
// Now assume this probe packet is acknowledged:
//
// 2nd probe packet acknowledged:
//    |------------------|--------------------|--------------------------------------|
//                      min                lost[0]                                  max
//
// First of all, we conclude that the path supports at least this MTU. That's progress!
// Second, we probe a bit more aggressively with the next probe packet:
// After an acknowledgement, the next probe packet has size (min+max)/2.
// This means we'll send a packet larger than the first probe packet (which was lost).
//
// If 3rd probe packet acknowledged:
//    |-------------------------------------------------|----------------------------|
//                                                     min                          max
//
// We can conclude that the loss of the 1st probe packet was not due to its size, and
// continue searching in a much smaller interval now.
//
// If 3rd probe packet lost:
//    |------------------|--------------------|---------|----------------------------|
//                      min                lost[0]     max
//
// Since in our example numPTOProbes = 2, and we lost 2 packets smaller than max, we
// conclude that this packet size is not supported on the path, and reduce the maximum
// value of the search interval.
//
// MTU discovery concludes once the interval min and max has been narrowed down to maxMTUDiff.

type mtuFinder struct {
	lastProbeTime time.Time
	mtuIncreased  func(protocol.ByteCount)

	rttStats *utils.RTTStats

	inFlight protocol.ByteCount // the size of the probe packet currently in flight. InvalidByteCount if none is in flight
	min      protocol.ByteCount
	limit    protocol.ByteCount

	// on initialization, we treat the maximum size as the first "lost" packet
	lost             [maxLostMTUProbes]protocol.ByteCount
	lastProbeWasLost bool

	tracer *logging.ConnectionTracer
}

var _ mtuDiscoverer = &mtuFinder{}

func newMTUDiscoverer(
	rttStats *utils.RTTStats,
	start, max protocol.ByteCount,
	mtuIncreased func(protocol.ByteCount),
	tracer *logging.ConnectionTracer,
) *mtuFinder {
	f := &mtuFinder{
		inFlight:     protocol.InvalidByteCount,
		min:          start,
		limit:        max,
		rttStats:     rttStats,
		mtuIncreased: mtuIncreased,
		tracer:       tracer,
	}
	for i := range f.lost {
		if i == 0 {
			f.lost[i] = max
			continue
		}
		f.lost[i] = protocol.InvalidByteCount
	}
	return f
}

func (f *mtuFinder) done() bool {
	return f.max()-f.min <= maxMTUDiff+1
}

func (f *mtuFinder) max() protocol.ByteCount {
	for i, v := range f.lost {
		if v == protocol.InvalidByteCount {
			return f.lost[i-1]
		}
	}
	return f.lost[len(f.lost)-1]
}

func (f *mtuFinder) Start() {
	f.lastProbeTime = time.Now() // makes sure the first probe packet is not sent immediately
}

func (f *mtuFinder) ShouldSendProbe(now time.Time) bool {
	if f.lastProbeTime.IsZero() {
		return false
	}
	if f.inFlight != protocol.InvalidByteCount || f.done() {
		return false
	}
	return !now.Before(f.lastProbeTime.Add(mtuProbeDelay * f.rttStats.SmoothedRTT()))
}

func (f *mtuFinder) GetPing() (ackhandler.Frame, protocol.ByteCount) {
	var size protocol.ByteCount
	if f.lastProbeWasLost {
		size = (f.min + f.lost[0]) / 2
	} else {
		size = (f.min + f.max()) / 2
	}
	f.lastProbeTime = time.Now()
	f.inFlight = size
	return ackhandler.Frame{
		Frame:   &wire.PingFrame{},
		Handler: &mtuFinderAckHandler{f},
	}, size
}

func (f *mtuFinder) CurrentSize() protocol.ByteCount {
	return f.min
}

type mtuFinderAckHandler struct {
	*mtuFinder
}

var _ ackhandler.FrameHandler = &mtuFinderAckHandler{}

func (h *mtuFinderAckHandler) OnAcked(wire.Frame) {
	size := h.inFlight
	if size == protocol.InvalidByteCount {
		panic("OnAcked callback called although there's no MTU probe packet in flight")
	}
	h.inFlight = protocol.InvalidByteCount
	h.min = size
	h.lastProbeWasLost = false
	// remove all values smaller than size from the lost array
	var j int
	for i, v := range h.lost {
		if size < v {
			j = i
			break
		}
	}
	if j > 0 {
		for i := 0; i < len(h.lost); i++ {
			if i+j < len(h.lost) {
				h.lost[i] = h.lost[i+j]
			} else {
				h.lost[i] = protocol.InvalidByteCount
			}
		}
	}
	if h.tracer != nil && h.tracer.UpdatedMTU != nil {
		h.tracer.UpdatedMTU(size, h.done())
	}
	h.mtuIncreased(size)
}

func (h *mtuFinderAckHandler) OnLost(wire.Frame) {
	size := h.inFlight
	if size == protocol.InvalidByteCount {
		panic("OnLost callback called although there's no MTU probe packet in flight")
	}
	h.lastProbeWasLost = true
	h.inFlight = protocol.InvalidByteCount
	for i, v := range h.lost {
		if size < v {
			copy(h.lost[i+1:], h.lost[i:])
			h.lost[i] = size
			break
		}
	}
}
