package management

import (
	"context"
	"errors"
	"fmt"
	"io"

	jsoniter "github.com/json-iterator/go"
	"github.com/rs/zerolog"
	"nhooyr.io/websocket"
)

var (
	errInvalidMessageType = fmt.Errorf("invalid message type was provided")
)

// ServerEventType represents the event types that can come from the server
type ServerEventType string

// ClientEventType represents the event types that can come from the client
type ClientEventType string

const (
	UnknownClientEventType ClientEventType = ""
	StartStreaming         ClientEventType = "start_streaming"
	StopStreaming          ClientEventType = "stop_streaming"

	UnknownServerEventType ServerEventType = ""
	Logs                   ServerEventType = "logs"
)

// ServerEvent is the base struct that informs, based of the Type field, which Event type was provided from the server.
type ServerEvent struct {
	Type ServerEventType `json:"type,omitempty"`
	// The raw json message is provided to allow better deserialization once the type is known
	event jsoniter.RawMessage
}

// ClientEvent is the base struct that informs, based of the Type field, which Event type was provided from the client.
type ClientEvent struct {
	Type ClientEventType `json:"type,omitempty"`
	// The raw json message is provided to allow better deserialization once the type is known
	event jsoniter.RawMessage
}

// EventStartStreaming signifies that the client wishes to start receiving log events.
// Additional filters can be provided to augment the log events requested.
type EventStartStreaming struct {
	ClientEvent
	Filters *StreamingFilters `json:"filters,omitempty"`
}

type StreamingFilters struct {
	Events   []LogEventType `json:"events,omitempty"`
	Level    *LogLevel      `json:"level,omitempty"`
	Sampling float64        `json:"sampling,omitempty"`
}

// EventStopStreaming signifies that the client wishes to halt receiving log events.
type EventStopStreaming struct {
	ClientEvent
}

// EventLog is the event that the server sends to the client with the log events.
type EventLog struct {
	ServerEvent
	Logs []*Log `json:"logs"`
}

// LogEventType is the way that logging messages are able to be filtered.
// Example: assigning LogEventType.Cloudflared to a zerolog event will allow the client to filter for only
// the Cloudflared-related events.
type LogEventType int8

const (
	// Cloudflared events are significant to cloudflared operations like connection state changes.
	// Cloudflared is also the default event type for any events that haven't been separated into a proper event type.
	Cloudflared LogEventType = iota
	HTTP
	TCP
	UDP
)

func ParseLogEventType(s string) (LogEventType, bool) {
	switch s {
	case "cloudflared":
		return Cloudflared, true
	case "http":
		return HTTP, true
	case "tcp":
		return TCP, true
	case "udp":
		return UDP, true
	}
	return -1, false
}

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

func (l LogEventType) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

func (e *LogEventType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.New("unable to unmarshal LogEventType string")
	}
	if event, ok := ParseLogEventType(s); ok {
		*e = event
		return nil
	}
	return errors.New("unable to unmarshal LogEventType")
}

// LogLevel corresponds to the zerolog logging levels
// "panic", "fatal", and "trace" are exempt from this list as they are rarely used and, at least
// the first two are limited to failure conditions that lead to cloudflared shutting down.
type LogLevel int8

const (
	Debug LogLevel = 0
	Info  LogLevel = 1
	Warn  LogLevel = 2
	Error LogLevel = 3
)

func ParseLogLevel(l string) (LogLevel, bool) {
	switch l {
	case "debug":
		return Debug, true
	case "info":
		return Info, true
	case "warn":
		return Warn, true
	case "error":
		return Error, true
	}
	return -1, false
}

func (l LogLevel) String() string {
	switch l {
	case Debug:
		return "debug"
	case Info:
		return "info"
	case Warn:
		return "warn"
	case Error:
		return "error"
	default:
		return ""
	}
}

func (l LogLevel) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

func (l *LogLevel) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.New("unable to unmarshal LogLevel string")
	}
	if level, ok := ParseLogLevel(s); ok {
		*l = level
		return nil
	}
	return fmt.Errorf("unable to unmarshal LogLevel")
}

