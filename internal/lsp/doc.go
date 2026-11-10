// Package lsp is a minimal Language Server Protocol client for the built-in
// editors' completion menu: JSON-RPC 2.0 over a server's stdio with
// Content-Length framing, written by hand on encoding/json (no third-party
// protocol module).
//
// Layers:
//
//   - Find / LangForPath / LangForLeetCode pick a server command for a file.
//   - Client is one running server process: Start does the initialize
//     handshake, DidOpen / DidChange / DidClose keep the server's copy of a
//     document (full-text sync, changes debounced), Complete asks for
//     completion items, Shutdown ends it (shutdown/exit, killed after 2 s).
//   - Manager is what an editor uses: one Client per (language, root),
//     started lazily on a goroutine, restarted at most three times after a
//     crash, stopped after ten unused minutes and by StopAll when masil exits.
//
// Nothing here blocks for long: notifications only queue a frame for a writer
// goroutine, and the blocking calls (Complete, Shutdown) are bounded (1.5 s,
// 2 s) so they fit a tea.Cmd. Positions on the wire are UTF-16 code units;
// UTF16Col and RuneCol convert columns of a line holding Hangul or emoji.
//
// Safety: servers run only for the local user's own folder (Manager.Open
// refuses a relative or missing root and a file outside it; callers never
// call it on behalf of a share guest). A server's stderr goes to the log,
// never to the screen, and it inherits masil's environment minus the
// LEETCODE_* sign-in variables.
package lsp
