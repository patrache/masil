# masil Go 클라이언트 core 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `masil` 단일 바이너리의 Go 클라이언트 기반(core)을 만든다 — 설정·자격 증명·REST 전 엔드포인트·실시간 소켓·공유 상태·알림 추상화·한글/이모지 폭·테마 4종·레이아웃 5모드·UI 프레임워크와 셸·포커스/키맵/마우스·명령 팔레트·도움말·로그인/가입/설정 화면·`masil` 진입점·골든 하네스와 가짜 서버/픽스처.

**Architecture:** 계약 `client-architecture.md`(이하 **CA**)가 정한 패키지 지도 그대로 `internal/*` 의 core 패키지를 만든다. 먼저 모든 core 공개 시그니처를 컴파일되는 스텁으로 깔아(Phase 0, Task 1–2) chat·sns-fun·daemon-release 계획이 병렬로 import 할 수 있게 하고, 그 뒤 아래 계층(폭·설정·API·실시간·상태)에서 위 계층(테마·레이아웃·위젯·앱 셸·화면)으로 TDD 로 채운다. 화면은 `ui.Context` 를 받는 포인터 컴포넌트, 논리 내비게이션 `app.NavState` → 결정적 투영 `app.Project` → 슬롯, 시간 지연은 `ui.After` 하나로만 — 그래서 `uitest` 하네스가 가짜 시계·동기 Cmd 실행·무결성 검사로 모든 크기의 골든을 결정적으로 찍는다.

**Tech Stack:** Go 1.26.1 · `charm.land/bubbletea/v2` v2.1.1 · `charm.land/lipgloss/v2` v2.0.6 · `charm.land/bubbles/v2` v2.2.1 · `github.com/charmbracelet/x/ansi` v0.11.9 · `github.com/alecthomas/chroma/v2` v2.27.0 · `github.com/coder/websocket` v1.8.15 · `github.com/gen2brain/beeep` v0.11.3 · `github.com/gofrs/flock` v0.13.1 · `github.com/oklog/ulid/v2` v2.1.1 · `github.com/BurntSushi/toml` v1.6.0 · `github.com/sahilm/fuzzy` v0.1.3 · `golang.org/x/image` v0.47.0 · `golang.org/x/sys` v0.49.0 · `github.com/charmbracelet/x/exp/golden` v0.1.0 · `github.com/charmbracelet/x/exp/teatest/v2` v2.0.0-20261101151554-27ecf3e4d454

**Spec:** `docs/superpowers/specs/2026-11-05-masil-design.md` (최상위 출처). 이 계획이 직접 구현하는 계약: `docs/contract/client-architecture.md`(CA — 1·2·3·4·5·8·9.1·10장 중 core 소유분), `docs/contract/api.md`(REST·오류 봉투·리미터), `docs/contract/events.md`(소켓·재개·재동기화). 공통 브리프: `docs/superpowers/plans/00-brief.md`. 실행자는 자기 Task 와 함께 **CA 의 해당 절을 반드시 연다** — 이 계획은 CA 의 시그니처를 다시 옮겨 적지 않고 절 번호로 가리키며, CA 가 비워 둔 결정만 여기서 정한다.

## Global Constraints

- 모듈 `github.com/patrache/masil`, `go 1.26`, `toolchain` 지시어 없음. `go.mod`/`go.sum` 은 이 계획(core)만 고친다(CA 1.3).
- 직접 의존성은 CA 1.2 목록의 정확한 버전만. glamour·go-runewidth 를 직접 import 하지 않는다. 다른 TUI 프레임워크·다른 웹소켓 라이브러리·cgo 필요 라이브러리 금지(`CGO_ENABLED=0` 로 darwin/linux/windows 빌드).
- Bubble Tea/Lip Gloss/Bubbles 는 v2 `charm.land/...` 경로. `View()` 는 `tea.View` 를 돌려준다. 기억이 아니라 CA 1.4 의 확인된 API 와 `go doc` 으로 쓴다.
- **다른 계획 소유 파일을 만들거나 고치지 않는다**: `cmd/masil/register_chat.go`, `register_sns.go`, `cmd_daemon.go`, `cmd_admin.go`, `internal/store/chatstore`, `internal/store/feedstore`, `internal/ui/chat`, `internal/ui/feed`, `internal/ui/fun`, `internal/imgrender`, `internal/daemon`, `internal/autostart`, `internal/admincli`, 릴리스 파일, `server/`, `docs/contract/*`.
- 공개 시그니처는 CA 와 글자 그대로. 바꿔야 하면 코드보다 CA 를 먼저 고치는데, 이는 오너 승인 사항이므로 멈추고 보고한다.
- 설정 디렉터리 = `MASIL_CONFIG_DIR` 또는 `<UserHomeDir>/.config/masil`(모든 OS), 디렉터리 `0700`·파일 `0600`, 모든 쓰기는 `config.WriteFileAtomic`(같은 디렉터리 `.<name>.tmp-<pid>` → fsync → rename).
- 토큰·비밀번호를 코드·골든·로그·apitest 기록에 남기지 않는다. 로그의 토큰 자리는 `msl_…(redacted)`.
- 테마 4종 hex 값·테두리·명암은 CA 5.1 표 그대로, 기본 테마 `amber`(D. 레트로 앰버).
- 화면 폭은 `x/ansi` 그래핌 폭 하나로 센다(`len`, `utf8.RuneCountInString` 금지 — 서버 길이 검사 제외). 화면에 그리는 모든 외부 문자열은 `textwidth.Sanitize`, 모든 입력 위젯은 `textwidth.SanitizeInput`(비밀번호 칸 제외).
- 모든 `ui.Component.View` 는 정확히 `size.H` 줄 × 각 `size.W` 칸(`textwidth.Fit`). 스타일 없는 칸 없이 `ctx.Theme.S.*`(최소 `Base`)로 감싼다.
- `internal/ui/...` 비시험 코드에서 `tea.Tick`, `tea.Every`, `tea.Sequence`, `time.Sleep`, `time.After`, `time.NewTimer`, `spinner.Model.Tick` 금지 — 지연은 `ui.After(ctx, d, msg)` 뿐(예외: `internal/ui/timers.go`, `internal/ui/uitest/`).
- **시험 패키지 규칙(import 순환 방지)**: `uitest` 는 `app` 을 import 하고(`Harness.Model`, `BoxesOf(app.FrameInfo)`), `app` 은 `widgets`·`palette`·`authui`·`settings` 등을 import 한다(CA 2.2 의존 그림). 그래서 `uitest`(하네스·`FakeClock`·`ModeSizes`·`Required`·`AssertIntegrity` 무엇이든)를 쓰는 시험은, 그 패키지가 `uitest` 의 (전이) 의존이면 **외부 시험 패키지**로 둔다: `internal/ui/app` → `app_test`, `internal/ui/widgets` → `widgets_test`, `internal/ui/palette` → `palette_test`, `internal/ui/authui` → `authui_test`, `internal/ui/settings` → `settings_test`, `internal/ui/layout`(diagram) → `layout_test`, `internal/markup` → `markup_test`(같은 이름 패키지로 두면 `import cycle not allowed in test`). 의존인지 애매하면 외부 시험 패키지로 둔다 — 외부 시험은 순환을 만들지 않는다. `package app`/`package widgets` 내부 시험은 `uitest` 를 import 하지 않는다 — 크기 표는 시험 파일 안에 직접 적고(값은 CA 10.2 와 같게), 타이머는 시험 파일 안의 기록형 `ui.Timers`(`After` 가 `(d, msg)` 를 기록하고 nil 반환)를 쓴다. 외부 시험이 비공개 상태에 닿아야 하면 `export_test.go`(내부 패키지)로 연다. `app` 의 `TestMain`(`main_test.go`)은 `app_test` 에 하나만 둔다(같은 시험 바이너리이므로 내부 시험에도 적용된다).
- 결정적 시험 환경(CA 10.1): 시계 `2026-11-05T02:05:00Z`, `Loc=Asia/Seoul`(`time/tzdata` import), 테마 D, 이미지 하프블록, 애니메이션 끔, 실시간 끔, 시드 고정.
- 화면이 있는 모든 Task 의 골든은 최소 `uitest.Required` = `{200×60, 120×40, 80×24, 50×20}` 을, 셸·화면 골든은 `uitest.AllSizes()` 를 덮고, 모든 골든은 `AssertIntegrity` 를 통과한다.
- `ui.Do` 태그 접두사는 `core.`. 팔레트 명령 id 는 CA 5.6 표의 core `ui/app` 행만 등록한다.
- 사용자에게 보이는 문구는 한국어, 코드 식별자·주석·커밋 메시지는 영어.
- 배포·Cloudflare 리소스 생성 없음. 서버가 필요한 시험은 `internal/api/apitest` 가짜 서버만 쓴다. 각 Task 의 커밋 단계는 오너가 그 실행 세션에 커밋을 허락한 경우에만 실행한다.

## Review Focus

1. **서버에 닿지 않음(시작 시·사용 중)** — 사람은 앱이 뜨고, 상태줄이 `✕ 오프라인`, 토스트 `서버에 연결할 수 없어요`, 그리고 서버가 돌아오면 저절로 채워지길 기대한다. → Task 30 `TestBootstrapNetworkFailureRetries`.
2. **다른 기기에서 로그아웃·비밀번호 변경(401/소켓 4001) 중 글 쓰는 중** — 사람은 크래시 없이 로그인 화면과 `다른 곳에서 로그아웃됐어요. 다시 로그인해 주세요` 를 기대한다(자격 증명 삭제, 토큰 재사용 없음). → Task 30 `TestUnauthorizedMidSessionReturnsToLogin`.
3. **깨졌거나 손으로 고친 로컬 파일**(`config.toml` 문법 오류·잘못된 값, `credentials.json` 쓰레기, `state.json` 다른 사용자/쓰레기, `device_id` 쓰레기, `alerts-shown.json` 깨짐) — 사람은 기본값으로 뜨는 앱을 기대한다. → Task 4 `TestLoadCorruptTOMLFallsBack`, Task 5 `TestLoadGarbageCredentials`·`TestDeviceIDReplacesGarbage`, Task 15 `TestLedgerRecoversCorruptFile`, Task 32 `TestRestoreIgnoresForeignOrCorruptState`.
4. **입력 칸에 거대하거나 이상한 붙여넣기**(10,000자, `\r\n`, C0/C1 제어 문자, ANSI, U+FFFD·잘못된 UTF-8) — 사람은 멈추지 않고, 정규화된 글만 들어가고, 테두리가 그대로이길 기대한다. → Task 6 `TestSanitizeInvalidUTF8`, Task 22 `TestComposerHugePaste`.
5. **같은 기기에서 masil TUI 두 개(+데몬)** — 사람은 둘 다 정상 동작, `state.json` 이 깨지지 않음(마지막 쓰기 승), 같은 alert 가 OS 알림으로 한 번만 뜨길 기대한다. → Task 5 `TestTwoTUIsShareLock`, Task 15 `TestLedgerTwoProcesses`, Task 32 `TestConcurrentSaveStateStaysValid`.

## CA 변경 요청 (오너 승인 필요 — 이 계획은 `docs/contract/*` 를 고치지 않는다)

교차 계획 검토에서 나온, CA 에 없지만 chat(plan 04)·sns-fun(plan 05)·daemon-release(plan 06)가 기대는 core 계약이다. 모두 **기존 시그니처를 바꾸지 않는 순수 추가 또는 시점 명시**다. 이 계획의 Task 는 아래가 CA 에 반영됐다고 보고 쓰였다. **Task 2 실행자는 시작 전에 CA 에 CR-A~D 가 반영됐는지 확인하고, 아니면 멈추고 보고한다**(Global Constraints "공개 시그니처는 CA 와 글자 그대로"). 승인·CA 반영은 오케스트레이터가 오너에게 받는다.

- **CR-A 자동완성 중 `tab` 전달**(= plan 04 CR-1): CA 5.3.1 선택 능력에 `type Completer interface{ Completing() bool } // true while an input-owned completion popup is open` 추가. CA 5.3.4 2.3 단계에 "하드 전역 `tab`/`shift+tab` 판정(창 1개 모드의 `tab.next`/`tab.prev` 포함) **전에**, 포커스 컴포넌트가 `CapturesInput() && Completing()` 이면 그 키를 `HandleKey` 로 넘기고, `handled=true` 면 끝, `false` 면 원래 하드 전역으로 처리" 를 명시. → Task 19(타입), Task 23(stub), Task 27(라우팅·시험).
- **CR-B 팔레트 명령 id 조회와 `RunCommandMsg` 디스패치**: CA 5.6 에 `func Lookup(id string) (Command, bool) // registered command by id (copy)` 추가. CA 5.3.4 에 "`ui.RunCommandMsg{ID, Args}` 는 앱이 소비한다(방송하지 않음): `palette.Lookup(ID)` → 없으면 무시(+`ctx.Debug` 일 때 slog debug) → `Available != nil && !Available(ctx)` 면 실행하지 않고 토스트 `지금은 쓸 수 없는 명령이에요` → 아니면 `Run(ctx, Args)` 의 Cmd 를 실행(`Args` 는 손대지 않고 그대로). 맨 위 오버레이가 앱이 연 팔레트면 실행 전에 먼저 닫는다" 를 명시. 팔레트 안 `enter` 도 같은 디스패치를 탄다. → Task 2(실제 구현), Task 29.
- **CR-C `Store.SetViewing` 갱신 시점**(= plan 04 CR-2): CA 5.4 "`Store.SetViewing`: 매 프레임 보이는 `ChannelViewer` 들의 채널" 을 "**Compute→Project(→Resize) 직후, 같은 `Update` 안에서, 그 `Update` 가 내는 방송(`ModeChangedMsg`·`FocusChangedMsg`·`RouteUpdatedMsg`·`EventMsg` 등)보다 먼저**" 로 명시. 방송을 받은 컴포넌트(chat 의 읽음 판정 `maybeArmRead`)가 직전 프레임이 아니라 이번 투영의 Viewing 집합을 본다. → Task 30.
- **CR-D 사용법 오류 표지**: CA 2.4 `main.go` 에 package-main 공용 `var errUsage = errors.New("usage")` 와 `func usageErr(err error) error`(`errors.Is(usageErr(x), errUsage) && errors.Is(usageErr(x), x)`; `usageErr(nil) == errUsage`) 추가, `run` 은 `nil`→0, `errors.Is(err, errUsage)`→2, 그 밖→1. core 는 `admincli` 를 import 하지 않는다(CA 2.2) — 같은 `package main` 인 `cmd_admin.go`(plan 05)가 `errors.Is(err, admincli.ErrUsage)` 면 `usageErr(err)` 로 감싸고, `cmd_daemon.go`(plan 06)는 사용법 오류를 `usageErr(…)` 로 돌려준다. → Task 2(실제 구현), Task 35.

## 실행 순서

- **Task 1–2 = Phase 0**(CA 2.3). 끝나면 chat·sns-fun·daemon-release 가 core 를 import 할 수 있다 — 오케스트레이터에 알린다.
- Task 3–35 는 번호 순(앞 Task 의 Interfaces 에 의존). Task 3–18 은 아래 의존 그래프(DAG, `X←Y` = X 는 Y 가 끝난 뒤)를 지키는 한 병렬 가능하다. 여기 적히지 않은 Task(3, 4, 6, 7, 16)는 Phase 0 뒤 바로 시작할 수 있다.
  - `5←3,4` (ids·`WriteFileAtomic`) / `8←3` (TestRequestHeaders 의 `version.UserAgent()`) / `9←8` / `10←9` / `11←9` / `12←9` / `13←12` / `14←10,11,12` (TestBootstrapRequests 는 GetMe·ListDMs·NotificationUnreadCount 등이 실제로 요청을 보내야 통과) / `15←4,6` (`WriteFileAtomic`, `Sanitize`) / `17←6,9,16` / `18←4`
  - Task 17(`TestRenderGolden` ← `uitest.Required`)·Task 18(`TestDiagramGolden` ← `uitest.AllSizes()`)의 크기 표는 Task 20 이 아니라 **Phase 0(Task 2)에서 실제 값·실제 구현**으로 채워지므로(Task 2 Step 3·`TestAllSizes`) Task 20 을 기다리지 않는다. 두 골든 시험은 크기 표가 비어 있으면 `t.Fatal` 하므로 공허하게 통과하지 않는다.
  - Task 19 부터는 Task 3–18 이 모두 끝난 뒤 번호 순.
- **Task 36(`ui/e2e`)은 chat·sns-fun 계획이 모두 끝난 뒤** 마지막에 실행한다.

## 파일 지도 (core 소유, CA 2.1)

| 경로 | 책임 |
|---|---|
| `go.mod`, `go.sum`, `Makefile`, `.github/workflows/ci.yml` | 모듈·빌드·CI |
| `internal/deps/deps.go` | 의존성 고정(CA 1.3 코드 그대로) |
| `internal/version/version.go` | 버전·User-Agent |
| `internal/ids/ids.go` | ULID |
| `internal/config/{paths.go,config.go,atomic.go}` | 경로·`config.toml`·원자적 쓰기 |
| `internal/auth/{credentials.go,deviceid.go}` | `credentials.json`·`device_id` |
| `internal/instance/instance.go` | `tui.lock`/`daemon.lock` |
| `internal/textwidth/{sanitize.go,width.go}` | 정규화·폭·자르기·줄바꿈·채우기 |
| `internal/humanize/humanize.go`, `internal/slash/slash.go`, `internal/termcaps/termcaps.go` | 시간/수 표기·슬래시 명령 파서·터미널 능력 |
| `internal/api/{client.go,errors.go,describe.go,opt.go,limiter.go,types.go,bundles.go}` | REST 공통·타입 |
| `internal/api/ep_{auth,me,users,social,dms,spaces,channels,messages,search,media,posts,stories,notifications,polls,guestbook,admin}.go` | 엔드포인트 메서드(api.md 5장 절 단위) |
| `internal/api/apitest/server.go` | 가짜 HTTP 서버 |
| `internal/realtime/{protocol.go,events.go,conn.go,client.go,backoff.go,outbox.go}` | 소켓 프레임·이벤트 타입·연결 루프 |
| `internal/store/{store.go,apply.go,reducer.go,bootstrap.go,window.go,ring.go}` | 공유 상태 |
| `internal/notify/{decide.go,format.go,ledger.go,backend.go,notifier.go}` | 알림 정책·공유 기록·OS 백엔드 |
| `internal/theme/{theme.go,palettes.go,styles.go,code.go}` | 테마 4종 |
| `internal/markup/{parse.go,render.go,highlight.go,plain.go}` | 본문 마크다운 부분집합 |
| `internal/ui/{component.go,context.go,route.go,msgs.go,registry.go,timers.go,do.go,overlay.go}` | UI 계약 |
| `internal/ui/layout/{mode.go,compute.go}` | 모드·기하 |
| `internal/ui/keymap/keymap.go` | 키맵 |
| `internal/ui/palette/{palette.go,chosung.go,sources.go,overlay.go}` | 명령 팔레트 |
| `internal/ui/widgets/{frame.go,placeholder.go,badges.go,chips.go,poll.go,tabs.go,toasts.go,spinner.go,fade.go,list.go,composer.go,confirm.go,emoji.go,report.go}` | 공용 위젯 |
| `internal/ui/app/{model.go,nav.go,project.go,cache.go,render.go,status.go,tabbar.go,focus.go,keys.go,mouse.go,overlays.go,help.go,toast.go,commands.go,sync.go,bridge.go,alerts.go,state.go,run.go,noop.go}` + `export_test.go` | 루트 앱 |
| `internal/ui/authui/{authui.go,form.go}` | 로그인·가입 화면 |
| `internal/ui/settings/{settings.go,sections.go}` | 설정 화면 |
| `internal/ui/uitest/{harness.go,options.go,clock.go,sizes.go,keys.go,integrity.go}` | 하네스 |
| `internal/ui/uitest/fixtures/{ids.go,seed.go,messages.go,timeline.go,hostile.go,api.go,image.go}` | 시안 데이터·적대적 세트·가짜 응답 |
| `internal/ui/uitest/stub/stub.go` | core 시험용 대역 화면 |
| `internal/ui/e2e/{main_test.go,e2e_test.go,sweep_test.go}` | 전체 제품 골든·스윕 |
| `cmd/masil/{main.go,cmd_core.go}` | 명령 레지스트리·core 명령 |

---

### Task 1: 모듈·의존성 고정·빌드 도구

**Files:**
- Create: `go.mod`, `go.sum`, `internal/deps/deps.go`, `Makefile`, `.github/workflows/ci.yml`
- Modify: `.gitignore` — **`bin/` 한 줄만** 추가(없을 때만). 파일은 레포 공용이고 `dist/` 줄은 plan 06(daemon-release, goreleaser 산출물)이 맡는다 — 다른 줄은 지우거나 고치거나 순서를 바꾸지 않는다(plan 06 과의 조율: 각 계획은 자기 산출물 줄만 덧붙인다).

**Interfaces:**
- Consumes: 없음
- Produces: 모듈 `github.com/patrache/masil`; CA 1.2 의 직접 의존성 전부가 `go.mod` 에 고정; `make test|test-short|golden|golden-pkgs|lint|build|cross` 목표; `make build` 산출물 `bin/` 이 git 에서 무시됨.

- [ ] **Step 1: 실패 확인** — 아직 모듈이 없다.

Run: `cd /Users/sam/Develop/test/terminal-sns && go list -m charm.land/bubbletea/v2`
Expected: FAIL `go: cannot match "charm.land/bubbletea/v2": go.mod file not found`

- [ ] **Step 2: 모듈 생성과 고정**

`go mod init github.com/patrache/masil` 후 `go.mod` 의 go 줄을 `go 1.26` 으로, `toolchain` 줄이 생기면 지운다. CA 1.2 의 15개 모듈을 정확한 버전으로 `go get <mod>@<ver>` 한다. `internal/deps/deps.go` 를 CA 1.3 코드 블록 **글자 그대로** 만든 뒤 `go mod tidy`(go 줄이 `go 1.26.0` 이 되면 그대로 둔다).

`Makefile`:
- `test`: `go test ./...` · `test-short`: `go test -short ./...` · `lint`: `go vet ./...` 그리고 `test -z "$$(gofmt -l .)"`
- `build`: `CGO_ENABLED=0 go build -o bin/masil ./cmd/masil`
- `cross`: `for os in darwin linux windows; do CGO_ENABLED=0 GOOS=$$os go build ./... || exit 1; done`
- `golden-pkgs`: **시험 바이너리가 `github.com/charmbracelet/x/exp/golden` 을 링크하는 패키지**만 한 줄에 하나씩 출력한다(golden 패키지가 `-update` 플래그를 등록하므로, 링크하지 않는 시험 바이너리에 `-update` 를 넘기면 `flag provided but not defined: -update` 로 실패). 패키지 이름·직접 import 문자열로 고르지 않는다 — `internal/ui/uitest/fixtures`·`internal/ui/uitest/stub` 처럼 경로에 `ui/uitest` 가 들어가거나 fixtures 만 import 하는 패키지는 golden 을 링크하지 않는다. 판정은 전이 의존성으로: `@for p in $$(go list ./...); do go list -test -deps $$p | grep -qx 'github.com/charmbracelet/x/exp/golden' && echo $$p; done; true`
- `golden`: `@pkgs="$$($(MAKE) -s --no-print-directory golden-pkgs)"; test -n "$$pkgs" || { echo "no golden packages"; exit 1; }; go test $$pkgs -args -update`

`.gitignore`: `grep -qx 'bin/' .gitignore || printf 'bin/\n' >> .gitignore`(파일 끝에 줄바꿈이 없으면 먼저 하나 붙인다). 현재 내용(`.superpowers/`, `node_modules/`, `.wrangler/`, `dist/`, `.dev.vars`, `*.log`, `.DS_Store`)은 그대로 둔다.

`.github/workflows/ci.yml`: `push`/`pull_request` 에서 `ubuntu-latest`, `macos-latest`, `windows-latest` 행렬, `actions/checkout`·`actions/setup-go` 는 **그날의 최신 메이저**를 GitHub 에서 확인해 쓰고 `go-version: '1.26.1'`, 단계 `go vet ./...`, `go test -short ./...`, `CGO_ENABLED=0 go build ./cmd/masil`(cmd/masil 이 생긴 뒤 의미가 있으며 그전에는 `./...`).

- [ ] **Step 3: 검증**

Run: `go list -m charm.land/bubbletea/v2 github.com/charmbracelet/x/exp/teatest/v2 github.com/charmbracelet/x/exp/golden && GOOS=windows go build -tags tools ./internal/deps && go mod tidy -diff && go mod verify`
Expected: `charm.land/bubbletea/v2 v2.1.1` / `github.com/charmbracelet/x/exp/teatest/v2 v2.0.0-20261101151554-27ecf3e4d454` / `github.com/charmbracelet/x/exp/golden v0.1.0`, `go mod tidy -diff` 출력 없음·종료 코드 0(tidy 가 고칠 것이 없음 = deps.go 가 모든 직접 의존성을 붙잡고 있음; `-diff` 는 파일을 바꾸지 않는다), 그리고 `all modules verified`

`GOOS=windows` 는 필수다: CA 1.3 의 `deps.go` 는 `golang.org/x/sys/windows/registry` 를 blank import 하는데 이 패키지의 파일은 모두 `//go:build windows` 라서, darwin·linux 에서 `go build -tags tools ./internal/deps` 를 그냥 돌리면 `build constraints exclude all Go files in …/windows/registry` 로 **반드시** 실패한다(x/sys v0.49.0 에서 확인). 이 실패를 이유로 `deps.go` 를 고치지 않는다 — `deps.go` 는 CA 1.3 코드 블록 글자 그대로 둔다(`go mod tidy` 는 모든 OS·태그 조합을 고려하므로 그대로 두어도 의존성 고정은 유지된다).

Run: `grep -c '^toolchain' go.mod; grep -E 'glamour|mattn/go-runewidth' go.mod | grep -v '// indirect'`
Expected: `0` 그리고 두 번째 명령은 출력 없음

Run: `grep -cx 'bin/' .gitignore; grep -cx 'dist/' .gitignore; mkdir -p bin && touch bin/probe && git check-ignore -q bin/probe && echo ignored; rm -rf bin`
Expected: `1`, `1`(plan 06 의 줄이 그대로), `ignored`

- [ ] **Step 4: 커밋**

```bash
git add go.mod go.sum internal/deps/deps.go Makefile .github/workflows/ci.yml .gitignore
git commit -m "build: init Go module with pinned client dependencies"
```

---

### Task 2: Phase 0 — core 공개 시그니처 스텁과 등록 레지스트리

**Files:**
- Create: 위 "파일 지도"의 core Go 파일 전부(`cmd/masil/*`, `internal/ui/e2e`, `export_test.go` 제외). 각 파일은 그 파일 책임에 해당하는 CA 시그니처만 담는다.
- Create: `cmd/masil/main.go`(레지스트리만, CA 2.4)
- Test: `internal/ui/registry_test.go`, `internal/ui/palette/register_test.go`, `internal/store/reducer_test.go`, `internal/ui/uitest/sizes_test.go`, `cmd/masil/usage_test.go`

**Interfaces:**
- Consumes: Task 1 의 모듈
- Produces: CA 의 core 공개 시그니처 **전부** — 3.3(config), 3.4(auth), 3.8(instance), 4.1(version), 4.2(ids), 4.3.1–4.3.3(api, apitest), 4.4(realtime 와 이벤트 타입 표 전부), 4.5(store), 4.6(notify), 4.7(textwidth), 4.8(markup), 4.9(humanize·slash·termcaps), 5.1(theme), 5.2(layout), 5.3.1–5.3.7(ui), 5.3.3·5.4(app: `NavState`, `TabState`, `Front`, `Project`, `Options`, `Model` 과 그 메서드, `FocusIntent`, `FrameInfo`, `Box`, `Run`), 5.5(keymap), 5.6(palette), 5.7(widgets), 10.2(uitest, stub), 10.4(fixtures). 추가로 이 계획이 정하는 core 공개 타입: `api.PollInput`(필드는 Task 9 결정 그대로 — `slash` 가 Task 9 전에 쓰므로 Phase 0 에서 완성), `authui.LoggedInMsg struct{ Credentials *auth.Credentials; Me *api.Me }`, `app.SavedState`/`app.SavedTab`(Task 32), `fixtures` ID 상수(Task 23). CA 변경 요청분(위 "CA 변경 요청"): `ui.Completer`(CR-A), `palette.Lookup`(CR-B), `cmd/masil` 의 `errUsage`·`usageErr`(CR-D).
- Produces(실제 동작, 스텁 아님): `ui.RegisterPane`, `ui.RegisterRoute`, `ui.RegisterOverlay`, `ui.Registered`, `palette.Register`, `palette.RegisterSource`, `palette.Lookup`, `store.RegisterReducer`, `cmd/masil` 의 `register`·`errUsage`·`usageErr`, `uitest.Required`·`uitest.ModeSizes`(값)·`uitest.AllSizes()`(본문). 시험 전용(이 계획의 결정): `ui.ResetRegistryForTest`, `ui.SnapshotRegistryForTest`, `palette.ResetForTest`, `palette.SnapshotForTest`.

- [ ] **Step 1: 레지스트리 실패 시험 작성**

```go
// internal/ui/registry_test.go
func TestRegisterRouteDuplicatePanics(t *testing.T) {
	ui.ResetRegistryForTest()
	f := func(*ui.Context, ui.Route) ui.Component { return nil }
	ui.RegisterRoute(ui.RouteSettings, f)
	require.Panics(t, func() { ui.RegisterRoute(ui.RouteSettings, f) })
}
func TestRegisterPaneRejectsRouteSlots(t *testing.T) {
	ui.ResetRegistryForTest()
	for _, s := range []layout.SlotID{layout.SlotMain, layout.SlotThread, layout.SlotFeed, layout.SlotNotifications, layout.SlotTabs, layout.SlotContext, layout.SlotStatus} {
		require.Panics(t, func() { ui.RegisterPane(s, func(*ui.Context) ui.Component { return nil }) }, s)
	}
	ui.RegisterPane(layout.SlotRail, func(*ui.Context) ui.Component { return nil }) // ok
	require.NotNil(t, ui.Registered().Panes[layout.SlotRail])
}
func TestRegisterOverlayDuplicatePanics(t *testing.T) { /* same pattern with "fun.x" */ }
// (every test above that calls ResetRegistryForTest starts with t.Cleanup(ui.SnapshotRegistryForTest()) — the rule in Step 3)
func TestSnapshotRestoresRegistry(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	f := func(*ui.Context, ui.Route) ui.Component { return nil }
	ui.ResetRegistryForTest(); ui.RegisterRoute(ui.RouteHelp, f)
	restore := ui.SnapshotRegistryForTest()
	ui.ResetRegistryForTest(); ui.RegisterRoute(ui.RouteSettings, f)
	restore() // Routes == {RouteHelp} only; registering RouteHelp again panics (duplicate), RouteSettings is free
}
```
(`require` 대신 표준 `testing` 만 써도 된다 — 이 계획의 시험 코드에서 `require.X` 는 "같은 뜻의 표준 비교"로 읽는다. 외부 assert 라이브러리는 의존성 목록에 없으므로 **추가하지 않는다**; 작은 헬퍼를 시험 파일 안에 둔다.)

