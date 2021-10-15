package qlog

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"runtime/debug"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"
	"github.com/lucas-clemente/quic-go/logging"

	"github.com/francoispqt/gojay"
)

// Setting of this only works when quic-go is used as a library.
// When building a binary from this repository, the version can be set using the following go build flag:
// -ldflags="-X github.com/lucas-clemente/quic-go/qlog.quicGoVersion=foobar"
var quicGoVersion = "(devel)"

func init() {
	if quicGoVersion != "(devel)" { // variable set by ldflags
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok { // no build info available. This happens when quic-go is not used as a library.
		return
	}
	for _, d := range info.Deps {
		if d.Path == "github.com/lucas-clemente/quic-go" {
			quicGoVersion = d.Version
			if d.Replace != nil {
				if len(d.Replace.Version) > 0 {
					quicGoVersion = d.Version
				} else {
					quicGoVersion += " (replaced)"
				}
			}
			break
		}
	}
}

const eventChanSize = 50

type tracer struct {
	getLogWriter func(p logging.Perspective, connectionID []byte) io.WriteCloser
}

var _ logging.Tracer = &tracer{}

// NewTracer creates a new qlog tracer.
func NewTracer(getLogWriter func(p logging.Perspective, connectionID []byte) io.WriteCloser) logging.Tracer {
	return &tracer{getLogWriter: getLogWriter}
}

func (t *tracer) TracerForConnection(_ context.Context, p logging.Perspective, odcid protocol.ConnectionID) logging.ConnectionTracer {
	if w := t.getLogWriter(p, odcid.Bytes()); w != nil {
		return NewConnectionTracer(w, p, odcid)
	}
	return nil
}

func (t *tracer) SentPacket(net.Addr, *logging.Header, protocol.ByteCount, []logging.Frame) {}
func (t *tracer) DroppedPacket(net.Addr, logging.PacketType, protocol.ByteCount, logging.PacketDropReason) {
}

type connectionTracer struct {
	mutex sync.Mutex

	w             io.WriteCloser
	odcid         protocol.ConnectionID
	perspective   protocol.Perspective
	referenceTime time.Time

	events     chan event
	encodeErr  error
	runStopped chan struct{}

	lastMetrics *metrics
}

var _ logging.ConnectionTracer = &connectionTracer{}

// NewConnectionTracer creates a new tracer to record a qlog for a connection.
func NewConnectionTracer(w io.WriteCloser, p protocol.Perspective, odcid protocol.ConnectionID) logging.ConnectionTracer {
	t := &connectionTracer{
		w:             w,
		perspective:   p,
		odcid:         odcid,
		runStopped:    make(chan struct{}),
		events:        make(chan event, eventChanSize),
		referenceTime: time.Now(),
	}
	go t.run()
	return t
}

func (t *connectionTracer) run() {
	defer close(t.runStopped)
	buf := &bytes.Buffer{}
	enc := gojay.NewEncoder(buf)
	tl := &topLevel{
		trace: trace{
			VantagePoint: vantagePoint{Type: t.perspective},
			CommonFields: commonFields{
				ODCID:         connectionID(t.odcid),
				GroupID:       connectionID(t.odcid),
				ReferenceTime: t.referenceTime,
			},
		},
	}
	if err := enc.Encode(tl); err != nil {
		panic(fmt.Sprintf("qlog encoding into a bytes.Buffer failed: %s", err))
	}
	if err := buf.WriteByte('\n'); err != nil {
		panic(fmt.Sprintf("qlog encoding into a bytes.Buffer failed: %s", err))
	}
	if _, err := t.w.Write(buf.Bytes()); err != nil {
		t.encodeErr = err
	}
	enc = gojay.NewEncoder(t.w)
	for ev := range t.events {
		if t.encodeErr != nil { // if encoding failed, just continue draining the event channel
			continue
		}
		if err := enc.Encode(ev); err != nil {
			t.encodeErr = err
			continue
		}
		if _, err := t.w.Write([]byte{'\n'}); err != nil {
			t.encodeErr = err
		}
	}
}

func (t *connectionTracer) Close() {
	if err := t.export(); err != nil {
		log.Printf("exporting qlog failed: %s\n", err)
	}
}

// export writes a qlog.
func (t *connectionTracer) export() error {
	close(t.events)
	<-t.runStopped
	if t.encodeErr != nil {
		return t.encodeErr
	}
	return t.w.Close()
}

func (t *connectionTracer) recordEvent(eventTime time.Time, details eventDetails) {
	t.events <- event{
		RelativeTime: eventTime.Sub(t.referenceTime),
		eventDetails: details,
	}
}

func (t *connectionTracer) StartedConnection(local, remote net.Addr, srcConnID, destConnID protocol.ConnectionID) {
	// ignore this event if we're not dealing with UDP addresses here
	localAddr, ok := local.(*net.UDPAddr)
	if !ok {
		return
	}
	remoteAddr, ok := remote.(*net.UDPAddr)
	if !ok {
		return
	}
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventConnectionStarted{
		SrcAddr:          localAddr,
		DestAddr:         remoteAddr,
		SrcConnectionID:  srcConnID,
		DestConnectionID: destConnID,
	})
	t.mutex.Unlock()
}

