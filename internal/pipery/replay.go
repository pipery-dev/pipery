package pipery

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/guptarohit/asciigraph"
	"github.com/pmezard/go-difflib/difflib"
)

type replayTrace struct {
	Path    string
	Entries []logEntry
}

type replayCommandResult struct {
	Index      int
	RawCommand string
	ExitCode   int
	Stdout     string
	Stderr     string
}

func loadReplayTrace(path string) (replayTrace, error) {
	file, err := os.Open(path)
	if err != nil {
		return replayTrace{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Allow large captured outputs to be validated and replayed.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	trace := replayTrace{Path: path}
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			return replayTrace{}, fmt.Errorf("psh: replay log %q contains an empty line at %d", path, lineNumber)
		}

		entry, err := decodeReplayEntry(line)
		if err != nil {
			return replayTrace{}, fmt.Errorf("psh: replay log %q line %d: %w", path, lineNumber, err)
		}

		trace.Entries = append(trace.Entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return replayTrace{}, err
	}
	if len(trace.Entries) == 0 {
		return replayTrace{}, fmt.Errorf("psh: replay log %q is empty", path)
	}

	return trace, nil
}

func decodeReplayEntry(line []byte) (logEntry, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return logEntry{}, err
	}

	required := []string{
		"timestamp",
		"started_at",
		"finished_at",
		"duration",
		"duration_ms",
		"mode",
		"builtin",
		"command",
		"raw_command",
		"cwd",
		"env",
		"exit_code",
	}
	for _, field := range required {
		if _, ok := raw[field]; !ok {
			return logEntry{}, fmt.Errorf("missing required field %q", field)
		}
	}

	var entry logEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return logEntry{}, err
	}

	if entry.Timestamp.IsZero() || entry.StartedAt.IsZero() || entry.FinishedAt.IsZero() {
		return logEntry{}, errors.New("timestamp, started_at, and finished_at must be valid RFC3339 timestamps")
	}
	if entry.Duration == "" {
		return logEntry{}, errors.New("duration must be non-empty")
	}
	if entry.DurationMillis < 0 {
		return logEntry{}, errors.New("duration_ms must be zero or greater")
	}
	if entry.Mode == "" {
		return logEntry{}, errors.New("mode must be non-empty")
	}
	if entry.Command == "" {
		return logEntry{}, errors.New("command must be non-empty")
	}
	if entry.RawCommand == "" {
		return logEntry{}, errors.New("raw_command must be non-empty")
	}
	if entry.Cwd == "" {
		return logEntry{}, errors.New("cwd must be non-empty")
	}
	if len(entry.Env) == 0 {
		return logEntry{}, errors.New("env must contain at least one variable")
	}
	for _, item := range entry.Env {
		if !strings.Contains(item, "=") {
			return logEntry{}, fmt.Errorf("env item %q must have KEY=VALUE format", item)
		}
	}
	for _, item := range entry.BeforeEnv {
		if !strings.Contains(item, "=") {
			return logEntry{}, fmt.Errorf("before_env item %q must have KEY=VALUE format", item)
		}
	}
	if entry.BeforeCwd == "" {
		entry.BeforeCwd = entry.Cwd
	}
	if len(entry.BeforeEnv) == 0 {
		entry.BeforeEnv = append([]string(nil), entry.Env...)
	}

	return entry, nil
}

func ensureComparableReplayTraces(traces []replayTrace) error {
	if len(traces) < 2 {
		return nil
	}

	baseline := traces[0]
	for _, candidate := range traces[1:] {
		if len(candidate.Entries) != len(baseline.Entries) {
			return fmt.Errorf("psh: replay log %q has %d entries, expected %d like %q", candidate.Path, len(candidate.Entries), len(baseline.Entries), baseline.Path)
		}

		for index := range baseline.Entries {
			left := baseline.Entries[index]
			right := candidate.Entries[index]
			if left.Builtin != right.Builtin ||
				left.Command != right.Command ||
				left.RawCommand != right.RawCommand ||
				!slices.Equal(left.Args, right.Args) {
				return fmt.Errorf(
					"psh: replay log %q differs from %q at command %d (%q vs %q)",
					candidate.Path,
					baseline.Path,
					index+1,
					left.RawCommand,
					right.RawCommand,
				)
			}
		}
	}

	return nil
}

func nextReplayLogPath(path string) (string, error) {
	for attempt := 1; attempt < 10_000; attempt++ {
		candidate := path + "." + strconv.Itoa(attempt)
		_, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}

	return "", fmt.Errorf("psh: could not find an available replay log path for %q", path)
}

func replayLogPath(cfg config, templatePath string) (string, error) {
	if cfg.ReplayLogFile != "" {
		return cfg.ReplayLogFile, nil
	}
	return nextReplayLogPath(templatePath)
}

