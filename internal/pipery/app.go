package pipery

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// App is the top-level coordinator for the CLI.
//
// We keep the standard streams as fields so they can be replaced in tests with
// in-memory buffers instead of real terminal/file handles.
type App struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

type sessionSummary struct {
	CommandCount int
	FailureCount int
}

type runSummary struct {
	Mode       string
	StartedAt  time.Time
	FinishedAt time.Time
	ExitCode   int
	Session    sessionSummary
}

// NewApp wires the three standard streams into an App instance.
func NewApp(stdin io.Reader, stdout io.Writer, stderr io.Writer) *App {
	return &App{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}
}

// Run is the main control flow for the CLI.
//
// It does four jobs:
// 1. Parse flags and decide which mode the user requested.
// 2. Build the configured log sinks and async logger.
// 3. Create a session that owns shell state like cwd/env.
// 4. Execute either a direct command, a list of -c commands, or the REPL.
func (a *App) Run(args []string) (int, error) {
	runStartedAt := time.Now()

	cfg, shellCommands, directCommand, showHelp, err := parseArgs(args, a.stderr)
	if err != nil {
		return 1, err
	}

	if showHelp {
		return 0, nil
	}

	sinks, err := buildSinks(cfg)
	if err != nil {
		return 1, err
	}

	// The logger runs in the background so command execution does not have to
	// wait for every log write to finish.
	redaction := buildRedactionConfig(cfg, os.Environ(), http.DefaultClient)
	logger := newAsyncLogger(sinks, cfg.QueueSize, a.stderr, redaction)
	defer func() {
		// We still try to flush on shutdown so we do not lose logs unnecessarily.
		if closeErr := logger.Close(cfg.FlushTimeout); closeErr != nil {
			fmt.Fprintln(a.stderr, closeErr)
		}
	}()

	// A session carries mutable shell-like state across commands. For example,
	// `cd` and `export` need to affect later commands in the same session.
	currentSession, err := newSession(sessionConfig{
		Shell:           cfg.Shell,
		Env:             os.Environ(),
		Stdout:          a.stdout,
		Stderr:          a.stderr,
		Logger:          logger,
		MaxCaptureBytes: cfg.MaxCaptureBytes,
		Prompt:          cfg.Prompt,
	})
	if err != nil {
		return 1, err
	}

	finishRun := func(mode string, exitCode int) (int, error) {
		a.printRunSummary(runSummary{
			Mode:       mode,
			StartedAt:  runStartedAt,
			FinishedAt: time.Now(),
			ExitCode:   exitCode,
			Session:    currentSession.summary(),
		})
		return exitCode, nil
	}

	switch {
	case len(directCommand) > 0:
		// Direct mode executes a real program directly, for example:
		//   pipery -- ls -la
		//
		// If stdin is coming from a pipe or redirected file, we forward it to the
		// child process and capture it in the log entry.
		var input io.Reader
		if !readerIsTerminal(a.stdin) {
			input = a.stdin
		}
		result, err := currentSession.runDirectCommand(directCommand[0], directCommand[1:], input, "direct")
		if err != nil {
			return 1, err
		}
		return finishRun("direct", result.ExitCode)
	case len(shellCommands) > 0:
		// Shell command mode executes each -c string in order through the
		// configured shell. This gives us shell syntax like pipes, redirects,
		// variable expansion, and multiple statements.
		lastExitCode := 0
		var input io.Reader

		// We only attach piped stdin to the first command. After a reader has been
		// consumed once, there is nothing left for later commands to read.
		if len(shellCommands) == 1 && !readerIsTerminal(a.stdin) {
			input = a.stdin
		}

		for index, commandLine := range shellCommands {
			lineInput := io.Reader(nil)
			if index == 0 {
				lineInput = input
			}

			result, shouldExit, err := currentSession.runLine(commandLine, lineRunOptions{
				// Built-ins such as `cd` and `export` are allowed in shell mode so
				// repeated -c flags can preserve state between commands.
				allowBuiltins: true,
				input:         lineInput,
				mode:          "shell",
			})
			if err != nil {
				return 1, err
			}

			lastExitCode = result.ExitCode
			if shouldExit {
				return finishRun("shell", result.ExitCode)
			}
		}

		return finishRun("shell", lastExitCode)
	default:
		// With no command arguments, pipery behaves like an interactive shell when
		// stdin is a terminal, or like a line-by-line script runner when commands
		// are piped into stdin.
		mode := "interactive"
		if !readerIsTerminal(a.stdin) {
			mode = "stdin"
		}

		exitCode, err := currentSession.runREPL(a.stdin, mode == "interactive")
		if err != nil {
			return 1, err
		}
		return finishRun(mode, exitCode)
	}
}

func (a *App) printRunSummary(summary runSummary) {
	duration := summary.FinishedAt.Sub(summary.StartedAt)
	fmt.Fprintf(
		a.stderr,
		"pipery summary: mode=%s commands=%d failed=%d exit_code=%d started_at=%s finished_at=%s duration=%s\n",
		summary.Mode,
		summary.Session.CommandCount,
		summary.Session.FailureCount,
		summary.ExitCode,
		summary.StartedAt.Format(time.RFC3339),
		summary.FinishedAt.Format(time.RFC3339),
		duration.Round(time.Millisecond),
	)
}

// buildSinks turns the parsed config into concrete log sink implementations.
func buildSinks(cfg config) ([]sink, error) {
	var sinks []sink

	if cfg.LogFile != "" {
		fileSink, err := newFileSink(cfg.LogFile)
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, fileSink)
	}

	if cfg.SyslogTarget != "" {
		syslogSink, err := newSyslogSink(cfg.SyslogTarget, cfg.SyslogTag)
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, syslogSink)
	}

	if len(sinks) == 0 {
		return nil, errors.New("pipery: at least one log sink is required, set -log-file or -syslog")
	}

	return sinks, nil
}

// readerIsTerminal is a small helper that answers: "is this reader a real
// terminal device?"
//
// We use this to decide whether to show a prompt and whether stdin should be
// forwarded into a child process.
func readerIsTerminal(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}

	info, err := file.Stat()
	if err != nil {
		return false
	}

	return (info.Mode() & os.ModeCharDevice) != 0
}
