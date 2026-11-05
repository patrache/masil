# masil Go 클라이언트 채팅 UI 구현 계획 (chat)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** core 위에 채팅 화면 전부(방 레일·채널/DM 목록·대화·스레드·멤버·입력창·자동완성·슬래시 명령·리액션/수정/삭제/고정·입력 중·접속 상태·친구·DM/그룹 DM·방 만들기/참여·초대 관리·채널 만들기·멤버/역할 관리·검색)와 채팅 상태 저장소 `chatstore` 를 만든다.

**Architecture:** `internal/store/chatstore` 는 core `store.Store` 에 리듀서 `"chat"` 으로 붙어 채널별 메시지 창·스레드·낙관적 전송·입력 중·고정 목록을 events.md 4.2 규칙대로 관리한다. `internal/ui/chat` 은 파일마다 자기 `init()` 에서 `ui.RegisterPane`(rail·sidebar·members)·`ui.RegisterRoute`(home·channel·thread·search·invite·space_settings·friends)·`ui.RegisterOverlay`(`chat.*`)·`palette.Register`(5.6 표의 chat 행)를 부른다. 배치(지휘소/표준/컴팩트/위젯/초소형)는 core `app.Project` 가 정하고, chat 컴포넌트는 `ctx.Density()` 와 `Resize` 로 받은 크기만 보고 그린다.

**Tech Stack:** Go 1.26.1, `charm.land/bubbletea/v2` v2.1.1, `charm.land/lipgloss/v2` v2.0.6, `charm.land/bubbles/v2` v2.2.1(`textinput`, `key`), core 패키지(`api`, `realtime`, `store`, `ui`, `ui/widgets`, `ui/palette`, `ui/keymap`, `ui/layout`, `markup`(chroma 강조 포함), `textwidth`, `humanize`, `slash`, `theme`, `ids`), 시험은 `ui/uitest` + `ui/uitest/fixtures` + `api/apitest` + `x/exp/golden` v0.1.0.

**Spec:** `docs/superpowers/specs/2026-11-05-masil-design.md` (최우선). 계약: `docs/contract/client-architecture.md`(이하 CA), `docs/contract/api.md`, `docs/contract/events.md`. 시안: `.superpowers/brainstorm/35477-1793846636/content/layouts.html`, `visual-style.html`.

**선행 조건:** 번호는 core 계획(`2026-11-05-masil-03-client-core.md`, 이하 plan 03)의 Task 번호다. core 는 Task 19 부터 번호 순으로 진행하므로(plan 03 "실행 순서") **"core Task N 완료"(N ≥ 19)는 core Task 3–18 전부와 19..N 완료**를 뜻하고, N ≤ 18 은 plan 03 의 DAG 상 그 Task 와 그 선행(`14←10,11,12`, `12←9`, `9←8`, `8←3` 등) 완료를 뜻한다.

- **컴파일**: core Phase 0(plan 03 Task 1–2, CA 2.3 — 모든 core 패키지가 공개 시그니처대로 컴파일되는 스텁)이 끝나면 chat 의 모든 Task 코드는 컴파일된다. 그래서 각 Task 의 Step 1(시험 작성)·Step 2(`undefined: …` 로 실패 확인)·Step 3(구현)은 Phase 0 직후 할 수 있다.
- **시험 통과(Step 4 이후)**: Phase 0 스텁은 등록 레지스트리(`store.RegisterReducer`·`store.New` 의 리듀서 생성·`(*Store).Reducer`, `ui.Register*`, `palette.Register`)만 실제로 동작하고 나머지 함수·메서드는 0값을 돌려준다(plan 03 Task 2 Step 3 — 예: `store.NewWindow` → nil, `realtime.Event.Decode` → `(nil, nil)`, `IsMe`/`IsBlocked` → false, `api` 메서드 → 0값, `uitest.New` → 0값 하네스). 그래서 **chatstore(Task 1–3)도** 아래 표의 core Task 가 실제 구현으로 끝나야 통과한다 — 예: Phase 0 에서는 `Window("C1")` 가 nil 창을 돌려줘 `TestWindowCreatedOnDemand` 가 패닉한다. 표의 core Task 가 끝나지 않아 시험이 스텁 때문에 패닉·실패하면 실행자는 **기다린다**(시험을 고치거나 스텁에 맞춰 통과시키지 않는다).

| chat Task | 직접 쓰는 core 실제 구현 (plan 03 Task 번호: 무엇) | Step 4 이후를 시작할 수 있는 시점 |
|---|---|---|
| 1 | 2(Phase 0 실제 동작): `store.RegisterReducer`·`store.New`·`(*Store).Reducer` / 9: `api.Message`·`api.Me` 타입 / 12: `realtime.Event`·`realtime.Typing` / 14: `NewWindow`·`IsMe`·`IsBlocked` | core Task 14 완료 |
| 2 | 12: `Event.Decode` 와 이벤트 타입 전부 / 14: `Window` 의 `Upsert/Update/Remove/Get/Covers/HasMoreAfter/Len/Items/Replace` | core Task 14 완료 |
| 3 | 8: `apitest.New/JSON/Requests/Client` / 9: `MessageQuery`·`MessageWindow`·`ThreadQuery`·`ThreadWindow` / 11: `ListMessages`·`GetThread` / 14: `Window.Replace/PrependOlder/AppendNewer`·`store.Resyncer` | core Task 14 완료(11 포함) |
| 4 | 6: `textwidth` / 19: `ui` 계약·`ui.Do`·`ResultMsg`·keymap / 20: `FakeClock`·`AssertIntegrity` / 21: `widgets.Spinner` / 23: `fixtures.Seed/Hostile/RegisterAPI` / 25: `uitest.New` 하네스 / 27: 키 라우팅(`Press`) / 28: 오버레이 틀·`OpenOverlayMsg`·오버레이 커서 / 30: `h.Event` → `Store.Apply` → 방송(`TestChatOfSetsClock`) | core Task 30 완료 |
| 5 | 7: `humanize.Clock/DayDivider` / 16: `theme` / 17: `markup.Parse/Render/Plain`(chroma) / 21: `ReactionChips`·`PollView`·`UserName` / 23: `HostileMessages` / 25 | core Task 30 완료 |
| 6 | 14: `SpaceOrder`·`SpaceChannels` / 18: `SlotRail` / 22: `widgets.List` / 24·25: 투영·하네스 / 30: `block.updated` 적용 | core Task 30 완료 |
| 7 | 14: `Store.Viewing`·`ChannelLabel` / 21: `Badge`·`PresenceDot` / 22: `List` / 30: `SetViewing` 갱신 | core Task 30 완료 |
| 8 | 10: `MarkRead` / 11: `ListMessages`·`GetMessage` / 19: `ui.After` / 20: `FakeClock` / 21: `Fade`·`Spinner`·`Empty` / 22: `List` / 27: `FocusChangedMsg` / 30: `EventMsg` 적용·`SetViewing`(매 Update 끝) | core Task 30 완료 |
| 9 | 3: `ids.New` / 8: `api.Describe`·`IsCode` / 11: `SendMessage`·`ReplyThread`·`EditMessage` / 22: `Composer`·`ctrl+c` 규칙 / 27: 입력 캡처 라우팅 | core Task 30 완료 |
| 10 | 7: `slash.Catalog` / 10: `ListChannelMembers` / 22: `Composer.SetCompletion` / 29: `palette.Chosung` | core Task 30 완료 |
| 11 | 7: `slash.Parse` / 10: `CreateInvite`·`SendDirectInvite` / 19: `ui.RunCommand` / 26: 토스트 / 29: 앱의 `RunCommandMsg` 디스패치 | core Task 30 완료 |
| 12 | 10: `Block` / 11: 리액션·고정·삭제·투표 메서드 / 28: `EmojiPicker`·`Confirm`·`ReportDialog` | core Task 30 완료 |
| 13 | 11: `GetThread`·`ReplyThread` / 18: `Layout.Pane` / 24: 스레드 배치 투영 / 25: `Frame().Routes` / 27: 포커스 의도 | core Task 30 완료 |
| 14 | 10: `ListChannelMembers`·`GetPresence` / 12: `Typing` / 21: `PresenceDot` / 28: `ReportDialog` / 30 | core Task 30 완료 |
| 15 | 14: `Friends`·`FriendReqIn`·`DirectInvites` / 21: `Empty` / 22: `List` | core Task 30 완료 |
| 16 | 10: 친구·`OpenDM`·`SearchUsers`·`Block` / 21: `Tabs` / 28: `Confirm`·`ReportDialog` / 29: `palette.Register`·`RunCommandMsg` 디스패치 | core Task 30 완료 |
| 17 | 10: 채널·DM 엔드포인트 / 28: `Confirm` / 29: `palette.Register`·`RunCommandMsg` 디스패치 | core Task 30 완료 |
| 18 | 10: 방·초대 엔드포인트 / 28: `ReportDialog` / 29: `palette.Register`·`RunCommandMsg` 디스패치 | core Task 30 완료 |
| 19 | 10: 방 멤버·역할·밴 / 12: `SpaceMemberEvent` / 21: `Tabs` / 28: `Confirm` / 30 | core Task 30 완료 |
| 20 | 10: 초대·채널 만들기 / 29: `palette.Register`·`RunCommandMsg` 디스패치 | core Task 30 완료 |
| 21 | 7: `humanize.MessageTime` / 11: `SearchMessages` / 22: `List` | core Task 30 완료 |
| 22 | 11: `ListPins` / 29: `palette.Register`·`palette.Command`·`RunCommandMsg` 디스패치 | core Task 30 완료 |
| 23 | 18: `layout.Auto`·`DefaultBreakpoints` / 20: `ModeSizes` / 25: `Frame()` / 27: `ctrl+w 1` 고정·포커스 / 30: `SetViewing` | core Task 30 완료 |

Task 4–23 은 모두 `uitest` 하네스(`open`/`openWith`/`devChannel`)로 돌고, 그 하네스가 쓰는 키 라우팅(27)·오버레이(28)·`RunCommandMsg` 디스패치(29)·`EventMsg` 적용과 `SetViewing`(30)이 화면 Task 공통이라 시작 시점을 **core Task 30 완료**로 통일한다(Task 31 alert·32 `state.json`·33–35 화면/진입점은 chat 시험이 쓰지 않는다 — Task 4 Step 5 의 `go build ./cmd/masil/` 는 Phase 0 의 `main.go` 로 충분). core plan 03 Task 36(`ui/e2e`)은 이 계획이 끝난 뒤 core 가 실행한다.

## Global Constraints

- 모듈 `github.com/patrache/masil`, `go 1.26`. chat 은 `go.mod`/`go.sum` 을 고치지 않는다 — `go get`·`go mod tidy` 금지(CA 0·1.3). 필요한 모듈은 CA 1.2 목록 안의 하위 패키지만 import.
- 이 계획이 만들거나 고치는 파일은 **`internal/store/chatstore/**`, `internal/ui/chat/**`, `cmd/masil/register_chat.go`** 뿐이다(CA 2.1). core·sns-fun 파일, `fixtures` 패키지, `docs/contract/*` 는 읽기만.
- import 방향(CA 2.2): `ui/chat` → `ui, ui/widgets, ui/palette, ui/keymap, ui/layout(타입), store, store/chatstore, api, markup, theme, textwidth, humanize, slash, ids`. `ui/feed`·`ui/fun`·`ui/app` 을 import 하지 않는다(시험의 `ui/uitest` 경유는 허용). 화면 이동은 `ui.Navigate`/`ui.NavigateTo` 로만.
- chat 시험은 `uitest/stub.Install()` 을 부르지 않는다(chat 이 `RouteChannel`/`RouteThread`/`RouteHome` 을 등록하므로 중복 등록 패닉).
- Bubble Tea **v2**: `tea.KeyPressMsg`(`msg.String()`), `tea.MouseClickMsg`/`tea.MouseWheelMsg`(`msg.Mouse()` → `tea.Mouse{X,Y,Button}`, `tea.MouseLeft`/`tea.MouseRight`), `tea.PasteMsg{Content}`, `tea.SetClipboard(s)`, `tea.Batch`. chat 컴포넌트는 `ui.Component`(`View(ctx, focused) string`)를 구현하며 `tea.Model` 을 구현하지 않는다.
- 컴포넌트는 포인터 수신자·제자리 변경. `View` 는 **정확히 size.H 줄 × size.W 칸**(`textwidth.Fit`), 모든 출력은 `ctx.Theme.S.*` 스타일로 감싼다(빈 칸도 `S.Base`). 외부 문자열(이름·본문·채널명·이모지)은 그리기 직전 `textwidth.Sanitize`; 전송 데이터에는 절대 적용하지 않는다.
- 시간 지연은 **오직 `ui.After(ctx, d, msg)`**. `tea.Tick`·`tea.Every`·`tea.Sequence`·`time.Sleep`·`time.After`·`time.NewTimer`·`spinner.Model.Tick` 금지(CA 5.3.2; core 의 정적 검사 시험이 `internal/ui/...` 전체를 검사). 시각 비교는 `ctx.Now()`.
- REST 호출은 `ui.Do(ctx, tag, fn)` 만, 태그는 `"chat.<동작>:<대상ID>"`(예 `chat.history:01JB…`). 처리 안 한 오류는 `ui.ErrorMsg{Err, Context}` 로 돌려 앱 토스트(`api.Describe`).
- 텍스트 입력(입력창 제외 모든 `textinput`): `SetVirtualCursor(false)`, `KeyMap.DeleteAfterCursor` 에서 `ctrl+k` 제거, 글자·붙여넣기는 `textwidth.SanitizeInput` 후 투입, `ui.Cursorer` 로 실제 커서 노출(CA 5.3.7·8.4). 입력창은 `widgets.Composer`.
- 키는 CA 5.5 표의 Action 으로 `ctx.Keys.Matches(msg, action)` 판정(좁은 범위 우선: 입력 > 메시지 선택 > 목록 > 소프트 전역). 메시지 목록의 `enter`·`t` = 스레드(`msg.thread`), `r` 리액션, `e` 수정, `d` 삭제(확인), `p` 고정, `y` 복사, `m` 동작 메뉴, `/` 채널 검색, `i`/`a` 입력창, `↑`(빈 입력창) 마지막 내 메시지 수정.
- 스레드 배치(설계서 4장, CA 5.2.2·5.3.3): **지휘소** = 오른쪽 위 `SlotThread`(닫혀 있으면 그 자리에 `SlotMembers`), **표준** = 대화창 아래 분할 `SlotThread`, **컴팩트·위젯·초소형** = Main 전체 교체, `esc` 로 채널 복귀. chat 은 `ui.Navigate(ui.Route{Kind: ui.RouteThread, ID: rootID, Arg: channelID})` 만 내고 배치는 core 가 한다.
- 밀도(CA 6.1): `DensityFull` 이름·시간 줄+본문+리액션/답글 줄+날짜 구분선 / `DensityLine` `11:02 민지 오늘 배포… 💬3` 한 줄 / `DensityBubble` 이름·시간 위·말풍선·**내 메시지 오른쪽 정렬**(`11:05 나`) / `DensityMinimal` 한 줄·이름 축약.
- 차단한 사용자의 메시지는 `차단한 사용자의 메시지` 한 줄로 접는다(api.md 3.2). 입력 중·자동완성에서도 차단 사용자 제외.
- 길이·형식 제한(api.md 0.3): 메시지 본문 1..4000 코드 포인트, 채널 이름 `^[a-z0-9가-힣_-]{1,32}$`, 주제 0..250, 방 이름 1..50·설명 0..200, 그룹 DM 이름 0..50·참여자 나 제외 1..9, 초대 `max_uses` 1..1000·`expires_in_sec` 300..2592000(생략=7일, null=무기한), 검색어 1..100.
- 슬래시(api.md 5.10, CA 4.9): `/me`→`kind:"me"`, `/shrug [글]`→본문 끝에 ``¯\_(ツ)_/¯``, `/roll 2d6`→`kind:"roll"`, `/poll`→`kind:"poll"`(스레드 불가), `/status`·`/focus` → `ui.RunCommand("status.set"|"focus.start"|"focus.stop", 인자 원문)`, `/invite @h`·`/invite link [7d] [10]`.
- 골든(CA 10): 화면(경로·창)마다 `uitest.AllSizes()` 전부, 오버레이마다 `uitest.Required` + 44×16 + 20×6, 화면마다 `fixtures.Hostile` 무결성(`h.Check()`) × `AllSizes()`. 환경: 시계 `2026-11-05T02:05:00Z`(서울 11:05), `Asia/Seoul`, 테마 D(amber), 애니메이션 끔, 실시간 끔. 갱신 `go test ./internal/ui/chat/ -update`.
- **골든 갱신 규칙**: `h.Golden` 은 셸 **전체**를 찍으므로, 뒤 Task 가 앞 골든의 **주변**(Main 자리표시 → 대화 화면, 입력창 추가, 지휘소 `SlotMembers` 채움, 홈 화면)을 바꾸면 그 골든은 낡는다. 그래서 (1) 레일·사이드바처럼 자기 창만 검증하면 되는 골든은 `paneGolden`(Task 6 헬퍼 — 그 창의 바깥 사각형만 잘라 저장)으로 찍어 주변 변화에 영향받지 않게 하고, (2) 셸 전체 골든은 주변을 바꾸는 Task 가 **갱신할 시험 이름을 명시한 단계**(Task 9·14·15, 최종 Task 23)에서만 `-run '<그 시험들>' -update` 하고 `git diff --stat internal/ui/chat/testdata` + 120×40·200×60 diff 를 열어 **바뀐 것이 그 Task 가 말한 주변뿐인지** 확인한다. 패키지 전체에 대한 무조건 `-update` 는 금지(시험을 약하게 하는 것과 같다).
- **시험용 API 서버 규칙(plan 03 Task 8 "라우팅" 결정·Task 23 Step 3 에 맞춤)**: `apitest` 는 `http.ServeMux` 에 직접 등록하지 않고 자체 라우트 표를 쓴다. 키 = 메서드 + 와일드카드 이름을 지운 경로 모양(`GET /v1/channels/{}/messages` — `{id}` 와 `{channel_id}` 는 같은 모양). 의미는 셋뿐이다: (i) **같은 모양을 다시 등록하면 패닉 없이 그 자리의 핸들러를 바꾼다(나중 등록이 이김, replace)**; (ii) 모양이 다르면 둘 다 남고 매칭은 Go 1.22 패턴 규칙(더 구체적인 쪽이 이김 — ID 를 박은 구체 경로 > `{id}` 와일드카드); (iii) 모양이 다르면서 서로 겹치고 어느 쪽도 더 구체적이지 않으면 **등록 시점에 `tb.Fatalf("apitest: pattern %q conflicts with %q")`**. `fixtures.RegisterAPI(srv)` 는 plan 03 Task 23 Step 3 의 **GET 17개 패턴만** 등록한다(쓰기 메서드·`/v1/presence`·멤버 목록·`/` catch-all 없음). 그래서 chat 시험은:
  - (a) **응답 바꾸기**: 시험 도중 같은 경로의 응답을 바꾸려면 같은 모양으로 **다시 등록**하거나(나중 등록이 이김), 한 핸들러 안에서 `r.URL.Query()`·클로저 변수로 가른다 — 둘 다 허용. 단 와일드카드 모양으로 다시 등록하면 **그 모양의 모든 ID** 응답이 바뀐다(RegisterAPI·`chatRoutes` 의 그 경로 전체가 대체됨)는 점을 안다.
  - (b) **덧씌우기**: `fixtures.RegisterAPI`·`chatRoutes` 위에 덧씌울 때는 기본적으로 **구체 경로**(`GET /v1/channels/01JB…/pins` 처럼 ID 를 박은 경로)를 쓴다 — 와일드카드보다 구체적이라 그 ID 만 이기고, 다른 ID 는 원래 핸들러가 계속 답한다. 같은 모양 와일드카드로 덧씌우는 것은 "이 시험에서 그 경로 전체를 대체한다" 는 의도일 때만 쓰고 그 줄에 주석으로 적는다. 겹치되 더 구체적이지 않은 패턴(예: `GET /v1/channels/01JB…/{x}` 는 `GET /v1/channels/{id}/messages` 와 (iii) 충돌)은 쓰지 않는다.
  - (c) **chat 전용 경로**: RegisterAPI 가 제공하지 않는 쓰기·보조 경로(전송 echo·멤버·접속 상태 등)는 `newAPI` 가 RegisterAPI **뒤에** 붙이는 `chatRoutes`(Task 4 헬퍼)로만 제공한다. replace 의미 때문에 core 가 나중에 RegisterAPI 목록을 늘려 `chatRoutes` 와 같은 모양이 생기면 **패닉도 오류도 없이 `chatRoutes` 가 RegisterAPI 응답을 말없이 덮는다**(등록 시 `recover` 로 잡을 것이 없다). 그래서 겹침 감지는 등록이 아니라 **시험**으로 한다: `chatRoutes` 는 표 `chatRouteTable`(패턴 + 표본 URL)로 등록하고, Task 4 의 `TestChatRoutesDisjointFromFixtures` 가 **RegisterAPI 만 붙인 별도 `apitest` 서버**에 표본 URL 마다 요청해 404 `not_found` 인지 확인한다(아니면 `t.Fatalf("fixtures.RegisterAPI now serves %s — drop it from chatRoutes", pattern)`). 별도 서버를 쓰는 것은 표본 요청이 시험 대상 서버의 `Requests()` 에 섞이지 않게 하기 위함이다. 이 시험이 실패하면 `chatRouteTable` 에서 그 항목을 빼고(RegisterAPI 응답을 쓴다) 그 응답 모양에 기대던 chat 시험을 확인한다 — RegisterAPI 응답을 덮어 통과시키지 않는다. 시험이 nonce echo·멤버 목록·`Sync.Bootstrapped` 같은 동작에 기대면 core fixtures 의 명시되지 않은 동작이 아니라 이 헬퍼·시드에서 온다.
- `uitest.ForEachSize(t, sizes, fn)` 는 옵션을 받지 않으므로 chat 시험은 같은 `"WxH"` 부시험 이름을 쓰는 `forEachSize` 래퍼(Task 4 `helpers_test.go`)로 `uitest.New(t, opts...)` + `h.Resize` 한다.
- 사용자 문구는 한국어, 코드 식별자·주석·커밋은 영어. 토큰·비밀을 코드·골든·로그에 남기지 않는다. `git push`·배포 없음.

## CA 변경 요청 (core 에 보낼 것 — chat 은 CA 파일을 고치지 않는다)

처리 결과는 plan 03 현행 본문(2026-11-05)을 읽어 확정한 것이다. chat 은 어느 CR 의 반영도 **기다리지 않는다** — 반영되지 않은 상태에서 무엇을 시험하고 어떻게 닫는지는 각 항목과 Task 23 Step 5 가 정한다.

- **CR-1 자동완성 중 `tab` 전달 — 처리 결과: 미반영(plan 03 에 받는 Task 없음).**
  - 문제: CA 5.3.4 2.3 은 "포커스가 입력 캡처 중이고 그 입력이 자동완성 팝업을 띄운 경우" `tab`/`shift+tab` 을 컴포넌트로 보낸다고 하지만, 앱이 그 상태를 알 인터페이스가 없다(`widgets.Composer` 는 `SetCompletion` 만 있음). plan 03 의 Task 19(`ui` 계약)·22(`Composer`)·27(키 라우팅) 어디에도 이 판정이 없고, Task 27 은 `tab` 을 언제나 하드 전역(`focus.next`, 창 1개 모드는 `tab.next` — `TestFocusRingAndTab`)으로 처리한다. 그래서 CA 5.5 `compose.complete`(`tab`) 행은 현재 어느 계획도 구현하지 않는다.
  - 제안(받을 곳 = **plan 03 Task 27** 키 라우팅, 오케스트레이터가 배정): `internal/ui` 에 선택 능력 `type Completer interface{ Completing() bool }` 추가, 앱은 하드 전역 판정 전에 포커스 컴포넌트가 `CapturesInput() && Completing()` 이면 `tab`/`shift+tab` 을 `HandleKey` 로 넘긴다(창 1개 모드의 `tab.next` 도 같음). 공개 시그니처 추가라 CA 수정 = 오너 승인 사항(plan 03 Global Constraints)이다.
  - chat 쪽: `channelView`·`threadView` 에 `Completing() bool`(입력창 포커스 + 팝업 항목 있음)을 **지금 구현**한다(Task 10) — 메서드일 뿐이라 CR-1 반영 전에도 컴파일된다. Task 10 의 시험은 자동완성 수락을 `enter` 로 한다.
  - 닫는 방법(영구 보류 금지): **Task 23 Step 5** 가 core 에 `Completer` 가 있는지 `grep` 으로 판정해 둘 중 하나를 반드시 통과시킨다 — 있으면 `TestCompletionTabAccept`(Task 10 에 적힌 시험)를 `complete_test.go` 에 추가해 통과, 없으면 `TestCompletionTabFallback`(현행 core 의미 고정: 팝업이 떠 있어도 `tab` 은 앱이 먹고 초안·인스턴스는 그대로)을 추가해 통과시키고 Task 23 완료 보고에 "CR-1 미반영 — `tab` 수락 불가(CA 5.5 `compose.complete` 미충족), plan 03 Task 27 배정 필요" 를 열린 항목으로 적는다.
- **CR-2 `Store.SetViewing` 갱신 시점 — 처리 결과: 요청과 다르게 확정됨 → chat 이 요청을 철회하고 의존을 없앤다.**
  - plan 03 Task 30 Step 3 은 "`SetViewing` 은 **매 Update 끝** 투영 결과의 `ChannelViewer` 합집합" 으로 정했다. 요청했던 "`Project` 직후, 같은 Update 의 방송 전" 이 아니므로, 같은 Update 안에서 방송되는 `ui.ModeChangedMsg`(CA 5.3.4 1단계)·탭 전환 직후 메시지를 받는 시점의 `Store.Viewing(ch)` 는 **이전 Update 의 값**이다.
  - chat 쪽 대응(Task 8): 읽음 판정을 두 단계로 나눈다 — **무장**(`maybeArmRead`)은 `Store.Viewing` 을 보지 않고(맨 아래 && 미읽음만) `ui.After(ctx, 1s, readTickMsg{ch, gen})` 를 걸고, **발사**(`readTickMsg` 처리)에서 `Store.Viewing(ch)` 를 포함한 전체 조건을 본다. `readTickMsg` 는 무장한 Update 가 끝난 뒤(= 그 Update 끝의 `SetViewing` 뒤)에 도착하므로 "매 Update 끝" 보장만으로 정확하다. 그래서 모드 전환·탭 전환으로 **새로 보이게 된 채널**도 다른 키 입력 없이 1초 뒤 읽힌다.
  - 완료 조건: Task 23 의 `TestChatModeSwitchMarksRead`(컴팩트에서 스레드가 Main 을 덮어 채널이 가려진 동안 온 메시지 → 지휘소로 크기 변경 → 키 입력 없이 `Advance(1s)` 뒤 `PUT …/read` 정확히 1회, 그 전에는 0회)와 Task 8 의 `TestChannelReadOnlyWhileViewing` 이 함께 통과해야 한다.

## Review Focus

1. **과거를 읽는 중(위로 스크롤) 새 메시지 도착** — 스크롤·선택이 튀지 않고 `새 메시지 N개 ↓` 배너만 뜬다, `G`/`end` 로 내려가면 사라진다. → Task 8 `TestChannelNewMessageWhileScrolledUp`.
2. **전송 실패(오프라인·5xx·429·read_only)** — 글이 사라지지 않고 실패한 대기 줄로 남으며, `enter` 재전송은 **같은 `client_nonce`** 를 쓴다(중복 메시지 없음). → Task 9 `TestComposerSendFailureRetrySameNonce`, `TestComposerSendNoDuplicate`.
3. **입력·수정·자동완성 도중 창 크기 변경** — 초안·수정 대상·자동완성 팝업·커서 위치(글 바로 뒤)가 모드를 오가도 유지. → Task 9 `TestComposerDraftSurvivesModeSwitch`, Task 23 `TestChatModeSwitchPreserves`.
4. **보고 있는 채널이 삭제되거나 방에서 강퇴됨** — 패닉 없이 `삭제되었거나 볼 수 없는 채널이에요` 안내, 입력창 비활성, 다른 탭 이동 가능. → Task 8 `TestChannelGoneWhileViewing`.
5. **차단** — 차단 즉시(`block.updated`) 그 사람 메시지가 접히고, 입력 중 표시·`@` 자동완성에서 빠지며, 그 사람과의 1:1 DM 이 사이드바·홈·레일 안 읽음 합에서 사라진다(api.md 3.2). → Task 8 `TestChannelBlockedFolds`, Task 10 `TestMentionExcludesBlocked`, Task 14 `TestTypingExcludesBlocked`, Task 7 `TestSidebarHidesBlockedDM`, Task 15 `TestHomeHidesBlockedDM`, Task 6 `TestRailUnreadMarker`.

