# P0 batch "tools" 실행 기록 (Task 18·19)

- 브랜치 `feat/v013-p0-tools`, 기준 `dev/v0.1.3` 5b4d9d2, 2026-11-09.
- 계획: `docs/superpowers/plans/2026-11-09-masil-v013-P0.md` Task 18·19.
- 시험은 macOS(darwin/arm64)에서만 실제로 돌렸다. Linux·Windows 는 `GOOS=linux go vet`·`GOOS=windows go vet` 만 했다.
- 모든 입구(`ctrl+t`, `tools.*` 팔레트, `/tools`, 도움말의 도구 줄)는 `features.Any()` 가 참일 때만 있다. 플래그를 끈 기본 상태의 기존 골든은 하나도 바뀌지 않았다.

## Task 18 — 기능 플래그 · 도구 등록부 · 도구 표면 · `ctrl+t` · 넓게 보기

- 내 판단: 키맵의 `tools.toggle`(`ctrl+t`)·`tools.zoom`(`ctrl+w` 다음 `z`) 두 줄은 Task 18 이 직접 더했다. 계획에서 `keymap.go` 는 Task 16 의 Files 에 있지만 16 이 18 에 기대므로, 18 이 키 없이 끝날 수 없었다.
- 내 판단: `ui.Registry` 에는 `Tools` 만 더했다. `Cards`·`KillSwitches`·`AlertTargets` 는 그 형을 만드는 Task(25·16·9)가 더한다(지금은 형이 없음).
- 내 판단: `ui.CloseToolMsg`, `ui.ToolSurfaceMsg`/`ToolSurfaceAction` 을 내보냈다. 앱이 받아야 하고 팔레트 명령(Task 19)이 보내야 하기 때문이다.
- 내 판단: `RegisterTool` 은 다섯 도구 ID 만 받는다(그 밖은 panic, JX3). `ToolByID` 도 기능 플래그를 따른다(꺼진 도구는 없는 것으로).
- 내 판단: 도구 표면은 허브를 포함해 6개까지. 7번째 열기는 거절하고 토스트 `도구는 6개까지 열 수 있어요 · 허브에서 하나를 닫아 주세요` 를 띄운다. (리뷰 뒤 고침: 허브 자리 하나는 늘 남긴다 — 아래 리뷰 절.)
- 내 판단: 활성 항목을 닫으면 그 앞 항목이 보이고, 마지막 하나를 닫으면 채팅(`FrontTab`)으로 돌아간다. 닫은 인스턴스는 인스턴스 캐시에서 바로 지운다(`Persistent` 상태도 남기지 않음).
- 내 판단: 도구 표면에서 `esc` 는 채팅으로 돌아가되 도구는 열어 둔다.
- 내 판단: 지휘소에서 채팅 이동(채널 열기)과 탭 전환은 도구 열을 그대로 둔다(대화 열이 옆에 보이므로). 넓게 보기 중이면 도구가 대화 열을 덮고 있으므로 비켜 준다. `applyNavigate`·`switchTab` 에만 적용했고, 기록 이동·`openParent` 는 손대지 않았다.
- 내 판단: 위젯·초소형에서 `[`/`]` 는 원래대로 탭을 돈다(도구 탭이 고정 탭 하나이므로). 그 모드에서 도구 사이 전환은 팔레트(`tools.next`/`tools.prev`)나 허브로 한다. (리뷰 뒤 고침: 위젯 맥락 줄 `▾` 클릭이 다음 도구로 간다 — 아래 리뷰 절.)
- 내 판단: 표준 모드 A8.3 의 "상태줄에 안 읽은 수" 는 이번에 하지 않았다(상태줄 골든을 건드리지 않으려고).
- 내 판단: `FrontTools` 는 `state.json` 에 기존 `frontNames` 기본값 `"tab"` 으로 저장된다(도구 표면은 저장 안 함 — A8.4). `state.go` 는 바꾸지 않았다.
- 내 판단: 도움말의 "도구" 묶음은 `withToolHelp` 로 플래그가 켜졌을 때만 붙인다(꺼짐 = 0.1.2 도움말 그대로).
- 내 판단: `model.go` 는 최소로 고쳤다 — dispatch 첫 줄의 `toolMsg`, 시작 경로가 도구 경로인데 쓸 수 없으면 건너뛰는 조건, Task 19 의 `m.initTools(o.Paths)` 한 줄. shell 배치(Task 9·10·12)와 병합 충돌이 날 수 있는 자리다.

