# masil
**English** · [한국어](README.ko.md)

**masil** is **terminal chat + a social feed** that lives inside the terminal you already have open. Talk with friends and coworkers in spaces, channels and threads like Slack, drop by the feed, stories and guestbooks, and get mentions and DMs as desktop notifications while you work in another window.

> The app's screens are in Korean in this release. English and Simplified Chinese open once their translations have been reviewed — see [Language](#language).

## Install

### Homebrew (macOS · Linux)

```sh
brew install patrache/tap/masil
```

To upgrade: `brew update && brew upgrade masil`.

### macOS · Linux — install script

```sh
curl -fsSL https://raw.githubusercontent.com/patrache/masil/main/scripts/install.sh | sh
```

- Installs to `~/.local/bin/masil` by default. It never uses `sudo`.
- The downloaded archive is installed only if it matches its SHA-256 in `checksums.txt`.
- If `~/.local/bin` is not on your `PATH`, the script prints the line to add to your shell profile.

### Windows — PowerShell

```powershell
irm https://raw.githubusercontent.com/patrache/masil/main/scripts/install.ps1 | iex
```

- Installs to `%LOCALAPPDATA%\Programs\masil\masil.exe` and adds that folder to your user `PATH` (takes effect in new terminals). No administrator rights needed.
- Works on both Windows PowerShell 5.1 and PowerShell 7.

### Install script environment variables

| Variable | Meaning | Default |
|---|---|---|
| `MASIL_VERSION` | Version to install (e.g. `0.1.0`; a leading `v` is fine) | Latest release |
| `MASIL_INSTALL_DIR` | Folder to install into | macOS · Linux `~/.local/bin`, Windows `%LOCALAPPDATA%\Programs\masil` |
| `MASIL_INSTALL_LANG` | Language of the install script's messages (`ko` · `en`) | Korean when `LC_ALL` (or `LANG` when unset) — on Windows the UI language — starts with `ko`, English otherwise |

```sh
curl -fsSL https://raw.githubusercontent.com/patrache/masil/main/scripts/install.sh | MASIL_VERSION=0.1.0 MASIL_INSTALL_DIR="$HOME/bin" sh
```

```powershell
$env:MASIL_VERSION = '0.1.0'; irm https://raw.githubusercontent.com/patrache/masil/main/scripts/install.ps1 | iex
```

### Manual download