func replaySequence(template replayTrace, cfg config, logPath string) (runSummary, error) {
	sinks, err := buildReplaySinks(logPath)
	if err != nil {
		return runSummary{}, err
	}

	logger := newAsyncLogger(sinks, cfg.QueueSize, io.Discard, redactionConfig{})
	defer logger.Close(cfg.FlushTimeout)

	runStartedAt := time.Now()
	lastExitCode := 0
	failureCount := 0

	for _, recorded := range template.Entries {
		sessionEnv := append([]string(nil), recorded.BeforeEnv...)
		currentSession, err := newSession(sessionConfig{
			Shell:           cfg.Shell,
			CWD:             recorded.BeforeCwd,
			Env:             sessionEnv,
			Stdout:          io.Discard,
			Stderr:          io.Discard,
			Logger:          logger,
			MaxCaptureBytes: cfg.MaxCaptureBytes,
			Prompt:          cfg.Prompt,
			FailOnError:     cfg.FailOnError,
		})
		if err != nil {
			return runSummary{}, err
		}

		input := io.Reader(nil)
		if recorded.Stdin != "" {
			input = strings.NewReader(recorded.Stdin)
		}

		var result executionResult
		switch {
		case recorded.Mode == "direct" && !recorded.Builtin:
			result, err = currentSession.runDirectCommand(recorded.Command, recorded.Args, input, "replay")
		default:
			result, _, err = currentSession.runLine(recorded.RawCommand, lineRunOptions{
				allowBuiltins: true,
				input:         input,
				mode:          "replay",
			})
		}
		if err != nil {
			return runSummary{}, err
		}

		lastExitCode = result.ExitCode
		if result.ExitCode != 0 {
			failureCount++
		}
		if cfg.FailOnError && result.ExitCode != 0 {
			break
		}
	}

	if err := logger.Close(cfg.FlushTimeout); err != nil {
		return runSummary{}, err
	}

	return runSummary{
		Mode:       "replay",
		StartedAt:  runStartedAt,
		FinishedAt: time.Now(),
		ExitCode:   lastExitCode,
		Session: sessionSummary{
			CommandCount: len(template.Entries),
			FailureCount: failureCount,
		},
	}, nil
}

func buildReplaySinks(logPath string) ([]sink, error) {
	fileSink, err := newFileSink(logPath)
	if err != nil {
		return nil, err
	}
	return []sink{fileSink}, nil
}

func renderReplayComparison(traces []replayTrace) string {
	var builder strings.Builder

	fmt.Fprintf(&builder, "psh replay comparison\n")
	for _, trace := range traces {
		totalDuration := int64(0)
		for _, entry := range trace.Entries {
			totalDuration += entry.DurationMillis
		}
		fmt.Fprintf(&builder, "run: %s commands=%d total_duration_ms=%d\n", trace.Path, len(trace.Entries), totalDuration)
	}
	builder.WriteString("\n")

	builder.WriteString(renderOutputDiffs(traces))
	builder.WriteString("\n")
	builder.WriteString(renderTimingChart(traces))

	return builder.String()
}

func renderOutputDiffs(traces []replayTrace) string {
	if len(traces) < 2 {
		return "no comparisons available\n"
	}

	var builder strings.Builder
	baseline := traces[0]
	diffCount := 0

	for traceIndex := 1; traceIndex < len(traces); traceIndex++ {
		candidate := traces[traceIndex]
		for entryIndex := range baseline.Entries {
			left := baseline.Entries[entryIndex]
			right := candidate.Entries[entryIndex]

			if left.ExitCode != right.ExitCode {
				diffCount++
				fmt.Fprintf(
					&builder,
					"command %d exit code differs: %s=%d %s=%d (%s)\n",
					entryIndex+1,
					baseline.Path,
					left.ExitCode,
					candidate.Path,
					right.ExitCode,
					left.RawCommand,
				)
			}

			if left.Stdout != right.Stdout {
				diffCount++
				builder.WriteString(unifiedDiff(
					fmt.Sprintf("%s:%d stdout", baseline.Path, entryIndex+1),
					fmt.Sprintf("%s:%d stdout", candidate.Path, entryIndex+1),
					left.Stdout,
					right.Stdout,
				))
			}

			if left.Stderr != right.Stderr {
				diffCount++
				builder.WriteString(unifiedDiff(
					fmt.Sprintf("%s:%d stderr", baseline.Path, entryIndex+1),
					fmt.Sprintf("%s:%d stderr", candidate.Path, entryIndex+1),
					left.Stderr,
					right.Stderr,
				))
			}
		}
	}

	if diffCount == 0 {
		return "outputs match across all compared runs\n"
	}

	return builder.String()
}

func unifiedDiff(fromName, toName, fromText, toText string) string {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(fromText),
		B:        difflib.SplitLines(toText),
		FromFile: fromName,
		ToFile:   toName,
		Context:  3,
	})
	if err != nil {
		return fmt.Sprintf("--- %s\n+++ %s\n-diff generation failed: %v\n", fromName, toName, err)
	}

	if diff == "" {
		return ""
	}

	return diff
}

func renderTimingChart(traces []replayTrace) string {
	if len(traces) == 0 {
		return ""
	}

	series := make([][]float64, 0, len(traces))
	labels := make([]string, 0, len(traces))
	for _, trace := range traces {
		durations := make([]float64, 0, len(trace.Entries))
		for _, entry := range trace.Entries {
			durations = append(durations, float64(entry.DurationMillis))
		}
		series = append(series, durations)
		labels = append(labels, filepath.Base(trace.Path))
	}

	var builder strings.Builder
	builder.WriteString("command duration chart (ms)\n")
	for index, label := range labels {
		fmt.Fprintf(&builder, "series %d: %s\n", index+1, label)
	}
	builder.WriteString(asciigraph.PlotMany(
		series,
		asciigraph.Caption("per-command duration"),
		asciigraph.Height(12),
	))
	builder.WriteString("\n")

	return builder.String()
}
