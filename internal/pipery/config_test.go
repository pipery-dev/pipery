package pipery

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseArgsLoadsYAMLConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "pipery.yaml")

	configBody := strings.Join([]string{
		"log_file: from-config.jsonl",
		"syslog: udp://127.0.0.1:5514",
		"syslog_tag: config-tag",
		"queue_size: 64",
		"max_capture_bytes: 4096",
		"shell: /bin/sh",
		`prompt: "config> "`,
		`flush_timeout: "5s"`,
	}, "\n")
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg, shellCommands, directCommand, showHelp, err := parseArgs([]string{"-config", configPath}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if showHelp {
		t.Fatalf("expected showHelp=false")
	}
	if len(shellCommands) != 0 {
		t.Fatalf("expected no shell commands, got %v", shellCommands)
	}
	if len(directCommand) != 0 {
		t.Fatalf("expected no direct command, got %v", directCommand)
	}

	if cfg.LogFile != "from-config.jsonl" {
		t.Fatalf("expected log file from config, got %q", cfg.LogFile)
	}
	if cfg.SyslogTarget != "udp://127.0.0.1:5514" {
		t.Fatalf("expected syslog target from config, got %q", cfg.SyslogTarget)
	}
	if cfg.SyslogTag != "config-tag" {
		t.Fatalf("expected syslog tag from config, got %q", cfg.SyslogTag)
	}
	if cfg.QueueSize != 64 {
		t.Fatalf("expected queue size 64, got %d", cfg.QueueSize)
	}
	if cfg.MaxCaptureBytes != 4096 {
		t.Fatalf("expected max capture bytes 4096, got %d", cfg.MaxCaptureBytes)
	}
	if cfg.Shell != "/bin/sh" {
		t.Fatalf("expected shell /bin/sh, got %q", cfg.Shell)
	}
	if cfg.Prompt != "config> " {
		t.Fatalf("expected prompt from config, got %q", cfg.Prompt)
	}
	if cfg.FlushTimeout != 5*time.Second {
		t.Fatalf("expected flush timeout 5s, got %s", cfg.FlushTimeout)
	}
}

func TestParseArgsEnvOverridesConfigAndFlagsOverrideEnv(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "pipery.yaml")

	configBody := strings.Join([]string{
		"log_file: config.jsonl",
		"queue_size: 64",
		`flush_timeout: "3s"`,
	}, "\n")
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	t.Setenv("PIPERY_LOG_FILE", "env.jsonl")
	t.Setenv("PIPERY_QUEUE_SIZE", "128")
	t.Setenv("PIPERY_FLUSH_TIMEOUT", "4s")

	cfg, _, _, _, err := parseArgs([]string{
		"-config", configPath,
		"-queue-size", "256",
		"-flush-timeout", "7s",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}

	if cfg.LogFile != "env.jsonl" {
		t.Fatalf("expected env var to override config log file, got %q", cfg.LogFile)
	}
	if cfg.QueueSize != 256 {
		t.Fatalf("expected flag to override env queue size, got %d", cfg.QueueSize)
	}
	if cfg.FlushTimeout != 7*time.Second {
		t.Fatalf("expected flag to override env flush timeout, got %s", cfg.FlushTimeout)
	}
}

func TestParseArgsIgnoresNonYAMLDefaultFiles(t *testing.T) {
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

	// This file should never be treated as config, even though it shares the
	// "pipery" basename that we use for the default YAML config names.
	if err := os.WriteFile("pipery.jsonl", []byte("{\"not\":\"yaml\"}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg, shellCommands, directCommand, showHelp, err := parseArgs(nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if showHelp {
		t.Fatalf("expected showHelp=false")
	}
	if len(shellCommands) != 0 {
		t.Fatalf("expected no shell commands, got %v", shellCommands)
	}
	if len(directCommand) != 0 {
		t.Fatalf("expected no direct commands, got %v", directCommand)
	}
	if cfg.ConfigFile != "" {
		t.Fatalf("expected no config file to be loaded, got %q", cfg.ConfigFile)
	}
	if cfg.LogFile != "pipery.jsonl" {
		t.Fatalf("expected default log file, got %q", cfg.LogFile)
	}
}
