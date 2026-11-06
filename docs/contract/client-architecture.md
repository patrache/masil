# masil Go 클라이언트 아키텍처 계약 v1

- 상태: 확정 계약 v1 (2026-11-05). 상위 출처: 설계서 `docs/superpowers/specs/2026-11-05-masil-design.md`(충돌 시 설계서 우선). 서버 계약: `api.md`, `events.md`, `data-model.md`(이 문서가 그 내용을 바꾸지 않는다).
- 이 문서는 Go 클라이언트를 만드는 **네 계획**이 병렬로 지켜야 할 단일 출처다.
  - **core** — 기반(설정·인증·API·실시간·상태·테마·레이아웃·UI 프레임워크·팔레트·알림 추상화·테스트 하네스·로그인/설정 화면·`masil` 진입점)
  - **chat** — 채팅 화면 전부(방 레일·채널/DM 목록·대화·스레드·멤버·검색·방/초대 관리)와 채팅 상태 저장소
  - **sns-fun** — 피드·글·프로필·스토리·해시태그/트렌드·알림함·방명록·상태 메시지·집중 모드 UI·이미지 렌더링·`masil admin`(운영자 CLI, 범위 지시서 재배정)
  - **daemon-release** — `masil daemon`, OS 자동 시작 등록, goreleaser·설치 스크립트·Homebrew tap
- 표기: **[결정]** = 이 문서가 정한 것(설계서가 빌더 재량으로 남긴 부분). **[검증]** = 2026-11-05 에 실제 모듈을 내려받아 `go doc`·소스·시험 코드로 확인한 사실.
- 문서 언어는 한국어, 코드 식별자·JSON 필드·경로·주석은 영어.

---

## 0. 읽는 순서와 규칙

1. 1장(모듈·의존성)과 2장(패키지 지도·소유권)을 먼저 읽는다. **자기 계획이 소유하지 않은 파일은 절대 만들거나 고치지 않는다.**
2. 다른 계획의 패키지를 쓸 때는 이 문서에 적힌 **공개 시그니처만** 쓴다. 문서에 없는 것을 기대하지 않는다.
3. 시그니처를 바꿔야 하면 코드를 고치기 전에 이 문서를 먼저 고친다(오너 승인 사항). 계획 안에서 임의로 바꾸지 않는다.
4. `go.mod`/`go.sum` 은 core 만 고친다. 다른 계획은 `go get`·`go mod tidy` 로 파일을 바꾸지 않는다(1.3).
5. 비밀(토큰 등)을 코드·테스트 골든·로그에 남기지 않는다. 로그에 토큰이 들어갈 자리에는 `msl_…(redacted)` 만 쓴다.

---

## 1. 모듈·의존성

### 1.1 모듈

```
module github.com/patrache/masil

go 1.26
```

- 로컬 툴체인은 go1.26.1 **[검증]**. `go mod tidy` 는 `go 1.26.0` 으로 적을 수 있으며 그대로 둔다.
- `toolchain` 지시어는 넣지 않는다.

### 1.2 직접 의존성 (정확한 버전)

scratch 모듈에서 아래 목록으로 `go mod tidy` 후 `GOOS=darwin|linux|windows go build ./...` 가 모두 통과함 **[검증]**.

```
require (
	charm.land/bubbles/v2 v2.2.1
	charm.land/bubbletea/v2 v2.1.1
	charm.land/lipgloss/v2 v2.0.6
	git.sr.ht/~jackmordaunt/go-toast v1.1.2
	github.com/BurntSushi/toml v1.6.0
	github.com/alecthomas/chroma/v2 v2.27.0
	github.com/charmbracelet/x/ansi v0.11.9
	github.com/charmbracelet/x/exp/golden v0.1.0
	github.com/charmbracelet/x/exp/teatest/v2 v2.0.0-20261101151554-27ecf3e4d454
	github.com/coder/websocket v1.8.15
	github.com/godbus/dbus/v5 v5.1.0
	github.com/gofrs/flock v0.13.1
	github.com/oklog/ulid/v2 v2.1.1
	github.com/sahilm/fuzzy v0.1.3
	go.yaml.in/yaml/v3 v3.0.5
	golang.org/x/image v0.47.0
	golang.org/x/sys v0.49.0
	golang.org/x/text v0.43.0
)
```

용도:

| 모듈 | 쓰는 곳 |
|---|---|
| bubbletea/v2, bubbles/v2, lipgloss/v2 | TUI 전부 |
| x/ansi (+ `ansi/kitty`, `ansi/iterm2`) | 폭 계산·자르기·줄바꿈(`textwidth`), kitty/iTerm2 이미지 이스케이프 |
| chroma/v2 | 코드 블록 문법 강조(`markup`) |
| coder/websocket | 실시간 소켓(`realtime`) |
| godbus/dbus/v5 | Linux 데스크톱 알림 — `org.freedesktop.Notifications.Notify` 직접 호출(`notify`) |
| go-toast (`wintoast` 만) | Windows 토스트 — WinRT COM 으로 직접 표시(`notify`), PowerShell 경로는 쓰지 않음 |
| gofrs/flock | 파일 잠금(`instance`, `notify` 기록 파일) |
| oklog/ulid/v2 | `device_id`, `client_nonce` 생성(`ids`) |
| BurntSushi/toml | `config.toml` |
| sahilm/fuzzy | 명령 팔레트 퍼지 검색 |
| go.yaml.in/yaml/v3 | **시험 전용** — `cmd/masil/workflow_pins_test.go` 가 `.github` 워크플로우를 YAML 로 읽어 모든 `uses` 키(따옴표·`? uses` 복합 키·이스케이프 표기 포함)가 SHA 로 고정됐는지 검사(보류 항목 2). 배포 바이너리에는 링크되지 않는다. 이미 gofrs/flock 의 간접 의존으로 go.sum 에 있던 버전 |
| x/image (`webp`, `draw`) | WebP 디코드, 이미지 축소 |
| x/sys (`windows/registry`) | Windows 자동 시작 등록(`autostart`) |
| x/text (`unicode/norm`) | 입력 NFC 정규화(`textwidth.Sanitize`, 8.2 규칙 6) |
| x/exp/golden, x/exp/teatest/v2 | 골든 테스트 |

**브리프 목록과 다른 점 [결정]**:
- `github.com/charmbracelet/glamour` 는 **쓰지 않는다**. v1.0.0 은 lipgloss **v1** 에 의존하고 **[검증]**, glamour 는 제목·목록·표까지 렌더하는데 `api.md` 2.1 은 "그 외 마크다운은 일반 텍스트로 보인다" 고 정한다. 본문 마크다운 부분집합은 `internal/markup` 이 직접 파싱하고, 코드 강조만 chroma 를 쓴다. (참고: `charm.land/glamour/v2 v2.0.1` 이 존재하고 lipgloss v2 를 쓴다 **[검증]** — 나중에 필요하면 이 경로로 추가한다.)
- `github.com/mattn/go-runewidth` 는 직접 쓰지 않는다. lipgloss v2 가 `x/ansi` 의 그래핌 폭을 쓰므로 폭 계산을 `x/ansi` 하나로 통일한다(8장). go-runewidth 는 간접 의존으로만 남는다.
- teatest v2 는 태그가 없어 의사 버전(2026-11-01 커밋)을 고정한다. 이 커밋의 go.mod 는 `charm.land/bubbletea/v2` 를 가져온다 **[검증]**.
- golden 은 태그 `v0.1.0` 을 쓴다(teatest 가 요구하는 의사 버전보다 높음, 최소 버전 선택으로 이것이 채택됨) **[검증]**.
- `github.com/gen2brain/beeep` 는 **쓰지 않는다 [보안 결정, 2026-11-06]**. beeep 는 알림 제목·본문(다른 사용자가 보낸 텍스트)을 macOS 에선 AppleScript 문자열(`%q`)에, Windows 에선 go-toast 의 CDATA 템플릿과 PowerShell here-string 폴백 스크립트에 끼워 넣는다 → `]]>`·`"@` 등으로 명령 실행이 가능하다. OS 알림은 4.6 의 "OS 백엔드 규칙" 대로 `notify` 가 직접 한다.

### 1.3 go.mod 관리 규칙

- core 의 첫 작업(Phase 0)에서 `go.mod`·`go.sum` 과 함께 `internal/deps/deps.go` 를 만든다:

```go
//go:build tools

// Package deps pins every direct dependency so `go mod tidy` never drops
// a module that another plan's package will import later.
package deps

import (
	_ "charm.land/bubbles/v2/key"
	_ "charm.land/bubbles/v2/spinner"
	_ "charm.land/bubbles/v2/textarea"
	_ "charm.land/bubbles/v2/textinput"
	_ "charm.land/bubbles/v2/viewport"
	_ "charm.land/bubbletea/v2"
	_ "charm.land/lipgloss/v2"
	_ "git.sr.ht/~jackmordaunt/go-toast/wintoast"
	_ "github.com/BurntSushi/toml"
	_ "github.com/alecthomas/chroma/v2"
	_ "github.com/alecthomas/chroma/v2/lexers"
	_ "github.com/charmbracelet/x/ansi"
	_ "github.com/charmbracelet/x/ansi/iterm2"
	_ "github.com/charmbracelet/x/ansi/kitty"
	_ "github.com/charmbracelet/x/exp/golden"
	_ "github.com/charmbracelet/x/exp/teatest/v2"
	_ "github.com/coder/websocket"
	_ "github.com/godbus/dbus/v5"
	_ "github.com/gofrs/flock"
	_ "github.com/oklog/ulid/v2"
	_ "github.com/sahilm/fuzzy"
	_ "go.yaml.in/yaml/v3"
	_ "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	_ "golang.org/x/sys/windows/registry"
	_ "golang.org/x/text/unicode/norm"
)
```

- `go mod tidy` 는 빌드 태그가 붙은 파일도 고려하므로 이 파일이 있으면 위 목록이 유지된다.
- chat·sns-fun·daemon-release 는 위 모듈의 **어떤 하위 패키지든** import 해도 된다(같은 모듈이므로 go.sum 이 이미 덮는다). 목록에 없는 모듈이 필요하면 이 문서 1.2 를 먼저 고친다.
- 표준 라이브러리 외 추가 금지 목록: 다른 TUI 프레임워크, 다른 웹소켓 라이브러리, cgo 가 필요한 라이브러리(goreleaser 크로스 빌드가 `CGO_ENABLED=0`).

### 1.4 v2 API 확인 사항 [검증]

기억이 아니라 아래 확인된 사실로 코드를 쓴다.

**Bubble Tea v2 (`charm.land/bubbletea/v2`, import 이름 `tea`)**
- `type Model interface { Init() Cmd; Update(Msg) (Model, Cmd); View() View }` — `View()` 는 문자열이 아니라 `tea.View` 구조체. `tea.NewView(s)` 로 만든다.
- 프로그램 옵션이던 것들이 `tea.View` 필드로 옮겨 왔다: `AltScreen bool`, `MouseMode tea.MouseMode`(`MouseModeNone|MouseModeCellMotion|MouseModeAllMotion`), `ReportFocus bool`, `Cursor *tea.Cursor`, `BackgroundColor`, `ForegroundColor color.Color`, `WindowTitle string`, `KeyboardEnhancements`, `DisableBracketedPasteMode`, `OnMouse func(tea.MouseMsg) tea.Cmd`.
- 키: `tea.KeyPressMsg`(=`tea.Key`), `msg.String()` 은 `"ctrl+k"`, `"shift+enter"`, `"enter"`, `"j"`, 한글이면 `"한"` 같은 완성 음절. `msg.Text` 는 입력 문자, `msg.Code` 는 키 코드, `msg.Mod` 는 수식키. 키 이벤트 인터페이스는 `tea.KeyMsg`(누름·뗌 둘 다). teatest 로 한글 문자열을 `Type("한글 입력")` 하면 음절마다 `KeyPressMsg` 가 오고 textarea 에 그대로 들어간다(시험 확인).
- 마우스: `tea.MouseClickMsg`, `tea.MouseReleaseMsg`, `tea.MouseWheelMsg`, `tea.MouseMotionMsg`(공통 인터페이스 `tea.MouseMsg`, `msg.Mouse()` → `tea.Mouse{X, Y, Button, Mod}`, 0 기준 좌표). 버튼 상수 `tea.MouseLeft`, `tea.MouseRight`, `tea.MouseWheelUp`, `tea.MouseWheelDown`.
- 붙여넣기 `tea.PasteMsg{Content}`, 포커스 `tea.FocusMsg`/`tea.BlurMsg`(`View.ReportFocus=true` 필요), 크기 `tea.WindowSizeMsg{Width, Height}`, 모드 보고 `tea.ModeReportMsg{Mode ansi.Mode, Value ansi.ModeSetting}`.
- `tea.Raw(s)` 는 문자열을 그대로 터미널에 쓴다(kitty 이미지 전송에 사용). `(*tea.Program).Send(msg)` 는 외부 고루틴에서 메시지를 넣는다(실행 전이면 블록, 종료 후면 무시).
- 렌더러는 처음에 wcwidth 방식으로 폭을 세고, 터미널이 유니코드 코어 모드(2027)를 지원한다고 답하면 그래핌 방식으로 바꾼다(`tea.go` 의 `ModeReportMsg` 처리). 즉 **터미널마다 폭 계산 방식이 다를 수 있다** → 8장의 정규화가 필수.

**Lip Gloss v2 (`charm.land/lipgloss/v2`)**
- `Style.Width(n)`/`Height(n)` 은 **테두리·패딩을 포함한 상자 전체 크기**다. 시험: `Border(DoubleBorder()).Width(40).Height(9)` → 바깥 폭 40, 높이 9.
- 테두리: `RoundedBorder()`, `NormalBorder()`, `DoubleBorder()`, `ThickBorder()`, `HiddenBorder()` 등. `BorderForeground(c ...color.Color)`.
- `lipgloss.Width(s)` 는 `ansi.StringWidth`(그래핌 폭)를 쓴다.
- 색: `lipgloss.Color("#fabd2f")` 는 `color.Color` 를 돌려준다. `lipgloss.Blend1D(steps, stops...)`, `Lighten`, `Darken`, `Alpha`.
- 겹치기: `lipgloss.NewLayer(content).X(x).Y(y).Z(z).ID(id)`, `lipgloss.NewCompositor(layers...).Render()`, `Compositor.Hit(x, y) LayerHit` — 팔레트·대화상자 오버레이에 쓴다.
- `Style.Hyperlink(url)` 로 OSC 8 링크.

**Bubbles v2 (`charm.land/bubbles/v2/...`)**
- `textarea.New()`; `SetVirtualCursor(false)` 후 `Cursor() *tea.Cursor` 로 **실제 터미널 커서**를 얻는다(textarea 기준 좌표 → 앱이 창 위치만큼 더한다). 한글 IME 조합창이 커서 위치에 뜨므로 입력창은 반드시 실제 커서를 쓴다.
- textarea 기본값은 줄 번호와 `┃` 프롬프트를 그린다 → 입력창은 `ShowLineNumbers=false`, `Prompt=""` 로 바꾼다(시험에서 확인).
- `key.NewBinding(key.WithKeys(...), key.WithHelp(k, desc))`, `key.Matches(msg, bindings...)`.
- `viewport`, `spinner`, `textinput` 도 v2 경로로 존재.

**teatest v2 / golden**
- `teatest.NewTestModel(tb, model, teatest.WithInitialTermSize(w, h))`, `tm.Send`, `tm.Type`, `tm.FinalModel(tb, teatest.WithFinalTimeout(d))`, `tm.Output()` 은 원시 이스케이프 스트림.
- `golden.RequireEqual(tb, out)` 은 `testdata/<tb.Name()>.golden` 과 비교, `go test ... -update` 로 갱신.

**coder/websocket**: `websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h})`, `websocket.CloseStatus(err)`.

**x/ansi**: `ansi.StringWidth`(그래핌), `ansi.StringWidthWc`(wcwidth), `ansi.Truncate(s, n, tail)`, `ansi.Wrap(s, n, breakpoints)`, `ansi.Strip(s)`, `kitty.Placeholder`(U+10EEEE), `kitty.Diacritic(i)`, `kitty.EncodeGraphics(w, img, *kitty.Options)`(옵션 `VirtualPlacement`, `ID`, `Columns`, `Rows`, `Quiet` 등).

---

## 2. 패키지 지도와 소유권

### 2.1 디렉터리 트리

```
go.mod, go.sum                         core
Makefile                               core   (test, golden-update, lint, build)
.github/workflows/ci.yml               core   (go vet + go test ./... on 3 OS)
.github/workflows/release.yml          daemon-release
.goreleaser.yaml                       daemon-release
scripts/install.sh, scripts/install.ps1  daemon-release
packaging/homebrew/masil.rb.tmpl       daemon-release

cmd/masil/
  main.go                              core   (command registry, TUI launch)
  cmd_core.go                          core   (login, signup, logout, join, version, config)
  register_chat.go                     chat   (blank import of internal/ui/chat)
  register_sns.go                      sns-fun (blank import of internal/ui/feed, internal/ui/fun)
  cmd_daemon.go                        daemon-release
  cmd_admin.go                         sns-fun

internal/
  deps/                                core   (tools pin, 1.3)
  version/                             core
  ids/                                 core
  config/                              core
  auth/                                core
  api/                                 core   (REST client, every endpoint)
  api/apitest/                         core   (fake HTTP server for tests)
  realtime/                            core   (WebSocket client)
  store/                               core   (shared app state + reducer registry)
  store/chatstore/                     chat
  store/feedstore/                     sns-fun
  notify/                              core   (alert policy, shown-ledger, OS backend)
  instance/                            core   (tui.lock / daemon.lock)
  textwidth/                           core
  markup/                              core   (markdown subset + chroma)
  humanize/                            core   (times, counts in Korean)
  slash/                               core   (slash command parser)
  termcaps/                            core   (terminal capability detection)
  theme/                               core
  imgrender/                           sns-fun
  daemon/                              daemon-release
  autostart/                           daemon-release
  admincli/                            sns-fun
  ui/                                  core   (framework: Component, Context, Route, registry, messages)
  ui/layout/                           core
  ui/keymap/                           core
  ui/palette/                          core
  ui/widgets/                          core
  ui/app/                              core   (root tea.Model, shell, routing, focus, status bar)
  ui/authui/                           core   (login / signup screens)
  ui/settings/                         core   (settings screen)
  ui/uitest/                           core   (harness, sizes, integrity checks)
  ui/uitest/fixtures/                  core   (seed data matching the mockups + hostile set)
  ui/uitest/stub/                      core   (test-only stand-in screens for core tests, 10.2)
  ui/e2e/                              core   (full-product goldens, last task after all plans)
  ui/chat/                             chat
  ui/feed/                             sns-fun
  ui/fun/                              sns-fun (status, focus mode, guestbook dialogs)
```

- 각 패키지의 `testdata/` 는 그 패키지 소유 계획 것이다.
- `server/`, `docs/contract/*` 는 이 네 계획 범위 밖이다(읽기만).

### 2.2 의존 방향 (import 규칙)

```
cmd/masil ─▶ ui/app ─▶ ui, ui/layout, ui/keymap, ui/palette, ui/widgets, ui/authui, ui/settings
ui/chat, ui/feed, ui/fun ─▶ ui, ui/widgets, ui/palette, ui/keymap, store, store/chatstore|feedstore, api, markup, theme, textwidth, humanize, slash, (feed/fun: imgrender)
ui ─▶ store, api, theme, layout, keymap, config, termcaps, realtime(타입만)
store/chatstore, store/feedstore ─▶ store, api, realtime
store ─▶ api, realtime
realtime ─▶ api(타입), ids, version
daemon ─▶ realtime, notify, instance, config, auth, api
notify ─▶ api(타입), config, instance
api ─▶ version
(모두) ─▶ textwidth, humanize 는 어디서든 가능
```

- **금지**: core 패키지가 chat/sns-fun/daemon-release 패키지를 import(예외: `ui/e2e` 테스트와 `cmd/masil/register_*.go`). chat ↔ sns-fun 상호 import 금지(`ui/chat` 이 `ui/feed` 를 부르지 않는다. 화면 이동은 `ui.Route` 로만).
- `ui/app` 은 chat/sns 화면을 **등록 레지스트리**(5.3.6)로만 알고 import 하지 않는다.

### 2.3 실행 순서 (병렬 작성, 단계적 실행)

- 네 계획은 병렬로 **작성**한다. 실행은:
  1. **core Phase 0** — `go.mod`·`deps.go`, 그리고 2.1 의 core 패키지 전부를 **이 문서의 공개 시그니처 그대로 컴파일되는 스텁**(본문 `panic("TODO")` 금지, 0값 반환 + `// TODO(core)` 허용)으로 만든다. 이 시점부터 다른 계획이 import 할 수 있다.
  2. core 나머지 / chat / sns-fun / daemon-release 를 병렬 실행. 각자 자기 패키지만.
  3. core 마지막 작업: `ui/e2e` 전체 골든(10장) — 모든 계획이 끝난 뒤.
- Phase 0 스텁이 생기기 전 다른 계획은 core 패키지를 import 하는 코드를 커밋하지 않는다(로컬 컴파일 불가).

### 2.4 `cmd/masil` 명령 등록

`main` 패키지 안에서 파일별 소유로 나눈다. core `main.go`:

```go
package main

type command struct {
	Name   string   // "daemon", "admin", "login", ...
	Usage  string   // one line, Korean
	Hidden bool     // not listed in `masil help`
	Run    func(ctx context.Context, env *Env, args []string) error
}

type Env struct {
	Paths  config.Paths
	Config *config.Config
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func register(c command) // panics on duplicate Name; call from init()

// Usage-error marker shared by every file of package main (CR-D).
var errUsage = errors.New("usage")
// usageErr marks err as a usage error: errors.Is(usageErr(x), errUsage) && errors.Is(usageErr(x), x); usageErr(nil) == errUsage.
func usageErr(err error) error

func main() // `masil` (no args) or `masil tui` -> app.Run; else dispatch commands; exit code 0/1/2(usage)
```

- **종료 코드(CR-D)**: 명령의 `Run` 이 돌려준 오류를 `nil` → 0, `errors.Is(err, errUsage)` → 2, 그 밖 → 1 로 바꾼다(감싼 오류도 `errors.Is` 로 판정 — `fmt.Errorf("…: %w", usageErr(x))` 도 2). 알 수 없는 명령·인자 오류는 명령이 stderr 에 사용법 줄을 쓴 뒤 `usageErr(err)` 로 돌려준다. `-h`/`--help` 는 사용법을 stdout 에 쓰고 0. 사용자에게 보일 문구는 명령이 스스로 `env.Stderr` 에 쓰고, 디스패처는 돌려받은 오류를 다시 출력하지 않는다(로그 파일에만 기록).
- core 는 `admincli` 를 import 하지 않는다(2.2). 같은 `package main` 인 `cmd_admin.go`(sns-fun)는 `errors.Is(err, admincli.ErrUsage)` 면 `usageErr(err)` 로 감싸고, `cmd_daemon.go`(daemon-release)는 사용법 오류를 `usageErr(…)` 로 돌려준다.

- 명령 소유: core = `tui`(기본), `login`, `signup`, `logout`, `join <code>`, `version`, `config path`, `help`. daemon-release = `daemon run|install|uninstall|status`. sns-fun = `admin ...`.
- `masil join <code>` 은 TUI 를 시작하며 초기 경로를 `ui.Route{Kind: ui.RouteInvite, ID: code}` 로 준다(초대 미리보기 화면은 chat 소유).
- `register_chat.go`: `import _ "github.com/patrache/masil/internal/ui/chat"`. `register_sns.go`: `import _ ".../internal/ui/feed"; import _ ".../internal/ui/fun"`. 이 import 만으로 각 패키지의 `init()` 이 화면·리듀서·팔레트 명령을 등록한다.

---

## 3. 로컬 파일 (`~/.config/masil/`)

### 3.1 위치

- 설정 디렉터리 = 환경변수 `MASIL_CONFIG_DIR` 가 있으면 그 값, 없으면 `os.UserHomeDir()` + `/.config/masil` — **모든 OS 공통**(Windows 도 `%USERPROFILE%\.config\masil`). `XDG_CONFIG_HOME` 은 보지 않는다 **[결정]**(launchd/systemd 로 뜬 데몬과 TUI 가 환경변수 차이로 다른 디렉터리를 보는 사고를 막기 위해).
- 디렉터리 권한 `0700`, 파일 권한 `0600`(Windows 는 권한 비트 무시). 읽을 때 권한이 더 넓으면 고쳐 쓴다(`chmod 0600`).
- 캐시는 `os.UserCacheDir()/masil/`(미디어). 테스트는 `MASIL_CACHE_DIR` 로 바꾼다.
- 모든 쓰기는 **원자적**: 같은 디렉터리의 임시 파일(`.<name>.tmp-<pid>`)에 쓰고 `fsync` 후 `rename`. 헬퍼 `config.WriteFileAtomic`.

### 3.2 파일 목록

| 파일 | 쓰는 쪽 | 형식 |
|---|---|---|
| `config.toml` | TUI(설정 화면), 사람 | 3.3 |
| `credentials.json` | TUI(로그인/로그아웃), `masil login` | 3.4 — TUI·데몬 공유 |
| `device_id` | 처음 실행한 쪽(TUI 또는 데몬) | ULID 26자 한 줄. 없으면 만들고, 있으면 절대 바꾸지 않음 |
| `state.json` | TUI | 3.5 UI 기억(열린 탭 등) |
| `daemon-state.json` | 데몬 | 3.6 |
| `alerts-shown.json` + `alerts-shown.lock` | TUI·데몬 | 3.7 기기 안 중복 방지 기록 |
| `tui.lock` | TUI(공유 잠금 유지) | 3.8 |
| `daemon.lock` | 데몬(배타 잠금 유지) | 3.8 |
| `logs/masil.log`, `logs/daemon.log` | 각 프로세스 | `log/slog` 텍스트, 5MB 넘으면 `.1` 로 한 번 회전 |

### 3.3 `config.toml`

```toml
# server: MASIL_SERVER env > this value > built-in default (config.DefaultServer)
server = "https://masil.example.workers.dev"
theme = "amber"            # charm | midnight | cream | amber

[layout]
pin = "auto"               # auto | command | standard | compact | widget | tiny
command_min_cols = 200
command_min_rows = 50
standard_min_cols = 100    # >= this => standard/compact family, below => widget/tiny
standard_min_rows = 40     # >= this => standard, below => compact
tiny_max_cols = 43         # cols <= this => tiny
tiny_max_rows = 15         # rows <= this => tiny
widget_min_rows = 24       # in widget width range, rows below this => tiny

[ui]
mouse = true
images = "auto"            # auto | kitty | iterm2 | halfblock | off
animations = true
paint_background = true    # set terminal default bg/fg to theme (tea.View.BackgroundColor)
clock = "24h"              # 24h | 12h

[notify]
desktop = true             # local master switch for OS notifications (TUI + daemon)

[keys]                     # optional overrides: action id -> list of keys (6.3)
# "palette.open" = ["ctrl+k"]
```

```go
package config

var DefaultServer = "http://localhost:8787" // release builds override via -ldflags "-X github.com/patrache/masil/internal/config.DefaultServer=https://..."

type Paths struct {
	Dir, Cache string
	Config, Credentials, DeviceID, State, DaemonState string
	AlertsShown, AlertsShownLock, TUILock, DaemonLock string
	LogDir string
}

type Config struct {
	Server string       `toml:"server"`
	Theme  string       `toml:"theme"`
	Layout LayoutConfig `toml:"layout"`
	UI     UIConfig     `toml:"ui"`
	Notify NotifyConfig `toml:"notify"`
	Keys   map[string][]string `toml:"keys"`
}
type LayoutConfig struct {
	Pin             string `toml:"pin"`
	CommandMinCols  int    `toml:"command_min_cols"`
	CommandMinRows  int    `toml:"command_min_rows"`
	StandardMinCols int    `toml:"standard_min_cols"`
	StandardMinRows int    `toml:"standard_min_rows"`
	TinyMaxCols     int    `toml:"tiny_max_cols"`
	TinyMaxRows     int    `toml:"tiny_max_rows"`
	WidgetMinRows   int    `toml:"widget_min_rows"`
}
type UIConfig struct {
	Mouse           bool   `toml:"mouse"`
	Images          string `toml:"images"`
	Animations      bool   `toml:"animations"`
	PaintBackground bool   `toml:"paint_background"`
	Clock           string `toml:"clock"`
}
type NotifyConfig struct{ Desktop bool `toml:"desktop"` }

func ResolvePaths() (Paths, error)              // MASIL_CONFIG_DIR / ~/.config/masil, MASIL_CACHE_DIR / UserCacheDir
func EnsureDir(p Paths) error                   // mkdir 0700 (+ logs/), fix perms
func Defaults() *Config                         // every field filled (values above)
func Load(p Paths) (*Config, error)             // missing file => Defaults(); unknown keys ignored; invalid values => default + warning in returned error wrapper ErrPartial; unreadable / unparsable file => Defaults() marked "not loaded" (+ ErrPartial)
func Save(p Paths, c *Config) error             // atomic, 0600; a "not loaded" Config (or a copy) => ErrNotLoaded, file untouched
func (c *Config) ServerURL() string             // env MASIL_SERVER > c.Server > DefaultServer; trailing "/" trimmed
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error
var ErrPartial = errors.New("config: some values were invalid and replaced by defaults")
var ErrNotLoaded = errors.New("config: config.toml did not load; not overwriting it")
```

