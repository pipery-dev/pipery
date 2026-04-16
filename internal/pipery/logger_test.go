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
	logger := newAsyncLogger([]sink{recordSink}, 4, stderr, redactionConfig{})

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

func TestAsyncLoggerRedactsSensitiveEnvVarsAndValues(t *testing.T) {
	recordSink := &recordingSink{}
	stderr := &bytes.Buffer{}
	logger := newAsyncLogger([]sink{recordSink}, 4, stderr, redactionConfig{})

	logger.Log(logEntry{
		Timestamp:      time.Unix(1, 0).UTC(),
		StartedAt:      time.Unix(1, 0).UTC(),
		FinishedAt:     time.Unix(2, 0).UTC(),
		Duration:       "1s",
		DurationMillis: 1000,
		Mode:           "shell",
		Command:        "echo",
		Args:           []string{"ghs_123456"},
		RawCommand:     "echo ghs_123456",
		Cwd:            "/tmp",
		Env: []string{
			"GITHUB_TOKEN=ghs_123456",
			"PLAIN=value",
		},
		Stdout:   "token=ghs_123456",
		Stderr:   "err ghs_123456",
		Stdin:    "input ghs_123456",
		Error:    "problem ghs_123456",
		ExitCode: 0,
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

	envItems, ok := entry["env"].([]any)
	if !ok {
		t.Fatalf("expected env to be an array, got %#v", entry["env"])
	}

	if envItems[0] != "GITHUB_TOKEN=[MASKED]" {
		t.Fatalf("expected masked env entry, got %#v", envItems[0])
	}
	if envItems[1] != "PLAIN=value" {
		t.Fatalf("expected plain env entry to remain visible, got %#v", envItems[1])
	}
	if entry["raw_command"] != "echo [MASKED]" {
		t.Fatalf("expected raw command to be masked, got %#v", entry["raw_command"])
	}
	if entry["stdout"] != "token=[MASKED]" {
		t.Fatalf("expected stdout to be masked, got %#v", entry["stdout"])
	}
	if entry["stderr"] != "err [MASKED]" {
		t.Fatalf("expected stderr to be masked, got %#v", entry["stderr"])
	}
	if entry["stdin"] != "input [MASKED]" {
		t.Fatalf("expected stdin to be masked, got %#v", entry["stdin"])
	}
	if entry["error"] != "problem [MASKED]" {
		t.Fatalf("expected error to be masked, got %#v", entry["error"])
	}
}

func TestAsyncLoggerRedactsConfiguredSecretPatterns(t *testing.T) {
	recordSink := &recordingSink{}
	stderr := &bytes.Buffer{}
	logger := newAsyncLogger([]sink{recordSink}, 4, stderr, redactionConfig{
		SecretNames:    []string{"CUSTOM_NAME"},
		SecretPrefixes: []string{"ORG_"},
		SecretSuffixes: []string{"_TAIL"},
	})

	logger.Log(logEntry{
		Timestamp:      time.Unix(1, 0).UTC(),
		StartedAt:      time.Unix(1, 0).UTC(),
		FinishedAt:     time.Unix(2, 0).UTC(),
		Duration:       "1s",
		DurationMillis: 1000,
		Mode:           "shell",
		Command:        "echo",
		RawCommand:     "echo name-secret prefix-secret suffix-secret",
		Cwd:            "/tmp",
		Env: []string{
			"CUSTOM_NAME=name-secret",
			"ORG_SERVICE_TOKEN=prefix-secret",
			"VALUE_TAIL=suffix-secret",
		},
		Stdout:   "name-secret prefix-secret suffix-secret",
		ExitCode: 0,
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

	envItems := entry["env"].([]any)
	if envItems[0] != "CUSTOM_NAME=[MASKED]" {
		t.Fatalf("expected exact-name matcher to redact, got %#v", envItems[0])
	}
	if envItems[1] != "ORG_SERVICE_TOKEN=[MASKED]" {
		t.Fatalf("expected prefix matcher to redact, got %#v", envItems[1])
	}
	if envItems[2] != "VALUE_TAIL=[MASKED]" {
		t.Fatalf("expected suffix matcher to redact, got %#v", envItems[2])
	}
	if entry["stdout"] != "[MASKED] [MASKED] [MASKED]" {
		t.Fatalf("expected stdout to be masked, got %#v", entry["stdout"])
	}
}
