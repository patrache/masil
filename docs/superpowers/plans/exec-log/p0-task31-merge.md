# P0 Task 31 통합 기록 (p0-t31)

- 날짜: 2026-11-10. 워크트리 `.claude/worktrees/dev-v013`, 브랜치 `dev/v0.1.3`, 기준 `348961d1`(P5 2묶음 통합 기록).
- 색인 3절 순서: P0 Task 31(통합·출시 준비) + P1 Task 17 Step 6(README lazygit 줄).
- push·tag·배포·원격 wrangler·유료 리소스: 없음. 브랜치·워크트리는 지우지 않았다.

## 병합 (`--no-ff`)

| 순서 | 브랜치 | 줄기 커밋 | 병합 커밋 | 충돌 |
|---|---|---|---|---|
| 1 | `feat/v013-p0-t31` (P0 Task 31 + P1 Task 17 Step 6 + 리뷰 반영) | `0382afc3`…`f0ee3d2a` (7개) | `5d7867c0` | 없음 |

줄기 커밋:

- `0382afc3` feat: enable released tools, README tools section, cross-tool integration tests (P0 Task 31)
- `f37f952a` docs(readme): lazygit name and link in the tools section (P1 Task 17 Step 6)
- `54c6c82c` 리뷰 반영: termhost `vtScreen.stop` 이 빈 쓰기 대신 입력 파이프를 닫음
- `c385d432` 리뷰 반영: e2e 누출 검사가 도구가 실제로 읽고 쓰는 비밀값을 씀
- `49dc93c9` 리뷰 반영: `cmd/masil/register_api.go` 주석
- `c2965b67` 리뷰 반영: CA 10.3 새 골든 목록에 `TestReleaseEntriesGolden`
- `f0ee3d2a` 리뷰 반영 exec-log

### 충돌과 해결

- 충돌 없음. 줄기가 `dev/v0.1.3` 머리(`348961d1`) 위에서 갈라졌다.
- `internal/i18n/legacy_literals.txt`·`internal/ui/e2e/testdata/pseudo_pending.txt`·`internal/i18n` 전체: 줄기에서 바뀌지 않았다.
- 생성 i18n 파일: 병합 뒤 `go generate ./internal/i18n` 를 돌렸다. `git status --porcelain` 출력 없음(zh-Hans missing 경고만, M4 허용).
- 골든: 새 파일 10개만(`internal/ui/e2e/testdata/TestReleaseEntriesGolden/{200x60,120x40,80x24,50x20,40x14}/{help_tools,palette_tools}.golden`). `git diff --name-status 348961d1 HEAD -- '*.golden'` 에 A 아닌 줄 0개. 기존 골든은 바뀌지 않았다.
- 언어 열기: `internal/i18n` 이 바뀌지 않아 `Selectable`·`Open` 그대로, en·zh-Hans 닫힌 채.
- server/: 바뀌지 않았다(지시대로 시험은 돌림).

### 병합 뒤 고친 것

- 없음(아래 내 판단 참고).

## 검증 (병합된 트리 `5d7867c0`, 이 세션. 기록 문서만 더한 이 커밋 전)

- `go build ./...` → `BUILD_OK`
- `go vet ./...` → `VET_OK`. `GOOS=windows go vet ./...` → `VET_WIN_OK`. `GOOS=linux go vet ./...` → `VET_LINUX_OK`.
- `gofmt -l` (병합으로 바뀐 .go 전부) → 출력 없음.
- `go test -count=1 ./...` 1회차 → exit 1. ok 83, FAIL 1: `internal/ui/leet` `TestModeSwitchKeepsBothTools` — `together_test.go:68: Cmd blocked > 1s: raw tea.Tick/Every or sleep in UI code?`(병렬 에이전트로 부하가 큰 기계, 직후 load average 5분 8.26).
  - 단독 재실행: `go test -count=1 ./internal/ui/leet` → `ok 10.8s`; `-count=5 -run TestModeSwitchKeepsBothTools` → `ok 2.9s`.
- `go test -count=1 ./...` 2회차(같은 트리) → exit 0. ok 84, FAIL 0(`internal/termhost ok 30.5s`, `internal/ui/e2e ok 156.9s`, `internal/ui/leet ok 19.4s`).
- `git status --porcelain -- '*.golden'` → 출력 없음.
- go test 가 끝난 뒤 따로:
  - `cd server && npm run typecheck` → exit 0
  - `npm test` → `Test Files 94 passed (94)`, `Tests 896 passed (896)`, exit 0

```
BUILD_OK
VET_OK
VET_WIN_OK
VET_LINUX_OK
GOFMT_DONE
run1: exit=1  ok 83  --- FAIL: TestModeSwitchKeepsBothTools (Cmd blocked > 1s)
rerun ./internal/ui/leet alone: ok 10.811s ; -count=5 -run TestModeSwitchKeepsBothTools: ok 2.865s
run2: exit=0  ok 84  FAIL 0
TC_EXIT=0
NPM_EXIT=0
 Test Files  94 passed (94)
      Tests  896 passed (896)
```

## 통합하며 내 판단

