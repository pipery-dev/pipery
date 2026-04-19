# CI and Release Workflow Guide

The repository uses a single GitHub Actions workflow at `.github/workflows/ci.yml`.

## What the workflow does

### Pull requests and non-main pushes

The workflow runs:

- version preparation from `VERSION`
- formatting checks with `gofmt`
- linting with `golangci-lint`
- `go test ./...`
- a regular Go build
- a multi-arch Docker build validation for `linux/amd64` and `linux/arm64`

### Pushes to `main`

On successful main-branch pushes, it also:

- builds and uploads the Linux AMD64 binary release tarball
- creates the Git tag from `VERSION` if needed
- creates or updates the GitHub release
- builds and publishes the GHCR container image
- publishes a multi-arch Docker manifest for `linux/amd64` and `linux/arm64`
- syncs the GitHub wiki from the repository Markdown docs

## Wiki sync behavior

The wiki sync job treats repository docs as the source of truth. After a successful release it:

- clones the repo wiki
- copies selected Markdown files into wiki page names
- refreshes `_Sidebar.md`
- commits and pushes only if the generated wiki content changed

That keeps the wiki aligned with the versioned docs in the main branch instead of manually editing two separate documentation sources.
