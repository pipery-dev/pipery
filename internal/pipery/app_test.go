package pipery

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppRunExecutesCommandsFromPipedStdin(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "pipery.jsonl")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(oldWD); chdirErr != nil {
			t.Fatalf("failed to restore cwd: %v", chdirErr)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	expectedWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd after Chdir returned error: %v", err)
	}

	stdin := strings.NewReader("echo Hi\npwd\n")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	app := NewApp(stdin, stdout, stderr)
	exitCode, err := app.Run([]string{"-log-file", logPath})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	if got := stdout.String(); got != "Hi\n"+expectedWD+"\n" {
		t.Fatalf("unexpected stdout %q", got)
	}
	if got := stderr.String(); !strings.Contains(got, "psh summary: mode=stdin commands=2 failed=0 exit_code=0") {
		t.Fatalf("expected run summary in stderr, got %q", got)
	}

	file, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var entries []map[string]any
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("failed to unmarshal log entry: %v", err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner returned error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}

	if got := entries[0]["mode"]; got != "stdin" {
		t.Fatalf("expected first mode %q, got %#v", "stdin", got)
	}
	if got := entries[0]["raw_command"]; got != "echo Hi" {
		t.Fatalf("expected first raw_command %q, got %#v", "echo Hi", got)
	}
	if cores, ok := entries[0]["system_cpu_cores"].(float64); !ok || cores < 1 {
		t.Fatalf("expected first entry system_cpu_cores to be present and positive, got %#v", entries[0]["system_cpu_cores"])
	}
	if memory, ok := entries[0]["system_memory_bytes"].(float64); !ok || memory < 1 {
		t.Fatalf("expected first entry system_memory_bytes to be present and positive, got %#v", entries[0]["system_memory_bytes"])
	}
	if _, ok := entries[0]["process_max_rss_bytes"].(float64); !ok {
		t.Fatalf("expected first entry process_max_rss_bytes to be present, got %#v", entries[0]["process_max_rss_bytes"])
	}

	if got := entries[1]["builtin"]; got != true {
		t.Fatalf("expected second entry to be builtin, got %#v", got)
	}
	if got := entries[1]["mode"]; got != "stdin" {
		t.Fatalf("expected second mode %q, got %#v", "stdin", got)
	}
	if got := entries[1]["raw_command"]; got != "pwd" {
		t.Fatalf("expected second raw_command %q, got %#v", "pwd", got)
	}
}

func TestAppRunCreatesDefaultLogFile(t *testing.T) {
	tempDir := t.TempDir()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(oldWD); chdirErr != nil {
			t.Fatalf("failed to restore cwd: %v", chdirErr)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}

	app := NewApp(strings.NewReader("echo default-log\n"), &bytes.Buffer{}, &bytes.Buffer{})
	exitCode, err := app.Run(nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	logPath := filepath.Join(tempDir, "pipery.jsonl")
	file, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatalf("expected at least one log entry, but file is empty")
	}

	var entry map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
		t.Fatalf("failed to unmarshal log entry: %v", err)
	}

	if stdout, ok := entry["stdout"].(string); !ok || stdout != "default-log\n" {
		t.Fatalf(`expected stdout to be "default-log\n", got %q`, stdout)
	}
}

func TestAppRunFailOnErrorStopsAfterFirstFailure(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "pipery.jsonl")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(oldWD); chdirErr != nil {
			t.Fatalf("failed to restore cwd: %v", chdirErr)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}

	stdin := strings.NewReader("printf 'before\\n'\nexit 7\necho after\n")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	app := NewApp(stdin, stdout, stderr)
	exitCode, err := app.Run([]string{"-log-file", logPath, "-fail-on-error"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if exitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", exitCode)
	}
	if got := stdout.String(); got != "before\n" {
		t.Fatalf("expected fail-on-error to stop before the final command, got stdout %q", got)
	}
	if got := stderr.String(); !strings.Contains(got, "psh summary: mode=stdin commands=2 failed=1 exit_code=7") {
		t.Fatalf("expected failing run summary in stderr, got %q", got)
	}

	file, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var entries []map[string]any
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("failed to unmarshal log entry: %v", err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries when fail-on-error stops the session, got %d", len(entries))
	}

	if stdout, ok := entries[0]["stdout"].(string); !ok || stdout != "before\n" {
		t.Fatalf(`expected first stdout to be "before\n", got %q`, stdout)
	}
	if exitCode, ok := entries[1]["exit_code"].(float64); !ok || int(exitCode) != 7 {
		t.Fatalf("expected second exit_code to be 7, got %#v", entries[1]["exit_code"])
	}
}