`palette/register_test.go`: 같은 id 두 번 `Register` → panic; `Register(Command{ID:"t.x", Title:"x"})` 뒤 `Lookup("t.x")` → `(그 Command, true)`, 돌려받은 값의 `Keywords` 를 고쳐도 다시 `Lookup` 한 값은 그대로(복사본), `Lookup("nope")` → `(Command{}, false)`(먼저 `t.Cleanup(palette.SnapshotForTest())`). `uitest/sizes_test.go`(package `uitest_test`): `TestAllSizes` — `uitest.Required` == `{200×60,120×40,80×24,50×20}`, `AllSizes()` 는 Required 를 먼저, 그다음 `ModeSizes` 를 모드 순(Command..TooSmall)으로, 중복 제거 → 길이 24(Task 17·18 골든이 Phase 0 직후 이 값에 기댄다 — 실행 순서 참고). `cmd/masil/usage_test.go`(package `main`): `TestUsageErr` — `errors.Is(usageErr(nil), errUsage)`; `x := errors.New("admincli: usage"); e := usageErr(x)` → `errors.Is(e, errUsage) && errors.Is(e, x)`; `errors.Is(errors.New("boom"), errUsage)==false`. `store/reducer_test.go`: `RegisterReducer` 두 개 → `New()` 가 두 리듀서를 만들고 `s.Reducer("a")` 가 그것을 돌려준다, 같은 `Name()` 두 번 → `New()` 에서 panic.

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/ui/ ./internal/ui/palette/ ./internal/store/`
Expected: FAIL — 패키지가 아직 없음(`no required module provides package` 또는 `undefined`)

- [ ] **Step 3: 스텁 전부 작성**

- 규칙(CA 2.3): 모든 함수·메서드 본문은 0값 반환 + `// TODO(core)`. **`panic("TODO")` 금지.** 구조체 비공개 필드는 비워 둔다. 인터페이스·상수·메시지 타입·열거 상수는 완전한 형태로 쓴다(이후 Task 에서 바뀌지 않음).
- 레지스트리는 진짜로 구현한다(다른 계획의 `init()` 이 Phase 0 직후부터 부른다): `Registry` 타입은 `struct{ Panes map[layout.SlotID]PaneFactory; Routes map[RouteKind]RouteFactory; Overlays map[string]func(*Context, string) Overlay }`, `Registered()` 는 복사본. `RegisterPane` 은 `rail|sidebar|members` 외 슬롯이면 panic. 시험 전용 `ResetRegistryForTest()` 와 `SnapshotRegistryForTest() (restore func())`(현재 레지스트리의 깊은 복사를 떠 두고, `restore` 가 그것으로 되돌림) 는 `internal/ui/export_test.go` 가 아니라 **일반 파일 `registry.go` 안의 공개 함수**로 둔다(다른 패키지 시험이 쓰므로) — 이름 끝 `ForTest` 로 제품 코드 사용 금지를 표시. **규칙: 레지스트리를 비우는 시험은 반드시 먼저 `t.Cleanup(ui.SnapshotRegistryForTest())` 를 건다** — 같은 시험 바이너리 안의 `init()` 등록(`RouteHelp`·`RouteLogin`·`RouteSettings` 등)과 `TestMain` 이 등록한 probe 가 뒤 시험에서 사라져 실행 순서에 따라 실패하는 것을 막는다(Task 31). 레지스트리를 만지는 시험은 `t.Parallel` 을 쓰지 않는다.
- `palette.Register`·`RegisterSource` 는 패키지 전역 슬라이스, 중복 id panic. `palette.Lookup(id) (Command, bool)`(CR-B)은 등록 슬라이스에서 id 로 찾아 **복사본**(`Keywords` 슬라이스도 복사)을 돌려준다 — 레지스트리와 같이 진짜로 구현(chat·sns-fun 시험이 Phase 0 직후 `ui.RunCommandMsg` 에 기대므로). `palette.ResetForTest()`·`palette.SnapshotForTest() (restore func())` 동일 규칙(비우기 전 스냅샷).
- `ui.Completer interface{ Completing() bool }`(CR-A)는 `component.go` 의 다른 선택 능력 옆에 완전한 형태로 둔다.
- `store.RegisterReducer` 는 팩토리 목록에 추가, `store.New()` 가 각 팩토리를 불러 이름으로 등록(중복 이름 panic).
- `cmd/masil/main.go`: CA 2.4 의 `command`, `Env`, `register`(중복 Name panic), 레지스트리 맵, 그리고 **사용법 오류 표지(CR-D, 실제 구현)** `var errUsage = errors.New("usage")`, `func usageErr(err error) error`(`err == nil` 이면 `errUsage`, 아니면 `fmt.Errorf("%w: %w", errUsage, err)`) — 같은 `package main` 의 `cmd_daemon.go`·`cmd_admin.go` 가 Phase 0 직후부터 쓴다. `main()` 은 아직 `os.Exit(2)` 만.
- `uitest`·`fixtures`·`stub` 도 시그니처 스텁(0값). **예외(상수 데이터, 실제 값)**: `uitest.Required`, `uitest.ModeSizes` 는 CA 10.2 값 그대로 지금 채우고, `uitest.AllSizes()` 도 0값 스텁이 아니라 지금 실제로 구현한다(Required ∪ ModeSizes, 중복 제거, 안정 순서 — 순수 함수). Task 17·18 의 골든이 Task 20 보다 먼저 돌 수 있기 때문이다(실행 순서).
- `realtime` 이벤트 타입 표(CA 4.4)의 Go 구조체는 **JSON 태그까지 완전하게** 지금 쓴다(events.md 4장 필드명 snake_case; `PinEvent.By` 는 Task 12 의 사용자 정의 디코드). `api` 타입(CA 4.3.2)은 Task 9 가 채우므로 지금은 이름만 있는 빈 구조체가 아니라 **CA 에 필드가 적힌 것은 그 필드까지** 쓴다(나머지 필드는 Task 9).

- [ ] **Step 4: 검증**

Run: `go test ./internal/ui/ ./internal/ui/palette/ ./internal/store/ ./internal/ui/uitest/ ./cmd/masil/ && go vet ./... && for os in darwin linux windows; do CGO_ENABLED=0 GOOS=$os go build ./... || exit 1; done && ! grep -rn 'panic("TODO' internal cmd`
Expected: `ok` 다섯 줄, vet·빌드 출력 없음, grep 출력 없음

- [ ] **Step 5: 커밋**

```bash
git add internal cmd
git commit -m "feat(core): phase-0 stubs for every core public signature and live registries"
```

---

### Task 3: `version` · `ids`

**Files:**
- Modify: `internal/version/version.go`, `internal/ids/ids.go`
- Test: `internal/version/version_test.go`, `internal/ids/ids_test.go`

**Interfaces:**
- Produces: `version.UserAgent() string`, `version.Semver() string`, `ids.New() string`, `ids.Valid(s string) bool`, `ids.Less(a, b string) bool` (CA 4.1·4.2)

- [ ] **Step 1: 실패 시험 작성**

```go
func TestUserAgent(t *testing.T) {
	ua := version.UserAgent()
	if !regexp.MustCompile(`^masil/0\.0\.0-dev \((darwin|linux|windows); [a-z0-9]+\)$`).MatchString(ua) { t.Fatal(ua) }
}
func TestSemver(t *testing.T) {
	for in, want := range map[string]string{"v0.2.0-dev": "0.2.0", "0.1.0": "0.1.0", "v1.2.3": "1.2.3", "0.0.0-dev": "0.0.0"} {
		version.Version = in
		if got := version.Semver(); got != want { t.Errorf("%s: %s", in, got) }
	}
}
func TestIDsMonotonicAndValid(t *testing.T) {
	prev := ""
	for i := 0; i < 2000; i++ {
		id := ids.New()
		if len(id) != 26 || !ids.Valid(id) || (prev != "" && !ids.Less(prev, id)) { t.Fatalf("%d %q after %q", i, id, prev) }
		prev = id
	}
}
func TestValidRejects(t *testing.T) {
	for _, s := range []string{"", "01JA000000000000000000SAM", "01JA0000000000000000000SAMX", "01ja0000000000000000000sam", "01JA00000000000000000000IL", "81JA0000000000000000000SAM"} {
		if ids.Valid(s) { t.Error(s) }
	}
	if !ids.Valid("01JA0000000000000000000SAM") { t.Fatal("fixture id must be valid") }
}
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/version/ ./internal/ids/` · Expected: FAIL(스텁 0값)

- [ ] **Step 3: 구현** — `UserAgent` = `"masil/" + Version + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"`. `Semver` 는 앞 `v` 와 뒤 `-dev` 만 뗀다. `ids.New` = `ulid.Make().String()`(v2.1.1 의 `Make` 는 프로세스 전역 단조 엔트로피 — `go doc github.com/oklog/ulid/v2 Make` 로 확인). `Valid` = 길이 26, 문자 집합 `0-9A-HJKMNP-TV-Z`(대문자만), 첫 글자 `0`–`7`. `Less` = 문자열 비교.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/version/ ./internal/ids/ -v` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/version internal/ids && git commit -m "feat(core): version user-agent and ULID helpers"`

---

### Task 4: `config` — 경로·`config.toml`·원자적 쓰기

**Files:**
- Modify: `internal/config/{paths.go,config.go,atomic.go}`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces(CA 3.3 그대로): `DefaultServer`, `Paths`, `Config`, `LayoutConfig`, `UIConfig`, `NotifyConfig`, `ResolvePaths() (Paths, error)`, `EnsureDir(p Paths) error`, `Defaults() *Config`, `Load(p Paths) (*Config, error)`, `Save(p Paths, c *Config) error`, `(*Config).ServerURL() string`, `WriteFileAtomic(path string, data []byte, perm os.FileMode) error`, `ErrPartial`

- [ ] **Step 1: 실패 시험 작성**

```go
func TestResolvePathsEnv(t *testing.T) {
	d, c := t.TempDir(), t.TempDir()
	t.Setenv("MASIL_CONFIG_DIR", d); t.Setenv("MASIL_CACHE_DIR", c); t.Setenv("XDG_CONFIG_HOME", "/nope")
	p, err := config.ResolvePaths()
	// Dir=d, Cache=c, Config=d/config.toml, Credentials=d/credentials.json, DeviceID=d/device_id, State=d/state.json,
	// DaemonState=d/daemon-state.json, AlertsShown=d/alerts-shown.json, AlertsShownLock=d/alerts-shown.lock,
	// TUILock=d/tui.lock, DaemonLock=d/daemon.lock, LogDir=d/logs  (filepath.Join)
}
func TestResolvePathsHome(t *testing.T) { // MASIL_CONFIG_DIR unset => <home>/.config/masil; cache => UserCacheDir()/masil
	h := t.TempDir(); t.Setenv("HOME", h); t.Setenv("USERPROFILE", h); t.Setenv("MASIL_CONFIG_DIR", ""); t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p, _ := config.ResolvePaths()
	// p.Dir == filepath.Join(h, ".config", "masil")
}
func TestDefaults(t *testing.T) {
	c := config.Defaults()
	// Server "", Theme "amber", Layout{Pin:"auto",200,50,100,40,43,15,24}, UI{Mouse:true,Images:"auto",Animations:true,PaintBackground:true,Clock:"24h"}, Notify{Desktop:true}, Keys empty non-nil
}
func TestLoadMissingIsDefaults(t *testing.T)        // err == nil, *c == *Defaults() (compare fields)
func TestLoadInvalidValuesPartial(t *testing.T) {   // file: theme="neon"\n[layout]\npin="huge"\ncommand_min_cols=-5\n[ui]\nimages="sixel"\nclock="25h"\nfoo=1\n[weird]\nx=1
	// errors.Is(err, config.ErrPartial); c.Theme=="amber"; c.Layout.Pin=="auto"; c.Layout.CommandMinCols==200; c.UI.Images=="auto"; c.UI.Clock=="24h"
}
func TestLoadPartialFileKeepsOtherDefaults(t *testing.T) // file: server="https://x.example"  => Server set, everything else default, err nil
func TestLoadCorruptTOMLFallsBack(t *testing.T)        // file: "theme = [unterminated" => *Defaults(), errors.Is(err, ErrPartial)   (Review Focus 3)
func TestLoadFixesPerms(t *testing.T)                   // !windows: file 0644 => after Load mode 0600
func TestSaveRoundTripAndPerms(t *testing.T)           // Save then Load equal; mode 0600; dir 0700 after EnsureDir; no ".config.toml.tmp-*" left
func TestServerURLPrecedence(t *testing.T) {
	c := config.Defaults(); config.DefaultServer = "http://d:1"
	t.Setenv("MASIL_SERVER", "");           // empty env = unset
	// "" server => "http://d:1"; c.Server="https://s/" => "https://s"; MASIL_SERVER="https://e/" => "https://e"
}
func TestWriteFileAtomicConcurrent(t *testing.T) // 8 goroutines write distinct 64KB payloads 20x each; final content equals one payload exactly
func TestEnsureDirFixesPerms(t *testing.T)       // !windows: dir 0755 => 0700, logs/ exists
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/config/` · Expected: FAIL

- [ ] **Step 3: 구현**
- `ResolvePaths`: `os.Getenv("MASIL_CONFIG_DIR")` 비어 있지 않으면 그 값, 아니면 `os.UserHomeDir()+"/.config/masil"`. 캐시는 `MASIL_CACHE_DIR` 또는 `os.UserCacheDir()+"/masil"`. `XDG_CONFIG_HOME` 은 읽지 않는다.
- `Load`: 파일 없음 → `Defaults(), nil`. 있으면 `Defaults()` 위에 `toml.Decode`(모르는 키 무시). 문법 오류 → `Defaults()` 와 `fmt.Errorf("%w: %v", ErrPartial, err)`. 값 검증(잘못되면 기본값으로 되돌리고 `ErrPartial` 로 감싼 오류에 필드 이름 나열): theme ∈ {charm,midnight,cream,amber,a,b,c,d}(대소문자 무시 — `config` 는 `theme` 를 import 하지 않는다), pin ∈ {auto,command,standard,compact,widget,tiny}, images ∈ {auto,kitty,iterm2,halfblock,off}, clock ∈ {24h,12h}, 레이아웃 정수 > 0. 권한이 0600 보다 넓으면 `os.Chmod(0600)`(Windows 제외).
- `Save`: `toml.NewEncoder` → `WriteFileAtomic(p.Config, b, 0600)`.
- `WriteFileAtomic`: `os.CreateTemp(dir, "."+base+".tmp-<pid>-*")` → write → `Sync` → `Close` → `Chmod(perm)` → `os.Rename`. 실패 시 임시 파일 삭제.
- `ServerURL`: env 비어 있지 않음 > `c.Server` > `DefaultServer`, 끝 `/` 제거.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/config/ -v` · Expected: PASS (Windows 에서는 권한 시험 `t.Skip`)

- [ ] **Step 5: 커밋** — `git add internal/config && git commit -m "feat(core): config paths, toml load/save with defaults, atomic writes"`

---

### Task 5: `auth` · `instance`

**Files:**
- Modify: `internal/auth/{credentials.go,deviceid.go}`, `internal/instance/instance.go`
- Test: `internal/auth/auth_test.go`, `internal/instance/instance_test.go`

**Interfaces:**
- Consumes: `config.Paths`, `config.WriteFileAtomic`, `ids.New`, `ids.Valid`
- Produces(CA 3.4·3.8): `auth.Credentials`, `auth.ErrNoCredentials`, `auth.Load/Save/Clear`, `(*Credentials).Matches(server string) bool`, `auth.DeviceID(p) (string, error)`, `(*Credentials).Redacted() string`; `instance.ErrAlreadyRunning`, `AcquireTUI`, `TUIRunning`, `AcquireDaemon`

- [ ] **Step 1: 실패 시험 작성**

```go
func TestCredentialsRoundTrip(t *testing.T)   // Save{Version:1,Server:"http://x",Token:"msl_abc",...} -> Load equal; file mode 0600
func TestLoadMissingOrEmptyToken(t *testing.T) // missing => ErrNoCredentials; {"version":1,"token":""} => ErrNoCredentials
func TestLoadGarbageCredentials(t *testing.T)  // "{{{not json" => errors.Is(err, ErrNoCredentials); file NOT deleted (Review Focus 3)
func TestClearAbsentIsNil(t *testing.T)
func TestMatchesIgnoresTrailingSlash(t *testing.T) // Server "https://a/" matches "https://a" and vice versa; not "https://b"
func TestRedactedNeverContainsToken(t *testing.T) {
	c := &auth.Credentials{Token: "msl_Q2xhdWRlIGlzIGEgZ29vZCBib3kgYW5kIHdyaXRlcyBnb29k"}
	if c.Redacted() != "msl_…(redacted)" || strings.Contains(c.Redacted(), c.Token[4:]) { t.Fatal(c.Redacted()) }
}
func TestDeviceIDStable(t *testing.T)          // first call creates valid ULID + "\n"; second returns same; mtime unchanged
func TestDeviceIDReplacesGarbage(t *testing.T) // file "hello" => new valid ULID written (Review Focus 3)

// instance
func TestTwoTUIsShareLock(t *testing.T) {      // Review Focus 5
	r1, err1 := instance.AcquireTUI(p); r2, err2 := instance.AcquireTUI(p) // both nil errors
	running, _ := instance.TUIRunning(p) // true
	r1(); r2(); running, _ = instance.TUIRunning(p) // false
}
func TestDaemonSingleton(t *testing.T)         // second AcquireDaemon => errors.Is(err, ErrAlreadyRunning); after release => ok
func TestTUIRunningAcrossProcesses(t *testing.T) {
	// re-exec: exec.Command(os.Args[0], "-test.run=TestHelperHoldTUILock") with env MASIL_HELPER=1, MASIL_CONFIG_DIR=dir;
	// helper prints "locked\n" after AcquireTUI then blocks on stdin. Parent reads "locked" => TUIRunning true;
	// kill helper, Wait => TUIRunning false.
}
func TestHelperHoldTUILock(t *testing.T) { if os.Getenv("MASIL_HELPER") != "1" { t.Skip() } /* ... */ }
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/auth/ ./internal/instance/` · Expected: FAIL

- [ ] **Step 3: 구현**
- `auth.Load`: 파일 없음·JSON 오류·빈 토큰 → `ErrNoCredentials`(JSON 오류는 `fmt.Errorf("%w: %v", ErrNoCredentials, err)`), 파일은 지우지 않는다. 권한 넓으면 0600 으로.
- `Save` 는 `json.MarshalIndent` + `WriteFileAtomic(…, 0600)` + `EnsureDir`. `Clear` 는 `os.Remove`, `ErrNotExist` 면 nil.
- `DeviceID`: 읽어 `strings.TrimSpace` 가 `ids.Valid` 면 그대로, 아니면(없음 포함) `ids.New()+"\n"` 원자적 쓰기.
- `instance`: `flock.New(p.TUILock).RLock()`, 해제 함수는 `Unlock`+`Close`. `TUIRunning`: 새 `flock.New(p.TUILock).TryLock()` — 성공이면 즉시 `Unlock` 후 `false`, 실패(이미 잠김)면 `true`, 오류면 `(false, err)`. `AcquireDaemon`: `TryLock` 실패 → `ErrAlreadyRunning`. 같은 프로세스의 다른 `Flock` 인스턴스끼리도 충돌하는지 Linux·macOS·Windows CI 에서 위 시험으로 확인된다.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/auth/ ./internal/instance/ -v -count=1` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/auth internal/instance && git commit -m "feat(core): credentials, device id, tui/daemon file locks"`

---

### Task 6: `textwidth` — 정규화·폭·자르기·줄바꿈

**Files:**
- Modify: `internal/textwidth/{sanitize.go,width.go}`
- Test: `internal/textwidth/sanitize_test.go`, `internal/textwidth/width_test.go`

**Interfaces:**
- Produces(CA 4.7): `Sanitize`, `SanitizeInput`, `Width`, `Truncate`, `TruncateLeft`, `Wrap`, `PadRight`, `PadLeft`, `Center`, `Fit`

- [ ] **Step 1: 실패 시험 작성**

```go
var sanitizeCases = []struct{ in, want string }{
	{"❄️ 첫눈", "❄ 첫눈"}, {"⚠️", "⚠"}, {"👍🏽", "👍"}, {"👨‍👩‍👧 가족", "👨 가족"}, {"1️⃣", "1"},
	{"🇰", "□ "}, {"🇰🇷", "🇰🇷"}, {"क्षि", "क"}, {"가", "ᄀ"},
	{"\x1b[31m빨강\x1b[0m", "빨강"}, {"\x1b]8;;https://x\x07링크\x1b]8;;\x07", "링크"},
	{"a\tb", "a    b"}, {"a\x00b\x07c\u0085d", "abcd"}, {"줄\n바꿈", "줄바꿈"},
	{"\xff\xfe", "□"}, {"�", "□"}, {"태그\U000E0067", "태그"},
}
func TestSanitizeCases(t *testing.T)     // each: Sanitize(in)==want
func TestSanitizeInvalidUTF8(t *testing.T) // Review Focus 4: invalid bytes and U+FFFD never survive; result utf8.ValidString
func TestSanitizeProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 10000; i++ {
		s := randomHostile(rng) // runes drawn from pools: ASCII, Hangul syllables, Jamo U+1100–11FF, CJK, emoji, VS15/VS16, ZWJ,
		                        // skin tones, regional indicators, keycap, tag chars, combining marks, Devanagari, C0/C1, ESC sequences, invalid bytes
		out := textwidth.Sanitize(s)
		if textwidth.Sanitize(out) != out { t.Fatalf("not idempotent %q", s) }
		for g, rest := "", out; rest != ""; rest = rest[len(g):] {
			g, _ = ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
			if ansi.StringWidth(g) != ansi.StringWidthWc(g) { t.Fatalf("%q: grapheme %q %d/%d", s, g, ansi.StringWidth(g), ansi.StringWidthWc(g)) }
		}
	}
}
func TestSanitizeInputKeepsNewlines(t *testing.T) // "a\r\nb\rc\n" => "a\nb\nc\n"; "❄️\t" => "❄    "; property test as above with "\n" allowed
func TestTruncate(t *testing.T) {
	// ("가나다",5,"…")=="가나…"; ("가나다",4,"…")=="가… " (wide char not split; 1 leftover cell padded); ("ab",5,"…")=="ab";
	// ("\x1b[1m가나다\x1b[0m",3,"…") Width==3 and ansi.Strip == "가…"; ("가나",0,"…")==""
}
func TestTruncateLeft(t *testing.T) // ("가나다",5,"…")=="…나다"
func TestWrap(t *testing.T) {
	// Wrap("가나다라마바사",5) == []string{"가나","다라","마바","사"}
	// Wrap("오늘 배포 몇 시에 해요?",10) every line Width<=10, strings.Join(lines,"") without spaces == input without spaces
	// Wrap(strings.Repeat("x",25),10) == 3 lines; Wrap("",10)==[]string{""}; Wrap(s,0) == []string{""} (no panic)
}
func TestPadAndCenter(t *testing.T) // PadRight("가",4)=="가  "; PadLeft("가",4)=="  가"; Center("가",5)==" 가  "; PadRight("가나다",3)=="가 "
func TestFit(t *testing.T)           // Fit([]string{"가나다"},5,2)==[]string{"가나 ","     "}; Fit(nil,0,0)==[]string{}; Fit(lines,3,1) truncates extra lines
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/textwidth/` · Expected: FAIL

- [ ] **Step 3: 구현** — CA 8.2 규칙 1–7 순서 그대로. 이 계획의 추가 결정(표시 전용, CA 8.2 를 좁히지 않음): 규칙 1 에서 **잘못된 UTF-8 바이트와 U+FFFD 를 `□`(U+25A1)로** 바꾼다(무결성 검사 3번이 U+FFFD 를 금지하므로). `\n` 은 `Sanitize` 에서 제거(한 줄 표시용), `SanitizeInput` 에서는 유지(`\r\n`/`\r` → `\n`). 규칙 7 의 그래핌 분할은 `ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)`. `Truncate` = `ansi.Truncate` 후, 실제로 잘렸고 결과 폭이 `w-1` 이면 끝에 공백 1칸. `Wrap`: `ansi.Wrap(s, w, "")` 로 단어 경계·강제 줄바꿈을 하되, 한글·CJK 는 음절 사이에서 끊을 수 있게 그래핌 단위 그리디 채우기(공백은 줄 끝에서 버림). `w <= 0` → `[]string{""}`. `Fit` 은 각 줄 `PadRight(Truncate(line, w, ""), w)`, 줄 수는 정확히 `h`.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/textwidth/ -v -count=1` · Expected: PASS (property 시험 포함 수 초 이내)

- [ ] **Step 5: 커밋** — `git add internal/textwidth && git commit -m "feat(core): width-stable unicode sanitize, truncate, wrap, fit"`

---

### Task 7: `humanize` · `slash` · `termcaps`

**Files:**
- Modify: `internal/humanize/humanize.go`, `internal/slash/slash.go`, `internal/termcaps/termcaps.go`
- Test: `internal/humanize/humanize_test.go`, `internal/slash/slash_test.go`, `internal/termcaps/termcaps_test.go`

**Interfaces:**
- Consumes: `api.PollInput`(Task 2 가 완성해 둔 타입)
- Produces(CA 4.9): `humanize.Clock/MessageTime/Relative/Count/Duration/DayDivider`, `slash.Name` 상수, `slash.Command`, `slash.Parse(input string) (*Command, bool, error)`, `slash.Catalog() []Spec`, `slash.Spec`, `termcaps.Graphics`/상수, `termcaps.Caps`, `termcaps.Detect(env []string, override string) Caps`

- [ ] **Step 1: 실패 시험 작성**

```go
var seoul = mustLoc("Asia/Seoul") // import _ "time/tzdata"
var now = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC) // 11:05 KST, Thursday
func TestClock(t *testing.T)       // (now,seoul,false)=="11:05"; (now,seoul,true)=="오전 11:05"; 13:05 KST h12 => "오후 1:05"; 00:30 h12 => "오전 12:30"
func TestMessageTime(t *testing.T) // now-3m => "11:02"; yesterday 23:10 KST => "어제 23:10"; 2026-11-03 11:05 => "11월 3일 11:05"; 2025-11-03 => "2025. 11. 3."
func TestRelative(t *testing.T)    // 30s "방금"; 3m "3분"; 2h "2시간"; yesterday "어제"; 2026-11-03 "11월 3일"; 2025-01-02 "2025. 1. 2."; future => "방금"
func TestCount(t *testing.T)       // 0 "0"; 999 "999"; 1000 "1천"; 1200 "1.2천"; 9999 "9.9천"; 10000 "1만"; 34000 "3.4만"; 120000000 "1.2억"
func TestDuration(t *testing.T)    // 45s "45초"; 25m "25분"; 90m "1시간 30분"; 2h "2시간"; 0 "0분"
func TestDayDivider(t *testing.T)  // now => "2026년 11월 5일 목요일"

func TestSlashParse(t *testing.T) {
	// "hello" => ok=false; "/" => ok=false; "/me 춤춘다" => {Name:Me, Text:"춤춘다"}
	// "/shrug 몰라" => {Shrug, Text:"몰라 ¯\\_(ツ)_/¯"}; "/shrug" => Text "¯\\_(ツ)_/¯"
	// `/poll "점심 메뉴" "국밥" "돈까스"` => Poll{Question:"점심 메뉴", Options:[국밥 돈까스]}
	// `/poll “점심” “A” “B” --multi --for 1h` => Multi true, DurationSec 3600
	// `/poll "질문" "A"` => err "선택지를 2개 이상 적어 주세요"; 11 options => err "선택지는 10개까지예요"
	// "/status 🎧 듣는 중" => Emoji "🎧", Text "듣는 중"; "/status 회의 중" => Emoji "", Text "회의 중"
	// "/focus" => {Focus} zero; "/focus 25m" => 25*time.Minute; "/focus 1h30m"; "/focus off" => FocusOff; "/focus abc" => err
	// "/roll 2d6" => Dice "2d6"; "/roll 30d6" => err; "/roll 2d6+3" ok
	// "/invite @MinJi" => InviteUser "minji"; "/invite link 7d 10" => InviteLink, LinkExpires 168h, LinkMaxUses 10; "/invite link" => 0,0
	// "/nope x" => ok=true, err "알 수 없는 명령이에요: /nope"
}
func TestCatalogOrder(t *testing.T) // names me, shrug, poll, status, focus, roll, invite; every Usage starts with "/"+name; Desc non-empty Korean

func TestDetect(t *testing.T) {
	// override "kitty"=>Kitty, "iterm2"=>ITerm2, "halfblock"=>HalfBlock, "off"=>None
	// auto: TMUX=x => HalfBlock+InTmux; TERM=screen-256color => HalfBlock; TERM=xterm-kitty => Kitty; KITTY_WINDOW_ID=1 => Kitty;
	// TERM_PROGRAM=ghostty => Kitty; TERM_PROGRAM=iTerm.app => ITerm2; TERM_PROGRAM=WezTerm => ITerm2; else HalfBlock
	// COLORTERM=truecolor|24bit => TrueColor; Term/Program copied; FocusReports/UnicodeCore false initially
}
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/humanize/ ./internal/slash/ ./internal/termcaps/` · Expected: FAIL

- [ ] **Step 3: 구현** — 위 기대값이 그대로 문구 결정이다. `Count` 는 소수 첫째 자리 **버림**, `.0` 은 생략. `/shrug` 는 api.md 5.10("본문 끝에 `¯\_(ツ)_/¯`")대로 Text 에 붙인다. `/roll` 식 검증은 api.md 2.1 정규식과 범위(개수 1..20, 면 2..1000, 보정 ±1000). `/poll` 인자 토크나이저는 곧은 따옴표 `"`, 둥근 따옴표 `“ ”` 를 짝으로 받고 `--multi`, `--for <dur>`(`time.ParseDuration` + `d` 단위 지원). `/status` 첫 토큰이 이모지(그래핌 하나이고 `ansi.StringWidth==2` 이며 한글·CJK 가 아님)면 Emoji. Catalog 문구: me `"/me <행동>" "행동을 3인칭으로 보내요"`, shrug `"/shrug [글]" "¯\\_(ツ)_/¯ 를 붙여요"`, poll `"/poll \"질문\" \"선택지\"… [--multi] [--for 1h]" "투표를 만들어요"`, status `"/status [이모지] <글>" "상태 메시지를 바꿔요"`, focus `"/focus [25m|off]" "집중 모드를 켜거나 꺼요"`, roll `"/roll 2d6" "주사위를 굴려요"`, invite `"/invite @아이디 | link [7d] [횟수]" "방에 초대해요"`.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/humanize/ ./internal/slash/ ./internal/termcaps/ -v` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/humanize internal/slash internal/termcaps && git commit -m "feat(core): korean humanize, slash command parser, terminal caps"`

---
### Task 8: `api` 기반 — 요청·오류 봉투·쿨다운·`apitest`

**Files:**
- Modify: `internal/api/{client.go,errors.go,describe.go,opt.go,limiter.go}`, `internal/api/apitest/server.go`
- Test: `internal/api/client_test.go`, `internal/api/describe_test.go`, `internal/api/apitest/server_test.go`

**Interfaces:**
- Consumes: `version.UserAgent()`
- Produces(CA 4.3.1): `Client`, `Option`, `WithToken`, `WithHTTPClient`, `WithUserAgent`, `WithOnUnauthorized`, `WithOnUpgradeRequired`, `WithClock`, `New(baseURL string, opts ...Option) *Client`, `BaseURL`, `SetToken`, `Token`, `MediaURL(rel string) string`, `WebSocketURL(mode, deviceID string) string`, `AuthHeader() http.Header`, `Cooldown(limiter string) time.Time`, `Error` + `Error()/Fields()/Limiter()`, `CodeOf`, `IsCode`, `Describe`, `PageOpts`, `Page[T]` + `Done()`, `Opt[T]` + `Some/Null/IsZero/MarshalJSON`, `UserRef`, `Me`, `Handle`, `UserID`; `apitest.Server{URL}`, `apitest.New`, `Handle`, `JSON`, `Error`, `Requests`, `Client`, `Recorded`
- Produces(이 계획의 비공개 결정, Task 10·11 이 쓴다): `func (c *Client) call(ctx context.Context, r request, out any) (status int, err error)` 와 `type request struct{ Method, Path string; Query url.Values; Body any; RawBody io.Reader; ContentType string; ContentLength int64; Limiter string; Auth bool; Timeout time.Duration; Header http.Header }` — `out` 이 non-nil 이면 JSON 디코드(204 는 읽지 않음). 리미터 상수 `rlSignup="RL_SIGNUP" … rlAPI="RL_API"`(api.md 0.7 열 이름 그대로).

- [ ] **Step 1: 실패 시험 작성**