- **고장 난 config.toml 보호**: 파일을 읽지 못했거나 TOML 문법 오류로 `Load` 가 `Defaults()` 로 물러선 경우, 그 `Config` 로 `Save` 하면 사용자 파일(서버 주소·키 재정의 등)이 기본값으로 덮인다. 그래서 `Save` 는 `ErrNotLoaded` 로 거절하고 아무것도 쓰지 않는다. 호출자(테마 전환·설정 화면)는 바뀐 값을 이번 실행의 메모리에만 적용하고 `config.toml 을 고친 뒤 다시 해 주세요` 를 보인다(토스트 또는 행 아래 오류). 값 일부만 잘못된 경우(파싱은 됨)는 지금처럼 저장한다.

### 3.4 `credentials.json`

```json
{
  "version": 1,
  "server": "https://masil.example.workers.dev",
  "token": "msl_...",
  "session_id": "01JA7...",
  "user_id": "01JA1...",
  "handle": "minji",
  "saved_at": "2026-11-05T02:03:04.567Z"
}
```

```go
package auth

type Credentials struct {
	Version   int       `json:"version"` // 1
	Server    string    `json:"server"`
	Token     string    `json:"token"`
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	Handle    string    `json:"handle"`
	SavedAt   time.Time `json:"saved_at"`
}

var ErrNoCredentials = errors.New("auth: not logged in")

func Load(p config.Paths) (*Credentials, error)          // ErrNoCredentials if missing/empty token
func Save(p config.Paths, c *Credentials) error          // atomic 0600
func Clear(p config.Paths) error                          // remove file; nil if absent
func (c *Credentials) Matches(server string) bool        // c.Server == server (trailing slash ignored)
func DeviceID(p config.Paths) (string, error)             // read or create (ids.New()), never rewrites an existing valid ULID
func (c *Credentials) Redacted() string                   // "msl_…(redacted)" for logs
```

- 설정의 서버 주소와 `credentials.server` 가 다르면 로그아웃 상태로 본다(토큰을 다른 서버에 보내지 않음).
- `401 unauthorized` 또는 소켓 `4001` 을 받은 **TUI** 는 `auth.Clear` 후 로그인 화면으로 간다. **데몬은 지우지 않고** 파일이 바뀔 때까지 기다린다(9.4).

### 3.5 `state.json` (TUI UI 기억)

```json
{ "version": 1, "user_id": "01JA1...",
  "open_tabs": [{"kind":"channel","id":"01JB..."},{"kind":"channel","id":"01JB2..."},{"kind":"home"}],
  "active_tab": 0, "front": "tab",
  "last_space_id": "01JB0...", "feed_tab": "following", "pinned_mode": "auto" }
```
- `user_id` 가 현재 로그인 사용자와 다르면 무시한다. 형식 소유 core(`ui/app`), 타입 `app.SavedState`.
- `open_tabs` = `NavState.Tabs` 의 **루트 경로만**(스택 위쪽·열린 스레드는 저장하지 않음), `active_tab` = `NavState.Active`, `front` = `"tab"|"feed"|"notifications"`, `feed_tab` = 피드 표면 루트의 `Arg`(5.3.3).

### 3.6 `daemon-state.json`

```json
{ "version": 1, "user_id": "01JA1...", "epoch": "01JC2...", "last_seq": 1041, "saved_at": "2026-11-05T02:03:04.567Z" }
```
- 데몬만 쓴다. 타입 `daemon.State`(daemon-release 소유). 재시작 시 이 값으로 `resume`, 파일이 없거나 `user_id` 가 다르면 `{"use_device_ack": true}`.

### 3.7 `alerts-shown.json` (기기 안 OS 알림 중복 방지 — events.md 7장 필수)

```json
{ "version": 1, "ids": ["01JC9ZA...", "01JC9ZB..."] }
```
- 최근 500개, **오래된 것이 앞**. 넘으면 앞에서 버린다.
- 읽기·확인·추가·쓰기를 `alerts-shown.lock` 의 **배타 flock** 안에서 한 번에 한다(`notify.Ledger.MarkIfNew`). 잠금 대기 최대 2초, 실패하면 "띄우지 않음"이 아니라 "띄움"으로 처리하고 경고 로그(놓치는 것보다 중복이 낫다).
- 파일이 깨졌으면 빈 기록으로 다시 만든다.

### 3.8 `tui.lock` / `daemon.lock`

- 실행 중인 **모든 TUI 프로세스**는 `tui.lock` 에 **공유 잠금**(`flock.RLock`)을 잡고 끝날 때까지 유지한다. 프로세스가 죽으면 OS 가 푼다.
- 데몬은 TUI 존재를 `tui.lock` 에 **배타 TryLock** 을 걸어 본다 — 성공(곧바로 해제) = 이 기기에 TUI 없음, 실패 = TUI 있음.
- 데몬은 `daemon.lock` 에 배타 잠금을 잡아 기기당 하나만 돈다(두 번째는 `instance.ErrAlreadyRunning` 으로 종료 코드 0 + 안내).

```go
package instance

var ErrAlreadyRunning = errors.New("instance: daemon already running")

func AcquireTUI(p config.Paths) (release func(), err error)     // shared lock on tui.lock
func TUIRunning(p config.Paths) (running bool, err error)       // exclusive TryLock probe; err => caller falls back to server signal
func AcquireDaemon(p config.Paths) (release func(), err error)  // exclusive on daemon.lock; ErrAlreadyRunning
```

---

## 4. 기반 패키지 (core)

### 4.1 `internal/version`

```go
package version
var (
	Version = "0.0.0-dev" // -ldflags -X
	Commit  = ""
	Date    = ""
)
func UserAgent() string // "masil/<Version> (<runtime.GOOS>; <runtime.GOARCH>)", e.g. "masil/0.1.0 (darwin; arm64)"
func Semver() string    // Version without "v" prefix and "-dev" suffix stripped for comparisons
```

### 4.2 `internal/ids`

```go
package ids
func New() string             // ULID, monotonic within process, Crockford upper-case, 26 chars (oklog/ulid Make)
func Valid(s string) bool     // 26 chars Crockford base32
func Less(a, b string) bool   // a < b lexicographically (IDs are opaque; ordering only)
```
- 메시지·글 전송의 `client_nonce` 는 `ids.New()`. 재시도 때 같은 값을 다시 쓴다.

### 4.3 `internal/api` — REST 클라이언트

#### 4.3.1 공통

```go
package api

type Client struct{ /* unexported */ }

type Option func(*Client)
func WithToken(token string) Option
func WithHTTPClient(h *http.Client) Option          // default: &http.Client{Timeout: 0}; per-call timeouts via ctx
func WithUserAgent(ua string) Option                 // default version.UserAgent()
func WithOnUnauthorized(fn func()) Option            // called (once per token) on 401 unauthorized
func WithOnUpgradeRequired(fn func(min string)) Option
func WithClock(now func() time.Time) Option          // for rate-limit cooldown tests

func New(baseURL string, opts ...Option) *Client
func (c *Client) BaseURL() string
func (c *Client) SetToken(token string)
func (c *Client) Token() string
func (c *Client) MediaURL(rel string) string         // "/v1/media/<id>" -> absolute
func (c *Client) WebSocketURL(mode, deviceID string) string // ws(s)://.../v1/ws?mode=..&device_id=..
func (c *Client) AuthHeader() http.Header            // Authorization + User-Agent, for realtime dial
func (c *Client) Cooldown(limiter string) time.Time  // zero if not rate-limited

// Error is the decoded error envelope (api.md 0.5).
type Error struct {
	Status     int
	Code       string          // "invalid_request", "rate_limited", ...
	Message    string
	Details    json.RawMessage // raw "details" object
	RequestID  string          // X-Request-Id
	RetryAfter time.Duration   // 429 only
}
func (e *Error) Error() string
func (e *Error) Fields() map[string]string          // details.fields for invalid_request
func (e *Error) Limiter() string                      // details.limiter for rate_limited
func CodeOf(err error) string                          // "" if not *Error; "network" for transport errors; "canceled" for ctx cancel
func IsCode(err error, code string) bool
func Describe(err error) string                        // Korean, user-facing, one line (table per code + fields)

type PageOpts struct {
	Cursor string // "" = first page
	Limit  int    // 0 = server default
}
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}
func (p *Page[T]) Done() bool

// Opt[T] distinguishes "omit" from "send null" (api.md optional+nullable fields).
// Uses Go 1.24+ `omitzero`: fields are tagged `json:"x,omitzero"`.
type Opt[T any] struct {
	Set   bool
	Null  bool
	Value T
}
func Some[T any](v T) Opt[T]
func Null[T any]() Opt[T]
func (o Opt[T]) IsZero() bool                 // !Set
func (o Opt[T]) MarshalJSON() ([]byte, error) // null or value

// UserRef is "{user}" (api.md 0.2): "@handle", "<ULID>", or "@me". Escaped per path segment.
type UserRef string
const Me UserRef = "@me"
func Handle(h string) UserRef // "@" + lowercase(h)
func UserID(id string) UserRef
```

동작 규칙:
- 요청마다 `Authorization: Bearer`(토큰 있을 때), `User-Agent`, `Accept: application/json`. JSON 본문은 `Content-Type: application/json; charset=utf-8`.
- 경로 매개변수는 `url.PathEscape` 로 세그먼트 단위 이스케이프(이모지 리액션, 해시태그, `@handle`).
- 요청 경로(서버가 준 `Media.URL` 포함)에 빈 조각(`//`, 끝의 `/`)·`.`·`..`(퍼센트 인코딩된 `%2e` 꼴 포함)·생 백슬래시가 있으면 보내지 않고 `400 invalid_request` `{"fields":{"path":"invalid"}}` 를 돌려준다(네트워크 요청 0회). WHATWG URL 해석·Cloudflare URL 정규화가 이런 경로를 다른 엔드포인트로 바꾸기 때문이다(예: 초대 코드 `..` → `DELETE /v1/invites/..` 가 `/v1/` 로).
- `FetchMedia` 는 API 출처(토큰을 붙이는 곳)에서는 `<BaseURL 경로>/v1/media/<조각 하나>` 만 가져온다. 다른 경로(`/v1/me`, `/v1/auth/sessions`, `/v1/media/a/b`, 디코딩하면 `/`·`\` 가 생기는 조각)는 보내지 않고 같은 `400 invalid_request` `{"fields":{"path":"invalid"}}` 를 돌려준다 — 악의적인 `Media.URL` 이 토큰을 다른 엔드포인트로 보내지 못하게. 다른 출처는 토큰 없이 그대로 가져온다.
- 응답의 모르는 필드는 무시(`encoding/json` 기본). 2xx 인데 본문이 비어야 하는 엔드포인트(`204`)는 본문을 읽지 않는다.
- **속도 제한**: 엔드포인트→리미터 표(api.md 0.7)를 정적 표로 가진다. `429` 를 받으면 `Cooldown[limiter] = now + retry_after_sec`. 쿨다운 중 같은 리미터 요청은 네트워크 없이 즉시 `&Error{Status:429, Code:"rate_limited", RetryAfter: 남은 시간}` 을 돌려준다. **자동 재시도는 `SendMessage`/`ReplyThread` 만 1회**: `RetryAfter ≤ 10s` 이면 그만큼 기다렸다가 같은 `client_nonce` 로 다시 보낸다(ctx 취소 존중).
- `401 unauthorized` → `OnUnauthorized` 호출(같은 토큰에 대해 1회). `426` → `OnUpgradeRequired(details.min_client_version)`.
- 기본 타임아웃(호출자가 ctx 에 기한을 안 줬을 때): 일반 15초, 업로드·미디어 120초.
- 타입 변환 규칙: `ID`→`string`, `Timestamp`→`time.Time`(nullable 이면 `*time.Time`), nullable 객체/문자열/숫자→포인터, 배열→슬라이스(서버가 `[]` 로 줌), `Record<string,unknown>`→`json.RawMessage`. 필드 이름·JSON 태그는 api.md 2장과 **글자 그대로**(snake_case). Go 타입 이름은 TS 인터페이스 이름 그대로(`UserSummary`, `Channel`, `Message`, `Post` …).

#### 4.3.2 타입 (api.md 2장과 1:1)

다음 Go 타입을 `internal/api/types.go` 에 둔다. 이름·필드는 api.md 2장 그대로이며, 아래는 변환이 기계적이지 않은 것만 명시한다.

`UserStatus, UserSummary, Relationship, FriendRequestRef, Profile, Settings, NotifySettings, Focus, PresenceState, Me, Session, Media, Upload, FriendRequest, Friend, SpacePreview, Space, SpaceDetail, SpaceMember, Invite, InvitePreview, DirectInvite, ChannelMyState, Channel, ChannelRef, UnreadState, MentionRef, ReactionSummary, ThreadSummary, RollResult, SystemEvent, SystemEventData, PollOption, Poll, PollInput, Message, QuotedPost, Post, PostViewer, TimelineItem, Story, StoryGroup, Notification, NotificationSubject, NotificationContext, Alert, AlertTarget, FocusSummary, FocusCounts, GuestbookEntry, SearchHighlight, AdminUserView, Report, AdminAction`

```go
// TS inheritance is flattened by embedding; JSON stays flat.
type Profile struct {
	UserSummary
	Bio            string          `json:"bio"`
	CreatedAt      time.Time       `json:"created_at"`
	FollowerCount  int             `json:"follower_count"`
	FollowingCount int             `json:"following_count"`
	PostCount      int             `json:"post_count"`
	Presence       *string         `json:"presence"`      // PresenceStatus or null
	LastSeenAt     *time.Time      `json:"last_seen_at"`
	Relationship   *Relationship   `json:"relationship"`
}
// Me embeds UserSummary; Space embeds SpacePreview; SpaceDetail embeds Space; Upload embeds Media.

type Relationship struct {
	IsFriend       bool              `json:"is_friend"`
	FriendRequest  *FriendRequestRef `json:"friend_request"` // {id, direction}
	Following      string            `json:"following"`     // none|pending|active
	FollowedBy     string            `json:"followed_by"`
	Blocked        bool              `json:"blocked"`
	CanDM          bool              `json:"can_dm"`
	CanViewPosts   bool              `json:"can_view_posts"`
	SharedSpaceIDs []string          `json:"shared_space_ids"`
}

// QuotedPost is a union discriminated by "available".
type QuotedPost struct {
	Available bool   `json:"available"`
	Post      *Post  `json:"post,omitempty"`   // when available
	ID        string `json:"id,omitempty"`     // when !available
	Reason    string `json:"reason,omitempty"` // deleted | not_visible
}

type Message struct {
	ID           string            `json:"id"`
	ChannelID    string            `json:"channel_id"`
	Author       UserSummary       `json:"author"`
	Kind         string            `json:"kind"` // text|me|roll|poll|system
	Body         string            `json:"body"`
	Mentions     []MentionRef      `json:"mentions"`
	MentionHere  bool              `json:"mention_here"`
	ThreadRootID *string           `json:"thread_root_id"`
	Thread       *ThreadSummary    `json:"thread"`
	Reactions    []ReactionSummary `json:"reactions"`
	Pinned       bool              `json:"pinned"`
	Poll         *Poll             `json:"poll"`
	Roll         *RollResult       `json:"roll"`
	System       *SystemEvent      `json:"system"`
	ClientNonce  *string           `json:"client_nonce"`
	CreatedAt    time.Time         `json:"created_at"`
	EditedAt     *time.Time        `json:"edited_at"`
	Deleted      bool              `json:"deleted"`
}

// String enums are typed constants (values exactly as api.md):
type PresenceStatus string // online|away|dnd|offline
type SpaceRole string      // owner|admin|member
type ChannelKind string    // text|dm|group_dm
type AlertKind string      // dm|mention|thread_reply|invite|friend_request|channel_message|social|focus_summary
type NotificationType string
```

응답 묶음 타입(엔드포인트 전용):

```go
type AuthResult struct{ Token string `json:"token"`; Session Session `json:"session"`; User Me `json:"user"` }
type MetaInfo struct{ APIVersion int; ServerTime time.Time; MinClientVersion string; Limits MetaLimits } // json snake_case
type MetaLimits struct{ MessageMaxChars, PostMaxChars, PostMaxMedia, UploadMaxBytes, GroupDMMaxMembers, StoryTTLSec, PollMaxOptions, ReactionsPerMessage, WatchTopicsMax int }
type PresenceItem struct{ UserID string; Status PresenceStatus; LastSeenAt *time.Time }
type BlockEntry struct{ User UserSummary; CreatedAt time.Time }
type FollowRequestEntry struct{ User UserSummary; CreatedAt time.Time }
type FriendRequestResult struct{ Request FriendRequest; Friend *Friend; AutoAccepted bool } // AutoAccepted = HTTP 200
type ChannelMember struct{ User UserSummary; JoinedAt time.Time }
type SpaceBan struct{ User UserSummary; BannedBy UserSummary; Reason string; CreatedAt time.Time }
type MessageWindow struct{ Items []Message; HasMoreBefore, HasMoreAfter bool }
type ThreadWindow struct{ Root Message; Items []Message; HasMoreBefore, HasMoreAfter bool }
type MessageSearchHit struct{ Message Message; Channel ChannelRef; Highlights []SearchHighlight }
type PostSearchHit struct{ Post Post; Highlights []SearchHighlight }
type HashtagSuggestion struct{ Tag string; PostCount24h int `json:"post_count_24h"` }
type TimelinePage struct{ Items []TimelineItem; NextCursor *string; Gap bool }
type PostContext struct{ Ancestors []Post; Post Post }
type LikeResult struct{ PostID string; Liked bool; LikeCount int }
type RepostResult struct{ PostID string; Reposted bool; RepostCount int }
type Trend struct{ Rank int; Tag string; PostCount, AuthorCount int }
type Trends struct{ Items []Trend; ComputedAt *time.Time }
type StoryViewer struct{ User UserSummary; ViewedAt time.Time }
type NotificationPage struct{ Items []Notification; NextCursor *string; UnreadCount int }
type MediaBlob struct{ Data []byte; ContentType, ETag string; NotModified bool }
```

#### 4.3.3 엔드포인트 메서드 (api.md 5장 전부, 한 엔드포인트 = 한 메서드)

모든 메서드 첫 인자는 `ctx context.Context`. 반환이 없는 `204` 엔드포인트는 `error` 만.

```go
// 4 meta
func (c *Client) Meta(ctx context.Context) (*MetaInfo, error)                        // GET /v1/meta
func (c *Client) Health(ctx context.Context) error                                    // GET /v1/health

// 1.4 auth
type SignupRequest struct{ Handle, Password string; DisplayName, Client, DeviceName string /* omitempty */ }
type LoginRequest struct{ Handle, Password string; Client, DeviceName string /* omitempty */ }
func (c *Client) Signup(ctx context.Context, r SignupRequest) (*AuthResult, error)   // POST /v1/auth/signup
func (c *Client) Login(ctx context.Context, r LoginRequest) (*AuthResult, error)     // POST /v1/auth/login
func (c *Client) Logout(ctx context.Context) error                                    // POST /v1/auth/logout
func (c *Client) ListSessions(ctx context.Context) ([]Session, error)                // GET /v1/auth/sessions
func (c *Client) RevokeSession(ctx context.Context, sessionID string) error          // DELETE /v1/auth/sessions/{id}
func (c *Client) ChangePassword(ctx context.Context, current, next string) error     // POST /v1/auth/password

// 5.1 me
type UpdateProfileRequest struct{ DisplayName Opt[string]; Bio Opt[string]; AvatarUploadID Opt[string] } // json display_name,bio,avatar_upload_id omitzero
type UpdateSettingsRequest struct{ DMPolicy Opt[string]; IsPrivate Opt[bool]; Notify *NotifySettingsPatch; FocusDefaultMin Opt[int] }
type NotifySettingsPatch struct{ DM, Mention, Invite, FriendRequest, ThreadReply, Social Opt[bool] }
type SetStatusRequest struct{ Emoji Opt[string]; Text string; ExpiresInSec Opt[int] }
func (c *Client) GetMe(ctx context.Context) (*Me, error)                              // GET /v1/me
func (c *Client) UpdateProfile(ctx context.Context, r UpdateProfileRequest) (*Me, error)   // PATCH /v1/me/profile
func (c *Client) GetSettings(ctx context.Context) (*Settings, error)                  // GET /v1/me/settings
func (c *Client) UpdateSettings(ctx context.Context, r UpdateSettingsRequest) (*Settings, error) // PATCH /v1/me/settings
func (c *Client) SetStatus(ctx context.Context, r SetStatusRequest) (*UserStatus, error)  // PUT /v1/me/status
func (c *Client) ClearStatus(ctx context.Context) error                               // DELETE /v1/me/status
func (c *Client) SetPresenceMode(ctx context.Context, mode string) (*PresenceState, error) // PUT /v1/me/presence
func (c *Client) GetPresence(ctx context.Context, userIDs []string) ([]PresenceItem, error) // GET /v1/presence (<=100, caller chunks)
func (c *Client) StartFocus(ctx context.Context, durationSec Opt[int]) (*Focus, error) // POST /v1/me/focus
func (c *Client) GetFocus(ctx context.Context) (*Focus, error)                        // GET /v1/me/focus (nil = not focusing)
func (c *Client) EndFocus(ctx context.Context) (*FocusSummary, error)                 // DELETE /v1/me/focus
func (c *Client) ListUnreads(ctx context.Context) ([]UnreadState, error)              // GET /v1/me/unreads

// 5.2 users
func (c *Client) GetUser(ctx context.Context, u UserRef) (*Profile, error)            // GET /v1/users/{user}
func (c *Client) GetUsers(ctx context.Context, ids []string) ([]UserSummary, error)   // GET /v1/users?ids=
func (c *Client) SearchUsers(ctx context.Context, q string, limit int) ([]UserSummary, error) // GET /v1/users/search
func (c *Client) ListUserPosts(ctx context.Context, u UserRef, filter string, p PageOpts) (*Page[TimelineItem], error) // filter posts|replies|media
func (c *Client) ListFollowers(ctx context.Context, u UserRef, p PageOpts) (*Page[UserSummary], error)
func (c *Client) ListFollowing(ctx context.Context, u UserRef, p PageOpts) (*Page[UserSummary], error)

// 5.3 friends
func (c *Client) ListFriends(ctx context.Context, p PageOpts) (*Page[Friend], error)
func (c *Client) RemoveFriend(ctx context.Context, u UserRef) error
func (c *Client) ListFriendRequests(ctx context.Context, direction string) ([]FriendRequest, error) // incoming|outgoing
func (c *Client) SendFriendRequest(ctx context.Context, u UserRef) (*FriendRequestResult, error)
func (c *Client) AcceptFriendRequest(ctx context.Context, requestID string) (*Friend, error)
func (c *Client) DeclineFriendRequest(ctx context.Context, requestID string) error
func (c *Client) CancelFriendRequest(ctx context.Context, requestID string) error

// 5.4 blocks
func (c *Client) ListBlocks(ctx context.Context) ([]BlockEntry, error)
func (c *Client) Block(ctx context.Context, u UserRef) error
func (c *Client) Unblock(ctx context.Context, u UserRef) error

// 5.5 follow
func (c *Client) Follow(ctx context.Context, u UserRef) (state string, err error)     // active|pending
func (c *Client) Unfollow(ctx context.Context, u UserRef) error
func (c *Client) RemoveFollower(ctx context.Context, u UserRef) error
func (c *Client) ListFollowRequests(ctx context.Context, p PageOpts) (*Page[FollowRequestEntry], error)
func (c *Client) AcceptFollowRequest(ctx context.Context, u UserRef) error
func (c *Client) DeclineFollowRequest(ctx context.Context, u UserRef) error

// 5.6 DMs / channel members
type OpenDMRequest struct{ Users []UserRef; Name Opt[string] }
func (c *Client) ListDMs(ctx context.Context, p PageOpts) (*Page[Channel], error)
func (c *Client) OpenDM(ctx context.Context, r OpenDMRequest) (ch *Channel, created bool, err error)
func (c *Client) AddChannelMembers(ctx context.Context, channelID string, users []UserRef) (*Channel, error)
func (c *Client) RemoveChannelMember(ctx context.Context, channelID string, u UserRef) error // u=Me => leave

// 5.7 spaces
type CreateSpaceRequest struct{ Name string; Description, IconEmoji string /* omitempty */ }
type UpdateSpaceRequest struct{ Name, Description Opt[string]; IconEmoji Opt[string] }
type SpaceMembersQuery struct{ Q, Role string; PageOpts }
type RemoveMemberOpts struct{ Ban bool; Reason string }
func (c *Client) ListSpaces(ctx context.Context) ([]Space, error)
func (c *Client) CreateSpace(ctx context.Context, r CreateSpaceRequest) (*SpaceDetail, error)
func (c *Client) GetSpace(ctx context.Context, spaceID string) (*SpaceDetail, error)
func (c *Client) UpdateSpace(ctx context.Context, spaceID string, r UpdateSpaceRequest) (*Space, error)
func (c *Client) DeleteSpace(ctx context.Context, spaceID string) error
func (c *Client) TransferSpace(ctx context.Context, spaceID string, to UserRef) (*Space, error)
func (c *Client) ListSpaceMembers(ctx context.Context, spaceID string, q SpaceMembersQuery) (*Page[SpaceMember], error)
func (c *Client) SetSpaceMemberRole(ctx context.Context, spaceID string, u UserRef, role SpaceRole) (*SpaceMember, error)
func (c *Client) RemoveSpaceMember(ctx context.Context, spaceID string, u UserRef, o RemoveMemberOpts) error // u=Me => leave
func (c *Client) ListSpaceBans(ctx context.Context, spaceID string) ([]SpaceBan, error)
func (c *Client) Unban(ctx context.Context, spaceID string, u UserRef) error

// 5.8 invites
type CreateInviteRequest struct{ MaxUses Opt[int]; ExpiresInSec Opt[int] } // omit ExpiresInSec => server default 7d
func (c *Client) ListInvites(ctx context.Context, spaceID string) ([]Invite, error)
func (c *Client) CreateInvite(ctx context.Context, spaceID string, r CreateInviteRequest) (*Invite, error)
func (c *Client) RevokeInvite(ctx context.Context, code string) error
func (c *Client) GetInvite(ctx context.Context, code string) (*InvitePreview, error)
func (c *Client) AcceptInvite(ctx context.Context, code string) (*SpaceDetail, error)
func (c *Client) SendDirectInvite(ctx context.Context, spaceID string, u UserRef) (*DirectInvite, error)
func (c *Client) ListMyDirectInvites(ctx context.Context) ([]DirectInvite, error)
func (c *Client) AcceptDirectInvite(ctx context.Context, inviteID string) (*SpaceDetail, error)
func (c *Client) DeclineDirectInvite(ctx context.Context, inviteID string) error
func (c *Client) CancelDirectInvite(ctx context.Context, inviteID string) error

// 5.9 channels
type CreateChannelRequest struct{ Name string; Topic string; IsPrivate bool; Members []UserRef } // omitempty
type UpdateChannelRequest struct{ Name, Topic Opt[string]; Position Opt[int] }
type ChannelPrefsRequest struct{ Notify Opt[string]; Muted Opt[bool] }
func (c *Client) ListChannels(ctx context.Context, spaceID string) ([]Channel, error)
func (c *Client) CreateChannel(ctx context.Context, spaceID string, r CreateChannelRequest) (*Channel, error)
func (c *Client) GetChannel(ctx context.Context, channelID string) (*Channel, error)
func (c *Client) UpdateChannel(ctx context.Context, channelID string, r UpdateChannelRequest) (*Channel, error)
func (c *Client) DeleteChannel(ctx context.Context, channelID string) error
func (c *Client) ListChannelMembers(ctx context.Context, channelID string, p PageOpts) (*Page[ChannelMember], error)
func (c *Client) SetChannelPrefs(ctx context.Context, channelID string, r ChannelPrefsRequest) (*ChannelMyState, error)
func (c *Client) MarkRead(ctx context.Context, channelID, messageID string) (*UnreadState, error)

// 5.10 messages
type MessageQuery struct{ Before, After, Around string; Limit int } // at most one anchor
type SendMessageRequest struct{ ClientNonce string; Kind string; Body string; Poll *PollInput }
func (c *Client) ListMessages(ctx context.Context, channelID string, q MessageQuery) (*MessageWindow, error)
func (c *Client) SendMessage(ctx context.Context, channelID string, r SendMessageRequest) (msg *Message, created bool, err error)
func (c *Client) GetMessage(ctx context.Context, messageID string) (*Message, error)
func (c *Client) EditMessage(ctx context.Context, messageID, body string) (*Message, error)
func (c *Client) DeleteMessage(ctx context.Context, messageID string) error

// 5.11 threads
type ThreadQuery struct{ Before, After string; Limit int }
type ThreadReplyRequest struct{ ClientNonce, Kind, Body string }
func (c *Client) GetThread(ctx context.Context, rootID string, q ThreadQuery) (*ThreadWindow, error)
func (c *Client) ReplyThread(ctx context.Context, rootID string, r ThreadReplyRequest) (msg *Message, created bool, err error)

// 5.12 reactions / pins
func (c *Client) AddReaction(ctx context.Context, messageID, emoji string) error
func (c *Client) RemoveReaction(ctx context.Context, messageID, emoji string) error
func (c *Client) ListReactionUsers(ctx context.Context, messageID, emoji string, p PageOpts) (*Page[UserSummary], error)
func (c *Client) Pin(ctx context.Context, messageID string) error
func (c *Client) Unpin(ctx context.Context, messageID string) error
func (c *Client) ListPins(ctx context.Context, channelID string) ([]Message, error)

// 5.13 search
type MessageSearchQuery struct{ Q, ChannelID, SpaceID string; Author UserRef; PageOpts }
type PostSearchQuery struct{ Q string; Author UserRef; PageOpts }
func (c *Client) SearchMessages(ctx context.Context, q MessageSearchQuery) (*Page[MessageSearchHit], error)
func (c *Client) SearchPosts(ctx context.Context, q PostSearchQuery) (*Page[PostSearchHit], error)
func (c *Client) SearchHashtags(ctx context.Context, q string, limit int) ([]HashtagSuggestion, error)

// 5.14 uploads / media
func (c *Client) Upload(ctx context.Context, purpose, contentType string, body io.Reader, size int64) (*Upload, error) // POST /v1/uploads?purpose=
func (c *Client) DeleteUpload(ctx context.Context, uploadID string) error
func (c *Client) FetchMedia(ctx context.Context, mediaURL, ifNoneMatch string) (*MediaBlob, error)            // GET /v1/media/{id}; 304 => NotModified

// 5.15 posts
type CreatePostRequest struct{ ClientNonce, Body string; MediaUploadIDs []string; ReplyToID, QuoteOfID string; Poll *PollInput } // omitempty
func (c *Client) CreatePost(ctx context.Context, r CreatePostRequest) (p *Post, created bool, err error)
func (c *Client) GetPost(ctx context.Context, postID string) (*Post, error)
func (c *Client) GetPostContext(ctx context.Context, postID string) (*PostContext, error)
func (c *Client) ListReplies(ctx context.Context, postID string, p PageOpts) (*Page[Post], error)
func (c *Client) DeletePost(ctx context.Context, postID string) error
func (c *Client) Like(ctx context.Context, postID string) (*LikeResult, error)
func (c *Client) Unlike(ctx context.Context, postID string) (*LikeResult, error)
func (c *Client) ListLikes(ctx context.Context, postID string, p PageOpts) (*Page[UserSummary], error)
func (c *Client) Repost(ctx context.Context, postID string) (*RepostResult, error)
func (c *Client) Unrepost(ctx context.Context, postID string) (*RepostResult, error)
func (c *Client) ListReposts(ctx context.Context, postID string, p PageOpts) (*Page[UserSummary], error)
func (c *Client) ListQuotes(ctx context.Context, postID string, p PageOpts) (*Page[Post], error)

// 5.16 timelines / hashtags / trends
type TimelineQuery struct{ PageOpts; Since string }
func (c *Client) FollowingTimeline(ctx context.Context, q TimelineQuery) (*TimelinePage, error)
func (c *Client) DiscoverTimeline(ctx context.Context, q TimelineQuery) (*TimelinePage, error)
func (c *Client) HashtagPosts(ctx context.Context, tag string, p PageOpts) (*Page[Post], error)
func (c *Client) Trends(ctx context.Context) (*Trends, error)

// 5.17 stories
type CreateStoryRequest struct{ Body, MediaUploadID, BG string } // omitempty
func (c *Client) CreateStory(ctx context.Context, r CreateStoryRequest) (*Story, error)
func (c *Client) StoryFeed(ctx context.Context) ([]StoryGroup, error)
func (c *Client) ListUserStories(ctx context.Context, u UserRef) ([]Story, error)
func (c *Client) ViewStory(ctx context.Context, storyID string) error
func (c *Client) ListStoryViewers(ctx context.Context, storyID string, p PageOpts) (*Page[StoryViewer], error)
func (c *Client) DeleteStory(ctx context.Context, storyID string) error

// 5.18 notifications
type NotificationQuery struct{ PageOpts; UnreadOnly bool }
type MarkNotificationsRead struct{ IDs []string; All bool; Before string } // exactly one set
func (c *Client) ListNotifications(ctx context.Context, q NotificationQuery) (*NotificationPage, error)
func (c *Client) NotificationUnreadCount(ctx context.Context) (int, error)
func (c *Client) MarkNotificationsRead(ctx context.Context, r MarkNotificationsRead) (unread int, err error)

// 5.19 polls
func (c *Client) GetPoll(ctx context.Context, pollID string) (*Poll, error)
func (c *Client) Vote(ctx context.Context, pollID string, optionIdxs []int) (*Poll, error)
func (c *Client) RetractVote(ctx context.Context, pollID string) (*Poll, error)
func (c *Client) ClosePoll(ctx context.Context, pollID string) (*Poll, error)

// 5.20 guestbook
func (c *Client) ListGuestbook(ctx context.Context, owner UserRef, p PageOpts) (*Page[GuestbookEntry], error)
func (c *Client) WriteGuestbook(ctx context.Context, owner UserRef, body string, secret bool) (*GuestbookEntry, error)
func (c *Client) DeleteGuestbookEntry(ctx context.Context, entryID string) error

// 5.21 reports
type ReportRequest struct{ TargetType, TargetID, Reason, Note string }
func (c *Client) Report(ctx context.Context, r ReportRequest) (reportID string, err error)

// 5.22 admin
type AdminReportQuery struct{ Status, TargetType string; PageOpts }
type AdminRemoveRequest struct{ TargetType, TargetID, Reason, ReportID string }
type AdminSuspendRequest struct{ Reason string; DurationSec Opt[int]; ReportID string }
type AdminUserQuery struct{ Q string; Suspended *bool; PageOpts }
type AdminActionQuery struct{ TargetType, TargetID string; PageOpts }
func (c *Client) AdminListReports(ctx context.Context, q AdminReportQuery) (*Page[Report], error)
func (c *Client) AdminGetReport(ctx context.Context, reportID string) (*Report, error)
func (c *Client) AdminResolveReport(ctx context.Context, reportID, resolution, note string) (*Report, error)
func (c *Client) AdminRemoveContent(ctx context.Context, r AdminRemoveRequest) (*AdminAction, error)
func (c *Client) AdminSuspend(ctx context.Context, u UserRef, r AdminSuspendRequest) (*AdminUserView, error)
func (c *Client) AdminUnsuspend(ctx context.Context, u UserRef) (*AdminUserView, error)
func (c *Client) AdminListUsers(ctx context.Context, q AdminUserQuery) (*Page[AdminUserView], error)
func (c *Client) AdminListActions(ctx context.Context, q AdminActionQuery) (*Page[AdminAction], error)

// 5.23 GET /v1/ws is dialed by internal/realtime using WebSocketURL + AuthHeader.
```

- 요청 구조체의 JSON 태그는 api.md 필드 이름(snake_case). `Opt[T]` 필드는 `omitzero`, 일반 선택 필드는 `omitempty`.
- `internal/api/apitest`(core): 테스트용 가짜 서버.

```go
package apitest
type Server struct{ URL string /* ... */ }
func New(tb testing.TB) *Server                                    // httptest.Server, closed on Cleanup
func (s *Server) Handle(pattern string, h http.HandlerFunc)        // Go 1.22 mux pattern, e.g. "GET /v1/channels/{id}/messages"
func (s *Server) JSON(pattern string, status int, body any)        // canned JSON
func (s *Server) Error(pattern string, status int, code string)    // error envelope
func (s *Server) Requests() []Recorded                             // method, path, query, body (token redacted)
func (s *Server) Client(opts ...api.Option) *api.Client            // token "msl_test"
type Recorded struct{ Method, Path, Query string; Body []byte }
```

### 4.4 `internal/realtime` — WebSocket 클라이언트 (TUI·데몬 공용)

```go
package realtime

type Mode string
const (
	ModeFull   Mode = "full"
	ModeNotify Mode = "notify"
)

type ResumePoint struct {
	Epoch        *string // nil => first connect
	LastSeq      *int64
	UseDeviceAck bool    // daemon restart without disk state
}

type Phase int
const (
	PhaseIdle Phase = iota
	PhaseConnecting   // dialing
	PhaseSyncing      // got hello, sent resume, waiting for ready (UI: "동기화 중")
	PhaseLive         // ready received
	PhaseBackoff      // waiting before reconnect
	PhaseStopped      // terminal: unauthorized/suspended/upgrade_required/ctx done
)

type State struct {
	Phase       Phase
	Attempt     int       // consecutive failures (n in backoff formula)
	NextRetry   time.Time // PhaseBackoff only
	LastClose   int       // last close code (0 if none)
	Err         error
	Epoch       string
	Seq         int64     // last processed seq
	TUIOnDevice *bool     // notify mode: hello.local_tui_connected / device.tui_state
}

type Event struct {
	Type     string          // events.md 8장 name
	Seq      *int64          // nil for ephemeral
	TS       time.Time
	Data     json.RawMessage
	Replayed bool            // received before `ready` on this connection
}
func (e Event) Decode() (any, error) // typed payload (table below); unknown type => (nil, nil)

type Options struct {
	API          *api.Client       // base URL, token, UA
	DeviceID     string
	Mode         Mode
	Resume       ResumePoint
	OnEvent      func(Event)       // called on the client goroutine, in order; must not block long
	OnState      func(State)
	Logger       *slog.Logger
	Now          func() time.Time
	Rand         func() float64    // jitter, tests inject
	Dial         func(ctx context.Context, url string, h http.Header) (Conn, error) // nil => coder/websocket
}

// Conn abstracts the socket for tests.
type Conn interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, data []byte) error
	Close(code int, reason string) error
}

