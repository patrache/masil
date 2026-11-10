# v0.1.3 Windows CI — group api

Branch `fix/v013-win-api` (from dev/v0.1.3 a9276cc1). Source: GitHub Actions `test (windows-latest)` run 42760118078,
package `internal/ui/apitool` FAIL. Windows CI not yet rerun — nothing below is a Windows result I saw passing.

| test | Windows log | cause | fix |
|------|-------------|-------|-----|
| TestExportCurlCopies | `editor_test.go:476: export file: <nil> &{out.postman.json … 0 560 …}` (Stat ok, mode check failed) | test assumption: `st.Mode().Perm() != 0o600` on every OS | 25a7319f — `assertPrivateMode` |
| TestSaveBodyToFile | `response_test.go:171: …\body.json mode = -rw-rw-rw-, want 0600` (via `assertFile`) | same | 25a7319f |
| TestSaveBodyAsksBeforeOverwrite | `review_test.go:251: …\Downloads\bashrc mode = -rw-rw-rw-, want 0600` (via `assertFile`) | same | 25a7319f |

Cause in detail: Windows has no POSIX modes. Go's `os.Stat` reports 0666 for every writable file and 0444 for a
read-only one; `(*os.File).Chmod(0o600)` (in `writeClose`, `internal/ui/apitool/prompts.go`) only clears
`FILE_ATTRIBUTE_READONLY` (`internal/poll.(*FD).Fchmod` in Go 1.26.6). So 0600 can never be observed there.
The save itself worked in all three (the `ExportSaved`/`BodySaved` dialog appeared before the mode check).

Fix: one helper `assertPrivateMode(t, path, fi)` in `response_test.go`, used by `assertFile`, TestExportCurlCopies and
TestSaveOverwriteDoesNotFollowSymlink (that one already skipped the mode on Windows; now it asserts the Windows form).
- Unix: unchanged, mode must be exactly 0600 (checked locally by turning `writeClose` to `Chmod(0o644)` — all four
  tests fail with `mode = -rw-r--r--, want 0600`; reverted).
- Windows: not skipped — asserts a plain regular file with mode 0666 as Go reports it, i.e. masil did not leave it
  read-only and did not save something other than a regular file.
- What protects the file on Windows instead of 0600: the NTFS ACL it inherits from its folder. The default save folder
  is `%USERPROFILE%\Downloads` (`saveDir`), whose inherited ACL grants only the user, SYSTEM and Administrators. This is
  the same protection every other masil "0600" file has on Windows (`config.WriteFileAtomic` skips Chmod on Windows,
  `auth.narrowPerm` is a no-op there, P4 plan line 117 "0600 (windows 제외)"). Product comment on `writeClose` now says so.
- No product code behaviour changed (only a doc comment in `prompts.go`).

## 내 판단

- 내 판단: 세 실패 모두 **시험 가정**(Unix 모드 비트)이지 제품 결함이 아니다. 저장 자체는 Windows 에서 성공했고, 설계서·계획서는
  Windows 에서 0600 을 별도로 요구하지 않으며, 저장소의 다른 0600 파일(credentials.json, config, secrets 대체 파일)도 Windows 에서는
  상속 ACL 에 맡긴다. API 저장만 따로 명시 DACL 을 거는 것은 범위 밖이고 일관성도 깨진다.
- 내 판단: `t.Skip` 대신 Windows 에서 보이는 모양(일반 파일·0666)을 단언했다. 보안 단언을 조용히 없애지 않기 위해서다.
- 내 판단: 같은 원인이라 커밋 하나(25a7319f)로 묶었다.

## 아직 불확실 (다음 Windows CI 전까지)

- Windows CI not yet rerun. 이 세 시험이 Windows 에서 통과하는지는 다음 실행에서 확인해야 한다.
- 세 시험 모두 모드 검사에서 `t.Fatalf` 로 멈췄으므로, 그 **뒤** 단계(TestSaveBodyToFile 의 넘침(spill) 본문 저장,
  TestSaveBodyAsksBeforeOverwrite 의 덮어쓰기 취소·`os.Rename` 덮어쓰기)는 Windows 에서 아직 한 번도 돌지 않았다.
  코드상으로는 `os.Rename` 이 Windows 에서 `MoveFileEx(MOVEFILE_REPLACE_EXISTING)` 로 기존 파일을 바꾸고, 대상 파일을 연 채로
  두는 곳이 없어 통과할 것으로 보지만 보지 못한 결과다.
- 남는 실제 차이(오너 결정 거리): 사용자가 공유 폴더(예 `C:\Users\Public`, 직접 만든 `C:\temp`)를 저장 경로로 직접 치면 Windows 에서는
  그 폴더 ACL 을 물려받아 다른 로컬 사용자가 읽을 수 있다. Unix 의 0600 은 이 경우도 막는다. 막으려면 `golang.org/x/sys/windows`
  로 소유자 전용 보호 DACL 을 거는 공통 함수가 필요하고, masil 의 모든 0600 파일에 같이 적용해야 일관된다 — 이번 수정에는 넣지 않았다.

## 검증 (이 Mac, darwin/arm64, go1.26.6)

- `GOOS=windows go vet ./...` — exit 0
- `GOOS=windows go test -c -o /dev/null ./internal/ui/apitool/` — ok
- `go test -count=1 ./internal/ui/apitool/` — ok
- `gofmt -l internal/ui/apitool` — 없음