```go
func TestRequestHeaders(t *testing.T) {
	s := apitest.New(t)
	var got http.Header
	s.Handle("PATCH /v1/me/profile", func(w http.ResponseWriter, r *http.Request) { got = r.Header.Clone(); w.Write([]byte(`{"user":{}}`)) })
	s.Client().UpdateProfile(ctx, api.UpdateProfileRequest{Bio: api.Some("hi")})
	// got: Authorization "Bearer msl_test", User-Agent == version.UserAgent(), Accept "application/json",
	// Content-Type "application/json; charset=utf-8"; s.Requests()[0].Body JSON == {"bio":"hi"}
}
func TestErrorEnvelope(t *testing.T) {
	// handler: X-Request-Id "01REQ", 400 {"error":{"code":"invalid_request","message":"body: too_long","details":{"fields":{"body":"too_long"}}}}
	// err.(*api.Error): Status 400, Code "invalid_request", RequestID "01REQ", Fields()["body"]=="too_long"; IsCode(err,"invalid_request")
	// non-JSON 502 body "bad gateway" => Code "internal_error", Status 502
}
func TestCodeOfTransportAndCancel(t *testing.T) // server closed => CodeOf=="network"; canceled ctx => "canceled"; nil => ""; errors.New("x") => ""
func TestUnauthorizedHookOncePerToken(t *testing.T) // 3x 401 {"code":"unauthorized"} with same token => hook called 1; SetToken("msl_new") then 401 => 2
func TestUnauthorizedHookOnlyForUnauthorizedCode(t *testing.T) {
	// 401 {"code":"invalid_credentials"} on ChangePassword (authed) => hook NOT called, returns *Error{Code:"invalid_credentials"};
	// 401 invalid_credentials on Login (Auth:false) => hook NOT called; 401 {"code":"unauthorized"} on Login/Signup/Meta (no token sent) => hook NOT called;
	// 401 with a non-JSON body on an authed call => hook NOT called (code falls back to "invalid_request")
}
func TestUpgradeRequiredHook(t *testing.T)          // 426 details {"min_client_version":"0.2.0"} => hook("0.2.0")
func TestRateLimitCooldown(t *testing.T) {
	// WithClock fixed T. POST /v1/posts => 429 {"limiter":"RL_POST","retry_after_sec":30}, Retry-After: 30
	// Cooldown("RL_POST") == T+30s; second CreatePost (clock T+10s) => *Error{Status:429, Code:"rate_limited", RetryAfter:20s}, len(Requests())==1
	// a different limiter endpoint (Like => RL_REACT) still goes out; RL_API cooldown blocks every authed call
}
func TestSendMessageRetriesOnceWithSameNonce(t *testing.T) {
	// first 429 retry_after_sec 1 limiter RL_MESSAGE, then 201 {"message":{...}}; both recorded bodies have client_nonce "n1"; created==true
	// retry_after_sec 11 => no retry, returns 429 error; ctx canceled during wait => CodeOf=="canceled"
}
func TestOptMarshal(t *testing.T) {
	type X struct{ A api.Opt[string] `json:"a,omitzero"`; B api.Opt[int] `json:"b,omitzero"` }
	// X{} => {}; X{A: api.Some("x")} => {"a":"x"}; X{A: api.Null[string]()} => {"a":null}; X{B: api.Some(0)} => {"b":0}
}
func TestUserRefAndEscaping(t *testing.T) // Handle("MinJi")=="@minji"; reaction path "/v1/messages/M/reactions/%F0%9F%91%8D"; hashtag "/v1/hashtags/%EC%B2%AB%EB%88%88/posts"
func TestURLs(t *testing.T) // New("https://h/").BaseURL()=="https://h"; MediaURL("/v1/media/01X")=="https://h/v1/media/01X"; WebSocketURL("full","D") == "wss://h/v1/ws?device_id=D&mode=full"; http => ws
func TestPageDone(t *testing.T) // NextCursor nil => Done
func TestDescribe(t *testing.T) // table below, plus invalid_request {"fields":{"handle":"invalid"}} => "아이디: 형식이 맞지 않아요"; rate_limited RetryAfter 12s => "너무 빨라요. 12초 뒤에 다시 해 주세요"
```

`apitest/server_test.go`: `Handle`/`JSON`/`Error` 경로 일치, 미등록 경로는 `404 {"error":{"code":"not_found",…}}` 이고 `Requests()` 에 기록됨, 기록된 본문에서 `msl_` 토큰이 `msl_…(redacted)` 로 바뀜. 그리고:

```go
func TestServerReRegisterReplaces(t *testing.T) {
	// JSON("GET /v1/me", 200, A) then JSON("GET /v1/me", 200, B) => no panic, GET /v1/me returns B (later wins)
	// Handle("GET /v1/channels/{id}/messages", h1) then Handle("GET /v1/channels/{cid}/messages", h2) => same shape (wildcard names ignored) => replaced, h2 serves
	// Handle("GET /v1/channels/{id}/pins", h1) then Handle("GET /v1/channels/C9/pins", h2) => both kept; /C9/pins => h2 (more specific, Go 1.22 rule), /C1/pins => h1
	// "/" catch-all + "GET /v1/me" => /v1/me served by the specific one, /x by the catch-all
	// Error("POST /v1/posts", 429, "rate_limited") then JSON("POST /v1/posts", 201, …) => 201
}
func TestServerRecordsEscapedPathAndRawQuery(t *testing.T) {
	// client GET /v1/hashtags/첫눈/posts?q=a b => Recorded.Path == "/v1/hashtags/%EC%B2%AB%EB%88%88/posts" (r.URL.EscapedPath(), NOT decoded),
	// Recorded.Query == r.URL.RawQuery exactly as sent (e.g. "q=a+b"), reaction path "/v1/messages/M/reactions/%F0%9F%91%8D"
}
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/api/... -run 'TestRequestHeaders|TestErrorEnvelope|TestCodeOf|TestUnauthorized|TestUpgrade|TestRateLimit|TestSendMessageRetries|TestOpt|TestUserRef|TestURLs|TestPageDone|TestDescribe|TestServer'` · Expected: FAIL

- [ ] **Step 3: 구현** — CA 4.3.1 "동작 규칙" 그대로. 결정:
  - `call` 은 ctx 에 기한이 없으면 `r.Timeout`(일반 15s, 업로드·미디어 120s)로 감싼다. 전송 오류는 `*Error` 가 아닌 원래 오류를 `%w` 로 감싸 돌려주고 `CodeOf` 가 `errors.Is(err, context.Canceled|DeadlineExceeded)` → `"canceled"`, 그 밖 `*url.Error`/`net.Error` → `"network"`.
  - 오류 본문 파싱 실패(JSON 아님) → `Code` = 5xx 면 `"internal_error"`, 4xx 면 `"invalid_request"`.
  - 쿨다운: `429` → `cooldown[details.limiter] = now + retry_after_sec`(`now` = `WithClock` 시계, 없으면 `time.Now`). 요청 전 `r.Limiter` 와(인증 요청이면) `"RL_API"` 의 쿨다운을 본다.
  - `OnUnauthorized` 호출 조건(CA 4.3.1 "401 unauthorized" 를 좁혀 확정): **토큰을 실어 보낸 요청(`r.Auth && token != ""`)이 `401` 이고 파싱된 `code == "unauthorized"` 일 때만**, 같은 토큰당 1회(`SetToken` 으로 토큰이 바뀌면 다시 1회 가능). 상태 코드만으로 판정하지 않는다 — api.md 에서 로그인 실패와 비밀번호 변경의 현재 비밀번호 오류도 `401 invalid_credentials` 이므로, 상태 코드로 판정하면 설정 화면에서 비밀번호를 틀린 사용자가 강제 로그아웃되고 로그인 화면의 오류 문구가 "다른 곳에서 로그아웃됐어요" 로 덮인다. `invalid_credentials`·본문 파싱 실패 401 은 평범한 `*Error` 로만 돌려준다.
  - `Describe` 문구(이 계획의 결정, `code` → 한국어):

| code | 문구 |
|---|---|
| invalid_json | 요청 형식이 올바르지 않아요 |
| invalid_request | `<필드 라벨>: <사유>`(필드 이름 정렬 후 첫 번째). 라벨: handle 아이디, password 비밀번호, display_name 표시 이름, body 본문, name 이름, bio 소개, text 상태 글, q 검색어, 그 외 필드 이름 그대로. 사유: required 필요해요, too_long 너무 길어요, too_short 너무 짧아요, invalid 형식이 맞지 않아요, not_allowed 허용되지 않아요. fields 없으면 `입력값을 확인해 주세요` |
| unauthorized | 로그인이 필요해요 |
| invalid_credentials | 아이디 또는 비밀번호가 맞지 않아요 |
| account_suspended | 정지된 계정이에요 |
| forbidden | 권한이 없어요 |
| not_admin | 운영자만 할 수 있어요 |
| dm_not_allowed | 이 사용자에게는 DM을 보낼 수 없어요 |
| action_not_allowed | 지금은 할 수 없는 동작이에요 |
| read_only | 읽기 전용 대화예요 |
| private_account | 비공개 계정이에요 |
| banned_from_space | 이 방에 들어갈 수 없어요 |
| not_found | 찾을 수 없어요 |
| handle_taken | 이미 쓰는 아이디예요 |
| already_exists | 이미 있어요 |
| already_member | 이미 멤버예요 |
| conflict | 지금 상태에서는 할 수 없어요 |
| poll_closed | 마감된 투표예요 |
| invite_invalid | 초대가 만료됐거나 더 쓸 수 없어요 |
| payload_too_large | 파일이 너무 커요 (최대 5MB) |
| unsupported_media_type | 지원하지 않는 이미지 형식이에요 |
| limit_exceeded | 한도를 넘었어요 (최대 `<details.limit>`) |
| upgrade_required | masil을 업데이트해 주세요 (최소 `<min_client_version>`) |
| rate_limited | 너무 빨라요. `<ceil(RetryAfter초)>`초 뒤에 다시 해 주세요 |
| internal_error | 서버 오류가 났어요 |
| service_unavailable | 서버가 잠시 바빠요. 다시 시도해 주세요 |
| network | 서버에 연결할 수 없어요 |
| canceled | 취소됐어요 |
| 그 밖 | 알 수 없는 오류예요 (`<code>`) |

  - `apitest.Server`: `httptest.NewServer`, `Cleanup` 에서 닫음. `Client(opts...)` = `api.New(s.URL, append([]api.Option{api.WithToken("msl_test")}, opts...)...)`. `Requests()` 는 복사본, 본문의 `msl_[A-Za-z0-9_-]+` 를 가림.
  - **라우팅(이 계획의 결정 — 다른 계획의 시험이 기대는 규칙)**: `http.ServeMux` 에 직접 등록하지 않는다(Go 1.22 mux 는 같은 패턴·같은 모양 패턴을 두 번 등록하면 panic). 서버는 자체 **라우트 표** `[]route{key, pattern, handler}` 를 뮤텍스로 보관하고, `Handle`/`JSON`/`Error` 는 `key` = 메서드 + 와일드카드 이름을 지운 경로 모양(`GET /v1/channels/{}/messages`)으로 찾아 **있으면 그 자리의 핸들러를 바꾼다(나중 등록이 이김, replace)**, 없으면 뒤에 붙인다. 등록이 바뀔 때마다 표로 새 `http.ServeMux` 를 만들어 갈아 끼우고(요청은 현재 mux 로 디스패치), 그래서 **매칭은 Go 1.22 패턴 규칙 그대로**(더 구체적인 패턴이 이김, `{x...}`·메서드 없는 패턴·`/` catch-all 포함)다. 모양이 다르지만 서로 겹치고 어느 쪽도 더 구체적이지 않은 두 패턴(Go mux 가 panic 하는 경우)은 등록 시점에 `tb.Fatalf("apitest: pattern %q conflicts with %q", …)` 로 알린다. 매칭 실패는 위 404 봉투.
  - **기록(`Recorded`, CA 4.3.1 이 비워 둔 의미를 확정)**: `Method` = `r.Method`, `Path` = `r.URL.EscapedPath()`(**이스케이프된 경로** — 디코드하지 않는다; 클라이언트가 세그먼트마다 `url.PathEscape` 한 결과를 그대로 검증하기 위함, 예 `/v1/hashtags/%EC%B2%AB%EB%88%88/posts`), `Query` = `r.URL.RawQuery`(**보낸 그대로의 원문**, 디코드·정렬하지 않음), `Body` = 토큰을 가린 원문 바이트. 다른 계획의 시험이 디코드된 경로를 기대하면 그 시험이 `url.PathUnescape(rec.Path)` 로 비교해야 한다(오케스트레이터 전달 사항 — sns-fun `TestHashtagGolden` 의 "apitest records decoded path" 가정과 충돌).

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/api/... -count=1` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/api && git commit -m "feat(core): api client core, error envelope, cooldowns, fake server"`

---

### Task 9: `api` 타입 (api.md 2장 1:1)

**Files:**
- Modify: `internal/api/types.go`, `internal/api/bundles.go`
- Create: `internal/api/testdata/types/<TypeName>.json`(타입마다 하나, 모든 필드를 채운 예시)
- Test: `internal/api/types_test.go`

**Interfaces:**
- Produces: CA 4.3.2 의 모든 타입과 응답 묶음 타입. 이 계획의 결정:
  - 열거형 필드에 타입을 쓰는 곳: `Friend.Presence`, `PresenceState.Status`, `PresenceItem.Status` → `PresenceStatus`; `SpaceMember.Role`, `Space.MyRole` → `SpaceRole`; `Channel.Kind`, `ChannelRef.Kind` → `ChannelKind`; `Alert.Kind` → `AlertKind`; `Notification.Type` → `NotificationType`. 그 밖 열거형 문자열은 `string`. 상수: `PresenceOnline/PresenceAway/PresenceDND/PresenceOffline`, `RoleOwner/RoleAdmin/RoleMember`, `ChannelText/ChannelDM/ChannelGroupDM`, `AlertDM/AlertMention/AlertThreadReply/AlertInvite/AlertFriendRequest/AlertChannelMessage/AlertSocial/AlertFocusSummary`.
  - `PollInput struct{ Question string `json:"question"`; Options []string `json:"options"`; Multi bool `json:"multi,omitempty"`; DurationSec *int `json:"duration_sec,omitempty"` }`
  - 중첩 익명 객체는 이름 있는 타입: `PostViewer{Liked, Reposted bool}`, `NotificationSubject{Type, ID string}`, `NotificationContext{PostID, ChannelID, MessageID, ThreadRootID, SpaceID, SpaceName *string}`, `AlertTarget{Type, ID string; ChannelID, MessageID, ThreadRootID, PostID, SpaceID *string}`, `FocusCounts{DM, Mention, ThreadReply, Invite, FriendRequest, Other int}`, `NotifySettings{DM, Mention, Invite, FriendRequest, ThreadReply, Social bool}`, `SystemEventData{Name, Topic, MessageID string /* omitempty */}`, `FriendRequestRef{ID, Direction string}`.
  - `Report.Snapshot`·`Error.Details` 는 `json.RawMessage`.

- [ ] **Step 1: 실패 시험 작성**

```go
// Every response field always exists (api.md header), so a strict decode of a full sample proves no Go field is missing or misspelled.
func TestTypesStrictRoundTrip(t *testing.T) {
	for name, newV := range map[string]func() any{
		"UserSummary": func() any { return new(api.UserSummary) }, "Profile": func() any { return new(api.Profile) },
		"Me": func() any { return new(api.Me) }, "Message": func() any { return new(api.Message) }, "Post": func() any { return new(api.Post) },
		/* … one entry per type in CA 4.3.2 list and each bundle type … */
	} {
		raw := mustRead(t, "testdata/types/"+name+".json")
		dec := json.NewDecoder(bytes.NewReader(raw)); dec.DisallowUnknownFields()
		v := newV(); if err := dec.Decode(v); err != nil { t.Fatalf("%s: %v", name, err) }
		out, _ := json.Marshal(v)
		if !sameKeysDeep(raw, out) { t.Errorf("%s: key set differs after re-marshal", name) } // every key in raw appears in out, recursively
	}
}
func TestEventsExamplesDecode(t *testing.T)  // events.md 4.2 message example -> api.Message (Author.Handle "sam", Mentions[0].Handle "minji"); 4.7 alert example -> api.Alert{Kind: AlertMention}
func TestQuotedPostUnion(t *testing.T)       // {"available":true,"post":{…}} and {"available":false,"id":"X","reason":"deleted"}
func TestNullableTimes(t *testing.T)         // "edited_at":null => nil; RFC3339 ms "2026-11-05T02:03:04.567Z" parses
func TestEmbeddedFlatten(t *testing.T)       // Profile/Me/Space/SpaceDetail/Upload marshal flat (no "UserSummary" key)
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/api/ -run 'TestTypes|TestEventsExamples|TestQuotedPost|TestNullable|TestEmbedded'` · Expected: FAIL

- [ ] **Step 3: 구현** — CA 4.3.1 "타입 변환 규칙" 대로 api.md 2장 인터페이스마다 Go 구조체, 응답 묶음 타입은 CA 4.3.2 목록 그대로(JSON 태그 snake_case, `HashtagSuggestion.PostCount24h` 는 `post_count_24h`). 샘플 JSON 은 api.md 2장 필드 전부를 채워 작성한다.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/api/ -run 'TestTypes|TestEventsExamples|TestQuotedPost|TestNullable|TestEmbedded' -v` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/api && git commit -m "feat(core): api wire types mirroring api.md section 2"`

---

### Task 10: `api` 엔드포인트 A — 메타·인증·나·사용자·친구·차단·팔로우·DM·방·초대·채널

**Files:**
- Modify: `internal/api/ep_{auth,me,users,social,dms,spaces,channels}.go`(메타는 `ep_auth.go`)
- Test: `internal/api/endpoints_a_test.go`, `internal/api/endpoint_harness_test.go`

**Interfaces:**
- Consumes: Task 8 `call`/`request`, Task 9 타입
- Produces: CA 4.3.3 의 "4 meta" ~ "5.9 channels" 블록 메서드 **전부**(정확한 시그니처는 CA 4.3.3 코드 블록), 요청 구조체 `SignupRequest`, `LoginRequest`, `UpdateProfileRequest`, `UpdateSettingsRequest`, `NotifySettingsPatch`, `SetStatusRequest`, `OpenDMRequest`, `CreateSpaceRequest`, `UpdateSpaceRequest`, `SpaceMembersQuery`, `RemoveMemberOpts`, `CreateInviteRequest`, `CreateChannelRequest`, `UpdateChannelRequest`, `ChannelPrefsRequest`

- [ ] **Step 1: 실패 시험 작성** — 엔드포인트 하나 = 표 한 줄. 공용 하네스(`endpoint_harness_test.go`):

```go
type epCase struct {
	name     string
	route    string // apitest pattern, e.g. "POST /v1/friends/requests"
	status   int
	resp     string // canned JSON ("" for 204)
	call     func(c *api.Client) (any, error)
	wantPath string // exact escaped path (compared to Recorded.Path == r.URL.EscapedPath())
	wantQuery string // url.Values.Encode() of expected query ("" if none); compared to url.ParseQuery(Recorded.Query) semantically (RawQuery as sent)
	wantBody string // expected JSON (semantic compare) or "" for none
	check    func(t *testing.T, got any)
}
func runCases(t *testing.T, cases []epCase) // per case: fresh apitest server, s.JSON/Handle(route,…), call, assert Requests()[0] Method/Path/Query/Body, check(got)
```

`endpoints_a_test.go` 는 위 범위의 **모든 메서드마다 한 줄**을 둔다(메서드 이름 = `name`, 누락 검사: 시험이 `reflect` 로 `*api.Client` 의 메서드 집합에서 이 범위 목록과 표의 `name` 집합을 비교). 반드시 포함할 특수 사례:

```go
{name: "Signup", route: "POST /v1/auth/signup", status: 201, resp: authResultJSON,
 call: func(c *api.Client) (any, error) { return c.Signup(ctx, api.SignupRequest{Handle: "minji", Password: "correct horse battery", DisplayName: "민지", Client: "tui", DeviceName: "mbp"}) },
 wantPath: "/v1/auth/signup", wantBody: `{"handle":"minji","password":"correct horse battery","display_name":"민지","client":"tui","device_name":"mbp"}`},
{name: "Login", /* omitempty: no display_name key */},
{name: "GetUser", call: c.GetUser(ctx, api.Handle("MinJi")), wantPath: "/v1/users/@minji", resp: `{"user":{…}}`},
{name: "GetPresence", wantQuery: "user_ids=A%2CB"}, // and a separate test: 101 ids => error, no request sent
{name: "SendFriendRequest", status: 200 /* auto-accept */, check: AutoAccepted==true && Friend!=nil}, // and 201 => AutoAccepted false
{name: "Follow", resp: `{"state":"pending"}`, check: state=="pending"},
{name: "OpenDM", status: 200, check: created==false}, // 201 => created true; body {"users":["@junho","@hana"],"name":"점심팟"}
{name: "RemoveSpaceMember", wantPath: "/v1/spaces/S/members/@me", wantQuery: "ban=true&reason=spam"},
{name: "GetFocus", resp: `{"focus":null}`, check: got==(*api.Focus)(nil)},
{name: "UpdateSettings", wantBody: `{"notify":{"social":true},"focus_default_min":30}`},
{name: "SetStatus", wantBody: `{"emoji":null,"text":"회의 중","expires_in_sec":3600}`},
{name: "CreateInvite", wantBody: `{}` /* both omitted => server default 7d */},
{name: "MarkRead", route: "PUT /v1/channels/C/read", wantBody: `{"message_id":"M"}`},
{name: "ChangePassword", wantBody: `{"current_password":"a","new_password":"b"}`},
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/api/ -run TestEndpointsA` · Expected: FAIL(스텁이 요청을 보내지 않음)

