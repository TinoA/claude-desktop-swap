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

On Windows, use the repository script so local checks match CI:

```powershell
.\scripts\project.ps1 -Task Validate
```

Use `-Race` when the installed C toolchain supports Go's race detector. The
complete validation also runs formatting checks, `go mod tidy -diff`, `go vet`,
the linter, and the reachable-vulnerability scan.

To build without installing:

```powershell
.\scripts\project.ps1 -Task Build -Arch amd64 -Version dev
```

To validate and create an installer:

```powershell
.\scripts\project.ps1 -Task All -Arch amd64 -Version dev.1 -InstallerVersion 0.5.0.1
```

Build output stays under `installer/dist/`.

## Reporting bugs

Use the GitHub bug-report form and include the app version, Windows version,
whether Claude Desktop was open, and exact reproduction steps. Screenshots are
welcome after private account details are hidden.

Never attach `.csb` backups, `.claude-swap` profiles, Claude cookies, tokens, or
the contents of `%APPDATA%\Claude`.

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

## Project documentation

- `docs/architecture.md` explains the account workflows and safety invariants.
- `docs/regression-review.md` maps release checks to permanent tests.
- `docs/assets.md` distinguishes source images from generated release assets.

Keep commits small and use one concern per commit: tests, implementation,
documentation, or build/release tooling. Do not mix generated artifacts with
unrelated source changes.