func (t *connectionTracer) NegotiatedVersion(chosen logging.VersionNumber, client, server []logging.VersionNumber) {
	var clientVersions, serverVersions []versionNumber
	if len(client) > 0 {
		clientVersions = make([]versionNumber, len(client))
		for i, v := range client {
			clientVersions[i] = versionNumber(v)
		}
	}
	if len(server) > 0 {
		serverVersions = make([]versionNumber, len(server))
		for i, v := range server {
			serverVersions[i] = versionNumber(v)
		}
	}
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventVersionNegotiated{
		clientVersions: clientVersions,
		serverVersions: serverVersions,
		chosenVersion:  versionNumber(chosen),
	})
	t.mutex.Unlock()
}

func (t *connectionTracer) ClosedConnection(e error) {
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventConnectionClosed{e: e})
	t.mutex.Unlock()
}

func (t *connectionTracer) SentTransportParameters(tp *wire.TransportParameters) {
	t.recordTransportParameters(t.perspective, tp)
}

func (t *connectionTracer) ReceivedTransportParameters(tp *wire.TransportParameters) {
	t.recordTransportParameters(t.perspective.Opposite(), tp)
}

func (t *connectionTracer) RestoredTransportParameters(tp *wire.TransportParameters) {
	ev := t.toTransportParameters(tp)
	ev.Restore = true

	t.mutex.Lock()
	t.recordEvent(time.Now(), ev)
	t.mutex.Unlock()
}

func (t *connectionTracer) recordTransportParameters(sentBy protocol.Perspective, tp *wire.TransportParameters) {
	ev := t.toTransportParameters(tp)
	ev.Owner = ownerLocal
	if sentBy != t.perspective {
		ev.Owner = ownerRemote
	}
	ev.SentBy = sentBy

	t.mutex.Lock()
	t.recordEvent(time.Now(), ev)
	t.mutex.Unlock()
}

