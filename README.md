# claude-swap

Switch between multiple Claude Desktop accounts without logging out of any of them.

## How it works

Claude Desktop is an Electron app that stores your session in two places:

- **Cookies** — a SQLite database containing your auth tokens
- **Local Storage** — a LevelDB directory containing your chat history and UI state

`claude-swap` snapshots both, stores them as named profiles, and swaps them on demand — killing and relaunching Claude Desktop automatically.

## Installation

Download the latest binary for your platform from the [releases page](../../releases/latest).

**macOS (Apple Silicon)**
```sh
curl -L https://github.com/FranCalveyra/claude-desktop-swap/releases/latest/download/claude-swap_darwin_arm64.tar.gz | tar xz
sudo mv claude-swap /usr/local/bin/
```

**macOS (Intel)**
```sh
curl -L https://github.com/FranCalveyra/claude-desktop-swap/releases/latest/download/claude-swap_darwin_amd64.tar.gz | tar xz
sudo mv claude-swap /usr/local/bin/
```

### Build from source

```sh
go install github.com/FranCalveyra/claude-desktop-swap@latest
```

## Usage

```sh
# Save your current session as a named profile (quit Claude first)
claude-swap save personal

# Switch to a saved profile (kills and restarts Claude Desktop)
claude-swap use work

# List all saved profiles (* = active)
claude-swap list

# Show the currently active profile
claude-swap status

# Delete a profile
claude-swap delete old-account
```

### Snapshot while Claude is running

By default, `save` requires Claude Desktop to be closed to ensure a consistent LevelDB snapshot. If you need to save while it's running:

```sh
claude-swap save personal --force
```

## Profile storage

Profiles are stored at `~/.claude-swap/profiles/<name>/` and contain:

| File | Contents |
|------|----------|
| `Cookies` | SQLite copy of your session cookies |
| `leveldb/` | LevelDB copy of your local storage |
| `meta.json` | Profile name, creation date, last used |

The active profile is tracked at `~/.claude-swap/current`.

## Platform support

| OS | Status |
|----|--------|
| macOS | Supported |
| Windows | Planned |
| Linux | Planned |

## Security

Cookie values are encrypted by Chromium using your OS keychain. `claude-swap` never decrypts them — it copies raw encrypted blobs, which are only usable on the machine where they were created.

Profile directories are created with `0700` permissions (owner read/write only).