---

### Task 1: chatstore — 상태 골격·창·대기 메시지·입력 중

**Files:**
- Create: `internal/store/chatstore/chatstore.go`, `internal/store/chatstore/pending.go`, `internal/store/chatstore/typing.go`
- Test: `internal/store/chatstore/chatstore_test.go`

**Interfaces:**
- Consumes: `store.New() *store.Store`, `store.RegisterReducer(factory func(s *store.Store) store.Reducer)`, `(*store.Store).Reducer(name string) store.Reducer`, `store.NewWindow[T](idOf func(T) string, timeOf func(T) time.Time) *store.Window[T]`, `(*store.Store).IsMe(id) bool`, `(*store.Store).IsBlocked(id) bool`, `api.Message`, `realtime.Event`.
- Produces (CA 6.1 계약 그대로):
  - `type ChatStore struct{ /* unexported */ }`, `func Get(s *store.Store) *ChatStore`
  - `func (c *ChatStore) Name() string` (= `"chat"`), `Apply(s *store.Store, ev realtime.Event)`(Task 2 가 채움), `Reset()`
  - `func (c *ChatStore) Window(channelID string) *store.Window[api.Message]` — 없으면 빈 창을 만들어 돌려준다
  - `func (c *ChatStore) Thread(rootID string) (root *api.Message, replies *store.Window[api.Message])` — 열린 적 없으면 `(nil, 빈 창)`
  - `func (c *ChatStore) Typing(channelID string, threadRootID *string, now time.Time) []string`
  - `func (c *ChatStore) SetOpen(channelID, anchorMessageID string, atBottom bool)`, `SetClosed(channelID string)`, `SetThreadOpen(rootID string, open bool)`
  - `func (c *ChatStore) Pending(channelID string) []PendingMessage`; `type PendingMessage struct{ Nonce, Body, Kind string; ThreadRootID *string; Failed error; SentAt time.Time }`
- Produces (chat 내부 추가 — `internal/ui/chat` 만 쓴다, 다른 계획은 쓰지 않음):
  - `func (c *ChatStore) HasWindow(channelID string) bool`, `Loaded(channelID string) bool`
  - `func (c *ChatStore) AddPending(channelID string, p PendingMessage)`, `FailPending(channelID, nonce string, err error)`, `RetryPending(channelID, nonce string, at time.Time)`(Failed=nil, SentAt=at), `DropPending(channelID, nonce string)`
  - `func (c *ChatStore) SetPendingPoll(channelID, nonce string, poll *api.PollInput)`, `PendingPoll(channelID, nonce string) *api.PollInput` — `PendingMessage`(CA 6.1 고정)에 `Poll` 칸이 없으므로 `/poll` 대기의 투표 내용을 nonce 별로 따로 보관(재전송용). 대기 항목이 사라지는 모든 경로(`DropPending`, nonce 병합, 채널 정리, `Reset`)에서 같이 지운다.
  - `func (c *ChatStore) SetClock(now func() time.Time)` — 입력 중 만료를 **로컬 수신 시각** 기준으로 계산하기 위한 시계(기본 `time.Now`). chat 컴포넌트는 `chatOf(ctx)`(Task 4) 로 `ctx.Now` 를 넣는다.
  - `func (c *ChatStore) SelectedSpace() string`, `SetSelectedSpace(id string)`, `ActiveChannel() string`, `SetActiveChannel(id string)`

- [ ] **Step 1: Write the failing test**

```go
func TestGetRegistered(t *testing.T) {
	s := store.New()
	c := Get(s)
	if c == nil || c.Name() != "chat" { t.Fatalf("chat reducer not registered") }
	if Get(s) != c { t.Fatal("Get must return the same instance") }
}
func TestWindowCreatedOnDemand(t *testing.T) {
	c := Get(store.New())
	if c.HasWindow("C1") { t.Fatal("no window yet") }
	w := c.Window("C1")
	if w == nil || !c.HasWindow("C1") || c.Loaded("C1") { t.Fatal("window created, not loaded") }
}
func TestPendingLifecycle(t *testing.T) {
	c := Get(store.New())
	at := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	c.AddPending("C1", PendingMessage{Nonce: "N1", Body: "안녕", Kind: "text", SentAt: at})
	c.AddPending("C1", PendingMessage{Nonce: "N2", Body: "둘", Kind: "text", SentAt: at.Add(time.Second)})
	c.FailPending("C1", "N1", errors.New("boom"))
	p := c.Pending("C1")
	if len(p) != 2 || p[0].Nonce != "N1" || p[0].Failed == nil { t.Fatalf("got %+v", p) }
	c.RetryPending("C1", "N1", at.Add(2*time.Second))
	if c.Pending("C1")[0].Failed != nil { t.Fatal("retry clears failure") }
	c.DropPending("C1", "N1")
	if got := c.Pending("C1"); len(got) != 1 || got[0].Nonce != "N2" { t.Fatalf("got %+v", got) }
}
func TestPendingPollKeptUntilDropped(t *testing.T) {
	c := Get(store.New())
	poll := &api.PollInput{Question: "점심", Options: []string{"국밥", "돈까스"}}
	c.AddPending("C1", PendingMessage{Nonce: "P1", Kind: "poll", Body: "📊 점심"})
	c.SetPendingPoll("C1", "P1", poll)
	c.FailPending("C1", "P1", errors.New("503"))
	if c.PendingPoll("C1", "P1") != poll { t.Fatal("poll input survives failure (needed for resend)") }
	c.DropPending("C1", "P1")
	if c.PendingPoll("C1", "P1") != nil { t.Fatal("dropped with the pending item") }
}
func TestTypingExpiryAndFilter(t *testing.T) {
	s := store.New()
	s.Me = &api.Me{UserSummary: api.UserSummary{ID: "ME"}}
	s.Blocks = map[string]time.Time{"BAD": {}}
	c := Get(s)
	t0 := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	now := t0
	c.SetClock(func() time.Time { return now })
	skewed := t0.Add(-time.Hour) // server clock far behind: must not matter (expiry is local receipt time + expires_in_ms)
	for _, u := range []string{"U1", "ME", "BAD"} {
		c.Apply(s, realtime.Event{Type: "typing", TS: skewed, Data: mustJSON(t, realtime.Typing{ChannelID: "C1", UserID: u, ExpiresInMS: 6000})})
	}
	if got := c.Typing("C1", nil, t0.Add(5*time.Second)); !slices.Equal(got, []string{"U1"}) { t.Fatalf("got %v", got) }
	if got := c.Typing("C1", nil, t0.Add(6*time.Second)); len(got) != 0 { t.Fatalf("expired, got %v", got) }
	now = t0.Add(10 * time.Second)
	c.Apply(s, realtime.Event{Type: "typing", Data: mustJSON(t, realtime.Typing{ChannelID: "C1", UserID: "U1", ExpiresInMS: 6000})}) // zero TS is fine
	if got := c.Typing("C1", nil, now.Add(time.Second)); !slices.Equal(got, []string{"U1"}) { t.Fatalf("zero TS, got %v", got) }
	root := "R1"
	if got := c.Typing("C1", &root, t0); len(got) != 0 { t.Fatal("thread scope is separate") }
}
func TestResetClearsEverything(t *testing.T) { /* AddPending+Window+SetSelectedSpace → Reset → HasWindow false, Pending empty, SelectedSpace "" */ }
```

