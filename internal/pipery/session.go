package pipery

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chzyer/readline"
)

// session represents one logical shell session.
//
// It owns the mutable state we want to preserve across commands, such as the
// current working directory and environment variables.
type session struct {
	shell           string
	cwd             string
	env             map[string]string
	stdout          io.Writer
	stderr          io.Writer
	logger          *asyncLogger
	maxCaptureBytes int
	prompt          string
	failOnError     bool
	commandCount    int
	failureCount    int
}

// executionResult is a deliberately small result object. Right now we only need
// the exit code, but wrapping it in a struct leaves room for future fields.
type executionResult struct {
	ExitCode int
}

// lineRunOptions controls how a single command line should be executed.
type lineRunOptions struct {
	allowBuiltins bool
	input         io.Reader
	mode          string
}

// sessionConfig is the constructor input for a session.
type sessionConfig struct {
	Shell           string
	CWD             string
	Env             []string
	Stdout          io.Writer
	Stderr          io.Writer
	Logger          *asyncLogger
	MaxCaptureBytes int
	Prompt          string
	FailOnError     bool
}

// newSession builds a session and fills in sensible defaults.
func newSession(cfg sessionConfig) (*session, error) {
	cwd := cfg.CWD
	if cwd == "" {
		// If no cwd is provided, start from the process's current directory.
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	return &session{
		shell:           cfg.Shell,
		cwd:             cwd,
		env:             envSliceToMap(cfg.Env),
		stdout:          cfg.Stdout,
		stderr:          cfg.Stderr,
		logger:          cfg.Logger,
		maxCaptureBytes: cfg.MaxCaptureBytes,
		prompt:          cfg.Prompt,
		failOnError:     cfg.FailOnError,
	}, nil
}

// runREPL executes the interactive read-eval-print loop.
//
// This is line-oriented input rather than a full pseudo-terminal shell. That
// keeps the implementation simpler while still supporting useful workflows.
func (s *session) runREPL(input io.Reader, showPrompt bool) (int, error) {
	if showPrompt {
		if exitCode, err, handled := s.runInteractiveREPL(input); handled {
			return exitCode, err
		}
	}

	reader := bufio.NewReader(input)
	lastExitCode := 0
	mode := "interactive"
	if !showPrompt {
		// When stdin is not a terminal, psh behaves more like a tiny shell
		// script runner: each incoming line is treated as a command.
		mode = "stdin"
	}

	for {
		if showPrompt {
			// Prompts are only useful when a human is looking at a terminal. In
			// non-interactive mode they would just pollute stdout.
			if _, err := fmt.Fprint(s.stdout, s.prompt); err != nil {
				return 1, err
			}
		}

		line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) && strings.TrimSpace(line) == "" {
			// Hitting EOF on an empty line means the input stream is finished.
			if showPrompt {
				if _, writeErr := fmt.Fprintln(s.stdout); writeErr != nil {
					return 1, writeErr
				}
			}
			return lastExitCode, nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return 1, err
		}

		// Every non-empty line goes through the same command execution path used
		// by -c mode, which keeps behavior consistent.
		result, shouldExit, runErr := s.runLine(line, lineRunOptions{
			allowBuiltins: true,
			mode:          mode,
		})
		if runErr != nil {
			return 1, runErr
		}

		if result.ExitCode != 0 || strings.TrimSpace(line) != "" {
			lastExitCode = result.ExitCode
		}

		if s.failOnError && result.ExitCode != 0 {
			return result.ExitCode, nil
		}

		if shouldExit {
			return result.ExitCode, nil
		}

		if errors.Is(err, io.EOF) {
			// If the final line did not end with a newline, we still execute it
			// once and then stop.
			return lastExitCode, nil
		}
	}
}

