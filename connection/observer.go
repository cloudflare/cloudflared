package connection

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/rs/zerolog"

	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const (
	LogFieldLocation          = "location"
	observerChannelBufferSize = 16
)

type Observer struct {
	log             *zerolog.Logger
	logTransport    *zerolog.Logger
	metrics         *tunnelMetrics
	tunnelEventChan chan Event
	uiEnabled       bool
	addSinkChan     chan EventSink
}

type EventSink interface {
	OnTunnelEvent(event Event)
}

func NewObserver(log, logTransport *zerolog.Logger, uiEnabled bool) *Observer {
	o := &Observer{
		log:             log,
		logTransport:    logTransport,
		metrics:         newTunnelMetrics(),
		uiEnabled:       uiEnabled,
		tunnelEventChan: make(chan Event, observerChannelBufferSize),
		addSinkChan:     make(chan EventSink, observerChannelBufferSize),
	}
	go o.dispatchEvents()
	return o
}

func (o *Observer) RegisterSink(sink EventSink) {
	o.addSinkChan <- sink
}

func (o *Observer) logServerInfo(connIndex uint8, location, msg string) {
	o.sendEvent(Event{Index: connIndex, EventType: Connected, Location: location})
	o.log.Info().
		Uint8(LogFieldConnIndex, connIndex).
		Str(LogFieldLocation, location).
		Msg(msg)
	o.metrics.registerServerLocation(uint8ToString(connIndex), location)
}

func (o *Observer) logTrialHostname(registration *tunnelpogs.TunnelRegistration) error {
	// Print out the user's trial zone URL in a nice box (if they requested and got one and UI flag is not set)
	if !o.uiEnabled {
		if registrationURL, err := url.Parse(registration.Url); err == nil {
			for _, line := range AsciiBox(TrialZoneMsg(registrationURL.String()), 2) {
				o.log.Info().Msg(line)
			}
		} else {
			o.log.Error().Msg("Failed to connect tunnel, please try again.")
			return fmt.Errorf("empty URL in response from Cloudflare edge")
		}
	}
	return nil
}

// Print out the given lines in a nice ASCII box.
func AsciiBox(lines []string, padding int) (box []string) {
	maxLen := maxLen(lines)
	spacer := strings.Repeat(" ", padding)

	border := "+" + strings.Repeat("-", maxLen+(padding*2)) + "+"

	box = append(box, border)
	for _, line := range lines {
		box = append(box, "|"+spacer+line+strings.Repeat(" ", maxLen-len(line))+spacer+"|")
	}
	box = append(box, border)
	return
}

func maxLen(lines []string) int {
	max := 0
	for _, line := range lines {
		if len(line) > max {
			max = len(line)
		}
	}
	return max
}

func TrialZoneMsg(url string) []string {
	return []string{
		"Your free tunnel has started! Visit it:",
		"  " + url,
	}
}

func (o *Observer) sendRegisteringEvent(connIndex uint8) {
	o.sendEvent(Event{Index: connIndex, EventType: RegisteringTunnel})
}

func (o *Observer) sendConnectedEvent(connIndex uint8, location string) {
	o.sendEvent(Event{Index: connIndex, EventType: Connected, Location: location})
}

func (o *Observer) sendURL(url string) {
	o.sendEvent(Event{EventType: SetURL, URL: url})
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