`mustJSON(t, v) json.RawMessage` 는 이 시험 파일의 헬퍼(`json.Marshal`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/chatstore/ -run 'TestGetRegistered|TestWindowCreatedOnDemand|TestPendingLifecycle|TestPendingPollKeptUntilDropped|TestTypingExpiryAndFilter|TestResetClearsEverything' -v`
Expected: FAIL — `undefined: Get` (패키지 미구현).

- [ ] **Step 3: Implement `chatstore.go`, `pending.go`, `typing.go`**

- `init()` 에서 `store.RegisterReducer(func(s *store.Store) store.Reducer { return newChatStore(s) })`. `ChatStore` 는 `*store.Store` 포인터를 보관(나·차단 판정용). `Get` = `s.Reducer("chat").(*ChatStore)`.
- 창: `map[string]*store.Window[api.Message]`(idOf=`m.ID`, timeOf=`m.CreatedAt`), `loaded map[string]bool`, 스레드 `map[string]*threadState{root *api.Message; replies *store.Window[api.Message]; open bool}`, `open map[string]openState{anchor string; atBottom bool}`.
- 대기: 채널별 슬라이스(추가 순서 유지). 투표 입력은 unexported `polls map[string]map[string]*api.PollInput`(채널 → nonce) — 대기 항목과 수명이 같다.
- 입력 중: 키 `(channelID, rootID|"")` → `userID → expiresAt`. 이 Task 의 `Apply` 는 `typing` 만 처리: `expiresAt = c.now() + ExpiresInMS` — **서버 `ev.TS` 는 쓰지 않는다**(클라이언트·서버 시계 차이로 표시가 안 뜨거나 남는 일을 막고, `ev.TS` 가 0 이어도 동작). `c.now` 는 `SetClock` 으로 받은 함수(기본 `time.Now`; `internal/store` 는 타이머 정적 검사 대상이 아니다). `Typing` 은 `now < expiresAt` 이고 `!IsMe && !IsBlocked` 인 사용자 ID 를 **처음 받은 순서**로.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/chatstore/ -v`
Expected: PASS (`ok  github.com/patrache/masil/internal/store/chatstore`)

- [ ] **Step 5: Commit**

```bash
git add internal/store/chatstore/
git commit -m "feat(chatstore): reducer skeleton, windows, pending sends, typing"
```

---

### Task 2: chatstore — 실시간 이벤트 규칙 (events.md 4.2·4.4)

**Files:**
- Create: `internal/store/chatstore/apply.go`
- Modify: `internal/store/chatstore/chatstore.go` (`Apply` 가 `apply.go` 의 분기로 위임)
- Test: `internal/store/chatstore/apply_test.go`

**Interfaces:**
- Consumes: `realtime.Event.Decode() (any, error)` 와 결과 타입 `realtime.MessageEvent`, `MessageDeleted`, `ThreadUpdated`, `ReactionEvent`, `PinEvent`, `PollUpdated`, `Typing`, `ChannelDeleted`, `SpaceDeleted`, `SpaceRemoved`; `store.Window` 의 `Upsert/Update/Remove/Get/Covers/HasMoreAfter/Len/Items`.
- Produces (chat 내부 추가):
  - `func (c *ChatStore) TakeMissing(channelID string) []string` — 범위 안인데 모르는 메시지 ID(중복 없음, 꺼내면 비움)
  - `func (c *ChatStore) FindMessage(id string) (api.Message, bool)` — 채널 창·스레드 루트·답글 전부에서
  - `func (c *ChatStore) Pins(channelID string) (pins []api.Message, loaded bool)`, `SetPins(channelID string, pins []api.Message)`

- [ ] **Step 1: Write the failing test**

공통 준비 `loadedStore(t) (*store.Store, *ChatStore)`: 나=`ME`, `c.SetClock(func() time.Time { return t0 })`(t0 = 2026-11-05T02:05:00Z), 채널 `C1` 창을 `ApplyPage` 없이 `Window("C1").Replace([m10,m20,m30], false, false)` + 내부 `loaded["C1"]=true`(시험 전용 `markLoaded` 대신 Task 3 전까지는 같은 패키지 시험에서 필드 직접 설정). `msg(id, author string)` 헬퍼.

```go
func TestCreatedInsertsByIDOrder(t *testing.T)        // m25 → Items IDs [10,20,25,30]; m05 (older than window, hasMoreBefore=true) ignored
func TestCreatedAppendsWhenNoMoreAfter(t *testing.T)  // m40 appended; with hasMoreAfter=true m40 ignored
func TestCreatedMergesPendingByNonce(t *testing.T)    // AddPending(C1,{Nonce:"N1"}) + SetPendingPoll(C1,"N1",p); created m41 ClientNonce=&"N1" → Pending(C1) empty, PendingPoll nil, Window has m41 once
func TestCreatedClearsAuthorTyping(t *testing.T)      // typing U1 then created by U1 → Typing(C1,nil,t0) empty
func TestThreadReplyGoesToOpenThreadOnly(t *testing.T)// SetThreadOpen("m20",true) + reply ThreadRootID=&m20 → replies has it; channel window unchanged; closed thread → ignored
func TestUpdatedKeepsLocalReactionMe(t *testing.T)    // local m20 reactions [{👍,2,me:true}]; updated payload [{👍,3,false},{👀,1,false}] → [{👍,3,true},{👀,1,false}]
func TestUpdatedUnknownInRangeRecordsMissing(t *testing.T) // updated m15 (not loaded, Covers) → TakeMissing(C1)==[m15]; second call empty
func TestDeletedTombstoneVsRemove(t *testing.T)       // tombstone:true → Deleted, Body "", Reactions empty, Pinned false; false → removed
func TestThreadUpdatedSetsSummary(t *testing.T)       // thread.updated root m20 reply_count 4 → Window m20 Thread.ReplyCount==4 and thread root too
func TestReactionAddedRemoved(t *testing.T)           // added 👍 by U1 count 3 → count 3, me unchanged; added by ME → me true; removed count 0 → emoji gone
func TestPinnedUpdatesFlagAndPins(t *testing.T)       // SetPins(C1,[]) then pinned m20 → m20.Pinned, Pins contains m20 first; unpinned → removed
func TestPollUpdatedKeepsMyVotes(t *testing.T)        // m30 poll my_votes [0]; poll.updated my_votes [] counts changed → counts new, MyVotes [0]
func TestChannelDeletedDropsCaches(t *testing.T)      // s.Channels has no C1 + channel.deleted C1 → HasWindow(C1) false, Pending/Pins cleared
func TestSpaceRemovedDropsGoneChannels(t *testing.T)  // windows C1 (still in s.Channels) and C2 (not) + space.removed → C2 dropped, C1 kept
func TestUnknownEventIgnored(t *testing.T)            // Type "post.stats" → no panic, state unchanged
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/chatstore/ -run 'TestCreated|TestThread|TestUpdated|TestDeleted|TestReaction|TestPinned|TestPoll|TestChannelDeleted|TestSpaceRemoved|TestUnknown' -v`
Expected: FAIL — `undefined: (*ChatStore).TakeMissing` 등.

- [ ] **Step 3: Implement `apply.go`**

규칙(events.md 4.2·4.4 그대로, 순서 무관 ID 병합):
- 최상위 메시지를 넣을 조건 `inRange(ch, id)`: 창이 있고 `Loaded` 이며 (`w.Covers(id)` 또는 (`w.Len()==0 && !w.HasMoreAfter()`)). 스레드 답글은 `threads[root].open` 일 때만 답글 창에 같은 규칙.
- `message.created`: 넣기 + `ClientNonce` 가 같은 대기 항목 제거 + 같은 (채널, 스레드) 의 작성자 입력 중 제거.
- `message.updated`: 아는 메시지면 전체 교체하되 `reactions[].me` 는 이모지별 로컬 값 유지(로컬에 없던 이모지는 `false`), `poll.my_votes` 도 로컬 유지. 모르는데 `inRange` → `missing[ch]` 에 추가. 스레드 루트 사본도 같이 갱신.
- `message.deleted`: `tombstone` → `Deleted=true, Body="", Reactions=nil, Pinned=false`; 아니면 `Remove`. 고정 목록에서도 제거.
- `thread.updated`: 루트의 `Thread` 교체(채널 창·스레드 루트 둘 다).
- `reaction.*`: 메시지를 `ThreadRootID` 로 찾아 `count` 반영(0 이면 제거, 처음 보는 이모지는 끝에 추가), `UserID` 가 나면 `me` 를 added/removed 로. 모르는데 `inRange` → missing.
- `message.pinned/unpinned`: `Pinned` 반영, 고정 목록이 `loaded` 면 앞에 넣기/빼기.
- `poll.updated`(`ContextType=="message"`): `ContextID` 메시지의 `Poll` 교체, `MyVotes` 로컬 유지.
- `channel.deleted`, `space.deleted`, `space.removed`: `s.Channels` 에 없는 채널의 창·대기·고정·입력 중·열린 상태를 모두 지운다.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/chatstore/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/chatstore/
git commit -m "feat(chatstore): apply realtime message/thread/reaction/pin/poll events"
```

---

### Task 3: chatstore — REST 결과 병합과 재동기화(Resyncer)

**Files:**
- Create: `internal/store/chatstore/merge.go`, `internal/store/chatstore/resync.go`
- Test: `internal/store/chatstore/merge_test.go`, `internal/store/chatstore/resync_test.go`

**Interfaces:**
- Consumes: `api.MessageQuery{Before, After, Around string; Limit int}`, `api.MessageWindow{Items; HasMoreBefore, HasMoreAfter}`, `api.ThreadQuery`, `api.ThreadWindow{Root; Items; HasMoreBefore, HasMoreAfter}`, `(*api.Client).ListMessages`, `(*api.Client).GetThread`, `store.Window.Replace/PrependOlder/AppendNewer`, `store.Resyncer`, `apitest.New/JSON/Requests/Client`.
- Produces (chat 내부 추가):
  - `func (c *ChatStore) ApplyPage(channelID string, q api.MessageQuery, w *api.MessageWindow)` — 앵커 없음/`Around` → `Replace`, `Before` → `PrependOlder`, `After` → `AppendNewer`; `loaded=true`; 결과의 `client_nonce` 와 같은 대기 제거
  - `func (c *ChatStore) ApplyThreadPage(rootID string, q api.ThreadQuery, w *api.ThreadWindow)`
  - `func (c *ChatStore) UpsertMessage(m api.Message)` — REST 결과(전송·수정·`GetMessage`) 반영, 대기 병합, `reactions[].me` 는 REST 값 사용
  - `func (c *ChatStore) SetMyReaction(messageID, emoji string, on bool) bool` — 낙관적 ±1·`me` 토글, 메시지 없으면 false
  - `func (c *ChatStore) SetPoll(p api.Poll, authoritative bool)` — true(REST `Vote` 결과)면 `MyVotes` 포함 교체
  - `func (c *ChatStore) Resync(s *store.Store, cl *api.Client) tea.Cmd` (`store.Resyncer`)
  - `type ResyncResultMsg struct{ Gen uint64; Channels map[string]*api.MessageWindow; Threads map[string]*api.ThreadWindow; Errs map[string]error }`
  - `func (c *ChatStore) ApplyResync(m ResyncResultMsg)` — 같은 `Gen` 두 번째부터 무시

- [ ] **Step 1: Write the failing test**

```go
func TestApplyPageModes(t *testing.T)        // latest → Replace; Before=first → PrependOlder (HasMoreBefore from page); After → AppendNewer dedup by ID; Around → Replace
func TestApplyPageMergesPending(t *testing.T) // pending N1 + page containing message with client_nonce N1 → Pending empty
func TestUpsertFromSendResult(t *testing.T)   // loaded empty window, pending N1, UpsertMessage(m with nonce N1) → Items==[m], Pending empty
func TestSetMyReactionOptimistic(t *testing.T)// none→on: new chip {😀,1,true}; on again no-op count; off → removed when count 0; unknown id → false
func TestSetPollAuthoritative(t *testing.T)   // authoritative true replaces MyVotes; false keeps local
func TestResyncRefetchesOpenWindows(t *testing.T) {
	srv := apitest.New(t)
	srv.JSON("GET /v1/channels/C1/messages", 200, map[string]any{"items": []api.Message{msgAt("m50", "C1")}, "has_more_before": true, "has_more_after": false})
	srv.JSON("GET /v1/channels/C2/messages", 200, map[string]any{"items": []api.Message{msgAt("m20", "C2")}, "has_more_before": true, "has_more_after": true})
	srv.JSON("GET /v1/messages/R1/thread", 200, map[string]any{"root": msgAt("R1", "C1"), "items": []api.Message{}, "has_more_before": false, "has_more_after": false})
	s := store.New(); c := Get(s)
	c.Window("C1"); c.SetOpen("C1", "", true)      // bottom => latest page
	c.Window("C2"); c.SetOpen("C2", "m20", false)  // scrolled => ?around=m20
	c.Window("C3")                                  // not open => dropped immediately
	c.SetThreadOpen("R1", true)
	msg := runCmd(t, c.Resync(s, srv.Client())).(ResyncResultMsg)
	if c.HasWindow("C3") { t.Fatal("closed cache must be dropped at Resync time") }
	q := queriesOf(srv.Requests())   // path -> raw query (api.Client encodes with url.Values.Encode: keys sorted)
	if q["/v1/channels/C1/messages"] != "limit=50" || q["/v1/channels/C2/messages"] != "around=m20&limit=50" || q["/v1/messages/R1/thread"] != "limit=50" { t.Fatalf("%v", q) }
	c.ApplyResync(msg); c.ApplyResync(msg) // idempotent
	if ids(c.Window("C1").Items()) != "m50" || !c.Loaded("C2") { t.Fatal("windows replaced") }
}
```

`runCmd(t, cmd) tea.Msg` 는 `tea.BatchMsg` 를 펴서 첫 non-nil 결과를 돌려주는 시험 헬퍼, `queriesOf`·`ids`·`msgAt` 도 시험 파일 헬퍼.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/chatstore/ -run 'TestApplyPage|TestUpsert|TestSetMy|TestSetPoll|TestResync' -v`
Expected: FAIL — `undefined: (*ChatStore).ApplyPage`.

- [ ] **Step 3: Implement `merge.go`, `resync.go`**

- `Resync` 는 UI 고루틴에서 불리므로 그 자리에서 열리지 않은 창(`open` 에 없는 채널)을 지우고, `Gen++` 후 **하나의 Cmd**(내부에서 열린 채널·스레드를 차례로 `ListMessages`/`GetThread`, ctx 15초)로 `ResyncResultMsg` 를 돌려준다(store 는 Cmd 안에서 건드리지 않는다). 채널 질의: `atBottom` → `MessageQuery{Limit: 50}`(질의 문자열 `limit=50`), 아니면 `MessageQuery{Around: anchor, Limit: 50}`(`around=<id>&limit=50`). 스레드: `ThreadQuery{Limit: 50}`(`limit=50`). Resync 도 `Limit` 을 항상 보낸다 — Task 8 의 `limit=50` 질의와 같은 규칙.
- `ApplyResync` 는 `ApplyPage`/`ApplyThreadPage` 로 교체. 이 메시지를 받은 chat 컴포넌트(아무나)가 호출한다(Task 8). `Errs` 의 채널은 창을 그대로 둔다.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/chatstore/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/chatstore/
git commit -m "feat(chatstore): REST page merge, optimistic reactions, resync"
```

---

### Task 4: ui/chat 골격 — 등록 파일·공용 헬퍼·메뉴/폼 오버레이·시험 헬퍼

**Files:**
- Create: `cmd/masil/register_chat.go`, `internal/ui/chat/chat.go`, `internal/ui/chat/menu.go`, `internal/ui/chat/form.go`
- Test: `internal/ui/chat/helpers_test.go`, `internal/ui/chat/overlay_test.go`

**Interfaces:**
- Consumes: `ui.Context`, `ui.Overlay{Component; Bounds(body layout.Rect) layout.Rect; Modal() bool}`, `ui.KeyHandler`, `ui.MouseHandler`, `ui.Cursorer`, `ui.Titled`, `ui.Helper`, `ui.CloseOverlayMsg`, `ui.OpenOverlay(o)`, `ui.ResultMsg[T]`, `textwidth.Fit/Truncate/Sanitize/SanitizeInput`, `textinput.New()` + `SetVirtualCursor(false)` + `Cursor() *tea.Cursor`, `uitest.New/Resize/Golden/Check/Press/Type/Paste/Click/Send/Advance/Event/Screen/Cursor/Nav/Instance/Context/TeaMsgs`, `uitest.WithSeed/WithAPI/WithRoute/WithPin`, `fixtures.Seed/Hostile/RegisterAPI`, `apitest.New/JSON/Handle/Error/Requests`.
- Produces (패키지 `chat` 내부, 이후 Task 가 사용):
  - `func chatOf(ctx *ui.Context) *chatstore.ChatStore` — `chatstore.Get(ctx.Store)` + `SetClock(ctx.Now)`(포인터 대입, 멱등). chat 코드는 `chatstore.Get` 대신 **항상 이것**을 쓴다(입력 중 만료가 `ctx.Now` 기준 — 시험에서는 FakeClock).
  - `func visibleDMs(ctx *ui.Context) []*api.Channel` — `Store.DMOrder` 에서 **상대가 `Store.IsBlocked` 인 1:1 DM 을 뺀** 목록(api.md 3.2: 차단한 쪽 화면에서 그 대화 숨김, `GET /v1/dms` 도 뺌 — 실시간 `block.updated` 뒤 재동기화 전까지도 같게). 그룹 DM 은 남긴다(메시지만 접힘). 사이드바·홈·레일 홈 안 읽음 합은 이것만 쓴다.
  - `func pendingFor(ctx *ui.Context, channelID, rootID string) []chatstore.PendingMessage` — `Pending(channelID)` 중 `rootID==""` 이면 `ThreadRootID == nil` 인 것만(채널 목록), 아니면 `*ThreadRootID == rootID` 인 것만(스레드 목록). `Pending` 은 채널 단위라 스레드 답글 대기도 섞여 있으므로 `buildRows` 에 넘기기 전 반드시 이것으로 거른다.
  - `func viewedChannel(ctx *ui.Context) *api.Channel` — `chatstore.ActiveChannel()` 가 `Store.Viewing` 이면 그것, 아니면 보이는 채널 중 ID 가 가장 작은 것, 없으면 nil
  - `func spaceOf(ctx *ui.Context, ch *api.Channel) *api.SpaceDetail`, `func currentSpaceID(ctx *ui.Context) string` (보는 채널의 방 → `SelectedSpace` → `SpaceOrder[0]` → `""`)
  - `func myRole(ctx *ui.Context, spaceID string) api.SpaceRole`, `func canManageSpace(ctx *ui.Context, spaceID string) bool`(owner|admin)
  - `func canPin(ctx *ui.Context, ch *api.Channel) bool`(text: owner|admin, dm/group_dm: 누구나), `func canDelete(ctx *ui.Context, ch *api.Channel, m api.Message) bool`(내 것, 또는 text 채널에서 owner|admin)
  - `func adoptSpace(s *store.Store, d *api.SpaceDetail)` — `Spaces[id]=d`, `SpaceOrder` 에 없으면 뒤에 추가, `ObserveChannel` 각 채널 (멱등)
  - `func markReadLocal(s *store.Store, st api.UnreadState)` — 채널 `My.UnreadCount/MentionCount/LastReadMessageID` 반영
  - `func channelTitle(ctx *ui.Context, ch *api.Channel, withTopic bool) string` — `"# 개발"`, `"# 개발 · 백엔드 얘기하는 곳"`, DM `"민지 ●"`(접속 점 ●◐⊘○), 그룹 `Store.ChannelLabel`; 비공개 text 는 `"🔒개발"`
  - `func resultOf(msg tea.Msg) (tag string, err error, ok bool)` — `ui.ResultMsg[T]` 를 reflect 로 판별
  - `type menuItem struct{ Label, Hint string; Danger, Disabled bool; Run func(ctx *ui.Context) (tea.Cmd, ui.Overlay) }`, `func newMenu(title string, items []menuItem) ui.Overlay`
  - `type fieldKind int` (`fieldText`, `fieldChoice`, `fieldToggle`), `type field struct{ Key, Label, Placeholder, Value string; Kind fieldKind; Choices []string; Max int; Validate func(string) string }`
  - `type formSpec struct{ Title, Submit string; Fields []field; OnSubmit func(ctx *ui.Context, vals map[string]string) (tag string, cmd tea.Cmd); OnDone func(ctx *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay) }`, `func newForm(spec formSpec) ui.Overlay`
  - 시험 헬퍼(`helpers_test.go`): `seeded() *store.Store`, `chanID(t, s, name string) string`, `dmWith(t, s, handle string) string`, `userBy(t, s, handle string) api.UserSummary`, `spaceByName(t, s, name string) string`, `hostileChannel(t, s) string`, `newAPI(t) *apitest.Server`(= `apitest.New` + `fixtures.RegisterAPI` + `chatRoutes`, 이 순서 — 시험이 그 뒤에 덧씌운다), `var chatRouteTable []chatRoute`(`type chatRoute struct{ Pattern, Sample string; H http.HandlerFunc }` — `Sample` 은 그 패턴에 맞는 구체 표본 URL, 예 `POST /v1/channels/01JBPROBE00000000000000000/messages`), `chatRoutes(t, srv)`(`chatRouteTable` 을 차례로 `srv.Handle`. RegisterAPI 가 제공하지 않는 경로를 **와일드카드 패턴**으로 등록: `POST /v1/channels/{id}/messages`·`POST /v1/messages/{id}/thread` = 보낸 `client_nonce`·`kind`·`body`·`poll` 을 그대로 담은 201 `{"message":…,"created":true}`(새 ID, 작성자 sam, 시각 = 고정 시계), `PATCH /v1/messages/{id}`·`DELETE /v1/messages/{id}`·`PUT|DELETE /v1/messages/{id}/reactions/{emoji}`·`PUT|DELETE /v1/messages/{id}/pin`·`PUT /v1/channels/{id}/read` = api.md 해당 절 형식의 성공 응답(읽음은 `{"state": UnreadState}` — `unread_count`·`mention_count` 0, `last_read_message_id` = 보낸 `message_id`), `GET /v1/channels/{id}/members` = `seeded()` 사용자 sam·minji·junho·hana·dev_kim, `GET /v1/presence` = `ids` 별 fixtures 접속 상태. 같은 모양 재등록은 apitest 에서 replace(패닉 없음)이므로 `recover` 감지는 쓰지 않는다 — RegisterAPI 와의 겹침은 `TestChatRoutesDisjointFromFixtures` 가 잡는다(Global Constraints 시험용 API 서버 규칙 (c))), `open(t, sz uitest.Size, r ui.Route, opts ...uitest.Option) *uitest.Harness`, `openWith(t, sz, r, srv, opts...)`, `forEachSize(t, sizes []uitest.Size, mk func(t *testing.T, sz uitest.Size) *uitest.Harness, fn func(t *testing.T, h *uitest.Harness))`, `var overlaySizes []uitest.Size`(Required+44×16+20×6), `type recordRT struct{ Typed []string; Watched [][]string; Idle []bool }`(ui.Realtime 구현, `Typed` 원소 `"<ch>|<root>"`), `reqs(srv, method, path string) []apitest.Recorded`, `lineWith(t, screen, s string) (row, col int)`, `msgID(i int) string`(= `fmt.Sprintf("01JCTEST%018d", i)`), `mkMsg(ch string, i int, author api.UserSummary, body string, at time.Time) api.Message`, `sameColor(a, b color.Color) bool`

- [ ] **Step 1: Write the failing test**

```go
// overlay_test.go
func TestMenuOverlayGolden(t *testing.T) {
	forEachSize(t, overlaySizes, openHome, func(t *testing.T, h *uitest.Harness) {
		h.Send(ui.OpenOverlayMsg{Overlay: newMenu("메시지", []menuItem{
			{Label: "스레드로 답글", Hint: "t"}, {Label: "리액션", Hint: "r"},
			{Label: "고정", Hint: "p", Disabled: true}, {Label: "삭제", Hint: "d", Danger: true},
		})})
		h.Golden("menu")
	})
}
func TestMenuRunsAndMorphs(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	ran := false
	h.Send(ui.OpenOverlayMsg{Overlay: newMenu("t", []menuItem{
		{Label: "A", Disabled: true},
		{Label: "B", Run: func(*ui.Context) (tea.Cmd, ui.Overlay) { ran = true; return nil, newMenu("다음", []menuItem{{Label: "C"}}) }},
	})})
	h.Press("enter") // disabled A is skipped: cursor starts on B
	if !ran || len(h.Model.Frame().Overlays) != 1 || !strings.Contains(h.Screen(), "다음") { t.Fatal("menu must morph in place, stack depth 1") }
	h.Press("esc")
	if len(h.Model.Frame().Overlays) != 0 { t.Fatal("esc closes") }
}
func TestFormGoldenAndCursor(t *testing.T) {
	spec := formSpec{Title: "방 만들기", Submit: "만들기", Fields: []field{
		{Key: "name", Label: "이름", Max: 50, Validate: func(v string) string { if v == "" { return "이름을 입력해 주세요" }; return "" }},
		{Key: "icon", Label: "아이콘", Kind: fieldChoice, Choices: []string{"없음", "🚀", "🍚"}},
		{Key: "private", Label: "비공개", Kind: fieldToggle},
	}}
	forEachSize(t, overlaySizes, openHome, func(t *testing.T, h *uitest.Harness) {
		h.Send(ui.OpenOverlayMsg{Overlay: newForm(spec)})
		h.Type("노바 ❄️")
		h.Golden("form")
		row, col := lineWith(t, h.Screen(), "노바 ❄")
		if c := h.Cursor(); c == nil || c.Y != row || c.X != col+textwidth.Width("노바 ❄") { t.Fatalf("cursor %+v", c) }
	})
}
func TestFormValidationAndDone(t *testing.T) {
	// empty name + enter → screen shows "이름을 입력해 주세요", OnSubmit not called.
	// name "a" + enter → OnSubmit returns ui.Do tag "chat.test:x" failing with 409 → api.Describe text shown, overlay stays.
	// success → OnDone called once with the ResultMsg, overlay closed (Frame().Overlays empty).
}
func TestResultOf(t *testing.T) { /* ui.ResultMsg[int]{Tag:"a",Err:e} → ("a", e, true); string → ok=false */ }
func TestAdoptSpaceIdempotent(t *testing.T) { /* twice → SpaceOrder has id once, channels observed */ }
func TestPendingForScopes(t *testing.T) { /* AddPending top-level N1 + thread reply N2 (ThreadRootID &"R") → pendingFor(ctx,ch,"")==[N1], pendingFor(ctx,ch,"R")==[N2], pendingFor(ctx,ch,"X") empty */ }
func TestVisibleDMsHidesBlocked(t *testing.T) { /* seeded: DMs 민지·준호 → Blocks[junho] → visibleDMs has only 민지; group DM with junho stays */ }
func TestChatOfSetsClock(t *testing.T) { /* chatOf(h.Context()) then typing event → Typing(ch,nil,ctx.Now()+5s) non-empty, +6s empty (FakeClock based) */ }

// helpers_test.go
func TestChatRoutesDisjointFromFixtures(t *testing.T) {
	// apitest re-registration of the same shape REPLACES silently (plan 03 Task 8), so overlap with RegisterAPI is caught here, not at Handle time.
	probe := apitest.New(t); fixtures.RegisterAPI(probe) // separate server: probe requests never show up in a test server's Requests()
	for _, r := range chatRouteTable {
		method, path, _ := strings.Cut(r.Sample, " ")
		req, _ := http.NewRequest(method, probe.URL+path, strings.NewReader("{}"))
		res, err := http.DefaultClient.Do(req)
		if err != nil { t.Fatal(err) }
		var env struct{ Error struct{ Code string } `json:"error"` }
		json.NewDecoder(res.Body).Decode(&env); res.Body.Close()
		if res.StatusCode != 404 || env.Error.Code != "not_found" {
			t.Fatalf("fixtures.RegisterAPI now serves %s — drop it from chatRoutes", r.Pattern)
		}
	}
}
func TestChatRoutesServe(t *testing.T) {
	// newAPI(t): every chatRouteTable Sample answers 2xx (POST …/messages echoes the posted client_nonce with created:true);
	// srv.JSON on an exact path afterwards wins for that id only, another id still gets the chatRoutes echo (rule (b));
	// srv.Error on the same wildcard shape afterwards replaces it for every id (rule (a), no panic)
}
```

`openHome(t, sz) *uitest.Harness` = `open(t, sz, ui.Route{Kind: ui.RouteHome})`. Task 15 전까지 `RouteHome` 은 `widgets.Placeholder` 로 그려지고 레일·사이드바·멤버 창도 Task 6·7·14 에서야 생기므로, 이 두 오버레이 골든의 배경은 Task 15 Step 4 에서 한 번 더 `-update` 한다(그 Task 에 명시). 그 사이 Task 들의 Step 은 이 시험을 `-run` 에 넣지 않는다.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run 'TestMenu|TestForm|TestResultOf|TestAdoptSpace|TestPendingFor|TestVisibleDMs|TestChatOf|TestChatRoutes' -v`
Expected: FAIL — `undefined: newMenu`.

- [ ] **Step 3: Implement `chat.go`, `menu.go`, `form.go`, `register_chat.go`**

- `register_chat.go`: `package main` + `import _ "github.com/patrache/masil/internal/ui/chat"` (CA 2.4).
- `chat.go`: 위 헬퍼들. `markReadLocal` 은 `s.Channels[id]` 가 없으면 아무것도 안 한다. 1:1 DM 의 상대 = `Channel.Members` 중 `!IsMe` 인 사람.
- 오버레이 공통: `Bounds(body)` = 가운데 상자(메뉴: 폭 `min(body.W, 40)`, 높이 항목수+2; 폼: 폭 `min(body.W, 56)`, 높이 필드수×2+4), `Modal()=true`, `Titled` 로 제목. 앱이 작은 창(초소형/24×6 미만)에서 본문 전체로 강등한다(CA 5.3.7) — 컴포넌트는 받은 크기에 맞춰 `textwidth.Fit`.
- **morph**: 항목 `Run` 이나 폼 `OnDone` 이 `ui.Overlay` 를 돌려주면 닫지 않고 그 오버레이로 **자기 자리를 바꾼다**(이후 `Bounds/View/HandleKey/Update/Cursor` 위임). 그렇지 않으면 `tea.Batch(func() tea.Msg { return ui.CloseOverlayMsg{} }, cmd)`. (닫기→열기 순서 경쟁을 없애기 위함 — `tea.Sequence` 금지.)
- 메뉴 키: `list.up/down`(j/k/↑/↓, 비활성 건너뜀), `list.open`(enter) 실행, `nav.back`(esc) 닫기, 클릭 실행. 비활성은 `S.Dim`, 위험은 `S.Error`, 힌트는 오른쪽 정렬 `S.Dim`.
- 폼: 필드마다 라벨 줄 + 값 줄. 텍스트 칸은 `textinput`(Global Constraints 규칙), `Max` 는 `CharLimit`. `tab`/`shift+tab`/`↓`/`↑` 필드 이동, 선택 칸은 `←/→`(`h`/`l`), 토글은 `space`. `enter` = 모든 `Validate` 통과 시 `OnSubmit`, 실패 시 첫 오류를 마지막 줄 `S.Error` 로. 제출 중에는 `widgets.Spinner` + 입력 무시, `resultOf(msg)` 의 태그가 같으면: 오류 → `api.Describe(err)` 표시하고 유지, 성공 → `OnDone`. 커서 = 포커스된 textinput 의 `Cursor()` + 그 칸의 내부 좌표.

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run 'TestMenuOverlayGolden|TestFormGoldenAndCursor' -update`
Expected: `ok`, 파일 `internal/ui/chat/testdata/TestMenuOverlayGolden/<WxH>/menu.golden` 등 생성. 120×40 골든을 열어 상자 테두리가 `╔═╗`(amber 이중선), 비활성 `고정` 행, 20×6 에서 테두리 없는 전체 본문 강등을 눈으로 확인.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -v && go build ./cmd/masil/`
Expected: PASS, 빌드 성공

- [ ] **Step 6: Commit**

```bash
git add cmd/masil/register_chat.go internal/ui/chat/
git commit -m "feat(chat): package scaffold, shared helpers, menu and form overlays"
```

---

### Task 5: 메시지 렌더러 — 4가지 밀도·마크다운·코드 강조·투표·시스템

**Files:**
- Create: `internal/ui/chat/msgrender.go`
- Test: `internal/ui/chat/msgrender_test.go`

**Interfaces:**
- Consumes: `markup.Parse(body string, opts markup.ParseOptions) markup.Doc`(`Hashtags:false`), `markup.Render(d, t, markup.RenderOptions{Width, MyHandle, Mentions, MentionHere, Compact, MaxLines, Hyperlinks}) []string`(코드 블록 chroma 강조 포함), `markup.Plain(body, maxCells)`, `widgets.ReactionChips(t, rs, width) []string`, `widgets.PollView(t, p, width, selected) []string`, `widgets.UserName(t, u, self) string`, `humanize.Clock(t, loc, h12)`, `humanize.DayDivider(t, loc)`, `ui.Density`, `chatstore.PendingMessage`.
- Produces:
  - `type rowKind int` (`rowMessage`, `rowPending`, `rowDay`, `rowUnread`, `rowLoading`, `rowNotice`)
  - `type row struct{ Kind rowKind; ID string; Msg *api.Message; Pending *chatstore.PendingMessage; Text string }` — ID: 메시지 ID / `"pending:"+nonce` / `"day:"+YYYYMMDD` / `"unread"` / `"loading"`
  - `type rowOpts struct{ LastRead string; Loading bool; InThread bool }`
  - `func buildRows(ctx *ui.Context, msgs []api.Message, pending []chatstore.PendingMessage, o rowOpts) []row`
  - `type msgStyle struct{ Density ui.Density; Width int; Selected, Continuation, InThread bool; Fade *lipgloss.Style }`
  - `func renderRow(ctx *ui.Context, r row, st msgStyle) []string` — 줄마다 `Width ≤ st.Width`
  - `func systemText(ctx *ui.Context, e api.SystemEvent) string`, `func rollText(r api.RollResult) string`

- [ ] **Step 1: Write the failing test**

```go
func TestRenderDensities(t *testing.T) {
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome})
	ctx := h.Context(); s := ctx.Store
	minji := userBy(t, s, "minji"); me := *s.User(s.Me.ID)
	at := time.Date(2026, 11, 5, 2, 2, 0, 0, time.UTC) // 11:02 KST
	m := mkMsg("C", 1, minji, "오늘 배포 몇 시에 해요?", at)
	m.Reactions = []api.ReactionSummary{{Emoji: "👍", Count: 2, Me: true}}
	m.Thread = &api.ThreadSummary{ReplyCount: 3}
	full := strip(renderRow(ctx, row{Kind: rowMessage, ID: m.ID, Msg: &m}, msgStyle{Density: ui.DensityFull, Width: 60}))
	mustContain(t, full[0], "민지", "11:02"); mustContain(t, strings.Join(full, "\n"), "오늘 배포 몇 시에 해요?", "답글 3개")
	line := strip(renderRow(ctx, row{Kind: rowMessage, ID: m.ID, Msg: &m}, msgStyle{Density: ui.DensityLine, Width: 60}))
	if len(line) != 1 || !strings.HasPrefix(line[0], "11:02 민지 오늘 배포 몇 시에 해요?") || !strings.Contains(line[0], "💬3") { t.Fatalf("%q", line) }
	mine := mkMsg("C", 2, me, "점심 먹고 ㄱㄱ", at.Add(3*time.Minute))
	bub := strip(renderRow(ctx, row{Kind: rowMessage, ID: mine.ID, Msg: &mine}, msgStyle{Density: ui.DensityBubble, Width: 40}))
	if strings.TrimSpace(bub[0]) != "11:05 나" || textwidth.Width(strings.TrimRight(bub[0], " ")) != 40 { t.Fatalf("mine must be right-aligned: %q", bub) }
	min := strip(renderRow(ctx, row{Kind: rowMessage, ID: m.ID, Msg: &m}, msgStyle{Density: ui.DensityMinimal, Width: 20}))
	if len(min) != 1 || !strings.HasPrefix(min[0], "민지 오늘") { t.Fatalf("%q", min) }
}
func TestRenderKinds(t *testing.T) {
	// kind "me": "* 민지 커피 마시는 중" ; roll {expr 2d6, rolls [3,5], modifier 2, total 10}: rollText == "🎲 2d6 → 3 + 5 + 2 = 10"
	// poll: first line "📊 투표: 점심 메뉴" then widgets.PollView lines; closed poll adds "(마감)"
	// system member_joined actor minji → "민지님이 들어왔어요" ; deleted → "삭제된 메시지" ; edited → suffix "(수정됨)"
	// blocked author (s.Blocks[minji]) → exactly one line "차단한 사용자의 메시지"
	// code block body "```sh\n$ wrangler deploy --env prod\n```" → a line containing "$ wrangler deploy --env prod" prefixed by "▎"
}
func TestBuildRowsDividersAndPending(t *testing.T) {
	// msgs on 11/4 and 11/5 → rowDay before each day group (Full only rendered); LastRead = id of 2nd → rowUnread right after it
	// Loading → first row rowLoading; pending appended after messages in SentAt order; failed pending renders "전송 실패"
}
func TestRenderWidthNeverExceeded(t *testing.T) {
	// for every Density × width in {1,2,3,10,33,94} × fixtures.HostileMessages(...) : every line Width ≤ width and StringWidth == StringWidthWc
}
```

`strip`, `mustContain` 는 시험 헬퍼(`ansi.Strip` 각 줄 / `strings.Contains` 다중).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run 'TestRender|TestBuildRows' -v`
Expected: FAIL — `undefined: renderRow`.

- [ ] **Step 3: Implement `msgrender.go`**

결정 사항(나머지는 시험이 정한다):
- Full: 머리줄 `<이름(UserColor, 굵게)> <시각 S.Timestamp>[ 📌]`, 연속 메시지(같은 작성자·5분 안·구분선 없음)는 머리줄 생략. 본문 `markup.Render(Width=폭-2, 들여쓰기 2칸)`. 그다음 리액션 칩 줄(`widgets.ReactionChips`)과 `💬 답글 N개`(답글 있을 때, `S.Dim`) — 칩과 답글이 한 줄에 들어가면 ` · ` 로 이음.
- Line: `HH:MM 이름 본문` 한 줄, `markup.Render(Compact:true, MaxLines:1)`, 끝에 `💬N`(답글 있을 때), 폭 넘치면 `…`.
- Bubble: 남 = `이름 HH:MM` 줄 + `S.Reaction` 배경 말풍선(폭 ≤ 80%), 내 것 = `HH:MM 나` 오른쪽 정렬 + 말풍선 오른쪽 정렬. 리액션/답글은 말풍선 아래 `💬3 👍2`.
- Minimal: `이름(6칸 자름) 본문(Plain)` 한 줄.
- 시스템 문구 표(`systemText`): member_joined `X님이 들어왔어요`, member_left `X님이 나갔어요`, member_kicked `X님이 내보내졌어요`, channel_created `#name 채널을 만들었어요`, channel_renamed `채널 이름 변경: #name`, channel_topic_changed `주제 변경: topic`, message_pinned `메시지를 고정했어요`, group_renamed `대화 이름 변경: name`, group_member_added `X님이 들어왔어요`, group_member_removed `X님이 빠졌어요`(X = target_user_id 의 표시 이름, 없으면 actor). `S.System` 으로 `── … ──` 가운데 정렬.
- 날짜 구분선은 Full 만(`humanize.DayDivider`, `S.Dim`, `─` 채움), 안 읽음 구분선 `── 새 메시지 ──`(`S.Error`)은 모든 밀도.
- 대기: 본문 `S.Dim` + ` · 보내는 중…`; 실패: `S.Error` 로 ` · 전송 실패 — enter 다시 보내기 · d 버리기`.
- 선택 행은 모든 줄에 `S.Cursorline`, `Fade` 가 있으면 그 배경.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run 'TestRender|TestBuildRows' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/chat/msgrender.go internal/ui/chat/msgrender_test.go
git commit -m "feat(chat): message renderer for full/line/bubble/minimal densities"
```

---

### Task 6: 방 레일 창 (`SlotRail`, 지휘소)

**Files:**
- Create: `internal/ui/chat/rail.go`
- Test: `internal/ui/chat/rail_test.go`

**Interfaces:**
- Consumes: `ui.RegisterPane(layout.SlotRail, ui.PaneFactory)`, `Store.SpaceOrder/Spaces/SpaceChannels/Channels`, `visibleDMs`(Task 4), `chatstore.Get(s).SetSelectedSpace`, `ui.Navigate`, `ui.OpenOverlay`, `widgets.List[T]`.
- Produces: `type railPane struct{...}`, `func newRail(ctx *ui.Context) ui.Component`(KeyHandler·MouseHandler); 오버레이 `func newSpaceAddMenu() ui.Overlay`(`chat.space_add`: `방 만들기` → `chat.space_create`, `초대 코드로 참여` → `chat.space_join` — 두 오버레이는 Task 18 이 등록, 여기서는 `ui.OpenRegisteredOverlay` 로만 부름).

- [ ] **Step 1: Write the failing test**

```go
func TestRailGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), devChannel, func(t *testing.T, h *uitest.Harness) { paneGolden(t, h, layout.SlotRail, "rail") })
}
func TestRailContent(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	p, _ := h.Context().Layout.Pane(layout.SlotRail)
	col := columnText(h.Screen(), p.Rect.Inner()) // helper: rows inside the rect
	mustContainInOrder(t, col, "홈", "노", "+")
}
func TestRailNavigates(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	clickText(t, h, layout.SlotRail, "홈")
	if top(h).Kind != ui.RouteHome { t.Fatalf("got %+v", top(h)) }
	clickText(t, h, layout.SlotRail, "노")
	sid := spaceByName(t, h.Context().Store, "노바 개발방")
	if top(h).ID != h.Context().Store.Spaces[sid].DefaultChannelID { t.Fatal("space click opens default channel") }
	clickText(t, h, layout.SlotRail, "+")
	if len(h.Model.Frame().Overlays) != 1 || !strings.Contains(h.Screen(), "초대 코드로 참여") { t.Fatal("+ opens space add menu") }
}
func TestRailUnreadMarker(t *testing.T) { /* #일반 unread 3 → rail row of 노바 has "•"; set MentionCount 1 → "●"; 민지 DM unread 0 + 준호 DM unread 2 → 홈 "•", then Blocks[junho] + block.updated → 홈 marker blank */ }
func TestRailHostileIntegrity(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), hostileOpen, func(t *testing.T, h *uitest.Harness) { h.Check() })
}
```

공용 시험 헬퍼(이 Task 에서 `helpers_test.go` 에 추가): `paneGolden(t, h, slot layout.SlotID, name string)` = `h.Check()`(셸 전체 무결성은 그대로 검사) 후 `t.Run(name, …)` 안에서 `golden.RequireEqual(t, cropped)` — `cropped` 는 `h.Model.Frame().Layout.Pane(slot)` 의 **바깥 사각형**(테두리 포함)을 `h.Screen()` 에서 칸 단위(`ansi.Cut`)로 잘라낸 줄들, 그 슬롯이 없는 크기면 한 줄 `(no pane)`; 경로 규칙은 `h.Golden` 과 같다(`testdata/<Test>/<WxH>/<name>.golden`). `devChannel(t, sz)` = `#개발` 채널로 연 하네스(시드에 `s.Sync.Bootstrapped = true` 를 더한다 — `fixtures.Seed` 가 설정한다는 보장이 없고, Task 8 의 "볼 수 없는 채널" 안내가 이 값에 기댄다), `hostileOpen(t, sz)` = `fixtures.Hostile` 까지 시드하고 적대적 채널로 연 하네스, `top(h) ui.Route`(활성 탭 스택 맨 위), `columnText`, `clickText(t, h, slot, text)`(그 창 내부에서 글자 위치를 찾아 `h.Click`), `mustContainInOrder`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run TestRail -v`
Expected: FAIL — `undefined: newRail`(또는 골든 없음).

- [ ] **Step 3: Implement `rail.go`**

- `init()`: `ui.RegisterPane(layout.SlotRail, newRail)`.
- 항목: `홈`, `SpaceOrder` 순서의 방(아이콘 이모지 있으면 그것, 없으면 이름 첫 그래핌 — `Sanitize` 후), `+`. 한 항목 = 가운데 정렬 글자 3칸 + 표시 1칸(`●` `S.Error` 멘션 있음 / `•` `S.Bold` 안 읽음 / 공백), 항목 사이 빈 줄 1개; 넘치면 선택이 보이게 스크롤. 홈의 표시는 `visibleDMs(ctx)` 의 안 읽음·멘션 합(차단한 상대의 1:1 DM 은 세지 않는다).
- 현재 방(= `currentSpaceID`, 홈 경로를 보는 중이면 `홈`)은 `S.Selected`. 포커스 시 `j/k` 이동, `enter`/클릭 실행: 홈 → `Navigate(RouteHome)`, 방 → `SetSelectedSpace(id)` + `Navigate(RouteChannel{ID: DefaultChannelID})`, `+` → `ui.OpenOverlay(newSpaceAddMenu())`. 제목 없음(`Titled` 미구현).

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run TestRailGolden -update`
Expected: `ok`. `testdata/TestRailGolden/200x60/rail.golden` 이 레일 창만(시안 ① 왼쪽 열) `홈 / 노 / +` 순서로 담는지, 120×40 이하 크기 골든은 `(no pane)` 인지(표준 이하에 `SlotRail` 없음) 확인. 창만 잘라 찍으므로 뒤 Task 가 Main·멤버 창을 바꿔도 이 골든은 갱신할 필요가 없다.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run TestRail -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/rail.go internal/ui/chat/rail_test.go internal/ui/chat/helpers_test.go internal/ui/chat/testdata/TestRail*
git commit -m "feat(chat): space rail pane"
```

---

### Task 7: 채널·DM 목록 창 (`SlotSidebar`, 지휘소·표준)

**Files:**
- Create: `internal/ui/chat/sidebar.go`
- Test: `internal/ui/chat/sidebar_test.go`

**Interfaces:**
- Consumes: `ui.RegisterPane(layout.SlotSidebar, …)`, `Store.SpaceChannels(spaceID) []*api.Channel`, `visibleDMs`(Task 4 — `Store.DMOrder` 에서 차단한 상대의 1:1 DM 을 뺀 것), `Store.Presence`, `Store.Viewing(id) bool`, `Store.ChannelLabel`, `widgets.Badge(t, ui.Badge)`, `widgets.PresenceDot(t, status)`, `widgets.List[T]`, `ctx.Mode`.
- Produces: `type sidebarPane struct{...}`, `func newSidebar(ctx *ui.Context) ui.Component`; 오버레이 `func newSpaceSwitchMenu(ctx *ui.Context) ui.Overlay`(id `chat.space_switch`: 홈 + 방 목록 + `방 만들기` + `초대 코드로 참여`).

- [ ] **Step 1: Write the failing test**

