package management

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"

	"github.com/cloudflare/cloudflared/internal/test"
)

var (
	debugLevel *LogLevel
	infoLevel  *LogLevel
	warnLevel  *LogLevel
	errorLevel *LogLevel
)

func init() {
	// created here because we can't do a reference to a const enum, i.e. &Info
	debugLevel := new(LogLevel)
	*debugLevel = Debug
	infoLevel := new(LogLevel)
	*infoLevel = Info
	warnLevel := new(LogLevel)
	*warnLevel = Warn
	errorLevel := new(LogLevel)
	*errorLevel = Error
}

func TestIntoClientEvent_StartStreaming(t *testing.T) {
	for _, test := range []struct {
		name     string
		expected EventStartStreaming
	}{
		{
			name:     "no filters",
			expected: EventStartStreaming{ClientEvent: ClientEvent{Type: StartStreaming}},
		},
		{
			name: "level filter",
			expected: EventStartStreaming{
				ClientEvent: ClientEvent{Type: StartStreaming},
				Filters: &StreamingFilters{
					Level: infoLevel,
				},
			},
		},
		{
			name: "events filter",
			expected: EventStartStreaming{
				ClientEvent: ClientEvent{Type: StartStreaming},
				Filters: &StreamingFilters{
					Events: []LogEventType{Cloudflared, HTTP},
				},
			},
		},
		{
			name: "sampling filter",
			expected: EventStartStreaming{
				ClientEvent: ClientEvent{Type: StartStreaming},
				Filters: &StreamingFilters{
					Sampling: 0.5,
				},
			},
		},
		{
			name: "level and events filters",
			expected: EventStartStreaming{
				ClientEvent: ClientEvent{Type: StartStreaming},
				Filters: &StreamingFilters{
					Level:    infoLevel,
					Events:   []LogEventType{Cloudflared},
					Sampling: 0.5,
				},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := json.Marshal(test.expected)
			require.NoError(t, err)
			event := ClientEvent{}
			err = json.Unmarshal(data, &event)
			require.NoError(t, err)
			event.event = data
			ce, ok := IntoClientEvent[EventStartStreaming](&event, StartStreaming)
			require.True(t, ok)
			require.Equal(t, test.expected.ClientEvent, ce.ClientEvent)
			if test.expected.Filters != nil {
				f := ce.Filters
				ef := test.expected.Filters
				if ef.Level != nil {
					require.Equal(t, *ef.Level, *f.Level)
				}
				require.ElementsMatch(t, ef.Events, f.Events)
			}
		})
	}
}

func TestIntoClientEvent_StopStreaming(t *testing.T) {
	event := ClientEvent{
		Type:  StopStreaming,
		event: []byte(`{"type": "stop_streaming"}`),
	}
	ce, ok := IntoClientEvent[EventStopStreaming](&event, StopStreaming)
	require.True(t, ok)
	require.Equal(t, EventStopStreaming{ClientEvent: ClientEvent{Type: StopStreaming}}, *ce)
}

func TestIntoClientEvent_Invalid(t *testing.T) {
	event := ClientEvent{
		Type:  UnknownClientEventType,
		event: []byte(`{"type": "invalid"}`),
	}
	_, ok := IntoClientEvent[EventStartStreaming](&event, StartStreaming)
	require.False(t, ok)
}

func TestIntoServerEvent_Logs(t *testing.T) {
	event := ServerEvent{
		Type:  Logs,
		event: []byte(`{"type": "logs"}`),
	}
	ce, ok := IntoServerEvent(&event, Logs)
	require.True(t, ok)
	require.Equal(t, EventLog{ServerEvent: ServerEvent{Type: Logs}}, *ce)
}

func TestIntoServerEvent_Invalid(t *testing.T) {
	event := ServerEvent{
		Type:  UnknownServerEventType,
		event: []byte(`{"type": "invalid"}`),
	}
	_, ok := IntoServerEvent(&event, Logs)
	require.False(t, ok)
}