func (s *session) runInteractiveREPL(input io.Reader) (int, error, bool) {
	stdinFile, ok := input.(*os.File)
	if !ok {
		return 0, nil, false
	}

	rl, err := readline.NewEx(&readline.Config{
		Prompt:                 s.prompt,
		AutoComplete:           newShellAutoCompleter(s),
		InterruptPrompt:        "^C",
		EOFPrompt:              "exit",
		Stdin:                  stdinFile,
		Stdout:                 s.stdout,
		Stderr:                 s.stderr,
		DisableAutoSaveHistory: true,
		HistoryLimit:           -1,
	})
	if err != nil {
		return 0, err, true
	}
	defer rl.Close()

	lastExitCode := 0
	for {
		line, err := rl.Readline()
		if err != nil {
			if errors.Is(err, readline.ErrInterrupt) {
				if _, writeErr := io.WriteString(s.stdout, "^C\n"); writeErr != nil {
					return 1, writeErr, true
				}
				continue
			}

			if errors.Is(err, io.EOF) {
				if _, writeErr := io.WriteString(s.stdout, "\n"); writeErr != nil {
					return 1, writeErr, true
				}
				return lastExitCode, nil, true
			}

			return 1, err, true
		}

		result, shouldExit, runErr := s.runLine(line, lineRunOptions{
			allowBuiltins: true,
			mode:          "interactive",
		})
		if runErr != nil {
			return 1, runErr, true
		}

		if result.ExitCode != 0 || strings.TrimSpace(line) != "" {
			lastExitCode = result.ExitCode
		}

		if s.failOnError && result.ExitCode != 0 {
			return result.ExitCode, nil, true
		}

		if shouldExit {
			return result.ExitCode, nil, true
		}
	}
}

// runLine handles one logical command line.
//
// It trims whitespace, dispatches built-ins if allowed, and otherwise falls
// back to executing the line through the configured shell.
func (s *session) runLine(line string, opts lineRunOptions) (executionResult, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return executionResult{ExitCode: 0}, false, nil
	}

	if opts.allowBuiltins {
		// Built-ins run inside psh itself because they need to mutate session
		// state. For example, an external `cd` process cannot change our cwd.
		result, handled, shouldExit, err := s.tryBuiltin(trimmed, opts.mode)
		if err != nil {
			return executionResult{}, false, err
		}
		if handled {
			s.recordResult(result)
			return result, shouldExit, nil
		}
	}

	result, err := s.runShellCommand(trimmed, opts)
	s.recordResult(result)
	return result, false, err
}

// runDirectCommand executes a real program directly without wrapping it in a
// shell. That means argument boundaries are explicit and not re-parsed by a
// shell.
func (s *session) runDirectCommand(command string, args []string, input io.Reader, mode string) (executionResult, error) {
	result, err := s.runExternal(command, args, input, mode, joinCommandLine(command, args))
	s.recordResult(result)
	return result, err
}

// runShellCommand executes one command line through the configured shell.
func (s *session) runShellCommand(commandLine string, opts lineRunOptions) (executionResult, error) {
	return s.runExternal(s.shell, shellArgs(commandLine), opts.input, opts.mode, commandLine)
}