var (
	ErrUnauthorized    = errors.New("realtime: unauthorized (4001/401)")
	ErrSuspended       = errors.New("realtime: account suspended (4003/403)")
	ErrUpgradeRequired = errors.New("realtime: upgrade required (4010/426)")
)

type Client struct{ /* unexported */ }
func New(opts Options) *Client
func (c *Client) Run(ctx context.Context) error           // connect+reconnect loop; returns nil on ctx done, or one of the Err* terminal errors
func (c *Client) Position() ResumePoint                   // current epoch + last processed seq
func (c *Client) MarkProcessed(seq int64)                 // caller finished applying; acks are sent at most every 5s
func (c *Client) Typing(channelID string, threadRootID *string) // full only; throttled to 1 per 3s per (channel,thread)
func (c *Client) SetIdle(idle bool)                       // full only; sends only on change, re-sent after reconnect
func (c *Client) Watch(topics []string)                   // full only; full replace, capped at 50, re-sent every 120s and after reconnect
func (c *Client) Close()                                  // send 1000 and stop
```

동작 규칙(events.md 그대로 구현):
- 연결: `GET /v1/ws?mode=&device_id=` + `AuthHeader()`. 업그레이드 전 HTTP 오류 봉투 → `401`=ErrUnauthorized(중단), `403 account_suspended`=ErrSuspended(중단), `426`=ErrUpgradeRequired(중단), `429`=`retry_after_sec` 후 재시도, 그 외 백오프.
- `hello` 수신 → 10초 안에 `resume` 1회(현재 `ResumePoint`). `ready` 전까지 `Replayed=true`. `ready` 수신 → `PhaseLive`, 그리고 `ready` 이벤트 자체도 `OnEvent` 로 넘긴다(상위가 `resync_required`/첫 연결을 보고 재동기화).
- 기록 이벤트: 같은 epoch 에서 `seq <= 마지막 처리 seq` 면 버린다(재전송 경계 중복). 새 epoch 가 되면(`ready.epoch` 가 다름) 기준을 바꾼다.
- 하트비트: `hello.heartbeat_interval_ms` 마다 정확히 `{"type":"ping"}`. 보낸 뒤 10초 동안 어떤 프레임도 없으면 연결을 닫고 재접속.
- 백오프: `delay = rand(0, min(30s, 0.5s × 2^n))`, `ready` 후 60초 유지되면 `n=0`.
- 종료 코드 처리: 1000=정지(우리가 닫은 경우), 1001·4002=즉시 재접속(백오프 n=0 지연 0), 1009·1011·4000=백오프, 4001=ErrUnauthorized, 4003=ErrSuspended, 4008=10초 후, 4009=재접속하되 60초 안에 두 번째면 `PhaseStopped`(`Err` 에 사유, UI 상태줄 표시), 4010=ErrUpgradeRequired.
- `ack` 는 `MarkProcessed` 된 최대 seq 를 5초에 1번 이하로 보낸다.
- 클라이언트 프레임 속도: 내부 큐로 10초에 25프레임을 넘지 않게 한다(서버 한도 30, ping 제외).

이벤트 타입 → `Decode()` 결과 타입(전부 `realtime` 패키지, JSON 은 events.md 4장 그대로):

| type | Go 타입 |
|---|---|
| hello | `Hello{ConnectionID, SessionID, UserID, Mode, DeviceID, Epoch string; Seq, OldestSeq int64; HeartbeatIntervalMS, HeartbeatTimeoutMS int; LocalTUIConnected bool; ServerTime time.Time; Limits HelloLimits}` |
| ready | `Ready{Resumed, ResyncRequired bool; Replayed int; Seq int64; Epoch string}` |
| reply | `Reply{Ref string; OK bool; Error *ReplyError}` |
| session.revoked | `SessionRevoked{SessionID, Reason string}` |
| message.created / message.updated | `MessageEvent{Message api.Message}` |
| message.deleted | `MessageDeleted{ChannelID, MessageID string; ThreadRootID *string; Tombstone bool}` |
| thread.updated | `ThreadUpdated{ChannelID, RootID string; Thread api.ThreadSummary}` |
| reaction.added / reaction.removed | `ReactionEvent{ChannelID, MessageID string; ThreadRootID *string; Emoji, UserID string; Count int}` |
| message.pinned / message.unpinned | `PinEvent{ChannelID, MessageID, By string}` (`pinned_by`/`unpinned_by` → By) |
| poll.updated | `PollUpdated{Poll api.Poll}` |
| typing | `Typing{ChannelID string; ThreadRootID *string; UserID string; ExpiresInMS int}` |
| read_state.updated | `ReadStateUpdated{State api.UnreadState}` |
| channel.created / channel.updated | `ChannelEvent{Channel api.Channel}` |
| channel.deleted | `ChannelDeleted{ChannelID string; SpaceID *string; Reason string}` |
| channel.member_added | `ChannelMemberAdded{ChannelID string; User api.UserSummary}` |
| channel.member_removed | `ChannelMemberRemoved{ChannelID, UserID string}` |
| channel.prefs_updated | `ChannelPrefsUpdated{ChannelID string; My api.ChannelMyState}` |
| space.joined | `SpaceJoined{Space api.SpaceDetail}` |
| space.updated | `SpaceUpdated{Space api.Space}` |
| space.deleted | `SpaceDeleted{SpaceID string}` |
| space.removed | `SpaceRemoved{SpaceID, Reason string}` |
| space.member_joined / space.member_updated | `SpaceMemberEvent{SpaceID string; Member api.SpaceMember}` |
| space.member_left | `SpaceMemberLeft{SpaceID, UserID, Reason string}` |
| space.invite_received | `InviteReceived{Invite api.DirectInvite}` |
| space.invite_removed | `InviteRemoved{InviteID, Reason string}` |
| friend.request_received / friend.request_accepted | `FriendRequestEvent{Request api.FriendRequest}` |
| friend.request_removed | `FriendRequestRemoved{RequestID, Reason string}` |
| friend.added | `FriendAdded{Friend api.Friend}` |
| friend.removed | `FriendRemoved{UserID string}` |
| block.updated | `BlockUpdated{UserID string; Blocked bool}` |
| user.updated | `UserUpdated{User api.UserSummary}` |
| settings.updated | `SettingsUpdated{Settings api.Settings}` |
| presence.snapshot | `PresenceSnapshot{Items []api.PresenceItem}` |
| presence.updated | `PresenceUpdated{UserID string; Status api.PresenceStatus; LastSeenAt *time.Time}` |
| presence.self | `PresenceSelf{Presence api.PresenceState}` |
| focus.started | `FocusStarted{Focus api.Focus}` |
| focus.ended | `FocusEnded{Summary api.FocusSummary}` |
| notification.created | `NotificationCreated{Notification api.Notification; UnreadCount int}` |
| notifications.read | `NotificationsRead{IDs []string; All bool; Before *string; UnreadCount int}` |
| alert | `AlertEvent{Alert api.Alert}` |
| timeline.item_created | `TimelineItemCreated{Item api.TimelineItem}` |
| post.deleted | `PostDeleted{PostID string}` |
| post.stats | `PostStats{PostID string; LikeCount, RepostCount, ReplyCount, QuoteCount int}` |
| post.reply_created | `PostReplyCreated{ParentID string; Post api.Post}` |
| story.created | `StoryCreated{UserID, StoryID string; ExpiresAt time.Time}` |
| story.deleted | `StoryDeleted{UserID, StoryID string}` |
| device.tui_state | `DeviceTUIState{DeviceID string; TUIConnected bool}` |

`poll.updated` 은 메시지 투표와 글 투표가 같은 타입을 쓴다(`Poll.ContextType` 으로 구분). 모르는 `type` 은 `Decode` 가 `(nil, nil)`.

### 4.5 `internal/store` — 공유 상태 (core)

**스레드 모델 [결정]**: Store 는 **Bubble Tea `Update` 고루틴 안에서만** 읽고 쓴다(잠금 없음). 실시간 이벤트·API 결과는 모두 `tea.Msg` 로 들어와 `app.Update` 가 `Store.Apply…` 를 부른다. 데몬은 Store 를 쓰지 않는다.

```go
package store

type Store struct {
	Me           *api.Me
	Self         api.PresenceState           // presence.self / PUT presence
	Focus        *api.Focus
	LastFocusSummary *api.FocusSummary       // set by focus.ended or the EndFocus caller (FocusEnded); read by the focus-summary overlay (9.1)
	Settings     api.Settings
	Users        map[string]*api.UserSummary // every embedded UserSummary seen (ObserveUser)
	Presence     map[string]PresenceEntry
	Spaces       map[string]*api.SpaceDetail
	SpaceOrder   []string                    // GET /v1/spaces order (join order)
	Channels     map[string]*api.Channel     // text + dm + group_dm; Channel.My kept authoritative here
	DMOrder      []string                    // last_message_at desc
	Friends      map[string]api.Friend       // by user id
	FriendReqIn  []api.FriendRequest
	DirectInvites []api.DirectInvite
	Blocks       map[string]time.Time        // blocked user id -> since
	NotifUnread  int
	Sync         SyncState
	Alerts       *Ring[api.Alert]            // last 50 alerts (in-app panel / toasts)
}

type PresenceEntry struct{ Status api.PresenceStatus; LastSeenAt *time.Time; FetchedAt time.Time }

type SyncState struct {
	Phase        realtime.Phase
	Ready        bool      // at least one ready on current process
	Bootstrapped bool      // step-1 REST done at least once
	LastResync   time.Time
	Err          error
}

func New() *Store

// Event application (core entities), then every registered reducer.
func (s *Store) Apply(ev realtime.Event)
func (s *Store) ObserveUser(u api.UserSummary)       // update cache, keeps newest
func (s *Store) ObserveChannel(ch api.Channel)       // merges; never overwrites per-viewer fields from event payloads (events.md 4.4 rule)
func (s *Store) User(id string) *api.UserSummary
func (s *Store) IsBlocked(userID string) bool
func (s *Store) IsMe(userID string) bool
func (s *Store) ChannelLabel(id string) string       // "#개발", "민지", "점심팟", "민지, 준호, 하나"
func (s *Store) SpaceChannels(spaceID string) []*api.Channel // visible, by position
func (s *Store) TotalUnread() (unread, mentions int)
func (s *Store) FocusEnded(sum api.FocusSummary)     // Focus=nil, LastFocusSummary=&sum (focus.ended event and EndFocus result both call this; never opens UI)
func (s *Store) Reset()                              // logout: clear everything, notify reducers

// Reducers: domain stores register at init().
type Reducer interface {
	Name() string
	Apply(s *Store, ev realtime.Event)
	Reset()                                          // logout / user switch
}
type Resyncer interface {
	// Called after `ready` when resync is required or on first connect, AFTER core step-1 data landed.
	// Returns commands that refetch open windows (events.md 5.3 steps 2-4).
	Resync(s *Store, c *api.Client) tea.Cmd
}
func RegisterReducer(factory func(s *Store) Reducer) // init()-time; reducer may also implement Resyncer
func (s *Store) Reducer(name string) Reducer         // e.g. chatstore.Get(s) uses this

// Bootstrap / resync (events.md 5.3 step 1), as tea commands.
func Bootstrap(c *api.Client) tea.Cmd                 // emits BootstrapMsg
type BootstrapMsg struct {
	Me *api.Me; Spaces []api.SpaceDetail; DMs []api.Channel; Unreads []api.UnreadState
	NotifUnread int; Friends []api.Friend; FriendReqIn []api.FriendRequest
	DirectInvites []api.DirectInvite; Blocks []api.BlockEntry; Err error
}
func (s *Store) BeginBootstrap()                       // the app calls it as a fetch starts: journals core events (and ObserveChannel calls) until ApplyBootstrap
func (s *Store) ApplyBootstrap(m BootstrapMsg) tea.Cmd // replaces core entities (a nil field = failed request, kept), re-applies the journal over the older snapshot (events.md 5.3 step 5), then batches every Resyncer

// Generic ordered window (ascending by ID) used by chatstore/feedstore.
type Window[T any] struct{ /* unexported */ }
func NewWindow[T any](idOf func(T) string, timeOf func(T) time.Time) *Window[T]
func (w *Window[T]) Items() []T                                // ascending
func (w *Window[T]) Len() int
func (w *Window[T]) Get(id string) (T, bool)
func (w *Window[T]) Upsert(items ...T)                         // insert by ID order, replace same ID
func (w *Window[T]) Update(id string, fn func(*T)) bool
func (w *Window[T]) Remove(id string) bool
func (w *Window[T]) Replace(items []T, hasMoreBefore, hasMoreAfter bool)
func (w *Window[T]) PrependOlder(items []T, hasMoreBefore bool)
func (w *Window[T]) AppendNewer(items []T, hasMoreAfter bool)
func (w *Window[T]) HasMoreBefore() bool
func (w *Window[T]) HasMoreAfter() bool
func (w *Window[T]) Covers(id string) bool                     // first.ID <= id <= last.ID (or hasMoreAfter==false and id > last)
func (w *Window[T]) OverlapAnchor(overlap time.Duration) string // events.md 5.3: newest item with time <= last.time-overlap; "" if none
func (w *Window[T]) Clear()

type Ring[T any] struct{ /* fixed-capacity */ }
func NewRing[T any](n int) *Ring[T]
func (r *Ring[T]) Push(v T)
func (r *Ring[T]) Items() []T // newest first
```

core 가 `Apply` 에서 직접 처리하는 이벤트: `channel.*`, `space.*`, `friend.*`, `block.updated`, `user.updated`, `settings.updated`, `presence.*`, `focus.*`(`focus.ended` → `FocusEnded`, UI 를 열지 않음), `read_state.updated`, `notification.created`(카운트만), `notifications.read`(카운트만), `alert`(링에 추가만), 그리고 `message.created` 의 **채널 요약 부분**(`last_message_*`, `DMOrder`, 안 읽음·멘션 증가 규칙 events.md 4.2 — "지금 보고 있는 채널" 판단은 `Store.Viewing` 집합으로).

```go
func (s *Store) SetViewing(channelIDs ...string)  // app sets each frame from visible routes; used for unread increment rule
func (s *Store) Viewing(channelID string) bool
```

### 4.6 `internal/notify` — 알림 추상화 (TUI·데몬 공용, core)

```go
package notify

type Role int
const (RoleTUI Role = iota; RoleDaemon)

type OSNotification struct{ Title, Body string; AlertID string }

type Backend interface{ Show(n OSNotification) error }
func NewOSBackend(appName string) Backend   // per-OS backend below; appName = "masil"
type Recorder struct{ Shown []OSNotification } // test backend
func (r *Recorder) Show(n OSNotification) error

type Ledger struct{ /* path + lock path */ }
func OpenLedger(p config.Paths) *Ledger
func (l *Ledger) MarkIfNew(alertID string) (isNew bool, err error) // flock + read + append + atomic write (3.7)
func (l *Ledger) Seen(alertID string) (bool, error)

type Input struct {
	Role            Role
	Now             time.Time
	Replayed        bool  // Event.Replayed
	DesktopEnabled  bool  // config.notify.desktop
	// TUI only:
	TerminalFocusKnown bool
	TerminalFocused    bool
	ViewingTarget      bool // the alert's channel/thread/post is visible right now
	// Daemon only:
	TUIPresent      bool  // final decision from daemon (local lock / server signal / 3s grace)
}

type Decision int
const (
	Skip   Decision = iota // nothing (stale replay, disabled, daemon with TUI present)
	Silent                 // in-app only (suppressed alert, or user is looking at it)
	Popup                  // OS notification (after ledger check)
)

func Decide(a api.Alert, in Input) Decision
func Format(a api.Alert) OSNotification      // title/body sanitized: control chars stripped, title <= 80, body <= 200 code points

