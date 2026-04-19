# Internal Packages Overview

The `internal` tree contains implementation details that are not intended to be imported by external modules.

## Package map

- `internal/pipery`: the core application package that powers the `psh` binary

## What lives here

The internal package is responsible for:

- Parsing layered configuration from defaults, YAML, environment variables, and flags
- Managing shell session state such as cwd and exported variables
- Executing built-ins, shell commands, and direct commands
- Capturing stdin, stdout, stderr, exit codes, and durations
- Writing structured logs to JSONL and optional syslog sinks
- Redacting secrets from environment and command outputs
- Coordinating release-friendly behavior such as fail-fast execution and end-of-run summaries

If you want the implementation details, start with [internal/pipery/README.md](pipery/README.md).
