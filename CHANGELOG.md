# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `save` command — snapshot the current Claude Desktop session as a named profile
- `use` command — switch to a saved profile (kills and restarts Claude Desktop)
- `list` command — list saved profiles with creation date and last used timestamp
- `delete` command — remove a saved profile
- `status` command — show the currently active profile
- Full session isolation: both cookies (SQLite) and local storage (LevelDB) are swapped on profile switch
- macOS platform implementation with graceful SIGTERM → SIGKILL shutdown sequence
- Platform abstraction layer for future Windows and Linux support
- CI pipeline (lint + tests on Ubuntu and macOS) via GitHub Actions
- CD pipeline (GoReleaser) triggered on SemVer tags — builds macOS binaries (amd64 + arm64)
- `CODEOWNERS` and branch protection documentation for maintainer-approval workflow
- `.gitmessage` commit template following Conventional Commits
