package pipery

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if got := stderr.String(); !strings.Contains(got, "pipery summary: mode=stdin commands=2 failed=0 exit_code=0") {
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
	if got := stderr.String(); !strings.Contains(got, "pipery summary: mode=stdin commands=2 failed=1 exit_code=7") {
		t.Fatalf("expected failing run summary in stderr, got %q", got)
	}

	file, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner returned error: %v", err)
	}
	if lineCount != 2 {
		t.Fatalf("expected 2 log entries when fail-on-error stops the session, got %d", lineCount)
	}
}
