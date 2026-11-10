# Windows CI — group git (internal/lazygit, internal/ui/gittool)

Run: GitHub Actions windows-latest, job log `ci-windows-42760118078` (dev/v0.1.3 @ a9276cc1).
Branch: `fix/v013-win-git`. **Windows CI not yet rerun** — nothing below was seen on Windows after the fix.

## Failing tests → cause → fix

| Test | Windows log | Cause | Fix |
|---|---|---|---|
| `lazygit.TestLocateConfigPathMissing` | `locate_test.go:55: cfg path: "D:\\opt\\lg\\lazygit", <nil>` | `Locate` makes the LookPath answer absolute (`filepath.Abs`). On Windows `/opt/lg/lazygit` is rooted but not absolute (no drive), so Abs put the runner's current drive (`D:`) in front. Unix-only fixture. | test: `native()` fixtures (`C:\opt\lg\lazygit` on Windows). New `TestLocateMakesAbsolute` pins the Abs behaviour. commit 8e0d0383 |
| `lazygit.TestResolveRepoOrder` | `repo_test.go:23: cfg: "" true <nil>` | `RepoRoot` asks git about `filepath.Abs(dir)` = `D:\cfg\repo`; the fake git only knew `git -C /cfg/repo …`, so every folder read as "not a repo" → picker. Unix-only fixture. | test: native fixtures; the fake git answers like Git for Windows (`C:/cfg/repo`, forward slashes). New `TestRepoRootAbsoluteNative` (forward slashes + CRLF answer → `C:\r\masil`; a drive-less rooted folder is asked about as its Abs form). Also tightened two checks that passed vacuously on Windows: `TestArgsUserFirst` (was guarded by `filepath.IsAbs("/r/masil")`, false on Windows) and `TestRepoRootNotRepo`'s empty-output case (the fixture key never matched, so it hit "no answer" instead of "empty answer"). commit 8e0d0383 |
| `ui/gittool.TestOpenTwoPresses` | `instance_test.go:50: Started = []` | Same root cause one layer up: the fake `lazygit.Env` (picker_test.go `fakeEnv`) answered `git -C /r/masil …`; the tool asked about `D:\r\masil`, the recent repo read as "not a git repository", nothing launched. | test: `native()` for recents, configured repo, instance ids, `lgBin`; fake git answers `C:/r/masil`. commit 698f2289 |
| `ui/gittool.TestPaneThemeOverlay/pane` | `panic: index out of range [0] with length 0` at `instance_test.go:98` | Same as above (nothing started), and the test indexed `f.Started()[0]`. The panic ended the test binary, so **every later gittool test (modes, order, picker, probe, goldens) never ran on Windows.** | test: same native fixtures; `firstStarted(t, f)` fails the test instead of panicking. commit 698f2289 |

No Windows product bug was found in this group: making paths absolute is right on Windows too (Windows itself resolves a drive-less rooted path on the current drive), `exec.LookPath` finds `lazygit.exe` through PATHEXT (also for a configured `[tools.lazygit] path` without `.exe`), `RepoRoot` already did `TrimRight("\r\n")` + `filepath.FromSlash` on git's answer, `UserConfigFiles` trims `lazygit -cd`'s CRLF, and `LaunchArgs` leaves the overlay out when a config path (e.g. under `%APPDATA%`) holds a comma.

No test was skipped. No security assertion was touched (the `LEETCODE*` env check in `TestOpenTwoPresses` should now be reached on Windows; before, the test failed before it).

## Golden: `TestGitRunningGolden`

The fake pane (termpanetest) draws `<Program> pane WxH` with the full path, and on Windows that path is `C:\usr\local\bin\lazygit`. A single golden cannot hold both, so on Windows the test compares `git_running_windows.golden` (`goldenName()` in picker_test.go).

How the Windows files were made (no Windows machine): a throwaway edit made `Locate` answer `C:\usr\local\bin\lazygit` on macOS, `-update` wrote the `_windows` files, and the edit was reverted (not committed). macOS `filepath.Base` does not split on `\`, so the status bar came out as `⌨ C:\usr\local\bin\lazygit 에 입력 중`; on Windows `filepath.Base` gives `lazygit`, so those status-bar lines were put back to the macOS text by hand. Result: the `_windows` files differ from the macOS ones only in the one pane line (sizes 80x24, 120x30, 120x40, 200x60; 40x14 and 50x20 are the same as macOS).

- 내 판단: a per-OS golden is better than skipping the golden on Windows — the layout is the same behaviour on Windows, only the fixture path differs.
- 내 판단: a fixed `C:` drive in `native()` instead of `filepath.Abs` keeps fixtures and goldens independent of the runner's working drive (`D:` on GitHub runners).

## Verified locally (macOS)

- `GOOS=windows go vet ./...` — exit 0
- `GOOS=windows go test -c -o /dev/null ./internal/lazygit` and `./internal/ui/gittool` — compile
- `go test -count=1 ./internal/lazygit ./internal/ui/gittool` — ok (macOS goldens unchanged)
- `gofmt -l internal/lazygit internal/ui/gittool` — clean

## Still uncertain until the next Windows CI run

- Whether the rest of `ui/gittool` passes on Windows: it has never run there (the panic stopped it). Reasoned through, not seen: `TestPickerTabCompletes` (real temp folder `C:\Users\RUNNER~1\…` after EvalSymlinks; typing `C:\…\ma` + tab), the `TestGitGolden` picker/install goldens (the recents are shown as given, `filepath.Base` splits `/` on Windows too), the hub/probe tests.
- The hand-made `git_running_windows.golden` files: right only if nothing else in the frame differs on Windows.
- `native()` assumes `C:\r`, `C:\usr\local\bin`, `C:\cfg` do not exist on the runner (EvalSymlinks then fails and the path stays as given). If one ever exists, EvalSymlinks could change its case.
- Not fixed, cosmetic, outside this group: with a real Windows lazygit the status bar says `⌨ lazygit.exe 에 입력 중` (`filepath.Base` of the Program; `internal/ui/app/exclusive.go`, `internal/ui/termpane/pane.go`). Worth a look by the termpane owner.
- Not fixed, minor: on Windows the recent list compares paths by exact string, so `C:\R\masil` and `c:\r\masil` would be two rows. `git rev-parse --show-toplevel` + EvalSymlinks give one canonical form in practice.
