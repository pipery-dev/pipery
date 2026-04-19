# Core Package Guide

`internal/pipery` contains the main application logic behind `psh`.

## Important files

- `app.go`: top-level application orchestration and end-of-run summary output
- `config.go`: config model, defaults, flag parsing, YAML loading, and environment overrides
- `session.go`: command execution, built-ins, fail-fast behavior, and session state
- `logger.go`: async structured logging and redaction
- `github_secrets.go`: GitHub Actions secret-name discovery for additional output masking
- `sinks.go`: file and syslog sink implementations
- `capped_buffer.go`: bounded capture buffers for stdin/stdout/stderr logging

## Execution flow

A typical run goes through these stages:

1. `parseArgs` resolves runtime config and the requested execution mode.
2. `App.Run` builds the sinks and async logger.
3. `newSession` initializes cwd, env, prompt, and fail-fast behavior.
4. The session executes:
   - direct commands through `--`
   - repeated `-c` shell commands
   - interactive or piped stdin sessions
5. Each command emits a structured log entry.
6. The app prints a human-readable run summary to stderr.

## Design principles

- Non-blocking logging: command execution should not wait on disk or syslog writes.
- Bounded capture: stdout/stderr/stdin logs are size-limited.
- Layered config: CLI overrides env, env overrides YAML, YAML overrides defaults.
- Safe redaction: secret values are scrubbed from command text and captured outputs.
- Predictable exit codes: the process exit code follows the session outcome.
