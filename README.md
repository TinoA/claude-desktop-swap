# Windows Claude Swap

Switch between multiple Claude Desktop accounts from the Windows system tray
without manually logging out.

This repository is a fork of
[`FranCalveyra/claude-desktop-swap`](https://github.com/FranCalveyra/claude-desktop-swap).
The upstream project and its MIT license remain credited. Windows-specific
tray support, account backup/restore, native overlays, installation, and
release automation are maintained in this fork.

## What it does

- Saves multiple Claude Desktop sessions as named profiles.
- Switches accounts by safely stopping and restarting Claude Desktop.
- Adds a new account through a guided login flow.
- Runs as a native Windows system-tray application.
- Detects normal, MSIX, Squirrel, and portable Claude Desktop installations.
- Exports and imports encrypted backups.
- Preserves account cookies, Local Storage, IndexedDB, and Session Storage.
- Preserves global device state such as `ant-did`.
- Detects available updates from GitHub Releases.

Claude Code is not targeted or terminated. The tool only operates on the
detected Claude Desktop installation.

## Install for normal users

Download the latest installer from the
[Releases page](https://github.com/TinoA/claude-desktop-swap/releases/latest):

- `Windows-Claude-Swap-Setup-amd64.exe` for standard 64-bit Windows.
- `Windows-Claude-Swap-Setup-arm64.exe` for Windows on ARM.

The installer:

1. Installs per user, without administrator privileges.
2. Creates a Start Menu shortcut.
3. Offers optional startup with Windows.
4. Starts the tray automatically after installation.
5. Includes a normal Windows uninstaller.

Uninstalling removes the program but preserves saved profiles and backups in
`%USERPROFILE%\.claude-swap`. Delete those files separately only if you want
to remove the saved sessions and encrypted tokens.

## Use the tray

After installation, open the Claude icon in the notification area:

- **Cuentas**: choose a saved profile and switch to it.
- **Agregar cuenta...**: opens Claude Desktop for a new login and saves the
  account automatically after login.
- **Eliminar cuenta...**: removes only the selected local profile.
- **Backup**: creates a password-encrypted or same-machine Windows-protected
  backup.
- **Importar backup**: detects the backup protection automatically.
- **Nueva versión disponible**: opens the latest GitHub release.

Before exporting a backup, the application safely stops Claude Desktop,
refreshes the active profile completely, and starts Claude again. A backup is
refused if an older profile still needs a complete refresh, so it cannot give
a false impression that every account is recoverable without verification.

## CLI

```text
claude-desktop-swap tray
claude-desktop-swap add <name>
claude-desktop-swap save <name>
claude-desktop-swap use [name]
claude-desktop-swap list
claude-desktop-swap delete <name>
claude-desktop-swap status
claude-desktop-swap export <file>
claude-desktop-swap export --local <file>
claude-desktop-swap import <file>
```

The `--local` backup is protected for the current Windows user and machine.
It is the best option for reinstalling Windows Claude Swap on the same
computer. Password-protected backups can be moved as archives, but Chromium
session cookies remain encrypted by the original Windows key and may require
login on a different computer.

## Session preservation

Each profile stores a snapshot of:

- Claude `Cookies` SQLite database, including encrypted session cookies.
- `Local Storage/leveldb`.
- `IndexedDB`.
- `Session Storage`.
- Profile metadata and integrity information.

Switching is transactional. Claude is stopped before files are changed, the
incoming state is staged, and every replaced path can be rolled back if an
operation fails. Cookies such as `cf_clearance` and `__cf_bm` are not deleted
automatically.

Older profiles can still be imported, but they need one successful switch or
refresh before they can be included in a complete new backup. Anthropic can
still request verification when it expires or revokes a session, detects a
security event, or changes its server-side requirements; no local tool can
guarantee otherwise.

Chats are not copied by Windows Claude Swap. Claude synchronizes chats through
the account's own service, so each account continues to see its server-side
history after switching.

## Profile location and security

Profiles are stored in:

```text
%USERPROFILE%\.claude-swap\profiles\<name>\
```

Cookie values are never decrypted, printed, or logged. Profile files are
treated as secrets and protected with restrictive permissions where the
operating system supports them. Do not upload `.claude-swap` or backup files
to GitHub.

## Build from source

Requirements:

- Go version declared in `go.mod`.
- Windows SDK support for Windows builds.

```powershell
go test ./...
go vet ./...
go build -trimpath -o claude-desktop-swap.exe .
.\claude-desktop-swap.exe tray
```

The development executable is separate from the installed release. Building
does not delete profiles, uninstall Claude Desktop, or modify saved accounts.

## Release automation

The `main` branch uses Conventional Commits and GitHub Actions:

1. CI runs tests, race tests, and lint on supported operating systems.
2. Release Please opens a version/changelog pull request when changes require
   a release.
3. Merging that release pull request creates the version tag and release.
4. GoReleaser publishes CLI archives and checksums.
5. The Windows job publishes x64 and ARM64 native installers.

No local token, cookie, profile, or backup is included in a release. The
installer is designed to preserve user data during upgrades and uninstall.

## Fork and upstream

The GitHub fork relationship is visible automatically on the repository page.
To create the same relationship manually:

```bash
gh repo fork FranCalveyra/claude-desktop-swap
git remote add upstream https://github.com/FranCalveyra/claude-desktop-swap.git
git remote add origin https://github.com/<your-user>/claude-desktop-swap.git
```

Use `upstream` for updates from the original project and `origin` for this
fork. This repository is currently published as
`TinoA/claude-desktop-swap`.

## License and attribution

MIT License. See [LICENSE](LICENSE). This project is based on and credits the
upstream [`FranCalveyra/claude-desktop-swap`](https://github.com/FranCalveyra/claude-desktop-swap)
project.
