# P0 Task 31 — 통합·출시 준비 (+ P1 Task 17 Step 6)

- 브랜치 `feat/v013-p0-t31`, 워크트리 `.claude/worktrees/v013-p0-t31`, 출발 `dev/v0.1.3` `348961d1`(P5 2묶음 통합 기록). 2026-11-10.
- push·tag·배포·원격 wrangler·유료 리소스 없음. 서버(`server/`)는 고치지 않았다.

## 한 일

- **출시 집합**: `internal/features` — `released` = `leet, workspace, arch, git, api`(다섯 도구 전부), `features.Released()`. `MASIL_FEATURES` 가 **있으면**(빈 값 포함) 출시 집합을 이긴다: 도구 ID 목록·`all`·`none`(빈 값 = `none`). 없으면 출시 집합만 켜짐. 시험 `TestReleaseFlagsDefault`·`TestFeaturesNoneAndEmpty`(기존 `TestFeaturesDefaultOff` 를 대신함).
- **통합 시험** `internal/ui/e2e/tools_integration_test.go`(e2e 는 이제 `cmd/masil/register_*.go` 와 같은 도구 패키지를 모두 링크 — `main_test.go`):
  - `TestPaneCapAcrossTools`(JX13): `termpanetest` 없이 진짜 `termpane.Launch` + 가짜 프로그램(`exec cat` 스크립트 — nvim·lazygit·로그인 셸). 오늘의 문제 nvim 1 + lazygit 1 + 작업공간 터미널 2 = 4, 5번째는 작업공간·lazygit 어느 쪽이든 `패널은 동시에 4개까지예요` 토스트·프로세스 없음, lazygit 을 닫으면 작업공간 터미널이 다시 열림. Windows 는 건너뜀.
  - `TestKillSwitchFromArchScreen`(JX6): 작업공간 공유(sharetest 릴레이) 중 앞 화면이 arch·api·채팅·허브일 때 각각 `ctrl+\ x` → 오버레이 없이 공유 끝(릴레이 `Ended`). 보낸 공유 프레임(`AssertNoLeakFrames`)·서버 요청·로그(`AssertNoLeak`)에 저장한 도구 비밀값 없음.
  - `TestFiveToolsOpenTogether`: 다섯 도구 인스턴스를 한 표면에(인스턴스 상한 5) 열고 200×60 → 80×24 → 40×14 → 120×40 에서 각 화면 `h.Check()`(AssertIntegrity), 같은 인스턴스, 프로그램 재시작 없음, `AssertNoLeak` 0.
  - `TestReleaseEntriesGolden`(새 골든): 출시 집합 기본값에서 도움말 끝(`?` `G`, 도구 줄)과 팔레트 `도구` 검색 × Required + 40×14.
  - 돌연변이 확인(이 세션, 되돌림): `MaxPanes` 를 5 로 → `TestPaneCapAcrossTools` FAIL(5번째 토스트 없음). 셸의 `ctrl+\ x` KillAll 을 끔 → `TestKillSwitchFromArchScreen` 네 하위 시험 모두 FAIL.
- **README 도구 절**: `README.md` `## Tools` / `README.ko.md` `## 도구`(주요 키 절 뒤, 알림 데몬 절 앞) — `ctrl+t` 숫자·이름(앱의 en/ko 이름)·한 줄 설명 다섯 줄, 패널·접두키·비밀값 보관·`MASIL_FEATURES=none` 안내, 주요 키 표에 `ctrl+t` 한 줄. `TestReadmeToolsSection`(등록부의 순서·이름·출시 집합과 두 README 표가 맞는지), `TestReadmeParity` 통과.
- **P1 Task 17 Step 6**(따로 커밋): 두 README 도구 절에 lazygit 이름·링크 줄. `TestReadmeLazygitLink`.
- **도움말·팔레트 골든**: `go test ./internal/ui/app ./internal/ui/palette -run 'TestHelp|TestPalette' -args -update` 를 돌렸고 **바뀐 골든 파일 0개**(아래 내 판단).
- **오너 확인 목록**: 각 줄기 계획의 "오너 확인" 절·통합 기록과 색인 4절을 대조해 9·11·16행을 넓히고 19~24행을 더했다.
- **계약**: CA 10.2·10.3 의 기능 플래그 문장(하네스 기본 = 출시 집합, `none`).

## 내 판단