- [ ] **Step 3: 구현** — 메서드마다 api.md 5장 해당 절의 메서드·경로·쿼리·본문·응답 봉투 키(`{"user":…}`, `{"items":…}`, `{"channel":…}` …)를 그대로 따른다. 리미터: signup `RL_SIGNUP`, login `RL_LOGIN`, 친구 요청·팔로우·차단·DM 생성·초대 생성·직접 초대·방 생성 `RL_SOCIAL`, `SearchUsers` `RL_SEARCH`, 그 밖 인증 요청은 `RL_API` 만. `Signup`/`Login`/`Meta`/`Health` 는 `Auth:false`. 경로 매개변수는 세그먼트마다 `url.PathEscape`. `GetPresence` 는 100 초과 시 요청 없이 `&Error{Status:400, Code:"invalid_request"}`.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/api/ -run TestEndpointsA -v` · Expected: PASS(표의 모든 줄 + 메서드 집합 누락 없음)

- [ ] **Step 5: 커밋** — `git add internal/api && git commit -m "feat(core): api endpoints meta/auth/me/users/social/dms/spaces/invites/channels"`

---

### Task 11: `api` 엔드포인트 B — 메시지·스레드·리액션·검색·업로드·글·타임라인·스토리·알림함·투표·방명록·신고·운영자

**Files:**
- Modify: `internal/api/ep_{messages,search,media,posts,stories,notifications,polls,guestbook,admin}.go`(신고는 `ep_admin.go`)
- Test: `internal/api/endpoints_b_test.go`

**Interfaces:**
- Consumes: Task 10 의 `runCases`
- Produces: CA 4.3.3 의 "5.10 messages" ~ "5.22 admin" 블록 메서드 전부와 요청 구조체 `MessageQuery`, `SendMessageRequest`, `ThreadQuery`, `ThreadReplyRequest`, `MessageSearchQuery`, `PostSearchQuery`, `CreatePostRequest`, `TimelineQuery`, `CreateStoryRequest`, `NotificationQuery`, `MarkNotificationsRead`, `ReportRequest`, `AdminReportQuery`, `AdminRemoveRequest`, `AdminSuspendRequest`, `AdminUserQuery`, `AdminActionQuery`

- [ ] **Step 1: 실패 시험 작성** — Task 10 과 같은 방식(범위 메서드 집합 누락 검사 포함). 특수 사례:

```go
{name: "ListMessages", wantQuery: "around=M&limit=50"},              // and Before+After both set => client error, no request
{name: "SendMessage", status: 201, wantBody: `{"client_nonce":"n1","kind":"poll","body":"","poll":{"question":"점심","options":["국밥","돈까스"]}}`, check: created},
{name: "SendMessage/dup", status: 200, check: !created},
{name: "AddReaction", wantPath: "/v1/messages/M/reactions/%F0%9F%91%8D"},
{name: "Upload", route: "POST /v1/uploads", wantQuery: "purpose=post"}, // handler asserts Content-Type "image/png", Content-Length "4", body bytes {1,2,3,4}
{name: "FetchMedia", /* If-None-Match "e1" => 304 => NotModified true, Data nil; 200 => Data, ContentType, ETag */},
{name: "CreatePost", wantBody: `{"client_nonce":"n","body":"오늘 첫 눈 ❄️ #첫눈","media_upload_ids":["U"]}`},
{name: "FollowingTimeline", wantQuery: "since=I&limit=30", resp: `{"items":[],"next_cursor":null,"gap":true}`, check: Gap},
{name: "HashtagPosts", wantPath: "/v1/hashtags/%EC%B2%AB%EB%88%88/posts"},
{name: "MarkNotificationsRead", wantBody: `{"all":true}`, check: unread==0}, // zero or two of IDs/All/Before set => client error
{name: "Vote", wantBody: `{"option_idxs":[0,2]}`},
{name: "WriteGuestbook", wantBody: `{"body":"놀러 왔어요","is_secret":true}`},
{name: "Report", status: 201, resp: `{"report_id":"R"}`, check: id=="R"},
{name: "AdminSuspend", wantBody: `{"reason":"spam","duration_sec":604800}`},
{name: "AdminListUsers", wantQuery: "q=kim&suspended=true"},
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/api/ -run TestEndpointsB` · Expected: FAIL

- [ ] **Step 3: 구현** — api.md 5.10–5.22 그대로. 리미터: 메시지 전송·스레드 답글·메시지 수정 `RL_MESSAGE`; 글·답글·인용·스토리·방명록 작성 `RL_POST`; 리액션·좋아요·리포스트·투표·고정 `RL_REACT`; 업로드 `RL_UPLOAD`; 신고 `RL_SOCIAL`; `/v1/search/*`·`/v1/hashtags/search` `RL_SEARCH`. `SendMessage`/`ReplyThread` 만 Task 8 의 1회 자동 재시도. `Upload`/`FetchMedia` 는 `RawBody`·120s 기본 타임아웃, `FetchMedia(mediaURL)` 은 상대(`/v1/media/…`)·절대 URL 모두 받는다. `MessageQuery` 앵커 2개 이상 → 요청 없이 `invalid_request`.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/api/... -count=1` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/api && git commit -m "feat(core): api endpoints messages through admin"`

---

### Task 12: `realtime` 프로토콜 — 프레임·이벤트 타입·`Decode`

**Files:**
- Modify: `internal/realtime/{protocol.go,events.go}`
- Test: `internal/realtime/protocol_test.go`

**Interfaces:**
- Consumes: Task 9 api 타입
- Produces: CA 4.4 의 `Event`, `(Event).Decode() (any, error)` 와 이벤트 타입 표의 모든 Go 타입(`Hello`, `HelloLimits{MaxFrameBytes, WatchTopicsMax, TypingMinIntervalMS int}`, `Ready`, `Reply`, `ReplyError{Code, Message string}`, … `DeviceTUIState`). 비공개 결정: `parseFrame(b []byte) (Event, error)`, `pingFrame = []byte(`{"type":"ping"}`)`, `encodeResume(ResumePoint) []byte`, `encodeAck(seq int64) []byte`, `encodeTyping(channelID string, threadRootID *string) []byte`, `encodeIdle(bool) []byte`, `encodeWatch([]string) []byte`, `var loggedTypes map[string]bool`(events.md 4장 "기록" 표기 타입).

- [ ] **Step 1: 실패 시험 작성**

```go
func TestParseFrame(t *testing.T)  // events.md 2장 message.created frame => Type, *Seq==1042, TS, Data raw; `{"type":"pong"}` => Type "pong", Seq nil
func TestDecodeAllTypes(t *testing.T) {
	// one sample per events.md 8장 server type (except pong) => Decode returns the CA 4.4 Go type (type switch), key fields asserted:
	// message.pinned {"pinned_by":"U"} => PinEvent.By=="U"; message.unpinned {"unpinned_by":"V"} => By=="V";
	// notifications.read {"ids":null,"all":true,"before":null,"unread_count":0}; hello limits; ready resync_required
}
func TestDecodeUnknownType(t *testing.T) // Type "future.thing" => (nil, nil)
func TestClientFrames(t *testing.T) {
	// string(pingFrame) == `{"type":"ping"}` && len == 15
	// encodeResume(ResumePoint{}) == `{"type":"resume","data":{"epoch":null,"last_seq":null}}`
	// encodeResume({Epoch:&"E", LastSeq:&5}) == `{"type":"resume","data":{"epoch":"E","last_seq":5}}`
	// encodeResume({UseDeviceAck:true}) == `{"type":"resume","data":{"use_device_ack":true}}`
	// encodeAck(9) == `{"type":"ack","data":{"seq":9}}`; encodeTyping("C",nil) == `{"type":"typing","data":{"channel_id":"C","thread_root_id":null}}`
	// encodeIdle(true) == `{"type":"idle","data":{"idle":true}}`; encodeWatch(nil) == `{"type":"watch","data":{"topics":[]}}`
}
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/realtime/ -run 'TestParseFrame|TestDecode|TestClientFrames'` · Expected: FAIL

- [ ] **Step 3: 구현** — 서버 프레임 `{type, seq, ts, data}` 를 `Event` 로. `Decode` 는 `map[string]func(json.RawMessage) (any, error)` 표. `PinEvent` 는 `pinned_by`/`unpinned_by` 를 `By` 로 받는 `UnmarshalJSON`. 클라이언트 프레임은 고정 순서 키로 직접 만든다(`ping` 은 상수 바이트).

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/realtime/ -run 'TestParseFrame|TestDecode|TestClientFrames' -v` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/realtime && git commit -m "feat(core): realtime wire protocol and typed event decoding"`

---

### Task 13: `realtime` 클라이언트 — 연결·재개·하트비트·재접속·전송 큐

**Files:**
- Modify: `internal/realtime/{conn.go,client.go,backoff.go,outbox.go}`
- Test: `internal/realtime/client_test.go`, `internal/realtime/conn_test.go`

**Interfaces:**
- Consumes: Task 12, `api.Client.WebSocketURL/AuthHeader`, `api.Error`
- Produces(CA 4.4): `Mode`, `ModeFull`, `ModeNotify`, `ResumePoint`, `Phase` 상수, `State`, `Options`, `Conn`, `ErrUnauthorized`, `ErrSuspended`, `ErrUpgradeRequired`, `Client`, `New(opts Options) *Client`, `Run(ctx) error`, `Position() ResumePoint`, `MarkProcessed(seq int64)`, `Typing`, `SetIdle`, `Watch`, `Close`. 비공개 결정: `Client.after func(time.Duration) <-chan time.Time`(기본 `time.After`, 같은 패키지 시험이 바꿔 낌), `func backoffDelay(n int, rnd float64) time.Duration`, `coderDial(ctx, url string, h http.Header) (Conn, error)`(업그레이드 실패 응답 본문 ≤1024B 를 api 오류 봉투로 파싱해 `*api.Error` 반환).

- [ ] **Step 1: 실패 시험 작성** — 같은 패키지 시험, `fakeConn{in chan []byte; out chan []byte; closed chan int}` 과 순서대로 conn/오류를 내주는 `fakeDial`, 기록형 `after`.

```go
func TestFirstConnectHandshake(t *testing.T) {
	// server: hello{epoch E, seq 10, heartbeat_interval_ms 25000}; expect client writes resume{"epoch":null,"last_seq":null} first;
	// server: ready{resumed:false, resync_required:false, epoch E, seq 10}; OnState phases == [Connecting, Syncing, Live]
	// OnEvent receives "ready" (not "hello"); State.Epoch=="E"
}
func TestReplayFlagAndDedupe(t *testing.T) {
	// Resume{Epoch:"E", LastSeq:5}: hello; replay seq 4,6,7; ready; live 8, 8(dup), ephemeral typing(seq nil)
	// OnEvent seqs == [6(Replayed),7(Replayed),ready,8,typing]; 4 and second 8 dropped
}
func TestNewEpochResetsBaseline(t *testing.T)   // Position E1/100; reconnect: hello epoch E2 seq 3; ready{epoch E2,resync_required:true}; event seq 4 delivered
func TestPositionUsesProcessed(t *testing.T)    // events 1..5 delivered, MarkProcessed(3) => Position().LastSeq==3; next resume sends last_seq 3
func TestHeartbeat(t *testing.T)                // hello interval 20ms => written frame bytes == pingFrame; no frame for 10s after ping (fake after fires) => conn closed, Dial called again
func TestAckThrottle(t *testing.T)              // MarkProcessed(1..50) within 1s => exactly one ack written per 5s window, carrying the max seq (50)
func TestCloseCodes(t *testing.T) {
	// 1001, 4002 => reconnect with delay 0 and Attempt reset; 1009/1011/4000 => backoff delay requested via after;
	// 4001 => Run returns ErrUnauthorized, final Phase Stopped; 4003 => ErrSuspended; 4010 => ErrUpgradeRequired;
	// 4008 => after(10s) requested before redial; 4009 once => reconnect; 4009 twice within 60s => Phase Stopped with Err != nil,
	// Run keeps blocking until ctx cancel, then returns nil
}
func TestHTTPErrorsBeforeUpgrade(t *testing.T)  // dial err *api.Error 401 => ErrUnauthorized; 403 account_suspended => ErrSuspended; 426 => ErrUpgradeRequired; 429 RetryAfter 7s => after(7s) then redial
func TestBackoffDelay(t *testing.T) {
	// backoffDelay(n, 0) == 0; backoffDelay(n, 0.9999) ≈ min(30s, 500ms<<n): n=0 ~0.5s, 3 ~4s, 6 ~30s(capped), 20 ~30s
}
func TestAttemptResetsAfter60sLive(t *testing.T) // ready, Now advanced 61s, close 1011 => next delay computed with n=0
func TestOutboxRateAndThrottles(t *testing.T) {
	// 60 Typing on distinct channels instantly => <=25 non-ping frames written in any 10s window (fake Now), rest sent later in order
	// Typing same (channel,thread) 5x within 3s => 1 frame; SetIdle(true) twice => 1 frame; after reconnect idle re-sent
	// Watch(60 topics) => frame carries first 50; resent after 120s (fake after) and after reconnect
}
func TestNotifyModeIgnoresFullOnlyFrames(t *testing.T) // Mode notify: Typing/SetIdle/Watch write nothing
func TestCloseStops(t *testing.T)                     // Close() => conn.Close(1000, ...) called; Run returns nil
// conn_test.go (httptest + websocket.Accept):
func TestCoderDialSendsAuthHeaders(t *testing.T)     // server sees Authorization "Bearer msl_test", User-Agent version.UserAgent(); text frames round-trip
func TestCoderDialHTTPError(t *testing.T)            // server replies 401 envelope => *api.Error{Status:401, Code:"unauthorized"}
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/realtime/ -run 'TestFirst|TestReplay|TestNewEpoch|TestPosition|TestHeartbeat|TestAck|TestClose|TestHTTP|TestBackoff|TestAttempt|TestOutbox|TestNotifyMode|TestCoder' -count=1` · Expected: FAIL

- [ ] **Step 3: 구현** — CA 4.4 "동작 규칙"과 events.md 1–3·5장 그대로. 결정:
  - `OnEvent` 는 `pong`·`hello` 를 뺀 모든 서버 프레임을 받는다(`hello.local_tui_connected` 는 `State.TUIOnDevice` 로, `device.tui_state` 는 이벤트 **와** `State.TUIOnDevice` 갱신 둘 다).
  - 중복 제거 기준 = 연결 시작 시 resume 한 `last_seq`, 이후 받은 기록 seq 의 최대. `Position()` 은 `MarkProcessed` 최대값.
  - 4009 두 번(60초 안): `PhaseStopped`, `State.Err = errors.New("realtime: replaced twice within 60s (4009)")`, 소켓 없이 ctx 끝날 때까지 대기 후 `nil`.
  - 전송 큐(`outbox.go`): 슬라이딩 10초 창 25프레임(ping 제외), 넘치면 대기열. typing 은 (채널, 스레드) 별 3초 1회, idle 은 값이 바뀔 때만, watch 는 마지막 목록을 기억해 재접속·120초마다 재전송.
  - 읽기 한도 `conn.SetReadLimit(1<<20)`(서버 프레임은 큰 객체를 실을 수 있음).

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/realtime/ -count=1 -race` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/realtime && git commit -m "feat(core): realtime client with resume, heartbeat, backoff, rate-limited outbox"`

---

### Task 14: `store` — 공유 상태·리듀서·부트스트랩·창

**Files:**
- Modify: `internal/store/{store.go,apply.go,reducer.go,bootstrap.go,window.go,ring.go}`
- Test: `internal/store/apply_test.go`, `internal/store/bootstrap_test.go`, `internal/store/window_test.go`

**Interfaces:**
- Consumes: api 타입·메서드, `realtime.Event`/`Decode` 타입, `apitest`
- Produces(CA 4.5 전부): `Store` 와 필드, `PresenceEntry`, `SyncState`, `New`, `Apply`, `ObserveUser`, `ObserveChannel`, `User`, `IsBlocked`, `IsMe`, `ChannelLabel`, `SpaceChannels`, `TotalUnread`, `FocusEnded`, `Reset`, `Reducer`/`Resyncer`, `RegisterReducer`, `(*Store).Reducer`, `Bootstrap`, `BootstrapMsg`, `ApplyBootstrap`, `SetViewing`, `Viewing`, `Window[T]` 와 메서드, `Ring[T]`

- [ ] **Step 1: 실패 시험 작성** — 이벤트는 `realtime.Event{Type, Seq, Data: mustJSON(…)}` 로 만든다.

```go
func TestChannelUpdatedKeepsPerViewerFields(t *testing.T) // local My.UnreadCount 3, ReadOnly true; channel.updated payload defaults => still 3/true; Name updated
func TestMessageCreatedUnreadRules(t *testing.T) {
	// other author, top-level, kind text, not viewing => My.UnreadCount+1, LastMessageID/At updated
	// own message / thread reply / kind system / SetViewing(ch) => no unread change (last_message_* still updated for top-level)
	// DM channel => moved to DMOrder[0]
	// mentions contains me => MentionCount+1; same message id applied twice => still +1 once
}
func TestHereMentionCountedViaNotification(t *testing.T) // mention_here:true, me not in mentions => no +1; then notification.created{type message_mention, context.message_id same, thread_root_id null} => +1; again => no change
func TestReadStateAndPrefs(t *testing.T)   // read_state.updated sets counts+pointers; channel.prefs_updated sets My.Notify/Muted
func TestSpaceEvents(t *testing.T)         // space.joined adds space+channels, SpaceOrder append; space.updated keeps MyRole; space.member_updated for me => MyRole; space.removed/deleted drop space and its channels; channel.deleted drops channel
func TestSocialEvents(t *testing.T)        // friend.request_received/removed, friend.added/removed, block.updated true/false => IsBlocked; space.invite_received/removed
func TestUserAndSettings(t *testing.T)     // user.updated for me updates Me.DisplayName & Users cache; settings.updated => Settings & Me.Settings
func TestPresenceFocusNotifAlert(t *testing.T) // presence.snapshot/updated => Presence; presence.self => Self; focus.started => Focus; focus.ended => Focus nil, LastFocusSummary set; notification.created/notifications.read => NotifUnread; alert => Alerts.Items()[0]
func TestReducersRunAfterCore(t *testing.T) // fake reducer sees Store already updated; Reset() calls reducer Reset and clears maps
func TestChannelLabel(t *testing.T)         // text "#개발"; dm => other member display name "민지"; group named "점심팟"; group unnamed => "민지, 준호, 하나" (members order, me excluded)
func TestTotalUnreadSkipsMuted(t *testing.T) // muted channel unread not counted, its mentions counted
func TestSpaceChannelsByPosition(t *testing.T)
func TestBootstrapRequests(t *testing.T) {
	// apitest with fixtures-like canned responses: Bootstrap(c)() returns BootstrapMsg with all fields; Requests() paths ==
	// /v1/me, /v1/spaces, /v1/spaces/{each}, /v1/dms, /v1/me/unreads, /v1/notifications/unread-count, /v1/friends,
	// /v1/friends/requests?direction=incoming, /v1/me/direct-invites, /v1/blocks (order not asserted)
	// /v1/dms and /v1/friends requests carry limit=100
	// one endpoint 500 => msg.Err != nil, other fields still filled
}
func TestBootstrapFollowsCursors(t *testing.T) {
	// /v1/dms: page 1 (no cursor) => 100 items + next_cursor "c2"; page 2 (cursor=c2) => 30 items + next_cursor null
	// => BootstrapMsg has 130 DMs in server order, exactly 2 GET /v1/dms recorded (queries "limit=100" and "cursor=c2&limit=100");
	// same for /v1/friends with 2 pages; a server that always returns next_cursor => stops after 20 pages (no Err, 2000 items kept);
	// page 2 returns 500 => Err set, page-1 items still delivered
}
func TestApplyBootstrapBatchesResyncers(t *testing.T) // reducer implementing Resyncer returns cmd => ApplyBootstrap returns cmd producing its msg; Sync.Bootstrapped true; unreads applied to Channel.My
func TestWindow(t *testing.T) {
	// Upsert out of order keeps ascending; same ID replaces; Remove; Update; Replace(items,true,false) flags;
	// PrependOlder/AppendNewer dedupe by ID; Covers(id) rules (CA 4.5); Clear
	// OverlapAnchor(5s): times 11:00:00,:01,:02,:08 => anchor = item at :02 (newest with time <= :08-5s); single item => ""
}
func TestRing(t *testing.T) // NewRing(50); push 60 => Items() len 50, newest first
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/store/` · Expected: FAIL

- [ ] **Step 3: 구현** — CA 4.5 와 events.md 4.2·4.4 규칙. 결정: 멘션 중복 방지는 최근 1,000개 메시지 ID 집합(링), `TotalUnread` 의 안 읽음 합에서 `My.Muted` 채널 제외(멘션은 포함), `ObserveUser` 는 마지막 관측 값이 이김. `Bootstrap` 은 요청들을 고루틴으로 동시에 보내고 모두 기다린 뒤 한 메시지로 돌려준다(첫 오류를 `Err`). 커서 페이지 API 인 `ListDMs`·`ListFriends`(api.md 0장: 기본 limit 30, 최대 100)는 **`PageOpts{Limit: 100}` 로 시작해 `next_cursor` 가 nil 이 될 때까지 같은 고루틴 안에서 이어 받는다**(안전 상한 20페이지 = 2,000개; 상한에 닿으면 받은 데까지 쓰고 오류로 보지 않는다). 중간 페이지가 실패하면 그때까지 받은 항목은 유지하고 `Err` 를 설정한다. 그래서 DM·친구가 30개를 넘어도 사이드바와 Store 에서 빠지지 않는다. 나머지 부트스트랩 엔드포인트는 페이지가 없다(CA 4.3.3 시그니처가 슬라이스를 돌려줌). `ApplyBootstrap` 은 `Users` 를 모든 내장 `UserSummary` 로 채운다.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/store/ -v -count=1` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/store && git commit -m "feat(core): shared store, event reducers, bootstrap, ordered windows"`

---

### Task 15: `notify` — 판정·포맷·공유 기록·OS 백엔드

**Files:**
- Modify: `internal/notify/{decide.go,format.go,ledger.go,backend.go,notifier.go}`
- Test: `internal/notify/decide_test.go`, `internal/notify/ledger_test.go`, `internal/notify/notifier_test.go`

**Interfaces:**
- Consumes: `api.Alert`, `config.Paths`, `config.WriteFileAtomic`, `textwidth.Sanitize`
- Produces(CA 4.6): `Role`, `RoleTUI`, `RoleDaemon`, `OSNotification`, `Backend`, `NewOSBackend`, `Recorder`, `Ledger`, `OpenLedger`, `MarkIfNew`, `Seen`, `Input`, `Decision`(`Skip/Silent/Popup`), `Decide`, `Format`, `Notifier`, `New`, `Handle`

- [ ] **Step 1: 실패 시험 작성**

```go
func TestDecideTable(t *testing.T) {
	// rows in CA 4.6 order, each for RoleTUI and RoleDaemon:
	// replayed && now-created>10m => TUI Silent, Daemon Skip; replayed && 9m => continues
	// suppressed => Silent / Skip; !DesktopEnabled => Silent / Skip; Daemon && TUIPresent => Skip
	// TUI focusKnown && focused && viewing => Silent; TUI focusKnown && !focused && viewing => Popup; TUI !focusKnown => Popup
	// otherwise Popup
}
func TestFormat(t *testing.T) // control chars stripped, VS16 normalized via Sanitize, title 81 cps => 80 with "…", body 201 => 200 with "…", AlertID copied
func TestLedgerDedup(t *testing.T)        // MarkIfNew("a") true, again false; Seen("a") true
func TestLedgerCap500(t *testing.T)       // 501 ids => file has 500, oldest ("0") dropped, order oldest-first
func TestLedgerRecoversCorruptFile(t *testing.T) // file "garbage" => MarkIfNew works, file rewritten valid (Review Focus 3)
func TestLedgerConcurrentGoroutines(t *testing.T) // 2 goroutines x same 200 ids => each id true exactly once overall
func TestLedgerTwoProcesses(t *testing.T) // re-exec helper process marks ids 0..99 while parent marks same; union of "true" results == 100 exactly once each (Review Focus 5)
func TestLedgerLockTimeoutShows(t *testing.T) // hold alerts-shown.lock exclusively elsewhere => MarkIfNew returns (true, err) after ~2s (prefer duplicate over miss)
func TestNotifierShowsOnce(t *testing.T)  // Recorder backend: Handle same Popup alert twice => Shown len 1; Silent => none; returns decisions
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/notify/` · Expected: FAIL

- [ ] **Step 3: 구현** — CA 4.6·3.7 규칙. `MarkIfNew`: `flock.New(lockPath).TryLockContext(ctx(2s), 50ms)` → 읽기(깨졌으면 빈 기록) → 있으면 false → 추가·앞에서 잘라 500 → `WriteFileAtomic(0600)`. 잠금 실패 시 `(true, err)`. `NewOSBackend(appName)`: `beeep.AppName = appName`, `Show` = `beeep.Notify(n.Title, n.Body, "")`(실제 OS 알림이라 시험은 `MASIL_TEST_OS_NOTIFY=1` 일 때만). `Format` 은 코드 포인트 기준(`[]rune`) 길이 제한 후 `textwidth.Sanitize`.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/notify/ -v -count=1` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/notify && git commit -m "feat(core): alert decision policy, shared shown-ledger, OS backend"`

---

### Task 16: `theme` — 테마 4종

**Files:**
- Modify: `internal/theme/{theme.go,palettes.go,styles.go,code.go}`
- Test: `internal/theme/theme_test.go`, golden `internal/theme/testdata/TestSwatchGolden/*.golden`

**Interfaces:**
- Produces(CA 5.1): `Name`, `Charm/Midnight/Cream/Amber/Default`, `Palette`, `Theme`, `Styles`(필드 전부), `Get`, `All`, `Parse`, `(*Theme).UserColor`, `StoryBG`, `CodeStyle`

- [ ] **Step 1: 실패 시험 작성**

```go
func TestPalettesExact(t *testing.T) // table = CA 5.1 (11 tokens x 4 themes); compare RGBA of lipgloss.Color(hex) vs Theme.P.*
func TestMeta(t *testing.T) {
	// All() names [charm midnight cream amber], letters A..D, labels "Charm 보라","미드나잇 블루","크림 파스텔","레트로 앰버";
	// Dark: true,true,false,true; Border: Rounded, Normal, Rounded, Double; Get(Default).Name == Amber
}
func TestParse(t *testing.T) // "AMBER","d","Cream","b" ok; "neon","" not ok
func TestUserColor(t *testing.T) // self => P.Acc; other => [Acc2,Acc3,Acc4][fnv32a(id)%3]; deterministic across calls
func TestStoryBG(t *testing.T)   // accent=>Acc, accent2=>Acc2, accent3=>Acc3, accent4=>Acc4, plain=>Chip, unknown=>Chip
func TestCodeStyle(t *testing.T) // chroma.Keyword => fg Acc + bold; LiteralString => Acc3; LiteralNumber => Acc2; Comment => Dim italic; NameFunction => Acc4; NameBuiltin => Acc2; Operator/Punctuation => FG; GenericDeleted => Acc2; GenericInserted => Acc3; Text => Acc3
func TestStylesInheritBase(t *testing.T) // every style except the bg-owning list has background P.BG; bg-owning: Selected/Cursorline/NewHighlight=SelBG, Badge/BadgeMention=Acc2, Pill/TabActive=Acc, InlineCode/CodeBlock/CodeBar=CodeBG, Mention=SelBG
func TestSwatchGolden(t *testing.T) // per theme t.Run(name): render one line per style "Name: sample 가나 👍" at width 80 via Fit; golden.RequireEqual(Styled)
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/theme/` · Expected: FAIL

- [ ] **Step 3: 구현** — `Styles` 는 `base := lipgloss.NewStyle().Foreground(P.FG).Background(P.BG)` 에서 `Inherit`/복사로 파생. `PaneFocused`/`PaneBlurred` = `Border(theme.Border).BorderForeground(Acc|Line).BorderBackground(BG)`. `Online/Away/DND/Offline` 글자색 acc3/acc/acc2/dim, `Error/Warning/Success` acc2/acc/acc3, `Link` acc4 underline, `Hashtag` acc3, `Mention` selbg bg + acc fg, `MentionMe` 그 위 bold, `Timestamp`/`System`/`Deleted`/`Placeholder` dim(`Deleted` italic), `Reaction` chip bg + fg, `ReactionMine` chip bg + acc fg, `Title` bold acc, `Subtitle` dim, `StatusBar` chip bg + fg, `StatusHint` chip bg + dim, `Tab` dim, `Overlay` base, `OverlayBorder` acc 테두리 색. `CodeStyle` 은 `tt.InCategory`/`InSubCategory` 로 판별.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/theme/ -update && go test ./internal/theme/ -v` · Expected: PASS(골든 4개 생성, 사람이 색 확인)

- [ ] **Step 5: 커밋** — `git add internal/theme && git commit -m "feat(core): four themes with derived styles and code colors"`

---

### Task 17: `markup` — 본문 마크다운 부분집합

**Files:**
- Modify: `internal/markup/{parse.go,render.go,highlight.go,plain.go}`
- Test: `internal/markup/markup_test.go`(package `markup_test` — `uitest.Required` 를 쓰므로, markup 이 나중에 uitest 의 전이 의존이 되어도 순환이 없게), goldens `internal/markup/testdata/TestRenderGolden/*`

**Interfaces:**
- Consumes: `theme.Theme`, `textwidth`, `api.MentionRef`, chroma `lexers.Get`/`lexers.Fallback`/`chroma.Coalesce`
- Produces(CA 4.8): `Doc`, `Block`, `BlockKind`(`Paragraph`, `Code`), `Inline`, `InlineKind`(`Text, Bold, Italic, Code, Link, Mention, Here, Hashtag`), `Parse`, `ParseOptions`, `RenderOptions`, `Render`, `Highlight`, `Plain`

- [ ] **Step 1: 실패 시험 작성**

```go
const sample = "오늘 배포 몇 시에 해요? **급함** `make deploy` _진짜_\n```bash\n$ wrangler deploy --env prod\n```\n@minji @nobody @here 링크 [문서](https://example.com/docs) https://example.com/very/long/path, #golang snake_case"
func TestParse(t *testing.T) {
	// Blocks: Paragraph, Code{Lang:"bash", Code:"$ wrangler deploy --env prod"}, Paragraph
	// inline kinds in order incl. Bold "급함", Code "make deploy", Italic "진짜", Mention "minji", Mention "nobody", Here,
	// Link{Text:"문서",URL:"https://example.com/docs"}, Link bare (trailing "," excluded), "snake_case" stays Text,
	// "#golang" is Text when Hashtags=false and Hashtag("golang") when true; "@x" inside code spans is not a mention; unclosed fence => code to end
}
func TestRenderWidthAndSanitize(t *testing.T) // for w in {200,120,80,50,20,1}: every line Width<=w and StringWidth==StringWidthWc; body with "❄️" renders "❄"
func TestRenderMentions(t *testing.T)  // only handles listed in Mentions styled (Mention style); MyHandle => MentionMe; "@nobody" plain
func TestRenderCompact(t *testing.T)   // Compact => single line, code block => "‹코드 1줄›", newlines => " "
func TestRenderMaxLines(t *testing.T)  // MaxLines 2 on 5-line body => 2 lines, last ends with "… 더 보기"
func TestHighlightFallback(t *testing.T) // lang "nope" => fallback lexer, no panic; each line prefixed "▎", padded to width
func TestPlain(t *testing.T)           // Plain(sample, 30) single line, no "**"/"`", Width<=30, ends "…"
func TestRenderGolden(t *testing.T)    // guard first: len(uitest.Required) != 4 => t.Fatal("uitest.Required empty — Phase 0 must fill it") (no vacuous pass);
                                       // for each uitest.Required width W: t.Run(fmt.Sprint(W)) golden.RequireEqual(ansi.Strip(strings.Join(Render(Parse(sample,{Hashtags:true}), amber, {Width:W, MyHandle:"sam", Mentions:[minji]}),"\n")))
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/markup/` · Expected: FAIL

- [ ] **Step 3: 구현** — api.md 2.1 문법만. 결정: 강조 안은 중첩 없이 평문, `_기울임_` 은 단어 경계(앞이 시작·공백·구두점이고 뒤가 공백 아님)일 때만, `[t](u)` 는 `http(s)://` 만, 맨 URL 끝 `.,)!?:;` 제외, 멘션 정규식은 api.md 2.1(코드 안 제외, `(?<!…)` 는 Go 정규식에 없으므로 앞 글자 수동 검사), 해시태그는 `ParseOptions.Hashtags` 일 때만. `Render` 는 세그먼트마다 `textwidth.Sanitize` 후 스타일, 줄바꿈은 스타일 적용 전 평문 기준 `textwidth.Wrap`. 코드 블록: `S.CodeBar.Render("▎")` + `Highlight` 줄, 줄은 `Width-1` 로 강제 줄바꿈 후 `CodeBlock` 배경으로 채움. 링크는 `RenderOptions.Hyperlinks` 면 `Style.Hyperlink(url)`.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/markup/ -update && go test ./internal/markup/ -v` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/markup && git commit -m "feat(core): markdown subset parser and themed renderer with chroma"`

---

### Task 18: `ui/layout` — 모드·중단점·기하

**Files:**
- Modify: `internal/ui/layout/{mode.go,compute.go}`
- Test: `internal/ui/layout/layout_test.go`, `internal/ui/layout/diagram_test.go`(external `layout_test`)

**Interfaces:**
- Consumes: `config.LayoutConfig`
- Produces(CA 5.2): `Mode` 상수, `Label`, `ID`, `ParseMode`, `Breakpoints`, `DefaultBreakpoints`, `FromConfig`, `Auto`, `Resolve`, `PinMinSize`, `SlotID` 상수, `Rect` + `Contains`/`Inner`, `Pane`, `State`, `Layout`, `Compute`, `(Layout).Pane`, `(Layout).At`

- [ ] **Step 1: 실패 시험 작성**

```go
func TestAutoTable(t *testing.T) // every row of CA 5.2.1 table (200x60 command … 30x5 too_small)
func TestResolvePin(t *testing.T) {
	// pin Command at 160x40 => Command,false; at 159x40 => Standard (Auto result), fellBack true
	// pin Tiny at 200x60 => Tiny,false; pin nil => Auto,false; PinMinSize table: 160x40, 80x24, 60x16, 44x16, 20x6
}
func TestLabelsIDs(t *testing.T) // labels "지휘소","표준","컴팩트","위젯","초소형","—"; ParseMode("auto") => (0,false)
func TestComputeCommand200x60(t *testing.T) // rail{0,0,6,59}, sidebar{6,0,26,59}, main{32,0,78,59}, feed{110,0,56,59}, members{166,0,34,32}, notifications{166,32,34,27}, status{0,59,200,1}; ThreadOpen => thread replaces members
func TestComputeStandard120x40(t *testing.T) // sidebar{0,0,26,39}, main{26,0,94,39}; ThreadOpen => main{26,0,94,23}, thread{26,23,94,16}; FeedSwapped+ThreadOpen => no thread pane
func TestComputeStandardShortThreadFolds(t *testing.T) // Compute(Standard,80,24,ThreadOpen): main H 13, thread H 10; Compute(Standard,100,12,ThreadOpen): B=11, bottom 1<6 => no thread pane, main full height
func TestComputeCompactWidgetTiny(t *testing.T) // compact 120x30: tabs{0,0,120,1} unbordered, main{0,1,120,28}; widget 80x24: tabs, context{0,1,80,1}, main{0,2,80,21}; tiny 50x20: main unbordered
func TestComputeTooSmall(t *testing.T) // 19x10 => one unbordered main {0,0,19,10}, no status; 0x0 => no panes; -5x-5 => no panic, no panes
func TestComputeExhaustive(t *testing.T) {
	// cols 0..320 x rows 0..100 x State{4 combos} x {Auto(cols,rows)} ∪ {pinned m | PinMinSize(m) <= (cols,rows)}:
	// no panic; panes pairwise disjoint; union == full screen (cell count == cols*rows, or empty when 0);
	// bordered => Inner().W>=1 && Inner().H>=1; unbordered => W>=1 && H>=1; command/standard formula widths for main/feed > 0
	// (t.Parallel subtests per mode; must finish in a few seconds; -short samples cols/rows step 3)
}
func TestDiagramGolden(t *testing.T) // external package: guard len(uitest.AllSizes()) != 24 => t.Fatal (Phase 0 fills it; no vacuous pass); for each uitest.AllSizes() size, Compute(Auto,…, State{}) rendered as letters per slot (r,s,m,f,t,b,n,T,c,S), golden per "WxH"
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/layout/` · Expected: FAIL

- [ ] **Step 3: 구현** — CA 5.2.1 `Auto` 규칙 1–6, `Resolve`, 5.2.2 기하 공식 그대로(정수 나눗셈 내림, `clamp`). 음수 크기는 0 으로. 지휘소에서 `rest` 가 너무 작아지는 고정 크기(예: 160×40)에서도 `chat`/`feed` > 0 이 되도록 공식 값을 그대로 쓰되, 시험이 실패하면 `side`/`right` 를 공식의 하한 대신 `min(하한, …)` 로 줄이는 쪽으로 고치고 **CA 수정이 필요하면 멈추고 보고**한다.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/layout/ -update -run TestDiagramGolden && go test ./internal/ui/layout/ -count=1 -v` · Expected: PASS(전수 시험 수 초 이내)

- [ ] **Step 5: 커밋** — `git add internal/ui/layout && git commit -m "feat(core): layout modes, pins and exhaustive geometry"`

---
### Task 19: `ui` 프레임워크 계약 · `keymap` · 타이머 규칙 정적 검사

**Files:**
- Modify: `internal/ui/{component.go,context.go,route.go,msgs.go,timers.go,do.go,overlay.go}`, `internal/ui/keymap/keymap.go`
- Test: `internal/ui/ui_test.go`, `internal/ui/timers_rule_test.go`, `internal/ui/keymap/keymap_test.go`

**Interfaces:**
- Consumes: layout, theme, store, api, config, termcaps, realtime 타입
- Produces(CA 5.3.1–5.3.4·5.3.7): `Size`, `Component`, `KeyHandler`, `MouseHandler`, `InputCapturer`, `Cursorer`, `Titled`, `Helper`, `Watcher`, `ChannelViewer`, `Persistent`, `Completer`(CR-A — Phase 0 에서 이미 완전한 형태; 앱 쪽 라우팅은 Task 27), `Point`, `Badge`, `Context` + `Me()`/`Density()`, `Density` 상수, `Timers`, `RealTimers()`, `After`, `Realtime`, `RouteKind` 상수, `Route` + `Key()`, `Target` 상수, `NavigateMsg`, `BackMsg`, `RouteUpdatedMsg`, `Navigate`, `NavigateTo`, `Back`, `EventMsg`, `RealtimeStateMsg`, `ModeChangedMsg`, `ThemeChangedMsg`, `SetThemeMsg`, `FocusChangedMsg`, `ToastMsg`, `ToastKind` 상수, `ErrorMsg`, `OpenOverlayMsg`, `CloseOverlayMsg`, `RunCommandMsg`, `LoggedOutMsg`, `ResyncStartedMsg`, `ResyncDoneMsg`, `Toast`, `RunCommand`, `OpenOverlay`, `OpenRegisteredOverlay`, `ResultMsg[T]`, `Do[T]`, `Overlay`, `OverlayFocusSummary`.
- Produces(CA 5.5): `keymap.Action`, `Keymap`, `Default`, `Load`, `Binding`, `Matches`, `Help`. 이 계획이 정하는 Action 상수 이름(값 = CA 5.5 표의 id): `AppQuit, PaletteOpen, SearchGlobal, NotificationsOpen, WindowPrefix, FocusNext, FocusPrev, HelpToggle, FeedToggle, TabPrev, TabNext, NavBack, ListUp, ListDown, ListTop, ListBottom, ListPageUp, ListPageDown, ListOpen, ComposeFocus, ComposeSend, ComposeNewline, ComposeComplete, ComposeEditLast, ComposeLeave, MsgThread, MsgReact, MsgEdit, MsgDelete, MsgPin, MsgCopy, MsgActions, ChannelSearch, PostLike, PostRepost, PostQuote, PostReply, PostOpenMedia, PostAuthor, PostCompose`.

- [ ] **Step 1: 실패 시험 작성**

```go
func TestRouteKey(t *testing.T)     // Route{Kind:RouteChannel,ID:"C",Anchor:"M",Query:{"a":"b"}}.Key()=="channel/C/"; Route{Kind:RouteFeed,Arg:"following"}.Key()=="feed//following"
func TestHelpersEmitMsgs(t *testing.T) // Navigate(r)()==NavigateMsg{Route:r}; NavigateTo(r,TargetSide)() Target; Back()==BackMsg{}; Toast("x",ToastError)()==ToastMsg{Text:"x",Kind:ToastError}; RunCommand; OpenOverlay(o)()==OpenOverlayMsg{Overlay:o}; OpenRegisteredOverlay("fun.x","a")
func TestAfterUsesContextTimers(t *testing.T) // recording Timers: After(ctx,3s,m) returns the Timers' cmd; RealTimers().After(time.Millisecond, m)() == m
func TestDoRunsWithContextAPI(t *testing.T)  // apitest server; Do(ctx,"core.x",func(c,a){return a.Meta(c)})() == ResultMsg[*api.MetaInfo]{Tag:"core.x",…}
func TestDensity(t *testing.T)    // Command/Standard Full, Compact Line, Widget Bubble, Tiny Minimal, TooSmall Minimal
func TestCompleterShape(t *testing.T) // CR-A: var _ ui.Completer = completingProbe{} (test-local type with Completing() bool); a type without it fails the assertion `_, ok := any(x).(ui.Completer)`
func TestNoRawTimersInUI(t *testing.T) {
	// walk ../../internal/ui (chat/feed/fun included when present); skip *_test.go, internal/ui/timers.go, internal/ui/uitest/**;
	// go/parser + ast.Inspect: fail on calls tea.Tick/tea.Every/tea.Sequence/time.Sleep/time.After/time.NewTimer
	// (selector on the package's import name), and on any X.Tick(...) in a file importing "charm.land/bubbles/v2/spinner".
	// Error message lists file:line and "use ui.After (CA 5.3.2)".
}
// keymap
func TestDefaultBindings(t *testing.T) // table from CA 5.5: e.g. PaletteOpen ["ctrl+k"], HelpToggle ["?","f1"], ListUp ["k","up"], ListTop ["home"] (gg handled by widgets.List, help "gg/home"),
                                       // ListBottom ["G","end"], ListPageUp ["ctrl+u","pgup"], ComposeNewline ["shift+enter","alt+enter","ctrl+j"], MsgThread ["t","enter"], …
func TestMatches(t *testing.T)         // Matches(tea.KeyPressMsg{Code:'k',Mod:tea.ModCtrl}, PaletteOpen); not ListUp
func TestLoadOverrides(t *testing.T)   // {"palette.open":["ctrl+p"]} => ctrl+p matches, ctrl+k not; {"nope":["x"]} => 1 error, defaults kept; {"help.toggle":["ctrl+"]} => error
func TestHelpKorean(t *testing.T)      // Help(PaletteOpen)[0].Help().Desc == "이동"; every default action has non-empty Korean desc
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/ ./internal/ui/keymap/` · Expected: FAIL

- [ ] **Step 3: 구현** — `RealTimers().After` = `tea.Tick(d, func(time.Time) tea.Msg { return msg })`(규칙 예외 파일 `timers.go`). `Do` 는 `context.Background()` 로 fn 실행(타임아웃은 api 기본값). `Context.Density()` 는 `Mode` 로만. 키 이름 유효성 검사는 `tea.KeyPressMsg.String()` 이 낼 수 있는 형식(`[ctrl+][alt+][shift+]<key>`, `<key>` = 한 글자 또는 `enter|esc|tab|space|backspace|up|down|left|right|home|end|pgup|pgdown|f1..f12|delete|insert`). 도움말 한국어(이 계획의 결정): app.quit 종료, palette.open 이동, search.global 검색, notifications.open 알림, window.prefix 창, focus.next 다음 패널, focus.prev 이전 패널, help.toggle 도움말, feed.toggle 피드, tab.prev 이전 탭, tab.next 다음 탭, nav.back 뒤로, list.up 위, list.down 아래, list.top 맨 위, list.bottom 맨 아래, list.pageup 한 쪽 위, list.pagedown 한 쪽 아래, list.open 열기, compose.focus 입력, compose.send 보내기, compose.newline 줄바꿈, compose.complete 자동완성, compose.edit_last 마지막 글 고치기, compose.leave 나가기, msg.thread 스레드, msg.react 리액션, msg.edit 수정, msg.delete 삭제, msg.pin 고정, msg.copy 복사, msg.actions 메뉴, channel.search 채널 검색, post.like 좋아요, post.repost 리포스트, post.quote 인용, post.reply 답글, post.open_media 이미지, post.author 작성자, post.compose 글쓰기.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/ ./internal/ui/keymap/ -v` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/ui && git commit -m "feat(core): ui contracts, timers, keymap and raw-timer static check"`

---

### Task 20: `uitest` 기초 — 가짜 시계·크기·키 파서·무결성 검사

**Files:**
- Modify: `internal/ui/uitest/{clock.go,keys.go,integrity.go}`(`sizes.go` 는 Task 2 에서 완성)
- Test: `internal/ui/uitest/primitives_test.go`

**Interfaces:**
- Consumes: layout, app `FrameInfo`/`Box`(타입만), lipgloss `Border`
- Produces(CA 10.2): `FakeClock`, `NewFakeClock`, `(*FakeClock).Now/After/Pending`, `Box`, `BoxesOf`, `AssertIntegrity`. (`Size`, `Required`, `ModeSizes`, `AllSizes` 는 Task 2 가 이미 실제 값·구현으로 채웠고 `sizes_test.go` 의 `TestAllSizes` 가 고정한다 — 이 Task 는 `sizes.go` 를 다시 쓰지 않는다.) 비공개 결정: `(*FakeClock).nextDue(until time.Time) (msg tea.Msg, ok bool)`(가장 이른 기한 ≤ until 하나를 꺼내며 시계를 그 기한으로 옮김, 같은 기한이면 등록 순), `parseKey(s string) tea.KeyPressMsg`.

- [ ] **Step 1: 실패 시험 작성**

```go
// TestAllSizes lives in sizes_test.go (Task 2); the -run filters below still include it
func TestFakeClock(t *testing.T) {
	c := uitest.NewFakeClock(t0)
	cmd := c.After(3*time.Second, "a"); if cmd() != nil { t.Fatal("must not block or deliver") }
	c.After(time.Second, "b")() ; c.After(time.Second, "c")()
	// Pending() == [1s,1s,3s]; nextDue(t0+2s) => "b", Now()==t0+1s; then "c"; then none (ok=false)
}
func TestParseKey(t *testing.T)  // "ctrl+k","shift+tab","shift+enter","alt+enter","enter","esc","tab","space","up","pgdown","f1","G","?","[","한","ㄱ" round-trip: parseKey(s).String()==s; printable keys carry Text
func TestIntegrityPasses(t *testing.T)  // synthetic 20x6 screen: bordered double box {0,0,20,5} + status row; AssertIntegrity on recTB => no errors
func TestIntegrityDetects(t *testing.T) {
	// each mutation reported by recTB (not panicking): a line 19 cells; "❄️" in a line (wc width mismatch); missing ╗ corner;
	// side char replaced in left edge; U+FFFD; 7 lines for h=6; overlay box covering last row; box X+W > cols;
	// a pane border cell covered by a higher-Z overlay box is NOT reported; title segment in top edge not reported
}
func TestBoxesOf(t *testing.T) // FrameInfo{Layout: 2 panes (one bordered), Overlays: [{r,true}], Toast:&{r2,true}} => boxes with Z 0,0,10,100
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/uitest/ -run 'TestAllSizes|TestFakeClock|TestParseKey|TestIntegrity|TestBoxesOf'` · Expected: FAIL

- [ ] **Step 3: 구현** — CA 10.2 `AssertIntegrity` 5개 검사. 결정: 위반마다 `tb.Errorf`(최대 20개 후 요약) — `Fatalf` 를 쓰지 않아 한 번에 모두 보이게. 화면은 `ansi.Strip` 후 줄별 그래핌 배열로 만들어 칸 좌표로 검사(넓은 글자는 첫 칸에 그래핌, 다음 칸은 빈 표시). 위 변의 "제목 구간" = 왼쪽 모서리 다음 칸부터, Top 문자가 아닌 칸이 연속되는 첫 구간(제목·배지)과 그 바로 뒤 공백 1칸. 상태줄 검사(4)는 `boxes` 에 레이아웃 창이 하나 이상이고 `TooSmall` 이 아닐 때 마지막 줄이 어떤 Bordered 상자의 일부가 아님을 본다. `parseKey`: 이름 키는 `tea.Key*` 상수, `ctrl+`/`alt+`/`shift+` 접두는 `Mod`, 한 그래핌이면 `Code`=첫 룬, `Text`=그래핌, `"space"` 는 `Code: tea.KeySpace, Text: " "`.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/uitest/ -run 'TestAllSizes|TestFakeClock|TestParseKey|TestIntegrity|TestBoxesOf' -v` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/ui/uitest && git commit -m "test(core): fake clock, size sets, key parser, frame integrity checker"`

---

### Task 21: `widgets` 1 — 그리기 위젯

**Files:**
- Modify: `internal/ui/widgets/{frame.go,placeholder.go,badges.go,chips.go,poll.go,tabs.go,toasts.go,spinner.go,fade.go}`
- Test: `internal/ui/widgets/draw_test.go`, `internal/ui/widgets/gallery_test.go`(goldens) — 둘 다 **`package widgets_test`**(uitest 를 쓰므로 — Global Constraints "시험 패키지 규칙")

**Interfaces:**
- Consumes: theme, textwidth, markup(없음), ui, api 타입, `uitest.NewFakeClock`/`AssertIntegrity`/`Required`(시험, 외부 시험 패키지에서만)
- Produces(CA 5.7): `Frame`, `Box`, `Placeholder`, `Badge`, `PresenceDot`, `Avatar`, `UserName`, `ReactionChips`, `PollView`, `Empty`, `Tabs` + `NewTabs/Set/View/Hit`, `Tab`, `Spinner` + `NewSpinner/Start/Stop/Update/View`, `Toasts`, `Fade` + `Start/Style/Update`

- [ ] **Step 1: 실패 시험 작성** — `ctx := &ui.Context{Theme: theme.Get(theme.Amber), Config: config.Defaults(), Timers: clk, Now: clk.Now, Loc: seoul}`.

```go
func TestFrame(t *testing.T) {
	out := widgets.Frame(th, "#개발", ui.Badge{Count: 3}, true, ui.Size{W: 30, H: 5}, "본문")
	// 5 lines x 30 cells; line0 starts "╔═ #개발  3 ═"…"╗" (Strip); title longer than W-6 truncated with "…"; W=2,H=2 => only corners, no panic
}
func TestBadge(t *testing.T)    // {0}=>""; {3}=>" 3 "; {100}=>" 99+ "; {Dot:true}=>"●"; Mention uses BadgeMention (Styled contains acc2 bg)
func TestPresenceDot(t *testing.T) // online "●", away "◐", dnd "⊘", offline "○"; unknown => "○"
func TestAvatarAndName(t *testing.T) // Avatar(u민지,2) Strip=="민"; Avatar(u,1) == "M" (handle initial upper); UserName self uses Acc
func TestReactionChips(t *testing.T) // [{👍,2,me},{👀,1}] width 80 => one line "👍 2  👀 1" (Strip), mine styled ReactionMine; width 6 => wraps, each line Width<=6
func TestPollView(t *testing.T) {
	// Poll "점심 메뉴" options 국밥 4, 돈까스 2, voter 6, my_votes [0]: line0 "점심 메뉴 · 6명 참여"; "✓ 국밥   ██████████ 4"-like bars:
	// bar cells proportional to vote_count/max(total,1); closed => header suffix " (마감)"; every line Width<=width for width in {80,30,12}
}
func TestTabsOverflow(t *testing.T) // 9 tabs, active 8, width 30 => contains "‹", active label visible, Width==30; Hit maps x to index; Hit outside => ok=false
func TestToastsIconsWidth(t *testing.T) // icons info "○" success "●" error "✕" alert "★": each StringWidth==StringWidthWc==1; lines Width<=width
func TestSpinner(t *testing.T) // Animations on: Start returns cmd recorded in FakeClock (Pending()==[Dot.FPS]); Update(tick) advances frame & re-arms; stale gen ignored; Animations off: Start nil, View static first frame
func TestFade(t *testing.T)    // Start(ctx,"m1") arms 250ms; 4 ticks => Style bg goes SelBG..BG; animations off => Start nil, Style == base
// TestSpinner/TestFade: the tick msg types are unexported, so (external package) these tests use a test-local recording ui.Timers
// that keeps each (d, msg); "Update(tick)" feeds the recorded msg back. Stale gen = keep the first recorded msg, Stop+Start, feed the old one.
func TestEmpty(t *testing.T)   // Empty(th,{40,10},"💬","메시지가 없어요","i 를 눌러 입력") exact 10x40, centered
func TestGalleryGolden(t *testing.T) {
	for _, sz := range uitest.Required { t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
		// compose a full-screen Frame whose content stacks: Tabs, Badge row, PresenceDots, Avatar+UserName, ReactionChips, PollView, Toasts, Empty
		uitest.AssertIntegrity(t, out, sz.W, sz.H, []uitest.Box{{Rect: layout.Rect{0, 0, sz.W, sz.H}, Bordered: true}}, th.Border)
		golden.RequireEqual(t, ansi.Strip(out))
	}) }
}
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/widgets/ -run 'TestFrame|TestBadge|TestPresenceDot|TestAvatar|TestReaction|TestPollView|TestTabs|TestToasts|TestSpinner|TestFade|TestEmpty|TestGallery'` · Expected: FAIL

- [ ] **Step 3: 구현** — 결정: `Frame` 위 변 = 모서리 + Top 1칸 + `" "+title+" "` + (배지 있으면 배지 + " ") + Top 채움 + 모서리, 제목은 `Truncate(W-6)`, 테두리 스타일 `PaneFocused|PaneBlurred`, 내용은 `textwidth.Fit(inner)` 후 각 줄 `Base`. `Box` 는 `style` 의 테두리를 쓰는 같은 규칙(제목 없음). `Placeholder(text)` 는 `Empty(…, "…", text, "")` 를 그리는 `ui.Component`. `PollView` 막대 문자 `█`/`░`, 라벨 폭 = 최장 라벨(최대 폭의 40%), 내 표는 `✓ ` 접두, 선택 행 `Selected`. `Avatar` = `Chip` 배경 `w` 칸에 첫 그래핌(`Sanitize(display_name)`), 글자색 `UserColor`; `w==1` 이면 handle 첫 글자 대문자. `Spinner` 프레임 = `spinner.Dot.Frames`, 간격 `spinner.Dot.FPS`(bubbles v2 `go doc charm.land/bubbles/v2/spinner Dot` 로 확인), 틱 메시지 `spinnerTickMsg{id, gen int}`. `Fade` 단계 색 = `lipgloss.Blend1D(5, SelBG, BG)`, 틱 `fadeTickMsg{id string; gen int}`.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/widgets/ -update -run TestGalleryGolden && go test ./internal/ui/widgets/ -v` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/ui/widgets && git commit -m "feat(core): drawing widgets (frame, badges, chips, poll, tabs, toasts, spinner, fade)"`

---

### Task 22: `widgets` 2 — `List[T]` · `Composer`

**Files:**
- Modify: `internal/ui/widgets/{list.go,composer.go}`
- Test: `internal/ui/widgets/list_test.go`, `internal/ui/widgets/composer_test.go`(goldens 포함) — **`package widgets_test`**(`uitest.Required`·`AssertIntegrity` 를 쓰므로)

**Interfaces:**
- Consumes: keymap(`ListUp` 등), textwidth, bubbles `textarea`
- Produces(CA 5.7): `List[T]` + `NewList/SetItems/Selected/Select/HandleKey/HandleMouse/View/Anchor/Restore/AtBottom/Follow`, `ScrollAnchor`, `Composer` + `NewComposer/Value/SetValue/Reset/Focus/Blur/Focused/Update/View/Height/Cursor/SetCompletion`, `Completion`

- [ ] **Step 1: 실패 시험 작성**

```go
// list
func TestListSelectionByID(t *testing.T)   // SetItems keeps selected ID; selected removed => item now at same index (or last)
func TestListKeys(t *testing.T)            // j/down, k/up, home, "g","g", G/end, ctrl+d/ctrl+u (half page) move selection; HandleKey returns handled=true; "enter" => handled=false
func TestListViewExactSize(t *testing.T)   // variable-height items (1–3 lines); View(ctx,{W:20,H:7}) == 7 lines x 20 cells for every scroll position
func TestListAnchorSurvivesWidthChange(t *testing.T) // scroll so item "m7" is top at width 40; Anchor(); View at width 12 then Restore(a) => first visible item is m7
func TestListFollowBottom(t *testing.T)   // Follow(true), at bottom, SetItems(+3) => AtBottom()==true and last item visible; scrolled up => stays put
func TestListMouse(t *testing.T)          // wheel down => scrolls 3 lines; click on line of item m4 => Selected()==m4
// composer
var composerInputs = []string{"❄️ 첫눈", "👍🏽", "👨‍👩‍👧 가족", "🇰", "क्षि", "가", "탭\t그리고\r\n줄", "\x1b[31m빨강\x1b[0m"}
func TestComposerNormalizesTypeAndPaste(t *testing.T) {
	for _, in := range composerInputs { for _, mode := range []string{"type", "paste"} {
		c := widgets.NewComposer("메시지"); c.Focus()
		// type: one tea.KeyPressMsg per grapheme cluster (Text=grapheme); paste: tea.PasteMsg{Content: in}
		// assert c.Value() == textwidth.SanitizeInput(in)
		// every View line: StringWidth == StringWidthWc; cursor X (minus border 1) == ansi.StringWidth(text before cursor on its line)
	}}
}
func TestComposerRenormalizesAcrossKeys(t *testing.T) // keys "👨", "‍", "👩" separately => Value() == SanitizeInput("👨‍👩") == "👨"
func TestComposerHugePaste(t *testing.T) {          // Review Focus 4
	in := strings.Repeat("가나다 abc\r\n\x07\xff", 1000) // ~10k runes with CRLF, BEL, invalid byte
	start := time.Now(); c.Update(ctx, tea.PasteMsg{Content: in})
	// Value(): utf8.ValidString, no '\r', no '\x07', no U+FFFD; utf8.RuneCountInString <= 4000 (CharLimit); took < 1s; View lines widths exact
}
func TestComposerHardGlobalsNotConsumed(t *testing.T) // ctrl+k, ctrl+f, ctrl+n leave Value unchanged and cursor unchanged; shift+enter/alt+enter/ctrl+j insert "\n"; enter inserts nothing
func TestComposerCtrlCClears(t *testing.T) // CA 5.5 app.quit: value "abc" (cursor mid) + ctrl+c => Value()=="" (Reset), cmd nil; value "" + ctrl+c => no change
                                           // (embedding components report handled = Value()!="" taken BEFORE Update: true for the 1st press, false for the 2nd)
func TestComposerAutoGrowAndCompletion(t *testing.T)  // Height(40,5): empty => 3 (1 row + border); 7 lines => 7 (5 rows + border); SetCompletion(3 items) => Height +3 and View shows them above box
func TestComposerGolden(t *testing.T) {
	for _, sz := range uitest.Required { for _, st := range []string{"empty", "one_line", "multi_line", "completion"} {
		// Frame full screen; composer pinned to bottom of inner area with width inner.W and maxRows 5; AssertIntegrity; golden "WxH/state"
	}}
}
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/widgets/ -run 'TestList|TestComposer'` · Expected: FAIL

- [ ] **Step 3: 구현**
  - `List`: 렌더 캐시 = (item ID, width) → lines. 스크롤 상태는 `ScrollAnchor` 로만 보관(폭이 바뀌면 다시 렌더 후 앵커 항목의 `LineOffset` 을 새 줄 수로 clamp). `gg` 는 내부 `pendingG` (다른 키가 오면 해제, 시간 제한 없음 — 타이머 금지 규칙). 페이지 이동 = 높이의 절반.
  - `Composer`: CA 5.7 주석 그대로(`SetVirtualCursor(false)`, `ShowLineNumbers=false`, `Prompt=""`, `CharLimit=4000`, KeyMap 에서 `DeleteAfterCursor`/`CharacterForward`/`LineNext` 의 `ctrl+k`/`ctrl+f`/`ctrl+n` 제거, `InsertNewline` = `shift+enter, alt+enter, ctrl+j`). 입력 정규화: `KeyPressMsg.Text` 는 `SanitizeInput` 후 `textarea.InsertString`(존재·시그니처를 `go doc charm.land/bubbles/v2/textarea Model.InsertString` 로 확인), `PasteMsg.Content` 도 같음, 그리고 **매 삽입 뒤 전체 값이 `SanitizeInput(value) != value` 면 다시 넣고 커서를 같은 논리 위치(앞 글의 정규화 길이)로 옮긴다**(키 단위로 들어온 결합 시퀀스 대비 — 이 계획의 결정). 테두리 `Box(th, InputFocused|Input, …)`, 자동완성 팝업은 박스 위 최대 5줄, 선택 행 `Selected`, `↑/↓` 이동·`tab`/`enter` 삽입은 팝업이 떠 있을 때만 Composer 가 처리.
  - **`ctrl+c` 규칙(CA 5.5 "입력창에 글이 있으면 첫 번째는 입력 지우기" 의 core 구현 — 이 계획의 결정)**: `Composer.Update` 가 `ctrl+c` 를 받으면 값이 비어 있지 않을 때 `Reset()`(자동완성 팝업도 닫음) 후 nil Cmd, 비어 있으면 아무것도 하지 않는다. `Composer.Update` 는 CA 시그니처상 handled 를 돌려주지 않으므로, Composer 를 품은 컴포넌트의 `HandleKey` 는 `ctrl+c` 에 대해 **Update 전 값 기준** `handled = Value() != ""` 를 돌려준다 — 그래서 빈 입력에서 `ctrl+c` 는 미처리로 앱에 돌아가 종료 무장이 된다(컴포넌트가 `ctrl+c` 까지 handled 로 삼키면 입력 중 종료가 불가능해지므로 금지). 공개 API 는 늘리지 않는다(규칙만 정함). 같은 규칙을 core 의 모든 창(pane) 안 한 줄 입력(`newTextInput` — Task 28, authui — Task 33, settings — Task 34)에도 적용한다: 값이 있으면 지우고 handled, 없으면 미처리. 오버레이 안 입력(팔레트·신고 메모 등)은 CA 5.3.4 2.1 대로 `ctrl+c` 를 받지 않는다(앱이 바로 종료 무장). chat·sns-fun 의 입력 컴포넌트도 이 규칙을 따라야 한다(오케스트레이터 전달 사항).

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/widgets/ -update -run TestComposerGolden && go test ./internal/ui/widgets/ -v -count=1` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/ui/widgets && git commit -m "feat(core): id-anchored list and IME-safe normalizing composer"`

---

### Task 23: `uitest/fixtures` · `uitest/stub`

**Files:**
- Modify: `internal/ui/uitest/fixtures/{ids.go,seed.go,messages.go,timeline.go,hostile.go,api.go,image.go}`, `internal/ui/uitest/stub/stub.go`
- Test: `internal/ui/uitest/fixtures/fixtures_test.go`, `internal/ui/uitest/stub/stub_test.go`

**Interfaces:**
- Consumes: store, api, apitest, widgets `List`/`Composer`, ui 레지스트리
- Produces(CA 10.4): `Seed`, `Messages`, `Timeline`, `Notifications`, `Hostile`, `HostileMessages`, `HostileTimeline`, `RegisterAPI`, `TestImage`; (CA 10.2) `stub.Chat`, `stub.List`, `stub.Install`. 이 계획의 결정 — fixtures ID 상수(26자 Crockford, core 시험 편의용; 다른 계획은 Store 조회를 쓴다):

```go
const (
	UserSam = "01JA0000000000000000000SAM"; UserMinji = "01JA00000000000000000000M1"; UserJunho = "01JA00000000000000000000J1"
	UserHana = "01JA00000000000000000000H1"; UserDevKim = "01JA00000000000000000000D1"
	SpaceNova = "01JB00000000000000000000S1"
	ChGeneral = "01JB00000000000000000000C1"; ChChat = "01JB00000000000000000000C2"; ChDev = "01JB00000000000000000000C3"; ChDeploy = "01JB00000000000000000000C4"
	DMMinji = "01JB0000000000000000000DM1"; DMJunho = "01JB0000000000000000000DM2"
	SpaceHostile = "01JB00000000000000000000X1"; ChHostile = "01JB00000000000000000000X2"
)
```

- [ ] **Step 1: 실패 시험 작성**

```go
func TestSeed(t *testing.T) {
	s := store.New(); fixtures.Seed(s)
	// s.Me.Handle=="sam"; Spaces[SpaceNova].Name=="노바 개발방"; SpaceChannels(SpaceNova) labels ["#일반","#잡담","#개발","#배포알림"];
	// Channels[ChGeneral].My.UnreadCount==3; DMOrder==[DMMinji,DMJunho]; Presence[UserMinji].Status online, junho offline; ChannelLabel(DMMinji)=="민지"
	// every ID in Store passes ids.Valid
}
func TestMessagesDev(t *testing.T) {
	// Messages(ChDev): 민지 02:02Z "오늘 배포 몇 시에 해요?" reactions 👍2(me) 👀1 thread.reply_count 3; 준호 02:04Z body has "```\n$ wrangler deploy --env prod\n```";
	// sam 02:05Z "@minji 점심 먹고 ㄱㄱ" mentions [minji]; 민지 kind poll "점심 메뉴" 국밥 4 돈까스 2; ascending IDs
}
func TestTimelineAndNotifications(t *testing.T) // hana "오늘 첫 눈 ❄️ 사무실 창밖" media 1 like 12 repost 3 reply 4; dev_kim "bubbletea v2 써보신 분? #golang" like 5 reply 2; Notifications() len 2
func TestHostileSet(t *testing.T) // Hostile adds SpaceHostile "☠ 적대적 데이터"; a 32-char handle, 32-rune display name (16 Hangul), 50-rune channel & space names,
                                  // HostileMessages has a 4000-rune body, control/ANSI/OSC8 bodies, VS16/skin/ZWJ/🇰/🇰🇷/1️⃣/क्षि/jamo/lone combining, 300-char URL,
                                  // 20 reactions, thread reply_count 9999, channel unread 9999, @here + 32-char mention, long status with emoji
func TestRegisterAPIServesBootstrap(t *testing.T) // store.Bootstrap against fixtures-registered apitest => no Err, Spaces include nova and hostile, unreads match Seed
func TestRegisterAPIPatterns(t *testing.T) {
	// each of the 17 patterns listed in Step 3 answers 200 (sample concrete URL per pattern); an unlisted route (e.g. GET /v1/presence) => 404 not_found;
	// re-registering one of them afterwards (s.JSON("GET /v1/friends/requests", 200, …)) does not panic and the new handler wins (Task 8 rule)
}
func TestTestImage(t *testing.T)  // 64x48 bounds
// stub (no app needed)
func TestStubChat(t *testing.T) {
	// Install(); factory for RouteChannel{ID:ChDev} => *stub.Chat with unique Serial; HandleKey "j"/"k" move List selection;
	// "i" => CapturesInput true, Cursor non-nil; Type text into Composer; "enter" while capturing => pending line appended, Composer reset;
	// "esc" capturing => leaves input (handled); "esc" not capturing => handled=false; "t" on selected => cmd yields NavigateMsg{RouteThread{ID:sel, Arg:ChDev}}
	// "ctrl+c" capturing with draft "abc" => draft cleared, handled=true, still capturing; "ctrl+c" capturing with empty draft => handled=false;
	// "ctrl+c" not capturing => handled=false (Task 22 ctrl+c rule)
	// ViewingChannels()==[ChDev] for RouteChannel, nil for RouteThread (like chat's threadView); SaveState/LoadState round-trip keeps draft + anchor + selection
	// test-only hooks: Completion=true while capturing => Completing()==true, "tab"/"shift+tab" => handled, appended to Completed, Completion=false;
	// Completion=true but not capturing => Completing()==false; OnUpdate set => called with (ctx, msg) before every Update
}
func TestStubList(t *testing.T) // RouteFeed/RouteNotifications/RouteHome => *stub.List; rows "row 1".."row 30"; j/k; Titled title "피드"/"알림"/"홈"
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/uitest/fixtures/ ./internal/ui/uitest/stub/` · Expected: FAIL

- [ ] **Step 3: 구현** — 데이터는 CA 10.4 문장과 시안(`layouts.html`)대로. 시각은 2026-11-05 KST 기준 UTC 로. 사용자: sam(sam), minji(민지), junho(준호), hana(하나), dev_kim(김개발). `RegisterAPI` 가 등록하는 패턴은 **정확히 아래 17개**다(다른 계획이 이 목록 위에 덮어 등록한다 — Task 8 의 "같은 모양이면 나중 등록이 이김" 규칙, 그리고 ID 를 박은 구체 경로는 와일드카드보다 구체적이라 이김. 목록을 바꾸면 이 문단과 `TestRegisterAPIPatterns` 를 같이 고친다):
    1. `GET /v1/me` 2. `GET /v1/spaces` 3. `GET /v1/spaces/{space_id}` 4. `GET /v1/dms`(커서 무시, 전체 한 페이지 + `next_cursor: null`) 5. `GET /v1/me/unreads` 6. `GET /v1/notifications/unread-count` 7. `GET /v1/friends`(전체 한 페이지 + `next_cursor: null`) 8. `GET /v1/friends/requests`(`direction` 별) 9. `GET /v1/me/direct-invites` 10. `GET /v1/blocks` — 여기까지 store 부트스트랩 10종
    11. `GET /v1/channels/{channel_id}/messages`(around/before/after 무시하고 전체) 12. `GET /v1/messages/{message_id}/thread`(민지 루트 + 답글 3개, 그 밖 ID 는 빈 답글) 13. `GET /v1/timeline/following` 14. `GET /v1/timeline/discover` 15. `GET /v1/notifications` 16. `GET /v1/users/search`(`q` 의 handle·이름 부분 일치, `limit` 존중) 17. `GET /v1/meta`
    `/` catch-all·쓰기 메서드·`/v1/presence`·멤버 목록 등은 등록하지 않는다(각 계획 시험이 직접 등록). 와일드카드 이름은 api.md 경로 표기(`{space_id}` 등)를 따르지만, Task 8 규칙상 이름이 달라도 같은 모양이면 같은 패턴으로 취급된다. `stub.Chat` 의 렌더는 밀도별 최소형(Full: `이름 시간` 줄 + 본문, Line: `11:02 민지 본문`, Bubble: 내 메시지 오른쪽 정렬, Minimal: 한 줄 축약) — 본문은 `textwidth.Sanitize` 후 `Truncate`. `Serial` 은 패키지 전역 `atomic.Uint64`. `stub.Chat.HandleKey` 의 키 규칙(위 시험이 고정): 입력 캡처 중이면 `enter` 전송·`esc` 빠져나오기·`ctrl+c` = Task 22 의 `ctrl+c` 규칙(Update 전 초안이 비어 있지 않으면 `Composer.Update` 로 지우고 handled, 비어 있으면 handled=false), 그 밖 키는 Composer 로 넘기고 handled; 캡처 중이 아니면 `j/k`·`i`·`t` 만 handled, 나머지(`ctrl+c` 포함)는 handled=false. **시험 전용 훅(CA 10.2 `stub.Chat` 에 필드·메서드를 더하는 것 — 시그니처 변경 아님)**: 필드 `Completion bool`(시험이 켜는 "자동완성 팝업 떠 있음"), `Completed []string`, `OnUpdate func(ctx *ui.Context, msg tea.Msg)`(nil 이면 무시; `Update` 맨 앞에서 호출 — Task 30 이 방송 순간의 `Store` 상태를 관찰), 메서드 `Completing() bool` = 캡처 중 && `Completion`(→ `ui.Completer`, CR-A). 캡처 중 && `Completion` 이면 `tab`/`shift+tab` 을 Composer 보다 먼저 받아 `Completed` 에 키 문자열을 붙이고 `Completion=false`, handled(plan 04 의 `tab` 수락과 같은 모양). `ViewingChannels` 는 `RouteChannel` 이면 `[ID]`, `RouteThread` 면 nil(chat `threadView` 와 같음 — 스레드만 보일 때 채널은 "보는 중" 이 아님; Task 30 `TestSetViewingBeforeBroadcast` 가 기댐). `stub.List` 에도 같은 `OnUpdate` 필드. `Install` 은 `sync.Once` 대신 **`ui.Registered().Routes[RouteChannel]` 이 없을 때만 등록**하도록 해 `ui.ResetRegistryForTest()` 뒤에도 다시 설치할 수 있게 한다(같은 프로세스에서 두 번 불러도 패닉 없음 — CA 10.2 의 의도 유지). `Titled`: Chat = `Store.ChannelLabel(ID)`(스레드면 `"스레드"`), 배지 = 채널 안 읽음.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/uitest/fixtures/ ./internal/ui/uitest/stub/ -v` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/ui/uitest && git commit -m "test(core): mockup fixtures, hostile set, fake api routes, stub screens"`

---

### Task 24: `app` 내비게이션 — `NavState` · `Project` · 이동/뒤로

**Files:**
- Modify: `internal/ui/app/{nav.go,project.go}`
- Test: `internal/ui/app/nav_test.go`(package `app`, 순수 함수 — 비공개 `navigate/back/…` 를 직접 부르므로 내부 시험. **`uitest` 를 import 하지 않는다**(순환): 아래 시험의 "ModeSizes" 는 이 파일 안의 `var modeFirstSize = map[layout.Mode][2]int{layout.ModeCommand: {200, 60}, layout.ModeStandard: {120, 40}, layout.ModeCompact: {120, 30}, layout.ModeWidget: {80, 24}, layout.ModeTiny: {50, 20}, layout.ModeTooSmall: {19, 10}}`(CA 10.2 `ModeSizes` 각 첫 값과 같게)를 뜻한다)

**Interfaces:**
- Consumes: ui `Route`/`NavigateMsg`/`Target`, layout
- Produces(CA 5.3.3): `Front` 상수, `TabState`, `NavState`, `(NavState).LayoutState(mode) layout.State`, `(NavState).Keys() []string`, `Project(l layout.Layout, n NavState) map[layout.SlotID]ui.Route`, `NotifRoute`. 비공개 결정(이후 Task 가 씀): `newNavState() NavState`(Tabs=[{Stack:[{home}]}], Feed=[{feed,Arg:"following"}], Front=FrontTab), `(n NavState) navigate(m ui.NavigateMsg, mode layout.Mode) (NavState, FocusIntent)`, `(n NavState) back(focus layout.SlotID, mode layout.Mode, l layout.Layout) NavState`, `(n NavState) toggleFeed(mode) (NavState, *FocusIntent)`, `(n NavState) toggleNotifications(mode) (NavState, *FocusIntent)`, `(n NavState) cycleTab(delta int, mode layout.Mode) NavState`(컴팩트: Tabs 루트들 + 피드 + 알림 순; 위젯·초소형: 채팅/피드/알림 3칸), `TabState.seen uint64`(비공개, LRU).

- [ ] **Step 1: 실패 시험 작성**

```go
func TestNavigateTable(t *testing.T) {
	// every row of CA 5.3.3 NavigateMsg table, e.g.:
	// channel C3 new => Tabs append {Stack:[C3]}, Active=1, Front=FrontTab, focus {main, "channel/C3/"}
	// channel C3 again after pushing search on it => same tab active, stack kept
	// thread {ID:R,Arg:C3} => tab C3 active, stack folded to root, Thread=&R, focus thread key
	// feed {Arg:"discover"} => Feed==[discover], Front=FrontFeed; post P => Feed=[following,P]; notifications => FrontNotifications
	// settings => pushed onto active tab stack; TargetSide post => Feed stack; TargetMain post => active tab stack
	// Replace=true on push kinds replaces top (not root); 10th tab => evicts least-recently-seen inactive tab; login => newNavState()+login only
	// pushing key K already deeper in another stack removes it there (invariant); pushing K already in destination stack pops back to it
}
func TestProjectTable(t *testing.T) { // every cell of CA 5.3.3 Project table for each mode at modeFirstSize[mode] (= CA 10.2 ModeSizes[mode][0])
	// command: main=top, thread=thr when open, feed=F, notifications=NotifRoute; standard: Front rules, thread pane when room;
	// compact/widget/tiny: FrontFeed=>F, FrontNotifications=>NotifRoute, FrontTab=>attached?thr:top; TooSmall => empty map
}
func TestLayoutState(t *testing.T) // ThreadOpen = thr!=nil && (mode==Command || Front==FrontTab); FeedSwapped = mode==Standard && Front==FrontFeed
func TestBackRules(t *testing.T)   // each bullet of CA 5.3.3 "뒤로" incl. root never popped, notifications pane no-op
func TestToggleFeedAndNotifications(t *testing.T) // command => focus intent feed / notifications slot, NavState unchanged; others toggle Front
func TestCycleTab(t *testing.T)    // compact with 3 tabs: order tab0,tab1,tab2,피드,알림 then wraps; widget: 채팅→피드→알림→채팅
func TestInvariantRandomWalk(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	// 1000 random steps (navigate random kind/ID from a pool of 12 channels/posts/profiles incl. Replace/Target variants, back with random focus slot,
	// toggles, cycleTab) — after each step, for every mode at modeFirstSize[mode]: Project values have unique Key(); Keys() unique;
	// Feed non-empty && Feed[0].Kind==RouteFeed; every Tabs[i].Stack[0].Kind in {home, channel}; no thread/feed/notifications in stacks;
	// 0<=Active<len(Tabs)<=9
}
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/app/ -run 'TestNavigate|TestProject|TestLayoutState|TestBack|TestToggle|TestCycleTab|TestInvariant'` · Expected: FAIL

- [ ] **Step 3: 구현** — CA 5.3.3 표·규칙만으로 순수 함수. `Project` 는 `layout.Layout.Pane(slot)` 존재 여부로 창 유무를 판단(표준 스레드 창이 없으면 `attached` 스레드를 Main 에).

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/app/ -run 'TestNavigate|TestProject|TestLayoutState|TestBack|TestToggle|TestCycleTab|TestInvariant' -v` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/ui/app && git commit -m "feat(core): mode-independent navigation state and slot projection"`

---

### Task 25: `app` 셸 골격 + 하네스 구동

**Files:**
- Modify: `internal/ui/app/{model.go,cache.go,render.go,status.go,tabbar.go,noop.go}`, `internal/ui/uitest/{harness.go,options.go}`
- Create: `internal/ui/app/export_test.go`(package `app`), `internal/ui/app/main_test.go`(package `app_test`)
- Test: `internal/ui/uitest/harness_test.go`(stub 미등록 — placeholder 골든), `internal/ui/app/shell_test.go`(stub 등록, package `app_test`), `internal/ui/app/context_test.go`(package `app_test`). 이 Task 부터 `internal/ui/app` 의 하네스·`uitest` 를 쓰는 시험 파일은 모두 `package app_test` 다(Global Constraints "시험 패키지 규칙").

**Interfaces:**
- Consumes: Task 18–24 전부, `config`, `auth`, `store`, `notify`
- Produces(CA 5.4): `Options`, `Model`, `New`, `Init`, `Update`, `View`, `Context`, `Nav`, `Instance`, `Focus`, `Frame`, `FocusIntent`, `FrameInfo`, `Box`; (CA 10.2) `uitest.Option`, `WithTheme`, `WithSeed`, `WithAPI`, `WithRoute`, `WithPin`, `WithUnicodeCore`, `WithCredentials`, `Harness`, `New`, `Resize`, `Press`, `Type`, `Paste`, `Click`, `Wheel`, `Send`, `Advance`, `Clock`, `TeaMsgs`, `Event`, `Screen`, `Styled`, `Golden`, `Check`, `Context`, `Cursor`, `Nav`, `Instance`, `ForEachSize`. 비공개/시험 전용 결정: `export_test.go` 의 `func SetRealtimeForTest(m *Model, rt ui.Realtime)`; `noopRealtime{state realtime.State}`(DisableRealtime 일 때, `State()` = `{Phase: PhaseLive}`); 상태줄 렌더 `renderStatus(ctx *ui.Context, w int, in statusInput) string`.

- [ ] **Step 1: 실패 시험 작성**

```go
// internal/ui/uitest/harness_test.go — no stub registered: unregistered routes draw widgets.Placeholder("준비 중")
func TestPlaceholderShellGolden(t *testing.T) {
	uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) { h.Golden("shell") })
}
func TestSendRunsCmdsBreadthFirst(t *testing.T) // test-only overlay/component (registered via ui.RegisterOverlay("core.testprobe")) returning tea.Batch(a,b) where a returns msg that returns c => Update order a,b,c; depth>32 => Fatal (recTB)
func TestTeaInternalMsgsRecorded(t *testing.T)  // a cmd returning tea.Quit() => not re-dispatched, TeaMsgs() contains tea.QuitMsg
func TestSequenceForbidden(t *testing.T)        // a cmd from tea.Sequence(x,y) => recTB Errorf "tea.Sequence is forbidden in UI code (5.3.2)"
func TestBlockingCmdFails(t *testing.T)         // cmd sleeping 1.5s => recTB Fatalf containing "Cmd blocked > 1s"
func TestAdvanceDeliversInOrder(t *testing.T)   // ui.After 3s "a", 1s "b", and on "b" arms 1s "c" => Advance(3s) delivers b, c, a; Clock().Now()==start+3s
func TestHarnessDefaults(t *testing.T)          // MASIL_CONFIG_DIR is t.TempDir(); Context().Now()==2026-11-05T02:05:00Z; Loc Asia/Seoul; Theme amber; Caps.Graphics HalfBlock; Config.UI.Animations false
// internal/ui/app/main_test.go (package app_test — the only TestMain of the app test binary): func TestMain(m *testing.M) { stub.Install(); os.Exit(m.Run()) }
func TestShellGoldens(t *testing.T) {
	onlyMe := func(s *store.Store) { full := store.New(); fixtures.Seed(full); s.Me = full.Me }
	for _, tc := range []struct{ name string; opts []uitest.Option }{
		{"empty_home", []uitest.Option{uitest.WithSeed(onlyMe)}},
		{"fixtures_home", nil},                                       // default seed = fixtures.Seed
		{"dev_channel", []uitest.Option{uitest.WithRoute(ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev})}},
	} { forEachSize(t, uitest.AllSizes(), tc.opts, func(t *testing.T, h *uitest.Harness) { h.Golden(tc.name) }) }
}
// test helper (app_test): like uitest.ForEachSize but with options — subtest "WxH", fresh uitest.New(t, opts...) then h.Resize(W, H)
func forEachSize(t *testing.T, sizes []uitest.Size, opts []uitest.Option, fn func(*testing.T, *uitest.Harness))
func TestThemesStyledGolden(t *testing.T) // for each theme.All(): New(WithTheme(n)), Resize(120,40), golden.RequireEqual(t, h.Styled()) under t.Run(string(n))
func TestTooSmallMessage(t *testing.T)    // 19x10 => middle row contains "창을 조금만 키워 주세요" truncated with "…" (Width 19); no status line; 30x5 likewise
func TestPaneSizesAndResize(t *testing.T) // 200x60 dev channel: Instance("channel/<ChDev>/") got Resize {76,57} (main inner); after Resize(120,40) => {92,37}; same Serial
func TestWindowTitleAndViewFlags(t *testing.T) // View(): AltScreen true, MouseMode CellMotion (Config.UI.Mouse=false => None), ReportFocus true, WindowTitle "masil · #개발", BackgroundColor == amber BG when paint_background
// context_test.go — app.New directly (no harness option exists for raw config); runInit(m) = test helper that runs m.Init()'s Cmd tree
// synchronously (expanding tea.BatchMsg) and returns the produced msgs.
func TestContextInitFromConfig(t *testing.T) {
	// Config{Theme:"b", Keys:{"palette.open":["ctrl+p"], "nope":["x"]}, UI.Images:"off"} => Context().Theme.Name==midnight;
	// Context().Keys.Matches(ctrl+p, PaletteOpen) && !Matches(ctrl+k, PaletteOpen); Context().Caps.Graphics==termcaps.None;
	// runInit(m) contains ui.ToastMsg{Text:"키 설정 일부를 무시했어요", Kind:ui.ToastError}; Config{Theme:"neon"} (bypassing config.Load) => amber, no panic;
	// Config.Keys empty => no toast, Keys == keymap.Default() bindings
}
func TestUnicodeCoreModeReport(t *testing.T) // uitest.New(t, uitest.WithUnicodeCore(true)) => h.Context().Caps.UnicodeCore == true; default harness => false;
                                             // h.Send(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeReset}) => false again
```

`uitest.ForEachSize` 는 CA 10.2 시그니처 그대로(기본 옵션, 크기마다 새 하네스 + `Resize`). 옵션이 필요한 시험은 위 `forEachSize` 시험 헬퍼를 쓴다(공개 API 를 늘리지 않음).

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/uitest/ ./internal/ui/app/ -run 'TestPlaceholder|TestSend|TestTeaInternal|TestSequence|TestBlocking|TestAdvance|TestHarnessDefaults|TestShell|TestThemes|TestTooSmall|TestPaneSizes|TestWindowTitle|TestContextInit|TestUnicodeCore'` · Expected: FAIL

- [ ] **Step 3: 구현**
  - **`ui.Context` 초기화(`app.New`, 이 순서로 — 하네스와 `Run` 이 같은 경로를 탄다)**: ① `Config` = `o.Config`(nil 이면 `config.Defaults()`). ② `Theme` ← `theme.Parse(cfg.Theme)`(a~d 별칭·대소문자 무시; 실패하면 `theme.Default` — `config.Load` 가 이미 걸렀어도 방어). ③ `Keys` ← `keymap.Load(cfg.Keys)`(CA 3.3 `[keys]` 재정의); 돌려받은 `[]error` 가 비어 있지 않으면 오류마다 `slog.Warn("keymap override ignored", "err", e)` 그리고 `Init` 이 돌려주는 Cmd 에 `ui.Toast("키 설정 일부를 무시했어요", ui.ToastError)` 를 더한다(New 에서는 Cmd 를 낼 수 없으므로 Model 에 보관했다가 Init 에서). Load 는 오류가 있어도 기본값을 채운 Keymap 을 돌려주므로 그것을 그대로 쓴다. ④ `Caps` ← `termcaps.Detect(os.Environ(), cfg.UI.Images)`. ⑤ `Now` = `o.Now`(nil 이면 `time.Now`), `Loc` = `o.Loc`(nil 이면 `time.Local`), `Timers` = `o.Timers`(nil 이면 `ui.RealTimers()`). ⑥ `API` = `o.API`, nil 이면 `api.New(cfg.ServerURL(), api.WithToken(<cred.Token, 자격 증명 없으면 생략>), api.WithClock(<⑤의 Now>))`(Task 30 이 같은 호출에 `WithOnUnauthorized`/`WithOnUpgradeRequired` 를 더한다). `Store`·`Realtime` 은 Task 30·32 결정. 실행 중 `tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore}` 를 받으면 `Caps.UnicodeCore = Value.IsSet()`(그 밖 모드 보고는 무시). 설정 화면이 `images` 를 바꾸면 Task 34 대로 `Caps` 를 다시 `Detect` 하되 `UnicodeCore`·`FocusReports` 는 유지한다.
  - **Model**: CA 5.3.4 1단계(WindowSize → `layout.Resolve` → `Compute(…, nav.LayoutState(mode))` → `Project` → 바뀐 내부 크기만 `Resize`, 모드 바뀌면 `ModeChangedMsg` 방송). 인스턴스 캐시(`cache.go`): `Route.Key()` → `{comp, lastSeen, saved any}`, 최근 32개, `NavState.Keys()` 는 고정, 해제 시 `Persistent.SaveState` 보관·재생성 시 `LoadState`; 새 인스턴스는 `Init` 의 Cmd 를 실행 대상에 더한다. 슬롯 팩토리(`rail|sidebar|members`) 인스턴스는 한 번 만들어 영구 보관. 미등록 슬롯·경로 → `widgets.Placeholder("준비 중")`.
  - **렌더**(`render.go`): 테두리 있는 창 = `widgets.Frame(theme, title, badge, focused, rect size, comp.View(ctx, focused))`; 제목 = `Titled` 또는 경로 기본 제목(home `홈`, channel `Store.ChannelLabel`, thread `스레드`, feed `피드`, notifications `알림`, settings `설정`, help `도움말`, 그 밖 kind 문자열). 창들을 `lipgloss.NewLayer(s).X().Y().Z(0)` 으로 합성(`NewCompositor(...).Render()`), 마지막에 `textwidth.Fit(lines, W, H)`. TooSmall 은 가운데 줄 `Center(Truncate("창을 조금만 키워 주세요", W, "…"), W)`, 다른 줄은 `Base` 공백.
  - **상태줄**(`status.go`, 1줄, `StatusBar` 배경): 왼쪽 `Pill(" "+mode.Label()+" ")` + 힌트(CA 5.4 모드별 기본 문구 그대로 — 초소형은 힌트 없이 `[초소형]` pill 과 연결 점), 포커스가 `CapturesInput` 이면 `enter 보내기 · shift+enter 줄바꿈 · esc 나가기`, `Helper` 바인딩 추가 규칙(CA 5.4). 오른쪽 항목은 Task 26 이 채우고 이 Task 에선 연결 상태만: `PhaseLive` → `Store.Self.Status` 에 따라 `● 온라인`/`◐ 자리비움`/`⊘ 방해금지`(Self 가 비면 온라인), `Connecting|Syncing` → `◌ 연결 중…`, 그 밖 → `✕ 오프라인`. 넘치면 힌트를 `…` 로 자른다.
  - **탭 줄·맥락 줄**(`tabbar.go`): CA 5.4 "탭" 절. 컴팩트 라벨 = 각 탭 루트의 `Store.ChannelLabel`(home 은 `홈`) + 채널 `My` 배지, 고정 `피드`·`알림`(배지 `Store.NotifUnread`). 위젯 `채팅`(배지 `TotalUnread`, 멘션 있으면 Mention) / `피드` / `알림`. 초소형 `챗`/`피`/`알`, 현재 탭만 `Pill`. 맥락 줄 = 활성 탭 루트 제목 + (채널이면 ` · ` + 방 이름) + ` ▾`.
  - **View 플래그**: CA 5.4 "View() 가 매 프레임 설정하는 것" 그대로(`Cursor` 는 Task 27·28 이 채움).
  - **하네스**: `New` 는 `t.TempDir()` 로 `MASIL_CONFIG_DIR`/`MASIL_CACHE_DIR`, `MASIL_SERVER` = apitest URL 을 `t.Setenv`; 기본 apitest 서버에 `fixtures.RegisterAPI`; 기본 자격 증명 `{Version:1, Server: srv.URL, Token:"msl_test", UserID: fixtures.UserSam, Handle:"sam"}`; `app.Options{Paths, Config(테마·Animations=false·Images="halfblock"), Credentials, InitialRoute, Now: clock.Now, Loc: Asia/Seoul, DisableRealtime: true, Notifier: notify.New(rec, OpenLedger(paths), discard), Timers: clock}`; 기본 시드 `fixtures.Seed`(`WithSeed` 가 대체), 시드는 `New` 직후 Store 에 적용하고 기본 크기 120×40 `WindowSizeMsg` 를 보낸다. `Send` 규칙·`Advance`·`Event` 는 CA 10.2 그대로(1초 감시는 별도 고루틴 + `time.NewTimer` — `uitest/` 는 타이머 규칙 예외). `Type` 은 **그래핌 단위**로 `KeyPressMsg` 를 보낸다(Task 22 결정과 일치). `WithUnicodeCore(true)` 는 `tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet}` 를 보낸다(`go doc github.com/charmbracelet/x/ansi ModeUnicodeCore` 로 상수 이름 확인). DisableRealtime 이면 `Init` 은 부트스트랩도 하지 않는다(시드가 상태의 유일한 출처).

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/uitest/ ./internal/ui/app/ -update -run 'TestPlaceholderShellGolden|TestShellGoldens|TestThemesStyledGolden' && go test ./internal/ui/uitest/... ./internal/ui/app/ -count=1` · Expected: PASS, 골든 `24×1 + 24×3 + 4` 개

- [ ] **Step 5: 커밋** — `git add internal/ui && git commit -m "feat(core): app shell rendering, status/tab bars, harness driver and shell goldens"`

---

### Task 26: 상태줄 오른쪽 항목 · 토스트 · 오류 메시지

**Files:**
- Modify: `internal/ui/app/{status.go,toast.go,model.go}`
- Test: `internal/ui/app/status_test.go`, `internal/ui/app/toast_test.go`(둘 다 package `app_test`)

**Interfaces:**
- Consumes: `ui.ToastMsg`, `ui.ErrorMsg`, `api.Describe`, `api.Client.Cooldown`, `Store.Focus`, `widgets.Toasts`, `humanize`
- Produces: `FrameInfo.Toast` 채움. 비공개: `toastExpireMsg{id int}`, `toastState{items []toastItem}`.

- [ ] **Step 1: 실패 시험 작성**

```go
func TestStatusRightItems(t *testing.T) {
	// Store.Focus ends_at = now+18m20s => right contains "🎯 18:20"; Advance(1s) => "🎯 18:19" (status re-renders each second only while focusing, via ui.After 1s)
	// api cooldown RL_MESSAGE 12s (apitest 429 via h.Context().API, whose clock is the harness FakeClock) => "⏳ 12초"; Advance(2s) => "⏳ 10초";
	// Advance(10s) => no "⏳" item; pinned mode => "📌"; Phase Syncing => "◌ 동기화 중…"
	// width 50: only connection item kept on the right, hints truncated with "…"; every line exact width
}
func TestPinFallbackHint(t *testing.T) // WithPin(Command) at 120x40 => hint area shows "📌 지휘소 고정 · 창이 작아 표준으로 표시"
func TestToastShowsAndExpires(t *testing.T) {
	uitest.ForEachSize(t, uitest.Required, func(t *testing.T, h *uitest.Harness) {
		h.Send(ui.ToastMsg{Text: "복사했어요", Kind: ui.ToastSuccess})
		h.Golden("toast")                          // box top-right of body, bordered, FrameInfo.Toast set
		h.Advance(4 * time.Second)
		if h.Model.Frame().Toast != nil || strings.Contains(h.Screen(), "복사했어요") { t.Fatal("toast must expire after 4s") }
	})
}
func TestToastTinyUsesStatusLine(t *testing.T) // 50x20 (tiny) => Frame().Toast==nil and status line shows "복사했어요"
func TestToastStackMax3(t *testing.T)          // 5 toasts => 3 newest visible
func TestErrorMsgBecomesToast(t *testing.T)    // ErrorMsg{Err: &api.Error{Code:"not_found"}, Context:"채널"} => toast "채널: 찾을 수 없어요" (ToastError)
func TestToastAlertClickNavigates(t *testing.T) // ToastMsg{Kind:ToastAlert, Route:&{channel C3}} then Click inside toast box => NavigateMsg applied (Nav active tab root C3)
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/app/ -run 'TestStatusRight|TestPinFallback|TestToast|TestErrorMsg'` · Expected: FAIL

- [ ] **Step 3: 구현** — 결정: 오른쪽 항목 순서 `🎯 mm:ss · ⏳ N초 · 📌 · <연결>`(구분 ` · `, 각 항목 `Sanitize`), 집중 중일 때만 1초 `ui.After` 재무장(세대 번호). 쿨다운은 `RL_MESSAGE`/`RL_POST`/`RL_REACT`/`RL_SEARCH`/`RL_SOCIAL`/`RL_UPLOAD`/`RL_API` 중 남은 시간이 가장 긴 것. **남은 시간 = `ctx.API.Cooldown(limiter)` − `ctx.Now()`**(0 이하이면 항목 없음, 표시는 초 올림). 이 비교가 결정적이려면 쿨다운 기록 시각도 같은 시계여야 하므로 앱이 만드는 `api.Client` 에는 `api.WithClock(o.Now)` 가 붙는다(Task 25 Context 초기화 ⑥, Task 30 이 훅을 더함). 하네스에서는 둘 다 FakeClock(2026-11-05T02:05Z)이라 `⏳ 12초` 가 정확히 나오고, 쿨다운 중에는 상태줄을 1초 `ui.After` 로 다시 그려 숫자가 줄어든다(집중 타이머와 같은 세대 번호 재무장 — 집중 또는 쿨다운 중 하나라도 있으면 무장). 토스트 상자: `w = min(44, body.W)`, 줄 = `widgets.Toasts(th, items(최신 3), w-2)`, `rect = {body.X+body.W-w, body.Y, w, len+2}`, 테두리 테마 `Border` + `OverlayBorder`, `Z=100`; 초소형이거나 `body.W < 24` 면 상자 대신 상태줄 힌트 자리에 최신 토스트. 각 토스트 `ui.After(4s, toastExpireMsg{id})`. `ErrorMsg` → `Context + ": " + api.Describe(err)`(Context 비면 Describe 만).

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/app/ -update -run TestToastShowsAndExpires && go test ./internal/ui/app/ -count=1` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/ui/app && git commit -m "feat(core): status bar right items, toasts with timed expiry, error toasts"`

---

### Task 27: 포커스 · 키 라우팅 · 마우스 · 모드 고정 — 모드 전환 보존

**Files:**
- Modify: `internal/ui/app/{focus.go,keys.go,mouse.go,model.go}`
- Test: `internal/ui/app/focus_test.go`, `internal/ui/app/preserve_test.go`(둘 다 package `app_test`)

**Interfaces:**
- Consumes: Task 24 `navigate/back/toggle*/cycleTab`, keymap, `ui.InputCapturer`/`KeyHandler`/`MouseHandler`/`Cursorer`/`ChannelViewer`/`Completer`(CR-A), Task 23 `stub.Chat.Completion`/`Completed`
- Produces: `(*Model).Focus() FocusIntent` 동작, `ui.FocusChangedMsg` 방송, `View.Cursor`(오버레이 없을 때), 자동완성 중 `tab`/`shift+tab` 의 컴포넌트 전달(CR-A — plan 04 CR-1 을 받는 곳; 이것이 들어와야 plan 04 Task 10 `TestCompletionTabAccept` 를 추가할 수 있다). 비공개: `pinMsg{mode *layout.Mode}`(nil = 자동), `quitArmMsg{gen int}`, `idleMsg{gen int}`(Task 30 이 사용), `prefixState`.

- [ ] **Step 1: 실패 시험 작성**

```go
func TestFocusRingAndTab(t *testing.T)    // 200x60: tab cycles rail→sidebar→main→feed→members→notifications→rail (bordered panes, CA order); shift+tab reverse; 120x30 compact: tab => tab.next (Nav changes, focus stays main)
func TestCtrlWPrefix(t *testing.T)        // 200x60 focus main: ctrl+w l => feed; ctrl+w h => main; ctrl+w w => next; ctrl+w 2 => pinned standard (status "📌"); ctrl+w 0 => auto
func TestCtrlWWhileTypingIsComposer(t *testing.T) // stub composer capturing: ctrl+w goes to component (Nav/focus unchanged)
func TestCapturedInputBlocksSoftGlobals(t *testing.T) // "i" then Type("f?[]") => all in draft, no feed toggle/help/tab change
func TestTabGoesToCompletingComponent(t *testing.T) { // CR-A (CA 5.3.4 2.3)
	for _, sz := range []uitest.Size{{120, 40}, {80, 24}} { // multi-pane ring (focus.next) and single-pane ring (tab.next)
		// WithRoute(channel ChDev) + a second tab (#일반) opened; Resize(sz); "i"; ch := h.Instance("channel/"+ChDev+"/").(*stub.Chat); ch.Completion = true
		// nav0, focus0 := h.Nav(), h.Model.Focus(); Press("tab") => ch.Completed == ["tab"], Nav()/Focus() deep-equal nav0/focus0, draft unchanged
		// ch.Completion = true; Press("shift+tab") => Completed == ["tab","shift+tab"], Nav()/Focus() unchanged
		// popup closed (Completion false): Press("tab") => hard global again: 120x40 focus moves off main (focus.next); 80x24 Nav active tab/Front changes (tab.next); Completed len still 2
		// Completion = true but not capturing ("esc" first) => Completing()==false => "tab" is the hard global, Completed unchanged
	}
}
func TestHardGlobals(t *testing.T)        // ctrl+n compact toggles FrontNotifications; ctrl+n command focuses notifications slot; ctrl+f => NavigateMsg RouteSearch (pushed); ctrl+k => palette overlay open (Task 29 asserts content)
func TestCtrlCTwice(t *testing.T) {
	// stub composer capturing ("i"), draft "abc": ctrl+c => draft "" (stub handled via Task 22/23 ctrl+c rule), no toast, not armed, still capturing;
	// ctrl+c (draft now empty, stub returns handled=false) => toast "한 번 더 누르면 종료해요" (armed); Advance(500ms); ctrl+c => TeaMsgs has tea.QuitMsg
	// fresh harness, not capturing: ctrl+c arms immediately; Advance(1s) without 2nd press => disarmed (next ctrl+c arms again, no quit)
	// (overlay case — ctrl+c bypasses an open overlay — is asserted in Task 28 TestOverlayStackAndModal)
}
func TestMouseClickFocusAndWheel(t *testing.T) // click inside feed pane => focus feed + stub got HandleMouse with local coords (x-inner.X, y-inner.Y); wheel over sidebar while focus main => sidebar receives wheel, focus unchanged
func TestCursorFollowsFocusedComposer(t *testing.T) // "i" => h.Cursor() == composer cursor + main inner origin; Type("가나") => X advanced by 4
func TestFocusIntentFollowsRoute(t *testing.T) // compact thread in Main focused => resize 200x60 => focus thread slot; back to 120x30 => main
func TestModeSwitchPreservation(t *testing.T) { // CA 10.3 core version
	h := uitest.New(t, uitest.WithRoute(ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}))
	h.Resize(200, 60); h.Press("i"); h.Type("배포 몇 시"); h.Press("esc"); h.Press("ctrl+u"); /* scroll up 5 lines */ h.Press("k")
	ch := h.Instance("channel/" + fixtures.ChDev + "/").(*stub.Chat); serial, sel := ch.Serial, selectedID(ch)
	for _, sz := range []uitest.Size{{200, 60}, {120, 40}, {120, 30}, {80, 24}, {50, 20}, {200, 60}} {
		h.Resize(sz.W, sz.H); h.Check()
		// (a) ch.Composer.Value()=="배포 몇 시" (b) selectedID(ch)==sel (c) anchor message visible in h.Screen() (d) integrity (e) same Serial
	}
	h.Press("ctrl+w", "1"); h.Resize(160, 40) // pinned command at its min size => command; h.Resize(150,40) => fallback hint shown
}
func TestCompactThreadToCommandAndBack(t *testing.T) // CA 10.3 bullet 1, with stub thread draft "2시?"
func TestWidgetFeedToCommand(t *testing.T)            // CA 10.3 bullet 2
func TestStandardFeedSwappedAndNotificationsToCommand(t *testing.T) // CA 10.3 bullet 3: same instance & selection; no key twice in Frame().Routes
func TestRandomResizeKeepsState(t *testing.T) {
	for _, seed := range []uint64{1, 2, 3} { t.Run(fmt.Sprint(seed), func(t *testing.T) {
		// same scenario as preservation; 200 random sizes cols∈[1,330], rows∈[1,100] (PCG(seed,seed)) with h.Check() each;
		// back to 200x60 => (a)(b)(c)(e) hold
	}) }
}
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/app/ -run 'TestFocus|TestCtrl|TestCaptured|TestTabGoesTo|TestHardGlobals|TestMouse|TestCursor|TestModeSwitch|TestCompactThread|TestWidgetFeed|TestStandardFeed|TestRandomResize'` · Expected: FAIL

- [ ] **Step 3: 구현** — CA 5.3.4 2·3단계, 5.3.5 그대로. 결정:
  - `ctrl+c`(오버레이를 건너뜀 — CA 5.3.4 2.1): 포커스 컴포넌트가 `CapturesInput()` 이면 먼저 그 `HandleKey` 에 넘기고 handled 면 끝(입력 지우기). 아니면 무장(`ui.After(1s, quitArmMsg{gen})` + 토스트 `한 번 더 누르면 종료해요`), 무장 중 두 번째면 `tea.Quit`. 컴포넌트 쪽 판정은 "책임 위임" 이 아니라 core 가 정한 규칙이다: Task 22 의 `ctrl+c` 규칙(입력 값이 있으면 지우고 handled, 비어 있으면 handled=false) — `widgets.Composer` 와 core 한 줄 입력(Task 28·33·34)이 구현하고, `stub.Chat`(Task 23)이 시험으로 고정하며, chat·sns-fun 의 입력 컴포넌트도 같은 규칙을 따른다(값이 빈 입력에서 `ctrl+c` 를 handled 로 삼키면 종료 불가 — 금지).
  - 하드·소프트 전역 키 판정은 모두 `ctx.Keys.Matches(msg, keymap.<Action>)` 로 한다(키 문자열 하드코딩 금지 — 그래야 `config.toml` `[keys]` 재정의가 먹는다; Task 25 Context 초기화 ③, Task 29 `TestKeysOverrideOpensPalette`). 예외는 `ctrl+w` 접두 뒤의 두 번째 키(`h/j/k/l`·숫자 등 고정)뿐.
  - **자동완성 중 `tab` 전달(CR-A, CA 5.3.4 2.3)**: 하드 전역 판정에서 키가 `FocusNext`·`FocusPrev`·`ComposeComplete` 중 하나에 맞으면, 포커스 의도/링을 건드리기 **전에** 포커스 컴포넌트를 본다 — `ui.InputCapturer` 로 `CapturesInput()==true` **이고** `ui.Completer` 로 `Completing()==true` 이면 그 키를 `HandleKey` 에 넘긴다. `handled=true` 면 그 키 처리는 끝(포커스·`NavState` 불변), `false` 면 원래 하드 전역(창 순환, 또는 포커스 링이 창 1개인 모드의 `tab.next`/`tab.prev`)으로 이어 간다. 둘 중 하나라도 아니면(입력 캡처 아님·`Completer` 미구현·팝업 없음) 지금과 같이 하드 전역. 이 판정은 오버레이(2.1)·`ctrl+w` 접두(2.2) 뒤, 다른 하드 전역(`ctrl+k`·`ctrl+f`·`ctrl+n`) 판정과 같은 자리다.
  - `ctrl+w` 접두 다음 키: `h/j/k/l`·화살표 = 창 중심점 기준 그 방향 가장 가까운 창, `w` 다음 창, `1..5` 고정(지휘소·표준·컴팩트·위젯·초소형), `0` 자동, 그 밖 키 = 접두 취소(키 버림).
  - 포커스 슬롯 결정은 매 Update 끝(그리고 View 직전)에 CA 5.3.5 (1)(2)(3), 바뀌면 `FocusChangedMsg` 방송. 이 방송 시점에는 이번 투영의 `Store.SetViewing` 이 이미 끝나 있어야 한다(Task 30, CR-C — 투영 직후 갱신).
  - 마우스는 `View.OnMouse` 를 쓰지 않고 `Update` 의 `tea.MouseMsg` 로 처리; 좌표 → `Layout.At`, 테두리 칸 클릭도 그 창 포커스, 내부 좌표 = `x-inner.X, y-inner.Y`(테두리면 음수 → 컴포넌트에 넘기지 않음). 탭 줄 클릭 = `Tabs.Hit` → 그 탭, 맥락 줄 클릭 = 팔레트 열기(Task 29).
  - 소프트 전역 `enter`: 보이는 `ToastAlert` 가 있으면 그 경로로 이동(Task 26 결정과 함께, 9.1).
  - 고정은 `pin *layout.Mode` 런타임 상태(디스크 저장은 Task 32).

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/app/ -count=1 -v -run 'TestFocus|TestCtrl|TestCaptured|TestTabGoesTo|TestHardGlobals|TestMouse|TestCursor|TestModeSwitch|TestCompactThread|TestWidgetFeed|TestStandardFeed|TestRandomResize'` · Expected: PASS. 끝나면 오케스트레이터에 "CR-A core 반영됨 — plan 04 Task 10 `TestCompletionTabAccept` 추가 가능" 을 알린다.

