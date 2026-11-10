# goeditor (fixed copy)

A copy of the root package of [github.com/ionut-t/goeditor](https://github.com/ionut-t/goeditor)
v0.19.0 (`editor.go`, `open.go`, `visual_layout.go`), MIT licensed — see `LICENSE`.
Its `core` and `highlighter` packages are still used from the module itself.

masil's changes (each changed line is marked `masil`):

1. `New` takes `WithDarkBackground(dark)` and then does not
   call `lipgloss.HasDarkBackground(os.Stdin, os.Stderr)`. That call puts the
   terminal in raw mode, writes OSC 11 / DA1 and reads the reply for up to two
   seconds; inside a running Bubble Tea program it races Bubble Tea's own reader
   for the reply bytes and the user's keys (on Windows it opens `CONIN$` itself,
   so no `os.Stdin` swap can stop it).
2. `SetSyntaxStyle(func(chroma.TokenType) lipgloss.Style)` colors the
   highlighted tokens with masil's theme instead of a Chroma style
   (v0.1.3-1, `editor.go` and `renderSegmentWithSyntax` in `visual_layout.go`).
3. `CompletionMenuVisible()` reports whether the completion menu is up:
   masil runs none of the Model's Cmds (its debounce timers never fire), so
   it requests and answers completions itself and needs to know (v0.1.3-1).

4. `SetCursorCellStyle(*lipgloss.Style)` draws the cursor cell in one style
   in every mode (masil: reverse video, so it stands out in every theme)
   instead of the mode's colors (v0.1.3-1, `getCursorStyles` in
   `visual_layout.go`).
5. `CursorPosition()` is the cell where `View` draws the cursor (after the
   command line's text in command mode): masil puts the terminal's real
   cursor there (v0.1.3-1).

6. `convertBubbleKey` takes a key's first rune, not its first byte: upstream
   typed a Korean syllable as a broken Latin-1 character (v0.1.3-1). Text of
   several runes (an IME committing more than one at once) is split into one
   key per rune by masil's wrapper (`internal/ui/leet/goeditor.go`).

Upgrading: copy the three files from the new version and apply the patches again.
