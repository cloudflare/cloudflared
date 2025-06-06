package ackhandler

import (
	"slices"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

// interval is an interval from one PacketNumber to the other
type interval struct {
	Start protocol.PacketNumber
	End   protocol.PacketNumber
}

// The receivedPacketHistory stores if a packet number has already been received.
// It generates ACK ranges which can be used to assemble an ACK frame.
// It does not store packet contents.
type receivedPacketHistory struct {
	ranges []interval // maximum length: protocol.MaxNumAckRanges

	deletedBelow protocol.PacketNumber
}

func newReceivedPacketHistory() *receivedPacketHistory {
	return &receivedPacketHistory{}
}

// ReceivedPacket registers a packet with PacketNumber p and updates the ranges
func (h *receivedPacketHistory) ReceivedPacket(p protocol.PacketNumber) bool /* is a new packet (and not a duplicate / delayed packet) */ {
	// ignore delayed packets, if we already deleted the range
	if p < h.deletedBelow {
		return false
	}

	isNew := h.addToRanges(p)
	// Delete old ranges, if we're tracking too many of them.
	// This is a DoS defense against a peer that sends us too many gaps.
	if len(h.ranges) > protocol.MaxNumAckRanges {
		h.ranges = slices.Delete(h.ranges, 0, len(h.ranges)-protocol.MaxNumAckRanges)
	}
	return isNew
}

func (h *receivedPacketHistory) addToRanges(p protocol.PacketNumber) bool /* is a new packet (and not a duplicate / delayed packet) */ {
	if len(h.ranges) == 0 {
		h.ranges = append(h.ranges, interval{Start: p, End: p})
		return true
	}

	for i := len(h.ranges) - 1; i >= 0; i-- {
		// p already included in an existing range. Nothing to do here
		if p >= h.ranges[i].Start && p <= h.ranges[i].End {
			return false
		}

		if h.ranges[i].End == p-1 { // extend a range at the end
			h.ranges[i].End = p
			return true
		}
		if h.ranges[i].Start == p+1 { // extend a range at the beginning
			h.ranges[i].Start = p

			if i > 0 && h.ranges[i-1].End+1 == h.ranges[i].Start { // merge two ranges
				h.ranges[i-1].End = h.ranges[i].End
				h.ranges = slices.Delete(h.ranges, i, i+1)
			}
			return true
		}

		// create a new range after the current one
		if p > h.ranges[i].End {
			h.ranges = slices.Insert(h.ranges, i+1, interval{Start: p, End: p})
			return true
		}
	}

	// create a new range at the beginning
	h.ranges = slices.Insert(h.ranges, 0, interval{Start: p, End: p})
	return true
}

// DeleteBelow deletes all entries below (but not including) p
func (h *receivedPacketHistory) DeleteBelow(p protocol.PacketNumber) {
	if p < h.deletedBelow {
		return
	}
	h.deletedBelow = p

	if len(h.ranges) == 0 {
		return
	}

	idx := -1
	for i := 0; i < len(h.ranges); i++ {
		if h.ranges[i].End < p { // delete a whole range
			idx = i
		} else if p > h.ranges[i].Start && p <= h.ranges[i].End {
			h.ranges[i].Start = p
			break
		} else { // no ranges affected. Nothing to do
			break
		}
	}
	if idx >= 0 {
		h.ranges = slices.Delete(h.ranges, 0, idx+1)
	}
}

// AppendAckRanges appends to a slice of all AckRanges that can be used in an AckFrame
func (h *receivedPacketHistory) AppendAckRanges(ackRanges []wire.AckRange) []wire.AckRange {
	for i := len(h.ranges) - 1; i >= 0; i-- {
		ackRanges = append(ackRanges, wire.AckRange{Smallest: h.ranges[i].Start, Largest: h.ranges[i].End})
	}
	return ackRanges
}

func (h *receivedPacketHistory) GetHighestAckRange() wire.AckRange {
	ackRange := wire.AckRange{}
	if len(h.ranges) > 0 {
		ackRange.Smallest = h.ranges[len(h.ranges)-1].Start
		ackRange.Largest = h.ranges[len(h.ranges)-1].End
	}
	return ackRange
}

func (h *receivedPacketHistory) IsPotentiallyDuplicate(p protocol.PacketNumber) bool {
	if p < h.deletedBelow {
		return true
	}
	// Iterating over the slices is faster than using a binary search (using slices.BinarySearchFunc).
	for i := len(h.ranges) - 1; i >= 0; i-- {
		if p > h.ranges[i].End {
			return false
		}
		if p <= h.ranges[i].End && p >= h.ranges[i].Start {
			return true
		}
	}
	return false
}
