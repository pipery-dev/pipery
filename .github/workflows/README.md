# CI and Release Workflow Guide

The repository uses two GitHub Actions workflows:

- `.github/workflows/ci.yml`: build, test, release, container publishing, and wiki sync
- `.github/workflows/codeql.yml`: CodeQL code scanning for GitHub Actions and Go

## CI workflow

### Pull requests and non-main pushes

The CI workflow runs:

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

The wiki sync job now uses `Andrew-Chen-Wang/github-wiki-action@v5.0.4` from the GitHub Marketplace.

The job:

- prepares a local `wiki/` folder from repository docs
- maps `README.md` to `Home.md`
- writes `_Sidebar.md` explicitly so the wiki nav stays stable
- publishes the generated pages to `https://github.com/pipery-dev/pipery/wiki`
- skips empty commits when nothing changed

Because the wiki action expects the Git-based wiki backend to already exist, the repository wiki needs at least one manually created starter page before the automation can push updates successfully.

## CodeQL workflow

The CodeQL workflow is intentionally separate from CI so code scanning can evolve independently without complicating release logic.

It analyzes:

- `actions` with `build-mode: none`
- `go` with `build-mode: autobuild`

It runs on:

- pushes to `main`
- pull requests targeting `main`
- a weekly schedule

The Go matrix entry also sets up the Go toolchain from `go.mod` before initializing CodeQL, which helps keep the autobuild environment predictable.