- [ ] **Step 5: 커밋** — `git add internal/ui/app && git commit -m "feat(core): focus intents, key routing, ctrl+w prefix, mouse, pins; preservation tests"`

---
### Task 28: 오버레이 틀 · 도움말 · 확인/이모지/신고 대화상자

**Files:**
- Modify: `internal/ui/app/{overlays.go,help.go,model.go}`, `internal/ui/widgets/{confirm.go,emoji.go,report.go}`
- Test: `internal/ui/app/overlay_test.go`(package `app_test`), `internal/ui/widgets/dialogs_test.go`(package `widgets_test` — 하네스를 쓰므로)

**Interfaces:**
- Consumes: `ui.Overlay`, `OpenOverlayMsg`, `CloseOverlayMsg`, `ui.Registered().Overlays`, `api.Report`, bubbles `textinput`
- Produces: `FrameInfo.Overlays`; `widgets.Confirm(title, body, yes, no string, onYes tea.Cmd) ui.Overlay`, `widgets.EmojiPicker(onPick func(emoji string) tea.Cmd) ui.Overlay`, `widgets.ReportDialog(targetType, targetID, label string) ui.Overlay`(CA 5.7); 앱 도움말(`?` 오버레이, 그리고 `ui.RegisterRoute(ui.RouteHelp, …)` 를 `ui/app` 의 `init()` 에서). 비공개: `helpView`(오버레이·경로 겸용 컴포넌트), `newTextInput() textinput.Model`(widgets 내부 — 실제 커서, `ctrl+k` 제거, `SanitizeInput` 거름, 창 안에서 쓰일 때 `ctrl+c` 는 Task 22 규칙: 값이 있으면 지우고 handled, 없으면 미처리 — 오버레이 안에서는 앱이 `ctrl+c` 를 오버레이에 주지 않으므로 해당 없음).

