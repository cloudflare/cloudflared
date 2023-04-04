package management

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"

	"github.com/cloudflare/cloudflared/internal/test"
)

var (
	noopLogger = zerolog.New(io.Discard)
)

func TestReadEventsLoop(t *testing.T) {
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
	m := ManagementService{
		log: &noopLogger,
	}
	events := make(chan *ClientEvent)
	go m.readEvents(server, context.Background(), events)
	event := <-events
	require.Equal(t, sentEvent.Type, event.Type)
	server.Close(websocket.StatusInternalError, "")
}

func TestReadEventsLoop_ContextCancelled(t *testing.T) {
	client, server := test.WSPipe(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	client.CloseRead(ctx)
	defer func() {
		client.Close(websocket.StatusInternalError, "")
	}()
	m := ManagementService{
		log: &noopLogger,
	}
	events := make(chan *ClientEvent)
	go func() {
		time.Sleep(time.Second)
		cancel()
	}()
	// Want to make sure this function returns when context is cancelled
	m.readEvents(server, ctx, events)
	server.Close(websocket.StatusInternalError, "")
}
