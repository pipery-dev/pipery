package pipery

import (
	"encoding/json"
	"fmt"
	"io"
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
	dropped    atomic.Uint64
	workerDone chan struct{}
	closeOnce  sync.Once
}

// newAsyncLogger builds the queue and starts the background worker.
func newAsyncLogger(sinks []sink, queueSize int, stderr io.Writer) *asyncLogger {
	logger := &asyncLogger{
		entries:    make(chan logEntry, queueSize),
		sinks:      sinks,
		stderr:     stderr,
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
	select {
	case l.entries <- entry:
	default:
		l.dropped.Add(1)
	}
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
