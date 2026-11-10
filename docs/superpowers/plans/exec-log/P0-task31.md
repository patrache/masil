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
6. ~~**`termhost` 멈춤**~~ — 리뷰 반영에서 고침(아래 "리뷰 반영" 2). 원래 기록: `vtScreen.stop` 은 `stopping` 을 켠 뒤 `respDone` 이 아직 안 닫혔으면 `SendText("")` 로 `Responses` 의 `Read` 를 깨운다. 그런데 그사이 `Responses` 가 다른 응답 바이트(에뮬레이터가 쓴 터미널 응답)를 읽고 `stopping` 을 보고 끝나면, 빈 쓰기를 읽을 사람이 없어 `stop` 이 영원히 막힌다 → `waitLoop` 가 `unregisterPane`·`ExitedMsg` 까지 가지 못함(패널 수가 줄지 않고, 프로그램이 끝나도 포커스가 돌아오지 않을 수 있음 — Review Focus 2). 고치는 길 후보: 깨우는 쓰기를 고루틴으로 보내고 `respDone` 뒤 `Emulator.Close()`(파이프 `done` 을 닫아 막힌 쓰기를 풂). 이 Task 범위 밖이라 고치지 않았다.

## 리뷰 반영 (2026-11-10, 같은 브랜치)

1. **e2e 누출 검사가 헛돌던 것(중)**: `storeSecrets` 가 어느 도구도 읽지 않는 이름(`cookie.session`, `env/prod/TOKEN`)에 값을 넣어, 오늘의 문제는 로그아웃 상태이고 API 도구는 값을 읽은 적이 없었다 — `AssertNoLeak`·`AssertNoLeakFrames` 가 도구가 무엇을 하든 통과. 이제 LeetCode 쿠키는 `leetcode.SaveCredentials`(이 계정 `CredNS`), API 토큰은 시드한 개인 환경의 `apitool.SecretKey("", envID, "token")` 에 넣고, `useSecrets` 가 검사 전에 두 도구가 값을 실제로 쓰게 한다: 오늘의 문제 `r` → 가짜 LeetCode 가 두 쿠키를 받음, API 도구 `enter` → 로컬 API 가 `Bearer <토큰>` 을 받음. 못 쓰면 실패. 킬 스위치 시험은 공유가 살아 있는 동안 이것을 하므로 새면 기록한 프레임에 잡힌다. 돌연변이 확인(이 세션, 되돌림): 쿠키를 다른 계정 이름공간에 넣음 → `today's problem did not run with the stored cookies`, 토큰을 옛 이름 `env/prod/TOKEN` 에 넣음 → `the API tool sent no request`.
2. **`termhost` 멈춤(중) — 고침**: `vtScreen.stop` 이 `SendText("")` 대신 입력 파이프의 `PipeWriter` 를 `CloseWithError(io.EOF)` 로 닫는다(진행 중인 `Read` 옆에서 안전; `Emulator.Close` 는 `Read` 가 보는 필드를 써서 경합). 막힌 `Read` 는 EOF 로 깨고, stop 뒤의 키·붙여넣기·질의 응답 쓰기는 막히지 않고 바로 실패한다(전에는 이것들도 읽을 사람 없는 파이프에서 영원히 막힐 수 있었다). 실제 원인(이 세션 스택으로 확인): 붙여넣기·키 홍수 중 `Responses` 가 전달할 입력을 읽은 직후 `stopping` 을 보고 끝남 → 깨우는 빈 쓰기를 읽을 사람 없음. `TestPaneNeverBlocksOnStuckChild -count=30` 으로 재현(90초 시간 초과, `vt.go:591` 에서 막힘) → 고친 뒤 30회 통과. 회귀 시험 `TestVTStopWhileInputFlows`(키 홍수 중 stop 300회)·`TestVTInputAfterStopNeverBlocks` — 고치기 전 둘 다 FAIL 확인.
3. **`setupTools` 의 LeetCode 환경변수 이름(하)**: `LEETCODE_CSRF`·`LEETCODE_CF_CLEARANCE` → `leetcode.EnvSession`·`EnvCSRF`·`EnvCFClearance` 상수.
4. **`cmd/masil/register_api.go` 주석(하)**: 출시 기본값(켜짐)에 맞게 고침.
5. **CA 10.3 새 골든 목록(하)**: `TestReleaseEntriesGolden` 을 더하고, 출시 PR 이 기존 도움말·팔레트 골든을 바꾸지 않은 까닭을 한 문장으로. 오너 확인 4 는 그대로.
6. **시험 이음매 이동·Step 4(하)**: 이음매(`testhooks.go`)는 오너 확인 2 로 그대로 둔다. Step 4 는 이번에 계획 그대로 돌렸다(`make lint`·`make cross`·`go test ./...`, 아래).

- 내 판단: `v0.1.2` 대비 새 골든 디렉터리는 CA 10.3 목록 넷 말고도 도구 패키지 것(`TestGoldenOwner`·`TestArchScreens`·`TestGitGolden` 등)이 많다 — 모두 A(추가)이고 M·D 는 0개. 리뷰가 짚은 `TestReleaseEntriesGolden` 만 목록에 더했고, 목록을 "도구 패키지의 새 골든 전부" 로 넓힐지는 오너 확인 7 로 올린다.
- 내 판단: 서버(`server/`)는 이 브랜치에서 바뀌지 않아(`git diff --stat 348961d1 -- server` 빈 출력) `npm run typecheck`·`npm test` 는 돌리지 않았다.

### 확인 (이 세션, 리뷰 반영 뒤)

- `go build ./...` → BUILD_OK
- `make lint`(= `go vet ./...` + 저장소 전체 `gofmt -l .` 빈 출력) → OK
- `make cross`(darwin·linux·windows `CGO_ENABLED=0 go build ./...`) → OK
- `GOOS=windows go vet ./...` / `GOOS=linux go vet ./...` → VET_WIN_OK / VET_LINUX_OK
- `go test -count=1 ./...` → exit 0, ok 84 패키지, FAIL 0(`internal/termhost` ok 28.3s)
- `gofmt -l`(dev/v0.1.3 `348961d1` 이후 바뀐 .go) → 출력 없음
- `git status --porcelain -- '*.golden'` → 빈 출력(이번 반영에서 골든 변화 없음). `git diff --name-status v0.1.2 -- '*.golden'` 에 A 아닌 줄 0개.
- 서버 바뀌지 않음 → `npm` 생략(위 내 판단).

## 오너 확인 (리뷰 반영에서 더함)

7. CA 10.3 의 "새 골든" 목록이 도구 패키지들의 새 골든(`TestGoldenOwner`·`TestArchScreens`·`TestGitGolden` …)을 이름으로 다 적지 않는다 — 넓힐지.

