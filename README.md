# pipery-shell

`pipery-shell` is a Go CLI that mediates shell command execution and records each run as structured JSON. The shipped binary name is `psh`.

Current version: `0.1.0` from [VERSION](VERSION)

It supports:

- Interactive, line-oriented shell mode
- One-shot shell commands with repeated `-c`
- Direct program execution with `--`
- Configuration from CLI flags, `PIPERY_*` environment variables, and YAML
- Async, non-blocking JSON logging to file
- Async mirroring to network syslog over `udp://` or `tcp://`
- Exit code, execution time, environment, stdin, stdout, and stderr capture

## Build

```bash
go build -o psh ./cmd/pipery
```

Container image:

```bash
docker build -t psh:base .
```

GitHub Actions:

- Runs lint, tests, Go build, and Docker build on pushes and pull requests
- Reads the release version from `VERSION`
- Publishes multi-arch Docker images for `linux/amd64` and `linux/arm64` to `ghcr.io/pipery-dev/pipery:0.1.0`, `:v0.1.0`, and `:latest` on pushes to `main`
- Creates a GitHub release and uploads a Linux AMD64 tarball on pushes to `main`
- When `psh` runs inside GitHub Actions and `GITHUB_TOKEN` can read Actions secrets metadata, it fetches repository secret names and uses them for additional masking

## Usage

Interactive mode:

```bash
./psh
```

Pipe commands into stdin:

```bash
echo "echo Hi" | ./psh
printf 'echo one\npwd\n' | ./psh
```

Run shell commands:

```bash
./psh -c "echo hello"
./psh -c "cd /tmp" -c "pwd"
```

Run a program directly:

```bash
./psh -- ls -la
```

Run from Docker:

```bash
docker run --rm -i -v "$PWD:/workspace" pipery:base -c "echo hello"
echo "echo hi" | docker run --rm -i -v "$PWD:/workspace" pipery:base
```

Published image tags use the repo version:

```bash
docker pull ghcr.io/pipery-dev/pipery:0.1.0
docker pull ghcr.io/pipery-dev/pipery:v0.1.0
docker pull ghcr.io/pipery-dev/pipery:latest
```

Log to a file and syslog:

```bash
./psh \
  -log-file ./pipery.jsonl \
  -syslog udp://127.0.0.1:514 \
  -c "echo hello"
```

Pipe stdin through a single command and capture it:

```bash
printf 'hello\n' | ./psh -c "cat"
```

Use a YAML config file:

```bash
./psh -config ./.pipery/config.yaml
```

Or environment variables:

```bash
export PIPERY_LOG_FILE=./custom.jsonl
export PIPERY_QUEUE_SIZE=512
export PIPERY_FLUSH_TIMEOUT=5s
export PIPERY_SECRET_PREFIXES=ORG_,CI_
export PIPERY_FAIL_ON_ERROR=true
./psh -c "echo hello"
```

## Configuration

Configuration is loaded in this order:

- Built-in defaults
- YAML config file
- `PIPERY_*` environment variables
- CLI flags

If you do not pass `-config`, `psh` automatically looks for `./.pipery/config.yaml`.

Example `./.pipery/config.yaml`:

```yaml
log_file: ./pipery.jsonl
syslog: udp://127.0.0.1:514
syslog_tag: psh
queue_size: 256
max_capture_bytes: 262144
shell: /bin/zsh
prompt: "psh> "
flush_timeout: 3s
fail_on_error: false
secret_names:
  - CUSTOM_SECRET_NAME
secret_prefixes:
  - ORG_
secret_suffixes:
  - _TAIL
```

Supported environment variables:

- `PIPERY_LOG_FILE`
- `PIPERY_SYSLOG`
- `PIPERY_SYSLOG_TAG`
- `PIPERY_QUEUE_SIZE`
- `PIPERY_MAX_CAPTURE_BYTES`
- `PIPERY_SHELL`
- `PIPERY_PROMPT`
- `PIPERY_FAIL_ON_ERROR`
- `PIPERY_FLUSH_TIMEOUT`
- `PIPERY_SECRET_NAMES`
- `PIPERY_SECRET_PREFIXES`
- `PIPERY_SECRET_SUFFIXES`

## Interactive built-ins

The REPL is line-oriented rather than a full PTY shell. These built-ins persist session state across commands:

- `cd [dir]`
- `pwd`
- `export KEY=VALUE`
- `unset KEY`
- `exit [code]`
- `quit [code]`

All other lines are executed through the configured shell, which defaults to `$SHELL` or `/bin/sh`.

## Logging behavior

Logs are written asynchronously through a bounded queue so command completion is not blocked by file or syslog writes.

- Default log file: `./pipery.jsonl`
- Default queue size: `256`
- Default capture size per stream: `262144` bytes
- If the async queue fills up, new log entries are dropped and a summary is printed on shutdown
- Syslog targets accept `udp://host:port` or `tcp://host:port`
- Secret env vars are masked automatically, and you can extend the matcher set with exact names, prefixes, and suffixes
- In GitHub Actions, repository and shared organization secret names can also be discovered via the Actions secrets API; `psh` still never receives secret values from GitHub, only names, and uses matching env vars to scrub captured outputs
- Set `fail_on_error` or `-fail-on-error` to stop after the first non-zero command result, similar to shell errexit behavior for batch runs

## Flags

```text
-config             YAML config file path
-c                  run a shell command; repeat to run multiple commands
-log-file           JSONL log file path, default: ./pipery.jsonl
-syslog             syslog target, for example udp://127.0.0.1:514
-syslog-tag         syslog app tag, default: psh
-queue-size         async log queue size, default: 256
-max-capture-bytes  max bytes recorded for stdin/stdout/stderr, default: 262144
-shell              shell path used for -c and REPL execution
-prompt             interactive prompt, default: psh>
-fail-on-error      stop the session when a command exits non-zero
-flush-timeout      max time to wait for async log flush on exit, default: 3s
-secret-names       comma-separated env var names to always mask in logs
-secret-prefixes    comma-separated env var prefixes to mask in logs
-secret-suffixes    comma-separated env var suffixes to mask in logs
```

## Log format

Each command produces one JSON object per line. Example:

```json
{
  "timestamp": "2026-04-14T11:42:30.123456Z",
  "started_at": "2026-04-14T11:42:30.100000Z",
  "finished_at": "2026-04-14T11:42:30.123456Z",
  "duration": "23.456ms",
  "duration_ms": 23,
  "mode": "shell",
  "builtin": false,
  "command": "/bin/zsh",
  "args": ["-lc", "echo hello"],
  "raw_command": "echo hello",
  "cwd": "/tmp",
  "env": ["PATH=/usr/bin:/bin", "USER=hamed"],
  "stdin": "",
  "stdout": "hello\n",
  "stderr": "",
  "exit_code": 0,
  "pid": 12345
}
```

## Notes

- A local `pipery.jsonl` file is created by default, so logging works even with no extra configuration
- The GitHub Actions release workflow uses the `VERSION` file as the source of truth for the Git tag, GitHub release, and Docker image tags
- The included `Dockerfile` builds a reusable Debian slim-based image with `psh` installed at `/usr/local/bin/psh`
- With no command arguments, piped stdin is treated as a line-by-line command source
- Stdout and stderr are streamed to the terminal while also being captured for logging
- Stdin capture is supported for direct execution and a single `-c` command when stdin is piped or redirected
- The tool intentionally avoids blocking on log delivery; file or syslog failures are reported to stderr
- Set `fail_on_error` or `-fail-on-error` to stop after the first non-zero command result, similar to shell errexit behavior for batch runs.
