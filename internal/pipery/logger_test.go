package pipery

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type recordingSink struct {
	mu      sync.Mutex
	records [][]byte
}

func (s *recordingSink) Name() string {
	return "recording"
}

func (s *recordingSink) Write(payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	copyPayload := append([]byte(nil), payload...)
	s.records = append(s.records, copyPayload)
	return nil
}

func (s *recordingSink) Close() error {
	return nil
}

func (s *recordingSink) Records() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	records := make([][]byte, len(s.records))
	copy(records, s.records)
	return records
}

func TestAsyncLoggerWritesJSONLine(t *testing.T) {
	recordSink := &recordingSink{}
	stderr := &bytes.Buffer{}
	logger := newAsyncLogger([]sink{recordSink}, 4, stderr)

	logger.Log(logEntry{
		Timestamp:      time.Unix(1, 0).UTC(),
		StartedAt:      time.Unix(1, 0).UTC(),
		FinishedAt:     time.Unix(2, 0).UTC(),
		Duration:       "1s",
		DurationMillis: 1000,
		Mode:           "shell",
		Command:        "echo",
		Args:           []string{"hello"},
		RawCommand:     "echo hello",
		Cwd:            "/tmp",
		Env:            []string{"FOO=bar"},
		ExitCode:       0,
	})

	if err := logger.Close(time.Second); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	records := recordSink.Records()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	var entry map[string]any
	if err := json.Unmarshal(records[0], &entry); err != nil {
		t.Fatalf("failed to unmarshal logged JSON: %v", err)
	}

	if got := entry["command"]; got != "echo" {
		t.Fatalf("expected command %q, got %#v", "echo", got)
	}

	if got := entry["mode"]; got != "shell" {
		t.Fatalf("expected mode %q, got %#v", "shell", got)
	}
}

func TestParseSyslogTargetAddsDefaultPort(t *testing.T) {
	network, address, err := parseSyslogTarget("udp://127.0.0.1")
	if err != nil {
		t.Fatalf("parseSyslogTarget returned error: %v", err)
	}

	if network != "udp" {
		t.Fatalf("expected network %q, got %q", "udp", network)
	}

	if address != "127.0.0.1:514" {
		t.Fatalf("expected address %q, got %q", "127.0.0.1:514", address)
	}
}