func (t *connectionTracer) toTransportParameters(tp *wire.TransportParameters) *eventTransportParameters {
	var pa *preferredAddress
	if tp.PreferredAddress != nil {
		pa = &preferredAddress{
			IPv4:                tp.PreferredAddress.IPv4,
			PortV4:              tp.PreferredAddress.IPv4Port,
			IPv6:                tp.PreferredAddress.IPv6,
			PortV6:              tp.PreferredAddress.IPv6Port,
			ConnectionID:        tp.PreferredAddress.ConnectionID,
			StatelessResetToken: tp.PreferredAddress.StatelessResetToken,
		}
	}
	return &eventTransportParameters{
		OriginalDestinationConnectionID: tp.OriginalDestinationConnectionID,
		InitialSourceConnectionID:       tp.InitialSourceConnectionID,
		RetrySourceConnectionID:         tp.RetrySourceConnectionID,
		StatelessResetToken:             tp.StatelessResetToken,
		DisableActiveMigration:          tp.DisableActiveMigration,
		MaxIdleTimeout:                  tp.MaxIdleTimeout,
		MaxUDPPayloadSize:               tp.MaxUDPPayloadSize,
		AckDelayExponent:                tp.AckDelayExponent,
		MaxAckDelay:                     tp.MaxAckDelay,
		ActiveConnectionIDLimit:         tp.ActiveConnectionIDLimit,
		InitialMaxData:                  tp.InitialMaxData,
		InitialMaxStreamDataBidiLocal:   tp.InitialMaxStreamDataBidiLocal,
		InitialMaxStreamDataBidiRemote:  tp.InitialMaxStreamDataBidiRemote,
		InitialMaxStreamDataUni:         tp.InitialMaxStreamDataUni,
		InitialMaxStreamsBidi:           int64(tp.MaxBidiStreamNum),
		InitialMaxStreamsUni:            int64(tp.MaxUniStreamNum),
		PreferredAddress:                pa,
		MaxDatagramFrameSize:            tp.MaxDatagramFrameSize,
	}
}

func (t *connectionTracer) SentPacket(hdr *wire.ExtendedHeader, packetSize logging.ByteCount, ack *logging.AckFrame, frames []logging.Frame) {
	numFrames := len(frames)
	if ack != nil {
		numFrames++
	}
	fs := make([]frame, 0, numFrames)
	if ack != nil {
		fs = append(fs, frame{Frame: ack})
	}
	for _, f := range frames {
		fs = append(fs, frame{Frame: f})
	}
	header := *transformExtendedHeader(hdr)
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventPacketSent{
		Header:        header,
		Length:        packetSize,
		PayloadLength: hdr.Length,
		Frames:        fs,
	})
	t.mutex.Unlock()
}

func (t *connectionTracer) ReceivedPacket(hdr *wire.ExtendedHeader, packetSize logging.ByteCount, frames []logging.Frame) {
	fs := make([]frame, len(frames))
	for i, f := range frames {
		fs[i] = frame{Frame: f}
	}
	header := *transformExtendedHeader(hdr)
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventPacketReceived{
		Header:        header,
		Length:        packetSize,
		PayloadLength: hdr.Length,
		Frames:        fs,
	})
	t.mutex.Unlock()
}

func (t *connectionTracer) ReceivedRetry(hdr *wire.Header) {
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventRetryReceived{
		Header: *transformHeader(hdr),
	})
	t.mutex.Unlock()
}

func (t *connectionTracer) ReceivedVersionNegotiationPacket(hdr *wire.Header, versions []logging.VersionNumber) {
	ver := make([]versionNumber, len(versions))
	for i, v := range versions {
		ver[i] = versionNumber(v)
	}
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventVersionNegotiationReceived{
		Header:            *transformHeader(hdr),
		SupportedVersions: ver,
	})
	t.mutex.Unlock()
}

func (t *connectionTracer) BufferedPacket(pt logging.PacketType) {
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventPacketBuffered{PacketType: pt})
	t.mutex.Unlock()
}

func (t *connectionTracer) DroppedPacket(pt logging.PacketType, size protocol.ByteCount, reason logging.PacketDropReason) {
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventPacketDropped{
		PacketType: pt,
		PacketSize: size,
		Trigger:    packetDropReason(reason),
	})
	t.mutex.Unlock()
}

func (t *connectionTracer) UpdatedMetrics(rttStats *utils.RTTStats, cwnd, bytesInFlight protocol.ByteCount, packetsInFlight int) {
	m := &metrics{
		MinRTT:           rttStats.MinRTT(),
		SmoothedRTT:      rttStats.SmoothedRTT(),
		LatestRTT:        rttStats.LatestRTT(),
		RTTVariance:      rttStats.MeanDeviation(),
		CongestionWindow: cwnd,
		BytesInFlight:    bytesInFlight,
		PacketsInFlight:  packetsInFlight,
	}
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventMetricsUpdated{
		Last:    t.lastMetrics,
		Current: m,
	})
	t.lastMetrics = m
	t.mutex.Unlock()
}

