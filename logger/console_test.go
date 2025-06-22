package logger

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestConsoleLoggerDuplicateKeys(t *testing.T) {
	r := bytes.NewBuffer(make([]byte, 500))
	logger := zerolog.New(&consoleWriter{out: r}).With().Timestamp().Logger()
	logger.Debug().Str("test", "1234").Int("number", 45).Str("test", "5678").Msg("log message")

	event, err := r.ReadString('\n')
	if err != nil {
		t.Error(err)
	}

	if !strings.Contains(event, "\"test\":\"5678\"") {
		t.Errorf("log event missing key 'test': %s", event)
	}
	if !strings.Contains(event, "\"number\":45") {
		t.Errorf("log event missing key 'number': %s", event)
	}
	if !strings.Contains(event, "\"time\":") {
		t.Errorf("log event missing key 'time': %s", event)
	}
	if !strings.Contains(event, "\"level\":\"debug\"") {
		t.Errorf("log event missing key 'level': %s", event)
	}
}
