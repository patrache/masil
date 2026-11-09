# goeditor (fixed copy)

A copy of the root package of [github.com/ionut-t/goeditor](https://github.com/ionut-t/goeditor)
v0.19.0 (`editor.go`, `open.go`, `visual_layout.go`), MIT licensed — see `LICENSE`.
Its `core` and `highlighter` packages are still used from the module itself.

masil's only change: `New` takes `WithDarkBackground(dark)` and then does not
call `lipgloss.HasDarkBackground(os.Stdin, os.Stderr)`. That call puts the
terminal in raw mode, writes OSC 11 / DA1 and reads the reply for up to two
seconds; inside a running Bubble Tea program it races Bubble Tea's own reader
for the reply bytes and the user's keys (on Windows it opens `CONIN$` itself,
so no `os.Stdin` swap can stop it). The changed lines are marked `masil`.

Upgrading: copy the three files from the new version and apply the patch again.
