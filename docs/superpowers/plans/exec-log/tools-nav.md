# 0.1.3 patch — tools navigation, login-shell claude, release v0.1.3-1

Spec: `tools-nav-spec.md` (items 1–6, items 5 and 6 from the "Added 16:38" section).
Branch: `fix/tools-nav` from origin/main 309d23fb (v0.1.3). **Nothing pushed, tagged or deployed** — the main session does the release.

## Commits

| # | Commit | What |
|---|---|---|
| 1 | 9e437e07 | ctrl+t from the chat always shows the hub, with the cursor on the last-active tool (`NavState.ToolLast`, `OpenToolInfo.Last`). Enter returns to that same instance. Digits still open their tool, nothing is closed, and ctrl+t from the hub goes back to the chat. |
| 2 | 359f8a42 | ctrl+t is the one key `grabbedKey` lets through: it leaves lazygit, nvim, the workspace terminal/claude, goeditor and tool text fields for the chat, and the program keeps running. `keymap.layoutAliases`: ctrl+ㅅ and BaseCode `t` for ctrl+t, and ctrl+₩/￦ and BaseCode `\` for ctrl+\ (only while the default key is bound, and only with Mod==Ctrl). The termpane prefix accepts the same aliases. Nothing else from the P7 layout work. |
| 3 | 7c1c1203 | apitool tests: return through the hub (ctrl+t, ctrl+t, enter), since ctrl+t no longer toggles straight back to the tool. |
| 4 | 7e7fdfa0 | Status line: `ctrl+t 도구` right after the ctrl+k hint wherever that hint shows, with tools on and the hub not in front. The pane hint is now `⌨ {program} 에 입력 중 · ctrl+t 나가기`. The pane `?` help gains a ctrl+t row. All new text is in `locales/{ko,en}`; no Hangul literal was added to Go. |
| 5 | 8abff5d0 | gittool exited state: the help showed the key twice (`x x 닫기`, and the enter hint the same way) because the description was itself "key + text". New catalog entries `lazygit.hint_close` / `hint_reopen` hold only the text. The other pane hints (termpane, workspace, leet, apitool) were checked and none repeats its key. |
| 6 | 4ff04658 | claude starts as `$SHELL -l -i -c 'printf …; claude "$@"' claude <args…>` when `$SHELL` is zsh/bash on darwin/linux (`launch.ViaLoginShell`, `termhost.Request.Exec`). It does not use exec or command, so a shell function named claude still runs. Check, Prepare and the share dialog still see masil's own argv. |
| 7 | 8bbe94c9 | `.goreleaser.yaml` `prerelease: false`. release.yml: a new `tag-check` job (only `vX.Y.Z` and `vX.Y.Z-N`), `goreleaser` needs it, and the `'-'` skips in homebrew-tap / install-smoke are gone. Version-parsing tests in Go and on the server. |
| 8 | a07a0e08 | README.md / README.ko.md: the tools section, the ctrl+t key row, and the claude login-shell note. The parity test passes. |

## Goldens

349 golden files were updated with `-update`. A script checked every diff line, and each change is one of these:
- ` · ctrl+t 도구` inserted after the ctrl+k hint (the right-hand gap shrinks by the same width; a line that no longer fits ends in `…` under the existing rule), or
- the pane hint `… · ctrl+\ esc 나가기` replaced by `… · ctrl+t 나가기`, or
- 5 e2e `help_tools` goldens that now carry the new `?` help text for ctrl+t.

`TestFeaturesOnShellUnchanged` still compares everything above the status line strictly. The status line must equal the 0.1.2 line with ` · ctrl+t 도구` removed.

## Verification (macOS, this machine)

- `go vet ./...`, `GOOS=windows go vet ./...`, `GOOS=linux go vet ./...`: OK.
- `go test -count=1 ./...` (full, not -short): 84 packages ok, exit 0.
- server: `vitest run test/unit/client-version.test.ts`: 1 passed. It was written as a confirmation, not red-first: the server gate already reads the `x.y.z` prefix, so `masil/0.1.3-1` passes as 0.1.3.
- goreleaser v2.19.0 (scratch install), run in a throwaway clone:
  - `goreleaser check` passes.
  - With `GORELEASER_CURRENT_TAG=v0.1.3-1`, the build logs "pre-release for tag v0.1.3-1 set to false", and the binary prints `masil 0.1.3-1 (…)`.
  - The snapshot release built all 6 archives and checksums.
  - No tag was created in the real repo.
- Item 5 was checked on this machine with a fake `claude` placed first on PATH, and with a fake shell in the tests (`internal/termhost/exec_argv_unix_test.go`, `internal/ui/workspace/toolsnav_test.go`). A real claude was not started, and ~/.claude and ~/.claude-work were not touched. Results:
  - The owner's zsh function applies: CLAUDE_CONFIG_DIR is `~/.claude-work` under ~/Develop/glowb-backend and unset under terminal-sns.
  - The args arrive unchanged.
  - The rc output is cleared before claude draws.

## Flaky e2e (pre-existing)

`e2e.TestPaneCapAcrossTools` and `TestFiveToolsOpenTogether` sometimes fail with "Cmd blocked > 1s" when the machine is loaded. These tests run real panes with stand-in lazygit/nvim scripts. Measured with `-count=20` on both:
- this branch: 0 failures
- base 309d23fb: 3 failures

So the flake is not caused by this branch. The final full run was green.

## Deviations from the spec

- 내 판단: ctrl+t leaves a panel **without** dropping its grab, unlike ctrl+\ esc. Coming back through the hub (enter) therefore types straight into the program again, which matches "the program keeps running".
- 내 판단: the `ctrl+t 도구` hint does not count against `maxHints`. Counting it would push a component hint off the status line. This way the change is purely additive, and the width budget and `…` truncation still apply.
- 내 판단: the login-shell script clears the screen and scrollback (`printf '\033[3J\033[H\033[2J'`) before `claude "$@"`. This keeps powerlevel10k instant-prompt or greeting output from staying above claude in the pane ("shell startup output must not break the pane").
- 내 판단: `prerelease: false` would publish a `-rc.N` tag as latest, so the new `tag-check` job refuses any tag other than `vX.Y.Z` / `vX.Y.Z-N`.
- en catalog strings were added next to the ko ones, because the catalog requires both.

## Left for the owner / main session

- Push the branch, then tag `v0.1.3-1` and release it.
- After the release, check that `releases/latest` points at v0.1.3-1 and that the tap formula carries `version "0.1.3-1"`.
- Windows CI was not run here. GOOS=windows vet passes, and the new unix-only tests are behind build tags or skips.