```go
func TestSidebarGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), devChannel, func(t *testing.T, h *uitest.Harness) { paneGolden(t, h, layout.SlotSidebar, "sidebar") })
}
func TestSidebarHidesBlockedDM(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60}); s := h.Context().Store
	junho := userBy(t, s, "junho")
	mustContain(t, paneText(t, h, layout.SlotSidebar), "준호")
	s.Blocks[junho.ID] = h.Context().Now()
	h.Event("block.updated", realtime.BlockUpdated{UserID: junho.ID, Blocked: true})
	side := paneText(t, h, layout.SlotSidebar)
	if strings.Contains(side, "준호") || !strings.Contains(side, "민지") { t.Fatalf("blocked 1:1 DM must be hidden at once:\n%s", side) }
	delete(s.Blocks, junho.ID)
	h.Event("block.updated", realtime.BlockUpdated{UserID: junho.ID, Blocked: false})
	mustContain(t, paneText(t, h, layout.SlotSidebar), "준호")
}
func TestSidebarCommandVsStandard(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	side := paneText(t, h, layout.SlotSidebar)
	mustContainInOrder(t, side, "노바 개발방", "─ 채널", "# 일반", "# 잡담", "# 개발", "# 배포알림", "─ DM", "● 민지", "○ 준호")
	h.Resize(120, 40)
	side = paneText(t, h, layout.SlotSidebar)
	mustContainInOrder(t, side, "노바 개발방 ▾", "# 일반", "─ DM")
	if strings.Contains(side, "─ 채널") { t.Fatal("standard: dropdown header replaces the section label") }
}
func TestSidebarBadges(t *testing.T) {
	// "# 일반" row ends with badge "3"; set My.MentionCount=2 on #개발 → its row shows the mention badge; muted channel row uses S.Dim (Styled contains dim fg on that row)
	// hostile unread 9999 → "99+"
}
func TestSidebarViewedHighlightAndOpen(t *testing.T) {
	// viewing #개발: its row Styled with S.Selected; focus sidebar (click), j/k + enter on "# 잡담" → top(h) is RouteChannel of 잡담; enter on "● 민지" → DM channel
}
func TestSidebarDropdownOpensSwitcher(t *testing.T) { /* 120x40: clickText "노바 개발방 ▾" → overlay with "홈" and "방 만들기" */ }
func TestSidebarPresenceLive(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	junho := userBy(t, h.Context().Store, "junho")
	h.Event("presence.updated", realtime.PresenceUpdated{UserID: junho.ID, Status: "online"})
	mustContain(t, paneText(t, h, layout.SlotSidebar), "● 준호")
}
func TestSidebarHostileIntegrity(t *testing.T) { forEachSize(t, uitest.AllSizes(), hostileOpen, func(t *testing.T, h *uitest.Harness) { h.Check() }) }
```

`paneText(t, h, slot) string` 헬퍼를 `helpers_test.go` 에 추가.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run TestSidebar -v`
Expected: FAIL — `undefined: newSidebar`.

- [ ] **Step 3: Implement `sidebar.go`**

- 줄 구성: 0열은 포커스 커서 `›`(포커스 아닐 때 공백). 방 이름 줄(지휘소 `S.Title`, 표준 `이름 ▾`), 지휘소만 `─ 채널`, 채널 줄 `# 이름`(비공개 `🔒이름`), `─ DM`, DM 줄(`visibleDMs` 순서 — `block.updated` 를 받은 즉시 차단한 상대의 1:1 DM 이 빠지고, 해제 이벤트에 다시 나타난다) `<접속 점> 이름`(1:1 은 상대 `Store.Presence` → 없으면 `Friends[id].Presence` → `offline`), 그룹 DM `◇ 라벨`. 배지는 오른쪽 정렬(`widgets.Badge`, 멘션 있으면 `Mention:true`), 이름은 남는 폭으로 `Truncate`.
- 스타일: 보고 있는 채널 `S.Selected`, 안 읽음(음소거 아님) `S.Bold`, 음소거·읽기 전용 DM `S.Dim`.
- 방 = `currentSpaceID(ctx)`. 방이 없으면 채널 구역 대신 `S.Dim` 으로 `방이 없어요 · + 로 만들기`.
- 키: `list.up/down/top/bottom`, `list.open` 열기; 클릭 = 선택, 선택 항목 재클릭 = 열기, 드롭다운 줄 = `newSpaceSwitchMenu`.

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run TestSidebarGolden -update`
Expected: `ok`. 사이드바 창만 잘라 찍는다(`paneGolden`). 200×60 골든이 시안 ① 목록 열(`─ 채널` … `○ 준호`), 120×40 이 시안 ②(`노바 개발방 ▾`)와 같은 구성인지, 120×30 이하 골든은 `(no pane)` 인지 확인.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run TestSidebar -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/sidebar.go internal/ui/chat/sidebar_test.go internal/ui/chat/helpers_test.go internal/ui/chat/testdata/TestSidebar*
git commit -m "feat(chat): channel and DM list pane with badges and presence"
```

---

### Task 8: 대화 화면 (`RouteChannel`) — 목록·무한 스크롤·읽음·새 메시지 강조

**Files:**
- Create: `internal/ui/chat/msgpane.go`, `internal/ui/chat/channel.go`
- Test: `internal/ui/chat/channel_test.go`

**Interfaces:**
- Consumes: `ui.RegisterRoute(ui.RouteChannel, ui.RouteFactory)`, `ui.RouteUpdatedMsg`, `ui.EventMsg`, `widgets.NewList[T]`(+`SetItems/Selected/Select/HandleKey/HandleMouse/View/Anchor/Restore/AtBottom/Follow`), `widgets.ScrollAnchor`, `widgets.Fade{Start, Style, Update}`, `widgets.Spinner`, `widgets.Empty`, `(*api.Client).ListMessages`, `(*api.Client).GetMessage`, `(*api.Client).MarkRead`, `chatstore` 전부(Task 1~3), `ui.After`.
- Produces:
  - `type msgPane struct{...}`, `func newMsgPane(channelID, rootID string) *msgPane`; `func (p *msgPane) refresh(ctx *ui.Context)`, `handleKey(ctx, msg tea.KeyPressMsg) (bool, tea.Cmd)`, `handleMouse(ctx, msg tea.MouseMsg, local ui.Point) tea.Cmd`, `view(ctx *ui.Context, size ui.Size, focused bool) string`, `selected() (row, bool)`, `lineAt(ctx, y int) (rowID string, line int, ok bool)`
  - `type channelView struct{ route ui.Route; pane *msgPane; ... }`, `func newChannel(ctx *ui.Context, r ui.Route) ui.Component` — `ui.Component`, `KeyHandler`, `MouseHandler`, `InputCapturer`, `Cursorer`, `ChannelViewer`, `Titled`, `Helper`, `Persistent`
  - 내부 메시지 `type readTickMsg struct{ ch string; gen uint64 }`
  - `type channelSaved struct{ Anchor widgets.ScrollAnchor; Selected string; Draft string }` (Task 9 가 Draft 채움)

- [ ] **Step 1: Write the failing test**

```go
func TestChannelGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), devChannel, func(t *testing.T, h *uitest.Harness) {
		scr := h.Screen()
		switch h.Context().Density() {
		case ui.DensityFull:    mustContain(t, scr, "민지", "11:02", "오늘 배포 몇 시에 해요?", "답글 3개", "$ wrangler deploy --env prod", "국밥")
		case ui.DensityLine:    mustContain(t, scr, "11:02 민지 오늘 배포 몇 시에 해요?", "💬3")
		case ui.DensityBubble:  mustContain(t, scr, "민지 11:02", "11:05 나")
		case ui.DensityMinimal: mustContain(t, scr, "민지")
		}
		h.Golden("channel")
	})
}
func TestChannelHostileIntegrity(t *testing.T) { forEachSize(t, uitest.AllSizes(), hostileOpen, func(t *testing.T, h *uitest.Harness) { h.Check() }) }
func TestChannelInfiniteScrollBack(t *testing.T) {
	srv := newAPI(t); ch := "01JBTESTCHAN0000000000000A"
	// ONE exact-path handler that switches on the query (re-registering the same shape would REPLACE the first, not add a second — apitest rule (a))
	srv.Handle("GET /v1/channels/"+ch+"/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("before") == msgID(51) {
			json.NewEncoder(w).Encode(pageBody(genMsgs(ch, 1, 50), false, true)) // ids msgID(1..50)
			return
		}
		json.NewEncoder(w).Encode(pageBody(genMsgs(ch, 51, 100), true, false)) // latest page: msgID(51..100)
	})
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: ch}, srv, uitest.WithSeed(withTestChannel(ch, 0)))
	sel0 := selectedID(t, h)
	h.Press("home"); h.Press("k")
	if q := lastQuery(srv, "/v1/channels/"+ch+"/messages"); q != "before="+msgID(51)+"&limit=50" { t.Fatalf("query %q", q) }
	if n := chatstore.Get(h.Context().Store).Window(ch).Len(); n != 100 { t.Fatalf("len %d", n) }
	if selectedID(t, h) != msgID(51) { t.Fatal("selection stays on the old first item after prepend") }
	_ = sel0
}
func TestChannelNewMessageWhileScrolledUp(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("k", "k", "k", "ctrl+u")
	before := channelOf(t, h).pane.list.Anchor(); sel := selectedID(t, h)
	h.Event("message.created", realtime.MessageEvent{Message: mkMsg(devID(t, h), 900, userBy(t, h.Context().Store, "minji"), "새 글", h.Context().Now())})
	if channelOf(t, h).pane.list.Anchor() != before || selectedID(t, h) != sel { t.Fatal("must not jump") }
	mustContain(t, h.Screen(), "새 메시지 1개 ↓")
	h.Press("G")
	if strings.Contains(h.Screen(), "새 메시지 1개") || !channelOf(t, h).pane.list.AtBottom() { t.Fatal("G jumps to bottom and clears banner") }
}
func TestChannelFollowsBottom(t *testing.T) { /* at bottom with last selected → new message from minji becomes selected and visible */ }
func TestChannelNewMessageFade(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40}); h.Context().Config.UI.Animations = true
	m := mkMsg(devID(t, h), 901, userBy(t, h.Context().Store, "minji"), "하이", h.Context().Now())
	h.Event("message.created", realtime.MessageEvent{Message: m})
	th := h.Context().Theme; f := &channelOf(t, h).pane.fade
	if !sameColor(f.Style(th, m.ID).GetBackground(), th.P.SelBG) { t.Fatal("starts at selbg") }
	h.Advance(time.Second)
	if !sameColor(f.Style(th, m.ID).GetBackground(), th.P.BG) { t.Fatal("faded after 4x250ms") }
}
func TestChannelMarksRead(t *testing.T) {
	srv := newAPI(t); ch := "01JBTESTCHAN0000000000000A"
	srv.JSON("GET /v1/channels/"+ch+"/messages", 200, pageBody(genMsgs(ch, 1, 5), false, false))
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: ch}, srv, uitest.WithSeed(withTestChannel(ch, 3)))
	if len(reqs(srv, "PUT", "/v1/channels/"+ch+"/read")) != 0 { t.Fatal("debounced 1s") }
	h.Advance(time.Second)
	r := reqs(srv, "PUT", "/v1/channels/"+ch+"/read")
	if len(r) != 1 || jsonField(r[0].Body, "message_id") != msgID(5) { t.Fatalf("%v", r) }
	if h.Context().Store.Channels[ch].My.UnreadCount != 0 { t.Fatal("local badge cleared") }
}
func TestChannelReadOnlyWhileViewing(t *testing.T) {
	// 120x30 (compact, Main only): open test channel, Advance(1s) → 1 PUT; Navigate RouteHome (channel instance cached, Store.Viewing(ch)==false)
	// message.created in ch + Advance(2s) → still 1 PUT (armed by the event, dropped at fire time: Viewing false);
	// navigate back to the channel, h.Press("G") + Advance(1s) → exactly 2 PUTs (re-arms from the nav/focus trigger and G collapse into the latest gen)
}
func TestChannelUnreadDivider(t *testing.T) { /* My.LastReadMessageID = 3rd of 5 → "── 새 메시지 ──" between 3rd and 4th, frozen after MarkRead */ }
func TestChannelAnchorJump(t *testing.T) {
	// Route{Kind: RouteChannel, ID: dev, Anchor: X} → query "around=X&limit=50", selectedID==X
	// then h.Send(ui.RouteUpdatedMsg{Route: ...Anchor: Y}) → second around=Y request, selectedID==Y
}
func TestChannelFetchesMissingInRange(t *testing.T) { /* reaction.added for unknown id inside range → GET /v1/messages/<id> recorded, then message visible */ }
func TestChannelAppliesResync(t *testing.T) { /* h.Send(chatstore.ResyncResultMsg{Gen:1, Channels:{dev: window[only m]}}) → screen shows only m */ }
func TestChannelGoneWhileViewing(t *testing.T) {
	forEachSize(t, uitest.Required, devChannel, func(t *testing.T, h *uitest.Harness) { // devChannel seeds Sync.Bootstrapped=true (the notice depends on it)
		id := devID(t, h); delete(h.Context().Store.Channels, id)
		h.Event("channel.deleted", realtime.ChannelDeleted{ChannelID: id, Reason: "deleted"})
		mustContain(t, h.Screen(), "삭제되었거나 볼 수 없는 채널이에요")
		h.Press("i")
		if channelOf(t, h).CapturesInput() { t.Fatal("composer disabled") }
		h.Check()
	})
}
func TestChannelBlockedFolds(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40}); minji := userBy(t, h.Context().Store, "minji")
	h.Context().Store.Blocks[minji.ID] = h.Context().Now()
	h.Event("block.updated", realtime.BlockUpdated{UserID: minji.ID, Blocked: true})
	scr := h.Screen()
	if strings.Contains(scr, "오늘 배포 몇 시에 해요?") || !strings.Contains(scr, "차단한 사용자의 메시지") { t.Fatal("fold") }
}
```

헬퍼 추가: `devID`, `channelOf(t, h) *channelView`(`h.Instance(route.Key()).(*channelView)`), `selectedID`, `genMsgs`, `pageBody`, `withTestChannel(id string, unread int) func(*store.Store)`(fixtures 시드 + 노바 개발방에 text 채널 `테스트` 하나 추가, `My.UnreadCount=unread`, `s.Sync.Bootstrapped = true`), `jsonField(body []byte, key string) string`, `plainOf(t, h, id) string`(그 메시지의 `markup.Plain(body, 1<<16)`(자르지 않음)의 **앞 2 룬** — `…` 가 붙지 않는 짧은 접두라 어느 밀도에서든 화면 부분 문자열로 찾을 수 있다; 시험 데이터 본문은 한글 음절로 시작), `rowVisible(t, h, id) bool`(`channelOf(t, h).pane.lineAt(ctx, y)` 를 목록 창의 모든 y 에 대해 불러 그 행 ID 가 나오는지 — 부분 문자열이 아니라 목록 기준 가시성), `lastQuery`. 이 Task 의 시험이 덧씌우는 경로는 모두 fixtures 에 없는 채널 ID(`01JBTESTCHAN…`)의 구체 경로다(Global Constraints 시험용 API 서버 규칙).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run TestChannel -v`
Expected: FAIL — `undefined: newChannel`.

- [ ] **Step 3: Implement `msgpane.go`, `channel.go`**

- `init()`: `ui.RegisterRoute(ui.RouteChannel, newChannel)`.
- `Init`: `SetActiveChannel`, 창이 `Loaded` 가 아니거나 `Anchor` 가 있으면 `ui.Do(ctx, "chat.history:"+ch, ListMessages(Limit 50 | Around anchor))` → `ApplyPage` → `refresh`. 그동안 `rowLoading`(스피너 `불러오는 중…`). 안 읽음 구분선 기준 `LastRead` 는 처음 열 때 `Channel.My.LastReadMessageID` 로 **고정**.
- chatstore 접근은 모두 `chatOf(ctx)`(Task 4). 이후 `refresh` 는 `buildRows(Window.Items(), pendingFor(ctx, ch, ""))` → `list.SetItems`(채널 목록에는 `ThreadRootID == nil` 인 대기만 — 스레드 답글 대기가 채널 목록에 끼면 안 된다)(ID 로 선택·앵커 유지). `SetOpen(ch, anchorID, atBottom)` 을 스크롤·선택 변경마다 갱신, 인스턴스가 영구 해제되기 전 `SetClosed` 는 하지 않는다(캐시는 Resync 가 정리).
- 무한 스크롤: 맨 위(`Anchor().ItemID == 첫 메시지 && LineOffset==0`)에서 `list.up`/`list.pageup`/휠 위 또는 `home` 후 `k` → `HasMoreBefore` 면 `ListMessages(Before: 첫 ID, Limit 50)`(진행 중 1개만). 맨 아래에서 `HasMoreAfter` 면 `After: Window.OverlapAnchor(5*time.Second)`.
- 따라가기: 맨 아래 + 마지막 행 선택 상태에서 새 행이 오면 선택을 새 마지막으로, 아니면 그대로 두고 `newBelow++` → 입력창 위 한 줄 `새 메시지 N개 ↓`(`S.Warning`), `list.bottom`(`G`/`end`)·맨 아래 도달 시 0.
- 새 메시지 강조: 남이 보낸 `message.created` 이고 이 창에 들어간 경우 `fade.Start(ctx, id)`.
- 읽음: `View` 는 문자열만, `Resize` 는 아무것도 돌려주지 못하므로 **Cmd 를 낼 수 있는 곳에서만** 판정한다 — `Init` 의 기록 결과(`chat.history:` `ResultMsg`), `EventMsg`(이 채널에 메시지가 들어온 뒤), `ResyncResultMsg`, 스크롤·선택을 바꾼 `HandleKey`/`HandleMouse`, `ui.ModeChangedMsg`, `ui.FocusChangedMsg`, `ui.RouteUpdatedMsg`. 판정은 두 단계다(CR-2 처리 결과 — core 는 `SetViewing` 을 **매 Update 끝**에 갱신하므로, 같은 Update 안에서 받는 `ModeChangedMsg` 등의 시점에 `Store.Viewing` 은 아직 이전 값이다): (1) **무장** `maybeArmRead(ctx)` = 목록이 맨 아래 **그리고** 최신 최상위 메시지 ID > `My.LastReadMessageID` 이면 세대 번호를 올리고 `ui.After(ctx, time.Second, readTickMsg{ch, gen})` — 여기서는 `Store.Viewing` 을 **보지 않는다**; (2) **발사** `readTickMsg` 는 최신 세대이고 그 시점에 `ctx.Store.Viewing(ch)`(앱이 보이는 `ChannelViewer` 로 갱신한 집합) **그리고** 맨 아래 **그리고** 미읽음이 모두 참일 때만 `MarkRead` → `markReadLocal`(아니면 버린다 — 다음 트리거가 다시 판정). 틱은 무장한 Update 가 끝난 뒤 오므로 그 사이 `SetViewing` 이 반드시 한 번 돌아, 모드·탭 전환으로 새로 보이게 된 채널도 추가 입력 없이 읽힌다(Task 23 `TestChatModeSwitchMarksRead`). 가려진 캐시 인스턴스가 `EventMsg` 마다 무장해도 발사 단계에서 버려지므로 요청은 나가지 않는다(`TestChannelReadOnlyWhileViewing`). `View` 호출 여부·시각은 판정에 쓰지 않는다(하네스가 언제 그리는지에 따라 결과가 바뀌면 안 됨).
- `EventMsg` 받을 때마다 `TakeMissing(ch)` → 각 ID `GetMessage`(태그 `chat.fetch:<id>`) → `UpsertMessage`. `chatstore.ResyncResultMsg` 는 `ApplyResync` 후 `refresh`.
- 채널이 `Store.Channels` 에 없고 `Store.Sync.Bootstrapped` 면 `widgets.Empty(…, "삭제되었거나 볼 수 없는 채널이에요", "esc 뒤로")` + 입력 비활성.
- 화면 구성(위→아래): 목록, (배너), (입력 중 줄 — Task 14), 입력창(Task 9). `Titled` = `channelTitle(ctx, ch, Density==Full)` + 배지(안 읽음/멘션), `Helper` = `msg.thread, msg.react, compose.focus, channel.search`, `ViewingChannels` = `[ch]`, `Persistent` = `channelSaved`.
- 마우스: 휠 = 3줄, 클릭 = 선택, 선택 행 재클릭 = 스레드 열기(Task 13 의 `openThread`, 이 Task 에서는 `ui.Navigate(RouteThread{ID, Arg: ch})` 를 직접 낸다).

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run TestChannelGolden -update`
Expected: `ok`. 200×60 = 시안 ① 대화 열(이름·시간 줄, 코드 블록 `▎`, 투표 막대), 120×30 = 시안 ③ 한 줄형, 80×24 = 시안 ④ 말풍선(`11:05 나` 오른쪽), 50×20 = 초소형 한 줄형인지 확인. 이 Task 는 Main 자리표시를 대화 화면으로 바꾸지만, 앞 Task 의 레일·사이드바 골든은 `paneGolden`(창만)이라 갱신하지 않는다 — Step 5 에서 그대로 통과해야 한다.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run 'TestChannel|TestRail|TestSidebar' -v`
Expected: PASS(레일·사이드바 골든이 깨지면 그 창의 결함이다 — `-update` 하지 말고 원인을 고친다)

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/msgpane.go internal/ui/chat/channel.go internal/ui/chat/channel_test.go internal/ui/chat/helpers_test.go internal/ui/chat/testdata/TestChannel*
git commit -m "feat(chat): channel view with history paging, read marking, new-message highlight"
```

---

### Task 9: 입력창 — 전송·낙관적 표시·재전송·한글 IME 커서·입력 중 송신·마지막 메시지 수정

**Files:**
- Create: `internal/ui/chat/composer.go`
- Modify: `internal/ui/chat/channel.go` (입력창 배치·하위 포커스·`Cursor`·`CapturesInput`·`SaveState` Draft)
- Test: `internal/ui/chat/composer_test.go`

**Interfaces:**
- Consumes: `widgets.NewComposer(placeholder) *widgets.Composer`(+`Value/SetValue/Reset/Focus/Blur/Focused/Update/View(ctx, width, maxRows)/Height/Cursor`), `(*api.Client).SendMessage(ctx, ch, api.SendMessageRequest{ClientNonce, Kind, Body, Poll}) (*api.Message, bool, error)`, `(*api.Client).ReplyThread`, `(*api.Client).EditMessage`, `ids.New()`, `ctx.RT.Typing(channelID, threadRootID)`, `chatstore.AddPending/FailPending/RetryPending/DropPending/SetPendingPoll/PendingPoll/UpsertMessage`(모두 `chatOf(ctx)` 경유), `pendingFor`, `api.Describe`, `api.IsCode`.
- Produces:
  - `type chatComposer struct{ c *widgets.Composer; channelID string; rootID *string; editing *api.Message; savedDraft string; ... }`
  - `func newChatComposer(channelID string, rootID *string, placeholder string) *chatComposer`
  - `func (cc *chatComposer) handleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd)`, `update(ctx, msg tea.Msg) tea.Cmd`, `view(ctx, width int) string`, `height(width int) int`, `cursor() *tea.Cursor`, `startEdit(m api.Message)`, `submit(ctx) tea.Cmd`
  - `func sendCmd(ctx *ui.Context, channelID string, rootID *string, p chatstore.PendingMessage, poll *api.PollInput) tea.Cmd` — 태그 `chat.send:<nonce>`
  - `func resendCmd(ctx *ui.Context, channelID, nonce string) tea.Cmd` — 대기 항목(`Pending`)에서 nonce·본문·kind·`ThreadRootID` 를, `chatOf(ctx).PendingPoll(channelID, nonce)` 에서 투표 입력을 다시 꺼내 같은 요청을 만든다(투표 입력이 필요한데 없으면 요청하지 않고 `FailPending` 유지)

- [ ] **Step 1: Write the failing test**

```go
func TestComposerGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), devChannel, func(t *testing.T, h *uitest.Harness) {
		h.Press("i"); h.Type("배포 몇 시")
		h.Golden("composer")
		row, col := lineWith(t, h.Screen(), "배포 몇 시")
		if c := h.Cursor(); c == nil || c.Y != row || c.X != col+textwidth.Width("배포 몇 시") { t.Fatalf("IME cursor %+v", c) }
	})
}
func TestComposerSendNoDuplicate(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("i"); h.Type("안녕하세요"); h.Press("enter")
	r := reqs(h.API, "POST", "/v1/channels/"+devID(t, h)+"/messages")
	mustJSONField(t, r[0].Body, "client_nonce", func(v string) bool { return ids.Valid(v) })
	mustJSONEq(t, r[0].Body, "kind", "text"); mustJSONEq(t, r[0].Body, "body", "안녕하세요")
	if channelOf(t, h).composer.c.Value() != "" { t.Fatal("cleared") }
	if strings.Count(h.Screen(), "안녕하세요") != 1 { t.Fatal("pending merged with server echo by nonce") } // echo of the posted client_nonce comes from chatRoutes (Task 4), not fixtures
}
func TestComposerSendFailureRetrySameNonce(t *testing.T) {
	srv := newAPI(t); ch := chanID(t, seeded(), "개발")
	fail := true
	// exact path: more specific than chatRoutes' {id} pattern, so it wins without a conflict
	srv.Handle("POST /v1/channels/"+ch+"/messages", func(w http.ResponseWriter, r *http.Request) { if fail { w.WriteHeader(503); io.WriteString(w, `{"error":{"code":"service_unavailable","message":"x","details":{}}}`); return }; /* echo 201 with posted nonce */ })
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: ch}, srv)
	h.Press("i"); h.Type("실패할 글"); h.Press("enter")
	mustContain(t, h.Screen(), "실패할 글", "전송 실패")
	fail = false
	h.Press("esc", "G", "enter") // failed pending row is last & selected; enter = resend
	r := reqs(srv, "POST", "/v1/channels/"+ch+"/messages")
	if len(r) != 2 || jsonField(r[0].Body, "client_nonce") != jsonField(r[1].Body, "client_nonce") { t.Fatal("same nonce on retry") }
	if strings.Contains(h.Screen(), "전송 실패") { t.Fatal("cleared after success") }
}
func TestComposerDiscardFailed(t *testing.T) { /* failed pending selected + "d" → row gone, no request */ }
func TestComposerRateLimited(t *testing.T) { /* 429 retry_after_sec 30 → pending failed, Screen contains api.Describe(err) text; no automatic second POST */ }
func TestComposerReadOnlyDM(t *testing.T) {
	// opened DIRECTLY by route (restored tab / search hit) — the sidebar and home hide a blocked peer's 1:1 DM (TestSidebarHidesBlockedDM), but the route itself must still render
	// DM with junho, set Channels[dm].ReadOnly=true → "읽기 전용 대화예요"; ReadOnlyReason "blocked" → "차단한 사용자와의 대화예요"; "i" does not capture
}
func TestComposerEditLast(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40}); h.Press("i", "up")
	if v := channelOf(t, h).composer.c.Value(); v != "@minji 점심 먹고 ㄱㄱ" { t.Fatalf("edit-last loads my last body, got %q", v) }
	mustContain(t, h.Screen(), "수정 중")
	h.Type(" 2시"); h.Press("enter")
	if r := reqs(h.API, "PATCH", "/v1/messages/"+myLastID(t, h)); len(r) != 1 { t.Fatal("PATCH sent") }
	h.Press("up", "esc") // cancel edit restores the previous (empty) draft
}
func TestComposerTypingEmits(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40}); rt := &recordRT{}; h.Context().RT = rt
	h.Press("i"); h.Type("가")
	if len(rt.Typed) == 0 || rt.Typed[0] != devID(t, h)+"|" { t.Fatalf("%v", rt.Typed) }
}
func TestComposerMultilineGrows(t *testing.T) { /* type "a", shift+enter, "b" → value "a\nb", composer height 2; ctrl+j also newline; enter sends */ }
func TestComposerSanitizesInput(t *testing.T) {
	for _, in := range []string{"❄️ 첫눈", "👍🏽", "👨‍👩‍👧 가족", "🇰", "क्षि", "\x1b[31m빨강"} {
		// h.Press("i"); h.Paste(in) → Value()==textwidth.SanitizeInput(in); h.Check()
	}
}
func TestComposerDraftSurvivesModeSwitch(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60}); h.Press("i"); h.Type("배포 몇 시")
	inst := channelOf(t, h)
	for _, sz := range []uitest.Size{{120, 40}, {120, 30}, {80, 24}, {50, 20}, {200, 60}} {
		h.Resize(sz.W, sz.H)
		if channelOf(t, h) != inst || inst.composer.c.Value() != "배포 몇 시" { t.Fatalf("%v draft lost", sz) }
		row, col := lineWith(t, h.Screen(), "배포 몇 시")
		if c := h.Cursor(); c == nil || c.Y != row || c.X != col+textwidth.Width("배포 몇 시") { t.Fatalf("%v cursor %+v", sz, c) }
		h.Check()
	}
}
```

