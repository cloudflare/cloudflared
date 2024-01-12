package congestion

import (
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
)

const maxBurstSizePackets = 10

// The pacer implements a token bucket pacing algorithm.
type pacer struct {
	budgetAtLastSent  protocol.ByteCount
	maxDatagramSize   protocol.ByteCount
	lastSentTime      time.Time
	adjustedBandwidth func() uint64 // in bytes/s
}

func newPacer(getBandwidth func() Bandwidth) *pacer {
	p := &pacer{
		maxDatagramSize: initialMaxDatagramSize,
		adjustedBandwidth: func() uint64 {
			// Bandwidth is in bits/s. We need the value in bytes/s.
			bw := uint64(getBandwidth() / BytesPerSecond)
			// Use a slightly higher value than the actual measured bandwidth.
			// RTT variations then won't result in under-utilization of the congestion window.
			// Ultimately, this will result in sending packets as acknowledgments are received rather than when timers fire,
			// provided the congestion window is fully utilized and acknowledgments arrive at regular intervals.
			return bw * 5 / 4
		},
	}
	p.budgetAtLastSent = p.maxBurstSize()
	return p
}

func (p *pacer) SentPacket(sendTime time.Time, size protocol.ByteCount) {
	budget := p.Budget(sendTime)
	if size >= budget {
		p.budgetAtLastSent = 0
	} else {
		p.budgetAtLastSent = budget - size
	}
	p.lastSentTime = sendTime
}

func (p *pacer) Budget(now time.Time) protocol.ByteCount {
	if p.lastSentTime.IsZero() {
		return p.maxBurstSize()
	}
	budget := p.budgetAtLastSent + (protocol.ByteCount(p.adjustedBandwidth())*protocol.ByteCount(now.Sub(p.lastSentTime).Nanoseconds()))/1e9
	if budget < 0 { // protect against overflows
		budget = protocol.MaxByteCount
	}
	return min(p.maxBurstSize(), budget)
}

func (p *pacer) maxBurstSize() protocol.ByteCount {
	return max(
		protocol.ByteCount(uint64((protocol.MinPacingDelay+protocol.TimerGranularity).Nanoseconds())*p.adjustedBandwidth())/1e9,
		maxBurstSizePackets*p.maxDatagramSize,
	)
}

// TimeUntilSend returns when the next packet should be sent.
// It returns the zero value of time.Time if a packet can be sent immediately.
func (p *pacer) TimeUntilSend() time.Time {
	if p.budgetAtLastSent >= p.maxDatagramSize {
		return time.Time{}
	}
	diff := 1e9 * uint64(p.maxDatagramSize-p.budgetAtLastSent)
	bw := p.adjustedBandwidth()
	// We might need to round up this value.
	// Otherwise, we might have a budget (slightly) smaller than the datagram size when the timer expires.
	d := diff / bw
	// this is effectively a math.Ceil, but using only integer math
	if diff%bw > 0 {
		d++
	}
	return p.lastSentTime.Add(max(protocol.MinPacingDelay, time.Duration(d)*time.Nanosecond))
}

func (p *pacer) SetMaxDatagramSize(s protocol.ByteCount) {
	p.maxDatagramSize = s
}