## Task 19 — 허브 · 여는 방식 메뉴 · 팔레트·`/tools` · `tools-state.json`

- 내 판단: 팔레트의 도구 항목은 `palette.Source` 가 아니라 도구 ID 마다 하나씩 등록한 명령 `tools.open.<id>` 다섯 개다(`ui/tools/source.go`). 팔레트는 고른 명령 항목을 등록된 id 로 `ui.RunCommand` 해서 실행하므로, 항목이 등록된 명령이어야 한다. 제목은 그릴 때마다 `"<아이콘> <spec.Name(ctx.T)>"`, 키워드 `tools`·도구 ID(영어 별칭만 — 한글 낱말 리터럴은 i18n 래칫이 막으므로 `도구` 는 카탈로그 제목 `도구 허브` 로 찾힌다), 묶음 `GroupNav`, `Available` = 그 도구가 등록돼 있고 플래그가 켜짐, 실행 = `spec.Open(ctx, ModeAuto)`.
- 내 판단: 허브가 열린 인스턴스 목록을 알도록 `ui.Context.OpenTools func() []ui.OpenToolInfo`(앱이 채움)와 `ui.OpenToolInfo{Route, Title, Status, Active}`, 선택 기능 `ui.ToolStatuser{ToolStatus(ctx) string}` 를 더했다. 계획은 이 통로를 정하지 않았다. 인스턴스의 "방식 · 상태"(`패널 · 실행 중`)는 따로 칸을 두지 않고 `ToolStatus` 문자열 하나로 받는다.
- 내 판단: 허브 첫 줄은 오른쪽 끝의 `ctrl+t 닫기`(키맵에서 읽음) 하나다. 제목 `도구` 는 패널 테두리 제목(`routeTitle`)에 이미 있어서 본문에 또 쓰지 않았다. (리뷰 뒤 고침: 테두리가 없는 초소형에서는 첫 줄 왼쪽에 `도구` 를 그린다 — 아래 리뷰 절.)
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
- 내 판단: 계획 Files 밖에 시험 파일을 더 고쳤다 — `internal/ui/app/tools_test.go`(`TestContextToolStateNeverNil`·`TestZoomRememberedInToolState`), `internal/slash/slash_test.go`(`TestSlashToolsBehindFlags`), `internal/ui/chat/slash_test.go`, `internal/config/config_test.go`(`TestResolvePaths` 기대값), `internal/ui/tools/export_test.go`(시험 이음매).
- 내 판단: 허브 골든의 여는 방식 메뉴는 고정한 macOS 환경(tmux 없음)에서 그린다(실제 환경을 읽으면 기계마다 달라짐).
- 새 골든: `internal/ui/tools/testdata/TestHubGolden/{200x60,120x40,80x24,50x20,40x14}/{hub,hub_open,open_menu}.golden` 15개(`-args -update` 로만 만듦). 바뀐 기존 골든 0개.
- i18n: `go generate ./internal/i18n` 이 zh-Hans 번역 없음 경고를 낸다(기존 tools 키들과 같음, 오류 아님).

## 리뷰 반영 (P0 review)