`mustJSONField`, `mustJSONEq`, `myLastID` 는 `helpers_test.go` 헬퍼.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run TestComposer -v`
Expected: FAIL — `undefined: newChatComposer` / `channelView.composer` 필드 없음.

- [ ] **Step 3: Implement `composer.go` and wire it into `channel.go`**

- 자리표시 문구: Full `› <channelTitle> 에 메시지 보내기`(DM `› 민지에게 메시지 보내기`), Line `› <channelTitle>`, Bubble/Minimal `›`. 최대 줄 수: Full 6, Line 3, Bubble 4, Minimal 1.
- 하위 포커스: 목록 ↔ 입력창. `compose.focus`(`i`/`a`) 진입, `compose.leave`(`esc`) 이탈(편집 중이면 편집 취소 → 이전 초안 복원, 자동완성 열려 있으면 그것 먼저 닫기 — Task 10). 입력창 포커스 동안 `CapturesInput()=true`.
- `compose.send`(`enter`): 값이 공백뿐이면 무시. 편집 중이면 `EditMessage(id, value)` → `UpsertMessage`(빈 값이면 Task 12 의 삭제 확인을 연다). 아니면 Task 11 의 `slash.Parse` 분기(이 Task 에서는 `ok=false` 경로만) → `PendingMessage{Nonce: ids.New(), Body, Kind:"text", ThreadRootID, SentAt: ctx.Now()}` → `AddPending` + `Reset` + 맨 아래로 + `sendCmd`.
- `sendCmd` 결과: 성공 → `UpsertMessage(msg)`(nonce 로 대기 제거); 실패 → `FailPending(ch, nonce, err)`(문구 `api.Describe(err)` 를 실패 줄에 표시). api 클라이언트의 `SendMessage` 가 429(`RetryAfter ≤ 10s`)를 1회 자동 재시도하므로 chat 은 재시도 루프를 만들지 않는다.
- `sendCmd` 는 `poll != nil` 이면 보내기 전에 `SetPendingPoll(ch, nonce, poll)` 로 투표 입력을 보관한다(`PendingMessage` 에 `Poll` 칸이 없음 — Task 1).
- 실패 대기 줄 선택 중: `enter` = `RetryPending` + `resendCmd`(같은 nonce, 같은 본문·kind, `/poll` 이면 보관한 같은 투표 입력), `d` = `DropPending`(보관한 투표 입력도 같이 지워진다).
- `compose.edit_last`(`↑`, 값이 비고 자동완성 없음): 이 채널(스레드면 그 스레드) 창에서 내 마지막 `text`/`me` 메시지 → `startEdit`(`SetValue(body)` — 정규화됨, 테두리 위 `수정 중 · esc 취소`).
- 입력 중 송신: 키·붙여넣기 뒤 값이 비어 있지 않으면 매번 `ctx.RT.Typing(ch, rootID)`(스로틀은 realtime 이 함).
- 읽기 전용: `Channel.ReadOnly` → 입력창 대신 한 줄 `읽기 전용 대화예요`(사유 `blocked` 면 `차단한 사용자와의 대화예요`), `S.Dim`, 포커스 불가.
- `Cursor()`: 입력창 포커스일 때 `composer.cursor()` + 입력창의 창 안 Y 오프셋, 아니면 nil.

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run TestComposerGolden -update`
Expected: `ok`. 입력창 테두리가 포커스 색(acc)으로 그려지고 `배포 몇 시` 바로 뒤가 커서 자리인지, 50×20 에서 한 줄 입력이 잘리지 않고 들어가는지 확인.

**주변 변경에 따른 갱신(이 Task 가 입력창을 대화 화면에 넣었으므로)**: `go test ./internal/ui/chat/ -run 'TestChannelGolden' -update` 후 `git diff --stat internal/ui/chat/testdata` 와 120×40·200×60 diff 를 열어 **입력창(과 그로 인해 줄어든 목록 높이)만** 바뀌었는지 확인. 레일·사이드바 골든은 창만 찍으므로 대상이 아니다. 그 밖의 셸 전체 골든은 아직 없다(Task 4 오버레이 골든은 Task 15 에서).

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run 'TestComposer|TestChannel' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/composer.go internal/ui/chat/channel.go internal/ui/chat/composer_test.go internal/ui/chat/helpers_test.go internal/ui/chat/testdata/
git commit -m "feat(chat): composer with optimistic send, retry, IME cursor, edit-last"
```

---

### Task 10: 자동완성 — `@멘션`·`:이모지`·`/명령`

**Files:**
- Create: `internal/ui/chat/complete.go`
- Modify: `internal/ui/chat/composer.go` (팝업 키 처리), `internal/ui/chat/channel.go` (`Completing()`)
- Test: `internal/ui/chat/complete_test.go`

**Interfaces:**
- Consumes: `(*widgets.Composer).SetCompletion([]widgets.Completion{Label, Insert, Detail})`, `(*api.Client).ListChannelMembers(ctx, ch, api.PageOpts{Limit: 100}) (*api.Page[api.ChannelMember], error)`, `Channel.Members`, `slash.Catalog() []slash.Spec{Name, Usage, Desc}`, `palette.Chosung(s)`, `Store.IsBlocked/IsMe`.
- Produces:
  - `type completer struct{ kind byte; token string; items []widgets.Completion; idx int; members []api.UserSummary; membersLoaded bool }`
  - `func (cc *chatComposer) recomplete(ctx *ui.Context) tea.Cmd` — 값 끝 토큰으로 팝업 갱신(구성원 미로드면 `chat.members:<ch>` 로 불러오기)
  - `func mentionCandidates(ctx *ui.Context, ch *api.Channel, members []api.UserSummary, prefix string) []widgets.Completion`
  - `func emojiCandidates(prefix string) []widgets.Completion`, `var emojiTable []emojiEntry`(`type emojiEntry struct{ Name, Emoji, Ko string }`)
  - `func commandCandidates(prefix string) []widgets.Completion`
  - `func (cc *chatComposer) completing() bool`, `func (v *channelView) Completing() bool` — CR-1 의 `ui.Completer` 모양(Task 13 의 `threadView` 도 같은 메서드)

- [ ] **Step 1: Write the failing test**

```go
func TestMentionCompletion(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40}); h.Press("i"); h.Type("@mi")
	mustContain(t, h.Screen(), "› 민지", "@minji") // chat prefixes the selected Label with "› "
	if !channelOf(t, h).Completing() { t.Fatal("Completing() reports the open popup (CR-1)") }
	h.Press("enter") // accept with enter: tab only reaches the composer once CR-1 lands (see TestCompletionTabAccept)
	if v := channelOf(t, h).composer.c.Value(); v != "@minji " || channelOf(t, h).Completing() { t.Fatalf("got %q", v) }
}
func TestMentionByDisplayNameAndChosung(t *testing.T) { /* "@민" and "@ㅁㅈ" both offer @minji */ }
func TestMentionExcludesBlocked(t *testing.T) { /* Blocks[junho] → "@ju" offers nothing for junho */ }
func TestHereCompletion(t *testing.T) { /* text channel "@he" offers "@here"; 1:1 DM does not */ }
func TestEmojiCompletion(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40}); h.Press("i"); h.Type(":thu"); h.Press("enter")
	if v := channelOf(t, h).composer.c.Value(); v != "👍 " { t.Fatalf("enter accepts while popup open, got %q", v) }
	if len(reqs(h.API, "POST", "/v1/channels/"+devID(t, h)+"/messages")) != 0 { t.Fatal("must not send") }
}
func TestCommandCompletion(t *testing.T) { /* "/" lists "/me" "/shrug" "/poll" "/status" "/focus" "/roll" "/invite"; "/ro"+enter → "/roll " (no request) */ }
func TestCompletionNavigateAndEsc(t *testing.T) { /* "@" → first item "› …", others "  …"; down moves the "›" marker to the 2nd item; esc closes popup (Completing false) but keeps focus (CapturesInput true); second esc leaves */ }
func TestCompletionGolden(t *testing.T) {
	forEachSize(t, uitest.Required, devChannel, func(t *testing.T, h *uitest.Harness) { h.Press("i"); h.Type("@"); h.Golden("mention_popup") })
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run 'TestMention|TestHere|TestEmoji|TestCommandCompletion|TestCompletion' -v`
Expected: FAIL — `undefined: mentionCandidates`.

- [ ] **Step 3: Implement `complete.go`**

- 토큰 = 값의 마지막 공백 뒤 글자(값 끝에서만 자동완성 — `SetValue` 가 커서를 끝으로 옮기므로 끝 토큰만 다룬다). `@`(0자 이상), `:`(2자 이상), `/`(값 전체가 `/…` 이고 공백 없음).
- 멘션 후보: 1:1/그룹 DM = `Channel.Members`, text = `ListChannelMembers`(한 번만, 결과 캐시). 나·차단 제외. 일치: handle 접두, 표시 이름 부분, `palette.Chosung(표시 이름)` 접두(대소문자 무시). 최대 8개, 정렬 = handle 접두 일치 먼저 → 표시 이름. `Label`=`표시 이름`, `Detail`=`@handle`, `Insert`=`@handle `. text 채널·그룹 DM 에는 `@here`(`Detail` `지금 접속한 사람 모두`)를 접두 일치 시 추가.
- `emojiTable` 40개 이상(예: `thumbsup 👍 좋아요`, `eyes 👀 눈`, `heart ❤ 하트`, `tada 🎉 축하`, `fire 🔥 불`, `pray 🙏 부탁`, `joy 😂 웃음`, `rocket 🚀 로켓`, `rice 🍚 밥`, `coffee ☕ 커피`, `check ✅ 확인` …), 영어 이름 접두 또는 한글 이름 포함. `Insert` = 이모지 + 공백.
- 명령 후보: `slash.Catalog()` 접두 일치, `Label`=`/name`, `Detail`=`Desc`, `Insert`=`/name `.
- 팝업 상태의 주인은 **chat**(`completer.idx`)이다. `widgets.Composer.SetCompletion` 은 목록만 받고 선택 인덱스를 받지 않으므로, chat 이 선택 표시를 직접 만든다: `SetCompletion` 에 넘기는 항목의 `Label` 앞에 선택 항목은 `› `, 나머지는 `  `(2칸)를 붙이고, 선택이 바뀔 때마다 다시 `SetCompletion` 한다. Composer 가 행 스타일을 따로 입히더라도 선택의 기준은 이 접두다.
- 팝업 키(입력창 포커스 + 팝업 있음): `chatComposer.handleKey` 가 `widgets.Composer` 보다 **먼저** 받아 소비한다 — `↑/↓` 선택, `enter`·`tab` 수락(토큰 교체 = `SetValue(앞부분 + Insert)`, 그 뒤 `SetCompletion(nil)`), `esc` 닫기. (core 의 Composer 가 팝업 키를 자체 처리하더라도 chat 이 먼저 소비하므로 거기까지 가지 않는다.)
- `tab` 이 컴포넌트까지 오려면 앱이 "자동완성 팝업이 떠 있음" 을 알아야 한다(CA 5.3.4 3단계) — 그 인터페이스가 CA 에 없으므로 **CR-1**(위 "CA 변경 요청")을 낸다. chat 은 `channelView`·`threadView` 에 `func Completing() bool`(= 입력창 포커스 && 팝업 항목 ≥ 1)을 이 Task 에서 구현해 둔다. plan 03 현행에는 CR-1 이 없어(위 "CA 변경 요청" 처리 결과) 앱이 `tab` 을 `focus.next`/`tab.next` 로 먹으므로 이 Task 의 시험은 `enter` 로 수락한다. 아래 `TestCompletionTabAccept` 는 이 Task 에서 추가하지 않는다 — 추가 여부는 **Task 23 Step 5** 가 core 의 `Completer` 유무로 판정하고, 없으면 그 자리에서 `TestCompletionTabFallback`(Task 23 에 적음)을 대신 넣는다(실패할 수밖에 없는 시험을 두지 않으면서 영구 보류도 막기 위함). CR-1 반영 시 추가할 시험:

```go
func TestCompletionTabAccept(t *testing.T) { // requires CR-1 (ui.Completer) in core
	for _, sz := range []uitest.Size{{120, 40}, {80, 24}} { // multi-pane ring and single-pane (tab.next) modes
		h := devChannel(t, sz); h.Press("i"); h.Type("@mi"); h.Press("tab")
		if v := channelOf(t, h).composer.c.Value(); v != "@minji " { t.Fatalf("%v: got %q", sz, v) }
		h.Press("tab") // popup closed now → hard global again (focus.next / tab.next), composer value unchanged
		if channelOf(t, h).composer.c.Value() != "@minji " { t.Fatal("tab without popup must not touch the draft") }
	}
}
```

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run TestCompletionGolden -update`
Expected: `ok`. 팝업이 입력창 바로 위에 뜨고 테두리와 겹치지 않는지, 첫 항목에 `› ` 표시가 있는지 4크기 모두 확인.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run 'TestMention|TestHere|TestEmoji|TestCommandCompletion|TestCompletion|TestComposer' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/complete.go internal/ui/chat/composer.go internal/ui/chat/channel.go internal/ui/chat/complete_test.go internal/ui/chat/testdata/TestCompletionGolden
git commit -m "feat(chat): mention, emoji and slash-command autocomplete"
```

---

### Task 11: 슬래시 명령 실행 — `/me /shrug /poll /status /focus /roll /invite`

**Files:**
- Create: `internal/ui/chat/slashexec.go`, `internal/ui/chat/invitecreated.go`
- Modify: `internal/ui/chat/composer.go` (`submit` 에서 `slash.Parse` 분기)
- Test: `internal/ui/chat/slash_test.go`

**Interfaces:**
- Consumes: `slash.Parse(input string) (*slash.Command, bool, error)`, `slash.Command{Name, Text, Emoji, Poll, Dice, FocusOff, FocusDuration, InviteUser, InviteLink, LinkExpires, LinkMaxUses}`, `ui.RunCommand(id, args string) tea.Cmd`, `(*api.Client).SendDirectInvite(ctx, spaceID, api.UserRef)`, `(*api.Client).CreateInvite(ctx, spaceID, api.CreateInviteRequest{MaxUses, ExpiresInSec api.Opt[int]})`, `api.Handle(h)`, `api.Some`, `ui.Toast`, `ui.RegisterOverlay`.
- Produces:
  - `func runSlash(ctx *ui.Context, cc *chatComposer, raw string, cmd *slash.Command) tea.Cmd`
  - `func rawArgs(input string) string` — 첫 공백 뒤 원문(trim)
  - `func createInviteCmd(ctx *ui.Context, spaceID string, r api.CreateInviteRequest) tea.Cmd`(태그 `chat.invite_create:<space>`), `func directInviteCmd(ctx *ui.Context, spaceID string, u api.UserRef) tea.Cmd`(태그 `chat.invite_direct:<space>`)
  - `func newInviteCreated(inv api.Invite) ui.Overlay` + 등록 `chat.invite_created`(args = 초대 코드; 코드만 받으면 `masil join <code>` 만 표시)

- [ ] **Step 1: Write the failing test**

```go
func TestSlashSendsKinds(t *testing.T) {
	cases := []struct{ in, kind, body string }{
		{"/me 커피 마시는 중", "me", "커피 마시는 중"},
		{"/shrug 몰라요", "text", `몰라요 ¯\_(ツ)_/¯`},
		{"/shrug", "text", `¯\_(ツ)_/¯`},
		{"/roll 2d6", "roll", "2d6"},
		{"/없는명령 그대로", "text", "/없는명령 그대로"},
	}
	for _, c := range cases { /* fresh devChannel; i; Type(c.in); enter; last POST body kind/body equal */ }
}
func TestSlashPoll(t *testing.T) {
	// `/poll "점심 메뉴" "국밥" "돈까스" --multi` → POST kind "poll", poll.question "점심 메뉴", options ["국밥","돈까스"], multi true
	// same input inside a thread composer → no request, toast "스레드에서는 투표를 만들 수 없어요"
}
func TestSlashPollRetryKeepsPoll(t *testing.T) {
	// newAPI + ONE exact-path handler for POST /v1/channels/<개발>/messages: 503 while fail==true, else echo 201 (posted nonce/kind/poll)
	// `/poll "점심 메뉴" "국밥" "돈까스"` + enter → failed pending "전송 실패"; fail=false; esc, G, enter (resend)
	// → 2 POSTs, same client_nonce, both kind "poll" and JSON-equal "poll" objects ({"question":"점심 메뉴","options":["국밥","돈까스"]}); "전송 실패" gone
	// discard path: another failing /poll, "d" → row gone and chatOf(ctx).PendingPoll(ch, nonce) == nil
}
func TestSlashStatusFocusRunCommands(t *testing.T) {
	got := recordCommands(t, "status.set", "focus.start", "focus.stop") // registers palette commands once (sync.Once) that append "id|args"
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	for _, in := range []string{"/status 🎧 듣는 중", "/focus 25m", "/focus", "/focus off"} { h.Press("i"); h.Type(in); h.Press("enter") }
	want := []string{"status.set|🎧 듣는 중", "focus.start|25m", "focus.start|", "focus.stop|"}
	if !slices.Equal(*got, want) { t.Fatalf("%v", *got) }
}
func TestSlashUsageError(t *testing.T) { /* "/roll" (parse err) → no request, draft kept, screen shows the err text */ }
func TestSlashInviteUser(t *testing.T) { /* "/invite @hana" → POST /v1/spaces/<노바>/direct-invites body {"user":"@hana"}; toast contains "초대했어요"; in a DM channel → toast "방 채널에서만 초대할 수 있어요", no request */ }
func TestSlashInviteLink(t *testing.T) {
	// "/invite link 7d 10" → POST /v1/spaces/<노바>/invites body {"max_uses":10,"expires_in_sec":604800}; overlay shows code and "masil join <code>"; "y" → TeaMsgs has setClipboardMsg "masil join <code>"
	// "/invite link" → body {} (server default 7d)
}
func TestInviteCreatedGolden(t *testing.T) {
	forEachSize(t, overlaySizes, devChannel, func(t *testing.T, h *uitest.Harness) { h.Send(ui.OpenOverlayMsg{ID: "chat.invite_created", Args: "AB12CD34"}); h.Golden("invite_created") })
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run 'TestSlash|TestInviteCreated' -v`
Expected: FAIL — `undefined: runSlash`.

- [ ] **Step 3: Implement `slashexec.go`, `invitecreated.go`**

- `submit`: `cmd, ok, err := slash.Parse(value)`; `err` → `ui.Toast(err.Error(), ui.ToastError)` + 초안 유지; `!ok` → 일반 전송; `ok` → `runSlash` 후 성공 경로만 `Reset`.
- `me` → kind `me`, body `Text`. `shrug` → body `strings.TrimSpace(Text + " ¯\\_(ツ)_/¯")`. `roll` → kind `roll`, body `Dice`. `poll` → kind `poll`, `Poll` = `cmd.Poll`(스레드면 거부 토스트). 모두 Task 9 의 대기/`sendCmd` 경로(낙관적 줄은 `Plain` 문구로). `poll` 은 `sendCmd(…, poll)` 이 `SetPendingPoll` 로 투표 입력을 보관하므로 실패 후 `enter` 재전송(`resendCmd`)도 같은 nonce·같은 `poll` 객체를 보낸다.
- `status` → `ui.RunCommand("status.set", rawArgs(value))`. `focus` → `FocusOff` 면 `("focus.stop", "")`, 아니면 `("focus.start", rawArgs(value))`.
- `invite` → 방 = 이 채널의 `SpaceID`(nil 이면 거부 토스트). `InviteUser` → `directInviteCmd(Handle(user))` 성공 토스트 `<user>님을 <방 이름>에 초대했어요`. `InviteLink` → `CreateInviteRequest{MaxUses: LinkMaxUses>0 ? Some : 생략, ExpiresInSec: LinkExpires>0 ? Some(secs) : 생략}` → 결과 `api.Invite` 로 `newInviteCreated`(OpenOverlay).
- 초대 결과 오버레이: 코드(크게, `S.Title`), `masil join <code>`, 만료·횟수(`humanize.MessageTime`/`무제한`), `y` = `tea.SetClipboard("masil join <code>")` + `ui.Toast("복사했어요", ui.ToastSuccess)`, `esc` 닫기.

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run TestInviteCreatedGolden -update`
Expected: `ok`. 코드·공유 문구가 20×6 강등 화면에서도 잘리지 않거나 `…` 로만 잘리는지 확인.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run 'TestSlash|TestInviteCreated|TestComposer' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/slashexec.go internal/ui/chat/invitecreated.go internal/ui/chat/composer.go internal/ui/chat/slash_test.go internal/ui/chat/testdata/TestInviteCreatedGolden
git commit -m "feat(chat): slash commands /me /shrug /poll /status /focus /roll /invite"
```

---

### Task 12: 메시지 동작 — 리액션·수정·삭제·고정·복사·동작 메뉴·리액션 누른 사람·신고·차단·투표

**Files:**
- Create: `internal/ui/chat/actions.go`, `internal/ui/chat/reactionusers.go`
- Modify: `internal/ui/chat/msgpane.go` (키·마우스 연결)
- Test: `internal/ui/chat/actions_test.go`

**Interfaces:**
- Consumes: `widgets.EmojiPicker(onPick func(emoji string) tea.Cmd) ui.Overlay`, `widgets.Confirm(title, body, yes, no string, onYes tea.Cmd) ui.Overlay`, `widgets.ReportDialog(targetType, targetID, label string) ui.Overlay`, `(*api.Client).AddReaction/RemoveReaction/ListReactionUsers/Pin/Unpin/DeleteMessage/Block/Vote/RetractVote/ClosePoll`, `tea.SetClipboard`, `chatstore.SetMyReaction/SetPoll/FindMessage`, `ui.RegisterOverlay("chat.reaction_users", …)`.
- Produces:
  - `func toggleReactionCmd(ctx *ui.Context, m api.Message, emoji string) tea.Cmd` — 낙관적 `SetMyReaction` 후 PUT/DELETE, 실패 시 되돌리고 `ui.ErrorMsg`(태그 `chat.react:<id>`)
  - `func deleteMessageCmd(ctx *ui.Context, m api.Message) tea.Cmd`, `func pinCmd(ctx *ui.Context, m api.Message, on bool) tea.Cmd`, `func voteCmd(ctx *ui.Context, p api.Poll, idxs []int) tea.Cmd`
  - `func messageActions(ctx *ui.Context, ch *api.Channel, m api.Message) []menuItem` + 오버레이 `chat.message_actions`(args = `"<channelID> <messageID>"`)
  - `func newReactionUsers(ctx *ui.Context, args string) ui.Overlay` (`chat.reaction_users`, args = `"<messageID> <emoji>"`, `ListReactionUsers` 커서 페이지)
  - `func chipAt(line string, rs []api.ReactionSummary, x int) (emoji string, ok bool)`, `func pollOptionAt(...)`

- [ ] **Step 1: Write the failing test**

```go
func TestReactKeyOpensPicker(t *testing.T) { /* select minji msg, "r" → Frame().Overlays len 1 */ }
func TestToggleReaction(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40}); m := fixtureMsg(t, h, "오늘 배포 몇 시에 해요?")
	h.Send(runCmdMsg(toggleReactionCmd(h.Context(), m, "😀")))
	if r := reqs(h.API, "PUT", "/v1/messages/"+m.ID+"/reactions/"+url.PathEscape("😀")); len(r) != 1 { t.Fatal("PUT") }
	got, _ := chatstore.Get(h.Context().Store).FindMessage(m.ID); if !hasMine(got, "😀") { t.Fatal("optimistic me") }
}
func TestReactionChipClickToggles(t *testing.T) { /* click on "👍" chip of minji msg (me:true in fixture) → DELETE .../reactions/%F0%9F%91%8D; right-click chip → overlay chat.reaction_users */ }
func TestReactionRollbackOnError(t *testing.T) { /* 422 limit_exceeded → chip state restored, toast */ }
func TestEditKey(t *testing.T) { /* select my msg, "e" → composer value = body, "수정 중"; on someone else's msg "e" → toast "내 메시지만 수정할 수 있어요" */ }
func TestDeleteConfirm(t *testing.T) {
	// "d" on my msg → confirm overlay containing "이 메시지를 삭제할까요?"; run deleteMessageCmd → DELETE /v1/messages/<id>
	// member on other's msg in text channel → toast "삭제할 권한이 없어요", no overlay; owner → overlay opens
}
func TestPinPermission(t *testing.T) {
	// Spaces[노바].MyRole="member" → "p" toast "고정할 권한이 없어요"; "owner" → PUT /v1/messages/<id>/pin and header shows "📌" after message.pinned event
	// DM channel: member may pin
}
func TestCopyKey(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40}); selectText(t, h, "오늘 배포 몇 시에 해요?"); h.Press("y")
	if !hasClipboard(h.TeaMsgs(), "오늘 배포 몇 시에 해요?") { t.Fatal("tea.SetClipboard") }
}
func TestActionsMenu(t *testing.T) {
	// "m" on minji msg as member → items "스레드로 답글","리액션","리액션 누른 사람","복사","신고","작성자 차단"; no "수정"; "고정" disabled
	// choose "신고" → ReportDialog overlay (depth 1); choose "작성자 차단" → Confirm, run → PUT /v1/blocks/@minji
}
func TestActionsMenuGolden(t *testing.T) {
	forEachSize(t, overlaySizes, devChannel, func(t *testing.T, h *uitest.Harness) { selectText(t, h, "오늘 배포"); h.Press("m"); h.Golden("message_actions") })
}
func TestReactionUsersGolden(t *testing.T) {
	forEachSize(t, overlaySizes, devChannel, func(t *testing.T, h *uitest.Harness) {
		m := fixtureMsg(t, h, "오늘 배포 몇 시에 해요?")
		h.API.JSON("GET /v1/messages/"+m.ID+"/reactions/"+url.PathEscape("👍"), 200, map[string]any{"items": []api.UserSummary{userBy(t, h.Context().Store, "junho")}, "next_cursor": nil})
		h.Send(ui.OpenOverlayMsg{ID: "chat.reaction_users", Args: m.ID + " 👍"}); h.Golden("reaction_users")
	})
}
func TestPollVoteViaMenuAndClick(t *testing.T) { /* poll msg "m" → "투표: 국밥" item → PUT /v1/polls/<id>/vote {"option_idxs":[0]} → SetPoll authoritative; click on "돈까스" bar → [1] */ }
```

덧씌우는 `GET /v1/messages/<id>/reactions/<emoji>`(TestReactionUsersGolden)는 fixtures 메시지 ID 를 박은 구체 경로이고 RegisterAPI·`chatRoutes` 어느 쪽도 GET 리액션 사용자 경로를 등록하지 않으므로 충돌하지 않는다(Global Constraints 시험용 API 서버 규칙). `fixtureMsg`, `selectText`, `hasMine`, `hasClipboard`(`fmt.Sprintf("%T", m)=="tea.setClipboardMsg"` 이고 `reflect.ValueOf(m).String()` 비교), `runCmdMsg` 헬퍼 추가.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run 'TestReact|TestToggleReaction|TestEditKey|TestDelete|TestPin|TestCopy|TestActions|TestReactionUsers|TestPollVote' -v`
Expected: FAIL — `undefined: toggleReactionCmd`.

- [ ] **Step 3: Implement `actions.go`, `reactionusers.go`**

