package cliutil

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2/altsrc"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/flags"
)

func TestTransportLogLevelFlagIsDeprecatedAndHidden(t *testing.T) {
	t.Parallel()

	var transportLogLevelFlag *altsrc.StringFlag
	for _, configuredFlag := range ConfigureLoggingFlags(false) {
		stringFlag, ok := configuredFlag.(*altsrc.StringFlag)
		if ok && stringFlag.Name == flags.TransportLogLevel {
			transportLogLevelFlag = stringFlag
			break
		}
	}

	require.NotNil(t, transportLogLevelFlag)
	assert.True(t, transportLogLevelFlag.Hidden)
	assert.Equal(t, "DEPRECATED. No longer has any effect.", transportLogLevelFlag.Usage)
	assert.Equal(t, []string{"proto-loglevel"}, transportLogLevelFlag.Aliases)
	assert.Equal(t, []string{"TUNNEL_PROTO_LOGLEVEL", "TUNNEL_TRANSPORT_LOGLEVEL"}, transportLogLevelFlag.EnvVars)
}

func TestLogTableWithoutTitle(t *testing.T) {
	t.Parallel()

	lines := captureTableLogs(t, []string{"first", "second"})

	assert.Equal(t, []string{
		"+----------+",
		"|  first   |",
		"|  second  |",
		"+----------+",
	}, lines)
}

func TestLogTableWithTitle(t *testing.T) {
	t.Parallel()

	lines := captureTableLogs(t, []string{"first", "second"}, "TT")

	assert.Equal(t, []string{
		"+----------+",
		"|    TT    |",
		"+----------+",
		"|  first   |",
		"|  second  |",
		"+----------+",
	}, lines)
}

func captureTableLogs(t *testing.T, lines []string, title ...string) []string {
	t.Helper()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	LogTable(&logger, lines, title...)

	// nolint: prealloc
	var messages []string
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var entry struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(line, &entry))
		messages = append(messages, entry.Message)
	}

	return messages
}
