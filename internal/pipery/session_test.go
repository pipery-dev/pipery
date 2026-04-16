package pipery

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestSessionBuiltinsAndShellExecution(t *testing.T) {
	tempDir := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	recordSink := &recordingSink{}
	logger := newAsyncLogger([]sink{recordSink}, 16, stderr, redactionConfig{})
	defer func() {
		if err := logger.Close(time.Second); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	}()

	currentSession, err := newSession(sessionConfig{
		Shell:           defaultShell(),
		CWD:             tempDir,
		Env:             []string{"BASE=value"},
		Stdout:          stdout,
		Stderr:          stderr,
		Logger:          logger,
		MaxCaptureBytes: 1024,
		Prompt:          "psh> ",
	})
	if err != nil {
		t.Fatalf("newSession returned error: %v", err)
	}

	result, shouldExit, err := currentSession.runLine("export HELLO=world", lineRunOptions{
		allowBuiltins: true,
		mode:          "shell",
	})
	if err != nil {
		t.Fatalf("runLine(export) returned error: %v", err)
	}
	if shouldExit {
		t.Fatalf("export should not exit the session")
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected export exit code 0, got %d", result.ExitCode)
	}
	if got := currentSession.env["HELLO"]; got != "world" {
		t.Fatalf("expected HELLO=%q, got %q", "world", got)
	}

	stdout.Reset()
	stderr.Reset()

	result, shouldExit, err = currentSession.runLine(`printf "%s" "$HELLO"; printf "%s" "warn" >&2; exit 7`, lineRunOptions{
		allowBuiltins: true,
		mode:          "shell",
	})
	if err != nil {
		t.Fatalf("runLine(shell) returned error: %v", err)
	}
	if shouldExit {
		t.Fatalf("shell execution should not exit the session")
	}
	if result.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", result.ExitCode)
	}
	if got := stdout.String(); got != "world" {
		t.Fatalf("expected stdout %q, got %q", "world", got)
	}
	if got := stderr.String(); got != "warn" {
		t.Fatalf("expected stderr %q, got %q", "warn", got)
	}

	stdout.Reset()

	result, shouldExit, err = currentSession.runLine("pwd", lineRunOptions{
		allowBuiltins: true,
		mode:          "interactive",
	})
	if err != nil {
		t.Fatalf("runLine(pwd) returned error: %v", err)
	}
	if shouldExit {
		t.Fatalf("pwd should not exit the session")
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected pwd exit code 0, got %d", result.ExitCode)
	}
	if got := strings.TrimSpace(stdout.String()); got != tempDir {
		t.Fatalf("expected pwd output %q, got %q", tempDir, got)
	}
}