- 메시지 선택 범위 키(입력창 포커스가 아닐 때, `rowMessage` 선택): `msg.react` → `EmojiPicker(func(e) { return toggleReactionCmd(…) })`, `msg.edit` → 내 `text|me` 면 입력창 `startEdit`, `msg.delete` → `canDelete` 면 `Confirm("메시지 삭제", "이 메시지를 삭제할까요?", "삭제", "취소", deleteMessageCmd)`, `msg.pin` → `canPin` 이고 최상위 메시지면 `pinCmd(!m.Pinned)`, `msg.copy` → `tea.SetClipboard(m.Body)` + 토스트 `복사했어요`, `msg.actions` → 메뉴. 삭제됨·시스템 메시지에는 react/edit/pin/copy 없음.
- 동작 메뉴 순서: `스레드로 답글 t`, `리액션 r`, `리액션 누른 사람`(리액션 있을 때, 여러 개면 하위 메뉴로 이모지 선택), `투표: <선택지>`/`투표 취소`/`투표 마감`(작성자) — 투표 메시지만, `수정 e`(내 것), `고정 p`/`고정 해제 p`(권한 없으면 Disabled), `복사 y`, `삭제 d`(권한), `신고`(남의 것 → `ReportDialog("message", id, "<이름>의 메시지")`), `작성자 차단`(남의 것 → `Confirm` → `Block(UserID(author))`).
- 마우스: 리액션 칩 줄 클릭 → `chipAt`(줄에서 각 이모지의 `Sanitize` 글자를 순서대로 찾아 다음 칩 시작 전까지를 그 칩 범위로) → 왼쪽 = 토글, 오른쪽 = `chat.reaction_users`. 투표 막대 클릭 = 그 선택지 투표(단일 = `[idx]`, 복수 = 토글한 집합).
- 리액션 누른 사람 오버레이: 제목 `<emoji> 누른 사람`, `widgets.List[api.UserSummary]`(아바타·이름·`@handle`), 맨 아래 도달 시 `next_cursor` 이어 받기.

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run 'TestActionsMenuGolden|TestReactionUsersGolden' -update`
Expected: `ok`. 비활성 `고정` 행이 dim 으로, 20×6 은 강등 전체 본문인지 확인.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run 'TestReact|TestToggle|TestEdit|TestDelete|TestPin|TestCopy|TestActions|TestReactionUsers|TestPollVote|TestChannel' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/actions.go internal/ui/chat/reactionusers.go internal/ui/chat/msgpane.go internal/ui/chat/actions_test.go internal/ui/chat/helpers_test.go internal/ui/chat/testdata/
git commit -m "feat(chat): message actions, reactions, pin, delete, copy, report, block, poll vote"
```

---

### Task 13: 스레드 화면 (`RouteThread`) — 모드별 배치

**Files:**
- Create: `internal/ui/chat/thread.go`
- Modify: `internal/ui/chat/msgpane.go`, `internal/ui/chat/channel.go` (`msg.thread`·재클릭이 `openThread` 를 쓰도록)
- Test: `internal/ui/chat/thread_test.go`

**Interfaces:**
- Consumes: `ui.RegisterRoute(ui.RouteThread, …)`, `ui.RouteUpdatedMsg`, `ui.Toast`, `pendingFor`(Task 4), `(*api.Client).GetThread(ctx, root, api.ThreadQuery{Before, After, Limit})`, `(*api.Client).ReplyThread(ctx, root, api.ThreadReplyRequest{ClientNonce, Kind, Body})`, `chatstore.Thread/SetThreadOpen/ApplyThreadPage`, `msgPane`(Task 8, `rootID` 지정), `chatComposer`(Task 9, `rootID` 지정), `layout.Layout.Pane`, `h.Model.Frame().Routes`.
- Produces:
  - `type threadView struct{...}`, `func newThread(ctx *ui.Context, r ui.Route) ui.Component` — `KeyHandler`, `MouseHandler`, `InputCapturer`, `Cursorer`, `Titled`, `Helper`, `Persistent`, `Completing() bool`(CR-1 모양, Task 10 과 같음) (**`ChannelViewer` 는 구현하지 않는다** — 스레드만 보일 때 채널 최상위 안 읽음은 늘어야 함)
  - `func (v *threadView) jumpTo(ctx *ui.Context, replyID string) tea.Cmd` — `Route.Anchor`(알림·알림함·검색 결과에서 특정 답글로 이동) 처리
  - `func openThread(channelID, rootID string) tea.Cmd` = `ui.Navigate(ui.Route{Kind: ui.RouteThread, ID: rootID, Arg: channelID})` (Task 8 의 `msg.thread`·재클릭이 이것을 쓰도록 교체)

- [ ] **Step 1: Write the failing test**

```go
func TestThreadPlacementPerMode(t *testing.T) {
	cases := []struct{ sz uitest.Size; slot layout.SlotID; mainKind ui.RouteKind }{
		{uitest.Size{W: 200, H: 60}, layout.SlotThread, ui.RouteChannel}, // 지휘소: 오른쪽 위
		{uitest.Size{W: 120, H: 40}, layout.SlotThread, ui.RouteChannel}, // 표준: 아래 분할
		{uitest.Size{W: 120, H: 30}, layout.SlotMain, ui.RouteThread},    // 컴팩트: 교체
		{uitest.Size{W: 80, H: 24}, layout.SlotMain, ui.RouteThread},     // 위젯: 교체
		{uitest.Size{W: 50, H: 20}, layout.SlotMain, ui.RouteThread},     // 초소형: 교체
	}
	for _, c := range cases {
		h := devChannel(t, c.sz); selectText(t, h, "오늘 배포"); h.Press("t")
		f := h.Model.Frame()
		if f.Routes[c.slot].Kind != ui.RouteThread || f.Routes[layout.SlotMain].Kind != c.mainKind { t.Fatalf("%v: %+v", c.sz, f.Routes) }
		if c.sz == (uitest.Size{W: 200, H: 60}) { if _, ok := f.Layout.Pane(layout.SlotMembers); ok { t.Fatal("thread replaces members") } }
		if c.sz == (uitest.Size{W: 120, H: 40}) {
			m, _ := f.Layout.Pane(layout.SlotMain); th, _ := f.Layout.Pane(layout.SlotThread)
			if th.Rect.Y <= m.Rect.Y || th.Rect.X != m.Rect.X { t.Fatal("standard: thread below main") }
		}
		if c.slot == layout.SlotMain { h.Press("esc"); if h.Model.Frame().Routes[layout.SlotMain].Kind != ui.RouteChannel { t.Fatal("esc back to channel") } }
	}
}
func TestThreadGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), devChannel, func(t *testing.T, h *uitest.Harness) {
		selectText(t, h, "오늘 배포"); h.Press("enter"); mustContain(t, h.Screen(), "스레드"); h.Golden("thread")
	})
}
func TestThreadReply(t *testing.T) {
	// open thread, i, "2시?", enter → POST /v1/messages/<root>/thread {client_nonce valid, kind "text", body "2시?"}; reply shown once (echo from chatRoutes)
	// pending stays out of the channel list: 200x60, exact-path handler for POST /v1/messages/<root>/thread returning 503 →
	//   paneText(SlotThread) contains "2시?" and "전송 실패"; paneText(SlotMain) (channel view) does NOT contain "2시?"
}
func TestThreadAnchorJump(t *testing.T) {
	// newAPI + ONE exact-path handler for GET /v1/messages/<root>/thread switching on r.URL.Query():
	//   no before/after → replies msgID(51..100), has_more_before=true; before=msgID(51) → msgID(1..50), has_more_before=false
	// open Route{Kind: RouteThread, ID: root, Arg: ch, Anchor: msgID(7)} at 120x30
	//   → requests "limit=50" then "before="+msgID(51)+"&limit=50"; selected reply == msgID(7) and visible
	// h.Send(ui.RouteUpdatedMsg{Route: same route with Anchor: msgID(90)}) → no new request (already in window), selected == msgID(90)
	// Anchor of an id that never shows up (pages exhausted) → selection stays on the root, toast "답글을 찾을 수 없어요", no panic
}
func TestThreadUpdatesChannelSummary(t *testing.T) { /* thread.updated reply_count 4 → channel view shows "답글 4개" */ }
func TestThreadLoadsOlderReplies(t *testing.T) { /* has_more_before → scrolling to top requests before=<first reply id> */ }
func TestThreadDraftAcrossModes(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 30}); selectText(t, h, "오늘 배포"); h.Press("enter", "i"); h.Type("2시?")
	key := h.Model.Frame().Routes[layout.SlotMain].Key(); inst := h.Instance(key)
	h.Resize(200, 60)
	if h.Model.Frame().Routes[layout.SlotThread].Key() != key || h.Instance(key) != inst || h.Model.Focus().Slot != layout.SlotThread { t.Fatal("same instance in SlotThread, focused") }
	if inst.(*threadView).composer.c.Value() != "2시?" { t.Fatal("draft kept") }
	h.Resize(120, 30)
	if h.Model.Frame().Routes[layout.SlotMain].Key() != key { t.Fatal("back to Main") }
}
func TestThreadHostileIntegrity(t *testing.T) { /* hostile root with 9,999 replies opened × AllSizes → h.Check() */ }
func TestThreadRootDeleted(t *testing.T) { /* message.deleted tombstone on root → header shows "삭제된 메시지", replies remain */ }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run TestThread -v`
Expected: FAIL — `undefined: newThread`.

- [ ] **Step 3: Implement `thread.go`**

- `init()`: `ui.RegisterRoute(ui.RouteThread, newThread)`. `Init`: `SetThreadOpen(root, true)`, `GetThread(Limit 50)`(태그 `chat.thread:<root>`) → `ApplyThreadPage`. 이전 답글: 맨 위에서 `Before: 첫 답글`.
- 그리기: 맨 위 루트 메시지(`renderRow`, `InThread:true`, 고정 높이 아님 — 길면 `MaxLines:6` + `… 더 보기`), 구분선 `답글 N개`(`S.Dim`), 답글 목록(`msgPane`), 입력창(`› 답글`). 밀도 = `ctx.Density()`, 단 Full 인데 창 폭 < 48 이면 `DensityLine`(지휘소 오른쪽 열 34칸 — 시안 ① `준호 2시?`).
- `Titled` = `스레드 · <루트 작성자>: <Plain(루트, 12칸)>`(시안 ②). 재진입(같은 키)은 같은 인스턴스. 루트가 없어지면(삭제·채널 소멸) Task 8 과 같은 안내.
- 입력 중 송신은 `RT.Typing(ch, &root)`, 전송은 `ReplyThread`(같은 대기/재전송 경로, `/poll` 거부). 답글 목록의 대기 줄은 `pendingFor(ctx, ch, root)`(= `*ThreadRootID == root`)만 — 채널 화면은 `pendingFor(ctx, ch, "")` 라서 스레드 답글 대기가 채널 목록에 섞이지 않는다(Task 8).
- **앵커(특정 답글로 이동)**: `Init` 과 `ui.RouteUpdatedMsg`(같은 경로 키에 새 `Anchor`) 에서 `Route.Anchor != ""` 면 `jumpTo(anchor)`. 답글 창에 있으면 그 행 선택 + 보이게 스크롤. 없으면 `ThreadQuery` 에 `Around` 가 없으므로 페이지를 넘긴다: 앵커 ID < 첫 답글 ID 이고 `HasMoreBefore` 면 `Before: 첫 답글`, 앵커 ID > 마지막 답글 ID 이고 `HasMoreAfter` 면 `After: 마지막 답글`(ULID 사전순 비교), 각 결과를 `ApplyThreadPage` 한 뒤 다시 판정(진행 중 1개, 최대 20쪽). 더 넘길 수 없는데도 없으면 루트를 선택하고 `ui.Toast("답글을 찾을 수 없어요", ui.ToastInfo)`. 처리한 앵커 값을 기억해 같은 값으로 두 번 끌려가지 않게 한다(사용자가 그 뒤 스크롤한 위치를 존중).

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run TestThreadGolden -update`
Expected: `ok`. 200×60 = 오른쪽 위 스레드 창(멤버 창 자리), 120×40 = 대화창 아래 분할(시안 ② `스레드 · 민지: 오늘 배포…`), 120×30·80×24·50×20 = Main 전체가 스레드인지 확인.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run 'TestThread|TestChannel|TestComposer' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/thread.go internal/ui/chat/msgpane.go internal/ui/chat/channel.go internal/ui/chat/thread_test.go internal/ui/chat/testdata/TestThread*
git commit -m "feat(chat): thread panel with per-mode placement"
```

---

### Task 14: 입력 중 표시·접속 상태·멤버 창 (`SlotMembers`)

**Files:**
- Create: `internal/ui/chat/typingline.go`, `internal/ui/chat/members.go`
- Modify: `internal/ui/chat/channel.go`, `internal/ui/chat/thread.go` (입력 중 줄)
- Test: `internal/ui/chat/presence_test.go`

**Interfaces:**
- Consumes: `chatstore.Typing(ch, root, now)`, `realtime.Typing{ExpiresInMS}`, `ui.RegisterPane(layout.SlotMembers, …)`, `(*api.Client).ListChannelMembers`, `(*api.Client).GetPresence(ctx, ids []string) ([]api.PresenceItem, error)`(≤100 씩 나눔), `Store.Presence`, `widgets.PresenceDot`, `widgets.ReportDialog("user", …)`, `ui.After`.
- Produces:
  - `func typingText(ctx *ui.Context, ids []string) string` — `민지 입력 중…` / `민지, 준호 입력 중…` / `여러 명이 입력 중…`(3명 이상)
  - `type typingTickMsg struct{ ch string }` — 만료 시점에 다시 그리기용
  - `type membersPane struct{...}`, `func newMembers(ctx *ui.Context) ui.Component`, 내부 `type presencePollMsg struct{ gen uint64 }`
  - `func memberActions(ctx *ui.Context, u api.UserSummary) []menuItem` — `DM 보내기`, `프로필 보기`(`ui.Navigate(RouteProfile{ID: u.ID})`), `사용자 신고`, `차단`

- [ ] **Step 1: Write the failing test**

```go
func TestTypingLine(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40}); s := h.Context().Store
	minji, junho := userBy(t, s, "minji"), userBy(t, s, "junho")
	h.Event("typing", realtime.Typing{ChannelID: devID(t, h), UserID: minji.ID, ExpiresInMS: 6000})
	mustContain(t, h.Screen(), "민지 입력 중…")
	h.Event("typing", realtime.Typing{ChannelID: devID(t, h), UserID: junho.ID, ExpiresInMS: 6000})
	mustContain(t, h.Screen(), "민지, 준호 입력 중…")
	h.Advance(6 * time.Second) // expiry = local receipt time (chatOf → SetClock(ctx.Now) = FakeClock) + 6s; the event's TS is not used
	if strings.Contains(h.Screen(), "입력 중") { t.Fatal("expired after 6s") }
}
func TestTypingClearedByMessage(t *testing.T) { /* typing minji then message.created by minji → line gone without Advance */ }
func TestTypingThreadScoped(t *testing.T) { /* typing with thread_root_id → only thread view shows it, channel view doesn't */ }
func TestTypingExcludesBlocked(t *testing.T) { /* Blocks[minji] → typing minji not shown */ }
func TestMembersPaneGoldenAndPolling(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	mustContain(t, paneText(t, h, layout.SlotMembers), "민지", "준호")
	n0 := len(reqs(h.API, "GET", "/v1/presence"))
	h.Advance(60 * time.Second)
	if len(reqs(h.API, "GET", "/v1/presence")) != n0+1 { t.Fatal("re-poll every 60s while visible") }
	h.Resize(120, 40); h.Advance(120 * time.Second) // ModeChangedMsg: SlotMembers gone → the pending tick finds no pane and stops
	if len(reqs(h.API, "GET", "/v1/presence")) != n0+1 { t.Fatal("no polling while hidden") }
	h.Resize(200, 60) // ModeChangedMsg: pane back → poll restarts immediately (no View-timing dependency)
	if len(reqs(h.API, "GET", "/v1/presence")) != n0+2 { t.Fatal("restart on show") }
	h.Advance(59 * time.Second)
	if len(reqs(h.API, "GET", "/v1/presence")) != n0+2 { t.Fatal("next poll only at 60s") }
	h.Advance(time.Second)
	if len(reqs(h.API, "GET", "/v1/presence")) != n0+3 { t.Fatal("re-armed after restart") }
}
func TestMembersPaneActions(t *testing.T) { /* focus members, select 준호, "m" → menu with "DM 보내기","사용자 신고","차단"; DM 보내기 → POST /v1/dms {"users":["@junho"]} */ }
func TestMembersHostileIntegrity(t *testing.T) { forEachSize(t, uitest.ModeSizes[layout.ModeCommand], hostileOpen, func(t *testing.T, h *uitest.Harness) { h.Check() }) }
```

