# Windows Discovery Checklist

Goal: empirically map what Claude Desktop actually does on Windows — file layout, process model, encryption — before writing `internal/platform/windows.go`. Do NOT skip ahead to implementation. Every shortcut here turns into a regression later.

Run all of this in a fresh Windows VM (or Parallels guest) with Claude Desktop installed and logged in.

## 1. App data layout

Capture the full tree under `%APPDATA%\Claude\` so we can compare to the macOS layout we already know.

```powershell
Get-ChildItem -Recurse -Force -Path "$env:APPDATA\Claude" |
    Select-Object FullName, Length, LastWriteTime |
    Format-Table -AutoSize > "$env:USERPROFILE\Desktop\claude-appdata.txt"
```

Open `claude-appdata.txt`. Paste back to me. We're looking for:

- [ ] `Cookies` (SQLite file)
- [ ] `Cookies-journal` (and/or `Cookies-wal` + `Cookies-shm` — important: is Chromium-on-Windows using WAL mode?)
- [ ] `Local Storage\leveldb\` directory with LevelDB files
- [ ] `IndexedDB\<some-origin-dir>.leveldb\` — note the exact origin directory name (on macOS it's `https_claude.ai_0.indexeddb.leveldb`)
- [ ] `Session Storage\` directory
- [ ] `ant-did` file (or a Windows-equivalent — name may differ)
- [ ] Anything else that looks session-tied (e.g., `Network\Cookies` — recent Chromium versions moved cookies into a subdirectory on some platforms)

## 2. Network subdir check (critical)

Recent Chromium has been migrating cookies into `Network\Cookies`. If this exists on Windows, our save/restore set is wrong:

```powershell
Test-Path "$env:APPDATA\Claude\Network\Cookies"
```

If `True` → flag it. The Windows implementation may need to point at `Network\Cookies` instead of (or in addition to) the top-level `Cookies`.

## 3. Process identity

While Claude Desktop is running:

```powershell
Get-Process | Where-Object { $_.ProcessName -like "*laude*" } |
    Select-Object Id, ProcessName, Path, MainWindowTitle
```

Confirm:
- [ ] Image name (likely `Claude.exe` — but verify)
- [ ] Full path to the executable (needed for `LaunchApp`)
- [ ] How many processes? Electron apps spawn helper subprocesses — we need the parent only

## 4. Graceful shutdown behavior

The macOS implementation sends SIGTERM, polls, then falls back to SIGKILL. Windows equivalent is `taskkill` without `/F` (sends `WM_CLOSE`) then `taskkill /F`.

Test:

```powershell
# Graceful — Claude should close cleanly
taskkill /IM Claude.exe
# Wait a second, then check if it's actually gone
Start-Sleep -Seconds 2
Get-Process Claude -ErrorAction SilentlyContinue
```

- [ ] Does graceful `taskkill /IM` actually close Claude, or does it hang? (Electron apps sometimes do, especially if there's an unsaved-prompt modal.)
- [ ] How long does graceful shutdown take? (informs the poll-then-force timeout)

Then test force kill:

```powershell
taskkill /F /IM Claude.exe
```

- [ ] Confirm this kills it immediately
- [ ] After force kill: are journals (`Cookies-journal`, `*-wal`) left behind? (matters for our restore logic)

## 5. Launch behavior

```powershell
# Find what's registered
Get-Command Claude -ErrorAction SilentlyContinue
# Or via Start Menu shortcut
Start-Process "shell:AppsFolder\<package-id>"  # if it's an MSIX install
# Or direct
Start-Process "$env:LOCALAPPDATA\AnthropicClaude\claude.exe"
```

Determine:
- [ ] Is the install path stable (`%LOCALAPPDATA%\AnthropicClaude\claude.exe`) or does it vary?
- [ ] Per-user vs all-users install — both possible?
- [ ] Can we launch via `start "" "Claude"` (using a registered URL/protocol/Start Menu name)?

## 6. Cookie encryption (DPAPI)

This matters for cross-machine concerns. On Windows, Chromium uses DPAPI (Data Protection API) which ties encrypted blobs to **the user account**, not the machine alone. Encrypted cookies copied between different user accounts on the same machine will NOT decrypt.

Verify this doesn't break our model — profiles for the same user account should still work. Just confirm:

- [ ] Cookies file is binary/encrypted (not plain text)
- [ ] The `Local State` file contains an `os_crypt.encrypted_key` (similar to macOS)

## 7. Bisect test (mirror the one we ran on macOS)

This proves the minimal restore set works on Windows the same way it did on macOS.

```powershell
# Quit Claude completely first
taskkill /F /IM Claude.exe

# Snapshot current logged-in state
robocopy "$env:APPDATA\Claude" "$env:TEMP\claude-loggedin" /E /COPYALL

# Wipe app data
Remove-Item -Recurse -Force "$env:APPDATA\Claude"

# Launch Claude — should be logged out
Start-Process "$env:LOCALAPPDATA\AnthropicClaude\claude.exe"
# Confirm logged out in UI, then quit Claude

# Save the clean empty state
robocopy "$env:APPDATA\Claude" "$env:TEMP\claude-empty" /E /COPYALL

# Test A: overlay just Cookies + Local Storage
Remove-Item -Recurse -Force "$env:APPDATA\Claude"
robocopy "$env:TEMP\claude-empty" "$env:APPDATA\Claude" /E /COPYALL
Copy-Item "$env:TEMP\claude-loggedin\Cookies" "$env:APPDATA\Claude\Cookies" -Force
Remove-Item -Recurse -Force "$env:APPDATA\Claude\Local Storage"
robocopy "$env:TEMP\claude-loggedin\Local Storage" "$env:APPDATA\Claude\Local Storage" /E /COPYALL
Start-Process "$env:LOCALAPPDATA\AnthropicClaude\claude.exe"
```

- [ ] Record: logged in or out after Test A?
- [ ] If logged out, extend the overlay with `IndexedDB`, `ant-did`, `Session Storage` and retry

Restore your original state when done:

```powershell
Remove-Item -Recurse -Force "$env:APPDATA\Claude"
robocopy "$env:TEMP\claude-loggedin" "$env:APPDATA\Claude" /E /COPYALL
```

## 8. Report back

Send back:
1. The contents of `claude-appdata.txt`
2. Results of each checkbox above
3. Anything that surprised you or didn't match macOS

With that data we can write `windows.go` against reality instead of assumptions.
