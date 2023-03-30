package management

// LogEventType is the way that logging messages are able to be filtered.
// Example: assigning LogEventType.Cloudflared to a zerolog event will allow the client to filter for only
// the Cloudflared-related events.
type LogEventType int

const (
	Cloudflared LogEventType = 0
	HTTP        LogEventType = 1
	TCP         LogEventType = 2
	UDP         LogEventType = 3
)

func (l LogEventType) String() string {
	switch l {
	case Cloudflared:
		return "cloudflared"
	case HTTP:
		return "http"
	case TCP:
		return "tcp"
	case UDP:
		return "udp"
	default:
		return ""
	}
}

// LogLevel corresponds to the zerolog logging levels
// "panic", "fatal", and "trace" are exempt from this list as they are rarely used and, at least
// the the first two are limited to failure conditions that lead to cloudflared shutting down.
type LogLevel string

const (
	Debug LogLevel = "debug"
	Info  LogLevel = "info"
	Warn  LogLevel = "warn"
	Error LogLevel = "error"
)