func TestAppRunRetriesFailedCommand(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "pipery.jsonl")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(oldWD); chdirErr != nil {
			t.Fatalf("failed to restore cwd: %v", chdirErr)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}

	app := NewApp(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	exitCode, err := app.Run([]string{
		"-log-file", logPath,
		"-retry-count", "1",
		"-c", "if [ ! -f retry.ok ]; then touch retry.ok; exit 7; fi",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0 after retry, got %d", exitCode)
	}

	entries := readLogEntries(t, logPath)
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries for one retry, got %d", len(entries))
	}
	if exitCode, ok := entries[0]["exit_code"].(float64); !ok || int(exitCode) != 7 {
		t.Fatalf("expected first exit_code to be 7, got %#v", entries[0]["exit_code"])
	}
	if exitCode, ok := entries[1]["exit_code"].(float64); !ok || int(exitCode) != 0 {
		t.Fatalf("expected second exit_code to be 0, got %#v", entries[1]["exit_code"])
	}
}

func TestAppRunCommandTimeoutStopsCommand(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "pipery.jsonl")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(oldWD); chdirErr != nil {
			t.Fatalf("failed to restore cwd: %v", chdirErr)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}

	stderr := &bytes.Buffer{}
	app := NewApp(strings.NewReader(""), &bytes.Buffer{}, stderr)
	exitCode, err := app.Run([]string{
		"-log-file", logPath,
		"-command-timeout", "50ms",
		"-c", "sleep 1",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if exitCode != timeoutExitCode {
		t.Fatalf("expected timeout exit code %d, got %d", timeoutExitCode, exitCode)
	}

	entries := readLogEntries(t, logPath)
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}
	if exitCode, ok := entries[0]["exit_code"].(float64); !ok || int(exitCode) != timeoutExitCode {
		t.Fatalf("expected timed out exit code %d, got %#v", timeoutExitCode, entries[0]["exit_code"])
	}
	if got := stderr.String(); !strings.Contains(got, "exit_code=124") {
		t.Fatalf("expected timeout summary in stderr, got %q", got)
	}
}

func TestAppRunSessionTimeoutStopsRemainingCommands(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "pipery.jsonl")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(oldWD); chdirErr != nil {
			t.Fatalf("failed to restore cwd: %v", chdirErr)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := NewApp(strings.NewReader(""), stdout, stderr)
	exitCode, err := app.Run([]string{
		"-log-file", logPath,
		"-session-timeout", "100ms",
		"-c", "sleep 1",
		"-c", "printf 'after\\n'",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if exitCode != timeoutExitCode {
		t.Fatalf("expected session timeout exit code %d, got %d", timeoutExitCode, exitCode)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("expected second command not to run, got stdout %q", got)
	}

	entries := readLogEntries(t, logPath)
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry before session timeout stopped the run, got %d", len(entries))
	}
	if got := stderr.String(); !strings.Contains(got, "commands=1 failed=1 exit_code=124") {
		t.Fatalf("expected session timeout summary in stderr, got %q", got)
	}
}

func TestAppRunParallelShellCommands(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "pipery.jsonl")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(oldWD); chdirErr != nil {
			t.Fatalf("failed to restore cwd: %v", chdirErr)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}

	startedAt := time.Now()
	app := NewApp(os.Stdin, &bytes.Buffer{}, &bytes.Buffer{})
	exitCode, err := app.Run([]string{
		"-log-file", logPath,
		"-parallelism", "3",
		"-c", "sleep 0.3",
		"-c", "sleep 0.3",
		"-c", "sleep 0.3",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	if elapsed := time.Since(startedAt); elapsed >= 650*time.Millisecond {
		t.Fatalf("expected parallel run to finish faster than sequential execution, got %s", elapsed)
	}

	entries := readLogEntries(t, logPath)
	if len(entries) != 3 {
		t.Fatalf("expected 3 log entries for the parallel commands, got %d", len(entries))
	}
}

func readLogEntries(t *testing.T, logPath string) []map[string]any {
	t.Helper()

	file, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var entries []map[string]any
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("failed to unmarshal log entry: %v", err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner returned error: %v", err)
	}

	return entries
}