const (
	// TimeKey aligns with the zerolog.TimeFieldName
	TimeKey = "time"
	// LevelKey aligns with the zerolog.LevelFieldName
	LevelKey = "level"
	// LevelKey aligns with the zerolog.MessageFieldName
	MessageKey = "message"
	// EventTypeKey is the custom JSON key of the LogEventType in ZeroLogEvent
	EventTypeKey = "event"
	// FieldsKey is a custom JSON key to match and store every other key for a zerolog event
	FieldsKey = "fields"
)

// Log is the basic structure of the events that are sent to the client.
type Log struct {
	Time    string                 `json:"time,omitempty"`
	Level   LogLevel               `json:"level,omitempty"`
	Message string                 `json:"message,omitempty"`
	Event   LogEventType           `json:"event,omitempty"`
	Fields  map[string]interface{} `json:"fields,omitempty"`
}

// IntoClientEvent unmarshals the provided ClientEvent into the proper type.
func IntoClientEvent[T EventStartStreaming | EventStopStreaming](e *ClientEvent, eventType ClientEventType) (*T, bool) {
	if e.Type != eventType {
		return nil, false
	}
	event := new(T)
	err := json.Unmarshal(e.event, event)
	if err != nil {
		return nil, false
	}
	return event, true
}

// IntoServerEvent unmarshals the provided ServerEvent into the proper type.
func IntoServerEvent[T EventLog](e *ServerEvent, eventType ServerEventType) (*T, bool) {
	if e.Type != eventType {
		return nil, false
	}
	event := new(T)
	err := json.Unmarshal(e.event, event)
	if err != nil {
		return nil, false
	}
	return event, true
}

// ReadEvent will read a message from the websocket connection and parse it into a valid ServerEvent.
func ReadServerEvent(c *websocket.Conn, ctx context.Context) (*ServerEvent, error) {
	message, err := readMessage(c, ctx)
	if err != nil {
		return nil, err
	}
	event := ServerEvent{}
	if err := json.Unmarshal(message, &event); err != nil {
		return nil, err
	}
	switch event.Type {
	case Logs:
		event.event = message
		return &event, nil
	case UnknownServerEventType:
		return nil, errInvalidMessageType
	default:
		return nil, fmt.Errorf("invalid server message type was provided: %s", event.Type)
	}
}

// ReadEvent will read a message from the websocket connection and parse it into a valid ClientEvent.
func ReadClientEvent(c *websocket.Conn, ctx context.Context) (*ClientEvent, error) {
	message, err := readMessage(c, ctx)
	if err != nil {
		return nil, err
	}
	event := ClientEvent{}
	if err := json.Unmarshal(message, &event); err != nil {
		return nil, err
	}
	switch event.Type {
	case StartStreaming, StopStreaming:
		event.event = message
		return &event, nil
	case UnknownClientEventType:
		return nil, errInvalidMessageType
	default:
		return nil, fmt.Errorf("invalid client message type was provided: %s", event.Type)
	}
}

// readMessage will read a message from the websocket connection and return the payload.
func readMessage(c *websocket.Conn, ctx context.Context) ([]byte, error) {
	messageType, reader, err := c.Reader(ctx)
	if err != nil {
		return nil, err
	}
	if messageType != websocket.MessageText {
		return nil, errInvalidMessageType
	}
	return io.ReadAll(reader)
}

// WriteEvent will write a Event type message to the websocket connection.
func WriteEvent(c *websocket.Conn, ctx context.Context, event any) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, payload)
}

// IsClosed returns true if the websocket error is a websocket.CloseError; returns false if not a
// websocket.CloseError
func IsClosed(err error, log *zerolog.Logger) bool {
	var closeErr websocket.CloseError
	if errors.As(err, &closeErr) {
		if closeErr.Code != websocket.StatusNormalClosure {
			log.Debug().Msgf("connection is already closed: (%d) %s", closeErr.Code, closeErr.Reason)
		}
		return true
	}
	return false
}

func AsClosed(err error) *websocket.CloseError {
	var closeErr websocket.CloseError
	if errors.As(err, &closeErr) {
		return &closeErr
	}
	return nil
}
