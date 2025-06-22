package ackhandler

import (
	"fmt"
	"iter"

	"github.com/quic-go/quic-go/internal/protocol"
)

type sentPacketHistory struct {
	packets          []*packet
	pathProbePackets []*packet

	numOutstanding int

	highestPacketNumber protocol.PacketNumber
}

func newSentPacketHistory(isAppData bool) *sentPacketHistory {
	h := &sentPacketHistory{
		highestPacketNumber: protocol.InvalidPacketNumber,
	}
	if isAppData {
		h.packets = make([]*packet, 0, 32)
	} else {
		h.packets = make([]*packet, 0, 6)
	}
	return h
}

func (h *sentPacketHistory) checkSequentialPacketNumberUse(pn protocol.PacketNumber) {
	if h.highestPacketNumber != protocol.InvalidPacketNumber {
		if pn != h.highestPacketNumber+1 {
			panic("non-sequential packet number use")
		}
	}
	h.highestPacketNumber = pn
}

func (h *sentPacketHistory) SkippedPacket(pn protocol.PacketNumber) {
	h.checkSequentialPacketNumberUse(pn)
	h.packets = append(h.packets, &packet{
		PacketNumber:  pn,
		skippedPacket: true,
	})
}

func (h *sentPacketHistory) SentNonAckElicitingPacket(pn protocol.PacketNumber) {
	h.checkSequentialPacketNumberUse(pn)
	if len(h.packets) > 0 {
		h.packets = append(h.packets, nil)
	}
}

func (h *sentPacketHistory) SentAckElicitingPacket(p *packet) {
	h.checkSequentialPacketNumberUse(p.PacketNumber)
	h.packets = append(h.packets, p)
	if p.outstanding() {
		h.numOutstanding++
	}
}

func (h *sentPacketHistory) SentPathProbePacket(p *packet) {
	h.checkSequentialPacketNumberUse(p.PacketNumber)
	h.packets = append(h.packets, &packet{
		PacketNumber:      p.PacketNumber,
		isPathProbePacket: true,
	})
	h.pathProbePackets = append(h.pathProbePackets, p)
}

func (h *sentPacketHistory) Packets() iter.Seq[*packet] {
	return func(yield func(*packet) bool) {
		for _, p := range h.packets {
			if p == nil {
				continue
			}
			if !yield(p) {
				return
			}
		}
	}
}

func (h *sentPacketHistory) PathProbes() iter.Seq[*packet] {
	return func(yield func(*packet) bool) {
		for _, p := range h.pathProbePackets {
			if !yield(p) {
				return
			}
		}
	}
}

// FirstOutstanding returns the first outstanding packet.
func (h *sentPacketHistory) FirstOutstanding() *packet {
	if !h.HasOutstandingPackets() {
		return nil
	}
	for _, p := range h.packets {
		if p != nil && p.outstanding() {
			return p
		}
	}
	return nil
}

// FirstOutstandingPathProbe returns the first outstanding path probe packet
func (h *sentPacketHistory) FirstOutstandingPathProbe() *packet {
	if len(h.pathProbePackets) == 0 {
		return nil
	}
	return h.pathProbePackets[0]
}

func (h *sentPacketHistory) Len() int {
	return len(h.packets)
}

func (h *sentPacketHistory) Remove(pn protocol.PacketNumber) error {
	idx, ok := h.getIndex(pn)
	if !ok {
		return fmt.Errorf("packet %d not found in sent packet history", pn)
	}
	p := h.packets[idx]
	if p.outstanding() {
		h.numOutstanding--
		if h.numOutstanding < 0 {
			panic("negative number of outstanding packets")
		}
	}
	h.packets[idx] = nil
	// clean up all skipped packets directly before this packet number
	for idx > 0 {
		idx--
		p := h.packets[idx]
		if p == nil || !p.skippedPacket {
			break
		}
		h.packets[idx] = nil
	}
	if idx == 0 {
		h.cleanupStart()
	}
	if len(h.packets) > 0 && h.packets[0] == nil {
		panic("remove failed")
	}
	return nil
}

// RemovePathProbe removes a path probe packet.
// It scales O(N), but that's ok, since we don't expect to send many path probe packets.
// It is not valid to call this function in IteratePathProbes.
func (h *sentPacketHistory) RemovePathProbe(pn protocol.PacketNumber) *packet {
	var packetToDelete *packet
	idx := -1
	for i, p := range h.pathProbePackets {
		if p.PacketNumber == pn {
			packetToDelete = p
			idx = i
			break
		}
	}
	if idx != -1 {
		// don't use slices.Delete, because it zeros the deleted element
		copy(h.pathProbePackets[idx:], h.pathProbePackets[idx+1:])
		h.pathProbePackets = h.pathProbePackets[:len(h.pathProbePackets)-1]
	}
	return packetToDelete
}

// getIndex gets the index of packet p in the packets slice.
func (h *sentPacketHistory) getIndex(p protocol.PacketNumber) (int, bool) {
	if len(h.packets) == 0 {
		return 0, false
	}
	first := h.packets[0].PacketNumber
	if p < first {
		return 0, false
	}
	index := int(p - first)
	if index > len(h.packets)-1 {
		return 0, false
	}
	return index, true
}

func (h *sentPacketHistory) HasOutstandingPackets() bool {
	return h.numOutstanding > 0
}

func (h *sentPacketHistory) HasOutstandingPathProbes() bool {
	return len(h.pathProbePackets) > 0
}

// delete all nil entries at the beginning of the packets slice
func (h *sentPacketHistory) cleanupStart() {
	for i, p := range h.packets {
		if p != nil {
			h.packets = h.packets[i:]
			return
		}
	}
	h.packets = h.packets[:0]
}

func (h *sentPacketHistory) LowestPacketNumber() protocol.PacketNumber {
	if len(h.packets) == 0 {
		return protocol.InvalidPacketNumber
	}
	return h.packets[0].PacketNumber
}

func (h *sentPacketHistory) DeclareLost(pn protocol.PacketNumber) {
	idx, ok := h.getIndex(pn)
	if !ok {
		return
	}
	p := h.packets[idx]
	if p.outstanding() {
		h.numOutstanding--
		if h.numOutstanding < 0 {
			panic("negative number of outstanding packets")
		}
	}
	h.packets[idx] = nil
	if idx == 0 {
		h.cleanupStart()
	}
}