type Notifier struct{ /* backend + ledger + logger */ }
func New(b Backend, l *Ledger, log *slog.Logger) *Notifier
func (n *Notifier) Handle(a api.Alert, in Input) (Decision, error) // Decide; if Popup => Show
func (n *Notifier) Show(a api.Alert) error                         // ledger.MarkIfNew; only new => backend.Show (blocking I/O: the TUI runs it in a tea.Cmd)
```

`Decide` 규칙(순서대로, events.md 4.7·7장):
1. `in.Replayed && now - alert.created_at > 10분` → TUI 는 `Silent`, 데몬은 `Skip`.
2. `alert.suppressed` → TUI `Silent`, 데몬 `Skip`.
3. `!in.DesktopEnabled` → TUI `Silent`, 데몬 `Skip`.
4. 데몬이고 `in.TUIPresent` → `Skip`.
5. TUI 이고 `TerminalFocusKnown && TerminalFocused && ViewingTarget` → `Silent`(보고 있는 것을 또 알리지 않음).
6. 나머지 → `Popup`. (포커스 보고를 지원하지 않는 터미널이면 `TerminalFocusKnown=false` → 항상 Popup: "다른 창에서 일하다가도 놓치지 않는다")

**OS 백엔드 규칙 [보안 결정, 2026-11-06]** — 제목·본문은 다른 사용자가 만든 텍스트다. 어떤 백엔드도 그것을 스크립트·셸 명령·마크업 문서에 **문자열로 끼워 넣지 않는다**(값은 데이터로만 넘긴다). TUI 와 데몬이 같은 `NewOSBackend` 를 쓴다.
- 공통: `Show` 는 표시 직전에 `Format` 과 같은 sanitize + 길이 제한(제목 80, 본문 200 코드 포인트)을 다시 적용한다 — `Format` 을 거치지 않은 호출도 제어 문자·무제한 길이가 OS 에 닿지 않는다. bidi·보이지 않는 서식 문자(Cf)·줄·문단 구분자(U+2028/U+2029)·변이 선택자·그 밖의 기본 무시 문자·비문자 제거는 `textwidth.Sanitize`(8.2 규칙 1) 를 그대로 따른다.
- macOS: 셸 없이 `/usr/bin/osascript -e 'on run argv' -e 'display notification (item 2 of argv) with title (item 1 of argv)' -e 'end run' -- <제목> <본문>` — 프로그램은 상수, 제목·본문은 `--` 뒤 argv 항목(`-e` 같은 값도 옵션이 아님).
- Linux: D-Bus `org.freedesktop.Notifications.Notify` 직접 호출(exec 없음, 세션 버스 자동 기동(`dbus-launch`) 안 함). summary 는 명세상 일반 텍스트지만 마크업으로 그리는 서버도 있으므로 `<`·`>` 를 `‹`(U+2039)·`›`(U+203A) 로 바꾼다(엔티티 이스케이프는 명세를 따르는 서버에서 `&amp;` 로 보이므로 쓰지 않음; `<` 가 없으면 엔티티만으로는 태그가 생기지 않는다). body 는 서버가 `body-markup` 능력을 밝히면(또는 능력 조회 실패 시) `& < >` 를 이스케이프하고, 밝히지 않으면 summary 와 같이 `<`·`>` 를 `‹`·`›` 로 바꾼다(능력을 밝히지 않고 마크업으로 그리는 서버에도 `<a href>`·`<img src>` 가 닿지 않음). 능력은 연결마다가 아니라 **알림마다** 조회한다(연결이 살아 있는 동안 알림 서버가 바뀔 수 있음). actions·icon·hints 는 비움.
- Windows: WinRT 토스트 COM API(`go-toast/wintoast.Push`, 옵션 없이 — PowerShell 폴백·우선 옵션 금지). 토스트 XML 은 직접 만들고 제목·본문은 `encoding/xml.EscapeText` 로 이스케이프한 문자 데이터(CDATA 금지).
- 그 외 OS: 오류(배포 대상 아님).
- 구조: 각 OS 의 `show`(실제 실행 경로)는 빌드 제약 없는 `backend_platform.go` 에 두고 마지막 OS 호출만 주입받는다 — macOS `darwinPlatform.run`(실물 `execRun` = `exec.CommandContext(ctx, name, args...).CombinedOutput()`, 셸 없음), Windows `windowsPlatform.setAppID`/`push`(실물 `wintoast.SetAppData`/`wintoast.Push(doc)`), Linux `dbusPlatform.dial`(실물 세션 버스 연결; `alive`·`close`·GetCapabilities·Notify). GOOS 전용 파일(`backend_darwin.go`·`backend_windows.go`·`backend_dbus.go`)은 그 실물을 끼우는 배선만 하고 `OSNotification`·텍스트 빌더를 참조하지 않는다.
- 시험: 각 OS 의 인자/XML/D-Bus 호출을 만드는 순수 함수를 모든 OS 에서 적대적 입력(`$(...)`, 백틱, `"@`, `]]>`, `" & do shell script "id" & "`, 줄바꿈, U+2028/U+2029, `-e`, `<a href>`, bidi 제어(U+202E 등)·보이지 않는 서식 문자)으로 단언한다. 그리고 **모든 OS 에서** 가짜 실행기를 넣은 각 플랫폼으로 `Show` 를 불러, 실제로 넘긴 argv·토스트 XML·Notify 인자가 빌더 결과(`darwinCommand`·`toastXML`·`dbusCall`)와 같고 비활성인지 단언한다(빌더만 시험하면 `show` 가 빌더를 건너뛰어도 잡히지 않음). `Show` 자체가 sanitize·길이 제한을 하는지도 `Show` 를 불러 단언하며, macOS 에선 `Show`→`execRun` 으로 실제 osascript 를 돌려 같은 argv 가 그대로 돌아오는지(명령 미실행) 확인한다. 각 OS 에선 `NewOSBackend` 가 실물 배선(`execRun`·`setToastAppID`/`pushToast`·`dialSessionBus`)을 쓰는지, Linux 에선 godbus 어댑터가 Notify 인자를 그대로 넘기는지 단언한다. 소스 검사: beeep·PowerShell 옵션·셸 리터럴·CDATA 리터럴 금지, `show` 메서드는 `backend_platform.go` 에만, `os/exec` 는 `execRun` 에서만(본문 고정), `wintoast.Push` 는 문서 인자 하나만.

### 4.7 `internal/textwidth` — 폭 계산 (8장 상세)

```go
package textwidth
func Sanitize(s string) string                    // display-only normalization (8.2); idempotent; every grapheme of the result has StringWidth == StringWidthWc
func SanitizeInput(s string) string               // text-entry normalization (8.4): same rules as Sanitize except "\n" is kept ("\r\n"/"\r"/U+2028/U+2029 -> "\n"); idempotent
func Width(s string) int                          // ansi.StringWidth(s) — call on sanitized text
func Truncate(s string, w int, tail string) string // grapheme-safe, ANSI-aware, never splits a wide char; result Width <= w
func TruncateLeft(s string, w int, head string) string
func Wrap(s string, w int) []string               // word wrap at spaces, hard-wrap long words, CJK breakable between syllables; each line Width <= w
func PadRight(s string, w int) string             // pad with spaces to exactly w (truncate if wider)
func PadLeft(s string, w int) string
func Center(s string, w int) string
func Fit(lines []string, w, h int) []string        // exactly h lines, each exactly w cells (pad/truncate) — used by every pane
```

### 4.8 `internal/markup` — 본문 마크다운 부분집합 (core)

```go
package markup

type Doc struct{ Blocks []Block }
type Block struct {
	Kind   BlockKind // Paragraph | Code
	Inline []Inline  // Paragraph
	Lang   string    // Code
	Code   string    // Code
}
type Inline struct {
	Kind InlineKind // Text | Bold | Italic | Code | Link | Mention | Here | Hashtag
	Text string
	URL  string     // Link (also bare URLs)
	Handle string   // Mention (lowercase, no @)
}

func Parse(body string, opts ParseOptions) Doc     // api.md 2.1 grammar; no other markdown
type ParseOptions struct{ Hashtags bool }          // posts/stories true, messages false

type RenderOptions struct {
	Width       int
	MyHandle    string            // highlight mentions of me
	Mentions    []api.MentionRef  // only these @handles are styled as mentions (resolved by server)
	MentionHere bool
	Compact     bool              // single-line summary mode: code blocks => "‹코드 N줄›", newlines => " "
	MaxLines    int               // 0 = unlimited; overflow adds "… 더 보기"
	Hyperlinks  bool              // OSC 8 via lipgloss Style.Hyperlink
}
func Render(d Doc, t *theme.Theme, o RenderOptions) []string // each line Width <= o.Width, sanitized
func Highlight(lang, code string, t *theme.Theme, width int) []string // chroma tokens -> theme.CodeStyle
func Plain(body string, maxCells int) string        // markdown stripped one-line preview
```

- 코드 블록은 왼쪽 막대 `▎`(acc) + codebg 배경 + chroma 강조. 언어 미지정/모름 → `lexers.Fallback`.
- 링크 안전 규칙(pending-fixes 0-b): `[t](u)` 의 u 에 공백·제어 문자(C0·DEL·C1)·잘못된 UTF-8 이 하나라도 있으면 링크가 아니라 원문 텍스트. 맨 URL 은 보이는 공백(스페이스·탭·`\n`·`\r\n`)이나 `<>"\``에서 끝나며, 그 전에 화면에 안 보이는 끊김(그 밖의 제어 문자·잘못된 UTF-8·U+2028/U+2029)이나 **보이지 않는 문자(`textwidth.Invisible`, VS15/VS16 제외)** 를 만나면 **앞부분도 링크로 만들지 않고** 전체를 텍스트로 둔다(보이는 주소와 여는 주소가 달라지지 않게). OSC 8 은 제어 문자·ESC/BEL·잘못된 UTF-8·보이지 않는 문자(`textwidth.Invisible`: Cf — bidi·zero-width·ZWJ·BOM·soft hyphen·태그 — ·U+2028/U+2029·CGJ·VS1–14/VS17–256·한글 채움 문자·비문자 등, VS15/VS16 제외)가 없는 URL 에만 붙이고, 대상은 `HyperlinkTarget` 으로 0x80 이상 바이트를 퍼센트 인코딩한 인쇄 가능 ASCII. **또한 `norm.NFC.String(u) == textwidth.Sanitize(u)` 일 때만 OSC 8 을 붙인다 [보안 결정, 2026-11-06, pending-fixes 0-c]** — 표시용 Sanitize 가 지우거나 바꾸는 글자(VS15/VS16, 피부색 U+1F3FB–1F3FF, 키캡 U+20E3, 태그 문자, rule 7 치환으로 빠지는 인도계 모음 기호 등)가 든 URL 은 보이는 주소와 여는 주소가 달라지므로 링크 모양의 일반 텍스트로만 보인다(NFD 처럼 NFC 차이만 있는 URL 은 눌린다).
- **토큰 경계는 화면 기준 [보안 결정, 2026-11-06, pending-fixes 1]**: `Parse` 는 원문을 파싱하고 조각은 그 뒤에 `Sanitize` 하므로, 보이지 않는 문자(`textwidth.Invisible`, ZWJ 포함)가 멘션·해시태그·맨 URL 의 경계를 몰래 바꾸지 못하게 한다. (a) 앞 글자 검사(`(?<![A-Za-z0-9_])` 등)는 보이지 않는 문자를 건너뛴 **앞의 보이는 글자**로 한다(`sam`+U+200B+`@minji` 는 멘션 아님). (b) 토큰이 보이지 않는 문자 연속에서 끊기고 그 뒤 첫 보이는 글자가 토큰을 이어 갈 글자면(`#공지`+U+200B+`사항`, `@sam`+U+034F+`_admin`), 화면에는 한 낱말로 보이므로 그 낱말 전체를 **일반 텍스트**로 둔다. 보이지 않는 문자 뒤가 공백·문장부호·끝이면 토큰은 그대로다. 서버의 멘션 추출은 원문 기준이라 이런 본문에서는 서버 멘션과 화면이 다를 수 있다(화면은 누를 수 있는 것만 강조).
- 링크는 텍스트에 밑줄 + acc4, 맨 URL 도 같음. 멘션은 `selbg` 배경 + `acc` 글자(mockup `.mention`). 내 멘션·`@here` 는 굵게.

### 4.9 `internal/humanize`, `internal/slash`, `internal/termcaps` (core)

```go
package humanize
func Clock(t time.Time, loc *time.Location, h12 bool) string        // "11:05" / "오전 11:05"
func MessageTime(now, t time.Time, loc *time.Location) string        // today "11:05", yesterday "어제 11:05", this year "11월 3일 11:05", else "2025. 11. 3."
func Relative(now, t time.Time) string                               // "방금", "3분", "2시간", "어제", "11월 3일"
func Count(n int) string                                             // 999, "1.2천", "3.4만"
func Duration(d time.Duration) string                                // "25분", "1시간 30분"
func DayDivider(t time.Time, loc *time.Location) string             // "2026년 11월 5일 목요일"
```

```go
package slash
type Name string
const (Me Name = "me"; Shrug = "shrug"; Poll = "poll"; Status = "status"; Focus = "focus"; Roll = "roll"; Invite = "invite")
type Command struct {
	Name Name
	Text string      // /me, /shrug, /status text
	Emoji string     // /status
	Poll *api.PollInput
	Dice string      // /roll "2d6"
	FocusOff bool; FocusDuration time.Duration // /focus 25m | /focus off | /focus (default)
	InviteUser string; InviteLink bool; LinkExpires time.Duration; LinkMaxUses int // /invite @h | /invite link [7d] [10]
}
func Parse(input string) (cmd *Command, ok bool, err error) // ok=false => not a slash command (send as text); err => Korean usage message
func Catalog() []Spec                                        // for composer completion & help
type Spec struct{ Name Name; Usage, Desc string }
```
- `/poll "질문" "A" "B"` 는 따옴표 인자(곧은 따옴표·둥근 따옴표 “” 모두), 옵션 `--multi`, `--for 1h`. 실행 주체: 채팅 입력창(chat) — `/me /shrug /poll /roll /invite` 는 chat 이 실행. `/status /focus` 는 입력창이 `ui.RunCommand("status.set"|"focus.start"|"focus.stop", args)` 를 내보내고 sns-fun 이 등록한 팔레트 명령이 실행(6.4).

```go
package termcaps
type Graphics int
const (GraphicsNone Graphics = iota; GraphicsHalfBlock; GraphicsKitty; GraphicsITerm2)
type Caps struct {
	Graphics      Graphics
	InTmux        bool
	FocusReports  bool   // updated when FocusMsg/BlurMsg first arrives
	UnicodeCore   bool   // mode 2027 reported (renderer uses grapheme width)
	TrueColor     bool
	Term, Program string // $TERM, $TERM_PROGRAM
}
func Detect(env []string, override string) Caps // override = config.ui.images
```
- 감지: `override != "auto"` 면 그 값(`off`=None). 아니면 `$TMUX` 있거나 `TERM` 이 `screen*`/`tmux*` → HalfBlock. `TERM=xterm-kitty` 또는 `KITTY_WINDOW_ID` 또는 `TERM_PROGRAM=ghostty` → Kitty. `TERM_PROGRAM=iTerm.app` 또는 `WezTerm` → ITerm2. 그 외 HalfBlock.

---

## 5. TUI 프레임워크 (core)

### 5.1 테마 — `internal/theme`

색 토큰은 **시안 `visual-style.html` 의 CSS 변수 값 그대로**(`acc4` 는 D 만 `layouts.html` 에 있음).

| 토큰 | A `charm` | B `midnight` | C `cream` | D `amber` (기본) |
|---|---|---|---|---|
| bg | `#1a1b26` | `#0f1419` | `#fdf6ec` | `#1d2021` |
| fg | `#e4e2f5` | `#c9d1d9` | `#3a3226` | `#ebdbb2` |
| dim | `#7a7896` | `#5c6773` | `#9a8b74` | `#928374` |
| line | `#3b3a55` | `#1f2a35` | `#e2d3bb` | `#504945` |
| acc | `#b794f6` | `#59c2ff` | `#e0607e` | `#fabd2f` |
| acc2 | `#ff6fae` | `#ffb454` | `#5b8def` | `#fb4934` |
| acc3 | `#7ee0c3` | `#aad94c` | `#2f9e6e` | `#b8bb26` |
| acc4 | `#7aa2f7` **[결정]** | `#d2a6ff` **[결정]** | `#8e6bbf` **[결정]** | `#83a598` |
| chip | `#2a2940` | `#151d26` | `#f3e6d2` | `#32302f` |
| selbg | `#2d2650` | `#13283a` | `#fbe3e8` | `#3c3836` |
| codebg | `#221f35` | `#121a22` | `#f5ead8` | `#282828` |
| 테두리 | Rounded `╭─╮` | Normal `┌─┐` | Rounded `╭─╮` | Double `╔═╗` |
| 어두움 | dark | dark | **light** | dark |

```go
package theme

type Name string
const (
	Charm    Name = "charm"    // A. Charm 보라
	Midnight Name = "midnight" // B. 미드나잇 블루
	Cream    Name = "cream"    // C. 크림 파스텔
	Amber    Name = "amber"    // D. 레트로 앰버
	Default       = Amber
)

type Palette struct{ BG, FG, Dim, Line, Acc, Acc2, Acc3, Acc4, Chip, SelBG, CodeBG color.Color }

type Theme struct {
	Name   Name
	Letter string        // "A".."D"
	Label  string        // "Charm 보라", "미드나잇 블루", "크림 파스텔", "레트로 앰버"
	Dark   bool
	P      Palette
	Border lipgloss.Border // RoundedBorder / NormalBorder / RoundedBorder / DoubleBorder
	S      Styles
}

type Styles struct {
	Base, Dim, Bold, Title, Subtitle lipgloss.Style
	PaneFocused, PaneBlurred         lipgloss.Style // border styles (acc / line)
	Selected, Cursorline             lipgloss.Style // selbg bg + acc fg
	Badge, BadgeMention              lipgloss.Style // acc2 bg + bg fg
	Pill                             lipgloss.Style // acc bg + bg fg + bold (status mode pill, active tab)
	Tab, TabActive                   lipgloss.Style
	StatusBar, StatusHint            lipgloss.Style
	Mention, MentionMe               lipgloss.Style
	Link, Hashtag                    lipgloss.Style // acc4 underline / acc3
	InlineCode, CodeBlock, CodeBar   lipgloss.Style // codebg, acc3 / codebg / acc
	Reaction, ReactionMine           lipgloss.Style // line border-ish chip / acc fg
	Timestamp, System, Deleted       lipgloss.Style
	Input, InputFocused, Placeholder lipgloss.Style
	Online, Away, DND, Offline       lipgloss.Style // ● acc3 / ◐ acc / ⊘ acc2 / ○ dim
	Error, Warning, Success          lipgloss.Style // acc2 / acc / acc3
	NewHighlight                     lipgloss.Style // selbg bg, faded by widgets.Fade
	Overlay, OverlayBorder           lipgloss.Style
}

func Get(n Name) *Theme
func All() []*Theme                         // A, B, C, D order
func Parse(s string) (Name, bool)           // accepts "charm|midnight|cream|amber" and "a|b|c|d" (case-insensitive)
func (t *Theme) UserColor(userID string, self bool) color.Color // self => Acc; others FNV-1a(userID) % 3 over [Acc2, Acc3, Acc4]
func (t *Theme) StoryBG(slot string) color.Color                 // accent|accent2|accent3|accent4|plain(chip)
func (t *Theme) CodeStyle(tt chroma.TokenType) lipgloss.Style    // Keyword=Acc bold, String=Acc3, Number=Acc2, Comment=Dim italic, Name.Function=Acc4, Name.Builtin=Acc2, Operator/Punctuation=FG, Generic.Deleted=Acc2, Generic.Inserted=Acc3, other=Acc3 (mockup code color)
```

- **모든 스타일은 `Base`(fg+bg)에서 파생**한다. 컴포넌트는 스타일 없는 문자열을 출력하지 않고 반드시 `ctx.Theme.S.*` 로 감싼다(빈 칸도 `Base` 로 채움). 그래야 터미널 기본 배경이 다른 경우에도 칸이 테마 색으로 채워진다.
- 추가로 `config.ui.paint_background=true` 면 앱이 `tea.View.BackgroundColor=P.BG`, `ForegroundColor=P.FG` 를 설정한다(종료 시 Bubble Tea 가 원복).
- 테마 전환은 즉시: `ui.SetThemeMsg{Name}` → 앱이 `ctx.Theme` 교체 + `config.Save` + 모든 컴포넌트에 `ui.ThemeChangedMsg` 방송(캐시된 렌더 결과 무효화).
- 색 프로파일 축소(256색/16색 터미널)는 Bubble Tea 출력 단계가 자동으로 한다. 테마는 항상 hex 로 정의.

### 5.2 레이아웃 엔진 — `internal/ui/layout`

#### 5.2.1 모드

```go
package layout

type Mode int
const (
	ModeTooSmall Mode = iota // below 20x6: centered "창을 조금만 키워 주세요"
	ModeTiny                 // 초소형
	ModeWidget               // 위젯
	ModeCompact              // 컴팩트
	ModeStandard             // 표준
	ModeCommand              // 지휘소
)
func (m Mode) Label() string // "지휘소","표준","컴팩트","위젯","초소형","—"
func (m Mode) ID() string    // "command","standard","compact","widget","tiny","too_small"
func ParseMode(s string) (Mode, bool) // ids above; "auto" => (0,false)

type Breakpoints struct {
	CommandMinCols, CommandMinRows   int // 200, 50
	StandardMinCols, StandardMinRows int // 100, 40
	TinyMaxCols, TinyMaxRows         int // 43, 15
	WidgetMinRows                    int // 24
	MinCols, MinRows                 int // 20, 6 (fixed, not configurable)
}
func DefaultBreakpoints() Breakpoints
func FromConfig(c config.LayoutConfig) Breakpoints

// Auto selects a mode from terminal size. Exactly this order:
func Auto(cols, rows int, bp Breakpoints) Mode
```

`Auto` 규칙 **[결정]** (순서대로 첫 일치):
1. `cols < 20 || rows < 6` → TooSmall
2. `cols <= 43 || rows <= 15` → Tiny
3. `cols < 100` → `rows < 24` 이면 Tiny, 아니면 **Widget**
4. `rows < 40` → **Compact**
5. `cols >= 200 && rows >= 50` → **Command(지휘소)**
6. 그 외 → **Standard(표준)**

| 크기 | 모드 |
|---|---|
| 200×60 | 지휘소 |
| 200×50 | 지휘소(경계) |
| 200×49, 199×50, 120×40, 100×40 | 표준 |
| 120×30, 100×39, 199×39, 160×16 | 컴팩트 |
| 80×24, 99×60, 60×60, 44×24 | 위젯 |
| 50×20, 99×23, 43×40, 120×15, 44×16 | 초소형 |
| 19×10, 30×5 | TooSmall |

**고정(pin)**: 사용자가 모드를 고정하면 창이 그 모드의 **최소 크기** 이상일 때만 적용하고, 작으면 `Auto` 결과를 쓰며 상태줄에 `📌 지휘소 고정 · 창이 작아 표준으로 표시` 를 띄운다. 고정 최소 크기: 지휘소 160×40, 표준 80×24, 컴팩트 60×16, 위젯 44×16, 초소형 20×6. 이 값은 고정 상수(설정 불가).

```go
func Resolve(cols, rows int, bp Breakpoints, pin *Mode) (mode Mode, pinFellBack bool)
func PinMinSize(m Mode) (cols, rows int)
```

모드 전환은 **즉시**(디바운스·히스테리시스 없음). 크기 계산이 결정적이므로 같은 크기는 늘 같은 모드다.

#### 5.2.2 영역(Slot)과 기하

```go
type SlotID string
const (
	SlotRail          SlotID = "rail"          // 방 레일 (chat)
	SlotSidebar       SlotID = "sidebar"       // 채널·DM 목록 (chat)
	SlotMain          SlotID = "main"          // 현재 경로(대화/피드/프로필…)
	SlotFeed          SlotID = "feed"          // 지휘소 전용 피드 열 (sns-fun)
	SlotThread        SlotID = "thread"        // 지휘소 오른쪽 위 / 표준 아래 분할
	SlotMembers       SlotID = "members"       // 지휘소 오른쪽 위(스레드 닫힘일 때) (chat)
	SlotNotifications SlotID = "notifications" // 지휘소 오른쪽 아래 (sns-fun)
	SlotTabs          SlotID = "tabs"          // 컴팩트·위젯·초소형 상단 탭 (core)
	SlotContext       SlotID = "context"       // 위젯 탭 아래 "#개발 · 노바 개발방 ▾" (core, 내용은 경로 제목)
	SlotStatus        SlotID = "status"        // 하단 상태줄 (core)
)

type Rect struct{ X, Y, W, H int }
func (r Rect) Contains(x, y int) bool
func (r Rect) Inner() Rect                     // minus 1-cell border on each side

type Pane struct {
	Slot     SlotID
	Rect     Rect
	Bordered bool   // app draws the theme border + title; component gets Rect.Inner()
}

// State is derived from app.NavState every frame (app.NavState.LayoutState, 5.3.3); layout never stores it.
type State struct {
	ThreadOpen  bool // the active tab has an open thread (NavState.Tabs[Active].Thread != nil)
	FeedSwapped bool // standard: NavState.Front == FrontFeed (feed surface shown in Main)
}

type Layout struct {
	Mode  Mode
	Cols, Rows int
	Panes []Pane // draw order; no overlaps; union covers the whole screen exactly
}
func Compute(mode Mode, cols, rows int, st State) Layout
func (l Layout) Pane(s SlotID) (Pane, bool)
func (l Layout) At(x, y int) (Pane, bool)
```

`Compute` 기하 **[결정]** — `W=cols`, `H=rows`, 본문 높이 `B = H-1`(맨 아래 1줄 상태줄 `SlotStatus`, 모든 모드 공통). `clamp(v, lo, hi)`. 정수 나눗셈은 내림.

- **지휘소** (`[레일][목록][대화][피드][스레드|멤버 / 알림]`):
  - `rail=6`, `side=clamp(W*13/100, 24, 34)`, `right=clamp(W*17/100, 32, 48)`, `rest=W-rail-side-right`, `chat=rest*7/12`, `feed=rest-chat`.
  - 오른쪽 열: 위 `top=B*55/100` 는 `ThreadOpen` 이면 `SlotThread` 아니면 `SlotMembers`, 아래 `B-top` 은 `SlotNotifications`.
  - 200×60 → rail 6, side 26, chat 78, feed 56, right 34(top 32 / bottom 27).
- **표준** (`[목록(방 드롭다운)][대화]`, 스레드는 아래 분할):
  - `side=clamp(W*22/100, 24, 32)`, `main=W-side`.
  - `ThreadOpen && !FeedSwapped` 이면 main 열을 위 `max(10, B*60/100)` = `SlotMain`, 아래 나머지 = `SlotThread`(아래가 6줄 미만이면 스레드는 Main 으로 대체 열림).
  - `FeedSwapped` 이면 `SlotMain` 에 피드 경로가 들어간다(분할 없음, 스레드 상태는 보존).
  - 120×40 → side 26, main 94.
- **컴팩트** (`상단 탭 + 대화 하나`): `SlotTabs` 1줄(테두리 없음) + `SlotMain` W×(B-1). 스레드는 Main 을 **교체**(5.3.3 투영 규칙, `esc` 로 채널로).
- **위젯** (`탭 채팅/피드/알림 + 맥락 줄 + 말풍선`): `SlotTabs` 1줄 + `SlotContext` 1줄(테두리 없음) + `SlotMain` W×(B-2).
- **초소형**: `SlotTabs` 1줄(축약형) + `SlotMain` W×(B-1) **테두리 없음**(`Bordered=false`).
- **TooSmall**: `SlotStatus` 없이 화면 전체에 가운데 정렬 안내 한 줄(없으면 잘림 없이 `…`). `Panes` = `[{SlotMain, {0,0,W,H}, Bordered:false}]`, `W==0 || H==0` 이면 빈 목록. 이 모드에서는 경로·오버레이를 그리지 않는다(상태는 그대로 보존).
- `Compute` 는 **어떤 입력에도** 패닉하지 않는다(`cols`/`rows` 0·음수 포함 — 음수는 0 으로 본다). 테두리 있는 창의 내부 크기가 1×1 미만이 되는 일은 위 최소값으로 생기지 않으며, 이는 골든이 아니라 `Compute` 전수 속성 시험(10.3)이 보장한다.

### 5.3 UI 계약 — `internal/ui`

#### 5.3.1 컴포넌트

**[결정]** 컴포넌트는 포인터 수신자로 **제자리 변경**한다(값 복사형 Elm 모델을 쓰지 않음). 인스턴스는 앱이 한 번 만들고 **모드가 바뀌어도 버리지 않는다** → 입력 중인 글·스크롤·선택이 그대로 남는다.

