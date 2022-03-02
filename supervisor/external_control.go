package supervisor

import (
	"time"
)

type ReconnectSignal struct {
	// wait this many seconds before re-establish the connection
	Delay time.Duration
}

// Error allows us to use ReconnectSignal as a special error to force connection abort
func (r ReconnectSignal) Error() string {
	return "reconnect signal"
}

func (r ReconnectSignal) DelayBeforeReconnect() {
	if r.Delay > 0 {
		time.Sleep(r.Delay)
	}
}
