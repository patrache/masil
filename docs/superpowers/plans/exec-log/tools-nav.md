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

## 리뷰 반영

Four review findings. Each was checked against the code before it was changed. All four were right and are fixed.

| # | Commit | Finding → what changed |
|---|---|---|
| R1 | de4b0818 | **Job control and an orphaned claude.** With `-i`, a `claude()` function's real claude ran in its own process group, so Close's group SIGKILL never reached it. Reproduced red first: `TestLoginShellCloseKillsFunctionChild` (termhost, real zsh and bash, HOME/ZDOTDIR rc files defining `claude() { trap '' HUP; sh -c 'echo CHILD $$; exec sleep 1000'; }`). The child outlived Close in both shells. The script now starts with `set +m`, and the test passes on both. |
| R2 | ebabf5ec | **`\033[3J` in exec mode.** In the macOS exec fallback, `\033[3J` wiped the user's real terminal scrollback. `termhost.Request.PaneExec` is now the pane-mode argv and runs `launch.PaneShellScript` (`set +m; printf '\033[3J\033[H\033[2J'; claude "$@"`). `Exec`, used by exec and window mode, runs `launch.ExecShellScript` (`set +m; printf '\033[H\033[2J'; claude "$@"`): it clears only the visible screen and never touches scrollback. `ViaLoginShell` gained a `pane bool` parameter. `Request.command(mode)` chooses the argv. Tests: launch (no `3J` in the exec script), termhost (`TestExecModeRunsExecNotPaneExec`, popup args use Exec, pane start uses PaneExec), workspace (`TestClaudeStartsThroughLoginShell` checks both argvs). |
| R3 | 66695545 | **The hub cursor jumped.** Reproduced red first: `TestHubZoomKeepsCursor` (ctrl+w z on the hub) and `TestHubChatHubKeepsCursor` (hub → chat → hub). In both, enter went to git's open row instead of the row the user picked. `ui.ToolSwitchedMsg.Toggled` is now set only by `Model.toggleTools`, and the hub repositions only on a Toggled message, so zoom and tools.next/prev leave its cursor alone. `NavState.toggleTools` clears `ToolLast` when the hub itself was the active entry. Because of that, `TestToggleToolsFromHubKeepsLast` (the old behaviour) became `TestToggleToolsFromHubClearsLast`. `TestToolSwitchedToggledOnlyByToggle` asserts the flag sequence. |
| R4 | 878121af | **README wording.** README.ko.md said the hint is "늘" shown, but `toolsHint` only follows the ctrl+k hint, which only Command and Standard mode show. Both READMEs now say: Command/Standard (지휘소·표준) mode, on the chat status bar, next to `ctrl+k`. |

내 판단 (R2): the exec and window script still clears the *visible* screen (`\033[H\033[2J`). Otherwise the rc output (p10k instant prompt) would stay above claude on the real terminal. Scrollback is never touched. `termpanetest` fake `cloneReq` also clones `PaneExec`.

Verification after the fixes:
- `go test ./...` (full, no -short): see the result below.
- `GOOS=windows go vet ./...`: OK.
- `go vet ./...`: OK.
- Final verify (main session, workflow wzf1tbh3j): full `go test ./...` PASS; tmux QA of ctrl+t hub/leave/x-label/status hint PASS; login-shell account switch PASS (glowb → ~/.claude-work, elsewhere unset).

## 7. [ ] 안내

Owner 16:56 / 16:58 / 16:59 (final): the top of the screen shows **everything `[` / `]` walk through right now**, all entries, in every mode (standard included), and a click on an entry goes there. Branch `release/v0.1.3-1` (worktree rel0131).

| Commit | What |
|---|---|
| 273d5c7d | `widgets.WalkBar`: every entry on one line, current one `TabActive`, `[` before the `[` target and `]` after the `]` target (wrapping; two entries → both marks on the other; one → none). When too wide: labels cut to a common width with `…`, then `Tab.Short` (icon / first two cells), then no padding, then no badges. Never scrolls an entry away. `Hit` for clicks. `widgets.Tabs` (friends, profile, feed tabs) is unchanged. |
| 95d2546f | `app/walk.go`: `NavState.walkPlaces(scope)` is the single order. `walkLen` / `walkPos` / `walkTo`, the keys (`walkStep`), the click (`walkGo`) and the bar (`walkItems`) all read it, so the bar cannot drift from the keys. Scopes: compact/standard = tab roots, DM, 피드, 알림 (+도구); widget/tiny = 채팅, DM, 피드, 알림 (+도구); command = tab roots, DM; tool surface focused (not widget/tiny) = the open tools, hub included. Layout: standard and command get `SlotTabs` on row 0. Goldens regenerated after checking several by eye (list in the commit message). |
| a4673a54 | README ko/en: the `[` / `]` row says the top tab bar lists the walk and is clickable. |

Tests (red first where the code did not exist yet: the WalkBar and walk tests failed to compile before the code was added):
- `widgets`: `TestWalkBarWideShowsEveryLabelAndMarks`, `TestWalkBarWrapsMarks`, `TestWalkBarNarrowKeepsEveryEntry` (widths 70…16, every active index: `[`/`]` visible and every entry clickable), `TestWalkBarHitMapsTheLabel`.
- `app`: `TestWalkBarOnTopEveryMode`, `TestWalkBarAgreesWithKeys` (`]` and `[` all the way round, five modes, chat side and tool surface, plus the shown place checked against the marked label), `TestWalkBarClick` (every entry, every mode), `TestWalkBarStandardGolden` (`standard_tools`, `standard_chat`). Mutation check: swapping the 피드/알림 labels in `walkItems` makes `TestWalkBarAgreesWithKeys` fail.
- Full `go test ./...` (no -short): PASS. `go vet` with GOOS=darwin/windows/linux: OK. Hangul ratchet: PASS (no new literals; the bar reuses the existing labels and `Tools.TabLabel/TabLabelShort`).

Deviations:
- 내 판단: the marks are `[` / `]` written **in place of the padding blank** of the target tab (`[알림 `, ` DM]`), so the bar is no wider than before. The current entry uses the existing `TabActive` pill rather than `▸ ◂`. Both themes already style `Tab`/`TabActive`.
- 내 판단: the bar is a full-width row 0 in standard and command (not the pane title line). This is the same place compact/widget/tiny already use, and it gives one look everywhere. The body is one row shorter in those modes.
- 내 판단: in compact (and standard/command) on the tool surface, the bar lists the open tools, because that is what `[` / `]` walk there. The chat side's `도구` entry shows again after ctrl+t. Widget/tiny keep their fixed 채팅/DM/피드/알림/도구 walk (they never walk tools with `[` `]`), so their bar is unchanged except for the marks. Their context line still shows the active tool.
- 내 판단: a click on a chat-side entry always moves the focus to Main (`followMain`), as clicks did before. A key does that only when Main had the focus.
- The old claim "with no tool open the shell is 0.1.2 byte for byte" no longer holds, because standard/command gained the bar and every bar gained the marks. The tools.go comment says so now.
- `TestHostileScreens/channel` looked for the head of the hostile long message at 120x40. With one row less, that head scrolls above the view, so the test now checks the same message's tail (`x`×60).