// runExternal is the shared implementation for both direct execution and shell
// execution.
//
// It starts the child process, streams stdout/stderr to the user, captures
// bounded copies for logging, waits for completion, and emits one log entry.
func (s *session) runExternal(command string, args []string, input io.Reader, mode string, rawCommand string) (executionResult, error) {
	startedAt := time.Now()

	// These capped buffers let us keep useful log data without risking unlimited
	// memory usage for very chatty commands.
	stdoutCapture := newCappedBuffer(s.maxCaptureBytes)
	stderrCapture := newCappedBuffer(s.maxCaptureBytes)
	stdinCapture := newCappedBuffer(s.maxCaptureBytes)

	cmd := exec.Command(command, args...)
	cmd.Dir = s.cwd
	cmd.Env = mapToSortedEnvSlice(s.env)

	// io.MultiWriter duplicates each output stream:
	// - one copy goes straight to the user's terminal
	// - another copy is retained for the structured log
	cmd.Stdout = io.MultiWriter(s.stdout, stdoutCapture)
	cmd.Stderr = io.MultiWriter(s.stderr, stderrCapture)

	var stdinPipe io.WriteCloser
	var copyDone chan copyResult
	if input != nil {
		// Stdin needs special handling because exec.Cmd wants a writer end from us
		// while our source is an io.Reader.
		var err error
		stdinPipe, err = cmd.StdinPipe()
		if err != nil {
			return executionResult{}, err
		}
	}

	if err := cmd.Start(); err != nil {
		// If the process never started, we still log the failure so the caller has
		// an audit trail for "command not found" or similar startup problems.
		exitCode := deriveExitCode(err)
		finishedAt := time.Now()

		s.logger.Log(logEntry{
			Timestamp:      finishedAt,
			StartedAt:      startedAt,
			FinishedAt:     finishedAt,
			Duration:       finishedAt.Sub(startedAt).String(),
			DurationMillis: finishedAt.Sub(startedAt).Milliseconds(),
			Mode:           mode,
			Command:        command,
			Args:           args,
			RawCommand:     rawCommand,
			Cwd:            s.cwd,
			Env:            mapToSortedEnvSlice(s.env),
			ExitCode:       exitCode,
			Error:          err.Error(),
		})

		return executionResult{ExitCode: exitCode}, nil
	}

	if stdinPipe != nil {
		// Copy stdin in a goroutine so the child can run at the same time that we
		// stream data into it.
		copyDone = make(chan copyResult, 1)
		go copyInput(stdinPipe, input, stdinCapture, copyDone)
	}

	// Wait blocks until the child exits.
	waitErr := cmd.Wait()
	finishedAt := time.Now()
	exitCode := deriveExitCode(waitErr)
	var errs []string
	if waitErr != nil && exitCode < 0 {
		// Exit errors with a real exit code are already represented by exitCode.
		// We only keep the string form for unusual failures.
		errs = append(errs, waitErr.Error())
	}

	if copyDone != nil {
		// Wait for stdin copying to finish so the log entry accurately reflects any
		// input-copy error.
		result := <-copyDone
		if result.err != nil {
			errs = append(errs, result.err.Error())
		}
	}
	entryErr := strings.Join(errs, "; ")

	entry := logEntry{
		Timestamp:       finishedAt,
		StartedAt:       startedAt,
		FinishedAt:      finishedAt,
		Duration:        finishedAt.Sub(startedAt).String(),
		DurationMillis:  finishedAt.Sub(startedAt).Milliseconds(),
		Mode:            mode,
		Command:         command,
		Args:            args,
		RawCommand:      rawCommand,
		Cwd:             s.cwd,
		Env:             mapToSortedEnvSlice(s.env),
		Stdin:           stdinCapture.String(),
		StdinTruncated:  stdinCapture.Truncated(),
		Stdout:          stdoutCapture.String(),
		StdoutTruncated: stdoutCapture.Truncated(),
		Stderr:          stderrCapture.String(),
		StderrTruncated: stderrCapture.Truncated(),
		ExitCode:        exitCode,
		PID:             cmd.Process.Pid,
		Error:           entryErr,
	}

	s.logger.Log(entry)

	return executionResult{ExitCode: exitCode}, nil
}

func (s *session) recordResult(result executionResult) {
	s.commandCount++
	if result.ExitCode != 0 {
		s.failureCount++
	}
}

func (s *session) summary() sessionSummary {
	return sessionSummary{
		CommandCount: s.commandCount,
		FailureCount: s.failureCount,
	}
}

// tryBuiltin checks whether the command should be handled by psh itself.
//
// The returned booleans mean:
// - handled: this line matched a built-in
// - shouldExit: the session should end after this built-in
func (s *session) tryBuiltin(line string, mode string) (executionResult, bool, bool, error) {
	switch {
	case line == "pwd":
		return s.runPwdBuiltin(line, mode), true, false, nil
	case line == "exit", line == "quit", strings.HasPrefix(line, "exit "), strings.HasPrefix(line, "quit "):
		result, shouldExit := s.runExitBuiltin(line, mode)
		return result, true, shouldExit, nil
	case line == "cd" || strings.HasPrefix(line, "cd "):
		return s.runCdBuiltin(line, mode), true, false, nil
	case strings.HasPrefix(line, "export "):
		return s.runExportBuiltin(line, mode), true, false, nil
	case strings.HasPrefix(line, "unset "):
		return s.runUnsetBuiltin(line, mode), true, false, nil
	default:
		return executionResult{}, false, false, nil
	}
}

