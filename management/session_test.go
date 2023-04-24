package management

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Validate the active states of the session
func TestSession_ActiveControl(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := newSession(4, actor{}, cancel)
	// session starts out not active
	assert.False(t, session.Active())
	session.active.Store(true)
	assert.True(t, session.Active())
	session.Stop()
	assert.False(t, session.Active())
}

// Validate that the session filters events
func TestSession_Insert(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	infoLevel := new(LogLevel)
	*infoLevel = Info
	warnLevel := new(LogLevel)
	*warnLevel = Warn
	for _, test := range []struct {
		name      string
		filters   StreamingFilters
		expectLog bool
	}{
		{
			name:      "none",
			expectLog: true,
		},
		{
			name: "level",
			filters: StreamingFilters{
				Level: infoLevel,
			},
			expectLog: true,
		},
		{
			name: "filtered out level",
			filters: StreamingFilters{
				Level: warnLevel,
			},
			expectLog: false,
		},
		{
			name: "events",
			filters: StreamingFilters{
				Events: []LogEventType{HTTP},
			},
			expectLog: true,
		},
		{
			name: "filtered out event",
			filters: StreamingFilters{
				Events: []LogEventType{Cloudflared},
			},
			expectLog: false,
		},
		{
			name: "sampling",
			filters: StreamingFilters{
				Sampling: 0.9999999,
			},
			expectLog: true,
		},
		{
			name: "sampling (invalid negative)",
			filters: StreamingFilters{
				Sampling: -1.0,
			},
			expectLog: true,
		},
		{
			name: "sampling (invalid too large)",
			filters: StreamingFilters{
				Sampling: 2.0,
			},
			expectLog: true,
		},
		{
			name: "filter and event",
			filters: StreamingFilters{
				Level:  infoLevel,
				Events: []LogEventType{HTTP},
			},
			expectLog: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := newSession(4, actor{}, cancel)
			session.Filters(&test.filters)
			log := Log{
				Time:    time.Now().UTC().Format(time.RFC3339),
				Event:   HTTP,
				Level:   Info,
				Message: "test",
			}
			session.Insert(&log)
			select {
			case <-session.listener:
				require.True(t, test.expectLog)
			default:
				require.False(t, test.expectLog)
			}
		})
	}
}

// Validate that the session has a max amount of events to hold
func TestSession_InsertOverflow(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := newSession(1, actor{}, cancel)
	log := Log{
		Time:    time.Now().UTC().Format(time.RFC3339),
		Event:   HTTP,
		Level:   Info,
		Message: "test",
	}
	// Insert 2 but only max channel size for 1
	session.Insert(&log)
	session.Insert(&log)
	select {
	case <-session.listener:
		// pass
	default:
		require.Fail(t, "expected one log event")
	}
	// Second dequeue should fail
	select {
	case <-session.listener:
		require.Fail(t, "expected no more remaining log events")
	default:
		// pass
	}
}
