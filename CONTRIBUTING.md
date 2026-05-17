# Contributing

Thanks for your interest in contributing to claude-desktop-swap.

## Before you start

- Open an issue to discuss significant changes before writing code.
- For bug fixes or small improvements, a PR is fine without prior discussion.

## Workflow

1. Fork the repository and create a branch off `main`.
2. Make your changes. CI runs automatically on every push.
3. Open a PR to `main`. All PRs require maintainer approval before merging.
4. Address review feedback and wait for the green check on CI.

## Adding platform support

The platform abstraction lives in `internal/platform/`. To add a new OS:

1. Create `internal/platform/<os>.go` with the build tag `//go:build <os>`.
2. Implement the `Platform` interface (`AppDataPath`, `IsRunning`, `KillApp`, `LaunchApp`).
3. Remove the corresponding OS from `unsupported.go`'s build constraint.
4. Add the new `goos` to `.goreleaser.yaml` under `builds`.
5. Update the platform support table in `README.md`.

## Running tests locally

```sh
go test -v -race ./...
```

## Git hooks

This repo ships hooks under `.githooks/` to catch broken builds before they hit the remote. Enable them locally with:

```sh
git config core.hooksPath .githooks
```

| Hook | Runs | What it checks |
|------|------|----------------|
| `pre-commit` | every commit | `gofmt -l .` — fails if any file isn't formatted |
| `pre-push` | every push | `go vet ./...` + `go test ./...` |

## Commit style

Use [Conventional Commits](https://www.conventionalcommits.org/). A `.gitmessage` template is included in the repo — activate it locally with:

```sh
git config commit.template .gitmessage
```

Example commits:

```
feat: add Windows platform support
fix: handle Claude not installed on PATH
docs: update installation instructions
```

## Code style

- Run `golangci-lint run` before pushing.
- No comments unless the why is non-obvious.
- No defensive error handling for impossible cases.