```go
package ui

type Size struct{ W, H int }

type Component interface {
	Init(ctx *Context) tea.Cmd
	// Update receives every non-input message (broadcast) plus input routed to it (5.3.4).
	Update(ctx *Context, msg tea.Msg) tea.Cmd
	// Resize is called before View whenever the pane's inner size changes (and on mode change).
	Resize(ctx *Context, size Size)
	// View returns EXACTLY size.H lines, each EXACTLY size.W cells wide (use textwidth.Fit).
	View(ctx *Context, focused bool) string
}

// Optional capabilities (type-asserted by the app):
type KeyHandler interface {
	// Focused component gets keys first (after overlays and hard globals). handled=false lets soft globals run.
	HandleKey(ctx *Context, msg tea.KeyPressMsg) (handled bool, cmd tea.Cmd)
}
type MouseHandler interface {
	HandleMouse(ctx *Context, msg tea.MouseMsg, local Point) tea.Cmd // local = coords inside the pane's inner rect
}
type InputCapturer interface{ CapturesInput() bool }   // true while a text field is active: printable keys never trigger soft globals
type Cursorer interface{ Cursor() *tea.Cursor }        // real cursor relative to inner rect (IME!)
type Titled interface{ Title(ctx *Context) (title string, badge Badge) } // pane border title / tab label
type Helper interface{ Help(ctx *Context) []key.Binding }               // status-bar hints (first 4 shown)
type Watcher interface{ WatchTopics() []string }       // visible posts ("post:<id>"), sns-fun
type ChannelViewer interface{ ViewingChannels() []string } // for Store.SetViewing / notify ViewingTarget
type Persistent interface{ SaveState() any; LoadState(any) } // optional, state.json
type Completer interface{ Completing() bool }          // true while an input-owned completion popup is open (CR-A, 5.3.4 2.3)

type Point struct{ X, Y int }
type Badge struct{ Count int; Mention bool; Dot bool } // Mention => BadgeMention style; Dot => "●"
```

#### 5.3.2 Context

```go
type Context struct {
	Theme    *theme.Theme
	Mode     layout.Mode
	Layout   layout.Layout
	Keys     *keymap.Keymap
	Store    *store.Store
	API      *api.Client
	RT       Realtime          // never nil (noop in tests/offline)
	Config   *config.Config
	Paths    config.Paths
	Caps     termcaps.Caps
	Now      func() time.Time  // fixed clock in tests (uitest.FakeClock.Now)
	Timers   Timers            // never nil; RealTimers() in production, uitest.FakeClock in tests
	Loc      *time.Location    // time.Local; Asia/Seoul in tests
	Debug    bool
}

// Timers schedules a delayed message. It is the ONLY way UI code may wait.
type Timers interface {
	// After returns a Cmd that, when run, delivers msg once d has elapsed.
	// RealTimers: tea.Tick(d, func(time.Time) tea.Msg { return msg }).
	// uitest.FakeClock: the Cmd records (now+d, msg) and returns nil immediately; h.Advance delivers it.
	After(d time.Duration, msg tea.Msg) tea.Cmd
}
func RealTimers() Timers
func After(ctx *Context, d time.Duration, msg tea.Msg) tea.Cmd // = ctx.Timers.After(d, msg)

type Realtime interface {
	Typing(channelID string, threadRootID *string)
	SetIdle(idle bool)
	Watch(topics []string)
	State() realtime.State
}

func (c *Context) Me() *api.Me
func (c *Context) Density() Density   // derived from Mode
type Density int
const (DensityFull Density = iota /*command, standard*/; DensityLine /*compact: one-line messages*/; DensityBubble /*widget: KakaoTalk-style bubbles, mine right-aligned*/; DensityMinimal /*tiny*/)
```

**타이머 규칙 [결정]** (모든 계획의 `internal/ui/...` 코드, `widgets` 포함):
- 시간이 지난 뒤에 무언가 하려면 **반드시 `ui.After(ctx, d, msg)`** 를 쓴다. 반복은 받은 메시지를 처리하면서 다시 `ui.After` 를 건다(재무장). 디바운스는 세대 번호를 msg 에 넣고 받은 쪽이 최신 세대만 처리한다.
- UI 코드에서 `tea.Tick`, `tea.Every`, `tea.Sequence`, `time.Sleep`, `time.After`, `time.NewTimer` 를 **쓰지 않는다**(`tea.Cmd` 는 불투명한 함수라 하네스가 구별할 수 없고, `tea.Sequence` 는 비공개 `sequenceMsg` 를 돌려줘 하네스가 펼 수 없다 **[검증: bubbletea v2.1.1 commands.go:30]**). 순서가 필요하면 앞 Cmd 의 결과 메시지를 받은 뒤 다음 Cmd 를 낸다. `tea.Batch`(공개 타입 `tea.BatchMsg`)는 써도 된다.
- 해당 타이머: 새 메시지 페이드(250ms×4), 토스트(4초), `watch` 디바운스(300ms), 유휴(10분), 팔레트 사람 검색 디바운스(300ms), 멤버 접속 상태(60초), 발견 피드 새 글 확인(30초), 트렌드(5분), 스피너 프레임, `ctrl+c` 두 번(1초) 판정. 시각 비교는 `ctx.Now()` 로만 한다.
- bubbles 의 textarea/textinput 은 `SetVirtualCursor(false)` 면 커서 모드가 `CursorHide` 가 되어 `Focus()` 가 깜빡임 Cmd 를 내지 않는다 **[검증: bubbles v2.2.1 textarea.go `updateVirtualCursorStyle`, cursor.go `Focus`]** → 입력 위젯은 항상 실제 커서 모드로 쓴다. bubbles `spinner.Model` 의 `Tick` 은 내부에서 `tea.Tick` 을 쓰므로 직접 쓰지 않고 `widgets.Spinner`(5.7)를 쓴다.
- realtime 클라이언트 내부 타이머(하트비트·백오프·120초 watch 재전송)는 Bubble Tea 밖 고루틴이라 이 규칙 대상이 아니다(하네스는 실시간을 끈다).

#### 5.3.3 경로(Route)와 내비게이션

```go
type RouteKind string
const (
	RouteHome          RouteKind = "home"          // chat: DM·친구·피드 진입 목록 (레일 "홈")
	RouteChannel       RouteKind = "channel"       // chat: ID=channel id (text|dm|group_dm), Anchor=message id
	RouteThread        RouteKind = "thread"        // chat: ID=root message id, Arg=channel id
	RouteSearch        RouteKind = "search"        // chat: Arg=query, ID=channel|space scope ("" global)
	RouteInvite        RouteKind = "invite"        // chat: ID=invite code (link preview); Arg="direct" => direct invite, ID=invite id ("" = list of pending Store.DirectInvites)
	RouteSpaceSettings RouteKind = "space_settings"// chat: ID=space id
	RouteFriends       RouteKind = "friends"       // chat
	RouteFeed          RouteKind = "feed"          // sns-fun: Arg="following"|"discover"
	RoutePost          RouteKind = "post"          // sns-fun: ID=post id
	RouteProfile       RouteKind = "profile"       // sns-fun: ID=user id or "@handle"
	RouteHashtag       RouteKind = "hashtag"       // sns-fun: ID=tag (no '#')
	RouteTrends        RouteKind = "trends"        // sns-fun
	RouteNotifications RouteKind = "notifications" // sns-fun
	RouteStories       RouteKind = "stories"       // sns-fun: ID=user id (start group)
	RouteCompose       RouteKind = "compose"       // sns-fun: Arg="post"|"story", ID=reply_to/quote target, Extra in Query
	RouteSettings      RouteKind = "settings"      // core
	RouteLogin         RouteKind = "login"         // core
	RouteHelp          RouteKind = "help"          // core
)

type Route struct {
	Kind   RouteKind
	ID     string
	Arg    string
	Anchor string            // jump target (message id / post id)
	Query  map[string]string // rare extras (e.g. compose: {"mode":"quote"})
}
func (r Route) Key() string  // Kind + "/" + ID + "/" + Arg  (Anchor/Query excluded => same instance)

type Target int
const (
	TargetAuto Target = iota // placement table below
	TargetMain               // push onto the active tab's stack (pushable kinds only; root/lifted kinds ignore Target)
	TargetSide               // push onto the feed surface stack (pushable kinds only; root/lifted kinds ignore Target)
)

type NavigateMsg struct{ Route Route; Target Target; Replace bool } // Replace: replace the destination stack's top instead of pushing
type BackMsg struct{}
type RouteUpdatedMsg struct{ Route Route } // sent ONLY to a cached instance when navigation reuses it with a different Anchor/Query
func Navigate(r Route) tea.Cmd
func NavigateTo(r Route, t Target) tea.Cmd
func Back() tea.Cmd
```

##### 논리 내비게이션 상태와 투영 [결정]

경로는 **모드와 무관한 논리 상태 `app.NavState`** 에만 기록하고, 매 프레임 결정적 함수 `app.Project` 가 그것을 현재 레이아웃의 슬롯에 **투영**한다. 모드 전환은 `NavState` 를 **한 글자도 바꾸지 않는다** — 투영만 다시 한다. 그래서 컴팩트↔지휘소를 몇 번 오가도 같은 인스턴스가 같은 상태로 다시 나타난다.

```go
package app

type Front int
const (
	FrontTab           Front = iota // single-pane modes show the active tab (its stack top, or its thread)
	FrontFeed                       // ... show the feed surface (standard: FeedSwapped)
	FrontNotifications              // ... show the notifications route
)

type TabState struct {
	Stack  []ui.Route // Stack[0] = tab root (RouteHome | RouteChannel); never RouteThread/RouteFeed/RouteNotifications
	Thread *ui.Route  // this tab's open thread (Kind=RouteThread, Arg=root channel id), nil = closed
}

type NavState struct {
	Tabs   []TabState // open places, max 9 (compact tab bar; command/standard: sidebar-selection history; widget/tiny: the "채팅" tab)
	Active int        // index into Tabs
	Feed   []ui.Route // feed surface stack; Feed[0].Kind == RouteFeed (default {feed, Arg:"following"}); never empty
	Front  Front      // which surface single-pane modes (and standard's FeedSwapped) show in Main
}
func (n NavState) LayoutState(mode layout.Mode) layout.State
func (n NavState) Keys() []string // every route key held anywhere (instance-cache pinning)

// Project maps the logical state onto the panes of l. Pure and deterministic.
// Result: slot -> Route; a slot absent from the map is a pane-factory slot (rail, sidebar, members, tabs, context, status) or empty.
func Project(l layout.Layout, n NavState) map[layout.SlotID]ui.Route
```

알림 경로는 상수 `NotifRoute = ui.Route{Kind: ui.RouteNotifications}`(키 `notifications//`) 하나뿐이다. `T = Tabs[Active]`, `top = T.Stack[len-1]`, `thr = T.Thread`, `F = Feed[len-1]`, `attached = thr != nil && top.Kind == RouteChannel && top.ID == thr.Arg`(스레드는 그 채널이 스택 맨 위일 때만 Main 에 올라온다).

`LayoutState(mode)`: `ThreadOpen = thr != nil && (mode == ModeCommand || Front == FrontTab)`, `FeedSwapped = mode == ModeStandard && Front == FrontFeed`. 앱은 `layout.Compute(mode, cols, rows, nav.LayoutState(mode))` 로 얻은 `Layout` 을 `Project` 에 넘긴다.

`Project` 규칙(첫 일치, 모드별):

| 모드 | `SlotMain` | `SlotThread` | `SlotFeed` | `SlotNotifications` |
|---|---|---|---|---|
| 지휘소 | `top` (`Front` 무시) | `thr`(창이 있을 때 = `ThreadOpen`) | `F` | `NotifRoute` |
| 표준 | `Front==FrontFeed` → `F` · `FrontNotifications` → `NotifRoute` · 그 외: `SlotThread` 창이 없고(아래 6줄 미만) `attached` 면 `thr`, 아니면 `top` | 창이 있으면 `thr` | — | — |
| 컴팩트·위젯·초소형 | `FrontFeed` → `F` · `FrontNotifications` → `NotifRoute` · `FrontTab` → `attached` 면 `thr`, 아니면 `top` | — | — | — |
| TooSmall | 없음 | — | — | — |

- **불변식**: 한 프레임에서 한 경로 키는 **최대 한 슬롯**에만 나타난다. 이를 위해 `NavState` 전체에서도 한 키는 한 곳에만 있다 — 어떤 스택에 키 K 를 넣으면 다른 모든 스택(루트 제외)에서 K 를 뺀다. 루트 종류(`home`·`channel`)는 탭 루트로만, `thread` 는 `TabState.Thread` 로만, `feed` 는 `Feed[0]` 으로만, `notifications` 는 `NotifRoute` 로만 존재하므로 서로 겹치지 않는다.
- 지휘소·표준에서는 스레드·피드·알림이 Main 스택에서 **들어 올려져** 각자 창에 뜨고, Main 은 남은 `top` 을 보인다. 컴팩트·위젯·초소형에서는 `Front` 와 `attached` 규칙으로 Main 하나에 **다시 내려 놓인다**(우선순위: `Front` → 스레드 → 스택 맨 위).
- 위젯·초소형의 고정 3탭은 **별도 스택이 아니다**: `채팅` = `FrontTab`(활성 탭 스택 + 그 스레드, 맥락 줄 `▾` 로 `Active` 전환), `피드` = `FrontFeed`(피드 표면 스택), `알림` = `FrontNotifications`. 컴팩트 탭 줄 = `Tabs` 각 루트 + 고정 `피드`·`알림` 두 칸(5.4).

`NavigateMsg` 처리(`TargetAuto`, 결과는 `NavState` 변경 + 필요 시 포커스 의도):

| 경로 종류 | `NavState` 변경 | 포커스 의도 |
|---|---|---|
| `home`, `channel` | 같은 루트 키의 탭이 있으면 그 탭 활성(스택 유지), 없으면 새 탭 추가 후 활성(9개 초과 시 활성이 아닌 가장 오래 안 본 탭 제거). `Front=FrontTab` | 그 경로 |
| `thread` | `Arg`(채널) 루트 탭을 활성/추가하고, 그 탭 스택을 루트까지 접은 뒤 `Thread=&r`. `Front=FrontTab` | 스레드 경로 |
| `feed` | `Feed=[r]`(피드 표면을 그 루트로 재설정). `Front=FrontFeed` | 피드 경로 |
| `notifications` | `Front=FrontNotifications` | `NotifRoute` |
| `post`, `profile`, `hashtag`, `trends`, `stories`, `compose` (sns 내용) | `Feed` 스택에 push. `Front=FrontFeed` | 그 경로 |
| 그 밖(`search`, `invite`, `space_settings`, `friends`, `settings`, `help`) | 활성 탭 스택에 push. `Front=FrontTab` | 그 경로 |
| `login` | `NavState` 를 비우고 로그인 화면만(셸 밖) | — |

- `TargetMain`/`TargetSide` 는 **push 하는 종류**(위 표의 sns 내용·그 밖 행)의 대상만 바꾼다: `TargetMain` = 활성 탭 스택, `TargetSide` = 피드 표면 스택(예: 지휘소 피드 열 안에서 검색 결과를 이어 보기). 루트·들어 올리는 종류(`home`·`channel`·`thread`·`feed`·`notifications`·`login`)는 `Target` 을 무시한다. `Replace=true` 는 대상 스택 맨 위를 바꾼다(루트는 바꾸지 않음).
- 같은 키의 캐시된 인스턴스를 다시 쓰는데 `Anchor`/`Query` 가 다르면 그 인스턴스에만 `ui.RouteUpdatedMsg{Route}` 를 보낸다(검색 결과 → 메시지로 점프 등).
- **뒤로(`BackMsg`, `esc`)** 는 포커스된 슬롯이 보이는 표면을 한 단계 되돌린다: 포커스가 `SlotThread` → 그 탭 `Thread=nil`. `SlotFeed` → `Feed` 가 2개 이상이면 pop. `SlotNotifications` → 아무것도 안 함. `SlotMain` → Main 이 보이는 것에 따라: 스레드(`attached`) → `Thread=nil` / `F` → 2개 이상이면 pop, 아니면 `Front=FrontTab` / `NotifRoute` → `Front=FrontTab` / `top` → 스택이 2개 이상이면 pop(루트는 pop 하지 않음).
- `f`(`feed.toggle`): 지휘소 → 포커스를 `SlotFeed` 로. 그 외 → `Front` 를 `FrontTab`↔`FrontFeed` 토글. `ctrl+n`(`notifications.open`): 지휘소 → 포커스를 `SlotNotifications` 로, 그 외 → `Front` 를 `FrontNotifications`↔`FrontTab` 토글.
- 앱은 경로별 컴포넌트 인스턴스를 `Route.Key()` 로 캐시한다(최근 32개, 그 이상은 가장 오래 안 본 것부터 `Persistent` 상태만 남기고 해제 — 단 `NavState.Keys()` 에 있는 키는 해제하지 않는다). 같은 채널로 돌아오면 같은 인스턴스(초안·스크롤 유지). 한 인스턴스는 한 프레임에 한 슬롯에만 있으므로 `Resize` 크기는 늘 하나다.

#### 5.3.4 메시지 흐름과 라우팅

`app.Update` 처리 순서:

1. `tea.WindowSizeMsg` → `layout.Resolve` → `layout.Compute(…, nav.LayoutState(mode))` → `app.Project`(5.3.3) → 슬롯에 놓인 인스턴스 중 내부 크기가 바뀐 것마다 `Resize` → 모드가 바뀌면 `ui.ModeChangedMsg{From, To}` 방송 + 포커스 재배치(5.3.5). `NavState` 가 바뀐 뒤(`NavigateMsg`·`BackMsg`·탭 전환)에도 같은 Compute→Project→Resize 를 한다.
2. `tea.KeyPressMsg`:
   1. 오버레이(팔레트·대화상자·도움말)가 열려 있으면 **맨 위 오버레이**로(`ctrl+c` 제외). 그 오버레이의 `HandleKey` 가 `handled=false` 를 돌려줘도 아래로 내려가지 않는다(`Modal()==false` 인 오버레이만 예외로 3~6단계 계속).
   2. 진행 중인 코드(`ctrl+w` 접두)가 있으면 그 처리.
   3. **하드 전역**: `ctrl+c`, `ctrl+k`, `ctrl+f`, `ctrl+n`, `tab`/`shift+tab`(포커스가 입력 캡처 중이고 그 입력이 자동완성 팝업을 띄운 경우에는 컴포넌트로; 포커스 링이 창 1개인 모드에서는 창 순환 대신 탭 순환 — 5.3.5). `ctrl+w` 는 포커스가 입력 캡처 중이 **아닐 때만** 창 접두로 쓰이고, 입력 중이면 입력창의 "단어 지우기"로 간다(`esc` 로 입력을 빠져나온 뒤 `ctrl+w`).
      - **자동완성 중 `tab`(CR-A)**: 하드 전역 `tab`/`shift+tab` 판정(창 1개 모드의 `tab.next`/`tab.prev` 포함) **전에**, 포커스 컴포넌트가 `CapturesInput() && Completing()`(`ui.InputCapturer`·`ui.Completer`) 이면 그 키를 `HandleKey` 로 넘긴다. `handled=true` 면 끝(포커스·`NavState` 불변), `false` 면 원래 하드 전역으로 처리. 둘 중 하나라도 아니면(입력 캡처 아님·`Completer` 미구현·팝업 없음) 그대로 하드 전역.
   4. 포커스 컴포넌트의 `HandleKey`. `handled=true` 면 끝.
   5. 포커스 컴포넌트가 `CapturesInput()==true` 면 여기서 끝(글자 키가 전역 단축키로 새지 않음).
   6. **소프트 전역**: `?`, `f`, `[`, `]`, `esc`(뒤로), `g` 계열.
3. 마우스 → `layout.At(x,y)` 의 창: 클릭이면 그 창으로 포커스 이동 후 `HandleMouse(local)`, 휠은 포커스와 무관하게 포인터 아래 창으로. 오버레이가 열려 있으면 오버레이가 먼저(바깥 클릭 = 닫기).
4. `tea.PasteMsg` → 오버레이가 열려 있으면 **맨 위 오버레이**(`Update`), 아니면 포커스 컴포넌트(`Update`). 오버레이가 열려 있는 동안 아래 창으로는 절대 가지 않는다.
5. `realtime.EventMsg`(앱 브리지가 감싼 것) → `Store.Apply(ev)` → **모든 살아 있는 컴포넌트에 방송** → 알림 처리(`alert` 이면 notify, 9장) → `rt.MarkProcessed(seq)`.
6. 그 밖 모든 메시지(API 결과, 틱, 내부 메시지) → 모든 살아 있는 컴포넌트에 방송. 컴포넌트는 자기와 관계없는 것은 무시한다. 단 `ui.RunCommandMsg` 는 방송하지 않는다(아래).
7. **`ui.RunCommandMsg{ID, Args}` 는 앱이 소비한다(방송하지 않음 — 명령이 두 번 실행되지 않게, CR-B)**: ① `palette.Lookup(ID)`(5.6) → 없으면 무시(`ctx.Debug` 일 때 slog debug, 토스트 없음). ② 맨 위 오버레이가 앱이 `ctrl+k` 로 연 팔레트면 실행 **전에** 먼저 닫는다(명령이 여는 오버레이가 팔레트 위에 쌓이지 않게). ③ `Available != nil && !Available(ctx)` 면 실행하지 않고 토스트 `지금은 쓸 수 없는 명령이에요`(`ToastInfo`). ④ 아니면 `Run(ctx, Args)` 의 Cmd 를 실행 — `Args` 는 손대지 않고 **그대로**(자르기·정규화는 각 명령의 몫), `Run == nil` 이면 무시. 팔레트 안 `enter`(명령 항목)도 직접 `Run` 을 부르지 않고 `ui.RunCommand(c.ID, "")` 를 내어 같은 디스패치를 탄다.

앱이 쓰는 공용 메시지(`internal/ui`):

```go
type EventMsg struct{ Event realtime.Event }           // from realtime bridge
type RealtimeStateMsg struct{ State realtime.State }
type ModeChangedMsg struct{ From, To layout.Mode }
type ThemeChangedMsg struct{ Theme *theme.Theme }
type SetThemeMsg struct{ Name theme.Name }
type FocusChangedMsg struct{ Slot layout.SlotID; RouteKey string }
type ToastMsg struct{ Text string; Kind ToastKind; Route *Route } // shown 4s (ui.After) as a bordered box at the body's top-right (status line in tiny); rect reported in app.FrameInfo.Toast
type ToastKind int
const (ToastInfo ToastKind = iota; ToastSuccess; ToastError; ToastAlert)
type ErrorMsg struct{ Err error; Context string }        // => ToastError with api.Describe(err)
type OpenOverlayMsg struct{ Overlay Overlay; ID, Args string } // Overlay != nil => push it; else build RegisterOverlay(ID) with Args (unknown ID => ignored + debug log)
type CloseOverlayMsg struct{}
type RunCommandMsg struct{ ID string; Args string }      // palette command by id
type LoggedOutMsg struct{ Reason string }
type ResyncStartedMsg struct{}
type ResyncDoneMsg struct{ Err error }

func Toast(text string, kind ToastKind) tea.Cmd
func RunCommand(id, args string) tea.Cmd
func OpenOverlay(o Overlay) tea.Cmd
func OpenRegisteredOverlay(id, args string) tea.Cmd // OpenOverlayMsg{ID: id, Args: args}

// API helper: runs fn off the UI goroutine and returns ResultMsg[T]{Tag, Value, Err}.
// In the uitest harness the returned Cmd runs synchronously (apitest server), like every other non-timer Cmd.
type ResultMsg[T any] struct{ Tag string; Value T; Err error }
func Do[T any](c *Context, tag string, fn func(ctx context.Context, api *api.Client) (T, error)) tea.Cmd
```
- **태그 규칙**: `"<계획>.<동작>:<대상ID>"`, 예 `"chat.history:01JB…"`, `"feed.like:01JD…"`. 각 계획은 자기 접두사(`core.`, `chat.`, `feed.`, `fun.`)만 쓴다.
- 오류는 컴포넌트가 처리하지 않으면 `ErrorMsg` 를 돌려 앱이 토스트로 띄운다(`api.Describe`).

#### 5.3.5 포커스 관리

- 앱은 **포커스 의도**(`FocusIntent{Slot, RouteKey}`)를 기억한다. `RouteKey` 는 경로 슬롯이면 그 경로 키, 창 팩토리 슬롯(`rail`·`sidebar`·`members`)이면 `""`. 포커스 링 = 현재 레이아웃의 테두리 있는 창들, 순서 `rail → sidebar → main → thread → feed → members → notifications`.
- `tab`/`shift+tab` 순환, `ctrl+w` + `h/j/k/l`(또는 화살표) 는 창 기하 기준 방향 이동, `ctrl+w w` 다음 창. **포커스 링에 창이 1개뿐인 모드(컴팩트·위젯·초소형)** 에서는 `tab`/`shift+tab` 이 `tab.next`/`tab.prev` 로 동작한다(5.5).
- 프레임마다 포커스 슬롯 결정(첫 일치): (1) 의도의 `RouteKey` 가 비어 있지 않고 `Project` 결과에서 그 키를 보이는 슬롯이 있으면 **그 슬롯**(경로를 따라간다 — 컴팩트에서 Main 에 뜬 스레드에 포커스가 있었다면 지휘소로 바뀔 때 `SlotThread` 로). (2) 의도의 `Slot` 이 레이아웃에 있으면 그 슬롯. (3) 대체: `rail|sidebar|members|thread|feed|notifications → main`. **의도는 바꾸지 않고 남겨 두었다가** 그 경로/창이 다시 보이면 복원한다. 사용자가 포커스를 옮기거나(`tab`, 클릭, `ctrl+w`) 내비게이션이 포커스 의도를 정하면(5.3.3 표) 그때만 의도를 갱신한다.
- 모드 전환 시 `Front` 는 바뀌지 않는다: 위젯 `피드` 탭(`FrontFeed`)에서 지휘소로 가면 피드 인스턴스가 `SlotFeed` 에 뜨고 포커스도 (1) 규칙으로 `SlotFeed`, 다시 위젯으로 오면 `피드` 탭이 그대로 선택되어 있다.
- 컴포넌트 안의 하위 포커스(예: 메시지 목록 ↔ 입력창)는 컴포넌트 내부 상태이며 모드가 바뀌어도 유지된다.
- 포커스된 창 테두리는 `acc`, 나머지는 `line`. 초소형은 테두리가 없으므로 탭 줄의 현재 탭 강조로 대신한다.

#### 5.3.6 등록 레지스트리 (chat/sns-fun 이 `init()` 에서 호출)

```go
type PaneFactory func(ctx *Context) Component
type RouteFactory func(ctx *Context, r Route) Component

func RegisterPane(slot layout.SlotID, f PaneFactory)   // ONLY rail, sidebar, members (chat). Route slots (main, thread, feed, notifications) => panic. Duplicate => panic.
func RegisterRoute(kind RouteKind, f RouteFactory)     // see RouteKind owners above. Duplicate => panic.
func RegisterOverlay(id string, f func(ctx *Context, args string) Overlay) // id has the plan prefix ("fun.", "chat.", ...). Duplicate => panic.
func Registered() Registry                             // used by app & tests

// Well-known overlay ids opened by core (ui/app) but registered by another plan.
const OverlayFocusSummary = "fun.focus_summary" // sns-fun ui/fun; args = alert id (9.1)
```

- 등록되지 않은 슬롯/경로는 앱이 `widgets.Placeholder("준비 중")` 로 그린다 → core 골든은 chat/sns 없이도 깨지지 않는다. core 시험은 `uitest/stub`(10.2)을 등록해 실제 상호작용을 시험한다.
- **경로 슬롯** `SlotMain`·`SlotThread`·`SlotFeed`·`SlotNotifications` 는 PaneFactory 가 없고 `app.Project`(5.3.3) 가 정한 **경로 인스턴스**를 띄운다: `SlotFeed` ← 피드 표면 맨 위(`RouteFeed` 등), `SlotNotifications` ← `RouteNotifications` 인스턴스, `SlotThread` ← 활성 탭의 `RouteThread` 인스턴스. 그래서 모드가 바뀌어 같은 경로가 Main 으로 내려오거나 전용 창으로 올라가도 **같은 인스턴스**(선택·스크롤·초안 유지)다.

#### 5.3.7 오버레이

