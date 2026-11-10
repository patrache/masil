# P0 wave 3 통합 기록

- 통합 브랜치: `dev/v0.1.3` (워크트리 `.claude/worktrees/dev-v013`), 2026-11-09. 기준 `5b4d9d2`(wave 2 끝).
- push·tag·배포·원격 wrangler 없음. 작업 브랜치·워크트리는 지우지 않았다.

## 병합한 것 (순서대로, 모두 `--no-ff`)

| 브랜치 | 머지 커밋 | 브랜치 끝 | 들어온 커밋 수 | Task |
|---|---|---|---|---|
| `feat/v013-p0-shell` | `b9d7785` | `81e2ec9` | 7 | 9·10·12·25 + 리뷰 반영 3개 |
| `feat/v013-p0-tools` | `e01f1fa` | `9a11f42` | 7 | 18·19 + 리뷰 반영 5개 |

## 충돌과 해결

- shell 병합은 충돌 없음(ort 자동 병합).
- tools 병합에서 두 파일이 충돌했다. 둘 다 덧붙이기만 한 자리라 양쪽을 모두 남겼다(index 2절: `internal/ui/registry.go`·`context.go` 는 P0 소유, 덧붙이기 등록부).
  - `internal/ui/context.go`: shell 의 `Context.Post Poster` 와 tools 의 `Context.ToolState`·`Context.OpenTools` — 셋 다 남김.
  - `internal/ui/registry.go`: shell 의 `Registry.AlertTargets`·`Registry.Cards`(+ `emptyRegistry`·`clone`)와 tools 의 `Registry.Tools`(`cloneTools`) — 셋 다 남김. tools 리뷰가 계획에 적은 "필드는 등록 함수를 만드는 Task 가 더함"(Cards = Task 25)과 맞는다.
- 자동 병합된 같은 파일: `internal/ui/app/keys.go`, `internal/ui/app/model.go`(tools 의 `toolMsg`·`initTools` 줄과 shell 의 `Post: appPoster{m}` 은 다른 자리).
- i18n 생성물: 병합 뒤 `go generate ./internal/i18n` 을 다시 돌려 `git status` 가 깨끗함을 확인(손으로 합친 생성 파일 없음). 경고는 tools 영역 zh-Hans 누락뿐(오류 아님).
- 골든: `git diff --name-status 5b4d9d2 HEAD -- '*.golden'` 에서 `A` 아닌 줄 0개 — 바뀐 기존 골든 없음. 새 골든 20개 = `internal/ui/chat/testdata/TestCardGolden/*/cards.golden` 5개 + `internal/ui/tools/testdata/TestHubGolden/*/{hub,hub_open,open_menu}.golden` 15개.

## 통합 수정

- 없음. 병합 뒤 첫 전체 `go test -count=1 ./...` 가 바로 통과했다(ok 45개, FAIL 0).

## 다음 wave 가 쓸 수 있는 인터페이스

alert (Task 9)
- `api.Alert.Template *api.AlertTemplate`(`json:"template"`), `api.AlertTemplate{ID string; Params map[string]any}`, `api.AlertTarget.DocType`(`doc_type,omitempty`).
- `notify.Render(a api.Alert, t *i18n.T) (title, body string)`, `notify.Format(a api.Alert, t *i18n.T) OSNotification`.
- `ui.AlertTargetFunc func(ctx *Context, a api.Alert) tea.Cmd`, `ui.RegisterAlertTarget(targetType string, f)` — 키 `"type"` 또는 `"type:doc_type"`, 빈 키·nil f·중복은 panic. `ui.AlertTargetCmd(ctx, a) (tea.Cmd, bool)`. 등록 함수가 nil Cmd 를 돌려주면 토스트만 닫힘, 등록 없으면 알림함. `Registry.AlertTargets`.

실시간 다리 (Task 10)
- `ui.Poster interface{ Post(msg tea.Msg); Coalesce(key string, msg tea.Msg) }`, `ui.DiscardPoster`, `ui.Context.Post`(앱에서 nil 아님).
- `uitest`: `(*Harness).Flush()`, `(*Harness).WaitPosted(n int, timeout time.Duration)`.