- [ ] **Step 1: 실패 시험 작성**

```go
func TestOverlayBoundsClampAndDegrade(t *testing.T) {
	// probe overlay Bounds = huge rect => clamped to body; 120x40 Bounds 60x12 => bordered box at that rect (FrameInfo.Overlays[0].Bordered)
	// Bounds 20x5 (W<24) => r=body, Bordered=false; tiny 50x20 => r=body, Bordered=false; Resize(19,10) => no overlay drawn, Overlays empty; Resize back => drawn again
}
func TestOverlayStackAndModal(t *testing.T) // 4th push ignored (len 3); modal swallows unhandled "j"; Modal()==false probe lets unhandled "f" reach soft globals; esc unhandled => top closes; click outside => top closes;
                                            // ctrl+c with a probe overlay open => probe HandleKey NOT called, quit armed (toast), overlay stays open
func TestOverlayPasteGoesToTop(t *testing.T) // stub composer focused + ReportDialog open with note focused: Paste("광고 링크") => note value, composer draft unchanged
func TestOverlayCursor(t *testing.T)      // ReportDialog note focused => h.Cursor() == note cursor + overlay inner origin (r.X+1, r.Y+1); Confirm open => h.Cursor()==nil
func TestHelpOverlay(t *testing.T)        // "?" opens help (title "도움말"); j scrolls; esc closes; contains "ctrl+k" "이동" and the two notices (emoji normalization, ambiguous-width terminals)
func TestHelpGolden(t *testing.T)         { uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) { h.Press("?"); h.Golden("help") }) }
var dialogSizes = append(append([]uitest.Size{}, uitest.Required...), uitest.Size{44, 16}, uitest.Size{20, 6}, uitest.Size{19, 10})
func TestDialogGoldens(t *testing.T) {
	for name, open := range map[string]func(h *uitest.Harness){
		"confirm": func(h *uitest.Harness) { h.Send(ui.OpenOverlayMsg{Overlay: widgets.Confirm("로그아웃할까요?", "이 기기에서 로그아웃해요.", "로그아웃", "취소", nil)}) },
		"emoji":   func(h *uitest.Harness) { h.Send(ui.OpenOverlayMsg{Overlay: widgets.EmojiPicker(nil)}) },
		"report":  func(h *uitest.Harness) { h.Send(ui.OpenOverlayMsg{Overlay: widgets.ReportDialog("message", "M1", "민지의 메시지")}) },
	} { uitest.ForEachSize(t, dialogSizes, func(t *testing.T, h *uitest.Harness) { open(h); h.Golden(name) }) }
}
// widgets/dialogs_test.go (with harness)
func TestConfirmKeys(t *testing.T)   // "y" or "enter" => onYes cmd's msg delivered, overlay closed; "n"/"esc" => closed, onYes not run
func TestEmojiPicker(t *testing.T)   // Type("박수") => first item 👏; enter => onPick("👏") and closed; reopen => 👏 first in recent row; Type("clap") also finds 👏
func TestReportDialogSubmit(t *testing.T) {
	// choose "스팸·광고", note "광고", submit => apitest POST /v1/reports body {"target_type":"message","target_id":"M1","reason":"spam","note":"광고"}
	// 201 => toast "신고했어요" + closed; 409 already_exists => toast "이미 신고한 대상이에요" + closed; 500 => inline "서버 오류가 났어요", stays open
}
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/app/ ./internal/ui/widgets/ -run 'TestOverlay|TestHelp|TestDialog|TestConfirm|TestEmoji|TestReport'` · Expected: FAIL

