# Docker Image Guide

The project ships a Debian slim-based runtime image for `psh`.

## What the Dockerfile does

The multi-stage Dockerfile keeps the final image small:

1. Uses `golang:1.22-bookworm` as a builder stage.
2. Accepts `TARGETOS` and `TARGETARCH` so Docker Buildx can cross-build for `linux/amd64` and `linux/arm64`.
3. Builds the CLI from `./cmd/pipery` into `/out/psh`.
4. Copies only the final binary into `debian:bookworm-slim`.
5. Installs `ca-certificates` so HTTPS and similar integrations can work cleanly in derived images.
6. Does not copy repository source code into the final runtime image.

## Runtime shape

- Entrypoint: `psh`
- Default command: none; the container starts `psh` directly
- Working directory: `/workspace`

## Local build examples

Build the default local image:

```bash
docker build -t psh:base .
```

Start an interactive shell session in the container:

```bash
docker run --rm -it -v "$PWD:/workspace" psh:base
```

Run the CLI with explicit arguments:

```bash
docker run --rm -i -v "$PWD:/workspace" psh:base -c "echo hello"
```

Pipe commands through stdin:

```bash
printf 'echo one\npwd\n' | docker run --rm -i -v "$PWD:/workspace" psh:base
```

## Final image contents

The final image intentionally contains only runtime dependencies plus `/usr/local/bin/psh`. The source tree, Go toolchain, module cache, and builder-stage filesystem stay behind in the intermediate build stage.

## Multi-arch release behavior

The CI workflow publishes a manifest list for:

- `linux/amd64`
- `linux/arm64`

That release path depends on the Dockerfile using `TARGETOS` and `TARGETARCH` in the builder stage, so the produced binary matches the requested platform instead of always being `amd64`.