// runPwdBuiltin prints the session's current working directory.
func (s *session) runPwdBuiltin(rawCommand, mode string) executionResult {
	startedAt := time.Now()
	output := s.cwd + "\n"
	_, _ = io.WriteString(s.stdout, output)
	finishedAt := time.Now()

	s.logger.Log(logEntry{
		Timestamp:      finishedAt,
		StartedAt:      startedAt,
		FinishedAt:     finishedAt,
		Duration:       finishedAt.Sub(startedAt).String(),
		DurationMillis: finishedAt.Sub(startedAt).Milliseconds(),
		Mode:           mode,
		Builtin:        true,
		Command:        "pwd",
		RawCommand:     rawCommand,
		Cwd:            s.cwd,
		Env:            mapToSortedEnvSlice(s.env),
		Stdout:         output,
		ExitCode:       0,
	})

	return executionResult{ExitCode: 0}
}

// runExitBuiltin parses an optional exit code and tells the caller whether the
// REPL / command loop should stop.
func (s *session) runExitBuiltin(rawCommand, mode string) (executionResult, bool) {
	startedAt := time.Now()
	code := 0
	fields := strings.Fields(rawCommand)
	if len(fields) > 1 {
		// We keep the syntax intentionally simple: only one numeric argument.
		parsed, err := strconv.Atoi(fields[1])
		if err != nil {
			stderr := fmt.Sprintf("psh: invalid exit code %q\n", fields[1])
			_, _ = io.WriteString(s.stderr, stderr)
			finishedAt := time.Now()

			s.logger.Log(logEntry{
				Timestamp:      finishedAt,
				StartedAt:      startedAt,
				FinishedAt:     finishedAt,
				Duration:       finishedAt.Sub(startedAt).String(),
				DurationMillis: finishedAt.Sub(startedAt).Milliseconds(),
				Mode:           mode,
				Builtin:        true,
				Command:        fields[0],
				Args:           fields[1:],
				RawCommand:     rawCommand,
				Cwd:            s.cwd,
				Env:            mapToSortedEnvSlice(s.env),
				Stderr:         stderr,
				ExitCode:       2,
			})

			return executionResult{ExitCode: 2}, false
		}
		code = parsed
	}

	finishedAt := time.Now()
	s.logger.Log(logEntry{
		Timestamp:      finishedAt,
		StartedAt:      startedAt,
		FinishedAt:     finishedAt,
		Duration:       finishedAt.Sub(startedAt).String(),
		DurationMillis: finishedAt.Sub(startedAt).Milliseconds(),
		Mode:           mode,
		Builtin:        true,
		Command:        fields[0],
		Args:           fields[1:],
		RawCommand:     rawCommand,
		Cwd:            s.cwd,
		Env:            mapToSortedEnvSlice(s.env),
		ExitCode:       code,
	})

	return executionResult{ExitCode: code}, true
}

// runCdBuiltin updates the session's cwd.
//
// Notice that we do not call os.Chdir here. Changing the whole process cwd
// would be a wider side effect than we need; the session's private cwd is all
// later child commands care about.
func (s *session) runCdBuiltin(rawCommand, mode string) executionResult {
	startedAt := time.Now()
	target := strings.TrimSpace(strings.TrimPrefix(rawCommand, "cd"))
	if target == "" {
		// Plain `cd` behaves like a normal shell and goes to the user's home dir.
		target, _ = os.UserHomeDir()
	}

	// These helpers make the built-in more user-friendly by supporting quotes,
	// "~", and relative paths.
	target = stripWrappingQuotes(target)
	target = expandHome(target)
	if !filepath.IsAbs(target) {
		target = filepath.Join(s.cwd, target)
	}

	resolved, err := filepath.Abs(target)
	if err == nil {
		target = resolved
	}

	// We validate the target before updating session state so bad paths do not
	// silently poison later commands.
	info, err := os.Stat(target)
	if err != nil {
		stderr := fmt.Sprintf("psh: %v\n", err)
		_, _ = io.WriteString(s.stderr, stderr)
		finishedAt := time.Now()

		s.logger.Log(logEntry{
			Timestamp:      finishedAt,
			StartedAt:      startedAt,
			FinishedAt:     finishedAt,
			Duration:       finishedAt.Sub(startedAt).String(),
			DurationMillis: finishedAt.Sub(startedAt).Milliseconds(),
			Mode:           mode,
			Builtin:        true,
			Command:        "cd",
			Args:           []string{target},
			RawCommand:     rawCommand,
			Cwd:            s.cwd,
			Env:            mapToSortedEnvSlice(s.env),
			Stderr:         stderr,
			ExitCode:       1,
		})

		return executionResult{ExitCode: 1}
	}

	if !info.IsDir() {
		stderr := fmt.Sprintf("psh: %s is not a directory\n", target)
		_, _ = io.WriteString(s.stderr, stderr)
		finishedAt := time.Now()

		s.logger.Log(logEntry{
			Timestamp:      finishedAt,
			StartedAt:      startedAt,
			FinishedAt:     finishedAt,
			Duration:       finishedAt.Sub(startedAt).String(),
			DurationMillis: finishedAt.Sub(startedAt).Milliseconds(),
			Mode:           mode,
			Builtin:        true,
			Command:        "cd",
			Args:           []string{target},
			RawCommand:     rawCommand,
			Cwd:            s.cwd,
			Env:            mapToSortedEnvSlice(s.env),
			Stderr:         stderr,
			ExitCode:       1,
		})

		return executionResult{ExitCode: 1}
	}

	s.cwd = target
	finishedAt := time.Now()
	s.logger.Log(logEntry{
		Timestamp:      finishedAt,
		StartedAt:      startedAt,
		FinishedAt:     finishedAt,
		Duration:       finishedAt.Sub(startedAt).String(),
		DurationMillis: finishedAt.Sub(startedAt).Milliseconds(),
		Mode:           mode,
		Builtin:        true,
		Command:        "cd",
		Args:           []string{target},
		RawCommand:     rawCommand,
		Cwd:            s.cwd,
		Env:            mapToSortedEnvSlice(s.env),
		ExitCode:       0,
	})

	return executionResult{ExitCode: 0}
}