- 내 판단: 허브 자리 하나는 늘 남긴다 — 허브 없이(팔레트 `tools.open.<id>`, 카드, 시작 경로) 연 인스턴스는 5개까지(`maxTools-1`). 허브는 언제나 열 수 있어서, 한도 토스트가 가리키는 허브의 `x` 로 닫을 수 있다. 허브가 이미 있으면 결과는 전과 같다(허브 + 5).
- 내 판단: `ui.CloseTool(instanceID)` 의 겉모양은 그대로 둔다(P1 계획이 `ui.CloseTool(inst.ID)` 로 쓴다). 대신 인스턴스 ID 를 도구 표면 전체에서 하나로 만든다 — 다른 도구가 같은 ID 로 열어 둔 상태에서 오는 `RouteTool` 은 앱이 거절한다(`slog.Warn`, 토스트 없음). 그래서 `CloseTool` 이 엉뚱한 도구의 인스턴스를 닫는 일이 없다. 계약에 "인스턴스 ID 는 `<도구 ID>:` 로 시작" 을 적었다(P1·P3·P4 계획의 ID 는 이미 다 그렇다. P3 의 새 설계 `""` 는 다른 도구가 `""` 를 쓰지 않는 한 그대로 된다).
- 내 판단: 위젯 맥락 줄 `▾` 클릭은 도구 표면이 앞에 있고 항목이 둘 이상이면 다음 항목(끝에서 처음으로)을 보인다. 하나뿐이거나 채팅 쪽이면 지금처럼 팔레트를 연다. `▾` 의 키보드 길은 원래 없으므로 키보드로는 팔레트 `tools.next`/`tools.prev` 와 허브. 초소형은 맥락 줄이 없어 그대로다.
- 내 판단: 초소형(테두리 없음)의 허브는 첫 줄 왼쪽에 `도구`(`t.Tools.TabLabel()`)를 그리고 `ctrl+t 닫기` 는 오른쪽 끝에 붙인다. 테두리가 있는 모드는 테두리 제목만(두 번 쓰지 않음). 바뀐 골든은 이 브랜치 Task 19 에서 새로 만든 `TestHubGolden/{40x14,50x20}/{hub,hub_open,open_menu}.golden` 6개뿐이고 `-args -update` 로만 다시 만들었다. 기존 골든 변화 0.
- 내 판단: 표준 모드 A8.3 의 "상태줄에 안 읽은 수" 는 이번에도 하지 않고 P0 계획 Task 20 에 넘겼다(Files 에 `status.go`, 시험 한 줄).
- 내 판단: 계약 `client-architecture.md` 를 코드에 맞췄다 — 5.3.2 `Context.OpenTools`, 5.3.3 허브 자리, 5.3.6 `CloseToolMsg`·`ToolStatuser`·`OpenToolInfo`·`ToolSurfaceMsg`/`Action`·`RegisterTool` 다섯 ID 제한·`ToolByID` 플래그 거름·인스턴스 ID 규칙·`Registry` 필드는 등록 함수를 만드는 Task 가 더함, 5.4 맥락 줄 `▾`·초소형 허브 제목, 5.6 `palette.Source` 문장을 `tools.open.<id>` 명령으로 바꾸고 명령 표에 행 추가. P0 계획 Task 18·19 Interfaces 와 "Interfaces 요약" 도 같게 고쳤다.
- 내 판단: `Registry.Cards`·`KillSwitches` 는 이번에 자리만 만들지 않는다(형 `CardRenderer`·`KillSwitch` 가 아직 없음). 대신 P0 계획 Task 16 Files 에 `registry.go`(`Registry.KillSwitches`), Task 25 Files 에 `registry.go`(`Registry.Cards`)를 적고, Task 18 Step 3 에 실행 기록 한 줄을 달았다. `AlertTargets` 는 Task 9 Files 에 이미 있다.
- 내 판단: P0 계획 Task 16 Files 의 `keymap.go` 를 "`TermhostEscape` 만 — `ToolsToggle`·`ToolsZoom` 은 Task 18 이 이미 더함" 으로 고치고, Task 18 Files 에 실제로 고친 `tabbar.go`(맥락 줄)·`mouse.go`·`keymap.go` 를 적었다(`status.go` 는 안 고침).
