# CLI Entrypoint Guide

The `cmd` directory contains the top-level executable entrypoint.

## Layout

- `cmd/pipery/main.go`: minimal process bootstrap for the `psh` binary

## Why the entrypoint is small

`main.go` intentionally does very little:

1. Wires `stdin`, `stdout`, and `stderr` into the application object.
2. Calls `Run` with the CLI arguments.
3. Prints any user-facing error to stderr.
4. Exits with the exact exit code returned by the application.

Keeping the entrypoint tiny makes the real behavior easier to test. The heavy lifting lives in `internal/pipery`, where command execution, config parsing, logging, masking, and session state are implemented as regular Go code.

## Binary naming

The module path and source folder still use `pipery`, but the built product is exposed as the `psh` binary. That separation lets the repository keep its package layout stable while presenting a shorter command name to users.