// runExportBuiltin stores KEY=VALUE in the session environment so future child
// processes inherit it.
func (s *session) runExportBuiltin(rawCommand, mode string) executionResult {
	startedAt := time.Now()
	assignment := strings.TrimSpace(strings.TrimPrefix(rawCommand, "export"))
	key, value, ok := strings.Cut(assignment, "=")
	key = strings.TrimSpace(key)
	value = stripWrappingQuotes(strings.TrimSpace(value))

	if !ok || key == "" {
		// We require the classic shell-like KEY=VALUE form to keep parsing simple
		// and predictable.
		stderr := "psh: export expects KEY=VALUE\n"
		_, _ = io.WriteString(s.stderr, stderr)
		finishedAt := time.Now()

		s.logger.Log(logEntry{
			Timestamp:      finishedAt,
			StartedAt:      startedAt,
			FinishedAt:     finishedAt,
			Duration:       finishedAt.Sub(startedAt).String(),
			DurationMillis: finishedAt.Sub(startedAt).Milliseconds(),
			Mode:           mode,
			Builtin:        true,
			Command:        "export",
			RawCommand:     rawCommand,
			Cwd:            s.cwd,
			Env:            mapToSortedEnvSlice(s.env),
			Stderr:         stderr,
			ExitCode:       2,
		})

		return executionResult{ExitCode: 2}
	}

	s.env[key] = value
	finishedAt := time.Now()
	s.logger.Log(logEntry{
		Timestamp:      finishedAt,
		StartedAt:      startedAt,
		FinishedAt:     finishedAt,
		Duration:       finishedAt.Sub(startedAt).String(),
		DurationMillis: finishedAt.Sub(startedAt).Milliseconds(),
		Mode:           mode,
		Builtin:        true,
		Command:        "export",
		Args:           []string{assignment},
		RawCommand:     rawCommand,
		Cwd:            s.cwd,
		Env:            mapToSortedEnvSlice(s.env),
		ExitCode:       0,
	})

	return executionResult{ExitCode: 0}
}