From [Releases](https://github.com/patrache/masil/releases), download the `masil_<os>_<arch>` archive for your OS and CPU together with `checksums.txt`.

| OS | CPU | File |
|---|---|---|
| macOS | Apple Silicon / Intel | `masil_darwin_arm64.tar.gz` / `masil_darwin_amd64.tar.gz` |
| Linux | arm64 / x86_64 | `masil_linux_arm64.tar.gz` / `masil_linux_amd64.tar.gz` |
| Windows | arm64 / x64 | `masil_windows_arm64.zip` / `masil_windows_amd64.zip` |

Check the checksum **before** you extract the archive.

```sh
# macOS (on Linux use sha256sum instead of shasum -a 256)
grep ' masil_darwin_arm64.tar.gz$' checksums.txt | shasum -a 256 -c
tar -xzf masil_darwin_arm64.tar.gz && mv masil ~/.local/bin/
```

```powershell
# Windows: the two values must match
(Get-FileHash .\masil_windows_amd64.zip -Algorithm SHA256).Hash
Select-String 'masil_windows_amd64.zip' .\checksums.txt
```

If macOS refuses to run a binary you downloaded with a browser, clear the quarantine flag with `xattr -d com.apple.quarantine ~/.local/bin/masil` (not needed when you install with the script or Homebrew).

## Getting started

```sh
masil signup          # pick a handle and password on the sign-up screen
masil login           # log in if you already have an account
masil                 # open masil in this terminal (same as masil tui)
masil join <invite-code>   # join a space with an invite code
```

Other commands: `masil logout` (log out on this device), `masil version`, `masil config path` (where the settings folder is), `masil help` (list of commands).

## Screen modes

The layout switches instantly with the window size. Sizes are `columns×rows`.

| Mode | When |
|---|---|
| Command | 200+ columns · 50+ rows |
| Standard | 100+ columns · 40+ rows (when not Command) |
| Compact | 100+ columns · fewer than 40 rows |
| Widget | 44–99 columns · 24+ rows |
| Tiny | Anything else (43 columns or fewer, 15 rows or fewer, or 99 columns or fewer with fewer than 24 rows) |

Below 20×6 masil only asks you to make the window a little bigger.

Widget shows `Chat / DM / Feed / Alerts` tabs at the top (Tiny uses one-letter tabs); Compact puts `DM / Feed / Alerts` after the tabs you have open (channels, home). The `DM` tab gathers 1:1 and group DMs by most recent conversation (with unread count, last message and time); a DM you open there, and its threads, stay inside the DM tab — switching chat ↔ DM with `[` `]` keeps your place on both sides. Wherever you open a DM from (home, `ctrl+k`, notifications), it always opens in the DM tab. Command mode opens the same list from the rail right below `Home`; Standard from the `DM` row right below the space name (`▾`) at the top of the sidebar.

Pinning a mode: `ctrl+w` then `1` Command · `2` Standard · `3` Compact · `4` Widget · `5` Tiny; `ctrl+w 0` goes back to automatic. If the window is smaller than the pinned mode's minimum, masil shows the automatic mode and says so in the status bar. You can also pin a mode with `[layout] pin` in `config.toml`.

## Key bindings

| Key | Action |
|---|---|
| `ctrl+k` | Command palette — go to a space, channel, DM or person |
| `ctrl+f` | Search everything |
| `ctrl+n` | Notifications (in a built-in code editor's insert mode: completion, see [Tools](#tools)) |
| `?` (`f1`) | Help |
| `f` | Open/close the feed |
| `ctrl+t` | Tools hub ↔ chat — also leaves a panel (see [Tools](#tools)) |
| `[` / `]` | Previous/next tab — Widget·Tiny `Chat → DM → Feed → Alerts`, Compact·Standard `open tabs → DM → Feed → Alerts`, Command `open tabs → DM`; on the tool surface, the open tools (on single-pane screens also `tab` / `shift+tab`). The tab row at the top of every screen lists everything `[` `]` step through right now, in order (`[` marks the previous entry, `]` the next); click a tab to go there |
| `t` / `enter` | Open the thread of the selected message |
| `c` (in a thread) | Go to that thread's channel or DM, with the root message selected |
| `y` | Copy the selected message |
| Mouse drag | Copy the dragged lines in chat (`esc` clears the highlight) |
| `i` (`a`) | Go to the input box — with a Korean keyboard layout you can just start typing in a conversation (channel, thread) (ASCII keys are shortcuts) |
| `enter` / `shift+enter` | Send / new line |
| `esc` | Back — one level up (thread → its conversation). In the input box, leave the input box (press again to go back) |
| `ctrl+o` / `alt+←` (`alt+b`) | Previous place — back to what you were just looking at (tabs, DM tab, channels, threads, feed, notifications alike; `ctrl+o` works while typing in chat too). On macOS this usually arrives as `option+←` (Terminal.app sends `alt+b`) |
| `alt+→` (`alt+f`) | Next place (undoes previous place) |
| `j` / `k`, `↓` / `↑` | Down/up in a list |
| `tab` / `shift+tab` | Next/previous pane |
| `ctrl+c` twice (within 1 s) | Quit — if the input box has text, the first press clears it |

Change keys in `[keys]` of `config.toml`. The full list is in the in-app `?` help.

## Tools

masil 0.1.3 adds five tools next to chat. `ctrl+t` always opens the tools hub — press a tool's number to open it, or `enter` to go back to the tool you used last (it is still open, as you left it) — and `ctrl+t` again goes back to chat. In Command and Standard mode the chat status bar shows `ctrl+t tools` next to `ctrl+k` as a reminder. Chat keeps running while a tool is open: messages and notifications still arrive and friends still see you online.

| `ctrl+t` then | Tool | What it does |
|---|---|---|
| `1` | Daily LC | Today's LeetCode problem: read it, solve it in nvim (in a panel) or the built-in editor, run and submit with your own LeetCode login, and share the result with friends and spaces |
| `2` | Workspace | A folder with a file tree, code view and terminals; share it with friends, end-to-end encrypted, and cut the share at once from anywhere with `ctrl+\` `x` |
| `3` | Architecture design | Draw a system architecture, simulate its traffic and cost, export it (Terraform and more), and edit it together in a channel |
| `4` | lazygit | Run your own lazygit on a repository, inside masil |
| `5` | API test | Send HTTP requests from collections and environments; import and export curl, OpenAPI (JSON) and Postman |

- Programs a tool runs (lazygit, nvim, workspace terminals) open in a panel inside masil, take over the whole terminal until they exit, or open in a tmux popup. The tool picks one; `m` in the hub lets you choose. Windows has no panels (the program takes over the terminal instead). At most 4 panels run at once, across all tools.
- In a panel every key goes to the program, except `ctrl+t`: it leaves the panel and goes back to chat — the program keeps running, and the hub's `enter` takes you back into it (so the program itself never gets `ctrl+t`). `ctrl+\` is the prefix key: `ctrl+\` then `esc` back to masil, `z` zoom, `x` end the program, `?` help. On a Korean keyboard layout both keys work as typed (`ctrl+ㅅ`, `ctrl+₩`).
- In Daily LC, `t` (or the palette's "Daily problem: open a terminal") opens your shell (`$SHELL`) in the solutions folder (`~/masil/leetcode` by default) — in a panel under the editor in the code area (on Windows and in the tiny layout it takes over the terminal until it exits); it closes when the shell exits or the tool closes. After `ctrl+\` `esc`, `e` goes back into the editor and `t` into the terminal. The solve language is always shown — in the code area's head line (`Python3 · <file>`) and at the end of the key hints (`l lang: Python3`); `l` switches it with a "Language: Go → Python3" toast, and an open editor switches to that language's file at once.
- `claude` in a workspace terminal starts through your login shell (`$SHELL -l -i`, zsh or bash, macOS/Linux), so a `claude` shell function or alias in your `~/.zshrc` / `~/.bashrc` — for example one that picks the account by folder — applies. The shell starts in your home folder, so your rc files run there and never inside the workspace folder; it then changes to the folder and runs `claude`.
- The built-in code editors (Daily LC's built-in editor, the workspace code view) color code in your theme and suggest words as you type — keywords, builtins and words already in your open files, with a language server's suggestions first when one is installed for the file's language (`gopls`, `pyright-langserver`, `clangd`, `rust-analyzer`, `typescript-language-server` …, found on your PATH; pick another with a `[lsp.<lang>]` table, e.g. `[lsp.go]` `command = ["gopls"]`, in config.toml). Only the folder's owner on their own machine uses it — never a share guest — and without one, or when it is slow (1.5 s), the word suggestions stay. In insert mode the menu opens after 2 letters or with `ctrl+n` / `ctrl+space`, `↑`/`↓` or `ctrl+n`/`ctrl+p` pick, `tab`/`enter` insert, `esc` closes it (a second `esc` leaves insert mode); turn either off in Settings → Display (completion off also leaves the language server out).
- Tool logins and secrets (your LeetCode login, API keys) stay on this PC — in the macOS Keychain, Windows Credential Manager or Linux Secret Service — and are never sent to the masil server. Settings (`ctrl+k` → Settings) → **Tool logins** lists them and forgets them.
- lazygit — https://github.com/jesseduffield/lazygit (MIT, install it yourself)
- To run masil without any tool, set `MASIL_FEATURES=none`.

```sh
MASIL_FEATURES=none masil   # chat only, every tool hidden
```

## Notification daemon

To get mentions, DMs and invites as OS desktop notifications even while masil is closed, register the notification daemon to start automatically.

```sh
masil daemon install     # start at login (macOS launchd · Linux systemd --user · Windows startup)
masil daemon status      # registration, running, account, notification setting and sync state
masil daemon uninstall   # stop starting at login
```

Sample `masil daemon status` output:

<!-- parity:skip -->
```
masil notification daemon
  Autostart      registered · launchd ~/Library/LaunchAgents/com.patrache.masil.daemon.plist
  Running        yes
  Account        @me · https://…
  Notifications  on
  Sync           saved 11:04 · seq 1041
```

When something is wrong, `⚠` warnings (such as a server mismatch) and the recent warnings (warning and error lines from the daemon log) follow the same output.

- Where it is registered: macOS `~/Library/LaunchAgents/com.patrache.masil.daemon.plist`, Linux `~/.config/systemd/user/masil-daemon.service`, Windows the `masil-daemon` value under `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`.
- On macOS and Linux, `install` starts the daemon right away and `uninstall` stops it right away. On Windows it starts at your next login, so to start it now run `masil daemon run` once (it starts in the background with no window). On Windows, `uninstall` leaves a running daemon in place until you log out.
- **The daemon stays quiet while the TUI is open** — when masil is open on the same device, masil notifies you itself. You never get the same notification twice.
- During **focus mode** notifications are held, and when focus mode ends you get a single summary notification (e.g. "Focus mode over — 3 mentions, 2 DMs").
- After a laptop wakes from sleep, only missed notifications from the last 10 minutes are shown.
- Log: `~/.config/masil/logs/daemon.log` (rotated once to `daemon.log.1` past 5 MB).
- To watch a problem directly, stop the autostarted daemon (`masil daemon uninstall`; only one daemon runs per device) and run it in the foreground. When you are done, register it again with `masil daemon install`.

  ```sh
  masil daemon run --foreground --verbose   # also prints the log to the terminal; ctrl+c to stop
  ```

### Upgrading

After `brew upgrade masil` or re-running the install script, a registered daemon on macOS and Linux notices that its executable changed and **restarts itself on the new version**. Check with `masil daemon status`; if it still has not changed, run `masil daemon install` again — re-registering restarts the running daemon on the new version. On Windows the new version starts at your next login.

## Settings

The settings folder is `~/.config/masil/` **on every OS** (`%USERPROFILE%\.config\masil` on Windows). Change it with the `MASIL_CONFIG_DIR` environment variable. `masil config path` shows where it is.

Main keys in `config.toml`:

```toml
server = "https://…"       # server to connect to
theme = "amber"            # charm | midnight | cream | amber

[notify]
desktop = true             # OS desktop notifications (masil and the notification daemon)

[ui]
mouse = true               # mouse clicks, wheel and drag-to-copy (false = the terminal's own selection)
```

The server address comes from the `MASIL_SERVER` environment variable, then `server` in `config.toml`, then the built-in default.

> **An autostarted notification daemon does not see your shell's environment variables.** If you log in to another server from the shell with `MASIL_SERVER=… masil login`, the autostarted daemon still connects to `server` in `config.toml` (or the built-in default), so no notifications arrive. To change servers, **edit `server` in `config.toml`.** `masil daemon install` and `masil daemon status` warn when your shell's `MASIL_SERVER` differs from the daemon's address.

### Language

The screen language is set by `[ui] language` in `config.toml`.

```toml
[ui]
language = "auto"          # auto | ko | en | zh-Hans
```

- `"auto"` (the default) follows your OS language. You can fix it to `"ko"` (한국어), `"en"` (English) or `"zh-Hans"` (简体中文).
- The `MASIL_LANG` environment variable, when set, wins over `config.toml` (e.g. `MASIL_LANG=en masil`).
- Once more than one language is open, you can also change it inside the app in Settings (`ctrl+k` → Settings) → Display → **Language**.
- This release opens **Korean only**. English and Simplified Chinese open after native-speaker review; until then changing `language` still shows Korean and the language row does not appear in Settings. `MASIL_LANG` shows the unreviewed translations as they are (text without a translation falls back to English, then Korean).

## Uninstall

1. **First** run `masil daemon uninstall` to remove autostart (if you delete the executable first, only the autostart entry is left behind).
2. If you installed with Homebrew run `brew uninstall masil`; otherwise delete the executable (`~/.local/bin/masil` or `%LOCALAPPDATA%\Programs\masil`).
3. To also remove your account credentials and settings, delete the `~/.config/masil` folder.

## Troubleshooting

- **No notifications on macOS**: masil shows notifications only through `/usr/bin/osascript` (AppleScript), never through other tools such as `terminal-notifier`. Notifications appear under the name **"Script Editor"**, so allow notifications for "Script Editor" in System Settings → Notifications.
- **No notifications on Linux**: masil sends notifications straight to the desktop notification daemon (`org.freedesktop.Notifications`, built into GNOME, KDE and others) over the D-Bus session bus. It does not fall back to other programs such as `notify-send` and never starts a session bus of its own (`dbus-launch`) — a notification daemon and a session bus (`DBUS_SESSION_BUS_ADDRESS`) must exist. Headless servers get no notifications.
- **No notifications at all**: check `masil daemon status`. If a `⚠` warning says the server you logged in to differs from the daemon's server, log in again to the same server or fix `server` in `config.toml` (see [Settings](#settings)). If notifications show as off, set `[notify] desktop` to `true` in `config.toml`.
- **Can't connect to the default server** (for example from mainland China): run your own server (see [Local development](#local-development)) and point masil at it with `MASIL_SERVER`, or with `server` in `config.toml` so the notification daemon uses it too.
- **Dragging doesn't select text / copy doesn't work**: masil takes the mouse itself (clicks, wheel, drag), which blocks the terminal's own drag selection. There are four ways around it.
  - **Drag** in chat (a channel or thread) to highlight the dragged lines; releasing copies them exactly as shown (`esc` or a click elsewhere clears the highlight).
  - Select a message and press **`y`** to copy its text.
  - For character-level selection use the terminal's own selection: drag while holding **⌥ Option** in iTerm2, **fn** in macOS Terminal (Terminal.app) (or turn off View → "Allow Mouse Reporting" ⌘R), and **Shift** in most Linux terminals and Windows Terminal.
  - To turn the mouse off entirely, switch off Settings (`ctrl+k` → Settings) → Display → **Mouse**, or set `[ui] mouse = false` in `config.toml` (`ui.mouse`). Clicks, wheel and drag-to-copy then give way to the terminal's own selection.

  Copying goes two ways: masil sends OSC 52 to the terminal (works over SSH) and also writes to the clipboard program on the same machine — macOS `pbcopy`, Linux Wayland `wl-copy` · X11 `xclip`/`xsel`, Windows `clip.exe`. On Linux, if masil says it sent the copy to the terminal but pasting gives nothing, there is no clipboard program: install `wl-clipboard` (Wayland) or `xclip`/`xsel` (X11). If masil says it could not put the text on the clipboard, a clipboard program exists but failed; use the terminal's own selection (drag with ⌥, fn or Shift held). Over SSH masil uses OSC 52 only, so the terminal must allow it: in iTerm2 Settings → General → Selection → "Applications in terminal may access clipboard", in tmux `set -g set-clipboard on`. macOS Terminal (Terminal.app) does not support OSC 52.
- **Borders are misaligned or Korean / box characters overlap**: turn off iTerm2's "Ambiguous characters are double-width" (Profiles → Text). Terminal settings that draw ambiguous-width characters two columns wide are not supported.
- **Korean text looks spaced out like "명 령  팔 레 트"**: your terminal font has no Korean glyphs, so the terminal borrowed narrower glyphs from another font and put them in two-column cells. Switch to a monospaced font that includes Korean (D2Coding, Sarasa Mono K, NanumGothicCoding, …) or set one as the terminal's font for non-ASCII text (iTerm2: Profiles → Text → "Use a different font for non-ASCII text").

## Local development

You need Go 1.26 and Node 24.

```sh
# Server (local Workers simulation — local D1, R2 and Durable Objects)
cd server && npm install && npx wrangler d1 migrations apply DB --local && npx wrangler dev --test-scheduled
# → http://localhost:8787

# Client (in another terminal, at the repository root)
# A development build without a version is masil/0.0.0-dev, which the server's
# MIN_CLIENT_VERSION (0.1.0) rejects with an "update masil" error, so stamp a version.
MASIL_SERVER=http://localhost:8787 go run -ldflags "-X github.com/patrache/masil/internal/version.Version=0.1.0-dev" ./cmd/masil

# Tests
go test ./...
cd server && npm test
```

Deploying the production server and working on the remote D1 and R2 need the owner's approval, so their commands are not written here.

## License

MIT — see [LICENSE](LICENSE).