- [ ] **Step 3: 구현** — CA 5.3.7 그대로(합성 `Z(10+i)`, `body` = 상태줄 뺀 사각형, 강등 규칙, 커서·붙여넣기 규칙, TooSmall 미표시). 결정:
  - 기본 Bounds: 도움말 `min(76, body.W) × min(28, body.H)` 가운데, Confirm `48×8`, EmojiPicker `44×14`, ReportDialog `52×16`, 모두 가운데.
  - `esc` 를 오버레이가 처리하지 않으면 앱이 맨 위를 닫는다. 오버레이 바깥 클릭도 닫기.
  - `OpenOverlayMsg{ID}` 는 `Registered().Overlays[ID](ctx, Args)`, 미등록이면 무시 + `ctx.Debug` 일 때 slog debug.
  - 도움말 내용: keymap 그룹(전역·창·목록·입력·메시지·글)별 `키  설명` 표 + 안내 두 줄 `이 클라이언트에서 입력한 글의 일부 이모지 수식자(피부색·결합 이모지 등)는 화면 폭을 맞추려고 정규화돼요.` / `모호 폭 문자를 2칸으로 그리는 터미널 설정은 지원하지 않아요.` 스크롤은 `widgets.List[string]`.
  - EmojiPicker 기본 집합(폭 2 이모지만, VS16 없음): 👍 👎 😀 😂 😍 🥲 😭 😡 🙏 👏 🎉 🔥 💯 ✅ ❌ 👀 🤔 😅 😎 🙌 💪 🤝 🍚 ☕ 🍺 🎂 🚀 🐛 💡 📌 ⭐ 💬 — 각 항목 한국어·영어 이름 2개 이상(`👏`: 박수, clap). 최근 8개는 패키지 메모리.
  - ReportDialog 사유 라벨: spam 스팸·광고, harassment 괴롭힘·혐오, illegal 불법 콘텐츠, sexual 음란물, violence 폭력·위협, other 기타. 메모 최대 1000자. 제출은 `ui.Do(ctx, "core.report:"+targetID, …)`.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/app/ ./internal/ui/widgets/ -update -run 'TestHelpGolden|TestDialogGoldens' && go test ./internal/ui/app/ ./internal/ui/widgets/ -count=1` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/ui && git commit -m "feat(core): overlay stack with clamping/degrade, help, confirm, emoji picker, report dialog"`

---

### Task 29: 명령 팔레트 · core 명령 · 테마 런타임 전환과 저장

**Files:**
- Modify: `internal/ui/palette/{palette.go,chosung.go,sources.go,overlay.go}`, `internal/ui/app/{commands.go,model.go}`
- Test: `internal/ui/palette/palette_test.go`(package `palette_test` — `TestPeopleSearchDebounced` 등이 하네스를 쓰고 `app` 이 `palette` 를 import 하므로), `internal/ui/app/palette_test.go`, `internal/ui/app/theme_test.go`(app 쪽 둘은 package `app_test`)

**Interfaces:**
- Consumes: `sahilm/fuzzy.Find`, `api.SearchUsers`, `Store`, `ui.SetThemeMsg`, `config.Save`
- Produces(CA 5.6): `palette.Command`, `Register`, `ItemKind` 상수, `Item`, `Source`, `RegisterSource`, `Open(ctx, initial) ui.Overlay`, `Chosung`, `Lookup`(CR-B — Task 2 에서 이미 실제 구현, 여기서 시험으로 고정); core 명령 등록(`ui/app` `init()`): `theme.cycle`, `theme.set.charm|midnight|cream|amber`, `layout.pin.command|standard|compact|widget|tiny`, `layout.auto`, `help`, `settings`, `blocks`, `logout`, `quit`, `reconnect`; `ThemeChangedMsg` 방송; **`ui.RunCommandMsg{ID, Args}` 앱 디스패치(CR-B)** — 등록된 모든 계획의 명령(core·chat·feed·fun)이 이 경로로 실행된다. plan 04 의 `/status`·`/focus` → `ui.RunCommand("status.set"|"focus.start"|"focus.stop", args)` 와 `h.Send(ui.RunCommandMsg{ID: "friend.add"|"dm.new"|"channel.prefs"|"pins.show"})` 시험들, plan 05 의 `TestStatusSetArgs`·`TestPaletteCommandsRun` 이 이것에 기댄다. 비공개: `(m *Model) runCommand(msg ui.RunCommandMsg) tea.Cmd`, `Model.palette ui.Overlay`(앱이 `ctrl+k` 로 연 팔레트 인스턴스).

- [ ] **Step 1: 실패 시험 작성**

```go
func TestChosung(t *testing.T) // "개발"=>"ㄱㅂ"; "노바 개발방"=>"ㄴㅂ ㄱㅂㅂ"; "abc가"=>"abcㄱ"; ""=>""
func TestRanking(t *testing.T) {
	// seeded ctx: query "ㄱㅂ" => first Item Title "#개발"; "개발" => "#개발"; "민지" => DM 민지 before person; "theme" => commands "테마: …";
	// Command.Available false => excluded; empty query => unread channels first ("#일반" badge 3) then recently chosen routes (max 8)
}
func TestPeopleSearchDebounced(t *testing.T) {
	// harness: ctrl+k, Type("mi") then Advance(299ms) => no request; Advance(1ms) => one GET /v1/users/search?limit=8&q=mi; Type("n") within 300ms of last key => only the latest query sent
	// results => ItemPerson "민지 @minji"; enter => NavigateMsg RouteProfile{ID:"@minji"} (TargetAuto)
}
func TestPaletteOpenCloseRun(t *testing.T) // ctrl+k opens; ctrl+k again closes; esc closes; enter on "#개발" => Nav active tab root ChDev; enter on command runs it and closes
func TestKeysOverrideOpensPalette(t *testing.T) {
	// CA 3.3 [keys]: app.New with Config.Keys {"palette.open":["ctrl+p"]} (context_test helper, Task 25) + WindowSizeMsg 120x40:
	// Update(ctrl+p) => Frame().Overlays len 1 (palette); Update(ctrl+k) on a fresh model => no overlay (default binding replaced)
}
func TestPaletteCursorAndPaste(t *testing.T) { // CA 10.3 overlay cursor/paste bullet
	// "i" (stub composer focused, draft "초안") ; ctrl+k ; Type("ㄱㅂ") => h.Cursor() == palette input origin + width("› ")+width("ㄱㅂ");
	// Paste("개발") => palette query "ㄱㅂ개발", stub draft still "초안"; ctrl+k => closed, h.Cursor() back on composer
}
func TestPaletteGolden(t *testing.T) { uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) { h.Press("ctrl+k"); h.Type("개"); h.Golden("palette") }) }
func TestThemeCommandsPersist(t *testing.T) {
	// run "theme.set.charm" => Context().Theme.Name==charm; stub instances received ThemeChangedMsg; config.Load(paths).Theme=="charm"
	// "theme.cycle" from amber => charm (A→B→C→D→A order); Styled() bg changed (contains charm BG sequence)
}
func TestLayoutCommands(t *testing.T) // "layout.pin.compact" at 200x60 => Mode compact + "📌"; "layout.auto" => command
func TestNavCommands(t *testing.T)    // "help" => help overlay; "settings" => RouteSettings pushed; "blocks" => Route{Kind:RouteSettings, Anchor:"blocks"}; "quit" => tea.QuitMsg recorded; "logout" => Confirm overlay opened
// (TestThemeCommandsPersist / TestLayoutCommands / TestNavCommands drive commands with h.Send(ui.RunCommandMsg{ID: …}) — the same dispatch other plans use)
// palette_test.go (package palette_test)
func TestLookup(t *testing.T) // t.Cleanup(palette.SnapshotForTest()); Register{ID:"t.echo"} => Lookup ok; Lookup("nope") => false; core ids from ui/app init() are NOT visible here (palette must not import app)
// app palette_test.go (package app_test) — CR-B dispatch
func TestRunCommandMsgDispatch(t *testing.T) {
	// t.Cleanup(palette.SnapshotForTest()) FIRST (keeps ui/app init() commands for later tests), then Register test commands (no ResetForTest):
	//   "core.test.echo": Run records (args) and returns ui.Toast("ran:"+args, ToastInfo); Available toggled by a test variable (nil Available also tested via "core.test.nilavail")
	// h.Send(ui.RunCommandMsg{ID:"core.test.echo", Args:"🎧 듣는 중 "}) => Run called once with exactly "🎧 듣는 중 " (no trim/sanitize), its Cmd ran => toast "ran:🎧 듣는 중 "
	// Available=false => Run not called; toast "지금은 쓸 수 없는 명령이에요" (ToastInfo); Nav/overlays unchanged
	// "core.test.nilavail" (Available nil) => runs
	// unknown ID "nope.x" => no panic, no toast, Nav()/Frame().Overlays unchanged, TeaMsgs unchanged
	// RunCommandMsg is consumed by the app: stub instances (OnUpdate hook) never see a ui.RunCommandMsg
	// palette open (ctrl+k) + Send(RunCommandMsg{ID:"logout"}) => palette closed first, Frame().Overlays len 1 and it is the Confirm (not stacked over the palette)
	// palette enter on "로그아웃" (Type("로그아웃"), enter) => identical outcome (enter emits ui.RunCommand(id, "") through the same dispatch)
}
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/palette/ ./internal/ui/app/ -run 'TestChosung|TestRanking|TestPeople|TestPalette|TestKeysOverride|TestThemeCommands|TestLayoutCommands|TestNavCommands|TestLookup|TestRunCommandMsg'` · Expected: FAIL(`TestLookup` 은 Task 2 구현으로 이미 통과할 수 있다)

- [ ] **Step 3: 구현** — CA 5.6 그대로. 결정:
  - 항목 점수 = `fuzzy.Find(query, candidates)` 중 최고 `Score`(후보: `Title`, `Chosung(Title)`, `Keywords…`). 같은 점수면 종류 순 `Channel, DM, Space, Person, Route, Command`, 그다음 제목.
  - 최근 이동 = 팔레트에서 **고른** 경로의 최근 8개(패키지 메모리, 새 API 없음).
  - 기본 소스(이 패키지 `init` 에서 `RegisterSource`): 채널(`#이름`, 부제 방 이름, 배지 `My`), DM(`ChannelLabel`, 접속 점), 방(`◆ 방 이름` → 그 방 기본 채널), 친구(→ `RouteProfile{ID:"@handle"}`), 명령(`Register` 된 것, `Available` 통과).
  - 사람 검색: 질의 룬 2개 이상이면 `ui.After(300ms, peopleSearchMsg{gen, q})` → 최신 세대만 `ui.Do(ctx, "core.palette.people:"+q, SearchUsers(q, 8))`.
  - 오버레이: Bounds `W=min(72, body.W-4)`, `H=min(20, body.H-2)`, 가로 가운데, `Y = body.Y + max(0, (body.H-H)/4)`. 첫 줄 입력 `› ` + textinput(실제 커서, `ctrl+k` 제거), 둘째 줄 구분선, 그 아래 항목(아이콘 `#`/`@`/`◆`/`›`, 제목, 부제 dim, 오른쪽 KeyHint·배지), 그룹 머리 `이동`/`명령`/`설정`. `↑/↓`·`ctrl+p/ctrl+n` 은 쓰지 않고 `↑/↓` 만(ctrl+n 은 하드 전역이지만 오버레이가 먼저 받으므로 팔레트 안에서는 무시).
  - 테마 전환: `SetThemeMsg` → `ctx.Theme = theme.Get(n)`, `cfg.Theme = string(n)`, `config.Save` 실패 시 토스트 `설정을 저장하지 못했어요`, 모든 살아 있는 컴포넌트·오버레이에 `ThemeChangedMsg`. 명령 제목: `테마 바꾸기`, `테마: Charm 보라|미드나잇 블루|크림 파스텔|레트로 앰버`, `레이아웃 고정: 지휘소|표준|컴팩트|위젯|초소형`(KeyHint `ctrl+w 1..5`), `레이아웃 자동`(`ctrl+w 0`), `도움말`(`?`), `설정`, `차단 목록`, `로그아웃`, `종료`(`ctrl+c ctrl+c`), `다시 연결`. 그룹: 테마·레이아웃 = `설정`, 도움말·설정·차단 목록 = `이동`, 로그아웃·종료·다시 연결 = `명령`. `logout` 은 `widgets.Confirm("로그아웃할까요?", "이 기기에서 로그아웃해요.", "로그아웃", "취소", <logout cmd — Task 30>)`. `reconnect` 는 실시간 클라이언트 재시작(Task 30), DisableRealtime 이면 토스트 `다시 연결해요` 만.
  - **`RunCommandMsg` 디스패치(CR-B, `commands.go` 의 `runCommand`)**: 앱 `Update` 가 `ui.RunCommandMsg` 를 **소비**한다(5.3.4 6단계 방송 대상에서 뺀다 — 명령이 두 번 실행되지 않게). 순서: ① `c, ok := palette.Lookup(msg.ID)`; 없으면 무시(`ctx.Debug` 일 때 `slog.Debug("unknown command", "id", msg.ID)`), 토스트 없음. ② 맨 위 오버레이가 `m.palette`(앱이 `ctrl+k` 로 연 그 인스턴스)이면 먼저 pop 하고 `m.palette = nil` — 그래서 명령이 여는 오버레이(로그아웃 `Confirm`, chat 의 `friend.add` 사용자 선택기 등)가 팔레트 위에 쌓이지 않는다(`tea.Sequence` 는 금지이므로 "닫기 Cmd + 실행 Cmd" 의 순서에 기대지 않고 앱이 한 Update 안에서 정한다). ③ `c.Available != nil && !c.Available(ctx)` 면 `Run` 을 부르지 않고 `ui.Toast("지금은 쓸 수 없는 명령이에요", ui.ToastInfo)`. ④ 아니면 `c.Run(ctx, msg.Args)` — `Args` 는 **원문 그대로**(자르기·정규화는 각 명령의 몫, 예 plan 05 `parseStatusArgs`), `Run == nil` 이면 무시. 돌려받은 Cmd 는 다른 Cmd 와 같이 실행. 팔레트 안 `enter`(명령 항목)는 직접 `Run` 을 부르지 않고 `ui.RunCommand(c.ID, "")` 만 낸다 — 팔레트·슬래시·시험의 `h.Send(RunCommandMsg)` 가 모두 같은 경로(Available 재확인 포함)를 탄다. 팔레트가 `esc`/`ctrl+k` 로 닫히면 `m.palette = nil`.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/palette/ ./internal/ui/app/ -update -run TestPaletteGolden && go test ./internal/ui/palette/ ./internal/ui/app/ -count=1` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/ui && git commit -m "feat(core): command palette with chosung fuzzy search, core commands, run-command dispatch, live theme switch"`

---

### Task 30: 실시간 브리지 · 부트스트랩/재동기화 · 유휴 · watch · 로그아웃 처리

**Files:**
- Modify: `internal/ui/app/{sync.go,bridge.go,model.go,commands.go}`, `internal/ui/app/export_test.go`
- Test: `internal/ui/app/sync_test.go`(package `app_test`, 하네스), `internal/ui/app/bridge_test.go`(package `app` — 비공개 `startRealtime`·`readyTimeoutMsg` 를 쓰므로 내부 시험. **`uitest` 를 import 하지 않는다**: 시계는 시험 파일 안의 기록형 `recTimers`(`After(d, msg)` 가 `(d, msg)` 를 기록하고 nil Cmd 반환, `Pending() []time.Duration`)와 고정 `Now`, API 는 `apitest.New`(api 만 import 하므로 순환 없음))

**Interfaces:**
- Consumes: `realtime.New/Options/Run/MarkProcessed/Position`, `store.Bootstrap/ApplyBootstrap/Reset/SetViewing`, `auth.Clear/DeviceID`, `api.WithOnUnauthorized/WithOnUpgradeRequired`
- Produces: CA 5.3.4 5단계(EventMsg → `Store.Apply` → 방송 → 알림(Task 31) → `MarkProcessed`), CA 5.4 "시작 순서"·유휴·watch·`SetViewing`(CR-C 시점: Project 직후·방송 전 — plan 04 CR-2 를 받는 곳), `ui.LoggedOutMsg`/`ResyncStartedMsg`/`ResyncDoneMsg` 방송. Consumes 추가: Task 23 `stub.Chat.OnUpdate`. 비공개: `(m *Model) startRealtime() tea.Cmd`, `readyTimeoutMsg{gen int}`, `bootstrapRetryMsg{gen int}`, `watchDebounceMsg{gen int}`, `unauthorizedMsg{}`, `Model.rtDial`(시험용 다이얼), `Model.send func(tea.Msg)`; export_test: `SetDialForTest(m *Model, dial func(ctx context.Context, url string, h http.Header) (realtime.Conn, error))`, `SetSenderForTest(m *Model, send func(tea.Msg))`.

- [ ] **Step 1: 실패 시험 작성**

```go
// bridge_test.go (package app): Model with DisableRealtime=false, Timers: &recTimers{}, Now: fixed 2026-11-05T02:05Z, fake dial, sender collecting msgs into a channel
func TestBridgeDeliversEventsAndState(t *testing.T) // fake conn: hello -> (client resume) -> ready -> message.created => collected RealtimeStateMsg phases Connecting,Syncing,Live and EventMsg{ready}, EventMsg{message.created}
func TestReadyTimeoutFallsBackToREST(t *testing.T) // fake dial that never sends hello: startRealtime's cmd, when run, records 5s in recTimers (Pending contains 5s); Update(readyTimeoutMsg{gen}) => store.Bootstrap issued (apitest /v1/me recorded); later ready => bootstrap once more
// sync_test.go (harness)
func TestFirstReadyBootstraps(t *testing.T) {
	h := uitest.New(t, uitest.WithSeed(func(*store.Store) {}))
	h.Event("ready", map[string]any{"resumed": false, "resync_required": false, "replayed": 0, "seq": 0, "epoch": "E"})
	// apitest saw /v1/me … /v1/blocks; Store.Sync.Bootstrapped; ResyncStartedMsg then ResyncDoneMsg{Err:nil} broadcast (stub records); screen shows fixtures channels
	// ready resumed:true later => no new requests; ready resync_required:true => bootstrap again
}
func TestBootstrapNetworkFailureRetries(t *testing.T) { // Review Focus 1
	// /v1/me handler hijacks & closes the connection on the first call, succeeds afterwards
	// after ready: toast "서버에 연결할 수 없어요", status shows "✕ 오프라인", Clock().Pending() contains 10s
	// Advance(10s) => bootstrap retried and succeeds; status "● 온라인"; no crash at any Required size (h.Check())
}
func TestPruneUnknownTabsAfterBootstrap(t *testing.T) // nav tab for channel id not in bootstrap => removed; active tab clamps
func TestEventAppliedAndBroadcast(t *testing.T)  // h.Event("message.created", …#일반 by minji) => Store #일반 unread 4; stub instances received ui.EventMsg; fake RT MarkProcessed(seq) recorded
func TestUnauthorizedMidSessionReturnsToLogin(t *testing.T) { // Review Focus 2
	// control: apitest 401 {"code":"invalid_credentials"} on that call => error toast only, credentials kept, no LoggedOutMsg (Task 8 hook rule)
	// stub composer draft "보내는 중"; apitest Error(…, 401, "unauthorized") on a ui.Do triggered call (palette people search) => next Update: credentials file removed,
	// Store reset (reducers Reset called), Nav shows login screen with "다른 곳에서 로그아웃됐어요. 다시 로그인해 주세요", LoggedOutMsg{Reason:"unauthorized"} broadcast
	// same outcome for RealtimeStateMsg{State{Phase: PhaseStopped, Err: realtime.ErrUnauthorized}}
}
func TestSuspendedAndUpgrade(t *testing.T) // ErrSuspended => login screen "정지된 계정이에요"; ErrUpgradeRequired => stays in shell, status "✕ 업데이트 필요", toast "masil을 업데이트해 주세요"
func TestLogoutCommand(t *testing.T)       // palette "logout" -> confirm "y" => POST /v1/auth/logout recorded, credentials removed, login screen, LoggedOutMsg{Reason:"logout"}; network error on logout => still logged out locally
func TestIdleDetection(t *testing.T)       // fake RT: Advance(10m) => SetIdle(true); Press("j") => SetIdle(false); Advance(9m)+Press+Advance(9m) => no SetIdle(true)
func TestWatchDebounced(t *testing.T)      // probe RouteTrends component implementing Watcher (topics post:A,post:B) registered in TestMain; navigate trends at 200x60 (feed slot) => Advance(299ms) no Watch; Advance(1ms) => Watch([post:A post:B]); 60 topics => capped 50, focused pane's first
func TestSetViewing(t *testing.T)          // #개발 visible => Store.Viewing(ChDev); navigate home in compact => false
func TestSetViewingBeforeBroadcast(t *testing.T) { // CR-C (= plan 04 CR-2): Viewing is updated right after Project, before this Update's broadcasts
	// WithRoute(channel ChDev); Resize(120,30) compact; Press("j"); Press("t") => thread replaces Main: Store.Viewing(ChDev)==false (stub thread is not a ChannelViewer)
	// ch := h.Instance("channel/"+ChDev+"/").(*stub.Chat); ch.OnUpdate = record Store.Viewing(ChDev) for each ui.ModeChangedMsg / ui.FocusChangedMsg / ui.RouteUpdatedMsg received
	// Resize(200,60) => first ModeChangedMsg{Compact→Command} recorded with Viewing(ChDev)==true (channel is back in Main beside the thread slot), every record in that Update true
	// Resize(120,30) => ModeChangedMsg recorded with Viewing(ChDev)==false
	// at 200x60: h.Send(NavigateMsg{channel ChGeneral}) => general's stub (OnUpdate set right after creation via h.Instance) records FocusChangedMsg with Viewing(ChGeneral)==true && Viewing(ChDev)==false
	// h.Event("message.created" in #개발) at 200x60 with #개발 visible => ch.OnUpdate sees ui.EventMsg with Viewing(ChDev)==true (same Update order: Project → SetViewing → Apply → broadcast)
}
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/app/ -run 'TestBridge|TestReadyTimeout|TestFirstReady|TestBootstrapNetwork|TestPrune|TestEventApplied|TestUnauthorized|TestSuspended|TestLogoutCommand|TestIdle|TestWatch|TestSetViewing' -count=1` · Expected: FAIL(`TestSetViewingBeforeBroadcast` 포함)

- [ ] **Step 3: 구현** — CA 5.4 "시작 순서"와 events.md 5.3. 결정:
  - `Options.API == nil` 이면 앱이 `api.New(cfg.ServerURL(), WithToken(cred.Token), WithClock(now), WithOnUnauthorized(m.onUnauthorized), WithOnUpgradeRequired(m.onUpgrade))` 를 만든다(Task 25 Context 초기화 ⑥ 의 호출에 두 훅을 더한 것, 하네스도 이 경로). `now` = `o.Now`, nil 이면 `time.Now` — 그래서 쿨다운 기록(`Cooldown`)과 상태줄 남은 시간 계산(`Cooldown − ctx.Now()`, Task 26)이 같은 시계를 쓴다. 자격 증명이 없어도(로그인 화면) 같은 옵션으로 토큰 없이 만들고, `LoggedInMsg` 에서 `SetToken` 한다. 콜백은 HTTP 고루틴에서 불리므로 `atomic.Bool` 플래그를 세우고 `m.send` 가 있으면 `unauthorizedMsg{}` 를 보낸다. `Update` 는 맨 앞에서 플래그를 보고 로그아웃 흐름을 탄다(하네스의 동기 Cmd 에서도 다음 Update 에서 처리됨).
  - 로그아웃 흐름(공통): realtime ctx 취소 → `auth.Clear(paths)` → `Store.Reset()` → 인스턴스 캐시 비움 → `NavState` 를 로그인 화면으로 → `LoggedOutMsg{Reason}` 방송. Reason → 로그인 화면 안내: `unauthorized` "다른 곳에서 로그아웃됐어요. 다시 로그인해 주세요", `suspended` "정지된 계정이에요", `logout` 없음.
  - 부트스트랩: 첫 `ready`(또는 `ResyncRequired`, 또는 5초 시한 경과)에 `ResyncStartedMsg` + `store.Bootstrap`; `BootstrapMsg` 오류면 `Sync.Err` 설정 + `ErrorMsg` 토스트 + `ui.After(10s, bootstrapRetryMsg{gen})`; 성공이면 `ApplyBootstrap` 의 Cmd + 알 수 없는 채널 탭 정리 + `ResyncDoneMsg`. 상태줄 연결 항목은 `!Sync.Bootstrapped && Sync.Err != nil` 이면 `✕ 오프라인`.
  - 브리지: `realtime.New(Options{API, DeviceID: auth.DeviceID(paths), Mode: ModeFull, Resume: 직전 Position, OnEvent: send(ui.EventMsg), OnState: send(ui.RealtimeStateMsg), Logger, Dial: m.rtDial})` 를 고루틴에서 `Run`. `reconnect` 명령은 ctx 취소 후 같은 `Position` 으로 새로 시작.
  - 유휴 10분·watch 300ms 디바운스(포커스 창의 Watcher 토픽 먼저, 합쳐 중복 제거 후 50개)는 CA 5.4 그대로.
  - **`SetViewing` 시점(CR-C — "매 Update 끝" 이 아니다)**: `Store.SetViewing(투영 결과에 놓인 인스턴스 중 ChannelViewer 들의 ViewingChannels 합집합)` 을 **Compute→Project(→Resize) 를 마친 바로 그 자리에서, 같은 `Update` 안에서, 그 `Update` 가 내는 어떤 방송보다 먼저** 부른다. 즉 Task 25 의 1단계 경로(WindowSize·Navigate·Back·탭 전환·Front 토글·고정·로그인/로그아웃 등 `NavState`/모드가 바뀌어 다시 투영하는 모든 경로)의 끝에 붙이고, 그 뒤에 `ModeChangedMsg`(1단계)·`FocusChangedMsg`(Task 27 의 포커스 결정)·`RouteUpdatedMsg`(캐시 재사용)를 방송한다. `EventMsg` 는 투영이 바뀌지 않으므로 직전 투영의 집합이 그대로 맞다(순서 `SetViewing` 완료 상태 → `Store.Apply` → 방송). 투영이 바뀌지 않는 Update 에서는 다시 부르지 않아도 된다(같은 집합이면 Store 쪽 no-op). 이유: chat 의 읽음 판정(plan 04 `maybeArmRead`)이 이 방송들을 받는 순간 `Store.Viewing` 을 읽으므로, 방송 뒤에 갱신하면 직전 프레임의 집합을 보고 읽음이 늦어진다(예: 컴팩트 스레드 → 지휘소 전환에서 Main 에 채널이 새로 보여도 미처리). `View` 에서는 갱신하지 않는다(View 는 순수).

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/app/ -count=1 -race` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/ui/app && git commit -m "feat(core): realtime bridge, bootstrap/resync, idle, watch, unauthorized/logout handling"`

---

### Task 31: TUI 의 alert 처리 — OS 알림·토스트·집중 요약

**Files:**
- Modify: `internal/ui/app/{alerts.go,model.go}`
- Test: `internal/ui/app/alerts_test.go`(package `app_test`)

**Interfaces:**
- Consumes: `notify.Notifier.Handle`, `notify.Input`, `Store.Alerts`, `ui.OverlayFocusSummary`, `ui.OpenRegisteredOverlay`, `termcaps.Caps.FocusReports`
- Produces: CA 9.1 동작. 비공개: `(m *Model) handleAlert(ev realtime.Event, a api.Alert) tea.Cmd`, `(m *Model) viewingTarget(a api.Alert) bool`, `(m *Model) alertTargetRoute(a api.Alert) ui.Route`(모든 `target.type` 의 이동 경로), 연 요약 id 최근 50개 링.

- [ ] **Step 1: 실패 시험 작성**

