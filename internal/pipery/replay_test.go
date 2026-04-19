package pipery

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadReplayTraceRejectsMissingRequiredField(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "invalid.jsonl")
	if err := os.WriteFile(path, []byte(`{"timestamp":"2026-01-01T00:00:00Z"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	_, err := loadReplayTrace(path)
	if err == nil || !strings.Contains(err.Error(), `missing required field "started_at"`) {
		t.Fatalf("expected missing field validation error, got %v", err)
	}
}

func TestAppRunReplayCreatesNumberedLogAndPrintsComparison(t *testing.T) {
	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "pipery.jsonl")
	existingReplayPath := inputPath + ".1"
	if err := os.WriteFile(existingReplayPath, []byte("already here\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	entry := logEntry{
		Timestamp:      time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		StartedAt:      time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		FinishedAt:     time.Date(2026, 1, 1, 10, 0, 0, 5*int(time.Millisecond), time.UTC),
		Duration:       "5ms",
		DurationMillis: 5,
		Mode:           "shell",
		Builtin:        false,
		Command:        defaultShell(),
		Args:           shellArgs("printf 'hello\\n'"),
		RawCommand:     "printf 'hello\\n'",
		BeforeCwd:      tempDir,
		Cwd:            tempDir,
		BeforeEnv:      os.Environ(),
		Env:            os.Environ(),
		Stdout:         "hello\n",
		ExitCode:       0,
	}
	if err := writeReplayLog(inputPath, []logEntry{entry}); err != nil {
		t.Fatalf("writeReplayLog returned error: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := NewApp(strings.NewReader(""), stdout, stderr)

	exitCode, err := app.Run([]string{"-replay", inputPath})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	replayPath := inputPath + ".2"
	if _, err := os.Stat(replayPath); err != nil {
		t.Fatalf("expected replay log file %q to exist: %v", replayPath, err)
	}

	if got := stdout.String(); !strings.Contains(got, "outputs match across all compared runs") {
		t.Fatalf("expected comparison report in stdout, got %q", got)
	}
	if got := stdout.String(); !strings.Contains(got, "command duration chart (ms)") {
		t.Fatalf("expected timing chart in stdout, got %q", got)
	}
	if got := stderr.String(); !strings.Contains(got, "psh summary: mode=replay commands=1 failed=0 exit_code=0") {
		t.Fatalf("expected replay summary in stderr, got %q", got)
	}
}

func writeReplayLog(path string, entries []logEntry) error {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	for _, entry := range entries {
		if err := encoder.Encode(entry); err != nil {
			return err
		}
	}
	return os.WriteFile(path, buffer.Bytes(), 0o644)
}
