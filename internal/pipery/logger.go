package pipery

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// logEntry is the structured record we emit for each command or built-in.
//
// The JSON tags define the output shape written to the file sink and syslog.
type logEntry struct {
	Timestamp       time.Time `json:"timestamp"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
	Duration        string    `json:"duration"`
	DurationMillis  int64     `json:"duration_ms"`
	Mode            string    `json:"mode"`
	Builtin         bool      `json:"builtin"`
	Command         string    `json:"command"`
	Args            []string  `json:"args,omitempty"`
	RawCommand      string    `json:"raw_command"`
	Cwd             string    `json:"cwd"`
	Env             []string  `json:"env"`
	Stdin           string    `json:"stdin,omitempty"`
	StdinTruncated  bool      `json:"stdin_truncated,omitempty"`
	Stdout          string    `json:"stdout,omitempty"`
	StdoutTruncated bool      `json:"stdout_truncated,omitempty"`
	Stderr          string    `json:"stderr,omitempty"`
	StderrTruncated bool      `json:"stderr_truncated,omitempty"`
	ExitCode        int       `json:"exit_code"`
	PID             int       `json:"pid,omitempty"`
	Error           string    `json:"error,omitempty"`
}

// sink is the minimal interface shared by every log destination.
//
// A sink might be a local file, syslog over the network, or anything else that
// can accept bytes.
type sink interface {
	Name() string
	Write([]byte) error
	Close() error
}

// asyncLogger is responsible for non-blocking log delivery.
//
// Callers enqueue log entries quickly, and a background goroutine serializes
// and writes them to every sink. If the queue fills up, entries are dropped
// instead of slowing down command execution.
type asyncLogger struct {
	entries    chan logEntry
	sinks      []sink
	stderr     io.Writer
	redactor   redactor
	dropped    atomic.Uint64
	workerDone chan struct{}
	closeOnce  sync.Once
}

type redactionConfig struct {
	SecretNames    []string
	SecretPrefixes []string
	SecretSuffixes []string
}

type redactor struct {
	secretNames    []string
	secretPrefixes []string
	secretSuffixes []string
}

// newAsyncLogger builds the queue and starts the background worker.
func newAsyncLogger(sinks []sink, queueSize int, stderr io.Writer, cfg redactionConfig) *asyncLogger {
	logger := &asyncLogger{
		entries:    make(chan logEntry, queueSize),
		sinks:      sinks,
		stderr:     stderr,
		redactor:   newRedactor(cfg),
		workerDone: make(chan struct{}),
	}

	go logger.run()

	return logger
}

// Log tries to enqueue an entry without blocking.
//
// The select/default pattern is what makes this non-blocking: if the channel is
// full, we immediately drop the entry and increment a counter.
func (l *asyncLogger) Log(entry logEntry) {
	entry = l.redactor.redactLogEntry(entry)

	select {
	case l.entries <- entry:
	default:
		l.dropped.Add(1)
	}
}

// redactLogEntry removes sensitive env values from the log entry before the
// entry is queued for asynchronous delivery.
//
// There is no reliable runtime API that says "this environment variable came
// from GitHub Secrets". Instead, we use a practical heuristic: mask variables
// whose names strongly suggest secret material, then scrub those values from the
// rest of the captured fields.
func (r redactor) redactLogEntry(entry logEntry) logEntry {
	secretValues := make([]string, 0)
	redactedEnv := make([]string, 0, len(entry.Env))

	for _, item := range entry.Env {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			redactedEnv = append(redactedEnv, item)
			continue
		}

		if r.shouldMaskEnvVar(key) {
			redactedEnv = append(redactedEnv, key+"=[MASKED]")
			if shouldScrubValue(value) {
				secretValues = append(secretValues, value)
			}
			continue
		}

		redactedEnv = append(redactedEnv, item)
	}

	entry.Env = redactedEnv
	entry.RawCommand = scrubSecrets(entry.RawCommand, secretValues)
	entry.Stdin = scrubSecrets(entry.Stdin, secretValues)
	entry.Stdout = scrubSecrets(entry.Stdout, secretValues)
	entry.Stderr = scrubSecrets(entry.Stderr, secretValues)
	entry.Error = scrubSecrets(entry.Error, secretValues)

	if len(entry.Args) > 0 {
		redactedArgs := make([]string, len(entry.Args))
		for index, arg := range entry.Args {
			redactedArgs[index] = scrubSecrets(arg, secretValues)
		}
		entry.Args = redactedArgs
	}

	return entry
}

func scrubSecrets(value string, secretValues []string) string {
	redacted := value
	for _, secret := range secretValues {
		redacted = strings.ReplaceAll(redacted, secret, "[MASKED]")
	}
	return redacted
}

func shouldScrubValue(value string) bool {
	// Avoid turning very short values into global replacements because that can
	// accidentally redact unrelated log text.
	return len(value) >= 4
}

func newRedactor(cfg redactionConfig) redactor {
	return redactor{
		secretNames:    normalizeMatchers(cfg.SecretNames),
		secretPrefixes: normalizeMatchers(cfg.SecretPrefixes),
		secretSuffixes: normalizeMatchers(cfg.SecretSuffixes),
	}
}

func normalizeMatchers(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.ToUpper(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; !ok {
			seen[trimmed] = struct{}{}
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}

func (r redactor) shouldMaskEnvVar(key string) bool {
	normalized := strings.ToUpper(key)

	if normalized == "GITHUB_TOKEN" {
		return true
	}

	for _, name := range r.secretNames {
		if normalized == name {
			return true
		}
	}

	for _, prefix := range r.secretPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}

	for _, suffix := range r.secretSuffixes {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}

	sensitiveMarkers := []string{
		"TOKEN",
		"SECRET",
		"PASSWORD",
		"PASS",
		"PRIVATE_KEY",
		"API_KEY",
		"ACCESS_KEY",
		"SECRET_KEY",
		"CREDENTIAL",
		"CREDENTIALS",
		"AUTH",
		"PAT",
	}

	for _, marker := range sensitiveMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}

	return false
}

// Close stops accepting new entries and waits for the worker to flush the queue.
//
// We use sync.Once so Close is safe to call multiple times.
func (l *asyncLogger) Close(timeout time.Duration) error {
	var closeErr error

	l.closeOnce.Do(func() {
		// Closing the channel tells the worker there will be no more entries.
		close(l.entries)

		select {
		case <-l.workerDone:
		case <-time.After(timeout):
			// We cap shutdown time because logging is important, but a stuck sink
			// should not hang the whole application forever.
			closeErr = fmt.Errorf("timed out while flushing log queue after %s", timeout)
		}

		if dropped := l.dropped.Load(); dropped > 0 {
			fmt.Fprintf(l.stderr, "pipery: dropped %d log entries because the async queue was full\n", dropped)
		}
	})

	return closeErr
}

// run is the background worker loop.
func (l *asyncLogger) run() {
	defer close(l.workerDone)
	defer l.closeSinks()

	for entry := range l.entries {
		// Each sink receives one JSON line per entry.
		payload, err := json.Marshal(entry)
		if err != nil {
			fmt.Fprintf(l.stderr, "pipery: failed to encode log entry: %v\n", err)
			continue
		}

		payload = append(payload, '\n')

		for _, currentSink := range l.sinks {
			if err := currentSink.Write(payload); err != nil {
				fmt.Fprintf(l.stderr, "pipery: failed to write log entry to %s: %v\n", currentSink.Name(), err)
			}
		}
	}
}

// closeSinks releases any resources owned by the configured sinks.
func (l *asyncLogger) closeSinks() {
	for _, currentSink := range l.sinks {
		if err := currentSink.Close(); err != nil {
			fmt.Fprintf(l.stderr, "pipery: failed to close %s: %v\n", currentSink.Name(), err)
		}
	}
}