// runUnsetBuiltin removes one variable from the session environment.
func (s *session) runUnsetBuiltin(rawCommand, mode string) executionResult {
	startedAt := time.Now()
	key := strings.TrimSpace(strings.TrimPrefix(rawCommand, "unset"))
	key = stripWrappingQuotes(key)

	if key == "" {
		stderr := "psh: unset expects a variable name\n"
		_, _ = io.WriteString(s.stderr, stderr)
		finishedAt := time.Now()

		s.logger.Log(logEntry{
			Timestamp:      finishedAt,
			StartedAt:      startedAt,
			FinishedAt:     finishedAt,
			Duration:       finishedAt.Sub(startedAt).String(),
			DurationMillis: finishedAt.Sub(startedAt).Milliseconds(),
			Mode:           mode,
			Builtin:        true,
			Command:        "unset",
			RawCommand:     rawCommand,
			Cwd:            s.cwd,
			Env:            mapToSortedEnvSlice(s.env),
			Stderr:         stderr,
			ExitCode:       2,
		})

		return executionResult{ExitCode: 2}
	}

	delete(s.env, key)
	finishedAt := time.Now()
	s.logger.Log(logEntry{
		Timestamp:      finishedAt,
		StartedAt:      startedAt,
		FinishedAt:     finishedAt,
		Duration:       finishedAt.Sub(startedAt).String(),
		DurationMillis: finishedAt.Sub(startedAt).Milliseconds(),
		Mode:           mode,
		Builtin:        true,
		Command:        "unset",
		Args:           []string{key},
		RawCommand:     rawCommand,
		Cwd:            s.cwd,
		Env:            mapToSortedEnvSlice(s.env),
		ExitCode:       0,
	})

	return executionResult{ExitCode: 0}
}

// copyResult is the small message sent back from the stdin-copy goroutine.
type copyResult struct {
	err error
}

// copyInput streams data from the provided reader into the child's stdin while
// also capturing a bounded copy for logging.
func copyInput(dst io.WriteCloser, src io.Reader, capture *cappedBuffer, done chan<- copyResult) {
	defer close(done)
	defer dst.Close()

	_, err := io.Copy(io.MultiWriter(dst, capture), src)
	if err != nil && !errors.Is(err, io.ErrClosedPipe) {
		done <- copyResult{err: err}
		return
	}

	done <- copyResult{}
}

// deriveExitCode normalizes different Go error shapes into a shell-like exit
// code.
func deriveExitCode(err error) int {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	if errors.Is(err, exec.ErrNotFound) {
		return 127
	}

	return 1
}

// shellArgs returns the correct "execute this command line" arguments for the
// current operating system's shell.
func shellArgs(commandLine string) []string {
	if runtime.GOOS == "windows" {
		return []string{"/C", commandLine}
	}

	return []string{"-lc", commandLine}
}

// defaultShell chooses the shell used for REPL and -c execution.
func defaultShell() string {
	if runtime.GOOS == "windows" {
		if comspec := os.Getenv("COMSPEC"); comspec != "" {
			return comspec
		}
		return "cmd.exe"
	}

	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}

	return "/bin/sh"
}

// envSliceToMap converts the standard os.Environ() format into a map so the
// session can update variables by key.
func envSliceToMap(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		values[key] = value
	}
	return values
}

// mapToSortedEnvSlice converts the session environment back into exec.Cmd's
// expected KEY=VALUE slice format.
//
// We sort the output to make logs and tests deterministic.
func mapToSortedEnvSlice(env map[string]string) []string {
	items := make([]string, 0, len(env))
	for key, value := range env {
		items = append(items, key+"="+value)
	}
	sort.Strings(items)
	return items
}

// stripWrappingQuotes removes one matching pair of surrounding quotes if they
// exist. This is a lightweight convenience helper, not a full shell parser.
func stripWrappingQuotes(value string) string {
	if len(value) < 2 {
		return value
	}

	if (value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"') {
		return value[1 : len(value)-1]
	}

	return value
}

// expandHome turns "~" and "~/..." into an absolute path inside the current
// user's home directory.
func expandHome(value string) string {
	if value == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}

	if strings.HasPrefix(value, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(value, "~/"))
		}
	}

	return value
}

// joinCommandLine reconstructs a shell-safe-ish human-readable command string
// from a program name and argument list.
//
// This is mainly used for logging the raw command in direct mode.
func joinCommandLine(command string, args []string) string {
	parts := []string{quoteArg(command)}
	for _, arg := range args {
		parts = append(parts, quoteArg(arg))
	}
	return strings.Join(parts, " ")
}

// quoteArg adds quotes when an argument contains characters that would be
// ambiguous in a shell command line.
func quoteArg(value string) string {
	if value == "" {
		return `""`
	}

	if strings.ContainsAny(value, " \t\n\"'\\$&|;()<>") {
		return strconv.Quote(value)
	}

	return value
}