```go
type Overlay interface {
	Component
	Bounds(body layout.Rect) layout.Rect // desired OUTER box (border included) inside body; centered box typically
	Modal() bool                         // true: blocks everything below
}
```
- 앱은 기본 화면을 그린 뒤 `lipgloss.NewCompositor(base, overlayLayer.Z(10+i))` 로 겹친다. 오버레이 스택 최대 3.
- **테두리와 크기는 앱이 정한다 [결정]**: `body` = 화면에서 상태줄을 뺀 사각형. 앱은 `r = Bounds(body) ∩ body` 로 자른다. 그 뒤 (a) 모드가 초소형이거나, (b) `r.W < 24 || r.H < 6` 이면 **`r = body`, 테두리 없음**(전체 본문을 덮는 강등 모드), 아니면 `r` 둘레에 테마 `Border` + `S.OverlayBorder` 로 테두리를 그리고(제목은 `Titled` 가 있으면 그것) 컴포넌트에는 `r.Inner()` 크기로 `Resize`/`View` 한다. TooSmall 에서는 오버레이를 그리지 않는다(열린 상태는 유지). 앱은 최종 사각형과 테두리 여부를 `app.FrameInfo.Overlays` 로 알린다(10.2 무결성 검사).
- **커서·붙여넣기 [결정]**: 오버레이가 하나라도 열려 있으면 `View.Cursor` = 맨 위 오버레이의 `Cursorer` 결과 + 그 오버레이 내부 원점(테두리 있으면 `r.X+1, r.Y+1`, 없으면 `r.X, r.Y`). 맨 위 오버레이가 `Cursorer` 가 아니거나 nil 을 주면 **`View.Cursor = nil`**(아래 창의 입력 커서를 보이지 않는다 — IME 조합창이 엉뚱한 곳에 뜨지 않게). `tea.PasteMsg` 는 맨 위 오버레이로만(5.3.4 4단계).
- 텍스트 입력이 있는 오버레이(팔레트, 대화상자의 입력 칸)는 bubbles `textinput` 을 `SetVirtualCursor(false)` 로 쓰고 `Cursor()` 를 `Cursorer` 로 내보낸다. `textinput.KeyMap.DeleteAfterCursor`(기본 `ctrl+k` **[검증: bubbles v2.2.1 textinput.go:76]**)에서 `ctrl+k` 를 **뺀다** — 오버레이가 키를 먼저 받으므로 `ctrl+k` 는 팔레트에서 "닫기(토글)", 다른 입력 오버레이에서는 "닫고 팔레트 열기"로 처리한다. 입력 글자·붙여넣기는 `textwidth.SanitizeInput` 을 거쳐 넣는다(8.4).
- core 제공 오버레이: 명령 팔레트, 도움말(`?`), 확인 대화상자(`widgets.Confirm`), 이모지 선택기(`widgets.EmojiPicker`, chat 리액션·sns 상태에 공용), 신고 대화상자(`widgets.ReportDialog`, chat·sns-fun 공용).

### 5.4 루트 앱 — `internal/ui/app` (core)

```go
package app

type Options struct {
	Paths        config.Paths
	Config       *config.Config
	Credentials  *auth.Credentials // nil => login screen
	InitialRoute *ui.Route         // `masil join <code>` => RouteInvite
	Now          func() time.Time
	Loc          *time.Location
	DisableRealtime bool           // tests
	Notifier     *notify.Notifier  // nil => notify.New(NewOSBackend("masil"), OpenLedger(paths), log)
	API          *api.Client       // nil => api.New(config.ServerURL(), WithToken(...))
	Timers       ui.Timers         // nil => ui.RealTimers(); uitest passes its FakeClock
}

type Model struct{ /* implements tea.Model */ }
func New(o Options) *Model
func (m *Model) Init() tea.Cmd
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m *Model) View() tea.View
func (m *Model) Context() *ui.Context // tests
func (m *Model) Nav() NavState        // tests: current logical navigation state (5.3.3)
func (m *Model) Instance(routeKey string) ui.Component // tests: cached route instance or nil
func (m *Model) Focus() FocusIntent   // tests: resolved focus slot + route key this frame
func (m *Model) Frame() FrameInfo     // tests: geometry of the last View() (10.2 AssertIntegrity)

type FocusIntent struct{ Slot layout.SlotID; RouteKey string }

type FrameInfo struct {
	Layout   layout.Layout
	Routes   map[layout.SlotID]ui.Route // Project result
	Overlays []Box                      // bottom -> top, final clamped rects (5.3.7)
	Toast    *Box                       // nil when no toast box is drawn (or tiny: shown in status line)
}
type Box struct{ Rect layout.Rect; Bordered bool }

func Run(ctx context.Context, o Options) error // AcquireTUI lock, build tea.Program (alt screen via View), start realtime bridge, run, save state.json on exit
```

`View()` 가 매 프레임 설정하는 것: `AltScreen=true`, `MouseMode=MouseModeCellMotion`(설정 `ui.mouse=false` 면 None), `ReportFocus=true`, `Cursor`=오버레이가 열려 있으면 5.3.7 규칙(맨 위 오버레이 기준, `Cursorer` 아니면 nil), 아니면 포커스 컴포넌트의 `Cursorer` 결과에 그 창 내부 원점을 더한 값(없으면 nil), `WindowTitle="masil · <현재 경로 제목>"`(경로 제목은 `currentTitle` 에서 `textwidth.Sanitize` — 제어 문자뿐 아니라 bidi·보이지 않는 문자도 지운다. 창·탭 제목은 터미널 격자가 아니라 OS GUI 글자 엔진이 bidi 를 온전히 적용해 그리므로 DM 상대 이름의 U+202E 가 제목을 뒤집을 수 있다. 시험: `uitest.AssertTitle`, pending-fixes 0-b·1), `BackgroundColor/ForegroundColor`(5.1).

시작 순서: 자격 증명 없음 → `RouteLogin`. 있음 → realtime `Run`(full) 시작, `PhaseSyncing` 동안 상태줄 "동기화 중…" → `ready` 를 받으면 `store.Bootstrap` → `ApplyBootstrap` → 각 Resyncer. 소켓이 5초 안에 `ready` 를 못 주면(오프라인 등) REST 부트스트랩을 먼저 하고, 나중에 `ready` 가 오면 한 번 더 재동기화한다.

유휴 감지: 마지막 키·마우스 입력 후 10분 → `RT.SetIdle(true)`, 입력 시 `false`(`ui.After` 세대 번호 방식).
`watch` 갱신: 레이아웃·스크롤 변화 후 300ms 디바운스(`ui.After`)로 보이는 `Watcher` 들의 토픽 합집합(포커스 창 우선, 최대 50)을 `RT.Watch`.
`Store.SetViewing`(CR-C): 투영 결과에 놓인 인스턴스 중 `ChannelViewer` 들의 `ViewingChannels` 합집합. **Compute→Project(→Resize) 직후, 같은 `Update` 안에서, 그 `Update` 가 내는 방송(`ModeChangedMsg`·`FocusChangedMsg`·`RouteUpdatedMsg`·`EventMsg` 등)보다 먼저** 부른다 — 방송을 받은 컴포넌트(chat 의 읽음 판정)가 직전 프레임이 아니라 이번 투영의 Viewing 집합을 보게. 투영이 바뀌지 않는 Update(`EventMsg` 등)에서는 직전 집합이 그대로 맞다. `View` 에서는 부르지 않는다(View 는 순수).

상태줄(`SlotStatus`, 1줄): `[모드 pill] 힌트 ··· 오른쪽: 집중 남은 시간(🎯 18:20) · 429 쿨다운 · 📌(고정) · 연결 상태(● 온라인 / ◐ 자리비움 / ⊘ 방해금지 / ◌ 연결 중… / ✕ 오프라인)`. 폭이 모자라면 오른쪽 항목을 연결 상태만 남기고 힌트를 자른다.

힌트 **[결정]** — 모드별 기본 힌트(시안 `layouts.html` 문구 그대로)를 먼저 쓰고, 포커스 컴포넌트가 `Helper` 면 그 바인딩 중 기본 힌트와 키가 겹치지 않는 것을 ` · ` 로 이어 붙인다(전체 항목 최대 5개, 폭 넘치면 뒤부터 자름). 포커스 컴포넌트가 `CapturesInput()==true` 인 동안은 기본 힌트 대신 `enter 보내기 · shift+enter 줄바꿈 · esc 나가기`. 오버레이가 열려 있으면 그 오버레이의 `Helper`(없으면 `esc 닫기`).

| 모드 | 기본 힌트 |
|---|---|
| 지휘소 | `ctrl+k 이동 · tab 패널 전환 · t 스레드 · ? 도움말` |
| 표준 | `f 피드 · t 스레드 · ctrl+k` |
| 컴팩트 | `[ ] 탭 이동 · enter 스레드` |
| 위젯 | `tab 탭` |
| 초소형 | (없음 — `[초소형] ●` 만) |

탭(`SlotTabs`):
- 컴팩트: `NavState.Tabs` 의 각 루트(`#개발`, `#일반 3`, `민지 ●`) 다음에 고정 `피드`·`알림 2` 두 칸(시안 순서). 현재 탭(`Front==FrontTab` 이면 `Tabs[Active]`, 아니면 `피드`/`알림`)은 `TabActive`, 고정 두 칸은 비활성일 때 `dim`. `[`/`]`·`tab`/`shift+tab` 이 이 순서로 순환, 클릭 이동, 넘치면 현재 탭이 보이도록 가로 스크롤 + 양 끝 `‹ ›`.
- 위젯: 고정 3탭 `채팅 / 피드 / 알림(배지)` = `FrontTab / FrontFeed / FrontNotifications`(5.3.3). 맥락 줄 `SlotContext` = 활성 탭 경로 제목 + 방 이름 + `▾`(클릭·`enter` 로 채널 전환 팔레트 → `Active` 변경). `[`/`]`·`tab`/`shift+tab` 이 3탭 순환.
- 초소형: `챗·피·알` 한 글자 축약 + 배지, 현재 탭만 pill. 동작은 위젯과 같다.

### 5.5 키맵 — `internal/ui/keymap`

```go
package keymap

type Action string // stable ids, used in config [keys]
type Keymap struct{ /* action -> key.Binding */ }
func Default() *Keymap
func Load(overrides map[string][]string) (*Keymap, []error) // unknown action/invalid key => error, default kept
func (k *Keymap) Binding(a Action) key.Binding
func (k *Keymap) Matches(msg tea.KeyPressMsg, a Action) bool
func (k *Keymap) Help(as ...Action) []key.Binding
```

기본 키 **[결정]** (vim + 화살표 둘 다):

| Action | 키 | 범위 |
|---|---|---|
| `app.quit` | `ctrl+c` ×2(1초 안) — 입력창에 글이 있으면 첫 번째는 입력 지우기 | 하드 |
| `palette.open` | `ctrl+k` | 하드 |
| `search.global` | `ctrl+f` | 하드 |
| `notifications.open` | `ctrl+n` | 하드 |
| `window.prefix` | `ctrl+w` 다음: `h j k l`/`← ↓ ↑ →` 방향 포커스, `w` 다음 창, `1` 지휘소 `2` 표준 `3` 컴팩트 `4` 위젯 `5` 초소형 `0` 자동(고정 해제) | 하드(입력 캡처 중 제외) |
| `focus.next` / `focus.prev` | `tab` / `shift+tab` — 포커스 링이 창 1개인 모드(컴팩트·위젯·초소형)에서는 `tab.next` / `tab.prev` 로 동작 | 하드 |
| `help.toggle` | `?`, `f1` | 소프트 |
| `feed.toggle` | `f` | 소프트 |
| `tab.prev` / `tab.next` | `[` / `]` | 소프트 |
| `nav.back` | `esc` | 소프트 |
| `list.up` / `list.down` | `k`,`↑` / `j`,`↓` | 목록 |
| `list.top` / `list.bottom` | `g g`,`home` / `G`,`end` | 목록 |
| `list.pageup` / `list.pagedown` | `ctrl+u`,`pgup` / `ctrl+d`,`pgdown` | 목록 |
| `list.open` | `enter` | 목록 |
| `compose.focus` | `i`, `a` | 대화·피드 |
| `compose.send` | `enter` | 입력 |
| `compose.newline` | `shift+enter`, `alt+enter`, `ctrl+j` | 입력 |
| `compose.complete` | `tab`(자동완성 팝업 있을 때) | 입력 |
| `compose.edit_last` | `↑`(입력이 비었을 때) | 입력 |
| `compose.leave` | `esc` | 입력 |
| `msg.thread` | `t`, `enter` — 선택된 메시지에서 스레드 열기(답글이 없어도 새 스레드로). **모든 모드 공통**: 지휘소·표준은 `SlotThread`, 컴팩트·위젯·초소형은 Main 교체(5.3.3). 메시지 목록에서 `enter` 는 `list.open` 대신 이것이다 | 메시지 선택 |
| `msg.react` | `r` | 메시지 선택 |
| `msg.edit` | `e` | 메시지 선택(내 것) |
| `msg.delete` | `d` (확인 대화상자) | 메시지 선택 |
| `msg.pin` | `p` | 메시지 선택 |
| `msg.copy` | `y` (`tea.SetClipboard`) | 메시지 선택 |
| `msg.actions` | `m` (동작 메뉴) | 메시지 선택 |
| `channel.search` | `/` | 대화 |
| `post.like` | `l` | 글 선택 |
| `post.repost` | `b` | 글 선택 |
| `post.quote` | `q` | 글 선택 |
| `post.reply` | `r` | 글 선택 |
| `post.open_media` | `o` | 글 선택 |
| `post.author` | `u` | 글 선택 |
| `post.compose` | `c` | 피드 |

- 같은 키가 여러 범위에 있으면 좁은 범위가 이긴다: 입력 > 메시지/글 선택 > 목록 > 소프트 전역. 예: 입력창 포커스의 `enter` = `compose.send`, 메시지 선택 중 `enter` = `msg.thread`, 채널 목록의 `enter` = `list.open`.
- 투표 메시지의 투표는 `msg.actions`(`m`) 메뉴 또는 투표 칸 클릭으로 한다(`enter` 는 늘 스레드).

마우스: 클릭 = 포커스 + 항목 선택, 선택된 항목 다시 클릭 = 열기, 휠 = 포인터 아래 창 3줄 스크롤, 탭·레일·목록 클릭 = 이동, 리액션 칩 클릭 = 토글, 오버레이 바깥 클릭 = 닫기.

### 5.6 명령 팔레트 — `internal/ui/palette` (core)

```go
package palette

type Command struct {
	ID        string            // "theme.cycle", "layout.pin.command", "status.set", ...
	Title     string            // Korean, e.g. "테마: 레트로 앰버"
	Keywords  []string
	Group     string            // "이동" | "명령" | "설정"
	KeyHint   string            // "ctrl+w 1"
	Available func(ctx *ui.Context) bool
	Run       func(ctx *ui.Context, args string) tea.Cmd
}
func Register(c Command)                  // init()-time; duplicate id => panic
func Lookup(id string) (Command, bool)    // registered command by id (copy, Keywords copied too) — CR-B; app's RunCommandMsg dispatch (5.3.4 7)

type ItemKind int
const (ItemChannel ItemKind = iota; ItemDM; ItemSpace; ItemPerson; ItemCommand; ItemRoute)
type Item struct {
	Kind     ItemKind
	Title    string
	Subtitle string
	Badge    ui.Badge
	Route    *ui.Route
	Command  *Command
	Score    int
}
type Source interface{ Items(ctx *ui.Context, query string) []Item }
func RegisterSource(s Source)

func Open(ctx *ui.Context, initial string) ui.Overlay // ctrl+k
func Chosung(s string) string                        // "개발" => "ㄱㅂ" (Korean initial-consonant search key)
```
- 검색: `sahilm/fuzzy` 로 `Title`, `Chosung(Title)`, `Keywords` 를 각각 맞춰 최고 점수. 입력이 비면 최근 이동 8개 + 안 읽은 채널.
- core 기본 소스: 채널·DM·방(Store), 친구, 명령. 2글자 이상이면 `SearchUsers` 를 300ms 디바운스로 불러 사람 항목 추가.
- 팔레트 입력은 `textinput`(실제 커서, `ctrl+k` 를 `DeleteAfterCursor` 에서 뺌, 5.3.7). 팔레트가 열린 상태의 `ctrl+k` = 닫기.
- **명령 실행(CR-B)**: 팔레트 안 `enter`(명령 항목)는 `Run` 을 직접 부르지 않고 `ui.RunCommand(c.ID, "")` 만 낸다. 팔레트·슬래시 명령·시험의 `RunCommandMsg` 가 모두 앱의 같은 디스패치(5.3.4 7단계: `Lookup` → 팔레트 먼저 닫기 → `Available` 재확인·`지금은 쓸 수 없는 명령이에요` 토스트 → `Run(ctx, Args)` 에 `Args` 그대로)를 탄다. `RunCommandMsg` 는 방송되지 않으므로 컴포넌트가 받아 따로 실행하지 않는다.
- 명령 id 는 **등록 패키지별로 겹치지 않게** 나눈다(`Register` 는 중복 시 패닉). 등록 패키지 = 아래 표의 패키지 `init()` 하나뿐.

| 등록 패키지 | 명령 id |
|---|---|
| core `ui/app` | `theme.cycle`, `theme.set.<name>`, `layout.pin.<mode>`, `layout.auto`, `help`, `settings`, `blocks`(설정의 차단 목록 섹션으로), `logout`, `quit`, `reconnect` |
| chat `ui/chat` | `space.create`, `space.join`, `space.leave`, `space.settings`, `channel.create`, `channel.prefs`, `dm.new`, `friends`, `friend.add`, `invite.create`, `invite.direct`, `invites.inbox`(받은 방 초대 목록), `search.messages`, `pins.show` |
| sns-fun `ui/feed` | `post.new`, `story.new`, `feed.following`, `feed.discover`, `trends`, `notifications`, `profile.me`, `profile.edit`, `follow_requests` |
| sns-fun `ui/fun` | `status.set`, `status.clear`, `presence.set`, `focus.start`, `focus.stop` |

### 5.7 공용 위젯 — `internal/ui/widgets` (core)

```go
package widgets

// Frame draws a themed pane border with title/badge; content is fit to inner size.
func Frame(t *theme.Theme, title string, badge ui.Badge, focused bool, size ui.Size, content string) string
func Box(t *theme.Theme, style lipgloss.Style, size ui.Size, content string) string // nested box (composer input)
func Placeholder(text string) ui.Component

type List[T any] struct{ /* selection by item ID, anchored scroll */ }
func NewList[T any](idOf func(T) string, render func(ctx *ui.Context, item T, selected bool, width int) []string) *List[T]
func (l *List[T]) SetItems(items []T)                 // keeps selection & top anchor by ID
func (l *List[T]) Selected() (T, bool)
func (l *List[T]) Select(id string)
func (l *List[T]) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd)
func (l *List[T]) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd
func (l *List[T]) View(ctx *ui.Context, size ui.Size) string
func (l *List[T]) Anchor() ScrollAnchor
func (l *List[T]) Restore(a ScrollAnchor)
func (l *List[T]) AtBottom() bool
func (l *List[T]) Follow(bottom bool)                 // stick to bottom on new items (chat)

// ScrollAnchor makes scroll position width-independent: survives re-wrap on mode/width change.
type ScrollAnchor struct{ ItemID string; LineOffset int; AtBottom bool }

type Composer struct{ /* textarea wrapper */ }
func NewComposer(placeholder string) *Composer        // textarea: virtual cursor OFF, no line numbers, no prompt, CharLimit from limits;
                                                      // textarea KeyMap: remove ctrl+k (DeleteAfterCursor), ctrl+f (CharacterForward), ctrl+n (LineNext) — they are hard globals;
                                                      // InsertNewline = shift+enter, alt+enter, ctrl+j (enter is send)
                                                      // INPUT NORMALIZATION (8.4): KeyPressMsg.Text, PasteMsg.Content and SetValue go through
                                                      // textwidth.SanitizeInput BEFORE reaching the textarea, so the textarea only ever holds width-stable text
func (c *Composer) Value() string                     // already normalized; this exact string is what gets sent
func (c *Composer) SetValue(s string)                 // normalizes (edit-last loads the server body through this)
func (c *Composer) Reset()
func (c *Composer) Focus() tea.Cmd
func (c *Composer) Blur()
func (c *Composer) Focused() bool
func (c *Composer) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd
func (c *Composer) View(ctx *ui.Context, width, maxRows int) string // auto-grows 1..maxRows, bordered with acc when focused
func (c *Composer) Height(width, maxRows int) int
func (c *Composer) Cursor() *tea.Cursor               // relative to Composer.View origin
func (c *Composer) SetCompletion(items []Completion)  // @mention / :emoji / /command popup above the input
type Completion struct{ Label, Insert, Detail string }

type Tabs struct{ /* horizontal tab bar with badges and overflow arrows */ }
func NewTabs() *Tabs
func (t *Tabs) Set(items []Tab, active int)
func (t *Tabs) View(th *theme.Theme, width int) string
func (t *Tabs) Hit(x int) (index int, ok bool)
type Tab struct{ Label string; Badge ui.Badge }

// Spinner: theme-colored dot spinner. Uses bubbles spinner.Dot frames only; ticks via ui.After (never spinner.Model.Tick).
// config ui.animations=false => static first frame, Start returns nil.
type Spinner struct{ /* frame index, generation */ }
func NewSpinner() *Spinner
func (s *Spinner) Start(ctx *ui.Context) tea.Cmd
func (s *Spinner) Stop()
func (s *Spinner) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd // consumes its own tick msg and re-arms
func (s *Spinner) View(t *theme.Theme) string
func Badge(t *theme.Theme, b ui.Badge) string        // " 3 ", " 99+ ", "●"
func PresenceDot(t *theme.Theme, s api.PresenceStatus) string
func Avatar(t *theme.Theme, u api.UserSummary, w int) string // text avatar: initial in user color chip (images are sns-fun)
func UserName(t *theme.Theme, u api.UserSummary, self bool) string
func ReactionChips(t *theme.Theme, rs []api.ReactionSummary, width int) []string
func PollView(t *theme.Theme, p api.Poll, width int, selected int) []string // bars "국밥 ████████░░ 4", chat + feed
func Empty(t *theme.Theme, size ui.Size, icon, title, hint string) string
func Confirm(title, body, yes, no string, onYes tea.Cmd) ui.Overlay
func EmojiPicker(onPick func(emoji string) tea.Cmd) ui.Overlay // recent + common set, search by Korean/English name
// ReportDialog: reason list (spam|harassment|illegal|sexual|violence|other, Korean labels) + optional note (textinput, 5.3.7 rules),
// submits api.Report via ui.Do tag "core.report:<targetID>"; 201 => ToastSuccess "신고했어요", 409 already_exists => ToastInfo "이미 신고한 대상이에요".
// targetType: user|message|post|story|guestbook_entry|space (api.md 5.21). label = what is shown as the target ("민지의 메시지").
func ReportDialog(targetType, targetID, label string) ui.Overlay
func Toasts(t *theme.Theme, items []ui.ToastMsg, width int) []string

// Fade: new-item highlight, selbg -> bg in 4 steps x 250ms via ui.After (config ui.animations=false => none, Start returns nil)
type Fade struct{ /* per item id */ }
func (f *Fade) Start(ctx *ui.Context, id string) tea.Cmd
func (f *Fade) Style(t *theme.Theme, id string) lipgloss.Style
func (f *Fade) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd
```

### 5.8 core 화면

- `internal/ui/authui`: 로그인·가입 화면(`RouteLogin`). 서버 주소 표시, handle/비밀번호/표시 이름, 오류는 `api.Describe`. 성공 시 `auth.Save` + `LoggedInMsg{Credentials, Me}` → 앱이 API 토큰 설정·realtime 시작.
- `internal/ui/settings`(`RouteSettings`): 테마(미리보기 4종), 레이아웃 고정, 마우스·이미지·애니메이션, 데스크톱 알림(로컬 스위치 + 서버 `notify.*` 토글), DM 정책, 비공개 계정, 집중 기본 시간, **차단 목록**(`ListBlocks` 로 표시 — `Store.Blocks` 와 같은 집합, 항목마다 `Unblock` + `widgets.Confirm`, `block.updated` 이벤트로 갱신; 팔레트 `blocks` 가 이 섹션으로 연다), 세션 목록·폐기, 비밀번호 변경, 로그아웃.
- **차단·신고 동작의 소유 [결정]**: 차단 **해제 목록**은 core(설정). 차단·신고를 **거는** 동작은 대상이 보이는 화면의 소유 계획이 단다 — chat: 메시지(`message`), 메시지 작성자·멤버 목록의 사용자(`user`), 방(`space`, 방 설정·초대 미리보기); sns-fun: 프로필의 사용자(`user`), 글(`post`), 스토리(`story`), 방명록 글(`guestbook_entry`). 신고 UI 는 모두 `widgets.ReportDialog`, 차단은 `widgets.Confirm` 후 `api.Block`.

---

## 6. 계획별 화면·상태 (chat / sns-fun)

### 6.1 chat 소유

`internal/store/chatstore`:

```go
package chatstore
type ChatStore struct{ /* per-channel message windows, threads, typing, pins */ }
func Get(s *store.Store) *ChatStore                          // s.Reducer("chat").(*ChatStore)
func (c *ChatStore) Window(channelID string) *store.Window[api.Message]
func (c *ChatStore) Thread(rootID string) (root *api.Message, replies *store.Window[api.Message])
func (c *ChatStore) Typing(channelID string, threadRootID *string, now time.Time) []string // user ids, excluding me & blocked
func (c *ChatStore) SetOpen(channelID, anchorMessageID string, atBottom bool) // which windows to refetch on resync ("" anchor + atBottom => latest page, else ?around=)
func (c *ChatStore) SetClosed(channelID string)
func (c *ChatStore) SetThreadOpen(rootID string, open bool)
func (c *ChatStore) Pending(channelID string) []PendingMessage // optimistic sends keyed by client_nonce
type PendingMessage struct{ Nonce, Body, Kind string; ThreadRootID *string; Failed error; SentAt time.Time }
```
- `init()` 에서 `store.RegisterReducer(...)`(이름 `"chat"`, `Resyncer` 구현 — events.md 5.3 2·3단계: 열린 채널 창 교체, 열린 스레드 교체, 안 열린 캐시 삭제).
- 이벤트 처리 규칙은 events.md 4.2 그대로(ID 순 삽입, 범위 밖 무시, 범위 안 모르는 메시지는 `GetMessage`, 리액션 `me` 로컬 유지, 같은 작성자 메시지 오면 입력 중 제거, `client_nonce` 로 낙관적 전송과 병합).

`internal/ui/chat` 의 `init()` 등록:
- `RegisterPane(SlotRail, …)` — 방 레일: `홈`, 방 이니셜(아이콘 이모지 있으면 그것), `+`. 폭 6(내부 4).
- `RegisterPane(SlotSidebar, …)` — 선택된 방의 채널(`# 이름` + 배지) + `─ DM` 목록(접속 점). 표준 모드에서는 맨 위에 방 이름 `▾` 드롭다운(레일 대체).
- `RegisterPane(SlotMembers, …)` — 지휘소 오른쪽 위(스레드 닫혔을 때): 채널 멤버 + 접속 상태(60초마다 `GetPresence`).
- `RegisterRoute(RouteHome|RouteChannel|RouteThread|RouteSearch|RouteInvite|RouteSpaceSettings|RouteFriends, …)`.
- `RouteInvite` 두 갈래: `ID=<code>` 는 초대 링크 미리보기·수락(`GetInvite`/`AcceptInvite`), `Arg="direct"` 는 **직접 초대**(`ID=<invite id>` 면 그 초대의 수락·거절 화면 `AcceptDirectInvite`/`DeclineDirectInvite`, `ID=""` 면 `Store.DirectInvites` 전체 목록). `RouteHome` 에도 받은 방 초대가 있으면 "받은 방 초대 N" 줄을 보이고 `enter` 로 목록으로 간다. 알림함(sns-fun)의 `space_invite` 알림(`subject.type="space_direct_invite"`, `subject.id`=초대 id)과 `alert{kind:"invite"}` 토스트(앱이 `target.space_id` 로 `Store.DirectInvites` 에서 초대 id 를 찾고, 없으면 `ID=""`)는 이 경로로 이동한다.
- `RegisterOverlay("chat.reaction_users", …)` — 리액션 누른 사람 목록(`ListReactionUsers`, 페이지네이션), args = `"<messageID> <emoji>"`. `msg.actions` 메뉴의 "리액션 누른 사람"과 리액션 칩 우클릭으로 연다.
- 차단·신고(5.8 소유 규칙): `msg.actions` 메뉴에 "신고"(`widgets.ReportDialog("message", id, …)`), "작성자 차단"; 멤버 목록·작성자 메뉴에 "사용자 신고"(`"user"`)·"차단"; 방 설정·초대 미리보기에 "방 신고"(`"space"`).
- 대화 화면 밀도: `DensityFull`(이름·시간 줄 + 본문 + 리액션/답글 줄, 날짜 구분선), `DensityLine`(`11:02 민지 오늘 배포… 💬3` 한 줄), `DensityBubble`(이름·시간 위, 말풍선, **내 메시지 오른쪽 정렬**), `DensityMinimal`(한 줄, 이름 축약).
- 입력창: `widgets.Composer`, `@` 자동완성(채널 멤버), `:` 이모지, `/` 명령(`slash.Catalog`), 입력 중 `RT.Typing`.
- 차단한 사용자의 메시지는 `차단한 사용자의 메시지` 한 줄로 접는다(api.md 3.2).
- 채팅 메시지 신고 동작은 `widgets.ReportDialog("message", id, label)`(내부에서 `api.Report(target_type:"message")`).
- 메시지 목록에서 선택된 메시지의 `enter`·`t` = 스레드 열기(`ui.Navigate(Route{Kind: RouteThread, ID: rootID, Arg: channelID})`, 5.5).