멤버 창 골든은 `TestChannelGolden` 의 지휘소 크기 골든에 포함된다(이 Task Step 4 에서 다른 낡는 골든과 함께 명시적으로 `-update`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run 'TestTyping|TestMembers' -v`
Expected: FAIL — `undefined: typingText`.

- [ ] **Step 3: Implement `typingline.go`, `members.go`**

- 입력 중 줄: 입력창 바로 위 1줄(`S.Dim`, 이탤릭), 비면 줄을 차지하지 않는다. `typing` 이벤트를 받으면 `ui.After(ctx, ExpiresInMS+50ms, typingTickMsg{ch})` 로 만료 직후 한 번 더 그린다. 이름은 `Sanitize` + 각 6칸 자름.
- 멤버 창: `init()` `ui.RegisterPane(layout.SlotMembers, newMembers)`. 대상 채널 = `viewedChannel(ctx)`(지휘소에서 스레드가 닫혀 있을 때만 이 창이 존재). DM/그룹 = `Channel.Members`, text = `ListChannelMembers`(첫 100명, `+ N명 더` 줄). 정렬: 온라인 → 자리비움 → 방해금지 → 오프라인 → 표시 이름. 줄 = `<점> 이름 · 역할`(방 owner/admin 은 `방장`/`관리자`, 공간 멤버 목록 캐시가 없으면 생략). `Titled` = `멤버 N`.
- 접속 상태 폴링: `View` 는 문자열만 돌려주고 `Resize` 는 Cmd 를 못 내므로 **`Init`·`Update`·`HandleKey` 에서만** 시작한다. 가시성은 결정적으로 `_, shown := ctx.Layout.Pane(layout.SlotMembers)` 로 판정한다(`View` 호출 시각·횟수는 쓰지 않는다). 상태 `polling bool` + `gen uint64`:
  - 시작 `ensurePolling(ctx)`: `shown && !polling` 이면 `polling=true`, `gen++`, 즉시 `GetPresence`(태그 `chat.presence:<ch>`) + `ui.After(ctx, 60*time.Second, presencePollMsg{gen})`. `Init`, 그리고 `Update` 가 받는 모든 방송 메시지(특히 `ui.ModeChangedMsg` — 모드 전환으로 창이 다시 생김, `ui.FocusChangedMsg` — 지휘소에서 스레드를 닫아 창이 돌아옴, `EventMsg`, `ResultMsg`)와 `HandleKey` 에서 부른다.
  - `presencePollMsg{gen}`: 최신 세대가 아니면 버림. `shown` 이면 다시 조회 + 같은 세대로 재무장, 아니면 `polling=false`(멈춤 — 다음 `ensurePolling` 이 다시 시작).
  - 보는 채널이 바뀌면(`viewedChannel` 의 ID 가 달라짐) 다음 `ensurePolling` 기회에 `gen++` 로 새 채널 기준 즉시 조회.
  - 결과는 `Store.Presence[id] = store.PresenceEntry{Status, LastSeenAt, FetchedAt: ctx.Now()}`.
- 키: 목록 이동, `msg.actions`(`m`)/`enter` → `memberActions` 메뉴.

- [ ] **Step 4: Update goldens and review them**

이 Task 가 지휘소(200×60 등 `ModeCommand` 크기)의 `SlotMembers` 를 채우므로, devChannel 위에서 셸 전체를 찍는 **앞 Task 의 골든 전부**가 지휘소 크기에서 낡는다. 정확히 이 목록만 갱신한다:

Run: `go test ./internal/ui/chat/ -run 'TestChannelGolden|TestComposerGolden|TestCompletionGolden|TestInviteCreatedGolden|TestActionsMenuGolden|TestReactionUsersGolden|TestThreadGolden' -update`
Expected: `ok`. `git diff --stat internal/ui/chat/testdata` 에서 바뀐 파일이 **지휘소 크기(200×60, 그리고 `AllSizes` 를 쓰는 시험의 200×50·240×70·320×90) 골든뿐**인지, 각 diff 가 오른쪽 위 `멤버` 창(접속 점 포함, 시안 ① 오른쪽 열)의 등장뿐인지 확인. 다른 크기·다른 줄이 바뀌었으면 갱신을 되돌리고(`git checkout -- internal/ui/chat/testdata`) 원인을 고친다. `TestThreadGolden` 은 지휘소에서 스레드가 멤버 자리를 쓰므로 바뀌지 않는 것이 정상이다(바뀌면 결함). 레일·사이드바 골든(`paneGolden`)과 홈 기반 오버레이 골든(Task 15 에서)은 대상이 아니다.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run 'TestTyping|TestMembers|TestChannel|TestThread|TestComposer|TestCompletion|TestMention|TestInviteCreated|TestActionsMenu|TestReactionUsers|TestRail|TestSidebar' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/typingline.go internal/ui/chat/members.go internal/ui/chat/channel.go internal/ui/chat/thread.go internal/ui/chat/presence_test.go internal/ui/chat/testdata/
git commit -m "feat(chat): typing indicator, presence polling and members pane"
```

---

### Task 15: 홈 화면 (`RouteHome`)

**Files:**
- Create: `internal/ui/chat/home.go`
- Test: `internal/ui/chat/home_test.go`

**Interfaces:**
- Consumes: `ui.RegisterRoute(ui.RouteHome, …)`, `visibleDMs`(Task 4), `Store.Channels/Friends/FriendReqIn/DirectInvites/Presence`, `widgets.List`, `widgets.Empty`, `ui.Navigate`.
- Produces: `type homeView struct{...}`, `func newHome(ctx *ui.Context, r ui.Route) ui.Component` — `KeyHandler`, `MouseHandler`, `Titled`(`홈`), `Helper`, `Persistent`; `type homeItem struct{ ID, Label, Detail string; Badge ui.Badge; Route ui.Route }`.

- [ ] **Step 1: Write the failing test**

```go
func TestHomeGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), openHome, func(t *testing.T, h *uitest.Harness) { mustContain(t, h.Screen(), "─ DM", "민지"); h.Golden("home") })
}
func TestHomeInvitesAndRequests(t *testing.T) {
	// seed one DirectInvite + one FriendReqIn → rows "받은 방 초대 1", "친구 요청 1" at top
	// enter on "받은 방 초대 1" → top(h) == Route{Kind: RouteInvite, Arg: "direct"}; on "친구 요청 1" → RouteFriends
}
func TestHomeOpenDMAndFeed(t *testing.T) { /* enter on "● 민지" → RouteChannel dm id; enter on "피드 보기" → h.Nav().Front == app.FrontFeed */ }
func TestHomeEmptyGolden(t *testing.T) {
	forEachSize(t, uitest.Required, func(t *testing.T, sz uitest.Size) *uitest.Harness { return open(t, sz, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(func(*store.Store) {})) },
		func(t *testing.T, h *uitest.Harness) { mustContain(t, h.Screen(), "아직 대화가 없어요"); h.Golden("home_empty") })
}
func TestHomeLiveUpdates(t *testing.T) { /* friend.request_received event → "친구 요청 1" appears without reopening */ }
func TestHomeHidesBlockedDM(t *testing.T) { /* openHome 120x40: "준호" DM row present → Blocks[junho] + block.updated → no 준호 DM row (and not in 접속 중인 친구 either — block removes friendship); unblock event → DM row back */ }
func TestHomeHostileIntegrity(t *testing.T) { /* Hostile seeded home × AllSizes → h.Check() */ }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run TestHome -v`
Expected: FAIL — `undefined: newHome`.

- [ ] **Step 3: Implement `home.go`**

- 항목 순서: (`DirectInvites` 있으면) `받은 방 초대 N` → `RouteInvite{Arg:"direct"}`, (`FriendReqIn` 있으면) `친구 요청 N` → `RouteFriends`, `피드 보기` → `ui.Navigate(ui.Route{Kind: ui.RouteFeed, Arg: "following"})`, `친구 목록` → `RouteFriends`, `─ DM`(구분, 선택 불가) + `visibleDMs(ctx)` 각 DM(사이드바와 같은 줄 형식·배지 — 차단한 상대의 1:1 DM 은 `block.updated` 즉시 빠짐), `─ 접속 중인 친구` + 온라인/자리비움 친구(`● 민지 · 🎧 듣는 중…` — `status` 있으면 `emoji text`).
- DM·친구가 모두 없으면 `widgets.Empty(t, size, "💬", "아직 대화가 없어요", "ctrl+k 로 친구를 찾거나 + 로 방을 만들어 보세요")`.
- 키: 목록 이동·`enter`·클릭(재클릭 열기), 줄 갱신은 매 `View` 에서 Store 로 다시 만든다(`SetItems`, ID 로 선택 유지).

- [ ] **Step 4: Create goldens and review them (and refresh Task 4/6 goldens)**

Run: `go test ./internal/ui/chat/ -run 'TestHomeGolden|TestHomeEmptyGolden|TestMenuOverlayGolden|TestFormGoldenAndCursor' -update`
Expected: `ok`. 홈 골든이 DM·접속 친구 목록을 보이는지 확인. Task 4 의 두 오버레이 골든(`openHome` 위)은 그 뒤 Task 6·7·14 가 레일·사이드바·멤버 창을, 이 Task 가 Main 의 홈 화면을 채웠으므로 이번에 **한 번** 갱신한다 — `git diff --stat internal/ui/chat/testdata` 가 이 네 시험의 파일만 보이는지, 오버레이 diff 가 오버레이 상자 **바깥**(배경: `준비 중` 자리표시 → 홈 화면, 지휘소·표준의 레일·사이드바·멤버 창)뿐이고 상자 안은 그대로인지 확인.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -v`
Expected: PASS(패키지 전체 — Task 9·14·이 Task 의 갱신 단계로 앞 골든이 모두 최신이어야 한다). 골든 불일치가 나오면 패키지 전체 `-update` 를 하지 말고 어느 Task 의 주변 변경이 빠졌는지 찾아 그 시험만 갱신·검토한다(Global Constraints 골든 갱신 규칙).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/home.go internal/ui/chat/home_test.go internal/ui/chat/testdata/
git commit -m "feat(chat): home route with DMs, friends, invites entry points"
```

---

### Task 16: 친구·요청 화면 (`RouteFriends`) + 사용자 선택기 + `friend.add`

**Files:**
- Create: `internal/ui/chat/friends.go`, `internal/ui/chat/userpicker.go`
- Test: `internal/ui/chat/friends_test.go`

**Interfaces:**
- Consumes: `ui.RegisterRoute(ui.RouteFriends, …)`, `palette.Register(palette.Command{ID, Title, Keywords, Group, Available, Run})`, `(*api.Client).ListFriendRequests(ctx, "outgoing")`, `SendFriendRequest(ctx, api.UserRef) (*api.FriendRequestResult, error)`, `AcceptFriendRequest`, `DeclineFriendRequest`, `CancelFriendRequest`, `RemoveFriend`, `OpenDM(ctx, api.OpenDMRequest{Users, Name}) (*api.Channel, bool, error)`, `SearchUsers(ctx, q, limit)`, `Block`, `widgets.Tabs{Set, View, Hit}`, `widgets.Confirm`, `widgets.ReportDialog`, `Store.Friends/FriendReqIn`, `ui.After`.
- Produces:
  - `type friendsView struct{ tab int; ... }`, `func newFriends(ctx *ui.Context, r ui.Route) ui.Component`
  - `type pickerSpec struct{ Title, Submit string; Max int; Search bool; Candidates func(ctx *ui.Context) []api.UserSummary; Exclude func(ctx *ui.Context, id string) bool; NameField bool; OnPick func(ctx *ui.Context, users []api.UserSummary, name string) (tag string, cmd tea.Cmd); OnDone func(ctx *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay) }`, `func newUserPicker(spec pickerSpec) ui.Overlay`
  - `func openDMCmd(ctx *ui.Context, users []api.UserRef, name string) tea.Cmd` — 성공 시 `ObserveChannel` + `Navigate(RouteChannel)`(태그 `chat.dm_open:<first>`)
  - 오버레이 `chat.friend_add`(`func newFriendAdd(ctx *ui.Context, args string) ui.Overlay` = `newUserPicker(Max 1, Search true)`, args = 처음 검색어)
  - 팔레트 `friends`(→`RouteFriends`), `friend.add`(→ `ui.OpenRegisteredOverlay("chat.friend_add", args)`)

- [ ] **Step 1: Write the failing test**

```go
func TestFriendsGolden(t *testing.T) {
	for i, name := range []string{"friends", "incoming", "outgoing"} {
		forEachSize(t, uitest.AllSizes(), openFriends, func(t *testing.T, h *uitest.Harness) {
			for j := 0; j < i; j++ { h.Press("l") }
			h.Golden(name)
		})
	}
}
func TestFriendsActions(t *testing.T) {
	// incoming tab: enter → POST /v1/friends/requests/<id>/accept; "m" → menu "거절" → POST .../decline
	// outgoing: "m" → "요청 취소" → DELETE /v1/friends/requests/<id>
	// friends: enter on 민지 → POST /v1/dms {"users":["@minji"]} → top(h).Kind == RouteChannel
	// friends: "m" → "친구 끊기" → Confirm → DELETE /v1/friends/@minji ; "차단" → Confirm → PUT /v1/blocks/@minji ; "신고" → ReportDialog
}
func TestFriendAddSearchDebounce(t *testing.T) {
	h := openFriends(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "friend.add"}); h.Type("jun")
	if len(reqs(h.API, "GET", "/v1/users/search")) != 0 { t.Fatal("debounced") }
	h.Advance(300 * time.Millisecond)
	if r := reqs(h.API, "GET", "/v1/users/search"); len(r) != 1 || !strings.Contains(r[0].Query, "q=jun") { t.Fatalf("%v", r) }
	h.Press("enter", "enter") // pick 준호, submit
	mustJSONEq(t, reqs(h.API, "POST", "/v1/friends/requests")[0].Body, "user", "@junho")
}
func TestFriendAddErrors(t *testing.T) { /* 409 already_exists → api.Describe shown in picker, stays open; 200 auto-accepted → toast "이제 친구예요" */ }
func TestFriendsLiveEvents(t *testing.T) { /* friend.request_received → incoming tab label count +1; friend.request_accepted → outgoing refetched */ }
func TestUserPickerGolden(t *testing.T) {
	forEachSize(t, overlaySizes, openHome, func(t *testing.T, h *uitest.Harness) { h.Send(ui.RunCommandMsg{ID: "friend.add"}); h.Golden("user_picker") })
}
func TestFriendsHostileIntegrity(t *testing.T) { /* Hostile × AllSizes × 3 tabs → h.Check() */ }
```

`openFriends(t, sz)` = `RouteFriends` 하네스, 보낸 요청 목록은 `srv.JSON("GET /v1/friends/requests", …)` 대신 질의로 갈라야 하므로 `srv.Handle` 에서 `direction` 을 보고 응답(시험 파일 안 데이터).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run 'TestFriend|TestUserPicker' -v`
Expected: FAIL — `undefined: newFriends`.

- [ ] **Step 3: Implement `friends.go`, `userpicker.go`**

- 화면: 맨 위 `widgets.Tabs` = `친구 N` / `받은 요청 N` / `보낸 요청 N`, `←/→`·`h/l`·클릭으로 전환(컴포넌트 내부 키, 전역 `[`/`]` 는 쓰지 않음). 친구 탭 첫 줄 `＋ 친구 추가`. 친구 줄 `<점> 표시 이름 @handle · 상태`, 요청 줄 `표시 이름 @handle · <humanize.Relative>`.
- `enter`: 친구 → `openDMCmd`, 받은 요청 → 수락, `＋ 친구 추가` → 선택기. `m` → 탭별 메뉴(받은: 수락/거절/사용자 신고/차단, 보낸: 요청 취소, 친구: DM 보내기/방에 초대(방이 1개 이상일 때 방 선택 하위 메뉴 → `directInviteCmd`)/친구 끊기/사용자 신고/차단).
- 보낸 요청은 화면 진입·`friend.request_*` 이벤트마다 `ListFriendRequests("outgoing")`(태그 `chat.friend_out:me`). 받은 요청·친구는 `Store` 그대로.
- 사용자 선택기: 위 textinput(검색어) + 고른 사람 칩 줄 + 후보 목록. 후보 = `Candidates(ctx)` 중 검색어 일치(handle·표시 이름·초성) + `Search` 면 2글자 이상에서 300ms 디바운스(`ui.After` 세대 번호) 후 `SearchUsers(q, 10)`. `Exclude` 적용, 나 제외. `enter`: 후보 선택 중이면 칩에 추가(`Max` 초과 → `최대 N명까지 고를 수 있어요`), 검색어가 비고 칩이 있으면 제출. `backspace`(빈 검색어) = 마지막 칩 제거. `NameField` 면 `tab` 으로 `대화 이름(선택)` 칸. 제출·결과는 Task 4 의 폼과 같은 태그·morph 규칙.

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run 'TestFriendsGolden|TestUserPickerGolden' -update`
Expected: `ok`. 탭 줄 배지·`＋ 친구 추가` 줄·선택기 칩 줄이 겹치지 않는지 확인.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run 'TestFriend|TestUserPicker' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/friends.go internal/ui/chat/userpicker.go internal/ui/chat/friends_test.go internal/ui/chat/helpers_test.go internal/ui/chat/testdata/
git commit -m "feat(chat): friends and requests screen, user picker, friend.add"
```

---

### Task 17: DM·그룹 DM 만들기와 채널 설정(`channel.prefs`)

**Files:**
- Create: `internal/ui/chat/dmnew.go`, `internal/ui/chat/channelprefs.go`
- Test: `internal/ui/chat/dm_test.go`

**Interfaces:**
- Consumes: `newUserPicker(pickerSpec)`, `openDMCmd`, `newForm`, `newMenu`, `(*api.Client).AddChannelMembers(ctx, ch, []api.UserRef)`, `RemoveChannelMember(ctx, ch, api.UserRef)`(`api.Me` = 나가기), `UpdateChannel(ctx, ch, api.UpdateChannelRequest{Name, Topic, Position})`, `DeleteChannel`, `SetChannelPrefs(ctx, ch, api.ChannelPrefsRequest{Notify, Muted})`, `widgets.Confirm`.
- Produces:
  - 오버레이 `chat.dm_new`(`func newDMDialog(ctx *ui.Context, args string) ui.Overlay`, `Max: 9`, 2명 이상이면 이름 칸), 팔레트 `dm.new`
  - 오버레이 `chat.channel_prefs`(`func newChannelPrefs(ctx *ui.Context, args string) ui.Overlay`, args = 채널 ID; 비면 `viewedChannel`), 팔레트 `channel.prefs`
  - `func channelPrefItems(ctx *ui.Context, ch *api.Channel) []menuItem`

- [ ] **Step 1: Write the failing test**

```go
func TestDMNewOneToOne(t *testing.T) {
	// RunCommandMsg dm.new; Type("minji"); enter; enter → POST /v1/dms {"users":["@minji"]} (no "name" key); 200 created=false → top(h) == existing dm channel
}
func TestDMNewGroup(t *testing.T) {
	// pick minji, junho; tab; Type("점심팟"); enter → body {"users":["@minji","@junho"],"name":"점심팟"}; 201 → Store.Channels has group, top(h) is it, sidebar "◇ 점심팟"
}
func TestDMNewLimit(t *testing.T) { /* picking a 10th user shows "최대 9명까지 고를 수 있어요" */ }
func TestDMNewNotAllowed(t *testing.T) { /* 403 dm_not_allowed → api.Describe text in dialog, still open */ }
func TestDMNewGolden(t *testing.T) { forEachSize(t, overlaySizes, openHome, func(t *testing.T, h *uitest.Harness) { h.Send(ui.RunCommandMsg{ID: "dm.new"}); h.Golden("dm_new") }) }
func TestChannelPrefsNotifyMute(t *testing.T) {
	// on #개발: channel.prefs → "알림: 멘션만" → PUT /v1/channels/<id>/prefs {"notify":"mentions"}; "음소거" → {"muted":true}
}
func TestChannelPrefsGroupDM(t *testing.T) {
	// group dm (owner=me): items "이름 바꾸기" (form → PATCH {"name":"새이름"}), "멤버 추가" (picker → POST members {"users":["@hana"]}), "멤버 내보내기", "나가기" (Confirm → DELETE /v1/channels/<id>/members/@me → top(h).Kind == RouteHome)
	// group dm (not owner): no "멤버 내보내기"
}
func TestChannelPrefsTextAdmin(t *testing.T) {
	// MyRole owner on #개발: "이름·주제 바꾸기" → PATCH {"name":..,"topic":..}; "채널 삭제" → Confirm → DELETE /v1/channels/<id>
	// on default channel #일반: "채널 삭제" Disabled with Hint "기본 채널"
	// private text, role member & channel member: "멤버 추가" present, "멤버 내보내기" absent
}
func TestChannelPrefsGolden(t *testing.T) { forEachSize(t, overlaySizes, devChannel, func(t *testing.T, h *uitest.Harness) { h.Send(ui.RunCommandMsg{ID: "channel.prefs"}); h.Golden("channel_prefs") }) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run 'TestDMNew|TestChannelPrefs' -v`
Expected: FAIL — `undefined: newDMDialog`.

- [ ] **Step 3: Implement `dmnew.go`, `channelprefs.go`**

- `dm.new`: 후보 = 친구(접속 순) + 검색, `Exclude` = 차단. 1명 → `OpenDMRequest{Users: [Handle(h)]}`, 2..9명 → `Name: Some(name)`(빈 이름이면 생략).
- 채널 설정 메뉴(모든 채널): `알림: 모두` / `알림: 멘션만` / `알림: 끄기`(현재 값에 `✓` Hint), `음소거`/`음소거 해제`, `고정 메시지 보기`(→ `chat.pins`, Task 22). 그룹 DM: `이름 바꾸기`(폼 0..50), `멤버 추가`(선택기 `Max` = 10-현재, `Exclude` = 기존 멤버), `멤버 내보내기`(owner_id 만, 멤버 하위 메뉴 → Confirm), `나가기`(Confirm → `RemoveChannelMember(api.Me)` → `ui.Navigate(RouteHome)`). text 채널: `canManageSpace` 면 `이름·주제 바꾸기`(폼: 이름 정규식 검증, 주제 0..250), `채널 삭제`(기본 채널이면 Disabled); 비공개 text 는 방 owner/admin 또는 채널 멤버면 `멤버 추가`(후보 = 방 멤버 목록 `ListSpaceMembers`), owner/admin 이면 `멤버 내보내기`.
- 결과 `Channel` 은 `ObserveChannel`, `ChannelMyState` 는 `Store.Channels[id].My` 에 반영.

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run 'TestDMNewGolden|TestChannelPrefsGolden' -update`
Expected: `ok`. `✓` 표시와 비활성 항목이 구분되는지 확인.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run 'TestDMNew|TestChannelPrefs' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/dmnew.go internal/ui/chat/channelprefs.go internal/ui/chat/dm_test.go internal/ui/chat/testdata/
git commit -m "feat(chat): DM and group DM creation, channel settings overlay"
```

---

### Task 18: 방 만들기·초대 코드로 참여·초대 화면(`RouteInvite`)

**Files:**
- Create: `internal/ui/chat/spacecreate.go`, `internal/ui/chat/invite.go`
- Modify: `internal/ui/chat/rail.go` (`chat.space_add` 를 `ui.RegisterOverlay` 로 등록)
- Test: `internal/ui/chat/space_test.go`

**Interfaces:**
- Consumes: `newForm`, `adoptSpace`, `(*api.Client).CreateSpace(ctx, api.CreateSpaceRequest{Name, Description, IconEmoji}) (*api.SpaceDetail, error)`, `GetInvite(ctx, code) (*api.InvitePreview, error)`, `AcceptInvite(ctx, code) (*api.SpaceDetail, error)`, `AcceptDirectInvite`, `DeclineDirectInvite`, `api.IsCode(err, "invite_invalid")`, `(*api.Error).Details`, `ui.RegisterRoute(ui.RouteInvite, …)`, `Store.DirectInvites`, `widgets.ReportDialog("space", …)`.
- Produces:
  - 오버레이 `chat.space_create`(`func newSpaceCreate(ctx *ui.Context, args string) ui.Overlay`), `chat.space_join`(`func newSpaceJoin(ctx *ui.Context, args string) ui.Overlay`), `chat.space_add`(Task 6 의 `newSpaceAddMenu` 를 이 id 로 등록)
  - 팔레트 `space.create`, `space.join`, `invites.inbox`
  - `func parseInviteCode(s string) (string, bool)` — `"AB12CD34"`, `"masil join AB12CD34"`, 앞뒤 공백 허용, 8자 영숫자
  - `type inviteView struct{...}`, `func newInvite(ctx *ui.Context, r ui.Route) ui.Component`
  - `func inviteErrorText(err error) string` — `expired` `만료된 초대예요`, `exhausted` `사용 횟수가 다 찬 초대예요`, `revoked` `취소된 초대예요`, `not_found` `없는 초대 코드예요`, `banned_from_space` `이 방에 들어갈 수 없어요`, 그 외 `api.Describe`

- [ ] **Step 1: Write the failing test**

```go
func TestSpaceCreate(t *testing.T) {
	// space.create: enter with empty name → "이름을 입력해 주세요"; Type("점심 모임"), tab, Type("밥 먹는 방"), enter → POST /v1/spaces {"name":"점심 모임","description":"밥 먹는 방"}
	// response SpaceDetail → Store.SpaceOrder ends with new id, top(h) == RouteChannel{ID: DefaultChannelID}
}
func TestSpaceJoinParsesCode(t *testing.T) {
	for _, in := range []string{"AB12CD34", " masil join AB12CD34 "} { /* space.join, Paste(in), enter → top(h) == Route{Kind: RouteInvite, ID: "AB12CD34"} */ }
	// "abc" → inline "초대 코드는 8자예요"
}
func TestInviteGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), openInviteLink, func(t *testing.T, h *uitest.Harness) { mustContain(t, h.Screen(), "노바 개발방", "초대한 사람"); h.Golden("invite_link") })
	forEachSize(t, uitest.AllSizes(), openDirectList, func(t *testing.T, h *uitest.Harness) { h.Golden("invite_direct_list") })
	forEachSize(t, uitest.AllSizes(), openDirectOne, func(t *testing.T, h *uitest.Harness) { h.Golden("invite_direct_one") })
}
func TestInviteAccept(t *testing.T) { /* enter → POST /v1/invites/AB12CD34/accept → adoptSpace + top(h) default channel */ }
func TestInviteAlreadyMember(t *testing.T) { /* already_member:true → "이미 참여한 방이에요"; enter navigates without POST */ }
func TestInviteInvalid(t *testing.T) {
	for reason, text := range map[string]string{"expired": "만료된 초대예요", "exhausted": "사용 횟수가 다 찬 초대예요", "revoked": "취소된 초대예요"} {
		// GET /v1/invites/AB12CD34 → 410 {"error":{"code":"invite_invalid","details":{"reason":reason}}} → screen contains text
	}
}
func TestDirectInviteAcceptDecline(t *testing.T) {
	// Route{RouteInvite, Arg:"direct", ID: inv} enter → POST /v1/direct-invites/<id>/accept; "d" → POST .../decline → top(h).Kind == RouteHome
	// space.invite_removed event for the open invite → "더 이상 유효하지 않은 초대예요"
}
func TestInviteReportSpace(t *testing.T) { /* "m" → "방 신고" → ReportDialog overlay */ }
func TestSpaceDialogsGolden(t *testing.T) {
	for _, id := range []string{"chat.space_create", "chat.space_join", "chat.space_add"} {
		forEachSize(t, overlaySizes, openHome, func(t *testing.T, h *uitest.Harness) { h.Send(ui.OpenOverlayMsg{ID: id}); h.Golden(strings.TrimPrefix(id, "chat.")) })
	}
}
func TestInviteHostileIntegrity(t *testing.T) { /* preview with 50-char hostile space name + hostile inviter × AllSizes → h.Check() */ }
```

`openInviteLink`/`openDirectList`/`openDirectOne` 은 시험 파일에서 `GET /v1/invites/AB12CD34` 응답과 `DirectInvites` 시드를 넣어 연다.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run 'TestSpaceCreate|TestSpaceJoin|TestInvite|TestDirectInvite|TestSpaceDialogs' -v`
Expected: FAIL — `undefined: newSpaceCreate`.

- [ ] **Step 3: Implement `spacecreate.go`, `invite.go`**

- `space.create` 폼: `이름`(1..50, 필수), `설명`(0..200), `아이콘`(선택 칸: `없음 🚀 🍚 🎮 🎧 💼 🐱 🌱 ☕ 📚`). 성공 → `adoptSpace` + `SetSelectedSpace` + `Navigate(RouteChannel{ID: DefaultChannelID})`.
- `space.join` 폼: `초대 코드` 1칸, 제출 시 `parseInviteCode` → `Navigate(RouteInvite{ID: code})`(API 호출 없음).
- 초대 화면(`ID=code`): `GetInvite`(태그 `chat.invite:<code>`) → 가운데 카드: 아이콘·방 이름(`S.Title`), 설명, `멤버 N명`, `초대한 사람: 표시 이름`, `<날짜>까지`/`만료 없음`, 버튼 줄 `enter 참여하기 · m 더보기 · esc 닫기`. `already_member` 면 `이미 참여한 방이에요 · enter 열기`. 오류는 `inviteErrorText` 를 `widgets.Empty` 로.
- 직접 초대(`Arg="direct"`): `ID=""` → `Store.DirectInvites` 목록(방·초대한 사람·상대 시간), `enter` → 같은 경로 `ID=<id>` 로 `Navigate`(push). `ID=<id>` → 카드 + `enter 수락 · d 거절 · m 더보기`. 수락/참여 성공 → `adoptSpace` + 기본 채널로 이동, 거절 → `RouteHome`. 초대가 `Store.DirectInvites` 에서 사라지면 `더 이상 유효하지 않은 초대예요`.

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run 'TestInviteGolden|TestSpaceDialogsGolden' -update`
Expected: `ok`. 카드가 모든 크기에서 가운데에 있고, 50×20 에서 문구가 `…` 로만 잘리는지 확인.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run 'TestSpace|TestInvite|TestDirectInvite|TestRail' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/spacecreate.go internal/ui/chat/invite.go internal/ui/chat/rail.go internal/ui/chat/space_test.go internal/ui/chat/testdata/
git commit -m "feat(chat): create and join spaces, invite link and direct invite screens"
```

---

### Task 19: 방 설정 (`RouteSpaceSettings`) — 개요·멤버와 역할·차단(밴)

**Files:**
- Create: `internal/ui/chat/spacesettings.go`, `internal/ui/chat/spacemembers.go`
- Test: `internal/ui/chat/spacesettings_test.go`

**Interfaces:**
- Consumes: `ui.RegisterRoute(ui.RouteSpaceSettings, …)`, `(*api.Client).GetSpace`, `UpdateSpace(ctx, id, api.UpdateSpaceRequest{Name, Description, IconEmoji})`, `DeleteSpace`, `TransferSpace(ctx, id, api.UserRef)`, `ListSpaceMembers(ctx, id, api.SpaceMembersQuery{Q, Role, PageOpts})`, `SetSpaceMemberRole(ctx, id, api.UserRef, api.SpaceRole)`, `RemoveSpaceMember(ctx, id, api.UserRef, api.RemoveMemberOpts{Ban, Reason})`, `ListSpaceBans`, `Unban`, `widgets.Tabs`, `widgets.Confirm`, `newForm`, `newMenu`, `newUserPicker`, `realtime.SpaceMemberEvent`.
- Produces:
  - `type spaceSettingsView struct{ spaceID string; tab settingsTab; ... }`, `func newSpaceSettings(ctx *ui.Context, r ui.Route) ui.Component`
  - `type settingsTab int` (`tabOverview`, `tabMembers`, `tabInvites`, `tabChannels`, `tabBans`) — `tabInvites`·`tabChannels` 내용은 Task 20
  - `func memberRowActions(ctx *ui.Context, spaceID string, m api.SpaceMember) []menuItem`
  - 팔레트 `space.settings`(현재 방), `space.leave`

- [ ] **Step 1: Write the failing test**

```go
func TestSpaceSettingsGolden(t *testing.T) {
	for _, role := range []string{"owner", "member"} {
		for i, name := range []string{"overview", "members"} {
			forEachSize(t, uitest.AllSizes(), settingsAs(role), func(t *testing.T, h *uitest.Harness) {
				for j := 0; j < i; j++ { h.Press("l") }
				h.Golden(role + "_" + name)
			})
		}
	}
}
func TestSpaceSettingsTabsByRole(t *testing.T) {
	// owner: tabs "개요","멤버","초대","채널","차단됨"; member: only "개요","멤버"
}
func TestMemberRoleAndKick(t *testing.T) {
	// owner, members tab, select 준호, "m" → "관리자로 지정" → PATCH /v1/spaces/<id>/members/@junho {"role":"admin"}
	// "내보내기" → Confirm → DELETE /v1/spaces/<id>/members/@junho (empty query)
	// "차단(밴)" → form reason "도배" → DELETE ...?ban=true&reason=%EB%8F%84%EB%B0%B0
	// admin cannot act on owner/admin rows (items absent); member sees only "DM 보내기","사용자 신고","차단"
}
func TestMemberFilter(t *testing.T) { /* members tab "/" focuses filter input, Type("준") → GET members?q=%EC%A4%80 after 300ms */ }
func TestMemberUpdatedLive(t *testing.T) { /* space.member_updated junho role admin → row shows "관리자" */ }
func TestBansTab(t *testing.T) { /* owner → GET /v1/spaces/<id>/bans; enter on row → Confirm → DELETE /v1/spaces/<id>/bans/@x */ }
func TestOverviewActions(t *testing.T) {
	// owner: "방 정보 바꾸기" form prefilled → PATCH {"name":"노바 개발방2"}; "방장 넘기기" picker (space members) → Confirm → POST /v1/spaces/<id>/transfer {"user":"@hana"}
	// owner: "방 삭제" → Confirm → DELETE /v1/spaces/<id> → top(h).Kind == RouteHome ; no "방 나가기"
	// member: "방 나가기" → Confirm → DELETE /v1/spaces/<id>/members/@me → RouteHome ; "방 신고" → ReportDialog
}
func TestSpaceSettingsHostileIntegrity(t *testing.T) { /* hostile space settings as owner × AllSizes × tabs → h.Check() */ }
```

`settingsAs(role)` 는 노바 개발방 `MyRole` 을 바꾼 시드로 `RouteSpaceSettings{ID: 노바}` 를 연다. 멤버 목록 응답은 시험 파일 데이터(`ListSpaceMembers` 형식).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run 'TestSpaceSettings|TestMember|TestBans|TestOverview' -v`
Expected: FAIL — `undefined: newSpaceSettings`.

- [ ] **Step 3: Implement `spacesettings.go`, `spacemembers.go`**

- 상단 `widgets.Tabs`(역할별 탭 집합), `←/→`·`h/l`·클릭 전환. `Titled` = `<방 이름> 설정`.
- 개요: 아이콘·이름·설명·`멤버 N명`·`내 역할`, 그 아래 동작 목록(역할별): `방 정보 바꾸기`(owner|admin, 폼 미리 채움, 변경된 칸만 `Opt` 로), `방장 넘기기`(owner), `방 나가기`(owner 아님), `방 삭제`(owner, `Danger`), `방 신고`(owner 아님).
- 멤버: `/` 로 여는 필터 칸(300ms 디바운스 `ListSpaceMembers{Q}`), 목록 `<점> 표시 이름 @handle · 방장|관리자`, 맨 아래 이어 받기. `memberRowActions`: owner → 남(관리자↔멤버 `관리자로 지정`/`관리자 해제`, `내보내기`, `차단(밴)` — 사유 폼 0..200); admin → member 행에만 `내보내기`/`차단(밴)`; 공통 → `DM 보내기`, `사용자 신고`, `차단`(사용자 차단). `space.member_*` 이벤트로 목록 갱신(`member_left` → 행 제거).
- 차단됨: `ListSpaceBans` 목록 `표시 이름 · 사유 · 차단한 사람`, `enter` → Confirm → `Unban`.
- 접근: 방 멤버가 아니게 되면(`Store.Spaces[id]==nil`) `볼 수 없는 방이에요`.

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run TestSpaceSettingsGolden -update`
Expected: `ok`. owner/member 탭 집합 차이, 역할 라벨이 오른쪽 정렬로 잘리지 않는지 확인.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run 'TestSpaceSettings|TestMember|TestBans|TestOverview' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/spacesettings.go internal/ui/chat/spacemembers.go internal/ui/chat/spacesettings_test.go internal/ui/chat/testdata/
git commit -m "feat(chat): space settings overview, member roles, kicks and bans"
```

---

### Task 20: 초대 관리와 채널 만들기(공개/비공개)

**Files:**
- Create: `internal/ui/chat/invites.go`, `internal/ui/chat/channelcreate.go`
- Modify: `internal/ui/chat/spacesettings.go` (`tabInvites`, `tabChannels` 내용)
- Test: `internal/ui/chat/invites_test.go`

**Interfaces:**
- Consumes: `(*api.Client).ListInvites(ctx, spaceID) ([]api.Invite, error)`, `CreateInvite`, `RevokeInvite(ctx, code)`, `CreateChannel(ctx, spaceID, api.CreateChannelRequest{Name, Topic, IsPrivate, Members})`, `createInviteCmd`/`directInviteCmd`/`newInviteCreated`(Task 11), `newForm`, `newUserPicker`, `channelPrefItems`(Task 17), `tea.SetClipboard`.
- Produces:
  - 오버레이 `chat.invite_create`(`func newInviteCreate(ctx *ui.Context, args string) ui.Overlay`, args = 방 ID), `chat.invite_direct`(`func newInviteDirect(ctx *ui.Context, args string) ui.Overlay`), `chat.channel_create`(`func newChannelCreate(ctx *ui.Context, args string) ui.Overlay`)
  - 팔레트 `invite.create`, `invite.direct`, `channel.create`
  - `var channelNameRe = regexp.MustCompile("^[a-z0-9가-힣_-]{1,32}$")`
  - `func canCreateInvite(ctx *ui.Context) bool`, `func canCreateChannel(ctx *ui.Context) bool`(= `canManageSpace(ctx, currentSpaceID(ctx))`), `func canDirectInvite(ctx *ui.Context) bool`(현재 방 있음) — 팔레트 `Available` 로 그대로 쓴다
  - `func inviteRow(ctx *ui.Context, inv api.Invite, width int) string` — `AB12CD34 · 3/10회 · 11월 12일 만료 · 민지`(무제한 `3회`, 만료 없음 `만료 없음`)

- [ ] **Step 1: Write the failing test**

```go
func TestInvitesTabGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), settingsAs("owner"), func(t *testing.T, h *uitest.Harness) { h.Press("l", "l"); h.Golden("invites") })
}
func TestInviteCreateChoices(t *testing.T) {
	cases := []struct{ expires, uses int; want string }{
		{0, 0, `{"expires_in_sec":604800}`},            // 7일(기본 선택), 무제한
		{1, 3, `{"max_uses":10,"expires_in_sec":86400}`},
		{4, 0, `{"expires_in_sec":null}`},               // 무기한
	}
	for _, c := range cases { /* invite.create; move choice cursors (expires: 7일,1일,1시간,30일,무기한 order index c.expires; uses: 무제한,1,5,10,25,100) ; enter → POST /v1/spaces/<id>/invites body JSON-equal c.want; overlay morphs into invite_created showing the code */ }
}
func TestInviteRevokeAndCopy(t *testing.T) { /* invites tab: "y" → clipboard "masil join <code>"; "m" → "폐기" → Confirm → DELETE /v1/invites/<code>; row removed */ }
func TestInviteDirect(t *testing.T) { /* invite.direct: candidates = friends not already space members; pick hana → POST /v1/spaces/<id>/direct-invites {"user":"@hana"}; 409 already_member → shown in picker */ }
func TestChannelCreatePublicPrivate(t *testing.T) {
	// name "Bad Name" → "영문 소문자·숫자·한글·_- 1~32자"
	// public: Type("배포알림"), enter → POST /v1/spaces/<id>/channels {"name":"배포알림"}; result observed, top(h) is it
	// private: toggle 비공개 → second step member picker → pick minji → body {"name":"비밀","is_private":true,"members":["@minji"]}; sidebar shows "🔒비밀"
	// 409 already_exists → api.Describe shown
}
func TestChannelsTab(t *testing.T) { /* owner channels tab lists channels with position; "n"/"＋ 새 채널" row opens chat.channel_create; enter on row opens channel prefs */ }
func TestInviteChannelDialogsGolden(t *testing.T) {
	for _, id := range []string{"chat.invite_create", "chat.invite_direct", "chat.channel_create"} {
		forEachSize(t, overlaySizes, devChannel, func(t *testing.T, h *uitest.Harness) { h.Send(ui.OpenOverlayMsg{ID: id, Args: spaceByName(t, h.Context().Store, "노바 개발방")}); h.Golden(strings.TrimPrefix(id, "chat.")) })
	}
}
func TestCreatePaletteAvailability(t *testing.T) { /* MyRole member: canCreateInvite(ctx)==false, canCreateChannel(ctx)==false, canDirectInvite(ctx)==true; owner: all true */ }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run 'TestInvites|TestInviteCreate|TestInviteRevoke|TestInviteDirect|TestChannelCreate|TestChannelsTab|TestInviteChannel|TestCreatePalette' -v`
Expected: FAIL — `undefined: newInviteCreate`.

- [ ] **Step 3: Implement `invites.go`, `channelcreate.go`, settings tabs**

- 초대 탭(owner|admin): 첫 줄 `＋ 새 초대 링크`, `＋ 친구 직접 초대`, 그다음 `ListInvites` 행(`inviteRow`). 키: `enter`(＋ 줄 실행 / 행 → `newInviteCreated`), `y` 복사, `m` 메뉴(`복사`, `폐기`).
- 초대 만들기 폼: `만료` 선택 `7일|1일|1시간|30일|무기한` → `ExpiresInSec` = `Some(604800|86400|3600|2592000)` / `Null[int]()`; `최대 사용` `무제한|1|5|10|25|100` → 무제한이면 생략, 아니면 `Some(n)`. 성공 → `newInviteCreated(inv)` 로 morph.
- 직접 초대: 선택기 `Max 1`, 후보 = `Store.Friends` 중 그 방 멤버가 아닌 사람(멤버 목록 캐시가 있으면 그것, 없으면 친구 전부), 제출 = `directInviteCmd`.
- 채널 만들기 폼: `이름`(정규식 검증 문구 `영문 소문자·숫자·한글·_- 1~32자`), `주제`(0..250), `비공개` 토글. 비공개면 제출 시 선택기(`Max 50`, 후보 = 방 멤버, `Submit` `만들기`, 아무도 안 골라도 됨)로 morph. 요청 필드는 omitempty(빈 주제·false 비공개·빈 멤버는 생략). 성공 → `ObserveChannel` + `Navigate(RouteChannel)`.
- 채널 탭(owner|admin): `＋ 새 채널` + `SpaceChannels` 목록(`# 이름 · 주제`), `enter` = 그 채널 설정 메뉴(`channelPrefItems`).
- 팔레트 `Available`: `invite.create` = `canCreateInvite`, `channel.create` = `canCreateChannel`, `invite.direct` = `canDirectInvite`.

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run 'TestInvitesTabGolden|TestInviteChannelDialogsGolden' -update`
Expected: `ok`. 선택 칸의 `‹ 7일 ›` 표시와 토글 `[ ] 비공개` 가 20×6 강등에서도 읽히는지 확인.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run 'TestInvite|TestChannelCreate|TestChannelsTab|TestCreatePalette|TestSpaceSettings' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/invites.go internal/ui/chat/channelcreate.go internal/ui/chat/spacesettings.go internal/ui/chat/invites_test.go internal/ui/chat/testdata/
git commit -m "feat(chat): invite management and public/private channel creation"
```