- 내 판단: 출시 집합 = 다섯 도구 전부 (오너 확인 필요). 색인 4절 19행.
- 내 판단: `MASIL_FEATURES` 는 "있으면 이긴다" 를 `os.LookupEnv` 로 읽었다 — 빈 값도 "있음" 이라 모두 끔(`none` 과 같음). 기존 시험 20여 곳이 `t.Setenv("MASIL_FEATURES", "")` 로 "플래그 끔" 을 뜻하고 있어 그 뜻을 그대로 지킨다. 출시 집합을 보는 시험은 `t.Setenv` 뒤 `os.Unsetenv`.
- 내 판단: 도움말·팔레트 골든은 `-update` 를 돌려도 하나도 바뀌지 않았다. 도움말 골든은 첫 쪽만 보이고 도구 줄(`도구`·`ctrl+t`·`ctrl+w z`)은 목록 끝에 붙어 첫 쪽 밖이며, 팔레트 골든은 `개` 검색·빈 검색이라 도구 명령이 안 나온다. 그래서 커밋 본문에 적을 "바뀐 골든" 은 없고, 오너가 볼 수 있게 새 골든 `TestReleaseEntriesGolden`(도움말 끝·팔레트 `도구`)을 더했다. e2e 의 기존 골든도 도구 패키지를 링크한 뒤 그대로 통과했다.
- 내 판단: 출시로 화면이 바뀐 기존 시험 다섯 곳을 고쳤다. 0.1.2 화면 자체를 지키는 시험은 `MASIL_FEATURES=none` 으로 고정했다(`slash` `TestCatalogOrder`·`TestSlashCatalogLocale` — "0.1.2 표와 같다", `ui/app` `TestLogoutWipeOption/flags_off`). 다른 것을 보다가 출시 화면을 지나는 시험은 출시 동작으로 기대를 바꿨다(`ui/app` `TestNavCommands`·`TestRunCommandMsgDispatch`: 로그아웃이 도구 로그인 지우기 선택지가 있는 `confirmCheck`, `ui/chat` `TestCommandCompletion`: `/` 목록 끝에 `/tools`, 8개). 골든은 바뀌지 않았다.
- 내 판단: e2e 에서 진짜 도구를 몰려고 시험 이음매 셋을 `export_test.go` 에서 운영 파일로 옮겼다 — `leet.SetLeetcodeForTest`·`leet.SetImageTransportForTest`(`internal/ui/leet/testhooks.go`), `workspace.SetDialForTest`(`internal/ui/workspace/testhooks.go`). 이름·동작은 그대로이고 `termpane.SetLaunchForTest` 처럼 시험 바이너리 밖에서는 패닉한다. P1·P2 소유 패키지라 오너 확인에 올린다.
- 내 판단: `TestPaneCapAcrossTools` 의 5번째 시도는 작업공간·lazygit 두 도구에서 했다. 오늘의 문제는 이미 nvim 이 떠 있어 `e` 가 새 패널을 열지 않는다(한도에 걸리면 내장 편집기로 가는 길은 P1 시험 몫).
- 내 판단: `TestFiveToolsOpenTogether` 는 `termpanetest` 를 쓴다(계획은 패널 상한 시험만 진짜 `Launch` 를 요구). 오늘의 문제는 문제 화면만(편집기 없이) 연다.
- 내 판단: 처음 전체 시험(출시 집합만 켠 상태)에서 `ui/leet` `TestModeSwitchKeepsBothTools` 가 한 번 `Cmd blocked > 1s` 로 실패했고, 단독으로 다시 돌리면 통과했다(이 시험은 `MASIL_FEATURES=leet,git` 를 스스로 정해 출시 집합과 무관). 전체 실행의 부하로 본다 — 끝의 전체 확인 결과는 아래.

## 확인 (이 세션, 두 커밋 직전 작업 트리 = 커밋 내용)

- `go build ./...` → BUILD_OK
- `go vet ./...` / `GOOS=windows go vet ./...` / `GOOS=linux go vet ./...` → VET_OK / VET_WIN_OK / VET_LINUX_OK
- `go test -count=1 ./...` → ok 83 패키지, FAIL 1: `internal/termhost` 가 `TestPaneNeverBlocksOnStuckChild` 에서 10분 시간 초과(이 브랜치는 `termhost` 를 고치지 않았다). 같은 패키지를 단독으로 다시 돌리면 `ok 9.2s`, 이 세션 첫 전체 실행에서도 `ok 35.8s`. 걸린 곳: `waitLoop` → `vtScreen.stop`(`vt.go:591`)의 `SendText("")` 가 `io.Pipe` 쓰기에서 영원히 기다림 — 아래 오너 확인 6.
- `gofmt -l`(바뀐·새 .go) → 출력 없음
- `git status --porcelain -- '*.golden'` → 새 파일 10개(`internal/ui/e2e/testdata/TestReleaseEntriesGolden/{200x60,120x40,80x24,50x20,40x14}/{help_tools,palette_tools}.golden`)만. `git diff --name-status v0.1.2 -- '*.golden'` 에 A 아닌 줄 0개.
- 서버는 바뀌지 않아 `npm` 시험은 돌리지 않았다.

## 오너 확인

1. 출시 집합 = 다섯 도구 전부(색인 4절 19행). 뺄 도구가 있으면 `features.go` `released` + README 두 표.
2. P1·P2 패키지의 시험 이음매 셋을 운영 파일(`testhooks.go`, 시험 밖 패닉)로 옮긴 것.
3. `MASIL_FEATURES=` (빈 값)도 모두 끔으로 읽는 것 — README 는 `none` 만 안내.
4. 출시 PR 인데 도움말·팔레트 기존 골든이 바뀌지 않은 것(도구 줄이 도움말 첫 쪽 밖). 도구 줄을 도움말 앞쪽으로 올릴지는 P0 Task 18 설계 몫 — 지금은 새 골든 `TestReleaseEntriesGolden` 으로 보여 드린다.
5. README 도구 절의 영어 문장(색인 16행, 원어민 검수 때).
6. **`termhost` 멈춤(이 Task 밖, P0 Task 14 코드)**: `vtScreen.stop` 은 `stopping` 을 켠 뒤 `respDone` 이 아직 안 닫혔으면 `SendText("")` 로 `Responses` 의 `Read` 를 깨운다. 그런데 그사이 `Responses` 가 다른 응답 바이트(에뮬레이터가 쓴 터미널 응답)를 읽고 `stopping` 을 보고 끝나면, 빈 쓰기를 읽을 사람이 없어 `stop` 이 영원히 막힌다 → `waitLoop` 가 `unregisterPane`·`ExitedMsg` 까지 가지 못함(패널 수가 줄지 않고, 프로그램이 끝나도 포커스가 돌아오지 않을 수 있음 — Review Focus 2). 고치는 길 후보: 깨우는 쓰기를 고루틴으로 보내고 `respDone` 뒤 `Emulator.Close()`(파이프 `done` 을 닫아 막힌 쓰기를 풂). 이 Task 범위 밖이라 고치지 않았다.