### 6.2 sns-fun 소유

`internal/store/feedstore`:

```go
package feedstore
type FeedStore struct{ /* timelines, posts by id, stories, notifications list, trends */ }
func Get(s *store.Store) *FeedStore                           // s.Reducer("feed")
func (f *FeedStore) Timeline(kind string) *store.Window[api.TimelineItem] // "following"|"discover"
func (f *FeedStore) Post(id string) *api.Post
func (f *FeedStore) Notifications() *store.Window[api.Notification]
func (f *FeedStore) Stories() []api.StoryGroup
func (f *FeedStore) Trends() *api.Trends
func (f *FeedStore) NewDiscoverCount() int                   // 30s polling banner "새 글 N개"
```
- `init()` 에서 리듀서 `"feed"` 등록(`Resyncer`: events.md 5.3 4단계 — 팔로잉 `since=<겹침 기준>`, `gap` 이면 처음부터, 열린 글 상세 다시 받기).

`internal/ui/feed` 의 `init()` 등록: `RegisterRoute(RouteFeed|RoutePost|RouteProfile|RouteHashtag|RouteTrends|RouteNotifications|RouteStories|RouteCompose, …)`, 팔레트 명령(5.6 표의 `ui/feed` 행만). **`RegisterPane` 은 부르지 않는다** — 지휘소의 `SlotNotifications` 는 `RouteNotifications` 인스턴스를, `SlotFeed` 는 피드 표면의 경로 인스턴스를 앱이 띄운다(5.3.6). 따라서 알림함 컴포넌트는 지휘소 오른쪽 아래 창(좁음)과 컴팩트·위젯의 Main(넓음) 양쪽 크기를 `Resize` 로 받아 그려야 한다.
- 차단·신고(5.8 소유 규칙): 프로필에 "차단"·"사용자 신고"(`"user"`), 글 메뉴에 "글 신고"(`"post"`), 스토리 보기에 "신고"(`"story"`), 방명록 글에 "신고"(`"guestbook_entry"`) — 모두 `widgets.ReportDialog`.
- 알림함 항목 `enter` 는 대상 경로로 이동한다(채팅 대상이면 `RouteChannel`/`RouteThread`, 방 초대면 `RouteInvite{Arg:"direct", ID:<subject.id>}`, 친구 요청이면 `RouteFriends`).
- 피드 카드: 작성자·상대 시간·본문(`markup`, 해시태그 강조)·이미지(`imgrender`)·투표(`widgets.PollView`)·`♥ 12  ↻ 3  💬 4`. 상단 스토리 링(안 본 것 acc 링). 지휘소 피드 열 상단 탭 `피드 / 팔로잉 / #트렌드`.
- 보이는 글은 `Watcher.WatchTopics()` 로 `post:<id>` 를 낸다.

`internal/ui/fun` 의 `init()` 등록: 팔레트 명령(5.6 표의 `ui/fun` 행만 — `status.set`·`status.clear`·`presence.set`·`focus.start`·`focus.stop`), 상태 메시지 설정 대화상자, 접속 모드 선택, 집중 모드(남은 시간을 상태줄에 보이도록 `store.Focus` 사용), 방명록 작성 대화상자(프로필 화면에서 호출), 그리고 **`ui.RegisterOverlay(ui.OverlayFocusSummary, …)`** — 집중 종료 요약 오버레이(args = alert id, 내용은 `ctx.Store.LastFocusSummary`; 아직 nil 이면 alert 의 title/body 만 보이고 `focus.ended` 방송을 받으면 다시 그림).
- `focus.stop` 이 `EndFocus` 결과를 받으면 `ctx.Store.FocusEnded(summary)` 만 부른다 — **오버레이를 직접 열지 않는다**(9.1: 유일한 트리거는 `alert{kind:"focus_summary"}`).

### 6.3 이미지 — `internal/imgrender` (sns-fun)

```go
package imgrender

type Options struct {
	Cols, Rows int        // target cell box
	BG         color.Color // half-block background (theme bg)
	Fit        FitMode     // FitContain (default) | FitCover
}
type FitMode int
const (FitContain FitMode = iota; FitCover)

type Rendered struct {
	Lines []string // exactly Rows lines, each exactly Cols cells (placeholders/half-blocks/spaces)
	Setup string   // escape sequence to emit once via tea.Raw before the frame (kitty transmit), "" otherwise
	ID    uint32   // kitty image id
}

type Renderer struct{ /* caps, cache, kitty id allocator, LRU of rendered results */ }
func New(caps termcaps.Caps, c *Cache) *Renderer
func (r *Renderer) Render(key string, img image.Image, o Options) Rendered
func HalfBlock(img image.Image, cols, rows int, bg color.Color) []string // "▀" fg=top pixel, bg=bottom pixel

type Cache struct{ /* disk: UserCacheDir/masil/media/<upload_id>, 200MB LRU; memory: decoded images 32 entries */ }
func NewCache(p config.Paths) *Cache
func (c *Cache) Load(ctx context.Context, a *api.Client, m api.Media) (image.Image, error) // FetchMedia with ETag
func Decode(data []byte, contentType string) (image.Image, error)                          // png/jpeg/gif(first frame)/webp

// Upload preparation: strip metadata (api.md 5.14: client responsibility).
func PrepareUpload(path string) (data []byte, contentType string, err error) // JPEG & PNG re-encoded (JPEG q=90); GIF/WebP rewritten keeping only pixel/animation blocks (api.md 5.14), malformed => ErrMalformed; >5MB => downscale JPEG until fit or error
```

- **하프블록**(기본·모든 터미널): 한 칸 = 위/아래 픽셀 2개. 축소는 `x/image/draw.CatmullRom`.
- 큰 이미지 축소(`Cache.Load` 의 긴 변 2048 맞춤, `PrepareUpload` 의 5MB 초과 재인코딩)는 `CatmullRom` 이 아니라 행 단위 면적 평균(`shrinkInto`)이다 — 커널 축소기는 출력 폭×원본 높이×32B 임시 버퍼를 잡아(6000×4000 → 약 262MB) 디코드 전 바이트 판정(~96MB)을 무력화하기 때문이다. 축소에 드는 메모리는 출력 버퍼 + 행 버퍼뿐이다 [보안 결정, 2026-11-06, pending-fixes 13].
- **kitty**(`GraphicsKitty`): 유니코드 자리표시자 방식 — `Setup` 에 `kitty.EncodeGraphics(..., &kitty.Options{Action: 'T'(transmit+place), ID: id, VirtualPlacement: true, Columns: cols, Rows: rows, Quiet: 2})`, 각 칸은 `kitty.Placeholder` + `kitty.Diacritic(row)` + `kitty.Diacritic(col)`, 글자색 = 이미지 ID(24비트). 자리표시자 한 칸 폭은 그래핌·wcwidth 모두 1 **[검증]** 이라 셀 렌더러와 충돌하지 않는다. tmux 안에서는 쓰지 않는다.
- **iTerm2**(`GraphicsITerm2`): 셀 렌더러가 덮어쓰는 문제 때문에 **목록·카드에서는 하프블록**, 전체 화면 이미지 보기(`o`)에서만 빈 칸으로 자리를 비우고 `tea.Raw(커서 저장 + 위치 이동 + iterm2 파일 이스케이프 + 커서 복원)` 를 크기·내용이 바뀔 때마다 다시 보낸다. 실패하거나 깨지면 하프블록으로 되돌린다(설계서 "가능하면").
- 골든 테스트에서는 항상 `GraphicsHalfBlock`.

---

## 7. 계획 경계 요약표

| 기능 | 계획 | 패키지 |
|---|---|---|
| 설정·자격 증명·device_id·잠금 | core | config, auth, instance |
| REST 전 엔드포인트 | core | api |
| 실시간 소켓·재접속·재개 | core | realtime |
| 공유 상태·부트스트랩·재동기화 틀 | core | store |
| 채팅 메시지 창·스레드·입력 중 | chat | store/chatstore |
| 타임라인·글·스토리·알림함 목록 | sns-fun | store/feedstore |
| 테마 4종·레이아웃 5모드·포커스·키맵·팔레트·상태줄·탭 | core | theme, ui/* |
| 마크다운·코드 강조·투표 막대·이모지 선택기 | core | markup, ui/widgets |
| 방 레일·채널 목록·대화·스레드·멤버·검색·방/초대/친구 | chat | ui/chat |
| 받은 방 직접 초대 수락·거절(`RouteInvite{Arg:"direct"}`) | chat | ui/chat |
| 리액션 누른 사람 오버레이(`chat.reaction_users`) | chat | ui/chat |
| 메시지·방·(채팅 화면의) 사용자 신고·차단 동작 | chat | ui/chat |
| 피드·글·프로필·해시태그·트렌드·스토리·알림함 | sns-fun | ui/feed |
| 글·스토리·방명록·(프로필의) 사용자 신고·차단 동작 | sns-fun | ui/feed, ui/fun |
| 상태 메시지·접속 모드·집중 모드·집중 요약 오버레이(`fun.focus_summary`)·방명록 UI | sns-fun | ui/fun |
| 신고 대화상자(`widgets.ReportDialog`)·차단 목록/해제(설정) | core | ui/widgets, ui/settings |
| 논리 내비게이션 상태·슬롯 투영(`NavState`, `Project`)·타이머(`ui.After`) | core | ui/app, ui |
| core 시험용 대역 화면 | core | ui/uitest/stub |
| 이미지(하프블록·kitty·iTerm2)·업로드 전처리 | sns-fun | imgrender |
| 알림 정책·공유 기록·OS 백엔드 | core | notify |
| TUI 의 alert 처리 | core | ui/app |
| 데몬·자동 시작·릴리스 | daemon-release | daemon, autostart, 릴리스 파일 |
| 운영자 CLI(`masil admin`) | sns-fun | admincli, cmd/masil/cmd_admin.go |
| 로그인/가입/설정 화면, `masil` 진입점 | core | ui/authui, ui/settings, cmd/masil/main.go |

---

## 8. 한글·이모지 폭

### 8.1 원칙

- 폭 계산은 **`x/ansi` 의 그래핌 폭 하나**로 통일한다(lipgloss v2 와 동일). `len()`, `utf8.RuneCountInString` 으로 화면 폭을 세지 않는다(서버 길이 제한 검사에만 `RuneCountInString`).
- 한글 음절·자모(ㄱㄱ), CJK, 대부분의 이모지(👍 💬 🫡)는 2칸, 상자 그리기 문자·`●○…▀★♥↻` 는 1칸 **[검증]**.
- **위험한 경우** **[검증]**: `❄️`·`⚠️`(VS16 이모지)는 그래핌 2 / wcwidth 1, `👨‍👩‍👧`(ZWJ)는 그래핌 2 / wcwidth 6. Bubble Tea 렌더러는 터미널이 2027 모드를 지원하지 않으면 wcwidth 로 칸을 배치하므로(1.4), 이런 문자가 그대로 나가면 줄 길이가 어긋나 테두리가 깨진다.

### 8.2 `textwidth.Sanitize` (표시 전용 정규화) [결정]

화면에 그릴 **모든 외부 문자열**(사용자 이름·본문·채널 이름·상태 메시지·알림 문구·리액션 이모지 표시)은 그리기 직전에 `Sanitize` 를 거친다. 결과 문자열의 **모든 그래핌**은 그래핌 폭 = wcwidth 폭이 되어(규칙 7 이 보장), 렌더러 방식과 무관하게 같은 칸을 차지한다.

1. 제어 문자(C0/C1, `\t` 는 공백 4칸으로) 제거, ANSI 이스케이프가 섞인 사용자 입력은 `ansi.Strip`. **서식 문자(유니코드 범주 Cf) 전부와 줄·문단 구분자 U+2028/U+2029 도 제거 [보안 결정, 2026-11-06]** — bidi embedding/override/isolate(U+202A–202E, U+2066–2069)·방향 표시(U+200E, U+200F, U+061C)·보이지 않는 문자(U+200B, U+200C, U+2060–2064, U+FEFF, U+00AD) 등으로 남이 보낸 이름·메시지·알림이 보낸 것과 다르게 보이게(`U+202E`+`txt.exe` → "exe.txt", 가짜 둘째 줄) 할 수 없게 한다. **Cf 밖의 기본 무시 문자도 제거 [보안 결정, 2026-11-06, pending-fixes 1 재검토]** — 변이 선택자(`unicode.Variation_Selector`: VS1–16 U+FE00–FE0F, VS17–256 U+E0100–E01EF, 몽골 FVS U+180B–180D·U+180F)와 `unicode.Other_Default_Ignorable_Code_Point`(CGJ U+034F, 한글 채움 문자 U+115F·U+1160·U+3164·U+FFA0, 크메르 U+17B4·U+17B5, 미할당 무시 문자). 이들도 두 폭 방식에서 폭 0(채움 문자는 빈칸)이라 `ad`+U+034F+`min` 이 "admin" 으로 보이고, VS17–256 에는 숨은 데이터를 실을 수 있으며, U+3164 만으로 된 표시 이름은 이름 없는 발신자처럼 보인다. **비문자(`unicode.Noncharacter_Code_Point`: U+FDD0–FDEF, 모든 평면의 U+nFFFE/U+nFFFF)도 제거** — 교환용이 아닌 코드 포인트이고, U+FFFE/U+FFFF 는 두 폭 방식 모두 폭 0 이라 CGJ 와 똑같이 `ad`+U+FFFE+`min` 이 "admin" 으로 보인다(나머지는 빈칸·두부 칸). 이 집합(Cf ∪ Zl ∪ Zp ∪ Variation_Selector ∪ Other_Default_Ignorable_Code_Point ∪ Noncharacter_Code_Point)을 `textwidth.Invisible(r)` 로 공개해 파서(4.8 토큰 경계)·OSC 8 검사가 같은 기준을 쓴다. 이체자 선택(IVS, 예: `葛`+U+E0100)은 기본 글리프로 보이게 된다. 예외는 ZWJ(U+200D) 하나 — 규칙 3 이 이모지 시퀀스를 자르는 데 쓰고, 규칙 3 뒤에는 남지 않는다. 태그 문자(규칙 4)도 Cf 라 여기서 함께 빠진다. 결합 문자를 뺀 rune 중 두 폭 방식에서 모두 폭 0 인 것은 전부 `Invisible` 에 든다(시험 `TestNoZeroWidthRuneOutsideInvisible` — 라이브러리 폭 표가 바뀌어도 새 구멍이 생기면 실패). **빈 이름 대체**: 표시 이름이 화면에 아무것도 남기지 않으면(`textwidth.Blank(s)` — `Sanitize(s)` 가 비었거나 공백 구분자(Zs)와 빈 칸으로 그려지는 글자 U+2800 BRAILLE PATTERN BLANK·U+1D159 MUSICAL SYMBOL NULL NOTEHEAD 만으로 됨) 비어 있을 때와 똑같이 핸들로 대체한다. U+2800 은 JS `trim` 이 지우지 않아 서버가 받아 주므로 클라이언트가 막는다. 본문 속 U+2800 은 공백처럼 보일 뿐이라 지우지 않는다. 적용 위치: `widgets.UserName`·`Avatar`, 채팅 `displayName`·멘션 완성 Label·직접 초대 토스트, 피드 `displayName`·알림 문구, 팔레트 사람 행, 설정 차단 목록, DM 이름 `Store.ChannelLabel`. 입력 정규화(8.4)에서는 U+2028/U+2029 가 `"\n"` 이 된다(`\r` 과 같음).
2. U+FE0F(VS16), U+FE0E 제거 → `❄️` 는 `❄`(1칸).
3. ZWJ 시퀀스는 첫 구성 이모지만 남김 → `👨‍👩‍👧` → `👨`.
4. 피부색 수식자 U+1F3FB–1F3FF 제거, 키캡 U+20E3 제거, 태그 문자 U+E0020–E007F 제거.
5. 결합 문자만 남은 그래핌(예: 단독 결합 악센트)은 앞에 공백을 붙여 1칸으로.
6. 그 외 문자는 그대로 두되, 규칙 7 직전에 **NFC 로 합친다**(`golang.org/x/text/unicode/norm.NFC`). macOS 파일 이름·끌어다 놓은 경로처럼 흔한 한글 조합형 NFD 자모(`ᄀ`+`ᅡ`+`ᆨ`)가 규칙 7 에 걸려 낱자로 무너지지 않게 하기 위함이다. 서버도 NFC 로 저장하므로 입력 정규화(8.4)로 보내는 글이 달라지지 않는다.
7. **최종 일반 규칙(마지막에 항상)**: 그래핌마다(`ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)` 로 차례로 자름 — x/ansi 와 같은 분할기) `ansi.StringWidth(g) != ansi.StringWidthWc(g)` 이면 바꾼다: (a) 그래핌의 룬을 **모두 남기고** 조합형 자모(U+1100–11FF, A960–A97F, D7B0–D7FF)만 호환 자모(`ᄀ`→`ㄱ`, `ᅡ`→`ㅏ`, `ᆨ`→`ㄱ`, `ᆞ`→`ㆍ`; 대응이 없으면 그 자모 폭만큼의 `□`)로 바꾼 결과의 모든 그래핌이 폭 일치면 그것으로, (b) 아니면 그 그래핌의 **첫 룬** `r`(조합형 자모면 (a) 의 대체형)이 `StringWidth(r) == StringWidthWc(r)` 이고 0 보다 크면 `r` 로, (c) 아니면 `□`(U+25A1) 뒤에 공백을 붙여 원래 그래핌 폭 `StringWidth(g)` 만큼(최소 1). 조합형 자모를 그대로 남기면 다음 자모·음절과 다시 한 그래핌으로 붙어 한 낱말이 자음 하나로 줄어들므로 (a) 가 필요하다. 실측 **[검증: x/ansi v0.11.9]**: 단독 지역 표시자 `🇰` g2/wc1 → `□ `, 데바나가리 `क्षि` g1/wc3 → `क`(1/1), NFC 로 합쳐지지 않는 `ᄀ`+`가` g2/wc4 → `ㄱ가`, `ᄀ`+`ᆞ` g2/wc3 → `ㄱㆍ`(조합형 `ᄀ`+`ᅡ` 는 규칙 6 에서 `가` 가 된다), `□` 은 1/1. 짝을 이룬 국기 `🇰🇷` 는 2/2 라 그대로.

- `Sanitize` 결과는 다시 `Sanitize` 해도 같다(멱등). 단위 시험이 위 예와 함께 "결과의 모든 그래핌 g 에 대해 `StringWidth(g)==StringWidthWc(g)`" 를 무작위 유니코드 문자열(시드 고정)로 검사한다.
- 받은 데이터를 그릴 때의 `Sanitize` 는 전송 데이터(메시지 본문·리액션 키 등)에 **절대 적용하지 않는다**(화면 전용). 입력창이 사용자가 **치는** 글을 정규화하는 것은 별개 규칙이다(8.4).
- **클립보드(OSC 52, `tea.SetClipboard`) [보안 결정, 2026-11-06]**: 남이 보낸 글을 복사할 때는 화면에 보인 것만 넣는다. 서버는 본문의 제어 문자를 지우지 않고 화면은 ESC 시퀀스를 내용째 지우므로, 원문을 그대로 넣으면 보이지 않던 괄호 붙여넣기 종료(`ESC[201~`)·CR·숨은 명령이 셸에 붙여 넣어진다(pastejacking). `msg.copy`(본문·투표 질문·주사위 결과)는 `textwidth.SanitizeInput`(줄바꿈 유지), 명령으로 붙여 넣는 한 줄 값(`masil join <code>`)은 `textwidth.Sanitize`(줄바꿈 없음)를 거친다.
- East Asian Ambiguous 를 2칸으로 그리는 터미널 설정(iTerm2 "Ambiguous characters are double-width" 등)은 지원하지 않는다 — 상자 문자 자체가 깨지므로. 도움말에 안내.

### 8.3 자르기·줄바꿈·채우기

- 자르기는 `textwidth.Truncate`(그래핌 경계, 넓은 문자를 반으로 쪼개지 않음, 1칸 남으면 공백으로 채움), 말줄임 `…`.
- 줄바꿈은 `textwidth.Wrap`: 공백에서 끊고, 한 단어가 폭을 넘으면 강제 줄바꿈. 한글은 음절 사이에서 끊어도 된다.
- 모든 창은 `textwidth.Fit` 으로 **정확히 W칸 × H줄**을 만든다. lipgloss `Style.Width` 를 쓸 때도 마지막에 `Fit` 으로 보정한다.

### 8.4 한글 입력(IME)

- 입력창은 bubbles textarea 를 **실제 커서 모드**(`SetVirtualCursor(false)`)로 쓰고, 앱이 `tea.View.Cursor` 를 입력창 위치로 옮긴다 → 터미널의 IME 조합 글자가 입력 위치에 뜬다.
- 조합 중 글자는 터미널이 그리므로 앱은 완성 음절만 받는다(`KeyPressMsg.Text`). 붙여넣기는 `tea.PasteMsg`.
- 입력창이 포커스일 때는 `InputCapturer=true` → 한글·영문 글자가 단축키로 해석되지 않는다.
- **입력 정규화 [결정]**: textarea/textinput 은 자기가 가진 원문을 그대로 그리므로, `❄️`·`👍🏽`·ZWJ 이모지를 그대로 담으면 2027 미지원 터미널에서 그 줄이 밀려 테두리가 깨진다. 그래서 **모든 텍스트 입력 위젯**(`widgets.Composer`, 팔레트·대화상자의 `textinput`, 로그인 화면의 handle·표시 이름 칸)은 `KeyPressMsg.Text`·`PasteMsg.Content`·`SetValue` 를 `textwidth.SanitizeInput` 으로 거른 **뒤에** 위젯에 넣는다. 결과: 입력창에 보이는 글 = 보내는 글이고, 모든 그래핌이 두 폭 방식에서 같다.
  - 이는 **의도된 전송 내용 변경**이다: 이 클라이언트에서 친 글에는 VS16·피부색·ZWJ 꼬리·키캡·태그 문자·서식 문자(bidi 제어·보이지 않는 문자)·변이 선택자·CGJ·한글 채움 문자·비문자(8.2 규칙 1)가 빠지고(U+2028/U+2029 는 줄바꿈이 되고), 규칙 7 대상 그래핌은 대체된다(`👍🏽`→`👍`, `❄️`→`❄`, `👨‍👩‍👧`→`👨`). 다른 클라이언트·서버가 만든 데이터는 바꾸지 않는다. 내 메시지 수정(`compose.edit_last`)은 `SetValue` 를 거치므로 원문에 그런 문자가 있었다면 수정 저장 시 함께 정규화된다 — 도움말에 한 줄 안내.
  - 비밀번호 칸은 예외: 값은 그대로 두고(`EchoPassword` 가림 문자만 그려지므로 폭 문제 없음) 전송한다.
  - 정규화된 텍스트는 그래핌·wcwidth 폭이 같으므로 textarea 의 커서 열 = 커서 앞 글의 `ansi.StringWidth` 이다. 하네스가 이를 검사한다(10.3 입력창 골든).

---

## 9. 알림과 데몬 조율

### 9.1 TUI

- `alert` 이벤트마다 `notify.Decide(alert, Input{Role: RoleTUI, Replayed, DesktopEnabled: cfg.notify.desktop, TerminalFocusKnown: caps.FocusReports, TerminalFocused, ViewingTarget})` 를 Update 안에서 바로 하고(토스트·집중 요약 판단에 필요), 결과가 `Popup` 이면 `Notifier.Show(alert)`(원장 flock 최대 2초 대기·원자적 쓰기·OS 알림 실행)를 **`tea.Cmd` 안에서** 돌린다 — Update 고루틴을 막지 않는다. 실패는 결과 메시지에서 로그로만 남긴다.
- `ViewingTarget` = alert 의 `target.channel_id`(스레드면 `thread_root_id`) 가 현재 보이는 `ChannelViewer`/스레드 경로이거나, `post_id` 가 보이는 글.
- 결과가 `Silent` 또는 `Popup` 이면 알림 패널(`Store.Alerts`)에 넣고, 대상이 화면에 안 보이면 `ToastAlert` 토스트(4초, 클릭·`enter` 시 대상 경로로 이동).
- **집중 요약 [결정]**: 요약 오버레이의 **유일한 트리거**는 `alert{kind:"focus_summary"}` 다. 앱(core `ui/app`)은 그 alert 에 대해 토스트 대신 `ui.OpenRegisteredOverlay(ui.OverlayFocusSummary, alert.ID)` 를 낸다(오버레이는 sns-fun `ui/fun` 이 등록, 5.3.6 — core 는 import 하지 않는다). 조건: `Decide` 결과가 `Skip` 이 아니고, `!(Replayed && now - created_at > 10분)`, 그리고 **이 프로세스에서 같은 `alert.id` 로 연 적이 없음**(앱이 연 id 집합을 최근 50개 기억). 미등록이면 무시(오버레이 대신 일반 `ToastAlert`).
  - `DELETE /v1/me/focus` 응답(`EndFocus` 의 `FocusSummary`)과 `focus.ended` 이벤트는 `Store.FocusEnded` 로 **상태만** 갱신한다(`Store.Focus=nil`, `LastFocusSummary`). 둘 다 UI 를 열지 않으므로 조기 종료든 자연 종료든 요약은 정확히 한 번 뜬다.

### 9.2 데몬 (`internal/daemon`, daemon-release)

```go
package daemon

type Options struct {
	Paths    config.Paths
	Config   *config.Config
	Backend  notify.Backend      // OS backend; Recorder in tests
	Logger   *slog.Logger
	Now      func() time.Time
	PollCredentials time.Duration // default 30s
}
type State struct {
	Version  int       `json:"version"`
	UserID   string    `json:"user_id"`
	Epoch    string    `json:"epoch"`
	LastSeq  int64     `json:"last_seq"`
	SavedAt  time.Time `json:"saved_at"`
}
func Run(ctx context.Context, o Options) error // AcquireDaemon; loop: load creds -> realtime(ModeNotify) -> handle alerts; saves State on every MarkProcessed (debounced 5s) and on exit
```

규칙(events.md 7장):
- `realtime.New(Options{Mode: ModeNotify, Resume: State 또는 UseDeviceAck})`.
- TUI 존재 판정 `tuiPresent()`:
  1. `instance.TUIRunning(paths)` 이 오류 없이 답하면 **그 값이 최종**(로컬 락이 "TUI 프로세스 없음"을 확실히 알려 주면 서버 신호보다 우선, "있음"이면 당연히 억제).
  2. 락 확인이 오류면 서버 신호(`hello.local_tui_connected`, `device.tui_state`) 사용, 그리고 `tui_connected:false` 를 받은 뒤 3초 동안의 alert 는 3초 뒤 다시 판정.
- `Decide` 입력 `TUIPresent` 에 위 결과를 넣고 `Notifier.Handle`.
- 자격 증명: `credentials.json` 이 없으면 소켓 없이 `PollCredentials` 간격으로 파일 변화를 기다린다. `ErrUnauthorized` 를 받으면 파일을 **지우지 않고** 같은 토큰인 동안 재접속하지 않는다(파일의 토큰이 바뀌면 재시작). `ErrSuspended`·`ErrUpgradeRequired` 는 로그 후 대기(같은 방식).
- 상태를 쌓지 않는다: `alert`, `focus.*`, `notification.*` 외 이벤트는 파싱만 하고 버린다.

`internal/autostart`(daemon-release):

```go
package autostart
type Status struct{ Installed, Running bool; Detail string }
func Install(exe string, p config.Paths) error   // macOS: ~/Library/LaunchAgents/com.patrache.masil.daemon.plist (RunAtLoad, KeepAlive{SuccessfulExit:false}, ProgramArguments [exe, "daemon", "run"], EnvironmentVariables MASIL_CONFIG_DIR if set) + `launchctl bootstrap gui/<uid>`;
                                                 // Linux: ~/.config/systemd/user/masil-daemon.service + `systemctl --user enable --now`;
                                                 // Windows: HKCU\Software\Microsoft\Windows\CurrentVersion\Run "masil-daemon" = "\"exe\" daemon run" (x/sys/windows/registry), console window hidden by daemon self-respawn
func Uninstall() error
func Query() (Status, error)
```

`internal/admincli`(sns-fun): `masil admin reports|report <id>|resolve <id> <resolution>|remove <type> <id>|suspend @h [--for 7d]|unsuspend @h|users|actions` — `api.Admin*` 메서드 사용, 표 출력은 `textwidth` 로 정렬.

---

## 10. 테스트 하네스·골든

### 10.1 원칙

- 화면 골든은 **`View().Content` 를 직접** 뽑아 `ansi.Strip` 한 평문을 비교한다(teatest 의 원시 출력 스트림은 차분 렌더 이스케이프라 레이아웃 비교에 부적합). teatest 는 키 입력 흐름 시험과 `FinalModel` 검사에 쓴다.
- 결정적 환경: 시계 `2026-11-05T02:05:00Z`(서울 11:05), `Loc=Asia/Seoul`(`time/tzdata` import), 테마 D(기본), 이미지 하프블록, 애니메이션 끔, 실시간 끔(`ui.Realtime` noop), 고정 랜덤 시드.
- 골든 파일: `testdata/<TestName>/<subtest>.golden`(golden 패키지 규칙), 갱신은 `go test ./internal/... -update`(Makefile `make golden`).

### 10.2 `internal/ui/uitest` (core)

```go
package uitest

type Size struct{ W, H int }

var Required = []Size{{200, 60}, {120, 40}, {80, 24}, {50, 20}} // every screen golden MUST cover these

var ModeSizes = map[layout.Mode][]Size{
	layout.ModeCommand:  {{200, 60}, {200, 50}, {240, 70}, {320, 90}},
	layout.ModeStandard: {{120, 40}, {100, 40}, {199, 50}, {200, 49}, {160, 45}},
	layout.ModeCompact:  {{120, 30}, {100, 39}, {199, 39}, {160, 16}},
	layout.ModeWidget:   {{80, 24}, {99, 60}, {60, 60}, {44, 24}},
	layout.ModeTiny:     {{50, 20}, {99, 23}, {43, 40}, {120, 15}, {44, 16}},
	layout.ModeTooSmall: {{19, 10}, {30, 5}},
}
func AllSizes() []Size // Required ∪ every ModeSizes entry, deduped, stable order

type Option func(*config)
func WithTheme(n theme.Name) Option
func WithSeed(fn func(s *store.Store)) Option              // preload Store (fixtures)
func WithAPI(srv *apitest.Server) Option                    // canned REST
func WithRoute(r ui.Route) Option
func WithPin(m layout.Mode) Option
func WithUnicodeCore(on bool) Option                       // simulate tea.ModeReportMsg 2027
func WithCredentials(c *auth.Credentials) Option            // default: logged-in fixture user "sam"

type Harness struct{ T testing.TB; Model *app.Model; API *apitest.Server; Notify *notify.Recorder }
func New(tb testing.TB, opts ...Option) *Harness            // temp MASIL_CONFIG_DIR/MASIL_CACHE_DIR
func (h *Harness) Resize(w, hgt int)
func (h *Harness) Press(keys ...string)                    // "ctrl+k", "j", "enter", "shift+tab", "esc"
func (h *Harness) Type(s string)                           // one KeyPressMsg per rune/syllable (Korean OK)
func (h *Harness) Paste(s string)
func (h *Harness) Click(x, y int)
func (h *Harness) Wheel(x, y, delta int)
func (h *Harness) Send(msg tea.Msg)                        // Update + run resulting Cmds synchronously (see rules below)
func (h *Harness) Advance(d time.Duration)                 // move the FakeClock; deliver due timer msgs via Send, in order (see rules)
func (h *Harness) Clock() *FakeClock
func (h *Harness) TeaMsgs() []tea.Msg                      // bubbletea-internal msgs produced by Cmds (QuitMsg, clipboard, raw...) — recorded, not re-dispatched
func (h *Harness) Event(typ string, data any)              // inject realtime.EventMsg (seq auto-increments for logged types)
func (h *Harness) Screen() string                          // ansi.Strip(View().Content)
func (h *Harness) Styled() string                          // raw View().Content
func (h *Harness) Golden(name string)                      // h.Check() + golden.RequireEqual(Screen) under t.Run(name)
func (h *Harness) Check()                                  // AssertIntegrity(T, View().Content, w, h, BoxesOf(Model.Frame()), Context().Theme.Border) without a golden (sweeps)
func (h *Harness) Context() *ui.Context
func (h *Harness) Cursor() *tea.Cursor
func (h *Harness) Nav() app.NavState
func (h *Harness) Instance(routeKey string) ui.Component   // = Model.Instance; tests type-assert to *stub.Chat etc.

// FakeClock implements ui.Timers and supplies Context.Now.
type FakeClock struct{ /* now, pending (deadline, seq, msg) */ }
func NewFakeClock(start time.Time) *FakeClock
func (c *FakeClock) Now() time.Time
func (c *FakeClock) After(d time.Duration, msg tea.Msg) tea.Cmd // Cmd records (now+d, msg) when RUN, returns nil
func (c *FakeClock) Pending() []time.Duration                   // remaining delays, ascending (assertions)

