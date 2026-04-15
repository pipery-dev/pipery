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
	configDir := filepath.Join(tempDir, ".pipery")
	configPath := filepath.Join(configDir, "config.yaml")

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

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
	configDir := filepath.Join(tempDir, ".pipery")
	configPath := filepath.Join(configDir, "config.yaml")

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	configBody := strings.Join([]string{
		"log_file: config.jsonl",
		"queue_size: 64",
		"fail_on_error: false",
		`flush_timeout: "3s"`,
		"secret_names:",
		"  - FROM_CONFIG",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	t.Setenv("PIPERY_LOG_FILE", "env.jsonl")
	t.Setenv("PIPERY_QUEUE_SIZE", "128")
	t.Setenv("PIPERY_FLUSH_TIMEOUT", "4s")
	t.Setenv("PIPERY_SECRET_PREFIXES", "ENV_,CI_")
	t.Setenv("PIPERY_FAIL_ON_ERROR", "true")

	cfg, _, _, _, err := parseArgs([]string{
		"-config", configPath,
		"-queue-size", "256",
		"-fail-on-error=false",
		"-flush-timeout", "7s",
		"-secret-suffixes", "_TAIL,_POSTFIX",
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
	if cfg.FailOnError {
		t.Fatalf("expected flag to override env fail_on_error to false")
	}
	if len(cfg.SecretNames) != 1 || cfg.SecretNames[0] != "FROM_CONFIG" {
		t.Fatalf("expected secret names from config, got %#v", cfg.SecretNames)
	}
	if len(cfg.SecretPrefixes) != 2 || cfg.SecretPrefixes[0] != "ENV_" || cfg.SecretPrefixes[1] != "CI_" {
		t.Fatalf("expected secret prefixes from env, got %#v", cfg.SecretPrefixes)
	}
	if len(cfg.SecretSuffixes) != 2 || cfg.SecretSuffixes[0] != "_TAIL" || cfg.SecretSuffixes[1] != "_POSTFIX" {
		t.Fatalf("expected secret suffixes from flags, got %#v", cfg.SecretSuffixes)
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

func TestParseArgsLoadsDefaultDotPiperyConfig(t *testing.T) {
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, ".pipery")
	configPath := filepath.Join(configDir, "config.yaml")

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	if err := os.WriteFile(configPath, []byte("queue_size: 99\nprompt: \"dotpipery> \"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

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
	currentWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}

	cfg, _, _, _, err := parseArgs(nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}

	expectedConfigFile := filepath.Join(currentWD, ".pipery", "config.yaml")
	if cfg.ConfigFile != expectedConfigFile {
		t.Fatalf("expected config file %q, got %q", expectedConfigFile, cfg.ConfigFile)
	}
	if cfg.QueueSize != 99 {
		t.Fatalf("expected queue size 99, got %d", cfg.QueueSize)
	}
	if cfg.Prompt != "dotpipery> " {
		t.Fatalf("expected prompt from default config, got %q", cfg.Prompt)
	}
}
