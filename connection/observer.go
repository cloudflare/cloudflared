package connection

import (
	"net"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/management"
)

const (
	LogFieldConnectionID      = "connection"
	LogFieldLocation          = "location"
	LogFieldIPAddress         = "ip"
	LogFieldProtocol          = "protocol"
	observerChannelBufferSize = 16
)

type Observer struct {
	log             *zerolog.Logger
	metrics         *tunnelMetrics
	tunnelEventChan chan Event

	sinksMu sync.RWMutex
	sinks   []EventSink
}

type EventSink interface {
	OnTunnelEvent(event Event)
}

func NewObserver(log *zerolog.Logger) *Observer {
	o := &Observer{
		log:             log,
		metrics:         newTunnelMetrics(),
		tunnelEventChan: make(chan Event, observerChannelBufferSize),
	}
	go o.dispatchEvents()
	return o
}

// RegisterSink registers sink and returns only once the sink is visible to
// event dispatching, so it cannot miss events sent after RegisterSink returns.
func (o *Observer) RegisterSink(sink EventSink) {
	o.sinksMu.Lock()
	defer o.sinksMu.Unlock()
	o.sinks = append(o.sinks, sink)
}

func (o *Observer) logConnecting(connIndex uint8, address net.IP, protocol Protocol) {
	o.log.Debug().
		Int(management.EventTypeKey, int(management.Cloudflared)).
		Uint8(LogFieldConnIndex, connIndex).
		IPAddr(LogFieldIPAddress, address).
		Str(LogFieldProtocol, protocol.String()).
		Msg("Registering tunnel connection")
}

func (o *Observer) logConnected(connectionID uuid.UUID, connIndex uint8, location string, address net.IP, protocol Protocol) {
	o.log.Info().
		Int(management.EventTypeKey, int(management.Cloudflared)).
		Str(LogFieldConnectionID, connectionID.String()).
		Uint8(LogFieldConnIndex, connIndex).
		Str(LogFieldLocation, location).
		IPAddr(LogFieldIPAddress, address).
		Str(LogFieldProtocol, protocol.String()).
		Msg("Registered tunnel connection")
	o.metrics.registerServerLocation(uint8ToString(connIndex), location)
}

func (o *Observer) sendRegisteringEvent(connIndex uint8) {
	o.sendEvent(Event{Index: connIndex, EventType: RegisteringTunnel})
}

func (o *Observer) sendConnectedEvent(connIndex uint8, protocol Protocol, location string, edgeAddress net.IP) {
	o.sendEvent(Event{Index: connIndex, EventType: Connected, Protocol: protocol, Location: location, EdgeAddress: edgeAddress})
}

func (o *Observer) SendURL(url string) {
	o.sendEvent(Event{EventType: SetURL, URL: url})

	if !strings.HasPrefix(url, "https://") {
		// We add https:// in the prefix for backwards compatibility as we used to do that with the old free tunnels
		// and some tools (like `wrangler tail`) are regexp-ing for that specifically.
		url = "https://" + url
	}
	o.metrics.userHostnamesCounts.WithLabelValues(url).Inc()
}

func (o *Observer) SendReconnect(connIndex uint8) {
	o.sendEvent(Event{Index: connIndex, EventType: Reconnecting})
}

func (o *Observer) sendUnregisteringEvent(connIndex uint8) {
	o.sendEvent(Event{Index: connIndex, EventType: Unregistering})
}

func (o *Observer) SendDisconnect(connIndex uint8) {
	o.sendEvent(Event{Index: connIndex, EventType: Disconnected})
}

func (o *Observer) sendEvent(e Event) {
	select {
	case o.tunnelEventChan <- e:
		break
	default:
		o.log.Warn().Msg("observer channel buffer is full")
	}
}

func (o *Observer) dispatchEvents() {
	for evt := range o.tunnelEventChan {
		o.notifySinks(evt)
	}
}

// notifySinks delivers evt to a snapshot of the registered sinks, so a slow or
// re-entrant sink callback cannot block sink registration.
func (o *Observer) notifySinks(evt Event) {
	o.sinksMu.RLock()
	sinks := slices.Clone(o.sinks)
	o.sinksMu.RUnlock()
	for _, sink := range sinks {
		sink.OnTunnelEvent(evt)
	}
}

type EventSinkFunc func(event Event)

func (f EventSinkFunc) OnTunnelEvent(event Event) {
	f(event)
}