func TestReadServerEvent(t *testing.T) {
	sentEvent := EventLog{
		ServerEvent: ServerEvent{Type: Logs},
		Logs: []*Log{
			{
				Time:    time.Now().UTC().Format(time.RFC3339),
				Event:   HTTP,
				Level:   Info,
				Message: "test",
			},
		},
	}
	client, server := test.WSPipe(nil, nil)
	server.CloseRead(context.Background())
	defer func() {
		server.Close(websocket.StatusInternalError, "")
	}()
	go func() {
		err := WriteEvent(server, context.Background(), &sentEvent)
		require.NoError(t, err)
	}()
	event, err := ReadServerEvent(client, context.Background())
	require.NoError(t, err)
	require.Equal(t, sentEvent.Type, event.Type)
	client.Close(websocket.StatusInternalError, "")
}

func TestReadServerEvent_InvalidWebSocketMessageType(t *testing.T) {
	client, server := test.WSPipe(nil, nil)
	server.CloseRead(context.Background())
	defer func() {
		server.Close(websocket.StatusInternalError, "")
	}()
	go func() {
		err := server.Write(context.Background(), websocket.MessageBinary, []byte("test1234"))
		require.NoError(t, err)
	}()
	_, err := ReadServerEvent(client, context.Background())
	require.Error(t, err)
	client.Close(websocket.StatusInternalError, "")
}

func TestReadServerEvent_InvalidMessageType(t *testing.T) {
	sentEvent := ClientEvent{Type: ClientEventType(UnknownServerEventType)}
	client, server := test.WSPipe(nil, nil)
	server.CloseRead(context.Background())
	defer func() {
		server.Close(websocket.StatusInternalError, "")
	}()
	go func() {
		err := WriteEvent(server, context.Background(), &sentEvent)
		require.NoError(t, err)
	}()
	_, err := ReadServerEvent(client, context.Background())
	require.ErrorIs(t, err, errInvalidMessageType)
	client.Close(websocket.StatusInternalError, "")
}

func TestReadClientEvent(t *testing.T) {
	sentEvent := EventStartStreaming{
		ClientEvent: ClientEvent{Type: StartStreaming},
	}
	client, server := test.WSPipe(nil, nil)
	client.CloseRead(context.Background())
	defer func() {
		client.Close(websocket.StatusInternalError, "")
	}()
	go func() {
		err := WriteEvent(client, context.Background(), &sentEvent)
		require.NoError(t, err)
	}()
	event, err := ReadClientEvent(server, context.Background())
	require.NoError(t, err)
	require.Equal(t, sentEvent.Type, event.Type)
	server.Close(websocket.StatusInternalError, "")
}

func TestReadClientEvent_InvalidWebSocketMessageType(t *testing.T) {
	client, server := test.WSPipe(nil, nil)
	client.CloseRead(context.Background())
	defer func() {
		client.Close(websocket.StatusInternalError, "")
	}()
	go func() {
		err := client.Write(context.Background(), websocket.MessageBinary, []byte("test1234"))
		require.NoError(t, err)
	}()
	_, err := ReadClientEvent(server, context.Background())
	require.Error(t, err)
	server.Close(websocket.StatusInternalError, "")
}

func TestReadClientEvent_InvalidMessageType(t *testing.T) {
	sentEvent := ClientEvent{Type: UnknownClientEventType}
	client, server := test.WSPipe(nil, nil)
	client.CloseRead(context.Background())
	defer func() {
		client.Close(websocket.StatusInternalError, "")
	}()
	go func() {
		err := WriteEvent(client, context.Background(), &sentEvent)
		require.NoError(t, err)
	}()
	_, err := ReadClientEvent(server, context.Background())
	require.ErrorIs(t, err, errInvalidMessageType)
	server.Close(websocket.StatusInternalError, "")
}