exec 중 알림 (Task 12)
- `app.ExecReturnedMsg{Info termhost.ExitInfo}`(받아도 아무 일 없음 — 토스트는 `termhost.Exclusive()` 가장자리로 뜸).
- `termhost.ExclusiveProgram() (program, title string)`, `termhost.SetExclusiveForTest(on bool, req ...Request) (restore func())`.
- i18n `Termpane.ExecReturn(program string, messages int, mentions int)`.

카드 (Task 25)
- `api.MessageKindCard = "card"`, `api.Card{Type string; V int; Data json.RawMessage}`, `api.Message.Card *Card`, `api.SendMessageRequest.Card`, `api.ThreadReplyRequest.Card`(와이어 `card,omitempty`).
- `ui.CardLeetDaily`·`CardLeetSolve`·`CardArchDesign`·`CardArchSim`·`CardAPIResult`·`CardAPIRun`.
- `ui.CardRenderer{Lines func(ctx, m, width) []string; Open func(ctx, m) tea.Cmd; Key func(ctx, m, k tea.KeyPressMsg) (handled bool, cmd tea.Cmd)}`, `ui.RegisterCard(cardType, r)`(빈 type·nil Lines·중복 panic), `ui.CardFor(cardType)`, `ui.MessageCard(m)`, `Registry.Cards`.
- `fixtures.ChCards`, `fixtures.MsgCardText`·`MsgCardDesign`·`MsgCardResult`, `fixtures.Cards(s *store.Store)`.

도구 표면 (Task 18)
- `features.Known()`, `features.On(id)`, `features.Any()` — `MASIL_FEATURES="all"|"a,b"`, 기본 꺼짐.
- `ui.RouteTools`, `ui.RouteTool`; `ui.ToolLeet`·`ToolWorkspace`·`ToolArch`·`ToolGit`·`ToolAPI`.
- `ui.PrefixKey{Key; Help func(*i18n.T) string}`, `ui.ReservedPrefixKeys`.
- `ui.ToolSpec{ID, Order, Icon, Name, Available, Status, New, Open func(*Context, termhost.Mode) tea.Cmd, PrefixKeys, WideDefault}`, `ui.RegisterTool(s)`(다섯 ID 만), `ui.Tools()`, `ui.ToolByID(id)`(플래그 따름), `Registry.Tools`.
- `ui.OpenTool(toolID, instanceID string, query map[string]string) tea.Cmd`, `ui.CloseToolMsg{InstanceID}`, `ui.CloseTool(instanceID)`, `ui.Closer`, `ui.ToolStatuser`, `ui.OpenToolInfo{Route, Title, Status, Active}`, `ui.ToolSwitchedMsg{Route, Zoomed}`, `ui.ToolSurfaceAction`(`ToolSurfaceToggle`/`Next`/`Prev`/`Zoom`), `ui.ToolSurfaceMsg{Action}`.
- 규칙: 인스턴스 ID 는 `"<도구 ID>:"` 로 시작(다른 도구가 같은 ID 를 쓰면 거절), 허브 없이 연 인스턴스는 5개까지(허브 자리 하나 남김).
- `keymap.ToolsToggle = "tools.toggle"`(`ctrl+t`), `keymap.ToolsZoom = "tools.zoom"`(`ctrl+w z`).
- `app.FrontTools`, `NavState.Tools []ui.Route`, `NavState.ToolActive`, `NavState.ToolZoom map[string]bool`, `SlotTool` 이 focusOrder 에서 `SlotThread` 뒤.