- 내 판단: `TestModeSwitchKeepsBothTools` 실패는 병합 결함이 아니라 부하 민감 시험으로 본다. 근거: (1) 이 시험은 gittool 이음매를 못 써서 진짜 `lazygit`(가짜 sh 스크립트)·`git` 하위 프로세스를 하네스 Cmd 안에서 돌리고(`gittool/instance.go` `start` 의 `Locate`·`LaunchArgs`), 하네스는 로컬에서 Cmd 하나에 1초만 준다(`uitest.blockLimit`, CA 10.2); (2) 단독으로는 매번 통과, 같은 트리의 2회차 전체 실행도 통과; (3) 같은 실패가 앞서 두 번 기록됐다(`security-fixes.md` p4a 묶음, `P0-task31.md` 줄기 첫 전체 실행). 병합은 leet·gittool 동작을 바꾸지 않았다(줄기는 leet 시험 이음매를 `testhooks.go` 로 옮겼을 뿐, 이름·동작 그대로). 그래서 고친 커밋은 없고, 하네스 한도(P0 계약)나 P1 시험 구조를 바꾸는 것은 오너 확인으로 올린다.
- 내 판단: 지시의 "전체 검증 한 번" 을 넘어 `go test ./...` 를 한 번 더 돌렸다 — 1회차 실패가 부하 탓인지 같은 트리에서 확인하려고. 두 결과 모두 위에 적었다.

## 줄기 내 판단 (요약. 전문은 `P0-task31.md`)

- 출시 집합 = 다섯 도구 전부(`leet`·`workspace`·`arch`·`git`·`api`), `features.Released()`.
- `MASIL_FEATURES` 는 `os.LookupEnv` 로 "있으면 이긴다" — 빈 값도 있음으로 보아 `none` 처럼 모두 끔(기존 시험 20여 곳이 빈 값을 "끔" 으로 씀).
- 도움말·팔레트 골든은 `-update` 를 돌려도 바뀐 기존 골든 0개(도구 줄이 도움말 첫 쪽 밖, 팔레트 골든 검색어에 도구 명령 없음) → 새 골든 `TestReleaseEntriesGolden`(도움말 끝·팔레트 `도구`).
- 출시로 깨진 기존 시험 다섯 곳: 0.1.2 화면을 지키는 시험(`slash` `TestCatalogOrder`·`TestSlashCatalogLocale`, `ui/app` `TestLogoutWipeOption/flags_off`)은 `none` 으로 고정, 출시 화면을 지나가는 시험(`TestNavCommands`·`TestRunCommandMsgDispatch` → `confirmCheck`, `TestCommandCompletion` → `/tools`)은 기대를 출시 동작으로.
- 시험 이음매 셋을 `export_test.go` 에서 운영 파일로: `leet.SetLeetcodeForTest`·`leet.SetImageTransportForTest`(`internal/ui/leet/testhooks.go`), `workspace.SetDialForTest`(`internal/ui/workspace/testhooks.go`). 시험 바이너리 밖에서는 패닉.
- `TestPaneCapAcrossTools` 의 5번째 시도는 작업공간·lazygit 에서(오늘의 문제는 이미 nvim). `TestFiveToolsOpenTogether` 는 `termpanetest` 사용.
- 리뷰 반영: termhost `vtScreen.stop` 은 입력 `PipeWriter.CloseWithError(io.EOF)` 로 멈춤(`Emulator.Close` 는 `Read` 와 경합); 회귀 시험 `TestVTStopWhileInputFlows`·`TestVTInputAfterStopNeverBlocks`.
- 리뷰 반영: CA 10.3 새 골든 목록에는 `TestReleaseEntriesGolden` 만 더함(다른 도구 패키지 새 골든은 오너 확인 7).
- 서버가 바뀌지 않아 줄기는 `npm` 을 생략(통합에서는 돌림, 위).

## 오너 확인

줄기(`P0-task31.md` "오너 확인", 색인 4절 19~24행):

1. 출시 집합 = 다섯 도구 전부(색인 4절 19행). 뺄 도구가 있으면 `internal/features/features.go` `released` + README 두 표.
2. P1·P2 패키지의 시험 이음매 셋을 운영 파일 `testhooks.go`(시험 밖 패닉)로 옮긴 것.
3. `MASIL_FEATURES=`(빈 값)도 모두 끔으로 읽는 것 — README 는 `none` 만 안내.
4. 출시 PR 인데 도움말·팔레트 기존 골든이 바뀌지 않은 것(도구 줄이 도움말 첫 쪽 밖). 앞으로 올릴지는 P0 Task 18 설계 몫.
5. README 영어 도구 절은 원어민 검수 때 함께(색인 16행).
6. (고침) termhost 멈춤 — 리뷰 반영 `54c6c82c`.
7. CA 10.3 "새 골든" 목록을 도구 패키지들의 새 골든(`TestGoldenOwner`·`TestArchScreens`·`TestGitGolden` …)까지 넓힐지.
- 색인 4절에 줄기가 더한 행: 20 Windows 자격 증명 관리자 실기, 21 macOS `ps -ww` 쿠키 값, 22 가격표 대조, 23 archsim 5ms 예산, 24 흩어진 판단 승인 묶음(9·11·16행은 넓힘).

통합에서 더함:

8. `internal/ui/leet` `TestModeSwitchKeepsBothTools` 가 부하 때 `Cmd blocked > 1s` 로 실패(이번이 세 번째 기록). 고칠 길 후보: (a) gittool 이음매를 leet 외부 시험이 쓸 수 있게(예: `testhooks.go` 식, 하위 프로세스 없이 가짜 `lazygit.Env`), (b) 시험을 `ui/e2e` 로 옮겨 같은 방식으로, (c) 하네스 1초 한도(CA 10.2)를 하위 프로세스를 도는 Cmd 에 넓힘. 지금은 그대로 두었다.