func ForEachSize(t *testing.T, sizes []Size, fn func(t *testing.T, h *Harness)) // subtest per "WxH", fresh harness each

type Box struct{ Rect layout.Rect; Bordered bool; Z int } // Z: panes 0, overlays 10+i, toast 100
func BoxesOf(f app.FrameInfo) []Box                        // panes (Layout) + Overlays + Toast
func AssertIntegrity(tb testing.TB, content string, w, h int, boxes []Box, border lipgloss.Border)
```

`Send` 규칙 **[결정]**:
- `Update(msg)` 가 돌려준 Cmd 를 **같은 고루틴에서 바로 실행**하고, 결과 메시지를 다시 `Update` 로 넣는다(너비 우선, 깊이 최대 32 — 넘으면 `t.Fatal`).
- `tea.BatchMsg` 는 펼쳐 각 Cmd 를 실행한다. 결과 메시지의 타입 패키지 경로(`reflect.TypeOf(m).PkgPath()`)가 `charm.land/bubbletea/v2` 이면(QuitMsg, 클립보드, `tea.Raw`, 비공개 `sequenceMsg` 등) `Update` 로 되돌리지 않고 `TeaMsgs()` 에 기록만 한다. `sequenceMsg` 가 기록되면 하네스는 `t.Errorf("tea.Sequence is forbidden in UI code (5.3.2)")`.
- 타이머는 `ctx.Timers` = `FakeClock` 이라 실행해도 블록하지 않는다(기록만). 5.3.2 규칙을 어긴 `tea.Tick`/`tea.Every` 는 실행하면 실제로 블록하므로, 하네스는 Cmd 하나가 **1초 넘게** 돌아오지 않으면 `t.Fatal("Cmd blocked > 1s: raw tea.Tick/Every or sleep in UI code?")` 로 즉시 알린다(규칙 위반을 숨기지 않음).
- `ui.Do`(API) Cmd 는 동기로 돌아 apitest 서버에 실제 요청한다.
- `Advance(d)`: 목표 시각 `T = now + d`. 대기 타이머 중 기한 ≤ T 인 가장 이른 것(같으면 등록 순)을 골라 시계를 그 기한으로 옮기고 그 msg 를 `Send`, 이를 반복한다(그 처리 중 새로 걸린 타이머도 기한 ≤ T 면 같은 호출 안에서 전달). 끝에 시계 = T. 예: 토스트 `h.Advance(4*time.Second)` → 사라짐, 페이드 `h.Advance(time.Second)` → 4단계 완료, 유휴 `h.Advance(10*time.Minute)`.
- 기본 시계 시작 = 10.1 의 고정 시각, `Context.Now = FakeClock.Now`.

`AssertIntegrity` 검사(`Golden`·`Check` 에서 자동). 보이는 상자 = `BoxesOf(Model.Frame())` — 레이아웃의 테두리 있는 창 + 열린 오버레이의 최종 사각형(5.3.7, 테두리 여부 포함) + 토스트 상자. 높은 `Z` 가 낮은 것을 가린다.
1. 줄 수 == H, 각 줄 `ansi.StringWidth == W` **그리고** `ansi.StringWidthWc == W`(두 렌더러 방식에서 같은 폭 — 8.2 정규화 검증).
2. 테두리: `Bordered` 인 상자마다 네 모서리(테마 Border 의 TopLeft/TopRight/BottomLeft/BottomRight)와 **네 변 전부**(위·아래 = Top/Bottom 문자, 왼쪽·오른쪽 = Left/Right 문자 — 제목·배지가 들어가는 위 변의 제목 구간은 제외)가 정확한 칸에 있다. 단 **더 높은 Z 상자에 덮인 칸은 검사하지 않는다**(오버레이 아래 창 테두리). 오버레이·토스트 자신의 테두리는 가려지지 않으므로 항상 전부 검사된다.
3. `U+FFFD`, 제어 문자, 짝 없는 서로게이트 없음. **서식 문자(유니코드 범주 Cf — bidi embedding/override/isolate·방향 표시·ZWSP/ZWNJ/ZWJ·WJ·BOM·soft hyphen·태그 문자)와 줄·문단 구분자(U+2028/U+2029), 변이 선택자·그 밖의 기본 무시 문자(CGJ·한글 채움 문자·크메르 U+17B4/U+17B5 등)·비문자(U+FDD0–FDEF, U+nFFFE/U+nFFFF) (`uitest.Hidden`)도 없음** [보안 결정, 2026-11-06, pending-fixes 1] — 두 폭 방식 모두에서 폭 0(채움 문자는 빈칸)이라 검사 1 이 잡지 못하지만, 8.2 규칙 1 이 지우지 않은 화면에서는 남이 보낸 이름·메시지가 보낸 것과 다르게 읽힌다.
4. 상태줄(TooSmall 제외)이 마지막 줄에 있음.
5. 모든 상자는 화면 안(`0 ≤ X, X+W ≤ cols, 0 ≤ Y, Y+H ≤ rows`)이고, 오버레이·토스트는 상태줄을 덮지 않는다.
6. 원시(raw) 줄에는 SGR(`CSI … m`)과 대상이 인쇄 가능 ASCII 인 OSC 8 말고 어떤 이스케이프·제어 시퀀스도 없다. OSC 8 대상이 `fixtures.InjectedLinkHost`(적대적 세트가 원문에 숨긴 OSC 8 의 호스트)이면 실패(pending-fixes 0-b). 앱 합성기(lipgloss v2.0.6 `Compositor`)가 OSC 52·OSC 0/2·CSI 2J 를 스스로 지우므로, 위젯의 정리를 증명하는 적대적 페이로드는 합성기를 통과하는 APC·DCS·SOS·PM·C1·OSC 8 이어야 한다.

`internal/ui/uitest/stub` (core, **시험 전용** — `_test.go` 에서만 import, 제품 코드·`ui/e2e` 는 import 금지):

```go
package stub

// Chat stands in for chat's channel/thread screens so core can test the framework
// (instance cache, Project, focus intent, ScrollAnchor, cursor, persistence) without importing ui/chat.
// Implements ui.Component, KeyHandler, MouseHandler, InputCapturer, Cursorer, ChannelViewer, Persistent, Titled, Helper.
type Chat struct {
	Route    ui.Route
	Serial   uint64                        // unique per instance (identity checks across mode switches)
	List     *widgets.List[api.Message]    // fixtures.Messages(channel), DensityFull/Line/Bubble/Minimal rendered minimally
	Composer *widgets.Composer             // `i` focuses, `esc` leaves, `enter` appends a pending line locally
}
// List stands in for feed/notification screens: a widgets.List of fixed rows, Persistent, Titled.
type List struct{ Route ui.Route; Serial uint64; List *widgets.List[string] }

func Install() // sync.Once; RegisterRoute(RouteChannel, RouteThread -> *Chat; RouteFeed, RouteNotifications, RouteHome -> *List). Call from TestMain.
```
- 키: `j/k` 선택, `ctrl+u/d` 스크롤, `t`·`enter`(선택 메시지) → `ui.Navigate(RouteThread{ID: 선택 ID, Arg: 채널})`, `i` 입력, `esc` 입력 해제(입력 중 아니면 처리 안 함 → 앱의 뒤로).

### 10.3 필수 시험 목록

core:
- `layout`: `Auto`/`Resolve` 표(5.2.1 표의 모든 행). **`Compute` 전수 속성 시험**: `cols 0..320 × rows 0..100` 의 모든 크기 × `State` 4조합 × {그 크기의 `Auto` 모드 + `PinMinSize` 가 그 크기 이하인 모든 고정 모드} 에 대해 — 패닉 없음, 창끼리 겹침 없음, 합집합 = 화면 전체(빈 화면이면 빈 목록), 테두리 있는 창의 `Inner()` ≥ 1×1, 테두리 없는 창 ≥ 1×1, 지휘소·표준 공식이 주는 `chat`/`feed`/`main` 폭 > 0. 골든 없이 수 초 안에 끝난다.
- `ui/app` 투영(`Project`/`NavState`) 단위 시험(stub 등록): 5.3.3 표의 모든 칸; 불변식 "한 키는 한 슬롯" 을 무작위 내비게이션 1,000단계(시드 고정) × 모든 모드에서 검사; `LayoutState` 값; 뒤로·`f`·`ctrl+n` 동작.
  - **컴팩트(스레드 열림) → 지휘소 → 컴팩트**: 120×30 에서 `#개발` → 메시지 선택 → `enter`(스레드가 Main) → 스레드 입력창에 `"2시?"` 입력 → 200×60: `SlotMain`=`#개발` 채널 인스턴스, `SlotThread`=같은 스레드 인스턴스(`Serial` 동일, 초안 `"2시?"`), 포커스 `SlotThread` → 120×30: Main 이 다시 그 스레드(같은 `Serial`), 포커스 Main → `esc` 로 채널, 채널 선택·스크롤 유지.
  - **위젯(피드 탭) → 지휘소**: 80×24 에서 `피드` 탭(`FrontFeed`) → 목록 3칸 아래 선택 → 200×60: `SlotFeed` = 같은 피드 인스턴스(선택 유지), `SlotMain` = 활성 탭 경로, 포커스 `SlotFeed` → 80×24: `피드` 탭 선택 그대로.
  - **표준 FeedSwapped → 지휘소**, **알림(컴팩트 Main) → 지휘소(`SlotNotifications`)**: 같은 인스턴스·선택 유지, 같은 키가 두 슬롯에 동시에 나타나지 않음.
- `ui/app`: 셸 골든(로그인 화면, 빈 홈, fixtures 채운 홈) × `AllSizes`; 테마 4종 × 120×40 `Styled()` 골든 1개씩; 팔레트 열림 × `AllSizes`(초소형·TooSmall 크기 포함 — 강등 전체 화면·미표시 확인); 도움말 × `AllSizes`; `widgets.Confirm`·`widgets.EmojiPicker`·`widgets.ReportDialog` × {Required, 44×16, 20×6, 19×10}; 토스트 표시 상태 × Required(+`h.Advance(4s)` 후 사라짐). 모두 `AssertIntegrity` 의 오버레이 규칙(10.2) 통과.
- 오버레이 커서·붙여넣기: 입력창 포커스 상태에서 팔레트 열기 → `h.Type("ㄱㅂ")` → `h.Cursor()` 가 팔레트 입력 칸 위치(오버레이 내부 원점 기준), `h.Paste("개발")` 이 팔레트 질의에 들어가고 아래 입력창 초안은 그대로, `ctrl+k` 로 닫힘, 닫힌 뒤 커서가 입력창으로 복귀. 커서 없는 오버레이(도움말)에서는 `h.Cursor()==nil`.
- **모드 전환 보존 시험**(core 판 — `uitest/stub` 를 `RouteChannel`/`RouteThread` 로 등록해 실행): fixtures 로 `#개발` 열기 → 입력창에 `"배포 몇 시"` 입력 → 위로 5줄 스크롤 → 메시지 하나 선택 → 크기 순서 `200×60 → 120×40 → 120×30 → 80×24 → 50×20 → 200×60` 마다 (a) 초안 유지 (b) 선택 메시지 ID 유지 (c) 스크롤 앵커 메시지가 화면에 보임 (d) 무결성 (e) 같은 인스턴스(`Serial`). 고정(`ctrl+w 1`)과 고정 실패 폴백 표시도. 실제 chat 화면 판은 `ui/e2e`(10.5).
- **무작위 크기 변경 시험**(stub 등록, 시드 고정 3개): 같은 시나리오 뒤 `cols∈[1,330]`, `rows∈[1,100]` 무작위 크기 200번 — 매번 `h.Check()`, 마지막에 원래 크기로 돌아와 (a)~(c)(e) 유지.
- 입력창(`widgets.Composer`) 골든·시험: `"❄️ 첫눈"`, `"👍🏽"`, `"👨‍👩‍👧 가족"`, `"🇰"`, `"क्षि"`, `"ᄀ"+"ᅡ"`, 탭·`\r\n` 섞인 글, ANSI 이스케이프 섞인 글을 각각 `Type` 과 `Paste` 로 넣고 — `Value()` == `textwidth.SanitizeInput(입력)`, 보이는 줄마다 `StringWidth == StringWidthWc`, 커서 열 == 커서 앞 글 `StringWidth`, `AssertIntegrity` 통과(Required 4크기, 1줄·여러 줄 자동 늘어남).
- `textwidth`: 8.2 의 각 규칙(규칙 7 의 세 실측 예 포함), 무작위 유니코드 문자열(시드 고정 10,000개)로 "결과 모든 그래핌 폭 일치·멱등", `SanitizeInput` 의 줄바꿈 보존, `Fit` 결과 폭.
- 타이머 규칙 정적 검사: `go vet` 대신 시험 하나가 `internal/ui/...`(시험 파일 제외)의 Go 소스를 `go/ast` 로 훑어 `tea.Tick`·`tea.Every`·`tea.Sequence`·`time.Sleep`·`time.After`·`time.NewTimer`·`spinner.Model.Tick` 호출이 있으면 실패(예외: `ui.RealTimers` 구현 파일 1개). chat·sns-fun 패키지도 core 의 이 시험이 함께 검사한다(경로 패턴으로).
- `realtime`: 가짜 `Conn` 으로 hello→resume→ready, 재전송 중복 제거, 4001/4003/4010 중단, 4009 두 번, 백오프 범위, 하트비트 타임아웃, watch 120초 재전송.
- `notify`: `Decide` 표, `Ledger` 동시성(고루틴 2개 + 프로세스 2개 흉내).
- `api`: 각 메서드의 메서드·경로·쿼리·본문(apitest 로 요청 기록), 오류 봉투 파싱, 429 쿨다운, `Opt` 직렬화.

chat / sns-fun: 자기 화면마다 `uitest.ForEachSize(t, uitest.AllSizes(), …)` 골든(해당 경로를 `WithRoute` 로 연 셸 전체), `fixtures.Hostile` 무결성 전용 시험 × `AllSizes`(10.4), 자기 오버레이(`chat.reaction_users`, `fun.focus_summary` 등) × {Required, 44×16, 20×6}, 그리고 자기 리듀서의 이벤트 규칙 단위 시험. 타이머가 있는 동작(페이드·토스트·폴링)은 `h.Advance` 로 시험한다. sns-fun 은 추가로 "조기 종료(`EndFocus` 응답) + `focus.ended` + `alert{focus_summary}` 를 모두 받아도 요약 오버레이가 정확히 1번", "`alert` 재전송(같은 id)에도 1번" 을 시험한다.

daemon-release: `daemon` 판정 시험(락 있음/없음/오류 × 서버 신호 × 3초 유예), `autostart` 는 생성 파일 내용 골든(실제 `launchctl`/`systemctl` 호출은 인터페이스 뒤로 빼서 시험에서 기록만).

### 10.4 `internal/ui/uitest/fixtures` (core)

시안과 같은 데이터: 나 = `sam`(id `01JA0000000000000000000SAM`), 사용자 `minji`(민지), `junho`(준호), `hana`, `dev_kim`; 방 `노바 개발방`(채널 `일반`·`잡담`·`개발`·`배포알림`, `#일반` 안 읽음 3), DM 민지(온라인)·준호(오프라인); `#개발` 메시지(민지 11:02 "오늘 배포 몇 시에 해요?" 👍2 👀1 답글 3, 준호 11:04 코드 블록 `$ wrangler deploy --env prod`, sam 11:05 "@minji 점심 먹고 ㄱㄱ", 민지 투표 "점심 메뉴" 국밥 4·돈까스 2); 피드 글 hana "오늘 첫 눈 ❄️ 사무실 창밖"(이미지 1, ♥12 ↻3 💬4), dev_kim "bubbletea v2 써보신 분? #golang"(♥5 💬2); 알림 2개.

```go
package fixtures
func Seed(s *store.Store)                         // core entities
func Messages(channelID string) []api.Message     // canned history
func Timeline() []api.TimelineItem
func Notifications() []api.Notification
func Hostile(s *store.Store)                      // adds the hostile set below to a seeded Store (separate space "☠ 적대적 데이터")
func HostileMessages(channelID string) []api.Message
func HostileTimeline() []api.TimelineItem
func RegisterAPI(srv *apitest.Server)             // canned responses for the above (normal + hostile)
func TestImage() image.Image                      // 64x48 gradient (mockup .img colors)
```
**적대적(hostile) 세트** `Hostile`: 32자 handle·32자 표시 이름(한글 16자), 50자 채널 이름(한글), 50자 방 이름, 4,000자 메시지(한글·영문·공백 없는 긴 단어 섞음), 500자 글, 탭·`\r`·C0/C1 제어 문자·ANSI 이스케이프(`\x1b[31m`, OSC 8, 합성기를 통과하는 APC(kitty 그래픽)·DCS·SOS·PM·8비트 CSI, OSC 52·CSI 2J) 섞인 본문·이름, bidi 제어·보이지 않는 서식 문자·U+2028/U+2029 묶음(`fixtures.FormatSoup`, 10.2 검사 3)이 든 본문·표시 이름·상태 메시지·채널 이름, VS16(`❄️ ⚠️`)·피부색(`👍🏽`)·ZWJ(`👨‍👩‍👧`)·단독 지역 표시자(`🇰`)·국기(`🇰🇷`)·키캡(`1️⃣`)·데바나가리(`क्षि`)·조합형 자모(`ᄀ`+`ᅡ`)·결합 악센트 단독, 공백 없는 300자 URL, 리액션 20종(이모지 키 위 문자들), 답글 9,999개, 안 읽음 9,999, 멘션 `@here` + 32자 handle 멘션, 상태 메시지 이모지 + 긴 문구.

chat·sns-fun 은 자기 테스트 전용 데이터를 자기 패키지 `testdata` 나 `_test.go` 에 추가한다(fixtures 패키지를 고치지 않는다). 단 자기 화면마다 `fixtures.Hostile` 을 넣은 **무결성 전용**(골든 없음) 시험 × `AllSizes` 를 반드시 둔다(`h.Check()`).

### 10.5 `internal/ui/e2e` (core, 마지막)

`cmd/masil/register_*.go` 와 같은 blank import 로 chat·sns-fun 을 모두 넣은 셸 골든: 홈·`#개발`·스레드 열림·피드·알림함 × `AllSizes`, 그리고 10.3 의 모드 전환 보존 시험·투영 시험(컴팩트 스레드 ↔ 지휘소, 위젯 피드 → 지휘소)을 실제 chat/sns 화면으로 다시 돈다.

추가로 **골든 없는 무결성 스윕**(`h.Check()` 만, chat·sns-fun 등록, fixtures + `Hostile`):
- 모든 너비 `1..260` × 높이 `{6, 15, 16, 23, 24, 39, 40, 49, 50, 60}`.
- 모든 높이 `1..90` × 너비 `{20, 43, 44, 99, 100, 159, 160, 199, 200}`.
- 각 크기에서 화면 상태 5종(홈, `#개발`, `#개발`+스레드, 피드 표면, 알림) + 팔레트 열림 1종. 한 하네스를 재사용하며 `Resize` 만 바꾼다(그 자체로 크기 변경 순서 시험).
- `Hostile` 를 연 화면(적대적 채널·적대적 글 상세·적대적 프로필) × `AllSizes`.
- 무작위 크기 변경 순서(10.3 과 같은 방식, 실제 화면, 시드 고정 3개).
- 시간 예산: 스윕 전체가 CI 한 OS 에서 2분 이내가 되도록 `-short` 에서는 너비·높이를 3칸 간격으로 표본 추출한다(전수는 `-short` 없이).

---

## 11. 결정 사항 요약 (빌더 재량을 이 문서가 정한 것)

1. glamour 미사용(마크다운 부분집합 자체 파서 + chroma), go-runewidth 직접 미사용, teatest/golden 버전 고정.
2. 설정 디렉터리 `~/.config/masil`(모든 OS, `MASIL_CONFIG_DIR` 만 예외), 원자적 쓰기, 0600/0700.
3. TUI↔데몬 로컬 조율 = `tui.lock` 공유 flock(+ 서버 신호 폴백), 기기 안 중복 방지 = `alerts-shown.json` + 배타 flock, 500개.
4. 모드 중단점: 지휘소 ≥200×50, 표준 ≥100×40, 컴팩트 ≥100×(16~39), 위젯 44~99×≥24, 초소형 그 밖(≤43칸 또는 ≤15줄 또는 위젯 폭에 24줄 미만), 20×6 미만 안내. 고정은 모드별 최소 크기 이상에서만.
5. 지휘소·표준 창 폭 공식(5.2.2), 상태줄 1줄 공통, 초소형 테두리 없음.
6. 컴포넌트는 포인터·제자리 변경, 인스턴스를 경로 키로 캐시 → 모드 전환 중 초안·스크롤(ID 앵커)·선택(ID) 보존. 경로는 모드 무관 논리 상태 `NavState`(탭별 스택 + 탭별 스레드 + 피드 표면 스택 + `Front`)에만 두고 매 프레임 `app.Project` 로 슬롯에 투영 — 한 경로 키는 한 프레임에 한 슬롯, 모드 전환은 투영만 다시 함. 알림·피드 창도 경로 인스턴스(PaneFactory 없음).
7. 키: vim + 화살표, `ctrl+w` 창 접두(포커스 이동·모드 고정), 입력 캡처 중에는 글자 단축키 무효. 메시지 선택 `enter` = 스레드(모든 모드), 창 1개 모드의 `tab` = 탭 순환, 모드별 기본 상태줄 힌트 = 시안 문구.
8. 표시 전용 유니코드 정규화(VS16·ZWJ·피부색 제거 + 그래핌별 폭 불일치 대체)로 렌더러 폭 방식 차이를 없앰. 입력 위젯은 입력 시점에 같은 정규화(`SanitizeInput`)를 적용 — 이 클라이언트에서 친 글은 정규화된 채로 전송(의도된 변경).
9. 이미지: kitty 는 유니코드 자리표시자, iTerm2 는 전체 화면 보기에서만, 그 외 하프블록.
10. 테마 `acc4` 를 A·B·C 에 새로 정함(`#7aa2f7`, `#d2a6ff`, `#8e6bbf`), 사용자 이름 색은 해시로 acc2/acc3/acc4, 나는 acc.
11. 시간 지연은 `ui.After`(주입 가능한 `Context.Timers`)만 — `tea.Tick/Every/Sequence` 금지, 하네스는 가짜 시계 + `h.Advance`.
12. 오버레이 테두리·크기는 앱이 정하고(작으면 전체 본문 강등), 열린 동안 커서·붙여넣기는 맨 위 오버레이로. 무결성 검사는 창 + 오버레이 + 토스트 상자의 네 변을 가림 순서대로 검사.
13. 집중 요약 오버레이 트리거는 `alert{focus_summary}` 하나(id 로 중복 제거), 신고는 core `widgets.ReportDialog` 공용, 차단 목록은 설정, 직접 초대는 `RouteInvite{Arg:"direct"}`(chat).
