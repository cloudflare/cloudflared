package connection

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/ui"
	"github.com/cloudflare/cloudflared/logger"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

type Observer struct {
	logger.Service
	metrics         *tunnelMetrics
	tunnelEventChan chan<- ui.TunnelEvent
}

func NewObserver(logger logger.Service, tunnelEventChan chan<- ui.TunnelEvent) *Observer {
	return &Observer{
		logger,
		newTunnelMetrics(),
		tunnelEventChan,
	}
}

func (o *Observer) logServerInfo(connIndex uint8, location, msg string) {
	// If launch-ui flag is set, send connect msg
	if o.tunnelEventChan != nil {
		o.tunnelEventChan <- ui.TunnelEvent{Index: connIndex, EventType: ui.Connected, Location: location}
	}
	o.Infof(msg)
	o.metrics.registerServerLocation(uint8ToString(connIndex), location)
}

func (o *Observer) logTrialHostname(registration *tunnelpogs.TunnelRegistration) error {
	// Print out the user's trial zone URL in a nice box (if they requested and got one and UI flag is not set)
	if o.tunnelEventChan == nil {
		if registrationURL, err := url.Parse(registration.Url); err == nil {
			for _, line := range asciiBox(trialZoneMsg(registrationURL.String()), 2) {
				o.Info(line)
			}
		} else {
			o.Error("Failed to connect tunnel, please try again.")
			return fmt.Errorf("empty URL in response from Cloudflare edge")
		}
	}
	return nil
}

// Print out the given lines in a nice ASCII box.
func asciiBox(lines []string, padding int) (box []string) {
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

func trialZoneMsg(url string) []string {
	return []string{
		"Your free tunnel has started! Visit it:",
		"  " + url,
	}
}

func (o *Observer) sendRegisteringEvent() {
	if o.tunnelEventChan != nil {
		o.tunnelEventChan <- ui.TunnelEvent{EventType: ui.RegisteringTunnel}
	}
}

func (o *Observer) sendConnectedEvent(connIndex uint8, location string) {
	if o.tunnelEventChan != nil {
		o.tunnelEventChan <- ui.TunnelEvent{Index: connIndex, EventType: ui.Connected, Location: location}
	}
}

func (o *Observer) sendURL(url string) {
	if o.tunnelEventChan != nil {
		o.tunnelEventChan <- ui.TunnelEvent{EventType: ui.SetUrl, Url: url}
	}
}
