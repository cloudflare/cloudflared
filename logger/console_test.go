package logger

import (
	"bytes"
	"strings"
	"sync"
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

type countingWriter struct {
	bytes.Buffer
	writes int
}

func (c *countingWriter) Write(b []byte) (int, error) {
	c.writes++
	return c.Buffer.Write(b)
}

func TestConsoleLoggerWrites(t *testing.T) {
	cw := &countingWriter{}
	logger := zerolog.New(&consoleWriter{out: cw}).With().Timestamp().Logger()

	logger.Error().
		Int("connIndex", 3).
		Str("error", "context canceled").
		Str("kind", "http").
		Msg("Request failed")

	if cw.writes != 1 {
		t.Fatalf("expected one underlying Write call for the event, got %d", cw.writes)
	}
	if !bytes.HasSuffix(cw.Bytes(), []byte("\n")) {
		t.Fatalf("expected output to end with a newline, got %q", cw.String())
	}
	var event map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(cw.Bytes()), &event); err != nil {
		t.Fatalf("expected valid JSON output, got %q: %v", cw.String(), err)
	}
	if event["message"] != "Request failed" {
		t.Errorf("unexpected message field: got %v", event["message"])
	}
}

// syncedBuffer makes the test safe while preserving per-Write
// interleaving, like concurrent writes to stderr.
type syncedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (w *syncedBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.Write(p)
}

func (w *syncedBuffer) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return bytes.Clone(w.buffer.Bytes())
}

func TestConsoleLoggerConcurrentWrites(t *testing.T) {
	t.Parallel()

	const (
		goroutines = 8
		events     = 5_000
	)

	buf := &syncedBuffer{}
	logger := zerolog.New(&consoleWriter{out: buf}).With().Timestamp().Logger()

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Go(func() {
			for i := 0; i < events; i++ {
				logger.Error().
					Int("connIndex", g).
					Int("event", 0).
					Str("dest", "https://example.com/api/x").
					Str("error", "Incoming request ended abruptly: context canceled").
					Str("type", "http").
					Msg("Request failed")
			}
		})
	}
	wg.Wait()

	output := buf.Bytes()
	lines := bytes.Split(
		bytes.TrimSuffix(output, []byte("\n")),
		[]byte("\n"),
	)

	expected := goroutines * events
	if len(lines) != expected {
		t.Fatalf("expected %d JSON lines, got %d", expected, len(lines))
	}

	invalid := 0
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			invalid++
		}
	}
	if invalid > 0 {
		t.Errorf("%d of %d output lines are invalid JSON", invalid, len(lines))
	}
}
