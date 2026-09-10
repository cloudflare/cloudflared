package connection

import (
	"net"
	"strings"

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
	logTransport    *zerolog.Logger
	metrics         *tunnelMetrics
	tunnelEventChan chan Event
	addSinkChan     chan EventSink
}

type EventSink interface {
	OnTunnelEvent(event Event)
}

func NewObserver(log, logTransport *zerolog.Logger) *Observer {
	o := &Observer{
		log:             log,
		logTransport:    logTransport,
		metrics:         newTunnelMetrics(),
		tunnelEventChan: make(chan Event, observerChannelBufferSize),
		addSinkChan:     make(chan EventSink, observerChannelBufferSize),
	}
	go o.dispatchEvents()
	return o
}

func (o *Observer) RegisterSink(sink EventSink) {
	o.addSinkChan <- sink
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
	var sinks []EventSink
	for {
		select {
		case sink := <-o.addSinkChan:
			sinks = append(sinks, sink)
		case evt := <-o.tunnelEventChan:
			for _, sink := range sinks {
				sink.OnTunnelEvent(evt)
			}
		}
	}
}

type EventSinkFunc func(event Event)

func (f EventSinkFunc) OnTunnelEvent(event Event) {
	f(event)
}