func (t *connectionTracer) AcknowledgedPacket(protocol.EncryptionLevel, protocol.PacketNumber) {}

func (t *connectionTracer) LostPacket(encLevel protocol.EncryptionLevel, pn protocol.PacketNumber, lossReason logging.PacketLossReason) {
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventPacketLost{
		PacketType:   getPacketTypeFromEncryptionLevel(encLevel),
		PacketNumber: pn,
		Trigger:      packetLossReason(lossReason),
	})
	t.mutex.Unlock()
}

func (t *connectionTracer) UpdatedCongestionState(state logging.CongestionState) {
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventCongestionStateUpdated{state: congestionState(state)})
	t.mutex.Unlock()
}

func (t *connectionTracer) UpdatedPTOCount(value uint32) {
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventUpdatedPTO{Value: value})
	t.mutex.Unlock()
}

func (t *connectionTracer) UpdatedKeyFromTLS(encLevel protocol.EncryptionLevel, pers protocol.Perspective) {
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventKeyUpdated{
		Trigger: keyUpdateTLS,
		KeyType: encLevelToKeyType(encLevel, pers),
	})
	t.mutex.Unlock()
}

func (t *connectionTracer) UpdatedKey(generation protocol.KeyPhase, remote bool) {
	trigger := keyUpdateLocal
	if remote {
		trigger = keyUpdateRemote
	}
	t.mutex.Lock()
	now := time.Now()
	t.recordEvent(now, &eventKeyUpdated{
		Trigger:    trigger,
		KeyType:    keyTypeClient1RTT,
		Generation: generation,
	})
	t.recordEvent(now, &eventKeyUpdated{
		Trigger:    trigger,
		KeyType:    keyTypeServer1RTT,
		Generation: generation,
	})
	t.mutex.Unlock()
}

func (t *connectionTracer) DroppedEncryptionLevel(encLevel protocol.EncryptionLevel) {
	t.mutex.Lock()
	now := time.Now()
	if encLevel == protocol.Encryption0RTT {
		t.recordEvent(now, &eventKeyRetired{KeyType: encLevelToKeyType(encLevel, t.perspective)})
	} else {
		t.recordEvent(now, &eventKeyRetired{KeyType: encLevelToKeyType(encLevel, protocol.PerspectiveServer)})
		t.recordEvent(now, &eventKeyRetired{KeyType: encLevelToKeyType(encLevel, protocol.PerspectiveClient)})
	}
	t.mutex.Unlock()
}

func (t *connectionTracer) DroppedKey(generation protocol.KeyPhase) {
	t.mutex.Lock()
	now := time.Now()
	t.recordEvent(now, &eventKeyRetired{
		KeyType:    encLevelToKeyType(protocol.Encryption1RTT, protocol.PerspectiveServer),
		Generation: generation,
	})
	t.recordEvent(now, &eventKeyRetired{
		KeyType:    encLevelToKeyType(protocol.Encryption1RTT, protocol.PerspectiveClient),
		Generation: generation,
	})
	t.mutex.Unlock()
}

func (t *connectionTracer) SetLossTimer(tt logging.TimerType, encLevel protocol.EncryptionLevel, timeout time.Time) {
	t.mutex.Lock()
	now := time.Now()
	t.recordEvent(now, &eventLossTimerSet{
		TimerType: timerType(tt),
		EncLevel:  encLevel,
		Delta:     timeout.Sub(now),
	})
	t.mutex.Unlock()
}

func (t *connectionTracer) LossTimerExpired(tt logging.TimerType, encLevel protocol.EncryptionLevel) {
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventLossTimerExpired{
		TimerType: timerType(tt),
		EncLevel:  encLevel,
	})
	t.mutex.Unlock()
}

func (t *connectionTracer) LossTimerCanceled() {
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventLossTimerCanceled{})
	t.mutex.Unlock()
}

func (t *connectionTracer) Debug(name, msg string) {
	t.mutex.Lock()
	t.recordEvent(time.Now(), &eventGeneric{
		name: name,
		msg:  msg,
	})
	t.mutex.Unlock()
}