```go
func TestAlertPopsOSNotificationOnce(t *testing.T) // h.Event("alert", mention alert for #일반 (not visible)) => h.Notify.Shown len 1 (title/body Format); toast ToastAlert visible; same alert id again (replay) => still 1
func TestAlertSilentWhenViewingFocusedTerminal(t *testing.T) // h.Send(tea.FocusMsg{}) ; viewing #개발 ; alert target #개발 => no OS popup, no toast, Store.Alerts has it
func TestAlertBlurredStillPops(t *testing.T)   // FocusMsg then BlurMsg; viewing target => Popup
func TestAlertDesktopOff(t *testing.T)         // Config.Notify.Desktop=false => no OS popup, toast still when not visible
func TestOldReplayedAlertSilent(t *testing.T)  // EventMsg{Replayed:true, alert created 11m ago} => no OS popup
func TestToastAlertRoutes(t *testing.T) { // CA 9.1 "대상 경로로 이동" for EVERY target.type (table-driven; each row: h.Event("alert", …) on a non-visible target => toast Route, then enter => Nav reflects it)
	// target message, dm/mention/channel_message (thread_root_id null)   => {channel, ID: channel_id, Anchor: message_id}
	// target message, thread_reply / mention in a thread (thread_root_id R) => {thread, ID: R, Arg: channel_id, Anchor: message_id}
	// target channel (message_id null)                                    => {channel, ID: channel_id ?? target.id}
	// target post (social like/repost/reply/quote, post mention)          => {post, ID: post_id ?? target.id}
	// target space, kind invite, space_id matching Store.DirectInvites[0] => {invite, Arg:"direct", ID:<invite id>}; no match => ID ""
	// target user, kind friend_request                                    => {friends}
	// target user, kind social (follow_accepted, guestbook_entry, …)      => {profile, ID: target.id}
	// target notification                                                 => {notifications}
	// kind focus_summary when ui.OverlayFocusSummary is unregistered      => {notifications} (any target)
	// fallbacks => {notifications}: unknown target.type ("weird"); message/channel target with channel_id and id both empty; post target with no id
}
func TestFocusSummaryOpensOnce(t *testing.T) {
	// probe overlay registered as ui.OverlayFocusSummary: alert{kind focus_summary, id F1} => overlay opened with args "F1", no toast;
	// same id again => not reopened; Store.FocusEnded via focus.ended event does not open anything
}
func TestFocusSummaryUnregisteredFallsBackToToast(t *testing.T) {
	// t.Cleanup(ui.SnapshotRegistryForTest()) FIRST (restores TestMain's probes — RouteTrends Watcher (Task 30), OverlayFocusSummary probe —
	// and init()-registered RouteHelp/RouteLogin/RouteSettings for later tests in this binary, e.g. sync_test.go runs after alerts_test.go);
	// then ui.ResetRegistryForTest(); stub.Install() => alert{kind focus_summary} => ToastAlert instead of overlay
	// (must not use t.Parallel: the registry is process-global)
}
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/app/ -run 'TestAlert|TestOldReplayed|TestToastAlertRoutes|TestFocusSummary' -count=1` · Expected: FAIL

- [ ] **Step 3: 구현** — CA 9.1 그대로. `Input{Role: RoleTUI, Now: ctx.Now(), Replayed, DesktopEnabled: cfg.Notify.Desktop, TerminalFocusKnown: caps.FocusReports, TerminalFocused, ViewingTarget}`. `tea.FocusMsg`/`BlurMsg` 를 처음 받으면 `Caps.FocusReports=true`. `ViewingTarget` = 투영된 `ChannelViewer` 채널에 `target.channel_id` 가 있음(스레드 답글이면 보이는 `RouteThread` 의 ID == `thread_root_id`) 또는 보이는 `Watcher` 토픽에 `post:<post_id>`. 토스트 문구 = `alert.title` + ` — ` + `alert.body`(Sanitize, 한 줄). 토스트 경로 = 비공개 `(m *Model) alertTargetRoute(a api.Alert) ui.Route` 하나로 정한다(위 시험의 표, 이 계획의 결정 — api.md `Alert.target.type` 6종 `channel|message|post|space|user|notification` 전부 + 대체). 판정 순서: ① `kind == focus_summary` → `RouteNotifications`(요약 오버레이 미등록일 때만 토스트가 생긴다). ② `kind == friend_request` → `RouteFriends`(chat 소유 친구 화면 — 요청 목록이 거기 있다). ③ `target.type` 별: `message`/`channel` → 채널 ID(`channel_id`, `channel` 이면 없을 때 `target.id`)가 비면 대체, `thread_root_id` 있으면 `RouteThread{ID: root, Arg: 채널, Anchor: message_id}`, 아니면 `RouteChannel{ID: 채널, Anchor: message_id 또는 ""}`; `post` → `RoutePost{ID: post_id 또는 target.id}`; `space` → `RouteInvite{Arg:"direct", ID: Store.DirectInvites 에서 space_id(없으면 target.id) 일치 초대 id 또는 ""}`; `user` → `RouteProfile{ID: target.id}`(팔로우 수락은 상대, 방명록은 target 이 나이므로 내 프로필); `notification` → `RouteNotifications`. ④ 모르는 `type`·필요한 ID 누락 → `RouteNotifications`(알림함에서 찾을 수 있게). sns-fun 의 집중 요약 안 이동(plan 05 `alertRoute`)과 같은 표를 쓰는 것이 바람직하다 — 달라지는 칸(`friend_request` → `RouteFriends`, `focus_summary`·대체 → `RouteNotifications`)은 오케스트레이터에 알린다.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/app/ -count=1` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/ui/app && git commit -m "feat(core): tui alert handling with ledger dedupe, toasts and focus summary trigger"`

---

### Task 32: `state.json` 기억 · `app.Run`

**Files:**
- Modify: `internal/ui/app/{state.go,run.go,model.go}`, `internal/ui/app/export_test.go`
- Test: `internal/ui/app/state_test.go`, `internal/ui/app/run_test.go`(둘 다 package `app_test`; `WriteStateForTest`·`RunProgramForTest` 는 `export_test.go` 로 연다)

**Interfaces:**
- Consumes: `config.WriteFileAtomic`, `instance.AcquireTUI`, `tea.NewProgram`, `tea.WithContext/WithInput/WithOutput`
- Produces: `app.SavedState`, `app.SavedTab`(아래), `Run(ctx, o Options) error`(CA 5.4). 비공개: `(m *Model) savedState() SavedState`, `(m *Model) restore(s SavedState)`, `readState(p config.Paths, userID string) (SavedState, bool)`, `writeState(p config.Paths, s SavedState) error`, `runProgram(ctx context.Context, m *Model, opts ...tea.ProgramOption) error`; export_test: `WriteStateForTest(m *Model) error`, `RunProgramForTest(ctx context.Context, o Options, opts ...tea.ProgramOption) error`.

```go
type SavedState struct {
	Version     int        `json:"version"` // 1
	UserID      string     `json:"user_id"`
	OpenTabs    []SavedTab `json:"open_tabs"`
	ActiveTab   int        `json:"active_tab"`
	Front       string     `json:"front"` // "tab" | "feed" | "notifications"
	LastSpaceID string     `json:"last_space_id"`
	FeedTab     string     `json:"feed_tab"`    // Feed[0].Arg
	PinnedMode  string     `json:"pinned_mode"` // layout Mode.ID() or "auto"
}
type SavedTab struct{ Kind string `json:"kind"`; ID string `json:"id,omitempty"` }
```

- [ ] **Step 1: 실패 시험 작성**

```go
func TestSavedStateShape(t *testing.T)   // after opening #일반,#개발 tabs, FrontFeed, pin compact => JSON equals CA 3.5 example shape (keys/values; tabs roots only, no threads)
func TestRestoreOnStart(t *testing.T)    // write state for sam => new harness: Nav tabs/active/front/feed arg and pin restored
func TestRestoreIgnoresForeignOrCorruptState(t *testing.T) // user_id other => default nav; "{{garbage" => default nav, no error toast; version 2 => ignored (Review Focus 3)
func TestConcurrentSaveStateStaysValid(t *testing.T) // two models WriteStateForTest 50x concurrently => final state.json parses and equals one of the two (Review Focus 5)
func TestPinStartupPrecedence(t *testing.T) // state.json pinned_mode "auto" + config layout.pin "widget" => widget; state "compact" => compact
func TestRunLifecycle(t *testing.T) {
	// RunProgramForTest(ctx with cancel after first frame, Options{…DisableRealtime:true}, tea.WithInput(strings.NewReader("")), tea.WithOutput(io.Discard), tea.WithWindowSize(120,40))
	// while running: instance.TUIRunning(paths)==true; after return: nil error (ErrProgramKilled+context.Canceled mapped to nil), TUIRunning==false, state.json exists with user_id sam
	// quit path: a cmd delivering tea.Quit (ctx NOT canceled) => nil error, state.json written, lock released
	// panic path: a probe component panicking in Update (ctx NOT canceled) => errors.Is(err, tea.ErrProgramKilled) && err != nil, lock still released
}
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/app/ -run 'TestSavedState|TestRestore|TestConcurrentSave|TestPinStartup|TestRunLifecycle' -count=1` · Expected: FAIL

- [ ] **Step 3: 구현** — `New` 에서 자격 증명이 있으면 `readState(paths, cred.UserID)` 로 복원(사용자 다름·파싱 실패·`version != 1` 이면 무시). 시작 고정 = `state.pinned_mode`(≠"auto") > `config.layout.pin`(≠"auto") > 없음. `Run`: `instance.AcquireTUI` → `tea.NewProgram(m, tea.WithContext(ctx))` → `m.send = p.Send` → (자격 증명 있고 실시간 켜짐이면) `startRealtime` → `p.Run()` → 종료 시 `writeState` + 잠금 해제(둘 다 `defer` 가 아니라 아래 오류 변환 **전에** 반드시 수행 — 오류 경로에서도 상태 저장·잠금 해제가 빠지지 않게; `writeState` 실패는 `slog.Warn` 만, Run 의 오류로 올리지 않는다) → **오류 변환**: bubbletea v2.1.1 은 `WithContext(ctx)` 의 ctx 가 취소되면 `p.Run()` 이 `fmt.Errorf("%w: %w", ErrProgramKilled, ctx.Err())`(tea.go 1183–1187)를 돌려준다. 바깥 ctx 취소(signal 로 인한 정상 종료)는 오류가 아니므로 `errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil` 이면 `nil` 을 돌려준다. 그 밖의 `ErrProgramKilled`(예: 패닉 `ErrProgramPanic`)와 다른 오류는 그대로 돌려준다. `tea.Quit` 으로 끝나면 `p.Run()` 이 이미 nil 이다. 로그 파일은 `cmd/masil` 이 연다(Task 35) — `Run` 은 `slog.Default()` 를 쓴다. `Options.Notifier == nil` 이면 `notify.New(notify.NewOSBackend("masil"), notify.OpenLedger(paths), slog.Default())`.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/app/ -count=1 -race` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/ui/app && git commit -m "feat(core): persist ui state.json and run the tea program under the tui lock"`

---

### Task 33: `authui` — 로그인·가입 화면

**Files:**
- Modify: `internal/ui/authui/{authui.go,form.go}`, `internal/ui/app/model.go`(로그인 상태 렌더·키 경로)
- Test: `internal/ui/authui/authui_test.go`(package `authui_test` — `app` 이 `authui` 를 import 하고 시험은 하네스를 쓰므로)

**Interfaces:**
- Consumes: `api.Login/Signup`, `auth.Save`, `widgets` 입력 규칙, `ui.RegisterRoute`
- Produces: `authui.LoggedInMsg{Credentials *auth.Credentials; Me *api.Me}`; `init()` 에서 `ui.RegisterRoute(ui.RouteLogin, …)`(`Route.Arg` = `"login"|"signup"`, 기본 login). 앱 쪽 결정: 로그아웃 상태(자격 증명 없음 또는 `RouteLogin` 강제)에서는 레이아웃 = `{SlotMain: {0,0,W,H-1} Bordered:false} + {SlotStatus}`(TooSmall 은 동일 규칙), **ctrl+c 외 모든 키를 로그인 컴포넌트가 먼저 받는다**(탭 순환 등 하드 전역 없음). `LoggedInMsg` → API 토큰 설정, `Store.Me`, 실시간 시작(꺼져 있지 않으면), `NavState` 초기화 후 `InitialRoute` 가 있으면 그리로.

- [ ] **Step 1: 실패 시험 작성**

```go
func TestLoginGolden(t *testing.T) {
	for _, mode := range []string{"login", "signup"} {
		uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) {
			h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteLogin, Arg: mode}}); h.Golden(mode)
		})
	}
}
func TestLoginSuccess(t *testing.T) {
	// logged-out harness (WithCredentials(nil)); apitest POST /v1/auth/login 200 AuthResult(token "msl_new", user sam);
	// Type("Sam") ; tab ; Type("correct horse") ; enter => request body {"handle":"sam","password":"correct horse","client":"tui","device_name":<hostname>}
	// credentials.json saved {Server: srv.URL, Token:"msl_new", UserID, Handle:"sam"} mode 0600; LoggedInMsg handled => shell visible (home), status not login
}
func TestLoginErrors(t *testing.T) // 401 invalid_credentials => "아이디 또는 비밀번호가 맞지 않아요" shown (NOT replaced by "다른 곳에서 로그아웃됐어요…" — Login is Auth:false and the code is not "unauthorized", so OnUnauthorized never fires; no LoggedOutMsg), password cleared, focus password; 429 => "너무 빨라요. 30초 뒤에 다시 해 주세요"; network => "서버에 연결할 수 없어요"
func TestLoginCtrlC(t *testing.T)  // handle field "sam": ctrl+c => field cleared, no quit toast; ctrl+c on empty field => "한 번 더 누르면 종료해요" (Task 22 rule)
func TestClientValidation(t *testing.T) // handle "ab" => "아이디는 영문 소문자·숫자·_ 3~20자예요" (no request); password "short" => "비밀번호는 8자 이상이에요"; signup confirm mismatch => "비밀번호가 서로 달라요"
func TestSignupFlow(t *testing.T)    // switch link => signup; fields handle, display name "민지 ❄️" (sanitized to "민지 ❄"), password, confirm; 409 handle_taken => "이미 쓰는 아이디예요"; 201 => logged in
func TestPasswordNotSanitizedOrShown(t *testing.T) // password "❄️pässwörd" sent byte-exact; Screen shows only mask chars; golden/log never contains it
func TestServerLineShown(t *testing.T) // screen contains "서버: " + srv.URL (truncated with "…" when narrow)
func TestUnauthorizedNotice(t *testing.T) // after LoggedOutMsg{Reason:"unauthorized"} the login screen shows the notice line from Task 30
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/authui/ -count=1` · Expected: FAIL

- [ ] **Step 3: 구현** — 결정: 가운데 카드(테마 테두리, 폭 `min(56, W)`, 좁으면 테두리 없이 본문 전체), 내용 순서: 제목 `masil`(Title) + 부제 `터미널 마실 — 로그인`/`터미널 마실 — 가입`, `서버: <URL>`, 칸(로그인: 아이디·비밀번호 / 가입: 아이디·표시 이름(선택)·비밀번호·비밀번호 확인), 버튼 `[ 로그인 ]`/`[ 가입하기 ]`, 전환 링크 `계정이 없나요? 가입하기`/`이미 계정이 있나요? 로그인`, 안내·오류 줄(Error 스타일), 제출 중 `widgets.Spinner`. 키: `tab`/`↓` 다음 칸, `shift+tab`/`↑` 이전 칸, 칸에서 `enter` = 다음 칸, 마지막 칸·버튼 `enter` = 제출, 링크 `enter` = 모드 전환. 입력 칸은 `authui` 안에서 Task 28 `newTextInput` 과 같은 규칙으로 만든다(bubbles `textinput`, `SetVirtualCursor(false)`, `KeyMap.DeleteAfterCursor` 에서 `ctrl+k` 제거, 글자·붙여넣기는 `SanitizeInput` — 비공개 함수는 패키지를 넘지 못하므로 같은 규칙을 복제; 아이디는 입력 시 소문자화, 비밀번호는 `EchoPassword` + 정규화 없음; `ctrl+c` 는 Task 22 규칙 — 포커스 칸에 값이 있으면 지우고 handled, 비어 있으면 handled=false 로 앱의 종료 무장에 넘긴다. 로그인 컴포넌트는 `CapturesInput()==true` 이므로 앱이 `ctrl+c` 를 먼저 이 컴포넌트에 준다 — Task 27 결정). `device_name` = `os.Hostname()`(실패 시 `""`), `client` = `"tui"`. 성공: `auth.Save(&auth.Credentials{Version:1, Server: ctx.API.BaseURL(), Token, SessionID: res.Session.ID, UserID: res.User.ID, Handle: res.User.Handle, SavedAt: ctx.Now()})` 후 `LoggedInMsg`.

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/authui/ -update -run TestLoginGolden && go test ./internal/ui/authui/ ./internal/ui/app/ -count=1` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/ui && git commit -m "feat(core): login and signup screens with client validation"`

---

### Task 34: `settings` — 설정 화면

**Files:**
- Modify: `internal/ui/settings/{settings.go,sections.go}`
- Test: `internal/ui/settings/settings_test.go`(package `settings_test` — `app` 이 `settings` 를 import 하고 시험은 하네스를 쓰므로)

**Interfaces:**
- Consumes: `api.GetSettings/UpdateSettings/ListBlocks/Unblock/ListSessions/RevokeSession/ChangePassword`, `config.Save`, `ui.SetThemeMsg`, `widgets.List/Confirm`, `Store.Blocks`
- Produces: `init()` 에서 `ui.RegisterRoute(ui.RouteSettings, …)`; `Route.Anchor` = 섹션 id(`theme|layout|display|notify|privacy|focus|blocks|sessions|password|logout`), 캐시 재사용 시 `RouteUpdatedMsg` 로 섹션 이동.

- [ ] **Step 1: 실패 시험 작성**

```go
func TestSettingsGolden(t *testing.T) { uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) { h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteSettings}}); h.Golden("settings") }) }
func TestSettingsSectionGoldens(t *testing.T) // Required sizes x {"blocks" (2 entries via apitest), "sessions" (current + 1 other), "notify"}
func TestThemeRowPreviewAndApply(t *testing.T) // theme row shows 4 swatches A–D with labels; "→" selects cream => ctx.Theme cream, config saved
func TestLocalToggles(t *testing.T)   // mouse off => View().MouseMode None & config saved; animations off; images "off" => Caps.Graphics None; paint_background off => View().BackgroundColor nil
func TestServerSettings(t *testing.T) // toggle 소셜 알림 => PATCH /v1/me/settings {"notify":{"social":true}}; DM 정책 => {"dm_policy":"friends_only"}; 비공개 계정 => {"is_private":true}; 집중 기본 30 => {"focus_default_min":30}; 400 => inline error, value reverted
func TestBlocksSection(t *testing.T)  // Anchor "blocks" opens there; enter on "@hana" => Confirm "차단을 풀까요?" => y => DELETE /v1/blocks/@hana, row removed; block.updated{blocked:true} event => list refetched
func TestSessionsSection(t *testing.T) // current session marked "현재 기기"; enter on other => Confirm => DELETE /v1/auth/sessions/{id}; current not revocable
func TestChangePassword(t *testing.T)  // current/new/confirm; mismatch => "비밀번호가 서로 달라요"; 401 invalid_credentials => "현재 비밀번호가 맞지 않아요" AND still logged in (credentials file kept, no LoggedOutMsg, Nav not on login — Task 8 hook fires only for code "unauthorized"); 204 => toast "비밀번호를 바꿨어요"
func TestLogoutRow(t *testing.T)       // enter => same Confirm as palette logout
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./internal/ui/settings/ -count=1` · Expected: FAIL

- [ ] **Step 3: 구현** — 결정: 넓으면(`W ≥ 72`) 왼쪽 섹션 목록(폭 22) + 오른쪽 상세, 좁으면 한 열(섹션 머리 + 행). 섹션 제목: 테마, 레이아웃, 화면, 알림, 개인정보, 집중 모드, 차단 목록, 기기·세션, 비밀번호, 로그아웃. 행 조작: `j/k` 이동, `enter`/`space` 토글·열기, `←/→` 값 순환(테마·고정·이미지·DM 정책), 숫자 칸은 Task 28 과 같은 규칙의 textinput(1..720 검증, `분`), 비밀번호 칸은 `EchoPassword`·정규화 없음. 서버 설정은 `UpdateSettingsRequest` 의 해당 `Opt` 하나만 채워 보내고 응답으로 `Store.Settings` 갱신, 실패하면 되돌리고 `api.Describe` 를 행 아래 표시. 화면을 열 때 `GetSettings`·`ListBlocks`·`ListSessions` 를 `ui.Do`(태그 `core.settings.*`). 로컬 설정 변경은 `config.Save` 후 즉시 반영(`images` 는 `termcaps.Detect(os.Environ(), v)` 로 `ctx.Caps` 갱신하되 실행 중 알게 된 `UnicodeCore`·`FocusReports` 는 유지 — Task 25 Context 초기화). 비밀번호 401 문구는 이 화면에 한해 `현재 비밀번호가 맞지 않아요`(`invalid_credentials` 는 `OnUnauthorized` 를 부르지 않으므로 로그아웃되지 않는다 — Task 8). 숫자·비밀번호 칸의 `ctrl+c` 는 Task 22 규칙(값이 있으면 지우고 handled, 비어 있으면 미처리).

- [ ] **Step 4: 통과 확인** — Run: `go test ./internal/ui/settings/ -update -run 'TestSettingsGolden|TestSettingsSectionGoldens' && go test ./internal/ui/settings/ -count=1` · Expected: PASS

- [ ] **Step 5: 커밋** — `git add internal/ui/settings && git commit -m "feat(core): settings screen with local, server, blocks, sessions and password sections"`

---

### Task 35: `cmd/masil` — 진입점과 core 명령

**Files:**
- Modify: `cmd/masil/main.go`, `cmd/masil/cmd_core.go`
- Test: `cmd/masil/main_test.go`

**Interfaces:**
- Consumes: `config`, `auth`, `api`, `app.Run`, `version`
- Produces(CA 2.4): `command`, `Env`, `register`, `main`; core 명령 `tui`(기본), `login`, `signup`, `logout`, `join <code>`, `version`, `config path`, `help`; 종료 코드 규칙(CR-D — `errUsage`·`usageErr` 는 Task 2 에서 이미 실제 구현, 여기서 `run` 의 매핑을 만든다). 비공개 결정: `func run(ctx context.Context, args []string, env *Env) int`(main 이 `os.Exit(run(...))`), `var runTUI = app.Run`(시험에서 대체), `func openLog(p config.Paths, name string) (*slog.Logger, func(), error)`(`logs/<name>.log`, 5MB 넘으면 시작 시 `.1` 로 한 번 회전, 0600).

- [ ] **Step 1: 실패 시험 작성**

```go
func TestHelpListsVisibleCommands(t *testing.T) // run(["help"]) => 0; stdout lists tui, login, signup, logout, join, version, config with Korean usage; a registered Hidden test command not listed
func TestUnknownCommand(t *testing.T)           // run(["nope"]) => 2; stderr "알 수 없는 명령이에요: nope" + "masil help"
func TestVersion(t *testing.T)                  // Version="0.1.0", Commit="abc", Date="2026-11-05" => stdout "masil 0.1.0 (abc, 2026-11-05)\n"
func TestConfigPath(t *testing.T)               // MASIL_CONFIG_DIR=d => stdout d+"\n"; "config" alone => 2 (usage)
func TestDefaultRunsTUI(t *testing.T)           // runTUI fake records Options: no args and "tui" => called; Credentials loaded when credentials.server matches ServerURL, nil when it differs (token not sent to another server)
func TestJoin(t *testing.T)                     // "join AbC12345" => InitialRoute {invite, ID:"AbC12345"}; "join" => 2
func TestLoginPasswordStdin(t *testing.T) {
	// MASIL_SERVER=apitest; run(["login","--handle","Sam","--password-stdin"]) with stdin "secret pw\n" => POST /v1/auth/login {"handle":"sam","password":"secret pw","client":"tui",…};
	// credentials saved; stdout "로그인했어요: @sam"; 401 => exit 1, stderr "아이디 또는 비밀번호가 맞지 않아요"; stdout/stderr never contain the token
}
func TestLoginInteractiveOpensTUI(t *testing.T) // "login" (no flags) => runTUI with Credentials nil and InitialRoute {login, Arg:"login"}; "signup" => Arg "signup"
func TestLogout(t *testing.T)                   // saved creds => POST /v1/auth/logout recorded, credentials removed, stdout "로그아웃했어요"; server unreachable => still removed, exit 0, stderr warning; no creds => "로그인돼 있지 않아요", exit 0
func TestRegisterDuplicatePanics(t *testing.T)
func TestExitCodes(t *testing.T) { // CA 2.4 0/1/2 for commands owned by ANY plan (package-main marker, CR-D)
	// register Hidden test commands once (unique names "zz-ok", "zz-fail", "zz-usage", "zz-wrapped"):
	//   zz-ok => nil => 0; zz-fail => errors.New("boom") => 1; zz-usage => usageErr(nil) => 2;
	//   zz-wrapped => fmt.Errorf("admin: %w", usageErr(errors.New("admincli: usage"))) (shape of cmd_admin wrapping admincli.ErrUsage) => 2
	// run never re-prints a returned error: stderr is exactly what the command itself wrote (zz-fail writes nothing => stderr "")
	// core's own usage errors => 2: "config" (no sub), "config nope", "join" (no code), "login --handle" (missing value), "nope" (unknown command)
}
func TestOpenLogRotates(t *testing.T)           // existing masil.log of 5MB+1 => renamed masil.log.1, new file empty, mode 0600
```

- [ ] **Step 2: 실패 확인** — Run: `go test ./cmd/masil/ -count=1` · Expected: FAIL

- [ ] **Step 3: 구현** — `main` = `ctx := signal.NotifyContext(Interrupt)` → `config.ResolvePaths` → `EnsureDir` → `config.Load`(`ErrPartial` 이면 stderr 경고 한 줄 후 계속) → `openLog` → `run`. 사용법(Usage) 문구: tui `masil [tui] — 터미널 마실을 열어요`, login `masil login [--handle <아이디> --password-stdin] — 로그인해요`, signup `masil signup — 가입해요`, logout `masil logout — 이 기기에서 로그아웃해요`, join `masil join <초대 코드> — 초대받은 방에 들어가요`, version `masil version — 버전을 보여 줘요`, config `masil config path — 설정 폴더 위치를 보여 줘요`, help `masil help — 도움말`. daemon·admin 명령은 이 계획이 만들지 않는다 — 그 파일이 없으면 `masil daemon` 은 "알 수 없는 명령"(2).
  - **종료 코드(CA 2.4, CR-D)**: `run` 은 명령의 `Run` 이 돌려준 오류를 `nil` → 0, `errors.Is(err, errUsage)` → 2, 그 밖 → 1 로 바꾼다(감싼 오류도 `errors.Is` 로 판정 — 그래서 `fmt.Errorf("…: %w", usageErr(x))` 도 2). 알 수 없는 명령·core 명령의 인자 오류(하위 명령 없음, 필수 인자 없음, `flag.NewFlagSet(…, flag.ContinueOnError)` 파싱 오류)는 core 명령이 stderr 에 사용법 줄을 쓴 뒤 `usageErr(err)` 로 돌려준다. `-h`/`--help`(`flag.ErrHelp`)는 사용법을 stdout 에 쓰고 0. **사용자에게 보일 문구는 명령이 스스로 `env.Stderr` 에 쓴다** — `run` 은 돌려받은 오류를 다시 출력하지 않고 로그 파일에만 `slog.Error("command failed", "cmd", name, "err", err)`(daemon·admin 명령이 이미 stderr 에 쓰는 문구와 겹치지 않게).
  - **다른 계획의 `package main` 파일이 따를 규칙**(core 는 `admincli`·`daemon` 을 import 하지 않는다 — CA 2.2): 사용법 오류는 `usageErr(…)` 로 감싸 돌려준다. `cmd_admin.go`(plan 05 Task 20): `if errors.Is(err, admincli.ErrUsage) { return usageErr(err) }`. `cmd_daemon.go`(plan 06): 인자 없음·모르는 하위 명령에서 사용법을 쓴 뒤 `usageErr(nil)`(또는 감싼 원인). 실행 파일 교체(`ErrRestartRequired`)처럼 1 이어야 하는 오류는 감싸지 않는다. 이 규칙은 오케스트레이터가 두 계획에 전달한다(이 계획은 그 파일을 고치지 않는다).

- [ ] **Step 4: 통과 확인** — Run: `go test ./cmd/masil/ -count=1 -v && make build && ./bin/masil version` · Expected: PASS 그리고 `masil 0.0.0-dev (, )`

- [ ] **Step 5: 커밋** — `git add cmd/masil && git commit -m "feat(core): masil entrypoint with command registry and core commands"`

---

### Task 36: `ui/e2e` — 전체 제품 골든·무결성 스윕 (chat·sns-fun 완료 후 마지막)

**Files:**
- Create: `internal/ui/e2e/main_test.go`, `internal/ui/e2e/e2e_test.go`, `internal/ui/e2e/sweep_test.go`

**Interfaces:**
- Consumes: `uitest`, `fixtures`(+`Hostile`), chat·sns-fun 패키지의 blank import(`cmd/masil/register_*.go` 와 같은 세 개: `internal/ui/chat`, `internal/ui/feed`, `internal/ui/fun`). `uitest/stub` 은 import 하지 않는다.
- Produces: CA 10.5 시험들.

- [ ] **Step 1: 시험 작성**

```go
// main_test.go
import (
	_ "github.com/patrache/masil/internal/ui/chat"
	_ "github.com/patrache/masil/internal/ui/feed"
	_ "github.com/patrache/masil/internal/ui/fun"
)
// e2e_test.go
func TestProductGoldens(t *testing.T) {
	for name, nav := range map[string]func(h *uitest.Harness){
		"home": func(*uitest.Harness) {}, "dev": openDev, "dev_thread": openDevThread /* select 민지 message, enter */,
		"feed": func(h *uitest.Harness) { h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteFeed, Arg: "following"}}) },
		"notifications": func(h *uitest.Harness) { h.Press("ctrl+n") },
	} { uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) { nav(h); h.Golden(name) }) }
}
func TestModeSwitchPreservationReal(t *testing.T)    // Task 27 scenario on the real chat channel screen (draft, selection, anchor, same instance via h.Instance identity)
func TestCompactThreadToCommandReal(t *testing.T)    // CA 10.3 bullet 1 with real screens
func TestWidgetFeedToCommandReal(t *testing.T)       // CA 10.3 bullet 2 with real screens
func TestRandomResizeReal(t *testing.T)              // seeds 1,2,3; 200 random sizes; h.Check() each
// sweep_test.go (no goldens, h.Check() only; fixtures + Hostile seeded)
func TestSweepWidths(t *testing.T)   // widths 1..260 x heights {6,15,16,23,24,39,40,49,50,60}; per size states home, dev, dev+thread, feed surface, notifications, palette open; one harness reused (Resize only)
func TestSweepHeights(t *testing.T)  // heights 1..90 x widths {20,43,44,99,100,159,160,199,200}; same states
func TestHostileScreens(t *testing.T) // hostile channel, hostile post detail, hostile profile x AllSizes => h.Check()
// testing.Short(): sample widths/heights every 3rd value; full sweep without -short
```

- [ ] **Step 2: 실행** — Run: `go test ./internal/ui/e2e/ -update -run 'TestProductGoldens' && go test -short ./internal/ui/e2e/ -count=1` · Expected: PASS, `-short` 전체가 한 OS 에서 2분 이내(`go test -short -json` 의 Elapsed 로 확인). 실패하면 원인 화면의 소유 계획(chat/sns-fun)에 보고하고 core 쪽 원인(셸·오버레이·폭)만 이 Task 에서 고친다.

- [ ] **Step 3: 전체 검증** — Run: `make lint && make test && make cross` · Expected: 모두 성공(`go vet` 경고 없음, gofmt 목록 없음, 세 OS 빌드 성공)

Run: `make -s golden-pkgs` · Expected: 목록에 `internal/theme`, `internal/markup`, `internal/ui/layout`, `internal/ui/widgets`, `internal/ui/uitest`, `internal/ui/app`, `internal/ui/authui`, `internal/ui/settings`, `internal/ui/e2e`(그리고 골든을 쓰는 chat·sns-fun 패키지)가 있고, `internal/ui/uitest/fixtures`, `internal/ui/uitest/stub`, `internal/api`, `internal/store` 는 **없다**. 이어서 `make golden && git status --porcelain -- '*.golden'` · Expected: `make golden` 성공(어떤 패키지도 `flag provided but not defined` 로 실패하지 않음), 골든 변경 없음.

- [ ] **Step 4: 커밋** — `git add internal/ui/e2e && git commit -m "test(core): end-to-end product goldens and integrity sweeps"`