허브·tools-state (Task 19)
- `ui.Context.ToolState *toolstate.File`(앱에서 nil 아님), `ui.Context.OpenTools func() []OpenToolInfo`(허브 제외).
- `toolstate.Load(p config.Paths) *File`, `(*File).Section(tool, v) bool`, `(*File).SetSection(tool, v) error`, `toolstate.Common{LastOpen termhost.Mode; Zoomed *bool}`, `(*File).Common(tool)`, `(*File).SetCommon(tool, c)`.
- `config.Paths.ToolsState`(`<dir>/tools-state.json`), `config.Paths.SecretsDir`(`<dir>/secrets`, 경로만).
- `slash.Tools Name = "tools"`(플래그 켜짐에서만).
- 팔레트 명령: `tools.hub`·`tools.toggle`·`tools.next`·`tools.prev`·`tools.zoom`(`features.Any()`), `tools.open.<leet|workspace|arch|git|api>`(`spec.Open(ctx, ModeAuto)`). 시험 이음매 `tools.SetEnvForTest(...) (restore func())`.

## 미룬 것 (오너·다음 wave 확인)

- **P0 계획 Task 10 Step 1 문구**: "resync 두 번 → 한 묶음" 과 실제 동작(`bootAgain` 으로 한 번 더, GET /v1/me 3번)이 다르다. 동작은 그대로 두었고 계획 문구 수정은 오너 판단.
- **P0 Task 20**: 표준 모드 A8.3 "상태줄에 안 읽은 수" 를 tools 배치가 Task 20 Files(`status.go`)로 넘겼다.
- **P0 Task 16**: `Registry.KillSwitches` 는 Task 16 이 더한다(계획에 적힘). `keymap.go` 는 `TermhostEscape` 만 더하면 된다.
- **P5**: `ui/fun/focus_summary.go` 는 아직 `a.Title` 을 쓴다.
- tools 영역 zh-Hans 번역 누락 경고(출시 관문 전까지 허용).
- 시험은 macOS 에서만 실제로 돌았다(Linux·Windows 는 vet 만).
- wave 2 의 미룬 것(`pendingLimiterRows` 5개는 Task 30, 명세 A7.2 글귀, server squash 여부 등)은 그대로 남아 있다.

## 검증 (이 세션, `e01f1fa` 기준 = 두 병합 뒤, 이 기록 커밋 직전)

- `go build ./...` rc=0
- `go test -count=1 ./...` rc=0, ok 패키지 45개, FAIL/panic 0 (마지막 `ok github.com/patrache/masil/internal/version 1.583s`)
- `go vet ./...` · `GOOS=windows go vet ./...` · `GOOS=linux go vet ./...` 모두 rc=0
- `5b4d9d2` 이후 바뀐 .go 파일 전체 `gofmt -l` → 빈 출력
- `go generate ./internal/i18n` 뒤 `git status` 깨끗
- server/: `5b4d9d2` 이후 바뀐 파일 0개라 `npm run typecheck`·`npm test` 는 해당 없음

## 배치별 "내 판단" 모음

wave 3 통합
- 내 판단: `context.go`·`registry.go` 충돌은 두 쪽 모두 필드 덧붙이기라 양쪽을 다 남겼다. 다른 손질은 하지 않았다.
- 내 판단: 충돌 주석(`# Conflicts:`)이 커밋 메시지 끝에 남아 trailer 가 마지막 줄이 아니어서, 방금 만든(아직 push 안 한) 머지 커밋 메시지만 `--amend` 로 고쳤다. 트리는 그대로다.