---

### Task 21: 검색 화면 (`RouteSearch`)

**Files:**
- Create: `internal/ui/chat/search.go`
- Modify: `internal/ui/chat/channel.go` (`channel.search` `/` → `RouteSearch{ID: ch}`)
- Test: `internal/ui/chat/search_test.go`

**Interfaces:**
- Consumes: `ui.RegisterRoute(ui.RouteSearch, …)`, `(*api.Client).SearchMessages(ctx, api.MessageSearchQuery{Q, ChannelID, SpaceID, Author, PageOpts}) (*api.Page[api.MessageSearchHit], error)`, `api.MessageSearchHit{Message, Channel api.ChannelRef, Highlights []api.SearchHighlight{Start, Length}}`, `api.IsCode(err, "limit_exceeded")`, `textinput`, `widgets.List`, `humanize.MessageTime`.
- Produces:
  - `type searchView struct{ scope searchScope; ... }`, `func newSearch(ctx *ui.Context, r ui.Route) ui.Component` — `KeyHandler`, `MouseHandler`, `InputCapturer`, `Cursorer`, `Titled`, `Helper`, `Persistent`
  - `type searchScope int` (`scopeChannel`, `scopeSpace`, `scopeAll`)
  - `func highlightSnippet(t *theme.Theme, body string, hs []api.SearchHighlight, width int) string` — 코드 포인트 오프셋, 첫 강조가 보이게 앞을 `…` 로 자름, 강조는 `S.Selected`
  - 팔레트 `search.messages`(args = 검색어 → `RouteSearch{Arg: args}`)

- [ ] **Step 1: Write the failing test**

```go
func TestHighlightSnippet(t *testing.T) {
	th := theme.Get(theme.Amber)
	got := ansi.Strip(highlightSnippet(th, "오늘 배포 몇 시에 해요?", []api.SearchHighlight{{Start: 3, Length: 2}}, 30))
	if got != "오늘 배포 몇 시에 해요?" { t.Fatalf("%q", got) }
	long := strings.Repeat("가", 40) + "배포" + strings.Repeat("나", 40)
	got = ansi.Strip(highlightSnippet(th, long, []api.SearchHighlight{{Start: 40, Length: 2}}, 20))
	if !strings.HasPrefix(got, "…") || !strings.Contains(got, "배포") || textwidth.Width(got) > 20 { t.Fatalf("%q", got) }
	// newline in body becomes a space; out-of-range highlight ignored without panic
}
func TestSearchGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), openSearchWithResults, func(t *testing.T, h *uitest.Harness) { mustContain(t, h.Screen(), "배포"); h.Golden("search") })
	forEachSize(t, uitest.Required, openSearchEmpty, func(t *testing.T, h *uitest.Harness) { mustContain(t, h.Screen(), "검색 결과가 없어요"); h.Golden("search_empty") })
}
func TestSearchFromChannel(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40}); h.Press("/")
	if top(h).Kind != ui.RouteSearch || top(h).ID != devID(t, h) { t.Fatal("scoped to channel") }
	h.Type("배포"); h.Press("enter")
	r := reqs(h.API, "GET", "/v1/search/messages"); q, _ := url.ParseQuery(r[0].Query)
	if q.Get("q") != "배포" || q.Get("channel_id") != devID(t, h) { t.Fatalf("%v", q) }
}
func TestSearchScopeSwitch(t *testing.T) { /* results focus, "l" → scope 이 방 → request with space_id; again → 전체 → neither param */ }
func TestSearchOpenResult(t *testing.T) {
	// enter on top-level hit → top(h) == Route{Kind: RouteChannel, ID: ch, Anchor: msgID}
	// enter on thread reply hit (ThreadRootID set) → top(h)/thread route == Route{Kind: RouteThread, ID: root, Arg: ch, Anchor: replyID} (threadView selects that reply — Task 13 TestThreadAnchorJump)
}
func TestSearchPagination(t *testing.T) { /* next_cursor → at bottom "G" + "j" requests cursor=<c> and appends */ }
func TestSearchScopeTooWide(t *testing.T) { /* 422 limit_exceeded search_scope → "검색 범위가 너무 넓어요 — 방이나 채널을 골라 주세요" */ }
func TestSearchQueryLimits(t *testing.T) { /* empty query + enter → no request; input CharLimit 100 */ }
func TestSearchHostileIntegrity(t *testing.T) { /* hits from fixtures.HostileMessages × AllSizes → h.Check() */ }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run 'TestHighlightSnippet|TestSearch' -v`
Expected: FAIL — `undefined: highlightSnippet`.

- [ ] **Step 3: Implement `search.go`**

- 구성: 검색 칸(textinput, `CharLimit 100`, 처음 포커스) + 범위 칩 줄 `이 채널 · 이 방 · 전체`(경로 `ID` 가 채널이면 `scopeChannel`, 방이면 `scopeSpace`, 빈 값이면 `scopeAll`; 채널 범위가 불가능하면 칩 숨김) + 결과 목록. 경로 `Arg` 가 있으면 바로 검색.
- `enter`(입력 중) → `SearchMessages{Q, ChannelID|SpaceID, PageOpts{Limit: 20}}`(태그 `chat.search:<gen>`), 결과 목록으로 포커스. 결과 목록에서 `i` 또는 `/` = 검색 칸으로, `h/l`(←/→) = 범위 바꾸고 다시 검색, 칩 클릭도 같음, `enter` = 열기(최상위 메시지 → `RouteChannel{ID: ch, Anchor: id}`, 스레드 답글(`ThreadRootID != nil`) → `RouteThread{ID: *ThreadRootID, Arg: ch, Anchor: id}` — 스레드 화면이 그 답글을 찾아 선택한다), 맨 아래 도달 + `next_cursor` = 이어 받기.
- 결과 행(2줄, 1줄 밀도에서는 1줄): `#개발 · 노바 개발방 · 민지 · <MessageTime>` / `highlightSnippet`. 차단 사용자 결과는 서버가 빼므로 그대로 그린다.
- 오류: `limit_exceeded`(`details.what=="search_scope"`) → 위 문구, 그 외 `api.Describe`. 비었으면 `widgets.Empty(…, "🔍", "검색 결과가 없어요", "다른 낱말로 찾아 보세요")`.
- `Titled` = `검색 · <범위>`.

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run TestSearchGolden -update`
Expected: `ok`. 강조 구간이 `Styled()` 에서 selbg 배경인지(골든은 평문) 120×40 결과 2줄 형식을 확인.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -run 'TestHighlight|TestSearch|TestChannel' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/search.go internal/ui/chat/channel.go internal/ui/chat/search_test.go internal/ui/chat/testdata/TestSearch*
git commit -m "feat(chat): message search with scopes, highlights, jump to message"
```

---

### Task 22: 고정 메시지 오버레이와 팔레트 명령 전수 확인

**Files:**
- Create: `internal/ui/chat/pins.go`, `internal/ui/chat/palette.go`
- Test: `internal/ui/chat/pins_test.go`, `internal/ui/chat/palette_test.go`

**Interfaces:**
- Consumes: `(*api.Client).ListPins(ctx, ch) ([]api.Message, error)`, `chatstore.Pins/SetPins`, `pinCmd`(Task 12), `palette.Register`, `palette.Command{ID, Title, Keywords, Group, KeyHint, Available, Run}`.
- Produces:
  - 오버레이 `chat.pins`(`func newPins(ctx *ui.Context, args string) ui.Overlay`, args = 채널 ID), 팔레트 `pins.show`
  - `palette.go`: `var chatCommandIDs = []string{"space.create", "space.join", "space.leave", "space.settings", "channel.create", "channel.prefs", "dm.new", "friends", "friend.add", "invite.create", "invite.direct", "invites.inbox", "search.messages", "pins.show"}`(CA 5.6 chat 행과 동일). 명령 등록 자체는 각 Task 파일의 `init()` 에 그대로 둔다.

- [ ] **Step 1: Write the failing test**

```go
func TestPinsGolden(t *testing.T) {
	forEachSize(t, overlaySizes, devChannel, func(t *testing.T, h *uitest.Harness) {
		// exact path on a fresh per-size server; neither RegisterAPI nor chatRoutes registers GET …/pins, so no conflict
		h.API.JSON("GET /v1/channels/"+devID(t, h)+"/pins", 200, map[string]any{"items": []api.Message{fixtureMsg(t, h, "오늘 배포 몇 시에 해요?")}})
		h.Send(ui.RunCommandMsg{ID: "pins.show"}); h.Golden("pins")
	})
}
func TestPinsJumpAndUnpin(t *testing.T) {
	// enter → overlay closed, Navigate RouteChannel Anchor=<id> (RouteUpdatedMsg on same instance → around= request)
	// owner "p" → DELETE /v1/messages/<id>/pin, row removed; member "p" → toast "고정할 권한이 없어요"
	// empty → "고정된 메시지가 없어요"
}
func TestChatPaletteCommandsRegistered(t *testing.T) {
	for _, id := range chatCommandIDs {
		func() {
			defer func() { if recover() == nil { t.Errorf("%s not registered by ui/chat", id) } }()
			palette.Register(palette.Command{ID: id}) // duplicate id must panic
		}()
	}
}
func TestChatOverlaysRegistered(t *testing.T) {
	for _, id := range []string{"chat.reaction_users", "chat.message_actions", "chat.space_add", "chat.space_switch", "chat.space_create", "chat.space_join", "chat.dm_new", "chat.channel_prefs", "chat.channel_create", "chat.invite_create", "chat.invite_created", "chat.invite_direct", "chat.pins", "chat.friend_add"} {
		h := devChannel(t, uitest.Size{W: 120, H: 40})
		h.Send(ui.OpenOverlayMsg{ID: id, Args: argsFor(t, h, id)})
		if len(h.Model.Frame().Overlays) != 1 { t.Errorf("%s did not open", id) }
	}
}
func TestPaletteRunTargets(t *testing.T) {
	// friends → RouteFriends; invites.inbox → Route{RouteInvite, Arg:"direct"}; search.messages "배포" → Route{RouteSearch, Arg:"배포"}; space.settings → RouteSpaceSettings{ID: 노바}; space.leave → Confirm overlay
}
```

`argsFor` 는 id 별 인자(채널 ID·`"<msg> 👍"`·방 ID·초대 코드)를 돌려주는 시험 헬퍼. `chat.friend_add`·`chat.message_actions` 등 앞 Task 에서 `RunCommand` 로만 열던 것도 등록 id 로 열리는지 여기서 확인한다(없으면 해당 Task 파일에 `ui.RegisterOverlay` 를 추가).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/chat/ -run 'TestPins|TestChatPalette|TestChatOverlays|TestPaletteRun' -v`
Expected: FAIL — `undefined: newPins`, `undefined: chatCommandIDs`.

- [ ] **Step 3: Implement `pins.go`, `palette.go`**

- 고정 목록: `chatstore.Pins` 가 `loaded` 면 즉시, 아니면 `ListPins`(태그 `chat.pins:<ch>`) → `SetPins`. 행 = `renderRow`(Line 밀도) 1~2줄 + 고정 순서. `message.pinned/unpinned` 이벤트로 갱신. `enter` → 닫고 `ui.Navigate(RouteChannel{ID: ch, Anchor: id})`, `p` → `pinCmd(false)`(권한 검사), `esc` 닫기. 제목 `📌 고정 메시지 N`.
- `pins.go` 의 `init()` 에서 `pins.show`(Available = 보는 채널 있음, Run = `ui.OpenRegisteredOverlay("chat.pins", 채널 ID)`) 등록. 다른 명령은 각 Task 파일 `init()` 에 이미 있고, 이 Task 의 시험이 14개 전부를 확인한다.

- [ ] **Step 4: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run TestPinsGolden -update`
Expected: `ok`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/chat/ -v`
Expected: PASS (패키지 전체). Task 16~22 는 앞 골든의 주변을 바꾸지 않으므로 골든 불일치가 나오면 결함이다 — `-update` 하지 말고 원인을 고친다.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat/pins.go internal/ui/chat/palette.go internal/ui/chat/pins_test.go internal/ui/chat/palette_test.go internal/ui/chat/testdata/TestPinsGolden
git commit -m "feat(chat): pinned messages overlay and palette command coverage"
```

---

### Task 23: 모드 횡단 시험 — 모드별 골든·전환 보존·무작위 크기·적대적 스윕

**Files:**
- Test: `internal/ui/chat/modes_test.go`
- Modify(Step 5 판정 결과에 따라 시험 하나 추가): `internal/ui/chat/complete_test.go`

**Interfaces:**
- Consumes: 앞선 모든 Task 의 화면, `uitest.ModeSizes`, `layout.Auto`, `layout.DefaultBreakpoints()`, `h.Model.Frame()`, `h.Model.Focus()`, `h.Instance`, `h.Press("ctrl+w", "1")`(고정), `Store.Viewing`(core Task 30 — 매 Update 끝 `SetViewing`), `fixtureMsg`(Task 12 헬퍼), CR-1 판정용 `internal/ui/*.go` 의 `Completing() bool` 유무.
- Produces: 없음(시험만).

- [ ] **Step 1: Write the tests**

```go
func TestChatModeGoldens(t *testing.T) {
	// one golden per layout mode at the FIRST ModeSizes entry, three states each: channel, channel+thread, home
	for _, mode := range []layout.Mode{layout.ModeCommand, layout.ModeStandard, layout.ModeCompact, layout.ModeWidget, layout.ModeTiny, layout.ModeTooSmall} {
		sz := uitest.ModeSizes[mode][0]
		t.Run(mode.ID(), func(t *testing.T) {
			h := devChannel(t, sz)
			if h.Context().Mode != mode { t.Fatalf("size %v resolved to %v", sz, h.Context().Mode) }
			h.Golden("channel")
			if mode != layout.ModeTooSmall { selectText(t, h, "오늘 배포"); h.Press("t"); h.Golden("thread"); h.Press("esc") }
			h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteHome}}); h.Golden("home")
		})
	}
}
func TestChatModeSwitchPreserves(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	h.Press("i"); h.Type("배포 몇 시"); h.Press("esc")
	h.Press("k", "k", "k", "k", "k"); sel := selectedID(t, h); inst := channelOf(t, h)
	for _, sz := range []uitest.Size{{200, 60}, {120, 40}, {120, 30}, {80, 24}, {50, 20}, {200, 60}} {
		h.Resize(sz.W, sz.H)
		if channelOf(t, h) != inst || inst.composer.c.Value() != "배포 몇 시" || selectedID(t, h) != sel { t.Fatalf("%v lost state", sz) }
		// visibility via the list itself (row ids per line), plus a short ellipsis-free text prefix (plainOf = first 2 runes of the untruncated Plain body)
		if !rowVisible(t, h, sel) || !strings.Contains(h.Screen(), plainOf(t, h, sel)) { t.Fatalf("%v selected message not visible", sz) }
		h.Check()
	}
	h.Resize(120, 40); h.Press("ctrl+w", "1") // pin command: too small → fallback notice
	mustContain(t, h.Screen(), "창이 작아")
}
func TestChatModeSwitchMarksRead(t *testing.T) { // CR-2 outcome (Task 8 two-phase read): no key press needed after a mode switch
	h := devChannel(t, uitest.Size{W: 120, H: 30}) // compact: Main only
	ch := devID(t, h); put := func() []apitest.Recorded { return reqs(h.API, "PUT", "/v1/channels/"+ch+"/read") }
	h.Press("G"); h.Advance(time.Second); n0 := len(put())
	root := fixtureMsg(t, h, "오늘 배포 몇 시에 해요?").ID
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteThread, ID: root, Arg: ch}}) // compact: thread replaces Main, channel instance cached but hidden
	if h.Context().Store.Viewing(ch) { t.Fatal("channel is hidden behind the thread in compact") }
	m := mkMsg(ch, 950, userBy(t, h.Context().Store, "minji"), "가려진 동안 온 글", h.Context().Now())
	h.Event("message.created", realtime.MessageEvent{Message: m})
	h.Advance(2 * time.Second)
	if len(put()) != n0 { t.Fatal("hidden channel must not be marked read") }
	h.Resize(200, 60) // command: channel back in Main, thread in SlotThread; ModeChangedMsg arms, SetViewing runs at the end of this Update
	if !h.Context().Store.Viewing(ch) { t.Fatal("core sets Viewing at the end of the resize Update") }
	h.Advance(time.Second) // no key press
	r := put()
	if len(r) != n0+1 || jsonField(r[len(r)-1].Body, "message_id") != m.ID { t.Fatalf("want exactly one read of %s, got %d", m.ID, len(r)-n0) }
	if h.Context().Store.Channels[ch].My.UnreadCount != 0 { t.Fatal("local badge cleared") }
}
func TestChatRandomResize(t *testing.T) {
	for _, seed := range []int64{1, 2, 3} {
		rng := rand.New(rand.NewPCG(uint64(seed), 0))
		h := devChannel(t, uitest.Size{W: 200, H: 60}); h.Press("i"); h.Type("초안"); h.Press("esc")
		selectText(t, h, "오늘 배포"); h.Press("t"); h.Press("i"); h.Type("답글 초안"); h.Press("esc")
		for i := 0; i < 200; i++ { h.Resize(1+rng.IntN(330), 1+rng.IntN(100)); h.Check() }
		h.Resize(200, 60)
		// channel draft "초안", thread draft "답글 초안", same instances
	}
}
func TestChatHostileSweep(t *testing.T) {
	routes := hostileRoutes(t) // channel, thread(9,999 replies), search hits, invite preview, space settings, friends, home
	for name, r := range routes {
		forEachSize(t, uitest.AllSizes(), func(t *testing.T, sz uitest.Size) *uitest.Harness { return hostileRoute(t, sz, r) },
			func(t *testing.T, h *uitest.Harness) { h.Check(); _ = name })
	}
}
func TestChatSmallWidthSweep(t *testing.T) {
	if testing.Short() { t.Skip("full sweep") }
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	for w := 1; w <= 260; w += 1 { for _, ht := range []int{6, 15, 16, 23, 24, 39, 40, 49, 50, 60} { h.Resize(w, ht); h.Check() } }
}
```

- [ ] **Step 2: Run tests to see what fails**

Run: `go test ./internal/ui/chat/ -run 'TestChatMode|TestChatRandom|TestChatHostile|TestChatSmallWidth' -v`
Expected: `TestChatModeGoldens` FAIL(골든 없음). `TestChatModeSwitchMarksRead` 는 Task 8 의 두 단계 읽음 판정이 맞으면 PASS — FAIL 이면 Task 8 `channel.go` 의 결함(무장 단계가 `Store.Viewing` 을 보거나 `ModeChangedMsg` 가 트리거에서 빠짐)이다. 나머지 FAIL 이 있으면 그 화면의 결함이다 — 해당 Task 파일을 고쳐(시험을 약하게 하지 않는다) 다시 돌린다.

- [ ] **Step 3: Create goldens and review them**

Run: `go test ./internal/ui/chat/ -run TestChatModeGoldens -update`
Expected: `ok`. 모드별 `channel`/`thread`/`home` 골든이 설계서 4장 배치(지휘소 5열, 표준 2열+아래 스레드, 컴팩트 탭+한 줄형, 위젯 3탭+말풍선, 초소형 축약, TooSmall 안내 한 줄)와 같은지 확인.

- [ ] **Step 4: Final golden consistency check (no blanket update)**

Run: `go test ./internal/ui/chat/ -run 'Golden' -count=1`
Expected: PASS. Task 9·14·15 의 갱신 단계를 거쳤으면 이 Task 까지 앞 골든을 낡게 하는 변경은 없다. 그래도 실패하는 골든이 있으면: (1) 실패 목록을 적고, (2) 각 diff 가 **뒤 Task 가 바꾼 주변**(Main·입력창·멤버 창·홈 배경)만인지 연다 — 그렇다면 그 시험 이름만 `go test ./internal/ui/chat/ -run '<실패한 시험들을 | 로>' -update` 하고 `git diff --stat internal/ui/chat/testdata` 로 그 시험들의 파일만 바뀌었는지 확인, (3) 창·오버레이 **안쪽**이 바뀌었다면 결함이므로 갱신하지 말고 해당 Task 파일을 고친다. 패키지 전체 `-update` 는 하지 않는다.

- [ ] **Step 5: CR-1 판정 — `tab` 수락 시험을 둘 중 하나로 닫는다**

Run: `grep -nE 'Completing\(\) bool' internal/ui/*.go` (core `ui` 패키지 파일만 — `internal/ui/chat` 의 chat 구현은 대상이 아니다)
- **찾음(core 가 `ui.Completer` 를 추가 — CR-1 반영)**: Task 10 Step 3 에 적힌 `TestCompletionTabAccept` 를 `internal/ui/chat/complete_test.go` 에 그대로 추가한다. Run: `go test ./internal/ui/chat/ -run 'TestCompletionTabAccept' -v` · Expected: PASS(`tab` 이 팝업을 수락하고, 팝업이 닫힌 뒤의 `tab` 은 다시 하드 전역).
- **못 찾음(plan 03 현행 — CR-1 미반영)**: 아래 시험을 `complete_test.go` 에 추가한다. 현재 core 의미(팝업이 떠 있어도 `tab` 은 앱의 하드 전역)를 고정하되, 그 `tab` 이 chat 상태를 망가뜨리지 않음을 확인한다.

```go
func TestCompletionTabFallback(t *testing.T) { // CR-1 not in core: tab is a hard global even with the popup open (plan 03 Task 27)
	for _, sz := range []uitest.Size{{120, 40}, {80, 24}} { // multi-pane ring (focus.next) and single-pane (tab.next)
		h := devChannel(t, sz); inst := channelOf(t, h)
		h.Press("i"); h.Type("@mi")
		if !inst.Completing() { t.Fatalf("%v: popup open", sz) }
		h.Press("tab")
		if inst.composer.c.Value() != "@mi" { t.Fatalf("%v: tab must not accept or clear the draft, got %q", sz, inst.composer.c.Value()) }
		h.Check()
		h.Press("shift+tab") // back to the channel pane / tab
		if channelOf(t, h) != inst || inst.composer.c.Value() != "@mi" { t.Fatalf("%v: same instance, draft kept", sz) }
		h.Check()
	}
}
```

Run: `go test ./internal/ui/chat/ -run 'TestCompletionTabFallback' -v` · Expected: PASS. 그리고 Step 7 커밋 메시지 본문과 이 Task 의 완료 보고에 열린 항목으로 적는다: `CR-1 open: tab cannot accept autocomplete (CA 5.5 compose.complete unmet) — needs plan 03 Task 27 (ui.Completer)`. 이후 core 가 CR-1 을 반영하면 `TestCompletionTabFallback` 을 지우고 위 "찾음" 갈래를 실행한다(두 시험은 동시에 둘 수 없다 — 서로 반대를 단언한다).

**Task 23 완료 조건**: (1) Step 5 의 두 갈래 중 하나의 시험이 추가되어 통과, (2) `TestChatModeSwitchMarksRead` 와 Task 8 `TestChannelMarksRead`·`TestChannelReadOnlyWhileViewing` 통과(CR-2 철회의 근거 — 모드 전환 직후 읽음이 core 의 "매 Update 끝" `SetViewing` 만으로 동작), (3) 아래 Step 6 전체 통과.

- [ ] **Step 6: Run the whole client suite**

Run: `go vet ./internal/store/chatstore/ ./internal/ui/chat/ ./cmd/masil/ && go test ./internal/store/chatstore/ ./internal/ui/... -count=1`
Expected: vet 출력 없음, 모든 패키지 `ok`(core 의 타이머 정적 검사 시험이 `internal/ui/chat` 을 포함해 통과, Step 5 에서 추가한 `TestCompletionTabAccept` 또는 `TestCompletionTabFallback` 포함).

- [ ] **Step 7: Commit**

```bash
git add internal/ui/chat/modes_test.go internal/ui/chat/complete_test.go internal/ui/chat/testdata/
git commit -m "test(chat): per-mode goldens, mode-switch preservation and read, tab completion gate, sweeps"
```

CR-1 미반영 갈래였다면 커밋 메시지 본문에 Step 5 의 `CR-1 open: …` 한 줄을 넣는다.
