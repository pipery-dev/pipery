# pipery

`pipery` is a Go CLI that mediates shell command execution and records each run as structured JSON.

Current version: `0.1.0` from [VERSION](/Users/hamed/src/github.com/pipery-dev/pipery/VERSION)

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
go build ./cmd/pipery
```

Container image:

```bash
docker build -t pipery:base .
```

GitHub Actions:

- Runs lint, tests, Go build, and Docker build on pushes and pull requests
- Reads the release version from `VERSION`
- Publishes the Docker image to `ghcr.io/<owner>/<repo>:0.1.0`, `:v0.1.0`, and `:latest` on pushes to `main`
- Creates a GitHub release and uploads a Linux AMD64 tarball on pushes to `main`

## Usage

Interactive mode:

```bash
./pipery
```

Pipe commands into stdin:

```bash
echo "echo Hi" | ./pipery
printf 'echo one\npwd\n' | ./pipery
```

Run shell commands:

```bash
./pipery -c "echo hello"
./pipery -c "cd /tmp" -c "pwd"
```

Run a program directly:

```bash
./pipery -- ls -la
```

Run from Docker:

```bash
docker run --rm -i -v "$PWD:/workspace" pipery:base -c "echo hello"
echo "echo hi" | docker run --rm -i -v "$PWD:/workspace" pipery:base
```

Published image tags use the repo version:

```bash
docker pull ghcr.io/<owner>/<repo>:0.1.0
docker pull ghcr.io/<owner>/<repo>:v0.1.0
docker pull ghcr.io/<owner>/<repo>:latest
```

Log to a file and syslog:

```bash
./pipery \
  -log-file ./pipery.jsonl \
  -syslog udp://127.0.0.1:514 \
  -c "echo hello"
```

Pipe stdin through a single command and capture it:

```bash
printf 'hello\n' | ./pipery -c "cat"
```

Use a YAML config file:

```bash
./pipery -config ./.pipery/config.yaml
```

Or environment variables:

```bash
export PIPERY_LOG_FILE=./custom.jsonl
export PIPERY_QUEUE_SIZE=512
export PIPERY_FLUSH_TIMEOUT=5s
./pipery -c "echo hello"
```

## Configuration

Configuration is loaded in this order:

- Built-in defaults
- YAML config file
- `PIPERY_*` environment variables
- CLI flags

If you do not pass `-config`, pipery automatically looks for `./pipery.yaml` or `./pipery.yml`.

If you do not pass `-config`, pipery automatically looks for `./.pipery/config.yaml`.
Example `./.pipery/config.yaml`:

```yaml
log_file: ./pipery.jsonl
syslog: udp://127.0.0.1:514
syslog_tag: pipery
queue_size: 256
max_capture_bytes: 262144
shell: /bin/zsh
prompt: "pipery> "
flush_timeout: 3s
```

Supported environment variables:

- `PIPERY_LOG_FILE`
- `PIPERY_SYSLOG`
- `PIPERY_SYSLOG_TAG`
- `PIPERY_QUEUE_SIZE`
- `PIPERY_MAX_CAPTURE_BYTES`
- `PIPERY_SHELL`
- `PIPERY_PROMPT`
- `PIPERY_FLUSH_TIMEOUT`

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

## Flags

```text
-config             YAML config file path
-c                  run a shell command; repeat to run multiple commands
-log-file           JSONL log file path, default: ./pipery.jsonl
-syslog             syslog target, for example udp://127.0.0.1:514
-syslog-tag         syslog app tag, default: pipery
-queue-size         async log queue size, default: 256
-max-capture-bytes  max bytes recorded for stdin/stdout/stderr, default: 262144
-shell              shell path used for -c and REPL execution
-prompt             interactive prompt, default: pipery>
-flush-timeout      max time to wait for async log flush on exit, default: 3s
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

- A local `pipery.jsonl` file is created by default, so logging works even with no extra configuration.
- The included `Dockerfile` builds a reusable Debian slim-based image with `pipery` installed at `/usr/local/bin/pipery`.
- The GitHub Actions release workflow uses the `VERSION` file as the source of truth for the Git tag, GitHub release, and Docker image tags.
- With no command arguments, piped stdin is treated as a line-by-line command source.
- Stdout and stderr are streamed to the terminal while also being captured for logging.
- Stdin capture is supported for direct execution and a single `-c` command when stdin is piped or redirected.
- The tool intentionally avoids blocking on log delivery; file or syslog failures are reported to stderr.