batch shell (`docs/superpowers/plans/exec-log/P0-shell.md`)
- 내 판단: 생성기 param 규칙은 `alerts` 영역만 완화했다 — 언어마다 쓰는 param ⊆ 선언, 모든 언어의 합집합 = 선언(서버 Task 8 규칙과 같음, ko `doc.comment_mention` 은 `{space}` 를 뺌). `internal/i18n/gen` 은 Files 목록 밖이다.
- 내 판단: 계획의 "알림함 enter" 는 ToastAlert 의 enter·클릭으로 옮겼다 — 알림함 목록은 `api.Notification` 이라 alert 가 아니다.
- 내 판단: 등록 안 된 target 은 지금의 알림함 경로로 간다(기존 시험·골든 그대로). 등록된 함수가 nil Cmd 를 돌려주면 토스트만 닫고 아무 화면도 열지 않는다(계획·CA 5.3.6 그대로 — 처음엔 알림함으로 보냈다가 리뷰 뒤 고침).
- 내 판단: 본문 출처는 카탈로그에 `<id>_body` 키가 있으면 템플릿, 없으면 서버 `a.Body`(Sanitize). Go 쪽에 body 종류 표를 따로 두지 않았다.
- 내 판단: zh-Hans alerts 문구는 서버 초안을 그대로 옮겼다.
- 내 판단: 데몬 `ledgerSink` 의 언어는 시작할 때와 설정 다시 읽기 때 `setLang` 으로 따라간다.
- 내 판단: api testdata 샘플(`Alert`·`FocusSummary`)에 `template` 키를 더하고 `AlertTemplate` 샘플을 새로 넣었다(엄격 왕복 시험).
- 내 판단: `ui/fun/focus_summary.go` 는 아직 `a.Title` 을 쓴다 — Files 목록 밖(P5 몫).
- 내 판단: 넘치면 큐는 "재동기화 표식이 배달될 때까지 이벤트를 버림" 상태로 머문다(상태 msg 만 남은 하나를 바꿈). 그래서 20번 push 가 정확히 상태 하나 + resync 하나로 배달된다.
- 내 판단: 부트스트랩이 도는 중에 온 `resyncNeededMsg` 는 기존 `bootAgain` 을 써서 많아야 한 번 더 돈다 — 시험 기대 GET /v1/me 3번(처음 1 + 비행 중 1 + bootAgain 1). 계획의 "두 번→한 묶음" 글자 그대로와 다른 까닭: 비행 중에 버려진 이벤트가 스냅숏보다 뒤일 수 있다.
- 내 판단: post 줄도 같은 한도 4096 을 쓰고, 꽉 차면 가장 오래된 일반 post 를 버린다(slog.Warn). Coalesce 키가 같은 것은 제자리에서 바꾼다.
- 내 판단: UI 가 막힌 동안 핑이 계속 나가는 시험은 처음 1초로 줄였다가 리뷰 뒤 계획대로 3초(하트비트 20ms)로 되돌렸다.
- 내 판단: `bootAgain` 때문에 "resync 두 번 → 한 묶음" 글자와 시험(GET /v1/me 3번)이 다른 점은 동작을 그대로 두고 오너·오케스트레이터에게 계획 Task 10 Step 1 문구 수정을 맡긴다(계획 문서는 이 batch 몫이 아님).
- 내 판단: `resyncNeededMsg` 가 비공개라 재동기화·핑 시험은 package app 내부 시험으로 썼다.
- 내 판단: `Run` 전에 부른 `ctx.Post` 는 배달할 프로그램이 없어 debug 로그를 남기고 버린다.
- 내 판단: 토스트에 프로그램 이름을 쓰려고 `termhost.ExclusiveProgram()` 을 더하고 `exec.go` 가 요청을 기록하게 했다 — Files 목록 밖.
- 내 판단: `termhost.SetExclusiveForTest(on bool, req ...Request) (restore func())` 는 가변 인자로 만들었다(요청 없이도 부를 수 있게). 시험 밖에서 부르면 panic.
- 내 판단: `ExecReturnedMsg` 는 package app 에 있어 도구가 import 할 수 없다. 그래서 토스트는 그 msg 가 아니라 `termhost.Exclusive()` 의 true→false 가장자리를 매 Update 맨 앞에서 보고 띄운다(`ExecReturnedMsg` 는 받아도 아무 일 없음).
- 내 판단: 멘션 수는 alert kind `mention` 으로 센다. 메시지 수는 남이 쓴 `message.created` 만 센다. 둘 다 0 이면 토스트를 띄우지 않는다.
- 내 판단: exec 중 알림은 `notify.Decide`(RoleTUI, TerminalFocusKnown=false)를 그대로 따르고, 데스크톱 설정은 매 Update 에 atomic 으로 찍어 둔 값을 realtime 고루틴이 읽는다. 같은 alert 를 UI 가 나중에 받아도 공유 ledger 가 OS 알림을 하나로 막는다.
- 내 판단: `Registry.Cards` 자리는 Task 18 이 만들 자리지만 아직 합쳐지지 않아 Task 25 에서 더했다(`clone` 복사 포함). tools batch 와 `internal/ui/registry.go` 에서 충돌할 수 있다 — 한 필드 더하기라 합칠 때 둘 다 남기면 된다.
- 내 판단: 카드 픽스처는 기존 골든이 바뀌지 않게 따로 부르는 시드 `fixtures.Cards(s)`(#카드 채널, SpaceNova 9번 자리)에 두었다. `fixtures.Messages(ChCards)` 는 텍스트 1·`arch.design` 카드(준호)·`api.result` 카드(sam) 3개.
- 내 판단: api `TestSendMessageCard` 의 빨간 단계는 `undefined: api.Card` 컴파일 실패였다(따로 실행 실패를 보지 않음). api testdata 샘플(Message·MessageSearchHit·MessageWindow·ThreadWindow)에 `card` 키를 더하고 `Card` 샘플을 넣었다.
- 내 판단: 계획의 "`textwidth.Fit(width)` 로 한 번 더 자름" 은 채팅의 `fit`(자르기만, 채우기 없음)으로 했다 — `textwidth.Fit` 은 줄 수·채움까지 하는 다른 함수다.
- 내 판단: 카드 그리기는 세 밀도 모두 렌더러를 쓴다 — 기본·버블은 `Lines` 전부, line 밀도(`compactBody`)와 minimal(초소형) 밀도는 `Lines` 의 첫 줄. 렌더러가 좁은 그림을 ctx·width 로 정한다는 계획에 맞췄다. 스레드 제목처럼 순수 글자 미리보기(`plainBody`)는 서버 `body` 를 그대로 쓴다.
- 내 판단: 카드 키 훅(`msgPane.cardKey`)은 계획의 "msgpane.go 키 처리 맨 앞" 을 채널·스레드 뷰 `HandleKey` 에서 composer·escDrag 다음, 나머지 채팅 키 앞으로 정했다(`channel.go`·`thread.go` 한 줄씩 — Files 목록 밖). 그래서 카드 `Key` 는 글자 입력 시작(`startsComposing`)보다 먼저 키를 받는다.
- 내 판단: `e` 는 등록 여부와 상관없이 모든 카드 메시지에서 조용히 삼킨다(토스트 없음). 동작 메뉴의 수정 항목은 기존 `editable`(kind text·me 만)이 이미 빼고 있어 따로 고치지 않았다. 지운 카드 메시지와 내가 차단한 사람의 카드(접혀 보임)는 일반 메시지처럼 다룬다(훅 없음 — 차단 쪽은 리뷰 뒤 더함).
- 내 판단: `enter` 는 `Open` 이 있을 때만 `Open`, 없으면(또는 등록 안 된 type) 지금처럼 스레드. `t` 는 늘 스레드.

batch tools (`docs/superpowers/plans/exec-log/P0-tools.md`)
- 내 판단: 키맵의 `tools.toggle`(`ctrl+t`)·`tools.zoom`(`ctrl+w` 다음 `z`) 두 줄은 Task 18 이 직접 더했다. 계획에서 `keymap.go` 는 Task 16 의 Files 에 있지만 16 이 18 에 기대므로, 18 이 키 없이 끝날 수 없었다.
- 내 판단: `ui.Registry` 에는 `Tools` 만 더했다. `Cards`·`KillSwitches`·`AlertTargets` 는 그 형을 만드는 Task(25·16·9)가 더한다(지금은 형이 없음).
- 내 판단: `ui.CloseToolMsg`, `ui.ToolSurfaceMsg`/`ToolSurfaceAction` 을 내보냈다. 앱이 받아야 하고 팔레트 명령(Task 19)이 보내야 하기 때문이다.
- 내 판단: `RegisterTool` 은 다섯 도구 ID 만 받는다(그 밖은 panic, JX3). `ToolByID` 도 기능 플래그를 따른다(꺼진 도구는 없는 것으로).
- 내 판단: 도구 표면은 허브를 포함해 6개까지. 7번째 열기는 거절하고 토스트 `도구는 6개까지 열 수 있어요 · 허브에서 하나를 닫아 주세요` 를 띄운다. (리뷰 뒤 고침: 허브 자리 하나는 늘 남긴다.)
- 내 판단: 활성 항목을 닫으면 그 앞 항목이 보이고, 마지막 하나를 닫으면 채팅(`FrontTab`)으로 돌아간다. 닫은 인스턴스는 인스턴스 캐시에서 바로 지운다(`Persistent` 상태도 남기지 않음).
- 내 판단: 도구 표면에서 `esc` 는 채팅으로 돌아가되 도구는 열어 둔다.
- 내 판단: 지휘소에서 채팅 이동(채널 열기)과 탭 전환은 도구 열을 그대로 둔다(대화 열이 옆에 보이므로). 넓게 보기 중이면 도구가 대화 열을 덮고 있으므로 비켜 준다. `applyNavigate`·`switchTab` 에만 적용했고, 기록 이동·`openParent` 는 손대지 않았다.
- 내 판단: 위젯·초소형에서 `[`/`]` 는 원래대로 탭을 돈다(도구 탭이 고정 탭 하나이므로). 그 모드에서 도구 사이 전환은 팔레트(`tools.next`/`tools.prev`)나 허브로 한다. (리뷰 뒤 고침: 위젯 맥락 줄 `▾` 클릭이 다음 도구로 간다.)
- 내 판단: 표준 모드 A8.3 의 "상태줄에 안 읽은 수" 는 이번에 하지 않았다(상태줄 골든을 건드리지 않으려고).
- 내 판단: `FrontTools` 는 `state.json` 에 기존 `frontNames` 기본값 `"tab"` 으로 저장된다(도구 표면은 저장 안 함 — A8.4). `state.go` 는 바꾸지 않았다.
- 내 판단: 도움말의 "도구" 묶음은 `withToolHelp` 로 플래그가 켜졌을 때만 붙인다(꺼짐 = 0.1.2 도움말 그대로).
- 내 판단: `model.go` 는 최소로 고쳤다 — dispatch 첫 줄의 `toolMsg`, 시작 경로가 도구 경로인데 쓸 수 없으면 건너뛰는 조건, Task 19 의 `m.initTools(o.Paths)` 한 줄. shell 배치(Task 9·10·12)와 병합 충돌이 날 수 있는 자리다.
- 내 판단: 팔레트의 도구 항목은 `palette.Source` 가 아니라 도구 ID 마다 하나씩 등록한 명령 `tools.open.<id>` 다섯 개다(`ui/tools/source.go`). 팔레트는 고른 명령 항목을 등록된 id 로 `ui.RunCommand` 해서 실행하므로, 항목이 등록된 명령이어야 한다. 제목은 그릴 때마다 `"<아이콘> <spec.Name(ctx.T)>"`, 키워드 `tools`·도구 ID(영어 별칭만 — 한글 낱말 리터럴은 i18n 래칫이 막으므로 `도구` 는 카탈로그 제목 `도구 허브` 로 찾힌다), 묶음 `GroupNav`, `Available` = 그 도구가 등록돼 있고 플래그가 켜짐, 실행 = `spec.Open(ctx, ModeAuto)`.
- 내 판단: 허브가 열린 인스턴스 목록을 알도록 `ui.Context.OpenTools func() []ui.OpenToolInfo`(앱이 채움)와 `ui.OpenToolInfo{Route, Title, Status, Active}`, 선택 기능 `ui.ToolStatuser{ToolStatus(ctx) string}` 를 더했다. 계획은 이 통로를 정하지 않았다. 인스턴스의 "방식 · 상태"(`패널 · 실행 중`)는 따로 칸을 두지 않고 `ToolStatus` 문자열 하나로 받는다.
- 내 판단: 허브 첫 줄은 오른쪽 끝의 `ctrl+t 닫기`(키맵에서 읽음) 하나다. 제목 `도구` 는 패널 테두리 제목(`routeTitle`)에 이미 있어서 본문에 또 쓰지 않았다. (리뷰 뒤 고침: 테두리가 없는 초소형에서는 첫 줄 왼쪽에 `도구` 를 그린다.)
- 내 판단: 숫자 키는 `spec.Order` 다(leet 1 … api 5). `enter`·숫자·팔레트는 늘 `ModeAuto` 로 연다. `m` 메뉴는 "이번 한 번만"(설계서 A7.2)이라, 고른 방식은 `tools-state.json` 의 `last_open` 에 남아 다음 메뉴의 처음 선택 줄이 될 뿐 `enter`·숫자에는 쓰지 않는다.
- 내 판단: 메뉴의 선택지는 pane·exec·window 중 `termhost.Choose` 가 그대로(내려가지 않고) 받아 주는 것만, 둘 이상이면 맨 앞에 auto. Windows 는 exec 하나. 환경은 `termhost.CurrentEnv(ctx.Config, 도구, 초소형인가)`(패키지 변수, 시험은 `SetEnvForTest`).
- 내 판단: 메뉴는 허브 안 "열려 있음" 자리에 그린다(`── 여는 방식 · <도구> ──`, `▸` 표시, 바닥 줄 `enter 고르기 · esc 취소`). 메뉴가 떠 있는 동안 다른 키는 삼킨다. 메뉴가 없을 때 `esc` 는 허브가 받지 않아 셸이 채팅으로 돌려보낸다.
- 내 판단: `x` 는 열린 인스턴스 줄에서만 닫는다(도구 줄에서는 아무 일 없음). 허브 자체는 `x` 로 닫지 않는다. 열린 인스턴스 줄의 `enter` 는 그 인스턴스를 다시 보여 준다.
- 내 판단: 등록된 도구가 하나도 없으면(지금 P0 이 그렇다 — 도구 패키지는 P1 부터) 허브는 `아직 쓸 수 있는 도구가 없어요` 한 줄만 보이고 바닥 줄은 없다.
- 내 판단: 넓게 보기 기억은 앱이 한다 — `tools.zoom` 으로 바꾼 값을 그 도구의 `zoomed` 에 쓰고, 새로 여는 인스턴스는 기억된 값이 있으면 `WideDefault` 대신 그것으로 시작한다. 허브의 넓게 보기는 저장하지 않는다. 이 때문에 Files 목록 밖의 `internal/ui/app/tools.go`(Task 18 파일)와 `model.go`(위 한 줄)를 고쳤다. `ctx.ToolState` 를 채우는 일도 같은 자리다.
- 내 판단: `toolstate` — 파일은 `{"version":1, "<도구 ID>": {…}, …}`. 모르는 최상위 키와 섹션 안의 모르는 키는 그대로 둔다. `SetSection` 은 도구 자기 필드를 갈아 끼우되 `last_open`·`zoomed` 는 v 가 직접 정하지 않으면 남긴다. 섹션 이름 `""`·`"version"`, 객체가 아닌 값은 오류. 1 MiB 를 넘거나 깨진 파일은 빈 파일로 읽는다(로그만). nil `*File` 은 비어 있는 것으로 읽히고 쓰기는 오류. `config.Paths` 가 비면(경로 없음) 메모리에만 둔다. 쓰기는 `config.WriteFileAtomic(…, 0o600)`.
- 내 판단: `config.Paths.SecretsDir` 는 경로만 정했다(폴더 만들기·0700 은 Task 21 몫). `EnsureDir` 는 바꾸지 않았다.
- 내 판단: `/tools` — 플래그가 꺼져 있으면 `slash.Parse` 가 지금과 똑같은 `알 수 없는 명령이에요: /tools` 오류를 낸다. 켜져 있으면 인자는 무시한다. `Catalog(t)` 는 플래그가 켜졌을 때만 맨 끝에 `/tools` 행(설명 `t.Tools.SlashDesc()`, t 가 nil 이면 ko)을 붙인다.
- 내 판단: `slash` 의 "알 수 없는 명령" 문구는 도우미 `unknownCommand` 하나로 모았다(같은 한글 리터럴을 둘로 늘리면 i18n 래칫 25개를 넘음).
- 내 판단: chat 쪽 시험(`TestSlashToolsOpensHub`)은 `tools.hub` 를 시험 안에서 기록용으로 등록해 확인한다. chat 은 `ui/tools` 를 import 하지 않는다(Task 23 의 import 규칙).
- 내 판단: 계획 Files 밖에 시험 파일을 더 고쳤다 — `internal/ui/app/tools_test.go`, `internal/slash/slash_test.go`, `internal/ui/chat/slash_test.go`, `internal/config/config_test.go`, `internal/ui/tools/export_test.go`.
- 내 판단: 허브 골든의 여는 방식 메뉴는 고정한 macOS 환경(tmux 없음)에서 그린다(실제 환경을 읽으면 기계마다 달라짐).
- 내 판단: 허브 자리 하나는 늘 남긴다 — 허브 없이(팔레트 `tools.open.<id>`, 카드, 시작 경로) 연 인스턴스는 5개까지(`maxTools-1`). 허브가 이미 있으면 결과는 전과 같다(허브 + 5).
- 내 판단: `ui.CloseTool(instanceID)` 의 겉모양은 그대로 둔다(P1 계획이 `ui.CloseTool(inst.ID)` 로 쓴다). 대신 다른 도구가 같은 ID 로 열어 둔 상태에서 오는 `RouteTool` 은 앱이 거절한다(`slog.Warn`, 토스트 없음). 계약에 "인스턴스 ID 는 `<도구 ID>:` 로 시작" 을 적었다.
- 내 판단: 위젯 맥락 줄 `▾` 클릭은 도구 표면이 앞에 있고 항목이 둘 이상이면 다음 항목(끝에서 처음으로)을 보인다. 하나뿐이거나 채팅 쪽이면 지금처럼 팔레트를 연다. 초소형은 맥락 줄이 없어 그대로다.
- 내 판단: 초소형(테두리 없음)의 허브는 첫 줄 왼쪽에 `도구`(`t.Tools.TabLabel()`)를 그리고 `ctrl+t 닫기` 는 오른쪽 끝에 붙인다. 바뀐 골든은 이 브랜치에서 새로 만든 `TestHubGolden/{40x14,50x20}/{hub,hub_open,open_menu}.golden` 6개뿐(`-args -update`). 기존 골든 변화 0.
- 내 판단: 표준 모드 A8.3 의 "상태줄에 안 읽은 수" 는 P0 계획 Task 20 에 넘겼다(Files 에 `status.go`, 시험 한 줄).
- 내 판단: 계약 `client-architecture.md` 를 코드에 맞췄다(5.3.2·5.3.3·5.3.6·5.4·5.6). P0 계획 Task 18·19 Interfaces 와 "Interfaces 요약" 도 같게 고쳤다.
- 내 판단: `Registry.Cards`·`KillSwitches` 는 자리만 만들지 않고, P0 계획 Task 16 Files 에 `registry.go`(`Registry.KillSwitches`), Task 25 Files 에 `registry.go`(`Registry.Cards`)를 적었다. `AlertTargets` 는 Task 9 Files 에 이미 있다.
- 내 판단: P0 계획 Task 16 Files 의 `keymap.go` 를 "`TermhostEscape` 만" 으로 고치고, Task 18 Files 에 실제로 고친 `tabbar.go`·`mouse.go`·`keymap.go` 를 적었다(`status.go` 는 안 고침).
