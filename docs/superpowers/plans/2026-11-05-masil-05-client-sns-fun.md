# masil 05 — Go 클라이언트 SNS·재미 UI 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** core 위에 sns-fun 계획 소유 패키지(`feedstore`·`imgrender`·`ui/feed`·`ui/fun`)를 만들어 피드·글·답글 트리·작성기·프로필·방명록·스토리·해시태그/트렌드·알림함·피드 투표·상태 메시지·접속 모드·집중 모드(요약)·신고/차단 동작을 TUI 에 올리고, 범위 지시서가 재배정한 운영자 CLI `masil admin …`(`internal/admincli`)을 만든다.

**Architecture:** 모든 화면은 `ui.RegisterRoute` 로 등록하는 경로 컴포넌트(포인터·제자리 변경, 경로 키로 캐시)이고, 지휘소의 피드 열·알림 창은 앱이 같은 경로 인스턴스를 `SlotFeed`·`SlotNotifications` 에 투영한다(PaneFactory 없음). 글·타임라인·스토리·알림 목록의 진실은 리듀서 `feedstore`(이름 `"feed"`)가 갖고, 화면은 ID 순서만 들고 `FeedStore.Post(id)` 로 그린다. 이미지는 `imgrender` 가 하프블록(기본)·kitty 자리표시자·iTerm2(전체 화면 보기만)로 그린다.

**Tech Stack:** Go 1.26.1 · `charm.land/bubbletea/v2` v2.1.1 · `charm.land/lipgloss/v2` v2.0.6 · `charm.land/bubbles/v2` v2.2.1 · `github.com/charmbracelet/x/ansi` v0.11.9 (`ansi/kitty`, `ansi/iterm2`) · `golang.org/x/image` v0.47.0 (`draw`, `webp`) · core 의 `uitest`/`apitest`/`fixtures`/`golden` 하네스.

**Spec:** `docs/superpowers/specs/2026-11-05-masil-design.md` (최우선). 계약: `docs/contract/client-architecture.md`(2.1 소유·5장 프레임워크·6.2 sns-fun·6.3 imgrender·10장 시험), `docs/contract/api.md`(2장 스키마, 3.2~3.4, 5.1·5.2·5.4·5.5·5.14~5.21), `docs/contract/events.md`(4.6~4.8, 5.3). 공통 브리프: `docs/superpowers/plans/00-brief.md`. 시안: `.superpowers/brainstorm/35477-1793846636/content/layouts.html`(피드 열·알림 창), `visual-style.html`.

## Global Constraints

- 소유 파일만 만든다/고친다: `cmd/masil/register_sns.go`, `internal/store/feedstore/**`, `internal/imgrender/**`, `internal/ui/feed/**`, `internal/ui/fun/**`(각 `testdata/` 포함). **범위 지시서가 추가로 배정한 파일**: 계약 2.1 에서 daemon-release 소유인 `cmd/masil/cmd_admin.go`(+ `cmd/masil/cmd_admin_test.go`)와 `internal/admincli/**`(+ `testdata/`) — 06 계획(daemon-release)은 Global Constraints 에서 "범위 지시서에 없으므로 만들지 않는다" 고 적었으므로, 06 이 자기 재배정 파일(`README.md` 등)을 기록한 방식 그대로 이 계획이 맡는다(Task 20). 오너가 이 재배정을 원치 않으면 Task 20 을 실행하기 전에 여쭙고, 어느 계획에도 배정되지 않은 채로 두지 않는다. **단 계약(CA 2.1 트리·7장 경계 요약표·9.2)은 아직 이 두 경로를 daemon-release 소유로 적고 있고, CA 0장은 "자기 계획이 소유하지 않은 파일은 절대 만들지 않는다"·"바꾸려면 문서를 먼저 고친다(오너 승인)" 고 한다 — 그래서 Task 20 은 아래 CR-S3(오너 승인을 받은 CA 소유 표 갱신)이 끝난 뒤에만 시작한다.** `go.mod`/`go.sum`·core 패키지·`internal/ui/uitest/fixtures`·core `cmd/masil/main.go`·`cmd_core.go`·`docs/contract/*` 는 고치지 않는다.
- 실행 전제: core Phase 0 스텁(계약 2.3)이 있어야 한다. 이 계획 코드는 그 전에 커밋하지 않는다. **Phase 0 스텁만으로는 어떤 Task 의 시험도 통과하지 않는다**(스텁은 0값을 돌려주므로 nil 창·빈 디코드·빈 하네스에서 실패하거나 패닉한다) — Task 별로 실제 구현이 끝나야 하는 core Task 는 아래 "선행 조건" 표를 따르고, 선행이 안 끝났으면 **시험을 고쳐 통과시키지 말고 기다린다**.
- **범위 경계(계약 2.1·2.4·5.8·6.1)**: 운영자 기능(설계서 2장 "전역 운영자 역할" — 신고 목록·게시물/이미지 강제 삭제·계정 정지)은 TUI 숨김 화면이 아니라 `masil admin …` CLI 로만 만든다(Task 20, 위 재배정). 채팅 안 `/poll` 생성·메시지 투표는 chat 소유, 막대 렌더는 core `widgets.PollView`. 차단 **해제** 목록은 core 설정. 신고 UI 는 core `widgets.ReportDialog`, 차단은 `widgets.Confirm` 후 `api.Block`.
- import 규칙: `ui/feed`·`ui/fun` → `ui`, `ui/widgets`, `ui/palette`, `ui/keymap`, `store`, `store/feedstore`, `api`, `markup`, `theme`, `textwidth`, `humanize`, `slash`, `imgrender`, `ids`, `layout`, `termcaps`(계약 2.2 — `realtime` 은 import 하지 않는다: 방송된 `ui.EventMsg` 는 `msg.Event.Type` 필드로만 구분). `ui/chat` import 금지, `ui/feed`↔`ui/fun` 상호 import 금지(오버레이는 `ui.OpenRegisteredOverlay`, 결과는 `ui.ResultMsg` 태그로 주고받는다). `admincli` → `api`, `textwidth`, `humanize` 만(UI 패키지 금지); `cmd/masil/cmd_admin.go` → `admincli`, `api`, `auth`, `config`.
- 모듈은 계약 1.2 목록의 하위 패키지만. 새 모듈 금지 — 시험도 표준 `testing` + `x/exp/golden` 만 쓴다(testify 없음; 아래 시험 스케치의 단언은 `if … { t.Fatalf(…) }` 로 옮겨 쓴다).
- 시간 지연은 `ui.After(ctx, d, msg)` 만. `tea.Tick`·`tea.Every`·`tea.Sequence`·`time.Sleep`·`time.After`·`time.NewTimer`·`spinner.Model.Tick` 금지(core 정적 검사가 이 패키지도 훑는다). 시각은 `ctx.Now()` 만.
- 모든 외부 문자열은 그리기 직전 `textwidth.Sanitize`; `View` 는 `textwidth.Fit` 으로 정확히 `size.W × size.H`; 모든 칸은 `ctx.Theme.S.*` 로 감싼다.
- 텍스트 입력은 `widgets.Composer` 또는 `textinput`(`SetVirtualCursor(false)`, `KeyMap.DeleteAfterCursor` 에서 `ctrl+k` 제거, 입력·붙여넣기는 `textwidth.SanitizeInput` 후). 입력 칸이 있는 컴포넌트는 `ui.InputCapturer`·`ui.Cursorer` 구현.
- 길이(코드 포인트, `utf8.RuneCountInString`): 글 0..500(미디어·투표·인용 없으면 1..500), 미디어 0..4장·장당 5,242,880B, 스토리 0..200, 방명록 1..300, `status.text` 0..64 + 이모지 1개, 투표 질문 1..200·선택지 2..10·각 1..80·`duration_sec` 60..604800, 집중 60..43200초, bio 0..160, display_name 1..32.
- `ui.Do` 태그는 `feed.<동작>:<대상ID>`(ui/feed)·`fun.<동작>:<대상ID>`(ui/fun), 오버레이 id 는 `feed.`·`fun.` 접두사.
- 팔레트 명령 id 는 계약 5.6 표 그대로: ui/feed = `post.new, story.new, feed.following, feed.discover, trends, notifications, profile.me, profile.edit, follow_requests`; ui/fun = `status.set, status.clear, presence.set, focus.start, focus.stop`.
- 집중 요약 오버레이는 `ui.OverlayFocusSummary`(`"fun.focus_summary"`)로 등록만 한다. `focus.stop`·`focus.ended` 는 `Store.FocusEnded` 로 **상태만** 바꾸고 오버레이를 열지 않는다.
- 골든: 화면마다 `uitest.AllSizes()` 전부, 오버레이는 `{Required…, 44×16, 20×6}`(+필요 시 19×10), 테마 D·하프블록·애니메이션 끔·시계 `2026-11-05T02:05:00Z`(서울 11:05). 화면마다 `fixtures.Hostile` 무결성 전용 시험(`h.Check()`) × `AllSizes`. 생성은 `-update`, 갱신 후 사람이 열어 시안과 대조.
- UI 문구는 한국어(시안 문구 그대로: 피드 열 탭 `피드 / 팔로잉 / #트렌드`, 카드 `♥ 12  ↻ 3  💬 4`, 알림 창 `♥ hana님이 좋아함`). 코드·주석·커밋 메시지는 영어. 비밀 하드코딩 금지(시험 토큰은 apitest 의 `msl_test` 만).

## 선행 조건 (Task 별 core 선행 Task)

core Phase 0(core 계획 `2026-11-05-masil-03-client-core.md` Task 1–2, CA 2.3)은 공개 시그니처를 **0값을 돌려주는 스텁**으로만 만든다 — 레지스트리(`ui.Register*`, `palette.Register`, `store.RegisterReducer`, `cmd/masil` 의 `register`)만 실제로 동작한다. 그래서 `store.NewWindow` 는 nil, `realtime.Event.Decode` 는 `nil, nil`, `apitest.New`·`uitest.New` 는 0값을 돌려주고, 이 계획의 시험은 해당 core Task 가 **실제 구현까지 끝나야** 통과한다. 아래 번호는 core 계획의 Task 번호다(core 계획 "실행 순서": Task 19 부터는 3–18 이 모두 끝난 뒤 번호 순이므로 "core N 완료" 는 N 이하 전부 완료를 뜻한다 — N ≥ 19 일 때).

| sns-fun Task | 실제 구현이 끝나야 하는 core Task | 이 계획이 기대는 것 |
|---|---|---|
| 1 | 2, 9, 12, 14 | `store.{New, RegisterReducer, NewWindow}`·`Window[T]` 메서드·`IsBlocked`(14), `realtime.Event.Decode` 와 이벤트 구조체(12), `api` 타입(9) |
| 2 | Task 1 의 것 + 8, 10, 11 | `apitest`(8), `FollowingTimeline`·`DiscoverTimeline`·`GetPost`·`ListReplies`·`StoryFeed`·`ListNotifications` 의 실제 요청(10·11), `store.Resyncer`·`OverlapAnchor`(14) |
| 3 | 2, 7 | `termcaps.Caps` 타입·상수(7). 디코드·하프블록 자체는 core 의존이 거의 없다 |
| 4 | 4, 7, 8, 11 | `config.{Paths, WriteFileAtomic}`(4), `termcaps`(7), `apitest`(8), `FetchMedia`·`MediaURL`(11) |
| 5 | 6, 7, 16, 17, 19, 21, 23 (+ sns Task 1) | `textwidth`(6), `humanize`(7), `theme`(16), `markup`(17), `ui.Density`·`Context`(19), `widgets.{PollView, UserName}`(21), `fixtures`(23, `snsfix_test.go` 의 `seeded()`) |
| 6–15, 18 | core 30 완료(= 3–30) | `uitest` 하네스(20·25), `widgets.{List, Composer}`(22), `fixtures`(23), `app` 투영·셸·토스트·포커스·키·마우스(24–27), 오버레이 틀·`ReportDialog`·`Confirm`·`EmojiPicker`(28), 팔레트(29), `ui.EventMsg` 방송·부트스트랩 폴백·watch(30) |
| 16 | core 30 완료 + **CR-S1** | `h.Send(ui.RunCommandMsg{…})` 를 앱이 등록 명령으로 실행해야 `TestStatusSetArgs`·`TestStatusSetTextOnly`·`TestStatusSetTooLong`·`TestStatusClear`·`TestPresenceSet` 이 통과 |
| 17 | core 31 완료 + **CR-S1** | `alert{focus_summary}` → `ui.OverlayFocusSummary` 열기·같은 id 1회(31), `RunCommandMsg{focus.start/stop}` |
| 19 | core 31 완료 + **CR-S1** | `TestPaletteCommandsRun`(RunCommandMsg 디스패치), 팔레트 검색(29), 타이머 정적 검사(19), 모드 전환 투영(24–27) |
| 20 | 2, 4, 5, 6, 7, 8, 9, 11 (+ 35 와 **CR-S2** 는 `TestAdminCmdUsageExitCode` 에만) + **CR-S3** | `api.Admin*` 메서드(11)·타입(9)·`apitest`(8), `textwidth`(6), `humanize.MessageTime`(7), `auth.{Load, ErrNoCredentials}`·`Credentials.Matches`(5), `config.{Defaults, Paths, ServerURL}`(4), `register`·`command`·`Env`(2 — Phase 0 에서 이미 실제 동작), `main` 의 명령 실행·종료 코드(35) |

규칙:
- 선행 core Task(와 CR)가 끝나지 않은 sns Task 는 시작하지 않는다. 시작했는데 시험이 스텁 때문에 실패·패닉하면 **시험을 고쳐 통과시키지 않는다** — `t.Skip`, 단언 완화, 스텁을 피해 가는 가짜 구현·자체 디스패처, core·fixtures·CA 파일 수정 모두 금지. 그 core Task 가 끝나길 기다리고, 그 Task 의 커밋도 시험이 통과한 뒤에만 한다.
- 선행이 모두 끝난 sns Task 끼리는 순서를 바꿔도 된다(예: core 14 가 먼저 끝나면 Task 1–2, core 7 만 끝났으면 Task 3). 단 sns Task 사이의 의존(Task 5 ← 1, Task 6 ← 1–5, Task 7–19 ← Task 6 의 `helpers_test.go`, Task 12 이후 ui/fun ← Task 12 의 `funAPI`)은 번호 순을 지킨다. Task 20 은 sns Task 1–19 와 코드 의존이 없으므로 위 행의 선행만 끝나면 언제 해도 된다.
- CR 이 core 에 들어오지 않아 막힌 시험은 지우거나 우회하지 않는다. 오케스트레이터에 그 CR 을 알리고, 그 Task 의 Step 4(통과 확인)·커밋을 CR 반영 뒤로 미룬다(다른 Task 는 계속).

## core·CA·서버 변경 요청 (이 계획은 core·CA·서버 파일을 고치지 않는다)

- **CR-S1 `ui.RunCommandMsg` 디스패치 (core `ui/app`, CA 5.3·5.6)**: CA 는 `ui.RunCommandMsg{ID, Args}`·`ui.RunCommand(id, args)` 를 정의하고 CA 4.9·6.4 는 chat 입력창의 `/status`·`/focus` 가 이 메시지로 sns-fun 이 등록한 팔레트 명령을 실행한다고 하지만, core 계획의 어느 Task 도 앱이 이 메시지를 받아 실행한다고 적지 않았다(core Task 29 는 팔레트 오버레이에서 `enter` 로 고른 명령만 실행). 요청: `app.Model.Update` 가 `ui.RunCommandMsg` 를 받으면 `palette.Register` 된 같은 `ID` 명령의 `Run(ctx, Args)` 가 돌려준 Cmd 를 반환한다. `Available` 은 보지 않는다(팔레트 목록 노출용이며, 실행 시 안내는 각 명령 `Run` 이 한다 — 예: `focus.stop` 이 집중 중이 아니면 `EndFocus` 의 409 → `집중 모드가 아니에요`). 모르는 ID 는 무시 + debug 로그. core 쪽 시험 예: `TestRunCommandMsgDispatches`. 이 계획은 같은 동작을 흉내 내는 자체 디스패처를 만들지 않는다.
- **CR-S2 package-main 사용법 오류 표지 (core `cmd/masil/main.go`, CA 2.4)**: CA 2.4 는 종료 코드 `0/1/2(usage)` 만 말하고, 등록 명령의 `Run` 이 "사용법 오류(→ 2)" 를 알리는 방법을 정하지 않았다(core Task 35 는 core 명령 안에서만 2 를 낸다). core `main.go` 는 `admincli` 를 import 할 수 없으므로(CA 2.2 — core 는 다른 계획 패키지에 기대지 않는다) `admincli.ErrUsage` 를 직접 알아볼 수도 없다. 요청: core `main.go` 에 package-main 비공개 표지(제안 이름 `errUsage = errors.New("usage")`)를 두고, `run` 이 명령 `Run` 의 오류가 `errors.Is(err, errUsage)` 면 종료 코드 2(명령이 이미 stderr 에 사용법을 썼으므로 추가 출력 없음), 그 밖 non-nil 은 1. **이름은 core 가 정하고, 이 계획 Task 20 의 `cmd_admin.go` 가 그 이름에 맞춰 `admincli.ErrUsage` 를 그 표지로 바꿔 돌려준다**(daemon-release 의 `daemon` 사용법 오류도 같은 표지를 쓸 수 있다). CR-S2 반영 전에는 `cmd_admin.go` 가 표지를 참조하지 않고(없는 이름이라 컴파일되지 않음) `admincli` 의 오류를 그대로 돌려준다 — 사용법 오류도 종료 코드 1 이 되는 것이 그 사이의 알려진 한계다.
- **CR-S3 CA 소유 표 갱신 (오너 승인, Task 20 의 차단 조건)**: CA 2.1 트리(`cmd_admin.go daemon-release`, `admincli/ daemon-release`), 7장 경계 요약표(`데몬·자동 시작·운영자 CLI·릴리스 | daemon-release | daemon, autostart, admincli, 릴리스 파일`), 9.2(`internal/admincli`(daemon-release))를 sns-fun 소유로 고친다(`masil admin …` 명령 소유를 적은 CA 2.4 "명령 소유" 줄도 같이). CA 0장 3 에 따라 오너 승인 사항이고 고치는 쪽은 오케스트레이터/오너다 — 이 계획은 CA 를 고치지 않는다. 갱신되기 전에는 Task 20 을 시작하지 않는다(06 계획 Global Constraints 의 `grep -n 'admincli'` 확인과 짝).
- **서버 의존 SV-1 집중 조기 종료의 요약 alert (서버 plan 01 Task 12)**: 이 클라이언트는 요약 오버레이를 **`alert{kind:"focus_summary"}` 로만** 연다(Global Constraints — `focus.stop`·`focus.ended` 는 상태만). 그래서 Task 17 `TestFocusSummaryExactlyOnce` 는 조기 종료(`focus.stop` = `DELETE /v1/me/focus`) 뒤에도 서버가 `focus.ended` 와 `alert{focus_summary}` 를 보낸다고 가정한다. api.md 5.1 은 "자연 종료 시에도 … 보낸다" 고만 적어 조기 종료 쪽이 문장상 비어 있고, 서버 계획이 알람(자연) 종료일 때만 보내면 **실서버에서 조기 종료 요약이 뜨지 않는다**(상태줄 집중 표시는 꺼지고 `Store.LastFocusSummary` 는 채워지지만 오버레이 없음). 시험은 가짜 이벤트로 돌므로 서버와 무관하게 통과해 이 결함을 잡지 못한다. 2026-11-05 확인 시점의 plan 01 Task 12 는 `finishFocus(endedAt)` 하나로 두 경로 모두 `focus.ended` + `focus_summary` alert 를 보내고 시험(`early end also sends the summary alert`)도 있다 — 이 문장이 빠지거나 알람 경로 전용으로 되돌아가면 plan 01 을 고쳐야 하며(이 계획이 바꾸지 않는다), 고쳐지기 전까지는 위 한계가 실서버에 남는다. Task 19 뒤 로컬 `wrangler dev` 수동 확인: `/focus 1m` → `/focus stop` → 요약 오버레이 정확히 1번.

## Review Focus

1. 비공개 계정 글에 `b`(리포스트)·`q`(인용) → 서버 403 을 기다리지 않고 요청 없이 토스트 `비공개 계정의 글은 리포스트·인용할 수 없어요` (Task 7 `TestRepostQuotePrivateBlocked`).
2. `poll.updated`(`my_votes` 는 늘 `[]`)가 와도 내가 고른 표시가 지워지지 않는다 (Task 1 `TestPollUpdatedKeepsMyVotes`, Task 10 `TestPollUpdatedEventKeepsMine`).
3. `block.updated{blocked:true}` 직후(또는 내가 차단한 `PUT /v1/blocks` 가 204 를 돌려준 즉시 — 이벤트를 기다리지 않음) 그 사용자의 글·리포스트·답글·알림이 FeedStore 창뿐 아니라 화면별 ID 목록(해시태그·프로필 탭·`feed.quotes`·`feed.user_list`)에서도 즉시 사라진다 (Task 1 `TestBlockHidesAuthorEverywhere`, Task 7 `TestPostMenuBlockAuthor`, Task 14 `TestBlockHidesInPerScreenLists`).
6. 이미지 결과가 도착한 뒤 **다른 메시지가 하나도 오지 않아도** kitty `Setup`·iTerm2 이스케이프가 이미 `tea.Raw` 로 나갔다 (Task 6 `TestTimelineKittyRawWithoutFollowUp`, Task 18 `TestMediaITerm2EmitsRaw`).
7. 운영자가 `masil admin` 으로 신고 목록·처리·강제 삭제·정지/해제를 할 수 있다 (Task 20).
4. 작성기 초안·첨부가 창 크기 변경(모드 전환)과 다른 화면 다녀오기 후에도 그대로다 (Task 9 `TestComposeDraftSurvivesModesAndNavigation`).
5. 보고 있는 글이 `post.deleted` 로 지워지면 화면이 깨지지 않고 `삭제된 글이에요` 로 바뀌며 좋아요 등 동작이 요청을 보내지 않는다 (Task 8 `TestOpenPostDeletedEvent`).
8. `/focus stop`(조기 종료) 뒤에도 놓친 것 요약이 **정확히 한 번** 뜬다 — 클라이언트 시험(Task 17 `TestFocusSummaryExactlyOnce`)은 가짜 이벤트라 통과하지만, 실서버에서는 서버가 조기 종료에도 `alert{focus_summary}` 를 보내야만 성립한다(위 SV-1, plan 01 Task 12). 서버가 알람 종료일 때만 보내면 실서버에서 조기 종료 요약이 뜨지 않으므로, 리뷰어는 plan 01 Task 12 의 `finishFocus` 문장·시험이 남아 있는지 함께 본다.
9. 서버·core 가 만든 검색·보조 API 가 고아로 남지 않는다: 해시태그 검색(`SearchHashtags`)·글 검색(`SearchPosts`)은 `RouteTrends` 의 `/` 검색(Task 14), 팔레트 `#태그` 소스(Task 19), 남의 스토리 보기(`ListUserStories`)는 프로필 `s`(Task 11·13), 팔로워 삭제(`RemoveFollower`)는 내 `팔로워` 목록의 `x`(Task 7). 메시지 검색(`RouteSearch`, `ctrl+f`)은 chat 소유라 건드리지 않는다.

---

## 파일 지도

| 파일 | 책임 |
|---|---|
| `internal/store/feedstore/feedstore.go` | `FeedStore` 상태·조회/갱신 메서드, `Get`, 리듀서 등록 |
| `internal/store/feedstore/reducer.go` | 이벤트 → 상태 규칙(events.md 4.7·4.8, `block.updated`, `user.updated`) |
| `internal/store/feedstore/resync.go` | `Resyncer`(events.md 5.3 4단계), `ResyncMsg` |
| `internal/imgrender/decode.go` | `Decode`, `PrepareUpload`, 오류 값 |
| `internal/imgrender/halfblock.go` | `HalfBlock`, 맞춤(contain/cover) 축소 |
| `internal/imgrender/renderer.go` | `Renderer`(모드 선택·kitty 자리표시자·LRU), `ITerm2Inline` |
| `internal/imgrender/cache.go` | 디스크(200MB LRU)·메모리(32) 미디어 캐시 |
| `internal/ui/feed/card.go` | 글 카드 렌더(밀도 4종, 클릭 영역) |
| `internal/ui/feed/feedtabs.go` | 피드 표면 상단 탭 `피드 / 팔로잉 / #트렌드`, 스토리 링 |
| `internal/ui/feed/timeline.go` | `RouteFeed`(팔로잉/발견, 무한 스크롤, 새 글 띠, watch) |
| `internal/ui/feed/actions.go` | 글 선택 공통 키(좋아요·리포스트·인용·답글·작성자·해시태그·메뉴) |
| `internal/ui/feed/overlays.go` | `feed.post_actions`, `feed.user_list`, `feed.quotes`, `feed.hashtag_pick` |
| `internal/ui/feed/post.go` | `RoutePost`(조상 + 글 + 답글 트리) |
| `internal/ui/feed/compose.go`, `attach.go` | `RouteCompose`(글·스토리), `feed.attach` |
| `internal/ui/feed/poll.go` | 피드 투표(`feed.poll_vote`, 카드 클릭 투표) |
| `internal/ui/feed/profile.go`, `profile_overlays.go`, `guestbook_tab.go` | `RouteProfile`, `feed.follow_requests`, `feed.profile_edit`, 방명록 탭 |
| `internal/ui/feed/stories.go` | `RouteStories` 보기(피드 그룹 없으면 `ListUserStories` 임시 그룹) |
| `internal/ui/feed/hashtag.go`, `trends.go` | `RouteHashtag`, `RouteTrends`(트렌드 Top 10 + `/` 해시태그·글 검색) |
| `internal/ui/feed/notifications.go` | `RouteNotifications` |
| `internal/ui/feed/media.go` | `feed.media` 전체 화면 이미지 보기(kitty/iTerm2) |
| `internal/ui/feed/palette.go` | ui/feed 팔레트 명령 9개, 팔레트 `#태그` 소스(`hashtagSource`) |
| `internal/ui/fun/status.go`, `presence.go`, `focus.go`, `focus_summary.go`, `guestbook.go` | ui/fun 명령 5개·`fun.status`·`fun.presence`·`fun.guestbook_write`·`fun.focus_summary` |
| `cmd/masil/register_sns.go` | blank import |
| `internal/ui/feed/snsfix_test.go`, `helpers_test.go` | 시험용 데이터(공유 픽스처 전부 Task 5 에서 정의)·가짜 API·크기 스윕 도우미(ui/fun 은 같은 이름 파일을 자기 패키지에 둔다) |
| `internal/ui/feed/images.go` | 미디어 렌더러 공유, 이미지 요청·`tea.Raw` 송출(`imageSlots`) |
| `internal/admincli/admincli.go`, `table.go` | `masil admin` 하위 명령 8종, `textwidth` 정렬 표(범위 지시서 재배정, Task 20) |
| `cmd/masil/cmd_admin.go` | `admin` 명령 등록·자격 증명으로 `api.Client` 만들기(범위 지시서 재배정, Task 20) |

---

### Task 1: feedstore — 상태와 이벤트 리듀서

**Files:**
- Create: `internal/store/feedstore/feedstore.go`, `internal/store/feedstore/reducer.go`
- Test: `internal/store/feedstore/feedstore_test.go`

**Interfaces:**
- Consumes: `store.New`, `store.RegisterReducer(func(*store.Store) store.Reducer)`, `(*store.Store).Reducer(name)`, `store.Reducer{Name; Apply; Reset}`, `store.NewWindow[T]`, `(*store.Window[T]).{Upsert, Update, Remove, Replace, Items, Get, Len}`, `(*store.Store).IsBlocked`, `realtime.Event.Decode()` → `TimelineItemCreated, PostDeleted, PostStats, PostReplyCreated, PollUpdated, StoryCreated, StoryDeleted, NotificationCreated, NotificationsRead, BlockUpdated, UserUpdated`.
- Produces (계약 6.2 그대로): `func Get(s *store.Store) *FeedStore`, `Timeline(kind string) *store.Window[api.TimelineItem]`, `Post(id string) *api.Post`(캐시 안 포인터 — 낙관적 변경은 여기에 직접), `Notifications() *store.Window[api.Notification]`, `Stories() []api.StoryGroup`, `Trends() *api.Trends`, `NewDiscoverCount() int`.
- Produces (sns-fun 내부용 공개 메서드 — ui/feed·ui/fun 만 사용):

```go
const Name = "feed"
func (f *FeedStore) Name() string
func (f *FeedStore) Apply(s *store.Store, ev realtime.Event)
func (f *FeedStore) Reset()
func (f *FeedStore) ObservePosts(ps ...api.Post)                 // REST results only: upsert cache (+ quote.post); authoritative for EVERY field incl. viewer.liked/reposted and poll.my_votes (may lower them)
func (f *FeedStore) observeEventPost(p api.Post)                 // unexported, reducer only (timeline.item_created, post.reply_created): body/media/counts/author from the event, but keeps the cached viewer and poll.my_votes (events.md 4.4 default values)
func (f *FeedStore) HideAuthor(userID string)                    // same pruning the reducer runs on block.updated{blocked:true}; used right after PUT /v1/blocks succeeds
func (f *FeedStore) Replies(parentID string) *store.Window[api.Post] // created on first call; HasReplies reports existence
func (f *FeedStore) HasReplies(parentID string) bool
func (f *FeedStore) SetPoll(p api.Poll)                          // REST result: authoritative incl. my_votes
func (f *FeedStore) SetStories(gs []api.StoryGroup)
func (f *FeedStore) MarkStorySeen(storyID string)
func (f *FeedStore) PruneStories(now time.Time)                  // drop expires_at <= now, drop empty groups
func (f *FeedStore) StoriesStale() bool                          // story.created for an unknown group => ring must refetch
func (f *FeedStore) SetTrends(t *api.Trends)
func (f *FeedStore) SetDiscoverNew(n int)
func (f *FeedStore) SetOpen(key string, open bool)               // "timeline:following" | "timeline:discover" | "post:<id>" (last 8 kept) | "stories" | "notifications"
func (f *FeedStore) IsOpen(key string) bool
```

규칙 **[결정]**: 글 캐시가 진실이고 목록(타임라인 항목·답글 창·화면별 ID 목록)은 순서만 쓴다 — 화면은 `Post(item.Post.ID)` 가 있으면 그것을 그린다.
- **제자리 갱신**: `ObservePosts`·`observeEventPost`·리듀서 규칙은 캐시에 이미 있는 글이면 새 `*api.Post` 를 만들지 않고 **그 구조체의 필드를 덮어쓴다**(`*cached = merged`). 그래서 화면이 들고 있는 `Post(id)` 포인터는 캐시 항목이 사는 동안 늘 같고, 낙관적 변경(Task 7)도 같은 포인터에 한다. 없던 글이면 새로 넣는다.
- **권위 구분**: REST 결과(타임라인·답글·프로필·해시태그 페이지, `GetPost`, 작성 결과)는 `ObservePosts` — `viewer`·`poll.my_votes` 까지 그대로 덮어쓴다(다른 기기에서 좋아요를 취소했으면 `liked=false` 로 내려감). 이벤트 글은 `observeEventPost` — 본문·미디어·작성자·숫자만 바꾸고 캐시의 `viewer`·`poll.my_votes` 는 지킨다(캐시에 없던 글이면 이벤트 값 그대로 넣는다).

- [ ] **Step 1: Write the failing tests**

```go
func ev(t *testing.T, typ string, data any) realtime.Event // json.Marshal(data) -> Event{Type: typ, Data: raw}

func TestGetReturnsRegisteredReducer(t *testing.T) {
	s := store.New()
	if f := feedstore.Get(s); f == nil || f.Name() != "feed" { t.Fatalf("feed reducer not registered: %v", f) }
}
func TestTimelineItemCreatedInserts(t *testing.T) {
	// item := fixtures.Timeline()[0]; s.Apply(ev(t,"timeline.item_created", map[string]any{"item": item}))
	// Timeline("following").Get(item.ID) ok; Post(item.Post.ID).Body == item.Post.Body; Timeline("discover").Len()==0
}
func TestPostStatsUpdatesCachedPost(t *testing.T)   // post.stats {like_count:20,...} -> Post(id).LikeCount==20, ReplyCount==7
func TestPostDeletedRemovesFromTimelines(t *testing.T) // post + a repost item of it in following & discover -> both gone; Post(id).Deleted && Body==""
func TestPostDeletedKeepsReplyPlaceholder(t *testing.T) // reply with reply_count 2 inside Replies(parent) stays with Deleted=true; reply_count 0 => removed
func TestPostReplyCreatedOnlyIntoLoadedWindow(t *testing.T) // no Replies(parent) yet => HasReplies false after event; after Replies(parent) exists => inserted in ID order
func TestPollUpdatedKeepsMyVotes(t *testing.T) {
	// SetPoll(Poll{ID:"P", ContextType:"post", ContextID:postID, MyVotes:[]int{1}, Options counts 4/2})
	// event poll.updated {poll: same id, counts 5/2, my_votes: []} -> Post(postID).Poll.Options[0].VoteCount==5 && MyVotes==[]int{1}
	// event with ContextType "message" -> ignored (no panic, post untouched)
}
func TestBlockHidesAuthorEverywhere(t *testing.T) {
	// seed: hana post in following+discover, a repost by hana of dev_kim's post, a reply by hana in Replies(x), a notification with actor hana
	// block.updated {user_id: hana, blocked: true} -> none of them remain; dev_kim's original post (not via hana's repost) remains
}
func TestNotificationsRead(t *testing.T) // ids / all / before (ID <= before) each set Read=true only on matching items
func TestStoryEvents(t *testing.T)       // story.deleted removes story & empty group; story.created for known group => HasUnseen=true; unknown group => StoriesStale()==true; PruneStories(now past expires_at) drops it
func TestUserUpdatedRefreshesAuthors(t *testing.T) // user.updated hana display_name "하나2" -> Post(id).Author.DisplayName=="하나2"
func TestReset(t *testing.T)            // after Reset: all windows Len()==0, Stories()==nil, Trends()==nil, NewDiscoverCount()==0
func TestObservePostsRESTIsAuthoritative(t *testing.T) {
	// ObservePosts(p with Viewer.Liked=true, LikeCount 13); held := f.Post(p.ID)
	// ObservePosts(p with Viewer.Liked=false, LikeCount 12)  // REST refetch after unliking on another device
	// f.Post(p.ID) == held (same pointer) && !held.Viewer.Liked && held.LikeCount == 12
}
func TestEventPostKeepsViewer(t *testing.T) {
	// ObservePosts(p with Viewer.Liked=true, Poll.MyVotes=[0]); held := f.Post(p.ID)
	// s.Apply(ev("timeline.item_created", {item: {post: p with Viewer{} (default), Poll.MyVotes=[], LikeCount 20}}))
	// f.Post(p.ID) == held && held.Viewer.Liked && held.Poll.MyVotes == []int{0} && held.LikeCount == 20
}
func TestHideAuthorMatchesBlockEvent(t *testing.T) // same seed as TestBlockHidesAuthorEverywhere; HideAuthor(hana) => identical windows to the block.updated path
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/feedstore/ -v`
Expected: FAIL — `undefined: feedstore.Get` (패키지 미존재로 빌드 실패)

- [ ] **Step 3: Implement `FeedStore` in `feedstore.go` and event rules in `reducer.go`**

`init()` 에서 `store.RegisterReducer(func(s *store.Store) store.Reducer { return newFeedStore() })`. `Get` = `s.Reducer(Name).(*FeedStore)`. 타임라인 창 키 `"following"|"discover"`, `idOf = item.ID`, `timeOf = item.At`; 알림 창 `idOf = n.ID`, `timeOf = n.CreatedAt`; 답글 창 `timeOf = p.CreatedAt`. 차단 판단은 이벤트가 바꾼 `s.Blocks` 를 `s.IsBlocked` 로(코어가 먼저 적용). 리포스트 항목은 `item.RepostedBy` 와 `item.Post.Author` 둘 다 검사. `block.updated{blocked:true}` 처리 본체는 `HideAuthor` 하나로 두고 리듀서가 그것을 부른다. 이벤트 글(`timeline.item_created`·`post.reply_created`)은 `observeEventPost`, REST 는 `ObservePosts`(위 권위 구분). `poll.updated` 는 `Poll.ContextType=="post"` 만, 기존 `MyVotes` 보존. 모든 갱신은 제자리(위 규칙).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/feedstore/ -v`
Expected: PASS (15 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/store/feedstore/
git commit -m "feat(feedstore): add feed reducer with timeline, post, story and notification rules"
```

---

### Task 2: feedstore — 재동기화

**Files:**
- Create: `internal/store/feedstore/resync.go`
- Test: `internal/store/feedstore/resync_test.go`

**Interfaces:**
- Consumes: `store.Resyncer`, `(*store.Window[T]).OverlapAnchor(5*time.Second)`, `api.(*Client).{FollowingTimeline, DiscoverTimeline, GetPost, ListReplies, StoryFeed, ListNotifications}`, `apitest.New/JSON/Requests/Client`.
- Produces:

```go
func (f *FeedStore) Resync(s *store.Store, c *api.Client) tea.Cmd // nil when nothing open
type ResyncMsg struct {
	Key      string               // the SetOpen key this result refreshes
	Timeline *api.TimelinePage
	Restart  bool                 // gap=true: replace the window from page 1
	Post     *api.Post
	Replies  *api.Page[api.Post]
	Stories  []api.StoryGroup
	Notifs   *api.NotificationPage
	Err      error
	Seq      uint64               // assigned by Resync (per FeedStore counter, one per result message)
}
func (f *FeedStore) ApplyResync(m ResyncMsg) // applies each Seq at most once (remembers applied Seqs > last-applied watermark); later calls with the same Seq are no-ops
```

전달자 **[결정]**: `ResyncMsg` 는 앱 규칙(5.3.4 6단계)대로 **살아 있는 모든 컴포넌트에 방송**된다. ui/feed 의 **모든 경로 컴포넌트**(`RouteFeed`·`RoutePost`·`RouteCompose`·`RouteProfile`·`RouteStories`·`RouteHashtag`·`RouteTrends`·`RouteNotifications`)와 ui/feed 오버레이 전부는 `Update` 첫머리에서 공용 도우미 `forwardResync(ctx, msg) bool`(Task 6 `timeline.go`)을 불러 `ApplyResync` 로 넘긴다. 같은 메시지를 여러 컴포넌트가 넘겨도 `Seq` 로 한 번만 적용되므로, 어떤 조합의 화면이 살아 있든(예: 컴팩트에서 알림 → 글 상세만 열려 `Feed[0]` 타임라인 인스턴스가 아직 없음) 열린 글 상세가 다시 채워진다. 살아 있는 ui/feed 컴포넌트가 하나도 없으면 그 글을 보는 화면도 없으므로 버려져도 되고, 각 화면의 `Init` 이 늘 첫 페이지를 다시 받는다.

규칙(events.md 5.3 4단계): `timeline:following` → `since=OverlapAnchor(5s)`(빈 문자열이면 첫 페이지), 결과 `Gap` 이면 같은 Cmd 안에서 since 없이 첫 페이지를 다시 받아 `Restart=true` 로 `Replace`, 아니면 `Upsert`. `timeline:discover` → 첫 페이지 `Replace` + `SetDiscoverNew(0)`. `post:<id>` → `GetPost` + `ListReplies` 첫 페이지 `Replace`(글은 REST 결과이므로 `ObservePosts` — viewer·my_votes 포함 권위). `stories` → `StoryFeed`. `notifications` → `ListNotifications` 첫 페이지 `Replace`. 여러 개면 `tea.Batch`.

- [ ] **Step 1: Write the failing tests**

```go
func drain(t *testing.T, cmd tea.Cmd) []tea.Msg // runs cmd, flattens tea.BatchMsg recursively

func TestResyncNothingOpenReturnsNil(t *testing.T) { if f.Resync(s, srv.Client()) != nil { t.Fatal("want nil cmd") } }
func TestResyncFollowingUsesOverlapAnchor(t *testing.T) {
	// window holds items at 11:00:00 (A), 11:00:03 (B), 11:00:09 (C); SetOpen("timeline:following", true)
	// msgs := drain(f.Resync(...)); request query contains "since=<B.ID>"; ApplyResync(msgs[0]) upserts new item, dedups C
}
func TestResyncGapRestarts(t *testing.T)       // server: since => gap:true; then no-since page => Restart==true; window == page items only
func TestResyncRefetchesOpenPost(t *testing.T) // SetOpen("post:P") -> GET /v1/posts/P and /v1/posts/P/replies recorded
func TestSetOpenKeepsLastEightPosts(t *testing.T) // open 9 posts -> Resync requests 8 posts (oldest dropped)
func TestApplyResyncOncePerSeq(t *testing.T) {
	// m := ResyncMsg for "post:P" (Seq assigned by Resync); ApplyResync(m); then a post.stats event sets LikeCount 30
	// ApplyResync(m) again (second forwarder) -> no-op: LikeCount still 30, Replies(P) unchanged
	// two separate Resync runs => different Seq, both applied
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/feedstore/ -run Resync -v`
Expected: FAIL — `f.Resync undefined`

- [ ] **Step 3: Implement `Resync`, `ResyncMsg`, `ApplyResync` in `resync.go`**

API 호출은 Cmd 함수 안에서만(Store 접근 금지), 상태 반영은 `ApplyResync` 에서만(Update 고루틴).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/feedstore/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/feedstore/resync.go internal/store/feedstore/resync_test.go
git commit -m "feat(feedstore): resync open timelines, posts, stories and notifications"
```

---

### Task 3: imgrender — 디코드·업로드 전처리·하프블록

**Files:**
- Create: `internal/imgrender/decode.go`, `internal/imgrender/halfblock.go`
- Test: `internal/imgrender/decode_test.go`, `internal/imgrender/halfblock_test.go`

**Interfaces:**
- Produces (계약 6.3): `func Decode(data []byte, contentType string) (image.Image, error)`, `func PrepareUpload(path string) (data []byte, contentType string, err error)`, `func HalfBlock(img image.Image, cols, rows int, bg color.Color) []string`, `type FitMode int; const (FitContain FitMode = iota; FitCover)`.
- Produces (내부용): `var ErrUnsupported = errors.New("imgrender: unsupported image format")`, `var ErrTooLarge = errors.New("imgrender: image too large even after downscaling")`, `const MaxUploadBytes = 5242880`, `func fit(img image.Image, cols, rows int, mode FitMode, bg color.Color) image.Image` (픽셀 박스 `cols × rows*2`, `draw.CatmullRom.Scale`, 남는 칸은 bg).

- [ ] **Step 1: Write the failing tests**

```go
const webp1x1 = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==" // 1x1 lossless WebP (base64)

func TestDecodeFormats(t *testing.T)  // png/jpeg/gif encoded from fixtures.TestImage() + webp1x1 decode; "image/bmp" => ErrUnsupported
func TestPrepareUploadStripsJPEGExif(t *testing.T) {
	// write JPEG with an injected APP1 "Exif\x00\x00..." segment after SOI to t.TempDir()
	// data, ct, err := PrepareUpload(p); ct=="image/jpeg"; !bytes.Contains(data, []byte("Exif")); Decode(data) ok
}
func TestPrepareUploadReencodesPNG(t *testing.T)   // PNG with a tEXt chunk -> output has no "tEXt"; ct=="image/png"
func TestPrepareUploadPassesSmallGIF(t *testing.T) // bytes.Equal(out, in), ct=="image/gif"
func TestPrepareUploadDownscalesHuge(t *testing.T) // 3000x3000 random-noise PNG (>5MB) -> ct=="image/jpeg", len(out) <= MaxUploadBytes
func TestPrepareUploadMissingFile(t *testing.T)    // errors.Is(err, fs.ErrNotExist)
func TestHalfBlockShapeAndColors(t *testing.T) {
	// 2x4 image: top row red, 2nd row blue, ... ; lines := HalfBlock(img, 2, 2, black)
	// len(lines)==2; ansi.Strip(lines[0])=="▀▀"; ansi.StringWidth==2 && ansi.StringWidthWc==2 per line
	// lines[0] contains "255;0;0" (fg top) and "0;0;255" (bg bottom)
}
func TestHalfBlockContainLetterbox(t *testing.T) // 100x10 image into 10x5 cells -> 5 lines, bg color present on top/bottom rows
func TestHalfBlockDegenerate(t *testing.T)       // cols or rows <= 0 => nil; 1x1 image works
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/imgrender/ -v`
Expected: FAIL — `undefined: Decode`

- [ ] **Step 3: Implement in `decode.go` and `halfblock.go`**

`Decode`: `image/png`, `image/jpeg`, `image/gif`(첫 프레임 `gif.Decode`), `image/webp`(`golang.org/x/image/webp`). `PrepareUpload`: `http.DetectContentType` 로 형식 판별(확장자 무시), JPEG 은 디코드 후 `jpeg.Encode(q=90)`, PNG 는 디코드 후 `png.Encode`(둘 다 메타데이터 소실), GIF/WebP 는 그대로. 결과가 `MaxUploadBytes` 초과면 첫 프레임을 긴 변 ×0.8 씩 줄여 JPEG q=85 로 재시도, 긴 변 < 320 이 되면 `ErrTooLarge`. `HalfBlock`: `fit(...FitContain...)` 뒤 칸마다 `lipgloss.NewStyle().Foreground(top).Background(bottom).Render("▀")`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/imgrender/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/imgrender/
git commit -m "feat(imgrender): decode, metadata-stripping upload prep and half-block renderer"
```

---

### Task 4: imgrender — Renderer(kitty·iTerm2 정책)와 미디어 캐시

**Files:**
- Create: `internal/imgrender/renderer.go`, `internal/imgrender/cache.go`, `internal/imgrender/export_test.go`
- Test: `internal/imgrender/renderer_test.go`, `internal/imgrender/cache_test.go`

**Interfaces:**
- Consumes: `termcaps.Caps{Graphics, InTmux}`, `kitty.EncodeGraphics(w, img, *kitty.Options)`, `kitty.Placeholder`, `kitty.Diacritic(i)`, `kitty.{Delete, DeleteID}` + `(*kitty.Options).Options()`, `ansi.KittyGraphics(payload, opts...)`, `iterm2.File{Inline, Width, Height, Size, Content}`, `iterm2.Cells`, `ansi.ITerm2`, `ansi.SaveCursor`, `ansi.RestoreCursor`, `ansi.CursorPosition(col,row)`, `api.(*Client).FetchMedia`, `api.(*Client).MediaURL`, `config.WriteFileAtomic`, `config.Paths.Cache`.
- Produces (계약 6.3): `type Options{Cols, Rows int; BG color.Color; Fit FitMode}`, `type Rendered{Lines []string; Setup string; ID uint32}`, `func New(caps termcaps.Caps, c *Cache) *Renderer`, `func (r *Renderer) Render(key string, img image.Image, o Options) Rendered`, `func NewCache(p config.Paths) *Cache`, `func (c *Cache) Load(ctx context.Context, a *api.Client, m api.Media) (image.Image, error)`.
- Produces (내부용): `func ITerm2Inline(img image.Image, x, y, cols, rows int) (string, error)` — `SaveCursor + CursorPosition(x+1, y+1) + ITerm2(File{Inline:true, Width:Cells(cols), Height:Cells(rows), Size:n, Content:base64(png)}) + RestoreCursor`. `func (r *Renderer) Caps() termcaps.Caps`. `func (r *Renderer) Peek(key string, img image.Image, o Options) (out Rendered, ready bool)` — **`View` 전용**: 상태를 바꾸지 않는다. kitty 에서 그 조합이 이미 `Render` 로 전송됐으면 `Render` 와 같은 자리표시자 줄(`Setup==""`)·`ready=true`, 아직이면 같은 크기의 **하프블록** 줄·`ready=false`(호출자는 다음 `Update` 에서 `Render` 해 `Setup` 을 내보낸다). kitty 가 아니면 `Render` 와 같은 결과·`ready=true`. 시험 훅 `export_test.go`: `func SetCacheLimit(c *Cache, bytes int64)`, `func SetRenderLimit(r *Renderer, n int)`.

정책 **[결정]**: `GraphicsKitty && !InTmux` → kitty 자리표시자, 그 외(`ITerm2` 포함, 목록·카드) → 하프블록, `GraphicsNone` → 빈 칸 + 가운데 `🖼` 한 개. kitty:
- **조합마다 자기 이미지 ID**: 렌더 키 `(key, Cols, Rows, Fit, BG)` 마다 서로 다른 ID(1부터 증가). 그래서 같은 사진을 피드 카드(48×12)와 `feed.media` 전체 보기(같은 키 `m.ID`)가 동시에 그려도, 모드 A(48×12) → B(30×8) → A 로 돌아와도 각 자리표시자는 자기 크기의 가상 배치만 가리킨다(같은 `i=` 로 `a=T` 를 다시 보내 배치를 덮어쓰는 일이 없다).
- `Setup` 은 그 조합의 **첫 `Render`** 에서만 비어 있지 않다(전송 + 가상 배치); 같은 조합을 다시 `Render` 하면 `Setup==""` 이고 ID 도 같다.
- 옵션 `{Action: kitty.TransmitAndPut, ID: id, Format: kitty.RGBA, Transmission: kitty.Direct, Chunk: true, VirtualPlacement: true, Columns: cols, Rows: rows, Quiet: 2}`; 각 줄 = 글자색 `#RRGGBB`(=ID 24비트)로 감싼 `cols` 개의 `Placeholder + Diacritic(row) + Diacritic(col)`.
- 렌더 LRU 64개(키: 위 조합 + 방식). kitty 항목이 LRU 에서 밀려나면 그 ID 를 지우는 `ansi.KittyGraphics(nil, (&kitty.Options{Action: kitty.Delete, Delete: kitty.DeleteID, DeleteResources: true, ID: id}).Options()...)`(`a=d,d=I,i=<id>`, 터미널 쪽 이미지 데이터까지 해제)를 대기열에 넣고, **그 밀어낸 새 조합의 `Setup` 앞에** 붙여 함께 내보낸다(밀어내기는 늘 새 전송과 같은 `Render` 에서 일어나므로 지우기가 누락되지 않는다). 밀려난 조합을 나중에 다시 그리면 새 ID 로 다시 전송한다.

- [ ] **Step 1: Write the failing tests**

```go
func TestRenderKittyPlaceholders(t *testing.T) {
	r := imgrender.New(termcaps.Caps{Graphics: termcaps.GraphicsKitty}, nil)
	out := r.Render("u1", fixtures.TestImage(), imgrender.Options{Cols: 10, Rows: 4, BG: bg})
	// strings.HasPrefix(out.Setup, "\x1b_G"); Setup contains "a=T", "U=1", "c=10", "r=4", "q=2", fmt.Sprintf("i=%d", out.ID)
	// len(out.Lines)==4; each line ansi.StringWidth==10 && ansi.StringWidthWc==10 && strings.ContainsRune(line, kitty.Placeholder)
	// second Render("u1", same opts).Setup == "" ; Render("u2").ID != out.ID
	// size round trip 10 -> 12 -> 10: o12 := Render("u1", Cols 12); o12.Setup != "" && o12.ID != out.ID
	//   back := Render("u1", Cols 10); back.ID == out.ID && back.Setup == "" (the 10-col placement was never overwritten)
	// Peek("u3", ...) before any Render("u3") => ready==false, lines are half-block ("▀"), and a following Render("u3").Setup != "" (Peek did not mark it sent)
}
func TestRenderKittyEvictionDeletes(t *testing.T) {
	// r := New(kitty caps); SetRenderLimit(r, 2); a := Render("a"); Render("b")
	// c := Render("c") => strings.HasPrefix(c.Setup, "\x1b_G") && c.Setup contains "a=d", "d=I", fmt.Sprintf("i=%d", a.ID) before the transmit of c.ID
	// Render("a") again => new ID != a.ID and Setup != ""
}
func TestRenderTmuxFallsBackToHalfBlock(t *testing.T) // Caps{Kitty, InTmux:true} => Setup=="" && Strip(line) is all "▀"
func TestRenderITerm2UsesHalfBlockInLists(t *testing.T)
func TestRenderNoneGraphics(t *testing.T)              // GraphicsNone => lines are spaces + one "🖼", widths exact
func TestITerm2Inline(t *testing.T) // prefix ansi.SaveCursor, contains "\x1b[3;5H" for (x=4,y=2), "1337;File=", "inline=1", "width=20", "height=10", suffix ansi.RestoreCursor
func TestCacheLoadFetchesOnceThenMemory(t *testing.T) // apitest GET /v1/media/{id} serves PNG+ETag; two Loads => 1 request
func TestCacheUsesETagAcrossInstances(t *testing.T)   // NewCache(same Paths) -> request header If-None-Match == etag; server 304 => image decoded from disk
func TestCacheEvictsOverLimit(t *testing.T)           // SetCacheLimit(c, 1500) and three ~1KB media => oldest file removed from <Cache>/media
func TestCacheLoadNotFound(t *testing.T)              // 404 => api.IsCode(err, "not_found")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/imgrender/ -run 'Render|ITerm2|Cache' -v`
Expected: FAIL — `undefined: imgrender.New`

- [ ] **Step 3: Implement `Renderer` in `renderer.go` and `Cache` in `cache.go`**

캐시 경로 `<Paths.Cache>/media/<upload_id>` + `<upload_id>.etag`, 쓰기는 `config.WriteFileAtomic(…, 0600)`, 크기 합 > 200MB 면 수정 시각 오래된 것부터 삭제. 메모리 LRU 32(디코드된 `image.Image`). `Load` 는 `a.MediaURL(m.URL)` 로 받는다.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/imgrender/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/imgrender/
git commit -m "feat(imgrender): kitty placeholder renderer, iTerm2 inline helper and media cache"
```

---

### Task 5: 글 카드 렌더러

**Files:**
- Create: `internal/ui/feed/card.go`, `internal/ui/feed/snsfix_test.go`
- Test: `internal/ui/feed/card_test.go`, `internal/ui/feed/testdata/TestCardGolden/*.golden`

**Interfaces:**
- Consumes: `markup.Parse(body, markup.ParseOptions{Hashtags: true})`, `markup.Render(doc, t, markup.RenderOptions{Width, MyHandle, Mentions, Compact, MaxLines, Hyperlinks})`, `markup.Plain`, `widgets.PollView(t, p, width, selected)`, `widgets.UserName`, `humanize.Relative`, `humanize.Count`, `textwidth.{Sanitize, Truncate, Fit, Width}`, `ui.Density`, `ctx.Theme.S.{Selected, Dim, Deleted, Hashtag, Timestamp}`, `(*theme.Theme).UserColor`.
- Produces (패키지 내부, 이후 모든 목록 화면이 사용):

```go
type hitKind int
const (hitHashtag hitKind = iota; hitPollOption; hitImage; hitAuthor; hitQuote)
type cardHit struct{ Line, X0, X1 int; Kind hitKind; Value string } // Value: tag | option idx | media idx | user id | post id; Line = index into cardView.Lines, X0..X1 (half-open) = cells from the card's left edge INCLUDING the 2-cell gutter
type cardOpts struct {
	Width    int
	Density  ui.Density
	Selected bool
	PollSel  int                                              // -1 = none
	Image    func(m api.Media, cols, rows int) []string       // nil or nil-result => dim placeholder box "이미지 불러오는 중"
}
type cardView struct{ Lines []string; Hits []cardHit }
func renderCard(ctx *ui.Context, it api.TimelineItem, p *api.Post, o cardOpts) cardView
func imageBox(m api.Media, contentW int, d ui.Density) (cols, rows int) // Full: cols=min(contentW,48); Bubble: min(contentW,30); rows=clamp(cols*h/w/2,3,12), w/h nil => 4:3; Line/Minimal: 0,0
func hiddenByBlock(ctx *ui.Context, it api.TimelineItem) bool         // ctx.Store.IsBlocked(it.Post.Author.ID) || (it.RepostedBy != nil && IsBlocked(it.RepostedBy.ID))
```

**차단 필터 [결정]** (Review Focus 3): FeedStore 창 밖에서 **화면이 직접 들고 있는 ID 목록**(해시태그 피드, 프로필 글·답글·미디어 탭, `feed.quotes`, `feed.user_list`, 방명록 탭의 작성자, 스토리 그룹)은 리듀서가 걸러 주지 않는다. 그래서 모든 목록 컴포넌트는 (1) `List.SetItems` 직전과 렌더 콜백에서 `hiddenByBlock`(사용자 목록·방명록은 `ctx.Store.IsBlocked(user.ID)`)인 항목을 건너뛰고, (2) 방송된 `ui.EventMsg`(`msg.Event.Type == "block.updated"`) 또는 `ui.ResultMsg` 태그 `feed.block:<userID>` 를 받으면 자기 ID 목록에서 그 항목을 지운 뒤 `SetItems` 를 다시 한다(선택이 지워진 항목이면 List 규칙대로 이웃으로). 이 Task 의 `renderCard` 는 필터하지 않는다(호출자 책임).

카드 구성 **[결정]** (왼쪽 2칸 거터: 선택 시 `▎ `(acc), 아니면 공백 2칸; 내용 폭 `Width-2`):
- `DensityFull`: (리포스트면) `↻ <이름>님이 리포스트`(dim) / `@handle 이름 · 3분`(이름 `UserColor`) / 본문 `markup` 최대 8줄(`… 더 보기`) / 첫 이미지 `imageBox` + 2장 이상이면 `🖼 +N` 칩 / 인용 상자(왼쪽 `┃`, 2줄, 볼 수 없으면 `볼 수 없는 글이에요`, 삭제면 `삭제된 글이에요`) / `widgets.PollView` / 통계 줄 `♥ 12  ↻ 3  💬 4`(↻ 는 0 이고 내가 안 했으면 생략, 인용 수 >0 이면 `  ❝ N`; 내가 좋아요 = `♥` acc2 굵게, 리포스트 = `↻` acc3) / 구분선 `┄`(line 색, 폭 전체).
- `DensityBubble`(위젯): Full 과 같되 이미지 `min(contentW,30)`, 본문 최대 6줄.
- `DensityLine`(컴팩트): 한 줄 `3분 @hana 오늘 첫 눈 ❄ 사무실 창밖 🖼1 ♥12 ↻3 💬4`.
- `DensityMinimal`(초소형): 한 줄 `hana: 오늘 첫 눈… ♥12`.
- 삭제된 글: 본문 자리 `삭제된 글이에요`(Deleted). 해시태그 클릭 영역은 줄의 `ansi.Strip` 결과에서 `"#"+tag` 를 대소문자 무시로 찾아 앞부분 `ansi.StringWidth` 로 X 를 구한다.

- [ ] **Step 1: Write the failing tests**

`snsfix_test.go` 에 **이 계획 ui/feed 시험이 쓰는 공유 픽스처 전부**를 여기서 정의한다(Task 6 의 `snsAPI` 가 처음부터 모두 등록하므로 뒤 Task 는 참조만 하고 새로 만들지 않는다). 시각 기준 = 하네스 시계 `2026-11-05T02:05:00Z`. ID 번호는 겹치지 않게 아래 그대로.

```go
func sid(n int) string { return fmt.Sprintf("01JDSNS%019d", n) }
func ptr[T any](v T) *T { return &v }
func at(minAgo int) time.Time                       // 02:05Z minus minAgo minutes (negative = future)
func seeded() *store.Store                          // store.New() + fixtures.Seed
func userOf(handle string) api.UserSummary          // seeded().Users by Handle (sam, minji, junho, hana, dev_kim); panics if absent
func postBy(handle string) api.Post                 // the fixtures.Timeline() post by hana or dev_kim
func mkPost(n int, handle, body string, minAgo int) api.Post // ID sid(n), Author userOf(handle), Body, CreatedAt at(minAgo), Hashtags = lowercased "#word" tokens of body
func itemOf(p api.Post) api.TimelineItem            // {ID: p.ID, Type: "post", Post: p, At: p.CreatedAt}
func allPosts() map[string]api.Post                 // every post below + fixtures.Timeline posts, by ID (snsAPI GET /v1/posts/{id})
```

| 함수 | 정확한 내용 |
|---|---|
| `quotePost()` | `mkPost(10, "minji", "이거 보세요", 5)` + `Quote: &api.QuotedPost{Available: true, Post: ptr(postBy("hana"))}` |
| `pollPost()` | `mkPost(11, "minji", "", 6)` + `Poll{ID: sid(12), ContextType: "post", ContextID: sid(11), CreatorID: userOf("minji").ID, Question: "점심 메뉴", Multi: false, Options: [{0,"국밥",4},{1,"돈까스",2}], VoterCount: 6, ClosesAt: at(-60), MyVotes: []int{0}}` |
| `multiPollPost()` | `pollPost()` 복사 + 글 ID `sid(13)`, `Poll.ID sid(14)`, `ContextID sid(13)`, `Multi: true`, `MyVotes: []int{}` |
| `myPollPost()` | `pollPost()` 복사 + 글 ID `sid(19)`, `Poll.ID sid(25)`, `ContextID sid(19)`, 작성자·`CreatorID` = sam(만든 사람 `c` 마감 시험) |
| `deletedPost()` | `mkPost(15, "junho", "", 20)` + `Deleted: true` |
| `repostItem()` | `{ID: sid(16), Type: "repost", Post: postBy("hana"), RepostedBy: ptr(userOf("dev_kim")), At: at(1)}` |
| `myPost()` | `mkPost(17, "sam", "점심 먹고 배포합니다", 2)` |
| `privatePost()` | `mkPost(18, "junho", "비공개 계정 글", 4)` + `Author.IsPrivate = true` |
| `longTimeline()` | `mkPost(20..24, "minji", "채우기 글 <i>", 1..5)` 5개(`itemOf`) 다음에 `itemOf(postBy("dev_kim"))` — 6개, dev_kim 이 마지막 |
| `replyTree()` | 반환 `(top []api.Post, children map[string][]api.Post)`; 부모 = `postBy("hana")`. top: `mkPost(30,"minji","우와 벌써?",2)`, `mkPost(31,"junho","사진 예쁘다",2)`(`ReplyCount: 2`), `mkPost(32,"sam","출근길 조심!",1)`; `children[sid(31)]`: `mkPost(33,"hana","고마워요",1)`, `mkPost(34,"minji","나도 찍었는데",1)`. 모두 `ReplyToID`=부모, `RootID`=hana 글, `ReplyToAuthor`=부모 작성자 |
| `deepChain()` | `mkPost(40..46, "minji"|"junho" 번갈아, "깊이 <d>", 1)` 7단 사슬(각각 앞 글의 답글, 첫째의 부모 = hana 글) |
| `profileOf(handle, rel)` | `UserSummary: userOf(handle)`; hana: `Bio "눈 오는 날 좋아함"`, 120/80/42, `Presence "online"`, `Status{Emoji "🎧", Text "듣는 중"}`; 그 외: `Bio ""`, 10/10/3, `Presence nil`; sam(`@me`) 이면 `Relationship nil`, 아니면 `&rel` |
| `relFollowing()` | `api.Relationship{Following: "active", FollowedBy: "active", CanDM: true, CanViewPosts: true}` |
| `profilePosts(handle, filter)` | dev_kim/`posts`: `[itemOf(postBy("dev_kim")), {ID: sid(71), Type: "repost", Post: postBy("hana"), RepostedBy: ptr(userOf("dev_kim")), At: at(2)}]`; hana/`posts`: `[itemOf(postBy("hana"))]`; sam/`posts`: `[itemOf(myPost())]`; `replies`·`media` 및 그 밖: `[]` |
| `hashtagPosts(tag)` | `첫눈`: `[itemOf(postBy("hana")), itemOf(mkPost(70,"minji","#첫눈 왔다",8))]`; `golang`: `[itemOf(postBy("dev_kim"))]`; 그 밖 `[]` |
| `guestbookEntries(ownerID)` | `{ID sid(50), Author minji, Body "놀러 왔어요 ✨", CreatedAt at(3)}`, `{ID sid(51), Author nil, Body "", IsSecret true, Hidden true, CreatedAt at(10)}`, `{ID sid(52), Author sam, Body "또 올게요", CreatedAt at(60)}`; 모두 `OwnerID = ownerID` |
| `followRequests()` | `[{User: minji, CreatedAt at(30)}, {User: junho, CreatedAt at(90)}]`(`api.FollowRequestEntry`) |
| `storyGroups()` | 순서 `[sam, hana, junho]`. sam: `{s sid(60) "출근 완료", Seen true, ViewCount ptr(3), CreatedAt at(30)}`; hana: `{sid(61) "눈 온다 ❄", BG "accent", at(15)}`, `{sid(62) "", Media: &Media{ID sid(63), URL "/v1/media/"+sid(63), ContentType "image/png", Width 64, Height 48}, BG "accent2", at(5)}` 둘 다 `Seen false`, `HasUnseen true`; junho: `{sid(64) "커피 ☕", Seen true, at(60)}`. 모든 `ExpiresAt = 2026-11-05T22:05:00Z`, `LatestAt` = 그룹 마지막 스토리 시각 |
| `trendsTop10()` | `&api.Trends{ComputedAt: ptr(2026-11-05T02:00:00Z) (서울 11:00), Items: rank 1..10 = 첫눈 42/30, golang 31/12, 점심 28/20, 배포 19/9, bubbletea 15/7, 출근길 12/10, 커피 11/9, 고양이 9/8, 야근 7/6, 주말 5/5}`(`PostCount/AuthorCount`) |
| `notificationsAll()` | `fixtures.Notifications()` 2개 + 계약 14종 type 각 1개(ID `sid(80..93)`, actor minji, `context` 는 Task 15 표의 이동 대상이 채워지도록: post 계열 `post_id = postBy("hana").ID`, message 계열 `channel_id`/`message_id`(+thread_reply 는 `thread_root_id`), `space_invite` 는 `subject{space_direct_invite, sid(94)}`·`space_name "노바 개발방"`) |

```go
func TestCardGolden(t *testing.T) {
	cases := []struct{ name string; w int; d ui.Density }{
		{"command_feed", 54, ui.DensityFull}, {"standard", 92, ui.DensityFull},
		{"compact", 118, ui.DensityLine}, {"widget", 78, ui.DensityBubble}, {"tiny", 50, ui.DensityMinimal}, {"narrow", 24, ui.DensityFull},
	}
	// for each case and each item [hana(image), dev_kim(#golang), repostItem, quotePost, pollPost, deletedPost]:
	//   v := renderCard(ctx, it, &p, cardOpts{Width: c.w, Density: c.d, Image: halfblockStub})
	//   every line: ansi.StringWidth==c.w && ansi.StringWidthWc==c.w
	//   golden.RequireEqual(t, ansi.Strip(strings.Join(all,"\n"))) under t.Run(c.name)
}
func TestCardStatsLine(t *testing.T)     // hana Full contains "♥ 12  ↻ 3  💬 4"; dev_kim contains "♥ 5  💬 2" and not "↻"
func TestCardHashtagHit(t *testing.T)    // dev_kim card Hits has {Kind: hitHashtag, Value: "golang"} with X0..X1 covering "#golang"
func TestCardPollHits(t *testing.T)      // pollPost Full => two hitPollOption hits Value "0","1"
func TestCardHostileWidths(t *testing.T) // every fixtures.HostileTimeline() item × widths 1..80 × 4 densities: no line wider than w, both width funcs equal
```

`ctx` 는 `uitest.New(t).Context()`(테마 D, 고정 시계).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/feed/ -run TestCard -v`
Expected: FAIL — `undefined: renderCard`

- [ ] **Step 3: Implement `renderCard` and `imageBox` in `card.go`**

- [ ] **Step 4: Generate goldens, inspect, re-run**

Run: `go test ./internal/ui/feed/ -run TestCardGolden -update && go test ./internal/ui/feed/ -run TestCard -v`
Expected: PASS. `testdata/TestCardGolden/command_feed.golden` 의 hana·dev_kim 카드가 `layouts.html` ① 피드 열(`@hana · 3분`, 이미지, `♥ 12  ↻ 3  💬 4` / `@dev_kim · 10분`, `#golang`, `♥ 5  💬 2`)과 같은 구성인지 눈으로 확인.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/feed/card.go internal/ui/feed/card_test.go internal/ui/feed/snsfix_test.go internal/ui/feed/testdata/TestCardGolden
git commit -m "feat(feed): post card renderer with four densities and hit regions"
```

---

### Task 6: 피드 화면 `RouteFeed` (팔로잉/발견·스토리 링·지휘소 피드 열)

**Files:**
- Create: `internal/ui/feed/timeline.go`, `internal/ui/feed/feedtabs.go`, `internal/ui/feed/images.go`, `internal/ui/feed/helpers_test.go`, `internal/ui/feed/export_test.go`
- Test: `internal/ui/feed/timeline_test.go`, `internal/ui/feed/testdata/TestTimelineGolden/**`

**Interfaces:**
- Consumes: `ui.RegisterRoute`, `ui.Component/KeyHandler/MouseHandler/Titled/Helper/Watcher/Persistent`, `ui.Do`, `ui.ResultMsg[T]`, `ui.After`, `ui.Navigate`, `ui.NavigateMsg`, `widgets.{List, NewList, Empty, Spinner, NewSpinner, Fade, Tabs, NewTabs}`, `ctx.Keys.Matches(msg, "list.down"…)`, `feedstore.Get`, `FeedStore.{Timeline, Post, ObservePosts, Stories, PruneStories, StoriesStale, SetStories, NewDiscoverCount, SetDiscoverNew, SetOpen, ApplyResync}`, `api.(*Client).{FollowingTimeline, DiscoverTimeline, StoryFeed}`, `imgrender.{New, NewCache, Options}`, `uitest.{New, WithAPI, WithSeed, WithRoute, AllSizes, Size}`, `apitest.New`, `fixtures.{Seed, Timeline, HostileTimeline, Hostile, TestImage}`.
- Produces:

```go
func newTimeline(ctx *ui.Context, r ui.Route) ui.Component // init(): ui.RegisterRoute(ui.RouteFeed, newTimeline)
func feedTabs(ctx *ui.Context, active string, width int) (line string, hit func(x int) (ui.Route, bool)) // "피드"=discover, "팔로잉"=following, "#트렌드"=RouteTrends
func storyRing(ctx *ui.Context, groups []api.StoryGroup, width int) (lines []string, hit func(x int) (userID string, ok bool))
func mediaRenderer(ctx *ui.Context) *imgrender.Renderer // one per (ctx.Paths.Cache, ctx.Caps.Graphics, ctx.Caps.InTmux)
func loadImage(ctx *ui.Context, m api.Media) tea.Cmd     // ui.Do tag "feed.media:<m.ID>" -> ResultMsg[image.Image]; called from Update only
func forwardResync(ctx *ui.Context, msg tea.Msg) bool   // feedstore.ResyncMsg -> feedstore.Get(ctx.Store).ApplyResync; every ui/feed component calls it first in Update (Task 2)
// images.go — shared by every ui/feed component that draws media (timeline, post, profile, hashtag, stories, feed.media)
type imageSlots struct{ /* requested ids, loaded images, want-list filled by View */ }
func (s *imageSlots) request(ctx *ui.Context, ms ...api.Media) tea.Cmd // Update: loadImage once per media ID not yet requested
func (s *imageSlots) cell(ctx *ui.Context, m api.Media, cols, rows int) []string // View (cardOpts.Image): nil if not loaded; else Renderer.Peek; !ready => remember (m.ID, cols, rows) in want-list
func (s *imageSlots) render(ctx *ui.Context, m api.Media, cols, rows int) tea.Cmd // Update: Renderer.Render; non-empty Setup => tea.Raw(Setup); nil otherwise
func (s *imageSlots) flush(ctx *ui.Context) tea.Cmd    // Update: render() every want-list entry, clear it; batched tea.Raw
func (s *imageSlots) loaded(msg tea.Msg) (id string, img image.Image, ok bool) // ResultMsg[image.Image] with tag "feed.media:<id>"
// list hit-testing shared by every card list (timeline, hashtag, profile tabs, post replies)
func cardAt(l *widgets.List[api.TimelineItem], items []api.TimelineItem, views func(api.TimelineItem) cardView, y int) (it api.TimelineItem, line int, ok bool)
// helpers_test.go (shared by later tasks)
func snsAPI(t *testing.T, hostile bool, opts ...snsOpt) *apitest.Server // registers every GET in the table below + a catch-all that fails the test
type snsOpt func(*snsCfg)
func withDiscoverSince(p api.TimelinePage) snsOpt              // response of GET /v1/timeline/discover WHEN "since" is present (default: discoverNew())
func withFollowing(items []api.TimelineItem) snsOpt            // first page of GET /v1/timeline/following (default fixtures.Timeline())
type postEdits struct{ Body map[string]string; ExtraReplies map[string][]api.Post } // maps made non-nil by withPostEdits
func withPostEdits(e *postEdits) snsOpt                        // GET /v1/posts/{id} and /replies read e AT REQUEST TIME (body override, replies appended) — lets a test change server data after the screen loaded
func discoverNew() api.TimelinePage                            // 2 new items (IDs "01JZZZZZZZZZZZZZZZZZZZZZ01"/"…02" — greater than every fixture ID; junho "새 글 하나", minji "새 글 둘", at(0)) + the window's newest fixtures.Timeline item (overlap); Gap false
func sweep(t *testing.T, sizes []uitest.Size, golden string, opts []uitest.Option, act func(h *uitest.Harness)) // t.Run("WxH") + uitest.New + h.Resize + act + h.Golden(golden) (golden=="" => h.Check())
func region(screen string, r layout.Rect) []string            // ansi.Cut per line
func lastReq(h *uitest.Harness, method, path string) (apitest.Recorded, bool)
func sendCmd(h *uitest.Harness, cmd tea.Cmd)                   // runs cmd, flattens tea.BatchMsg recursively, h.Send(each msg)
func cellOf(t *testing.T, screen, s string) (x, y int)        // first occurrence of s in h.Screen() (cell column via ansi.StringWidth of the prefix); t.Fatal if absent
// export_test.go
func SelectedID(c ui.Component) string                         // selected card item ID; "" while the new-posts banner is selected
func AnchorID(c ui.Component) string                           // the card List's Anchor().ItemID
```

`snsAPI` 가 등록하는 GET(모두 Task 5 `snsfix_test.go` 데이터, hostile 이면 `fixtures.HostileTimeline` 기반): `/v1/timeline/following`(`fixtures.Timeline()`), `/v1/timeline/discover`(같은 항목), `/v1/stories/feed`(`storyGroups()`), `/v1/trends`(`trendsTop10()`), `/v1/posts/{id}`(`allPosts()`), `/v1/posts/{id}/context`(`ReplyToID` 를 따라 올라간 조상), `/v1/posts/{id}/replies`(`replyTree()`·`deepChain()`), `/v1/users/{user}`(`profileOf(handle, relFollowing())`), `/v1/users/{user}/posts`(`profilePosts(handle, filter)`), `/v1/users/{user}/guestbook`(`guestbookEntries`), `/v1/notifications`(`notificationsAll()`), `/v1/hashtags/{tag}/posts`(`hashtagPosts`), `/v1/me/follow-requests`(`followRequests()`), `/v1/media/{id}`(fixtures.TestImage PNG + ETag). 사용자 행동이 있어야만 나가는 GET — `/v1/users/{user}/stories`(Task 11·13), `/v1/hashtags/search`·`/v1/search/posts`(Task 14), `/v1/users/{user}/followers`·`/following`·`/v1/posts/{id}/likes` 등 목록(Task 7) — 은 등록하지 않고 그것을 쓰는 시험이 직접 `srv.JSON` 으로 등록한다.

**core 요청 [결정]**: 하네스 앱은 `ready` 를 못 받으면 5초 뒤 REST 부트스트랩을 할 수 있다(계약 5.4 "시작 순서"; 하네스가 이를 건너뛰는지는 계약에 없으므로 기대지 않는다) — 이 계획 시험은 `h.Advance(30*time.Second)` 등으로 그 시점을 넘긴다. 그래서 `snsAPI` 는 events.md 5.3 1단계 GET 10종도 등록한다: `/v1/me`, `/v1/spaces`, `/v1/spaces/{id}`, `/v1/dms`, `/v1/me/unreads`, `/v1/notifications/unread-count`, `/v1/friends`, `/v1/friends/requests`, `/v1/me/direct-invites`, `/v1/blocks`. 응답 값은 **그 시험의 `uitest.WithSeed` 가 Store 에 넣는 것과 같다** — 즉 `fixtures.Seed` 가 채운 Store(hostile 이면 `fixtures.Seed` + `fixtures.Hostile`)에서 읽어 만든다: `helpers_test.go` 가 `s := store.New(); fixtures.Seed(s)`(hostile 이면 `fixtures.Hostile(s)` 추가)로 만든 Store 의 `Me`·방·DM·안 읽음·알림 수·친구·친구 요청·직접 초대·차단을 api.md 응답 모양으로 감싼다(부트스트랩이 돌아도 seed 상태가 바뀌지 않게). `fixtures.RegisterAPI` 는 부르지 않는다 — 겹친다고 panic 하지는 않는다(core Task 8 의 `apitest` 는 같은 모양 패턴의 재등록을 replace, 즉 나중 등록이 이김으로 처리한다). 부르지 않는 이유는 (1) RegisterAPI 는 "normal + hostile" 응답을 담는다(CA 10.4)고만 정해져 있어 그 부트스트랩 응답이 이 시험의 seed(일반 또는 적대적)와 같다는 보장이 계약에 없고 — 다르면 부트스트랩이 돈 뒤 Store 가 바뀌어 골든이 부트스트랩 여부에 따라 달라진다 —, (2) 그 13–15번(`/v1/timeline/following`·`/discover`·`/v1/notifications`)이 이 계획 데이터와 등록 순서에 따라 조용히 바뀌며, (3) 아래 catch-all 로 "이 표에 있는 요청만 나간다" 를 단언하려면 등록 목록 전체를 이 헬퍼가 쥐어야 하기 때문이다. 같은 이유로 `snsAPI` 안에서도 같은 모양 패턴을 두 번 등록하지 않는다(replace 로 앞 핸들러가 소리 없이 사라진다) — 응답을 바꿀 때는 `snsOpt` 나 시험이 나중에 `srv.JSON`/`srv.Handle` 로 **의도적으로** 덮어쓴다. 마지막으로 catch-all `srv.Handle("/", …)` 가 `t.Errorf("snsAPI: unregistered %s %s", r.Method, r.URL.Path)` 후 `404` 를 준다 — 등록 안 된 core 요청(예: 60초 `/v1/presence`)이 생기면 골든에 오류 토스트가 끼기 전에 시험이 바로 알린다(그때 이 목록에 seed 값으로 추가). 쓰기 엔드포인트는 각 시험이 직접 등록.

동작 **[결정]**:
- 위에서부터: 탭 줄(`feedTabs`, `Density` 가 Line/Minimal 이면 생략) → 스토리 링 1줄(그룹 없거나 Minimal 이면 생략; 안 본 그룹 `◉`(acc) 본 그룹 `○`(dim), 첫 칸 `+ 내 스토리` 또는 내 그룹) → 발견 탭이고 `NewDiscoverCount()>0` 이면 띠 1줄 → 카드 목록(`widgets.List[api.TimelineItem]`, 렌더는 `renderCard`). 탭 매핑은 시안 문구 그대로 `피드`=발견(discover), `팔로잉`=following 으로 둔다 — 시안 ① 피드 열은 `피드` 가 켜져 있지만 core 기본 `Feed[0]` 은 `{feed, Arg:"following"}` 이므로 **기본 골든에서는 `팔로잉` 이 켜진 것이 정상**이다(Step 4 참고).
- **새 글 띠 [결정]**: 문구 `새 글 N개 · enter`(N ≥ 100 이면 `새 글 100개+ · enter`). 띠는 List 밖의 선택 가능한 가짜 줄이다 — 띠가 보이는 동안 (a) `g g`(`list.top`) 는 첫 카드가 아니라 **띠**를 고르고, (b) 첫 카드가 선택된 상태의 `k`/`↑` 는 띠로 올라가고, (c) 띠에서 `j`/`↓` 는 첫 카드로 내려가며, (d) 띠 클릭도 고른다. 띠가 선택된 동안 `enter` = 발견 첫 페이지를 since 없이 다시 받아 `Replace` + `SetDiscoverNew(0)`(띠 사라짐, 선택은 첫 카드). 띠가 선택된 동안 글 키(`l b q r …`)는 무시. 띠가 사라지면 선택 표시도 사라진다.
- **새 글 수 [결정]**: 30초 확인 응답의 항목 중 **ID 가 창의 가장 새 항목 ID 보다 크고(문자열 비교, events.md 5.3) 창에 아직 없는 것**의 수. `OverlapAnchor` 때문에 겹쳐 오는 기존 항목은 세지 않는다. 응답이 `gap=true` 면 `SetDiscoverNew(100)`(→ `100개+`).
- 키: `1`/`2`/`3` = 피드(발견)/팔로잉/#트렌드(앞 둘은 `ui.Navigate(Route{Kind: RouteFeed, Arg: "discover"|"following"})`, 셋째는 `RouteTrends`), `s` = 첫 안 본 그룹으로 `RouteStories`, `c`(`post.compose`) = `RouteCompose{Arg:"post"}`, `enter`(`list.open`) = `RoutePost`(띠 선택 중이면 위 새로 받기). 마우스: 탭·링·띠·카드 클릭, 휠은 List.
- **카드 클릭 판정 [결정]** (`cardAt`, 이후 모든 카드 목록이 같은 함수를 쓴다): 컴포넌트는 List 렌더 콜백이 만든 `cardView` 를 `(item ID, 폭, 밀도, 선택 여부, 투표 선택)` 키로 캐시해 두고(같은 프레임의 View 결과 그대로), 클릭 `local.Y` 에서 목록 위 머리 줄 수(탭·링·띠)를 뺀 `y` 로 다음을 한다: `a := List.Anchor()`; `items` 에서 `a.ItemID` 의 위치부터 시작해 `top := -a.LineOffset`(앵커 카드의 첫 줄이 목록 맨 윗줄보다 `LineOffset` 줄 위에 있다는 계약 `ScrollAnchor` 의미); 각 카드마다 `h := len(views(it).Lines)`, `y < top+h` 이면 그 카드·`line = y-top` 로 끝, 아니면 `top += h` 하고 다음 카드(목록 끝을 넘으면 `ok=false`). 찾은 카드의 `Hits` 중 `Line == line && X0 <= local.X < X1` 인 것이 있으면 `handlePostHit`(Task 7), 없으면(빈 곳·통계 줄 등) `List.HandleMouse` 에 그대로 넘겨 선택만 바꾼다. 캐시에 없는 카드(아직 그린 적 없음)는 `renderCard` 를 같은 옵션으로 다시 불러 채운다.
- 무한 스크롤: 선택이 끝에서 5개 안이고 `next_cursor != nil` → 다음 페이지(`feed.timeline_more:<kind>`), 맨 아래 `widgets.Spinner` + `불러오는 중…`.
- 발견 새 글 확인: 30초 `ui.After`(세대 번호). `View` 가 기록한 `lastViewAt` 이 31초 안일 때만 요청(`DiscoverTimeline{Since: OverlapAnchor(5s), Limit: 100}` → 위 새 글 수 → `SetDiscoverNew`)·재무장, 아니면 멈췄다가 다음 `Update` 에서 다시 건다.
- 빈 상태: 팔로잉 `widgets.Empty(t, size, "🌱", "팔로우한 사람의 글이 여기 모여요", "1 로 전체 피드 보기")`, 발견 `"아직 글이 없어요", "c 로 첫 글을 써 보세요"`.
- `Init`: 창이 비었으면 첫 페이지 + `StoryFeed`, `SetOpen("timeline:"+kind, true)`, `SetOpen("stories", true)`. `StoriesStale()` 이면 다음 `Update` 에서 `StoryFeed`. `Update` 첫머리에서 `forwardResync`(Task 2 전달자 규칙).
- `timeline.item_created` 방송을 받으면 새 항목 `widgets.Fade.Start`.
- 차단: Task 5 차단 필터 규칙을 따른다(타임라인 창은 리듀서가 이미 거르지만 렌더 콜백의 `hiddenByBlock` 검사는 그대로 둔다).
- **이미지 [결정]** (`imageSlots`, 계약상 `View` 는 Cmd 를 낼 수 없고 core 는 View 뒤에 Update 를 보장하지 않으므로 **Raw 는 Update 안에서 계산해 그 자리에서 돌려준다**):
  - 요청: 페이지 결과·실시간 삽입·재동기화로 항목이 들어오는 `Update` 에서 `request` 로 각 글 첫 미디어의 `loadImage` 를 낸다(ID 당 한 번). `View` 는 요청하지 않는다.
  - 도착: `ResultMsg[image.Image]`(태그 `feed.media:<id>`)를 받은 `Update` 가 그 미디어를 쓰는 항목마다 `imageBox(m, 마지막 Resize 폭-2, ctx.Density())`(폭·밀도만의 순수 함수라 Update 에서 계산 가능)로 칸 수를 구해 `render` → kitty 면 `Setup` 이 든 `tea.Raw` 를 **그 Update 의 반환값으로** 낸다. 뒤에 다른 메시지가 오지 않아도 이미지가 뜬다.
  - 모드·테마: `ui.ModeChangedMsg`(앱이 Resize 를 먼저 부른 뒤 방송, 5.3.4)·`ui.ThemeChangedMsg`(BG 변경) 를 받으면 불러온 이미지가 있는 항목 전부를 새 칸 수·BG 로 `render` 해 새 조합의 `Setup` 을 낸다.
  - 그 밖의 크기 변경: 같은 모드 안 Resize 는 Update 를 부르지 않는다(5.3.4 1단계). 카드 이미지 칸은 Full 48·Bubble 30 상한이라 같은 모드 안에서는 사실상 바뀌지 않지만, 바뀌는 경우에도 `View` 의 `cell` 은 `Renderer.Peek` 로 그려 **아직 전송 안 된 조합은 하프블록**(깨지지 않음)으로 보이고 want-list 에 남긴다. 모든 `Update` 끝에서 `flush` 가 그것을 `Render` 해 Raw 로 내보내고, 다음 View 부터 자리표시자로 바뀐다.
  - `Image` 콜백 = `imageSlots.cell`; kitty 가 아니면 `Peek` 는 늘 `ready` 라 하프블록·빈 칸 그대로.
- `WatchTopics()` = 마지막 `View` 에서 보인 카드의 `post:<id>`(`cardAt` 과 같은 방식으로 `Anchor()` 부터 카드 높이를 더해 계산). `Titled` = `"피드"`. `Helper` = `l 좋아요 · b 리포스트 · r 답글 · c 새 글`. `Persistent` = `{Selected, Anchor}`.

- [ ] **Step 1: Write the failing tests**

```go
func feedOpts(t *testing.T, arg string, hostile bool, so ...snsOpt) []uitest.Option {
	srv := snsAPI(t, hostile, so...)
	seed := fixtures.Seed; if hostile { seed = func(s *store.Store) { fixtures.Seed(s); fixtures.Hostile(s) } }
	return []uitest.Option{uitest.WithAPI(srv), uitest.WithSeed(seed), uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: arg})}
}
func TestTimelineGolden(t *testing.T) {
	sweep(t, uitest.AllSizes(), "following", feedOpts(t, "following", false), nil)
	sweep(t, uitest.AllSizes(), "discover", feedOpts(t, "discover", false), nil)
}
func TestTimelineHostile(t *testing.T) { sweep(t, uitest.AllSizes(), "", feedOpts(t, "following", true), nil) }
func TestCommandModeShowsFeedColumnBesideChat(t *testing.T) {
	// 200x60: h.Model.Frame().Routes[layout.SlotFeed].Kind == ui.RouteFeed; Routes[layout.SlotMain].Kind != ui.RouteFeed
	// p, _ := h.Model.Frame().Layout.Pane(layout.SlotFeed); joined region(h.Screen(), p.Rect) contains "피드", "팔로잉", "#트렌드", "@hana", "♥ 12  ↻ 3  💬 4"
}
func TestTimelineInfiniteScroll(t *testing.T) // page1 next_cursor "c2": h.Press("G") -> lastReq GET /v1/timeline/following has Query "cursor=c2"
func TestDiscoverNewPostsBanner(t *testing.T) {
	// discover (default discoverNew(): 2 new + 1 overlap); h.Advance(30*time.Second) -> request Query contains "since="; screen contains "새 글 2개 · enter" (overlap not counted)
	// h.Press("g","g") -> banner selected (SelectedID == ""), h.Press("enter") -> GET /v1/timeline/discover WITHOUT "since"; banner gone; SelectedID == first card
}
func TestDiscoverBannerKeys(t *testing.T) // banner shown, first card selected: "k" -> banner selected; "j" -> first card; "l" while banner selected -> no PUT like
func TestDiscoverBannerGap(t *testing.T)  // withDiscoverSince(TimelinePage{Gap: true}); Advance(30s) -> screen contains "새 글 100개+"
func TestSnsHarnessNoStrayRequests(t *testing.T) {
	// feedOpts(following) at 120x40 FrontFeed; h.Advance(6*time.Second) (past the 5s ready fallback => REST bootstrap may run)
	// catch-all saw nothing (it t.Errorf's itself); h.Model.Frame().Toast == nil; every h.API.Requests() path is one snsAPI registered
}
func TestTimelineKittyRawWithoutFollowUp(t *testing.T) {
	// Review Focus 6. feedOpts(following) at 120x40 (WithRoute puts the feed surface in front); then h.Context().Caps.Graphics = termcaps.GraphicsKitty
	// m := postBy("hana").Media[0]; n := len(h.TeaMsgs())
	// h.Send(ui.ResultMsg[image.Image]{Tag: "feed.media:" + m.ID, Value: fixtures.TestImage()})  // the image result — NOTHING is sent after it
	// some tea.RawMsg in h.TeaMsgs()[n:] whose fmt.Sprint(Msg) has prefix "\x1b_G" and contains "a=T"; strings.ContainsRune(h.Screen(), kitty.Placeholder)
	// h.Resize(200, 60) (mode change -> ModeChangedMsg): a new RawMsg for the 48-col box appears without any further message
}
func TestCardClickWhenScrolled(t *testing.T) {
	// feedOpts(t, "following", false, withFollowing(longTimeline())) at 80x24 FrontFeed; h.Press("G") (select last = dev_kim, list scrolls)
	// if AnchorID(inst) == postBy("dev_kim").ID { t.Fatal("precondition: dev_kim card must not be the first visible card") }
	// x, y := cellOf(t, h.Screen(), "#golang"); h.Click(x+1, y) -> Nav().Feed top == {RouteHashtag, ID "golang"}
	// h.Press("esc"); click on the stats line of the first visible card -> selection moves to that card (List.HandleMouse), no navigation
}
func TestDiscoverPollingStopsWhenHidden(t *testing.T) // navigate main to a channel at 120x40 (feed not visible), Advance(5m) => no further /discover requests after the first stop
func TestTimelineWatchTopics(t *testing.T)   // h.Instance("feed//following").(ui.Watcher).WatchTopics() contains "post:"+postBy("hana").ID
func TestTimelineLiveInsert(t *testing.T)    // h.Event("timeline.item_created", map[string]any{"item": newItem}) -> first card shows newItem body
func TestTimelineTabsKeys(t *testing.T)      // "2" -> Nav().Feed[0] == {RouteFeed, Arg "following"}; "3" -> top Kind RouteTrends
func TestTimelineSelectionAcrossModes(t *testing.T) {
	// 120x40 FrontFeed: Press("j","j"); id := SelectedID(h.Instance("feed//following"))
	// for each size in {200x60,120x30,80x24,50x20,120x40}: h.Resize; h.Check(); SelectedID unchanged; same instance pointer
}
func TestStoryRingOpensStories(t *testing.T) // Press("s") -> Nav().Feed top == {RouteStories, ID: hana id}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/feed/ -run 'Timeline|CommandMode|Discover|StoryRing|SnsHarness|CardClick' -v`
Expected: FAIL — `undefined: newTimeline` / 골든 없음

- [ ] **Step 3: Implement `timeline.go`, `feedtabs.go`, `images.go`, test helpers**

- [ ] **Step 4: Generate goldens, inspect, re-run**

Run: `go test ./internal/ui/feed/ -run TestTimelineGolden -update && go test ./internal/ui/feed/ -v`
Expected: PASS. `testdata/TestTimelineGolden/200x60/following.golden` 의 피드 열 카드 구성(`@hana · 3분`, 이미지, `♥ 12  ↻ 3  💬 4` / `@dev_kim · 10분`, `#golang`, `♥ 5  💬 2`)과 탭 문구 `피드 / 팔로잉 / #트렌드` 가 `layouts.html` ①과 같은지 확인한다 — **단 켜진 탭은 시안(`피드`)과 다르게 `팔로잉` 이어야 한다**(기본 `Feed[0]` = following, 위 탭 매핑). `피드` 가 켜진 모습은 `200x60/discover.golden` 이 시안 ①과 같아야 한다. `80x24` 가 위젯 `피드` 탭, `50x20` 이 초소형 한 줄 카드인지 확인.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/feed/
git commit -m "feat(feed): timeline route with tabs, story ring, discover polling and command-mode column"
```

---

### Task 7: 글 선택 동작 — 좋아요·리포스트·인용·답글·메뉴·신고·차단

**Files:**
- Create: `internal/ui/feed/actions.go`, `internal/ui/feed/overlays.go`
- Modify: `internal/ui/feed/timeline.go` (HandleKey/HandleMouse 를 `handlePostKey`/`handlePostHit` 로 위임)
- Test: `internal/ui/feed/actions_test.go`, `internal/ui/feed/testdata/TestPostOverlaysGolden/**`

**Interfaces:**
- Consumes: `ctx.Keys.Matches(msg, "post.like"|"post.repost"|"post.quote"|"post.reply"|"post.open_media"|"post.author"|"msg.actions")`, `api.(*Client).{Like, Unlike, Repost, Unrepost, ListLikes, ListReposts, ListQuotes, DeletePost, Block, ListFollowers, ListFollowing, ListStoryViewers, RemoveFollower}`, `api.UserID`, `ctx.Store.Blocks`, `FeedStore.HideAuthor`, `cardAt`, `hiddenByBlock`, `widgets.{ReportDialog, Confirm}`, `ui.{RegisterOverlay, OpenOverlay, OpenRegisteredOverlay, Toast, ErrorMsg, Back}`, `api.Describe`.
- Produces:

```go
func handlePostKey(ctx *ui.Context, msg tea.KeyPressMsg, p *api.Post) (bool, tea.Cmd) // l b q r o u m '#'
func handlePostHit(ctx *ui.Context, h cardHit, p *api.Post) tea.Cmd                  // hashtag -> RouteHashtag; author -> RouteProfile; image -> feed.media; quote -> RoutePost; poll -> Task 10
func toggleLike(ctx *ui.Context, p *api.Post) tea.Cmd    // optimistic; tag "feed.like:<id>"
func toggleRepost(ctx *ui.Context, p *api.Post) tea.Cmd  // optimistic; tag "feed.repost:<id>"
// overlays (init):
//   "feed.post_actions" args=<postID>
//   "feed.user_list"    args="likes <postID>"|"reposts <postID>"|"followers <userRef>"|"following <userRef>"|"story_viewers <storyID>"
//   "feed.quotes"       args=<postID>
//   "feed.hashtag_pick" args=<postID> (2+ hashtags)
```

동작 **[결정]**:
- `l`: 즉시 `viewer.liked` 토글·`like_count ±1`, 결과의 `LikeCount` 로 확정, 오류면 되돌리고 `ui.ErrorMsg{Err, Context: "좋아요"}`. `b`: 같은 방식(`RepostCount`).
- 작성자가 `is_private` 이면 `b`·`q` 는 요청 없이 `ui.Toast("비공개 계정의 글은 리포스트·인용할 수 없어요", ui.ToastInfo)`. 삭제된 글이면 모든 동작이 `ui.Toast("삭제된 글이에요", ui.ToastInfo)`.
- `q` → `ui.Navigate(Route{Kind: RouteCompose, Arg: "post", ID: p.ID, Query: {"mode": "quote"}})`, `r` → 같은 경로 `{"mode": "reply"}`(두 경로는 `Route.Key()` 가 같아 **한 작성기 인스턴스**를 쓴다 — 캐시된 인스턴스로 갈 때 앱이 보내는 `ui.RouteUpdatedMsg` 로 모드를 바꾸는 규칙은 Task 9), `u` → `RouteProfile{ID: p.Author.ID}`, `o` → `ui.OpenRegisteredOverlay("feed.media", p.ID+" 0")`(미디어 없으면 무시), `#` → 해시태그 1개면 `RouteHashtag`, 여러 개면 `feed.hashtag_pick`.
- `m` → `feed.post_actions` 목록(순서 고정): `좋아요 누른 사람`, `리포스트한 사람`, `인용한 글`, `작성자 프로필`, (내 글) `글 삭제`(Confirm `이 글을 삭제할까요?` → `DeletePost` → `post.deleted` 와 같은 로컬 처리), (남의 글) `글 신고`(`widgets.ReportDialog("post", id, "<이름>의 글")`), `작성자 차단`(Confirm `<이름>님을 차단할까요? 서로의 글이 보이지 않게 돼요` → `ui.Do` 태그 `feed.block:<userID>` 로 `Block(api.UserID(id))` → 성공(204)한 그 `ResultMsg` 를 받은 즉시 **이벤트를 기다리지 않고** 로컬 반영: `ctx.Store.Blocks[id] = ctx.Now()`(맵이 nil 이면 만든다) + `feedstore.Get(ctx.Store).HideAuthor(id)` + `ui.Toast("차단했어요", ui.ToastSuccess)`. 같은 `ResultMsg` 가 모든 컴포넌트에 방송되므로 화면별 목록은 Task 5 차단 필터 규칙대로 그 사용자 항목을 지운다. 로컬 반영은 그 `Do` 를 낸 컴포넌트가 하고, 뒤이어 오는 `block.updated` 는 같은 상태를 다시 만들 뿐이다(멱등).
- `feed.user_list`: 제목 `좋아요 누른 사람`/`리포스트한 사람`/`팔로워`/`팔로잉`/`본 사람`, `widgets.List` + 무한 스크롤, `enter` = 닫고 `RouteProfile`. `Bounds` = 가운데 `min(48, W-4) × min(20, H-2)`. 차단한 사용자 줄은 Task 5 차단 필터 규칙대로 숨긴다. `feed.quotes` 도 같은 규칙(`hiddenByBlock`).
- **팔로워 삭제 [결정]** (`RemoveFollower` 의 유일한 UI 소비자): `feed.user_list` 가 `followers <userRef>` 이고 그 userRef 가 나(`@me` 또는 `ctx.Me().ID`)일 때만 `x` = 선택한 사람 빼기 — `widgets.Confirm("@<handle>님을 팔로워에서 뺄까요?", "내 글이 그 사람 팔로잉 피드에 더는 오지 않아요.", "빼기", "취소", …)` → `ui.Do` 태그 `feed.remove_follower:<userID>` 로 `RemoveFollower(api.UserID(id))`(`DELETE /v1/users/{user}/follower`, 204; 대기 요청이면 거절과 같음 — api.md 5.5) → 성공 시 그 줄 제거 + `ui.Toast("팔로워에서 뺐어요", ui.ToastSuccess)`, 오류는 `ui.ErrorMsg{Err, Context: "팔로워 삭제"}`. 남의 팔로워 목록·다른 종류 목록에서는 `x` 를 무시하고 도움말(`Helper`)에도 넣지 않는다. 내 팔로워 목록의 `Helper` = `enter 프로필 · x 팔로워에서 빼기`.
- 카드 클릭은 Task 6 `cardAt` 으로 카드·줄을 찾은 뒤 `handlePostHit` 로 넘긴다(이 Task 에서 `timeline.go` 의 `HandleMouse` 를 그렇게 바꾼다).

- [ ] **Step 1: Write the failing tests**

```go
func TestLikeOptimistic(t *testing.T) {
	// unit part: p := postBy("hana"); cmd := toggleLike(ctx, &p) => p.Viewer.Liked && p.LikeCount == 13 before cmd runs
	// harness part: srv.JSON("PUT /v1/posts/{id}/like", 200, api.LikeResult{PostID: id, Liked: true, LikeCount: 20})
	//   Press("l") -> lastReq PUT "/v1/posts/<id>/like" exists; screen contains "♥ 20"
}
func TestLikeFailureReverts(t *testing.T)            // srv.Error(PUT like, 429, "rate_limited") -> screen "♥ 12" and toast text == api.Describe(err) first 20 chars
func TestRepostQuotePrivateBlocked(t *testing.T) {   // Review Focus 1
	// author IsPrivate=true; Press("b") then Press("q"): no PUT/compose; screen contains "비공개 계정의 글은 리포스트·인용할 수 없어요"; Nav().Feed top still RouteFeed
}
func TestQuoteAndReplyNavigate(t *testing.T)         // q -> Feed top == {RouteCompose,"post",id,{"mode":"quote"}}; esc (leave input), esc (empty draft => Back to feed); r -> top {"mode":"reply"} and h.Instance("compose/"+id+"/post") is the same instance as after q
func TestAuthorAndHashtagNavigate(t *testing.T)      // dev_kim selected: "#" -> {RouteHashtag, ID "golang"}; esc; "u" -> {RouteProfile, ID dev_kim id}
func TestHashtagClick(t *testing.T)                  // Click on "#golang" cell (located via strings.Index on h.Screen() lines) -> RouteHashtag golang
func TestPostMenuReport(t *testing.T)                // m, select "글 신고", enter -> screen contains "신고" and reason "스팸"; Frame().Overlays len 1
func TestPostMenuBlockAuthor(t *testing.T)           // srv.Handle("PUT /v1/blocks/{user}", 204); m, "작성자 차단", enter, confirm "y" -> PUT /v1/blocks/<hana id>; with NO block.updated event: screen has no "@hana" card, ctx.Store.IsBlocked(hana)==true
func TestPostMenuDeleteMine(t *testing.T)            // my post -> "글 삭제" present, "글 신고" absent; confirm -> DELETE /v1/posts/<id>; card gone
func TestUserListRemoveFollower(t *testing.T)        // srv.JSON("GET /v1/users/{user}/followers", 200, page [junho, hana]); srv.Handle("DELETE /v1/users/{user}/follower", 204); OpenOverlayMsg{ID:"feed.user_list", Args:"followers @me"}; x, y -> DELETE /v1/users/<junho id>/follower; junho row gone; toast "팔로워에서 뺐어요"
func TestUserListRemoveFollowerOnlyMine(t *testing.T) // Args "followers @hana": x -> no DELETE request, no confirm overlay
func TestPostOverlaysGolden(t *testing.T) {
	sizes := append(append([]uitest.Size{}, uitest.Required...), uitest.Size{W: 44, H: 16}, uitest.Size{W: 20, H: 6})
	sweep(t, sizes, "post_actions", feedOpts(t, "following", false), func(h *uitest.Harness) { h.Press("m") })
	sweep(t, sizes, "likes", feedOpts(t, "following", false), func(h *uitest.Harness) { h.Send(ui.OpenOverlayMsg{ID: "feed.user_list", Args: "likes " + postBy("hana").ID}) })
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/feed/ -run 'Like|Repost|Quote|Author|Hashtag|PostMenu|PostOverlays|UserList' -v`
Expected: FAIL — `undefined: handlePostKey`

- [ ] **Step 3: Implement `actions.go`, `overlays.go`; delegate from `timeline.go`**

- [ ] **Step 4: Generate goldens, run**

Run: `go test ./internal/ui/feed/ -run TestPostOverlaysGolden -update && go test ./internal/ui/feed/ -v`
Expected: PASS (20×6 골든은 테두리 없는 전체 본문 강등 화면)

- [ ] **Step 5: Commit**

```bash
git add internal/ui/feed/
git commit -m "feat(feed): post actions, menu, report/block and user list overlays"
```

---

### Task 8: 글 상세 `RoutePost` — 조상과 답글 트리

**Files:**
- Create: `internal/ui/feed/post.go`
- Test: `internal/ui/feed/post_test.go`, `internal/ui/feed/testdata/TestPostGolden/**`

**Interfaces:**
- Consumes: `api.(*Client).{GetPostContext, ListReplies}`, `FeedStore.{ObservePosts, Post, Replies, HasReplies, SetOpen}`, `handlePostKey`, `handlePostHit`, `renderCard`, `humanize.MessageTime`.
- Produces:

```go
func newPost(ctx *ui.Context, r ui.Route) ui.Component // init(): ui.RegisterRoute(ui.RoutePost, newPost)
type treeRow struct{ Kind rowKind; PostID string; Depth int; ParentID string; Cursor string } // rowKind: rowAncestor|rowMain|rowReply|rowMore
func flattenTree(f *feedstore.FeedStore, rootID string, expanded map[string]bool) []treeRow
```

동작 **[결정]**:
- 위: 조상(루트부터, `DensityLine` 한 줄씩, 왼쪽 `│`, dim), 가운데: 본 글(선택 카드, 시각은 `humanize.MessageTime`), 구분 `── 답글 N ──`, 아래: 답글 트리.
- 트리 들여쓰기 3칸/단계, 가지 `├─ `·`└─ `·`│  `, 최대 깊이 6(더 깊으면 `↳ 이어서 보기` 줄 → `RoutePost{ID: 그 답글}`). 다음 페이지가 있으면 그 단계 끝에 `답글 더 보기` 줄.
- 키: `enter` = 선택한 답글을 `RoutePost` 로, `space`·`→` = 하위 답글 펼치기(처음이면 `ListReplies(child)`), `space`·`←` = 접기, `답글 더 보기` 에서 `enter` = 다음 페이지. 나머지 글 키는 `handlePostKey`.
- `post.reply_created`(펼친 부모) → 끼워 넣기, `post.deleted`(본 글) → 본문 `삭제된 글이에요`, 동작 막힘. `SetOpen("post:"+id, true)`. `Update` 첫머리 `forwardResync`(재동기화 결과로 본 글·답글 첫 페이지가 바뀌면 `flattenTree` 를 다시 하고 선택은 ID 로 유지). 차단한 사용자의 답글 줄은 숨긴다(리듀서가 답글 창에서 지우고, 렌더 때도 `IsBlocked` 검사). 이미지는 Task 6 `imageSlots`. 답글 줄 클릭은 `cardAt` 과 같은 방식(행 높이 누적)으로 찾는다. `Titled` = `"글"`. `WatchTopics` = 본 글 + 보이는 답글.

- [ ] **Step 1: Write the failing tests**

`replyTree()`·`deepChain()` 은 Task 5 `snsfix_test.go` 에 이미 있다(여기서 새로 만들지 않는다).

```go
func postOpts(t *testing.T, id string, hostile bool, so ...snsOpt) []uitest.Option // like feedOpts but WithRoute(ui.Route{Kind: ui.RoutePost, ID: id})
func TestPostGolden(t *testing.T)   { sweep(t, uitest.AllSizes(), "hana", postOpts(t, postBy("hana").ID, false), nil) }
func TestPostHostile(t *testing.T)  { sweep(t, uitest.AllSizes(), "", postOpts(t, fixtures.HostileTimeline()[0].Post.ID, true), nil) }
func TestReplyTreeExpand(t *testing.T) // select junho reply, Press("space") -> GET /v1/posts/<junho reply>/replies; screen has "│  ├─" and "│  └─"
func TestReplyTreeMoreRow(t *testing.T) // first replies page next_cursor "r2" -> "답글 더 보기" row; enter -> Query "cursor=r2"
func TestReplyCreatedEventInserts(t *testing.T) // h.Event("post.reply_created", {parent_id, post}) -> new reply visible
func TestOpenPostDeletedEvent(t *testing.T) {   // Review Focus 5
	// h.Event("post.deleted", map[string]any{"post_id": id}); h.Check(); screen contains "삭제된 글이에요"
	// select main post, Press("l") -> no PUT /like recorded; toast "삭제된 글이에요"
}
func TestDeepReplyOpensNewRoute(t *testing.T) // depth-7 chain -> "↳ 이어서 보기" row; enter -> Feed top RoutePost of that reply
func TestResyncRefreshesPostWhenOnlyPostAlive(t *testing.T) {
	// edits := &postEdits{}; h := uitest.New(t, postOpts(t, hanaID, false, withPostEdits(edits))...); h.Resize(120, 30) (compact: feed surface not shown)
	// precondition h.Instance("feed//following") == nil (Fatal otherwise — the test must prove RoutePost alone forwards)
	// after the first render: edits.Body[hanaID] = "오늘 첫 눈 (수정)"; edits.ExtraReplies[hanaID] = []api.Post{reply} where reply = mkPost(35, "junho", "새 답글", 0) with ReplyToID/RootID = hanaID
	// sendCmd(h, feedstore.Get(h.Context().Store).Resync(h.Context().Store, h.Context().API))
	// screen contains "오늘 첫 눈 (수정)" and "새 답글"
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/feed/ -run 'TestPost|Reply|OpenPost|DeepReply|ResyncRefreshes' -v`
Expected: FAIL — `undefined: newPost`

- [ ] **Step 3: Implement `post.go`**

- [ ] **Step 4: Generate goldens, inspect, run**

Run: `go test ./internal/ui/feed/ -run TestPostGolden -update && go test ./internal/ui/feed/ -v`
Expected: PASS; `120x40/hana.golden` 의 트리 선이 어긋나지 않는지 확인.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/feed/post.go internal/ui/feed/post_test.go internal/ui/feed/testdata/TestPostGolden
git commit -m "feat(feed): post detail with ancestors and expandable reply tree"
```

---

### Task 9: 작성기 `RouteCompose` — 글(500자·사진 4장·투표·답글·인용)과 스토리

**Files:**
- Create: `internal/ui/feed/compose.go`, `internal/ui/feed/attach.go`
- Test: `internal/ui/feed/compose_test.go`, `internal/ui/feed/testdata/TestComposeGolden/**`, `internal/ui/feed/testdata/TestAttachOverlayGolden/**`

**Interfaces:**
- Consumes: `widgets.{NewComposer, Composer.Value/SetValue/Reset/Focus/Blur/Focused/Update/View/Height/Cursor}`, `slash.Parse` (`Command{Name: slash.Poll, Poll *api.PollInput}`), `ids.New`, `imgrender.PrepareUpload`, `api.(*Client).{Upload, CreatePost, CreateStory, DeleteUpload, StoryFeed}`, `api.CreatePostRequest{ClientNonce, Body, MediaUploadIDs, ReplyToID, QuoteOfID, Poll}`, `api.CreateStoryRequest{Body, MediaUploadID, BG}`, `theme.(*Theme).StoryBG(slot)`, `widgets.{PollView, Confirm, Spinner}`, `textinput`, `ui.RouteUpdatedMsg`, `forwardResync`.
- Produces:

```go
func newCompose(ctx *ui.Context, r ui.Route) ui.Component // init(): ui.RegisterRoute(ui.RouteCompose, newCompose); implements InputCapturer, Cursorer, Persistent
type attachment struct{ Name string; Size int64; Upload *api.Upload; Err error }
// overlay "feed.attach" args = "post"|"story": textinput for a file path ("~/" expanded), enter -> PrepareUpload -> Upload(purpose, ct, bytes.NewReader, size)
type attachedMsg struct{ Route string; A attachment } // delivered back to the compose instance keyed by Route.Key()
```

동작 **[결정]**:
- 머리: `새 글` / `답글 · @hana`(부모 카드 `DensityLine` 1줄) / `인용`(인용 대상 카드 2줄) / `새 스토리`. 입력: `widgets.NewComposer("무슨 생각을 하고 있나요?")`, 스토리는 `"스토리에 짧게 남겨 보세요 · 24시간 뒤 사라져요"`. 오른쪽 아래 글자 수 `N/500`(스토리 `N/200`), 넘으면 `S.Error`.
- 첨부 줄: `🖼 photo.jpg 1.2MB ✓` 칩, 올리는 중 `widgets.Spinner`, 실패 `✕ 업로드 실패`. 글 4장 넘으면 `ui.Toast("사진은 4장까지예요", ui.ToastInfo)`, 스토리는 1장(바꿔 끼움).
- 투표: 본문이 `/poll` 로 시작하면 `slash.Parse` 결과를 입력창 아래에 `widgets.PollView` 미리보기(0표)로, 오류면 그 한국어 사용법을 `S.Warning` 로. 보낼 때 `Poll=cmd.Poll`, `Body=""`. 답글에 투표 → `ui.Toast("답글에는 투표를 붙일 수 없어요", ui.ToastInfo)`.
- 입력 중 키: `enter`(`compose.send`) 보내기, `ctrl+o` 사진 첨부(`feed.attach`), `esc` 입력 벗어나기. 입력 밖: `i`/`a` 입력으로, `o` 첨부, `x` 마지막 첨부 빼기(`DeleteUpload`), `b` 스토리 배경 순환 `accent→accent2→accent3→accent4→plain`, `esc` = 초안이 있으면 `widgets.Confirm("작성 중인 글을 버릴까요?", …, "버리기", "계속 쓰기", tea.Batch(reset, ui.Back()))`, 없으면 `ui.Back()`.
- 보내기: 빈 글(미디어·투표·인용 없음)·초과 글은 요청 없이 토스트(`500자를 넘었어요`/`내용을 입력해 주세요`). `client_nonce` = `ids.New()` 를 처음 보낼 때 만들어 성공 전까지 재사용. 성공: `ObservePosts`, 답글이면 `Replies(parent).Upsert`, 스토리면 `StoryFeed` 다시 받아 `SetStories`, `ui.Toast("올렸어요"|"스토리를 올렸어요", ui.ToastSuccess)`, `Reset` 후 `ui.Back()`.
- 스토리 미리보기: 입력 위에 `StoryBG(slot)` 배경 상자.
- **모드와 `RouteUpdatedMsg` [결정]**: 모드 = `Arg=="story"` 면 스토리, 아니면 `Query["mode"]`(`"quote"`|`"reply"`, 없으면 새 글). `Route.Key()` 가 `Query` 를 빼므로 같은 글을 인용·답글하는 두 경로는 **한 인스턴스**(`compose/<id>/post`)이고, 캐시된 인스턴스로 다시 갈 때 앱은 `ui.RouteUpdatedMsg{Route}` 만 보낸다(계약 5.3.3). 작성기는 그 메시지에서 새 `Query["mode"]` 가 현재 모드와 다르면: (1) 모드를 바꾸고 머리(`답글 · @hana` ↔ `인용`)를 다시 그린다, (2) 본문·첨부는 **그대로 둔다**(사용자가 친 글을 버리지 않음), (3) `client_nonce` 를 비운다(다음 보내기에서 새로 만듦 — 인용으로 실패했던 요청의 nonce 를 답글 요청에 재사용하지 않기 위해), (4) 답글 모드가 됐고 본문이 `/poll` 이면 미리보기 아래에 `답글에는 투표를 붙일 수 없어요`(S.Warning)를 보인다. 모드가 같으면 아무것도 안 한다. 보내기는 언제나 **현재** 모드로 `reply_to_id`/`quote_of_id` 를 정한다(인용 초안이 말없이 답글로 바뀌는 일은 머리가 바뀌어 보이므로 사용자가 안다).
- `Persistent.SaveState` = `{Mode, Body, Attachments, BG, Nonce}`.

- [ ] **Step 1: Write the failing tests**

```go
func composeOpts(t *testing.T, r ui.Route) []uitest.Option
func writePNG(t testing.TB) string // fixtures.TestImage() -> t.TempDir()/photo.png

func TestComposeGolden(t *testing.T) {
	sweep(t, uitest.AllSizes(), "post", composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"}), func(h *uitest.Harness) {
		h.Type("오늘 첫 눈 ❄️ 사무실 창밖 #첫눈"); h.Press("ctrl+o"); h.Type(writePNG(h.T)); h.Press("enter")
	})
	sweep(t, uitest.Required, "story", composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "story"}), func(h *uitest.Harness) { h.Type("점심 국밥") })
	sweep(t, uitest.Required, "quote", composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post", ID: postBy("hana").ID, Query: map[string]string{"mode": "quote"}}), nil)
}
func TestComposeCounterBlocksOver500(t *testing.T) // Type(strings.Repeat("가",501)) -> "501/500"; enter -> no POST /v1/posts; screen "500자를 넘었어요"
func TestComposeSendsPost(t *testing.T)            // Type("안녕"), enter -> POST body JSON client_nonce!="" && body=="안녕"; Nav back to feed; toast "올렸어요"
func TestComposeRetryKeepsNonce(t *testing.T)      // first 503 then 201: two POSTs with identical client_nonce
func TestComposeAttachLimit(t *testing.T)          // 4 attaches ok (4 POST /v1/uploads?purpose=post); 5th -> toast "사진은 4장까지예요", still 4 uploads; send -> media_upload_ids len 4
func TestComposePoll(t *testing.T)                 // Type(`/poll "점심" "국밥" "돈까스"`) -> preview shows "국밥"; enter -> body.poll.question=="점심", options==["국밥","돈까스"], body.body==""
func TestComposeReplyPollRejected(t *testing.T)    // mode reply + /poll -> no POST, toast "답글에는 투표를 붙일 수 없어요"
func TestComposeQuoteAndReplyFields(t *testing.T)  // quote -> quote_of_id==id; reply -> reply_to_id==id and new reply in Replies(id)
func TestComposeStory(t *testing.T)                // Arg story: Type("점심"); Press("esc","b","i","enter") -> POST /v1/stories {"body":"점심","bg":"accent2"}; toast "스토리를 올렸어요"
func TestComposeDraftSurvivesModesAndNavigation(t *testing.T) { // Review Focus 4
	// Type("초안"), attach 1; inst := h.Instance(key); for each AllSizes(): Resize, Check, same inst, Value()=="초안", chip present
	// Navigate to RouteFeed and back to same compose route -> same inst, draft and chip intact
}
func TestComposeEscConfirmsDiscard(t *testing.T)   // draft non-empty: esc, esc -> Confirm overlay; "n" keeps; esc,esc,"y" -> Back + empty draft
func TestComposeRouteUpdatedSwitchesMode(t *testing.T) {
	// open quote route for hana post; POST /v1/posts first 503: Type("안녕"), enter -> nonce N1 recorded; inst := h.Instance("compose/"+id+"/post")
	// h.Send(ui.NavigateMsg{Route: same route with Query{"mode":"reply"}}) -> h.Instance(key) == inst; screen header "답글 · @hana" (no "인용"); Value()=="안녕"
	// server now 201: enter -> body reply_to_id==id, no quote_of_id, client_nonce != N1
	// navigating again with {"mode":"reply"} (same mode) changes nothing (nonce kept)
}
func TestAttachOverlayGolden(t *testing.T)         // sizes Required+44x16+20x6, after Press("ctrl+o")
func TestAttachErrors(t *testing.T)                // missing file -> toast "파일을 찾을 수 없어요"; Upload 415 -> api.Describe toast; chip "✕ 업로드 실패"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/feed/ -run 'Compose|Attach' -v`
Expected: FAIL — `undefined: newCompose`

- [ ] **Step 3: Implement `compose.go` and `attach.go`**

- [ ] **Step 4: Generate goldens, inspect, run**

Run: `go test ./internal/ui/feed/ -run 'TestComposeGolden|TestAttachOverlayGolden' -update && go test ./internal/ui/feed/ -v`
Expected: PASS; 골든에서 `❄️` 가 `❄` 로 정규화되어 테두리가 맞는지, 커서 위치(`h.Cursor()`)가 입력 끝인지 확인.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/feed/
git commit -m "feat(feed): post and story composer with uploads, polls, reply and quote"
```

---

### Task 10: 피드 투표 — 투표·취소·마감·실시간 막대

**Files:**
- Create: `internal/ui/feed/poll.go`
- Modify: `internal/ui/feed/actions.go` (`handlePostHit` 의 `hitPollOption`, 메뉴에 `투표하기` 항목)
- Test: `internal/ui/feed/poll_test.go`, `internal/ui/feed/testdata/TestPollVoteOverlayGolden/**`

**Interfaces:**
- Consumes: `api.(*Client).{Vote, RetractVote, ClosePoll, GetPoll}`, `FeedStore.SetPoll`, `widgets.PollView`, `api.IsCode(err, "poll_closed")`.
- Produces: overlay `"feed.poll_vote"` args=`<postID>`; `func votePoll(ctx *ui.Context, p *api.Post, idxs []int) tea.Cmd` (tag `"feed.vote:<pollID>"`).

동작 **[결정]**: 카드의 선택지 칸 클릭 → 단일 선택 투표는 바로 `Vote([idx])`, 복수 선택은 그 선택지를 미리 체크한 오버레이. 오버레이: 선택지 `( )`/`(•)`(단일)·`[ ]`/`[x]`(복수), `j/k` 이동, `space` 고르기, `enter` 제출, `x` 내 투표 취소(`RetractVote`), 만든 사람이면 `c` 마감(`ClosePoll`, Confirm `투표를 마감할까요?`). 결과는 응답 `Poll` 을 `SetPoll` 로(내 표 권위). 마감됐으면 오버레이는 읽기 전용 `마감된 투표예요` + 막대. `409 poll_closed` → `ui.Toast("마감된 투표예요", ui.ToastInfo)` + `GetPoll` 로 갱신. 막대·내 표시 렌더는 `widgets.PollView` 에 맡긴다.

- [ ] **Step 1: Write the failing tests**

```go
func TestPollVoteSingleClick(t *testing.T)        // withFollowing([...longTimeline()[:3], itemOf(pollPost())]) so the poll card is not first; Press("G"); click "돈까스" cell (cellOf) -> cardAt finds the poll card -> PUT /v1/polls/<id>/vote body {"option_idxs":[1]}; bars show new counts
func TestPollVoteOverlayMulti(t *testing.T)       // multiPollPost(): m -> "투표하기" -> space on 0, j, space on 1, enter -> {"option_idxs":[0,1]}
func TestPollRetractAndClose(t *testing.T)        // pollPost: x -> DELETE /v1/polls/<id>/vote; myPollPost (creator = sam): c,y -> POST /v1/polls/<id>/close; overlay shows "마감된 투표예요"
func TestPollClosedConflict(t *testing.T)         // Vote -> 409 poll_closed -> toast "마감된 투표예요" + GET /v1/polls/<id>
func TestPollUpdatedEventKeepsMine(t *testing.T)  // Review Focus 2: after vote [0], h.Event("poll.updated", {poll: counts 9/2, my_votes: []}) -> screen shows "9" and my mark on 국밥 unchanged
func TestPollVoteOverlayGolden(t *testing.T)      // Required+44x16+20x6
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/feed/ -run Poll -v`
Expected: FAIL — `undefined: votePoll`

- [ ] **Step 3: Implement `poll.go`, wire into `actions.go`**

- [ ] **Step 4: Generate goldens, run**

Run: `go test ./internal/ui/feed/ -run TestPollVoteOverlayGolden -update && go test ./internal/ui/feed/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/feed/
git commit -m "feat(feed): feed poll voting, retract, close and live results"
```

---

### Task 11: 프로필 `RouteProfile` — 소개·상태·아바타·팔로우·비공개 요청·편집·차단/신고

**Files:**
- Create: `internal/ui/feed/profile.go`, `internal/ui/feed/profile_overlays.go`
- Test: `internal/ui/feed/profile_test.go`, `internal/ui/feed/testdata/TestProfileGolden/**`, `.../TestProfileOverlaysGolden/**`

**Interfaces:**
- Consumes: `api.(*Client).{GetUser, ListUserPosts, Follow, Unfollow, ListFollowRequests, AcceptFollowRequest, DeclineFollowRequest, UpdateProfile, Upload, Block}`, `api.{Handle, UserID, Me, Some, Null}`, `api.UpdateProfileRequest{DisplayName, Bio, AvatarUploadID}`, `widgets.{Avatar, PresenceDot, Tabs, ReportDialog, Confirm, Composer}`, `imgrender.PrepareUpload`, `mediaRenderer`, `loadImage`, `FeedStore.Stories`(머리의 스토리 링 표시), `ui.Navigate`.
- Produces:

```go
func newProfile(ctx *ui.Context, r ui.Route) ui.Component // init(): ui.RegisterRoute(ui.RouteProfile, newProfile)
func userRef(id string) api.UserRef                         // "@me"->api.Me, "@x"->api.Handle("x"), else api.UserID(id)
// overlays: "feed.follow_requests" (args ""), "feed.profile_edit" (args "")
type profileTab int; const (tabPosts profileTab = iota; tabReplies; tabMedia; tabGuestbook)
```

동작 **[결정]**:
- 머리(Full): 왼쪽 아바타(이미지면 `imgrender` 8×4 칸, 없으면 `widgets.Avatar(t, u, 8)`), 오른쪽 `이름 @handle ● 온라인`(presence nil 이면 점 없음), 상태 메시지 `🎧 듣는 중…`, 소개(최대 4줄), `팔로워 120 · 팔로잉 80 · 글 42`, 관계 칩 `[팔로우]`/`[팔로잉 ✓]`/`[요청됨]`·`친구`·`나를 팔로우해요`. Line/Bubble/Minimal 은 아바타 없이 2~3줄로 줄인다.
- 하위 탭(`widgets.Tabs`): `글 · 답글 · 미디어 · 방명록`, 키 `1`~`4`·클릭. 목록은 `ListUserPosts(ref, "posts"|"replies"|"media", page)` + `renderCard` + `handlePostKey`, 방명록 탭은 Task 12. 경로 `Query["tab"]=="guestbook"` 이면 방명록 탭으로 연다.
- `+` = 팔로우/언팔로우(`Follow` 결과 `active|pending` 로 칩 갱신, `[팔로잉 ✓]` 에서 `+` 는 Confirm `팔로우를 끊을까요?` 후 `Unfollow`), `m` = 메뉴: `스토리 보기`, `팔로워`, `팔로잉`(→ `feed.user_list`), 남이면 `차단`, `사용자 신고`(`ReportDialog("user", id, "@handle")`), 나면 `프로필 편집`, `팔로우 요청`. `e` = (나) 프로필 편집. `s` = `스토리 보기` = `ui.Navigate(Route{Kind: RouteStories, ID: <user id>})` — 팔로우하지 않은 공개 계정의 스토리도 볼 수 있게 하는 진입점이다(스토리 피드 `GET /v1/stories/feed` 는 팔로우한 사람만 담으므로; 보기 화면이 `ListUserStories` 로 채운다 — Task 13). 머리의 이름 앞에는 `FeedStore.Stories()` 에 그 사용자 그룹이 있고 `has_unseen` 이면 `◉`(acc), 본 그룹만 있으면 `○`(dim)를 붙인다(없으면 아무것도 — 그룹이 없어도 `s` 는 동작). `Blocked`·`Suspended`·`!CanViewPosts` 면 `s`·`스토리 보기` 는 요청 없이 무시.
- 상태: `!Relationship.CanViewPosts` 이고 `IsPrivate` → 목록 자리 `widgets.Empty(…, "🔒", "비공개 계정이에요", "팔로우 요청이 승인되면 글을 볼 수 있어요")`; `Blocked` → `"⊘", "차단한 사용자예요", "차단 해제는 ctrl+k → 차단 목록"`; `Suspended` → `"정지된 계정이에요"`. 내 비공개 프로필이면 머리에 `팔로우 요청 N`(첫 페이지 수, 클릭·`m` 으로 목록).
- `feed.follow_requests`: 목록 + `a` 수락 / `d` 거절, 처리 후 줄 제거. `feed.profile_edit`: 이름 `textinput`(1..32, 비면 `이름을 입력해 주세요`), 소개 `widgets.Composer`(0..160, `N/160`), 아바타 경로 `textinput`(비우면 그대로), `ctrl+x` 아바타 지우기 → `AvatarUploadID: api.Null[string]()`, `enter` 저장(바뀐 필드만 `Some`), 성공 시 `ctx.Store.Me` 의 이름·소개·아바타 갱신 + `ui.Toast("프로필을 저장했어요", ui.ToastSuccess)`.
- 탭 목록은 화면별 ID 목록이므로 Task 5 차단 필터 규칙(렌더 때 `hiddenByBlock` 건너뛰기, `block.updated`·`feed.block:*` 결과에 목록에서 지우기)을 따른다. 메뉴 `차단` 은 Task 7 과 같은 `feed.block:<id>` 경로·로컬 반영을 쓰고, 그 결과 이 프로필 자체는 `Blocked` 상태 화면으로 바뀐다. 이미지·아바타는 Task 6 `imageSlots`, 카드 클릭은 `cardAt`, `Update` 첫머리 `forwardResync`.
- `Titled` = `"@handle"`.

- [ ] **Step 1: Write the failing tests**

`profileOf`·`relFollowing`·`profilePosts`·`followRequests` 는 Task 5 `snsfix_test.go` 에 이미 있다.

```go
func profileOpts(t *testing.T, id string, hostile bool, mut func(*api.Profile)) []uitest.Option
func TestProfileGolden(t *testing.T) {
	sweep(t, uitest.AllSizes(), "hana", profileOpts(t, "@hana", false, nil), nil)
	sweep(t, uitest.Required, "private", profileOpts(t, "@hana", false, func(p *api.Profile){ p.IsPrivate = true; p.Relationship.CanViewPosts = false; p.Relationship.Following = "none" }), nil)
	sweep(t, uitest.Required, "me", profileOpts(t, "@me", false, nil), nil)
}
func TestProfileHostile(t *testing.T)            // hostile user (32-char name, long status) × AllSizes, h.Check()
func TestProfileFollowToggle(t *testing.T)       // Following "none": "+" -> PUT /v1/users/<id>/follow; reply {"state":"pending"} -> "[요청됨]"
func TestProfileUnfollowConfirms(t *testing.T)   // active: "+", "y" -> DELETE /v1/users/<id>/follow
func TestProfileTabsLoadFilters(t *testing.T)    // "2" -> GET /v1/users/<id>/posts Query has "filter=replies"; "3" -> "filter=media"
func TestProfileBlockAndReport(t *testing.T)     // m -> "차단", y -> PUT /v1/blocks/<id> (204, no event) -> screen "차단한 사용자예요"; m -> "사용자 신고" -> report overlay open
func TestProfileEditSave(t *testing.T)           // @me: e; change name to "새 이름"; enter -> PATCH /v1/me/profile body has "display_name":"새 이름" and no "avatar_upload_id" key
func TestProfileEditClearAvatar(t *testing.T)    // ctrl+x, enter -> body "avatar_upload_id":null
func TestProfileEditAvatarUpload(t *testing.T)   // path to PNG -> POST /v1/uploads?purpose=avatar then PATCH with avatar_upload_id=<upload id>
func TestFollowRequestsAcceptDecline(t *testing.T) // overlay: a -> POST /v1/me/follow-requests/<id>/accept; d -> .../decline; rows removed
func TestProfileOpenStories(t *testing.T)        // profile @dev_kim (no group in storyGroups()): "s" -> Nav().Feed top == {RouteStories, ID: dev_kim id}; private+!CanViewPosts profile: "s" -> no navigation
func TestProfileOverlaysGolden(t *testing.T)     // follow_requests, profile_edit × Required+44x16+20x6
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/feed/ -run 'Profile|FollowRequests' -v`
Expected: FAIL — `undefined: newProfile`

- [ ] **Step 3: Implement `profile.go`, `profile_overlays.go`**

- [ ] **Step 4: Generate goldens, inspect, run**

Run: `go test ./internal/ui/feed/ -run 'TestProfileGolden|TestProfileOverlaysGolden' -update && go test ./internal/ui/feed/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/feed/
git commit -m "feat(feed): profile route with follow, private requests, edit, block and report"
```

---

### Task 12: 방명록 — 프로필 탭과 `fun.guestbook_write`

**Files:**
- Create: `internal/ui/feed/guestbook_tab.go`, `internal/ui/fun/guestbook.go`, `internal/ui/fun/helpers_test.go`, `internal/ui/fun/snsfix_test.go`
- Test: `internal/ui/feed/guestbook_test.go`, `internal/ui/fun/guestbook_test.go`, 각 `testdata/**`

**Interfaces:**
- Consumes: `api.(*Client).{ListGuestbook, WriteGuestbook, DeleteGuestbookEntry}`, `api.GuestbookEntry{IsSecret, Hidden, Deleted, Author}`, `widgets.{Composer, ReportDialog, Confirm}`, `ui.{RegisterOverlay, OpenRegisteredOverlay, ResultMsg}`.
- 픽스처: `guestbookEntries(ownerID)` 는 Task 5 `snsfix_test.go`(ui/feed). ui/fun 시험은 패키지가 달라 쓸 수 없으므로 `internal/ui/fun/snsfix_test.go` 에 같은 내용의 `guestbookEntries` 를 둔다(ui/fun 시험이 쓰는 것은 이 하나뿐).
- Produces: overlay `"fun.guestbook_write"` args=`<owner user id>`; 성공 시 방송 메시지 `ui.ResultMsg[*api.GuestbookEntry]{Tag: "fun.guestbook_write:<ownerID>"}`(프로필 방명록 탭이 이 태그를 받아 맨 위에 넣는다). `func (p *profile) guestbookView(...)` 내부.

동작 **[결정]**:
- 탭: 차단한 작성자의 항목은 숨긴다(Task 5 차단 필터). 항목 `@handle 이름 · 3분` / 본문(`markup`, 해시태그 없음) / 비밀글이면 `🔒 비밀글` 칩, `Hidden` 이면 본문 대신 `비밀글이에요`(dim, 작성자 숨김 `누군가`), `Deleted` 는 생략. 키: `w` 쓰기(`OpenRegisteredOverlay("fun.guestbook_write", ownerID)`), `d` 삭제(주인 또는 작성자만, Confirm `방명록 글을 지울까요?`), `m` 신고(`ReportDialog("guestbook_entry", id, "<이름>의 방명록 글")`). 빈 상태 `"📖", "아직 방명록이 비어 있어요", "w 로 첫 글을 남겨 보세요"`.
- 오버레이: 제목 `방명록 남기기 · @owner`, `widgets.Composer`(1..300, `N/300`), 아래 토글 `[ ] 비밀글로 남기기`. `tab` 으로 입력↔토글 이동, 토글에서 `space` 반전, 입력에서 `enter` 제출. 비공개 계정 `403 private_account`·차단 `403 action_not_allowed` → `api.Describe` 토스트, 오버레이 유지. `Bounds` = 가운데 `min(56, W-4) × min(12, H-2)`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/ui/feed/guestbook_test.go
func TestGuestbookTabGolden(t *testing.T)     // profile @hana with Query{"tab":"guestbook"} × AllSizes; fixture: 3 entries incl. one Hidden
func TestGuestbookTabHostile(t *testing.T)
func TestGuestbookDeleteConfirm(t *testing.T) // own entry: d, y -> DELETE /v1/guestbook/<id>; row gone
func TestGuestbookReport(t *testing.T)        // m -> report overlay with target "guestbook_entry"
func TestGuestbookWrittenResultPrepends(t *testing.T) // h.Send(ui.ResultMsg[*api.GuestbookEntry]{Tag: "fun.guestbook_write:"+hanaID, Value: &e}) -> first row is e
// internal/ui/fun/guestbook_test.go (imports _ ".../internal/ui/feed" not allowed -> opens overlay directly)
func TestGuestbookWriteSubmits(t *testing.T) {
	// h.Send(ui.OpenOverlayMsg{ID: "fun.guestbook_write", Args: hanaID}); Type("놀러 왔어요"); tab; space; tab; enter
	// POST /v1/users/<hanaID>/guestbook body {"body":"놀러 왔어요","is_secret":true}; overlay closed; toast "방명록을 남겼어요"
}
func TestGuestbookWritePrivateError(t *testing.T) // 403 private_account -> toast api.Describe(...), overlay still open
func TestGuestbookWriteOverlayGolden(t *testing.T) // Required+44x16+20x6 after typing "놀러 왔어요"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/feed/ ./internal/ui/fun/ -run Guestbook -v`
Expected: FAIL — overlay id 미등록 / 탭 미구현

- [ ] **Step 3: Implement `guestbook_tab.go` (feed) and `guestbook.go` (fun)**

`ui/fun/helpers_test.go` 는 Task 6 의 `sweep`·`lastReq`·`sendCmd` 와 같은 시그니처를 이 패키지에 두고, 가짜 서버는 `funAPI(t *testing.T) *apitest.Server` = Task 6 `snsAPI` 의 **core 1단계 GET 10종 + catch-all** 부분만(`fixtures.Seed` 로 채운 Store 와 같은 값, sns GET 없음, `fixtures.RegisterAPI` 미사용 — Task 6 과 같은 이유)으로 만든다 — ui/fun 시험도 `h.Advance` 로 5초 부트스트랩 시점을 넘기므로(Task 16·17) 같은 이유로 필요하다. 쓰기 엔드포인트는 각 시험이 등록(시험 코드는 패키지 사이 공유 불가).

- [ ] **Step 4: Generate goldens, run**

Run: `go test ./internal/ui/feed/ ./internal/ui/fun/ -run Guestbook -update && go test ./internal/ui/feed/ ./internal/ui/fun/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/feed/ internal/ui/fun/
git commit -m "feat(fun): guestbook tab on profiles and write dialog"
```

---

### Task 13: 스토리 보기 `RouteStories`

**Files:**
- Create: `internal/ui/feed/stories.go`
- Test: `internal/ui/feed/stories_test.go`, `internal/ui/feed/testdata/TestStoryViewerGolden/**`

**Interfaces:**
- Consumes: `FeedStore.{Stories, PruneStories, MarkStorySeen, SetStories}`, `api.(*Client).{ViewStory, DeleteStory, StoryFeed, ListUserStories}`, `theme.(*Theme).StoryBG(slot)`, `markup` (Hashtags true), `mediaRenderer`, `ui.After`.
- Produces: `func newStories(ctx *ui.Context, r ui.Route) ui.Component` (init: `ui.RegisterRoute(ui.RouteStories, newStories)`); `type storyTickMsg struct{ Gen int }`.

동작 **[결정]**: **그룹 찾기** — `r.ID`(사용자 ID)의 그룹이 `FeedStore.Stories()` 에 있으면 그것을 쓴다. 없으면(팔로우하지 않은 공개 계정 — 프로필 `s`, Task 11) `ui.Do` 태그 `feed.user_stories:<userID>` 로 `ListUserStories(api.UserID(r.ID))`(`GET /v1/users/{user}/stories`, api.md 3.4 열람 규칙)를 불러 **이 화면만의 임시 그룹**(`StoryGroup{User: stories[0].Author, Stories, HasUnseen: 어느 하나라도 !Seen, LatestAt: 마지막 CreatedAt}`)을 만든다 — FeedStore 의 스토리 피드에는 넣지 않는다(피드 링은 팔로우 기준이므로). 받는 동안 `widgets.Spinner` + `불러오는 중…`, 빈 결과면 `ui.Toast("지금 볼 수 있는 스토리가 없어요", ui.ToastInfo)` + `ui.Back()`, `403 private_account` 등 오류는 `ui.ErrorMsg` + `ui.Back()`. 임시 그룹을 다 보면 다음 그룹으로 넘어가지 않고 `ui.Back()`. 그 그룹의 첫 안 본 스토리(없으면 첫 스토리)부터. 화면: 맨 위 진행 막대(스토리 수만큼 균등 분할, 지난·현재 `━`(acc), 남은 `─`(line)), `@hana 이름 · 3분`, 본문은 `StoryBG(bg)` 배경 상자 가운데, 미디어면 남은 칸에 맞춘 이미지. 표시할 때 `ViewStory`(id 당 1번, `feed.story_view:<id>`) + `MarkStorySeen`. 자동 넘김 5초(`ui.After(ctx, 5*time.Second, storyTickMsg{gen})`, 손으로 넘기면 세대 증가). `space`·`→`·`n` 다음, `←`·`p` 이전, 그룹 끝이면 다음 안 본 그룹, 모두 끝이면 `ui.Back()`. `m` 메뉴: 내 스토리 `본 사람`(`feed.user_list story_viewers <id>`, 수 `view_count`)·`삭제`(Confirm → `DeleteStory`), 남의 스토리 `신고`(`ReportDialog("story", id, "<이름>의 스토리")`)·`작성자 프로필`. `story.deleted` 가 현재 것이면 다음으로. 매 `View` 전 `PruneStories(ctx.Now())`. 차단한 사용자의 그룹은 건너뛴다(`IsBlocked`, Task 5 차단 필터). 미디어는 Task 6 `imageSlots`(크기는 남은 칸에서 Update 가 계산 가능 — 마지막 Resize 크기 기준), `Update` 첫머리 `forwardResync`. `Titled` = `"스토리"`.

- [ ] **Step 1: Write the failing tests**

`storyGroups()` 는 Task 5 `snsfix_test.go` 에 이미 있다(sam 본 것 1, hana 안 본 2 — 둘째는 이미지·bg `accent2`, junho 본 것 1, 만료 `02:05+20h`).

```go
func TestStoryViewerGolden(t *testing.T)     // RouteStories{ID: hana} × AllSizes
func TestStoryViewerHostile(t *testing.T)
func TestStoryAutoAdvance(t *testing.T)      // POST /v1/stories/<s1>/view; h.Advance(5*time.Second) -> second story shown + POST view s2
func TestStoryManualResetsTimer(t *testing.T) // Advance(4s), "n", Advance(4s) -> still on s2 (no double skip)
func TestStoryEndGoesBack(t *testing.T)      // last story of last unseen group: "n" -> Nav().Feed top RouteFeed
func TestStoryReport(t *testing.T)           // m, "신고" -> report overlay (target "story")
func TestStoryMineViewersAndDelete(t *testing.T) // own group: m -> "본 사람" opens user_list; "삭제", y -> DELETE /v1/stories/<id>
func TestStoryExpiredPruned(t *testing.T)    // h.Advance(21*time.Hour) -> feed ring no longer lists hana
func TestStoryDeletedEventSkips(t *testing.T) // h.Event("story.deleted", {user_id, story_id: current}) -> next story shown
func TestStoryUserFallbackLoads(t *testing.T) // RouteStories{ID: dev_kim id} (not in storyGroups()): srv.JSON("GET /v1/users/{user}/stories", 200, {items:[one dev_kim story]}) -> request path /v1/users/<dev_kim id>/stories; screen shows its body; "n" at the end -> ui.Back (Nav().Feed top back to previous), FeedStore.Stories() unchanged
func TestStoryUserFallbackEmpty(t *testing.T) // same with items [] -> toast "지금 볼 수 있는 스토리가 없어요"; Nav().Feed top is not RouteStories
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/feed/ -run Story -v`
Expected: FAIL — `undefined: newStories`

- [ ] **Step 3: Implement `stories.go`**

- [ ] **Step 4: Generate goldens, inspect, run**

Run: `go test ./internal/ui/feed/ -run TestStoryViewerGolden -update && go test ./internal/ui/feed/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/feed/
git commit -m "feat(feed): story viewer with auto-advance, report and delete"
```

---

### Task 14: 해시태그 피드 `RouteHashtag` · 트렌드 Top 10 `RouteTrends`(+ 해시태그·글 검색)

**Files:**
- Create: `internal/ui/feed/hashtag.go`, `internal/ui/feed/trends.go`
- Test: `internal/ui/feed/hashtag_test.go`, `internal/ui/feed/trends_test.go`, 각 `testdata/**`

**Interfaces:**
- Consumes: `api.(*Client).{HashtagPosts, Trends, SearchHashtags, SearchPosts}`, `api.{PostSearchQuery, PostSearchHit, HashtagSuggestion, PageOpts}`, `FeedStore.{SetTrends, Trends, ObservePosts}`, `renderCard`, `handlePostKey`, `handlePostHit`, `cardAt`, `feedTabs`, `humanize.{Clock, Count}`, `ui.{After, InputCapturer, Cursorer}`, `textinput`.
- Produces: `func newHashtag(ctx *ui.Context, r ui.Route) ui.Component`, `func newTrends(ctx *ui.Context, r ui.Route) ui.Component` (init 등록), `type trendsTickMsg struct{ Gen int }`, `type searchDebounceMsg struct{ Gen int; Q string }`.

동작 **[결정]**: 해시태그 — 제목 `#첫눈`, 카드 목록 + 무한 스크롤, 빈 상태 `"#", "이 태그의 글이 아직 없어요", ""`. 화면별 ID 목록이므로 Task 5 차단 필터 규칙, 이미지 `imageSlots`, 클릭 `cardAt`, `Update` 첫머리 `forwardResync`(트렌드도). 트렌드 — `feedTabs(ctx, "trends", w)` 줄(Line/Minimal 생략) + 10줄 `1  #첫눈   42개 글 · 30명`(1~3위 순위 acc 굵게), 아래 `11:00 기준`(`computed_at`, nil 이면 생략), `enter`/클릭 → `RouteHashtag{ID: tag}`, 5분마다 다시 받기(타임라인과 같은 `lastViewAt` 규칙), 빈 상태 `"📈", "아직 트렌드가 없어요", "글에 #태그를 달아 보세요"`.

**트렌드 화면의 검색 [결정]** (서버 `GET /v1/hashtags/search`·`GET /v1/search/posts`(plan 01 Task 29·plan 02 Task 17)와 core `SearchHashtags`·`SearchPosts` 의 UI 소비자. chat 의 `RouteSearch`·`ctrl+f` 는 메시지 검색 전용이라 쓰지 않고, CA 5.3 의 RouteKind 나 5.6 의 명령 id 를 새로 만들지 않는다 — 트렌드 화면이 Threads/X 의 "탐색" 처럼 트렌드 + 검색을 함께 맡는다):
- `/` = 탭 줄 아래(탭 줄이 없는 밀도면 맨 위)에 검색 줄 `🔍 ` + `textinput`(Global Constraints 의 입력 규칙, `ui.InputCapturer`·`ui.Cursorer` 구현) 을 열고 포커스. `esc` = 입력이 있으면 지우고, 비었으면 검색 줄을 닫고 Top 10 으로. 검색 줄이 열려 있는 동안 아래 목록은 검색 결과이며, `↓`/`tab` 으로 입력에서 결과 목록으로, 목록 첫 줄의 `k`/`↑` 로 입력으로 돌아간다.
- 질의는 `textwidth.SanitizeInput` 후 trim, 바뀔 때마다 세대를 올리고 `ui.After(ctx, 300*time.Millisecond, searchDebounceMsg{gen, q})` — 최신 세대만 요청(core 팔레트 사람 검색과 같은 디바운스). 길이 1..100 코드 포인트(api.md 0.3 검색어), 비면 요청 없이 Top 10 표시.
- `#` 로 시작하면 **해시태그 검색**: 나머지(앞 `#` 를 뗀 것)가 1..50 코드 포인트일 때(서버 plan 02 Task 17 의 `q` 규칙 — 51자 이상이면 요청 없이 안내 줄 `태그는 50자까지예요`) `ui.Do` 태그 `feed.search_tags:<q>` 로 `SearchHashtags(q[1:], 20)` → 줄 `#첫눈   24시간 글 42개`(`post_count_24h`, `humanize.Count`), `enter`/클릭 = `RouteHashtag{ID: tag}`. 결과에 질의와 정확히 같은 태그가 없으면 목록 **끝**에 `#<질의> 태그 피드 열기` 줄을 하나 더 두어(결과가 비면 그 줄만) 최근 7일에 안 쓰인 태그로도 갈 수 있게 한다.
- 그 밖은 **글 검색**: `ui.Do` 태그 `feed.search_posts:<q>` 로 `SearchPosts(PostSearchQuery{Q: q, PageOpts: {Limit: 20}})` → `hit.Post` 를 `ObservePosts` 후 `renderCard` 카드 목록(`handlePostKey`·`handlePostHit`·`cardAt`·`imageSlots`·무한 스크롤 `next_cursor` → `feed.search_posts_more:<q>`). `Highlights` 는 쓰지 않는다(카드 본문 그대로). 빈 결과 `widgets.Empty(…, "🔍", "검색 결과가 없어요", "#태그 로 시작하면 태그를 찾아요")`. 결과 ID 목록은 화면별 목록이므로 Task 5 차단 필터 규칙.
- `422 limit_exceeded`·`429` 등 오류는 `ui.ErrorMsg{Err, Context: "검색"}`. 검색 줄·질의·결과 선택은 `Persistent` 에 넣어 모드 전환·다른 화면 다녀오기 뒤에도 그대로.
- `Helper` = 검색 줄이 닫혀 있으면 `enter 태그 피드 · / 검색`, 열려 있으면 `#태그 로 태그 검색 · esc 닫기`.

- [ ] **Step 1: Write the failing tests**

```go
func TestHashtagGolden(t *testing.T)       // RouteHashtag{ID:"첫눈"} × AllSizes; request Path == "/v1/hashtags/첫눈/posts" (apitest records decoded path)
func TestHashtagHostile(t *testing.T)
func TestHashtagActionsWork(t *testing.T)  // l on first card -> PUT like
func TestTrendsGolden(t *testing.T)        // RouteTrends × AllSizes (Task 5 trendsTop10())
func TestTrendsRefreshEvery5m(t *testing.T) // initial GET /v1/trends; h.Advance(5*time.Minute) -> 2 requests
func TestTrendsEnterOpensHashtag(t *testing.T) // j, enter -> Nav().Feed top {RouteHashtag, ID "golang"}
func TestTrendsEmpty(t *testing.T)         // items [] -> "아직 트렌드가 없어요"
func TestTrendsHashtagSearch(t *testing.T) {
	// RouteTrends at 120x40; srv.JSON("GET /v1/hashtags/search", 200, {items:[{tag:"첫눈",post_count_24h:42},{tag:"첫눈사진",post_count_24h:3}]})
	// Press("/"); Type("#첫"); h.Advance(299*time.Millisecond) -> no request; Advance(1ms) -> one GET /v1/hashtags/search Query q=첫&limit=20
	// screen contains "#첫눈" and "글 42개"; Press("down","enter") -> Nav().Feed top == {RouteHashtag, ID "첫눈"}
}
func TestTrendsSearchDebounceLatestOnly(t *testing.T) // Type("#a") then within 300ms Type("b") -> exactly one request with q=ab
func TestTrendsPostSearch(t *testing.T) {
	// srv.JSON("GET /v1/search/posts", 200, {items:[{post: postBy("dev_kim"), highlights:[]}], next_cursor:null})
	// Press("/"); Type("bubbletea"); Advance(300ms) -> GET /v1/search/posts Query q=bubbletea&limit=20; screen contains "@dev_kim" card; Press("down","l") -> PUT /v1/posts/<id>/like
}
func TestTrendsSearchEmptyAndEsc(t *testing.T) // items [] -> "검색 결과가 없어요"; esc (clears), esc (closes) -> Top 10 list again, no further request
func TestTrendsSearchSurvivesModes(t *testing.T) // query "bubbletea" + results at 120x40 -> 80x24 -> 200x60 -> 120x40: query and selected result ID unchanged, h.Check() each step
func TestTrendsSearchGolden(t *testing.T)   // hashtag results and post results × uitest.Required
func TestBlockHidesInPerScreenLists(t *testing.T) {
	// Review Focus 3, per-screen lists (not FeedStore windows).
	// (a) RouteHashtag{ID:"첫눈"} at 120x40: screen contains "@hana" (hashtagPosts("첫눈")[0]) and "#첫눈 왔다" (minji)
	//     h.Event("block.updated", map[string]any{"user_id": hanaID, "blocked": true}) -> screen has no "@hana"; "#첫눈 왔다" still there; h.Check()
	// (b) RouteProfile{ID:"@dev_kim"} posts tab: contains dev_kim's own post and "↻ dev_kim님이 리포스트" of hana's post
	//     same event -> hana's reposted card gone (no "@hana", no "오늘 첫 눈"), dev_kim's own post remains
	// (c) h.API.JSON("GET /v1/posts/{id}/likes", 200, page of [hana, minji]); h.Send(ui.OpenOverlayMsg{ID: "feed.user_list", Args: "likes " + postBy("dev_kim").ID}) -> both rows; same event -> hana row gone
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/feed/ -run 'Hashtag|Trends|BlockHidesInPerScreen' -v`
Expected: FAIL — `undefined: newHashtag`

- [ ] **Step 3: Implement `hashtag.go`, `trends.go`**

- [ ] **Step 4: Generate goldens, run**

Run: `go test ./internal/ui/feed/ -run 'TestHashtagGolden|TestTrendsGolden|TestTrendsSearchGolden' -update && go test ./internal/ui/feed/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/feed/
git commit -m "feat(feed): hashtag feed, top-10 trends with hashtag and post search"
```

---

### Task 15: 알림함 `RouteNotifications` (지휘소 오른쪽 아래 창 · 단일 창 Main)

**Files:**
- Create: `internal/ui/feed/notifications.go`
- Test: `internal/ui/feed/notifications_test.go`, `internal/ui/feed/testdata/TestNotificationsGolden/**`

**Interfaces:**
- Consumes: `api.(*Client).{ListNotifications, MarkNotificationsRead}`, `api.MarkNotificationsRead{IDs, All, Before}`, `FeedStore.{Notifications, SetOpen, ApplyResync}`, `ctx.Store.{NotifUnread, DirectInvites}`, `ui.Badge`, `humanize.Relative`.
- Produces: `func newNotifications(ctx *ui.Context, r ui.Route) ui.Component` (init: `ui.RegisterRoute(ui.RouteNotifications, newNotifications)`), `func notifTarget(n api.Notification) (r ui.Route, overlay string, ok bool)`.

문구 표 **[결정]** (아이콘 · 좁은 창 문구 / 넓은 창 문구, `<이름>` = actor 표시 이름):

| type | 아이콘 | 좁게(창 내부 폭 < 40) | 넓게 | 이동 |
|---|---|---|---|---|
| post_like | `♥` | `<이름>님이 좋아함` | `<이름>님이 내 글을 좋아해요` | `RoutePost{context.post_id}` |
| post_repost | `↻` | `<이름>님이 리포스트` | `<이름>님이 내 글을 리포스트했어요` | 같음 |
| post_reply | `↩` | `<이름>님이 답글` | `<이름>님이 답글을 남겼어요` | 같음 |
| post_quote | `❝` | `<이름>님이 인용` | `<이름>님이 내 글을 인용했어요` | 같음 |
| post_mention | `@` | `<이름>님이 멘션` | `<이름>님이 글에서 나를 멘션했어요` | 같음 |
| message_mention | `@` | `<이름>님이 멘션` | `<이름>님이 <space_name>에서 나를 멘션했어요` | `thread_root_id` 있으면 `RouteThread{ID: root, Arg: channel_id, Anchor: message_id}`, 없으면 `RouteChannel{ID: channel_id, Anchor: message_id}` |
| thread_reply | `↩` | `<이름>님이 스레드 답글` | `<이름>님이 스레드에 답글을 달았어요` | 같음 |
| follow | `+` | `<이름>님이 팔로우` | `<이름>님이 나를 팔로우해요` | `RouteProfile{actor.id}` |
| follow_request | `+` | `<이름>님의 팔로우 요청` | `<이름>님이 팔로우를 요청했어요` | 오버레이 `feed.follow_requests` |
| follow_accepted | `✓` | `<이름>님이 수락` | `<이름>님이 팔로우 요청을 수락했어요` | `RouteProfile{actor.id}` |
| friend_request | `+` | `<이름>님의 친구 요청` | `<이름>님이 친구 요청을 보냈어요` | `RouteFriends` |
| friend_accepted | `✓` | `<이름>님과 친구` | `<이름>님과 친구가 되었어요` | `RouteProfile{actor.id}` |
| space_invite | `⌂` | `<space_name> 초대` | `<이름>님이 <space_name>에 초대했어요` | `RouteInvite{Arg: "direct", ID: subject.id}` |
| guestbook_entry | `✎` | `<이름>님의 방명록` | `<이름>님이 방명록을 남겼어요` | `RouteProfile{ID: "@me", Query: {"tab": "guestbook"}}` |

- 줄 형식: 넓게 `● ♥ 하나님이 내 글을 좋아해요 · "오늘 첫 눈…"            3분`(안 읽음 점 acc, 읽음은 공백; `preview` 있으면 따옴표 붙여 남는 폭에), 좁게 `♥ hana님이 좋아함`(시안 그대로, `<이름>` = 표시 이름). Line/Minimal 밀도는 좁은 형식.
- `Update` 첫머리 `forwardResync`(Task 2 전달자 규칙). 차단한 actor 의 알림은 리듀서가 지우고 렌더 때도 `IsBlocked` 로 건너뛴다.
- `enter`/클릭 = `MarkNotificationsRead{IDs: [id]}`(안 읽은 것만) 후 이동, `x` = `{All: true}`. `notification.created` 방송 → 맨 위(페이드), `notifications.read` → 반영(리듀서). `Titled` = `"알림"`, `ui.Badge{Count: ctx.Store.NotifUnread}`. 빈 상태 `"🔔", "새 알림이 없어요", ""`.

- [ ] **Step 1: Write the failing tests**

```go
func notifOpts(t *testing.T, hostile bool, r *ui.Route) []uitest.Option // default route RouteNotifications
func TestNotificationsGolden(t *testing.T)  // × AllSizes; at 200x60 the route sits in SlotNotifications (right bottom)
func TestNotificationsHostile(t *testing.T)
func TestNotificationsCommandPane(t *testing.T) {
	// 200x60 with route RouteChannel(#개발): Frame().Routes[layout.SlotNotifications].Kind == ui.RouteNotifications
	// region(screen, notif pane rect) contains "♥ hana님이 좋아함" and "@ 민지님이 멘션"
}
func TestNotificationTargets(t *testing.T) // table over the 14 types of Task 5 notificationsAll(): notifTarget(n) == expected route / overlay id
func TestNotificationEnterMarksReadAndRoutes(t *testing.T) // enter -> POST /v1/notifications/read {"ids":[id]}; Nav reflects target
func TestNotificationsMarkAll(t *testing.T)  // x -> body {"all":true}
func TestNotificationCreatedEventPrepends(t *testing.T) // h.Event("notification.created", {notification, unread_count: 3}) -> first row is it
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/feed/ -run Notification -v`
Expected: FAIL — `undefined: newNotifications`

- [ ] **Step 3: Implement `notifications.go`**

- [ ] **Step 4: Generate goldens, inspect, run**

Run: `go test ./internal/ui/feed/ -run TestNotificationsGolden -update && go test ./internal/ui/feed/ -v`
Expected: PASS; `200x60` 알림 창이 `layouts.html` ① 오른쪽 아래(`알림 2`, `♥ hana님이 좋아함`, `@ 민지님이 멘션` 구성)와 맞는지 확인.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/feed/
git commit -m "feat(feed): notifications inbox for command pane and single-pane modes"
```

---

### Task 16: 상태 메시지·접속 모드 (`status.set`·`status.clear`·`presence.set`)

**선행:** core 30 완료 + CR-S1(`ui.RunCommandMsg` 디스패치). CR-S1 전에는 `RunCommandMsg` 를 쓰는 시험이 "요청 없음" 으로 실패한다 — 정상이며 기다린다(선행 조건 규칙).

**Files:**
- Create: `internal/ui/fun/status.go`, `internal/ui/fun/presence.go`
- Test: `internal/ui/fun/status_test.go`, `internal/ui/fun/testdata/**`

**Interfaces:**
- Consumes: `palette.Register(palette.Command{ID, Title, Keywords, Group, KeyHint, Available, Run})`, `ui.RunCommandMsg`, `api.(*Client).{SetStatus, ClearStatus, SetPresenceMode}`, `api.SetStatusRequest{Emoji, Text, ExpiresInSec}`, `widgets.EmojiPicker(onPick)`, `widgets.PresenceDot`, `textinput`, `ctx.Store.{Me, Self, ObserveUser}`.
- Produces: palette `status.set`(`상태 메시지 설정`, 명령), `status.clear`(`상태 메시지 지우기`, Available: `Me.Status != nil`), `presence.set`(`접속 상태 바꾸기`, Keywords `자리비움 방해금지 away dnd`); overlays `"fun.status"`, `"fun.presence"`; `func parseStatusArgs(args string) (emoji api.Opt[string], text string, err error)`.

동작 **[결정]**:
- `status.set` args 가 비면 `fun.status` 오버레이, 있으면 첫 그래핌이 이모지(첫 룬이 `unicode.So` 이거나 ≥ U+1F000)면 `Emoji`, 나머지 trim 이 `Text`; 64자 초과 → `ui.Toast("상태 메시지는 64자까지예요", ui.ToastError)`. (`/status 🎧 듣는 중` 은 chat 입력창이 `ui.RunCommand("status.set", "🎧 듣는 중")` 으로 보낸다.)
- 오버레이 `상태 메시지`: 미리 정한 5개 `🎧 듣는 중`, `🔨 배포 중`, `🍚 점심 먹는 중`, `🤒 아파요`, `🏖 휴가 중` + 직접 입력 줄(이모지 칩 + `textinput`), 유지 시간 줄 `없음 · 30분 · 1시간 · 4시간 · 오늘 · 1주`(초: null, 1800, 3600, 14400, 오늘 23:59:59 까지(최소 60), 604800). `j/k`·`tab` 줄 이동, `←/→` 시간 선택, 이모지 칩에서 `enter` = `widgets.EmojiPicker`, `enter` 저장, `ctrl+x` 지우기(`ClearStatus`). 성공: `ctx.Store.Me.Status = res`, `ObserveUser(ctx.Store.Me.UserSummary)`, `ui.Toast("상태를 바꿨어요", ui.ToastSuccess)`.
- `presence.set` args `auto|away|dnd` 면 바로, 비면 `fun.presence` 오버레이(`● 자동`, `◐ 자리비움`, `⊘ 방해금지`, 현재 모드 표시). 결과 `ctx.Store.Self = *res`.

- [ ] **Step 1: Write the failing tests**

```go
func TestStatusSetArgs(t *testing.T)        // h.Send(ui.RunCommandMsg{ID:"status.set", Args:"🎧 듣는 중"}) -> PUT /v1/me/status {"emoji":"🎧","text":"듣는 중"}; Me.Status.Text=="듣는 중"
func TestStatusSetTextOnly(t *testing.T)    // Args "배포 중" -> body has no "emoji" key (omitzero)
func TestStatusSetTooLong(t *testing.T)     // 65 runes -> no request, toast "상태 메시지는 64자까지예요"
func TestStatusOverlayPresetWithExpiry(t *testing.T) // empty args -> overlay; j (preset 2), tab to expiry, → → (1시간), enter -> {"emoji":"🔨","text":"배포 중","expires_in_sec":3600}
func TestStatusClear(t *testing.T)          // RunCommand status.clear -> DELETE /v1/me/status; Me.Status==nil
func TestPresenceSet(t *testing.T)          // RunCommand presence.set "dnd" -> PUT /v1/me/presence {"mode":"dnd"}; Store.Self.Mode=="dnd"
func TestStatusOverlaysGolden(t *testing.T) // "fun.status" and "fun.presence" × Required+44x16+20x6
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/fun/ -run 'Status|Presence' -v`
Expected: FAIL — 명령 미등록(요청 없음)

- [ ] **Step 3: Implement `status.go`, `presence.go` (each with its own `init()`)**

- [ ] **Step 4: Generate goldens, run**

Run: `go test ./internal/ui/fun/ -run TestStatusOverlaysGolden -update && go test ./internal/ui/fun/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/fun/
git commit -m "feat(fun): status message and presence mode commands and dialogs"
```

---

### Task 17: 집중 모드 (`focus.start`·`focus.stop`)와 놓친 것 요약 `fun.focus_summary`

**선행:** core 31 완료(alert → 요약 오버레이 열기) + CR-S1. **서버 의존:** SV-1 — 조기 종료 요약은 서버가 `DELETE /v1/me/focus` 에도 `alert{focus_summary}` 를 보내야 실서버에서 뜬다(plan 01 Task 12). 아래 시험은 가짜 이벤트라 이 의존을 검증하지 못한다.

**Files:**
- Create: `internal/ui/fun/focus.go`, `internal/ui/fun/focus_summary.go`
- Test: `internal/ui/fun/focus_test.go`, `internal/ui/fun/testdata/TestFocusSummaryGolden/**`

**Interfaces:**
- Consumes: `api.(*Client).{StartFocus, EndFocus}`, `api.Some[int]`, `ctx.Store.{Focus, FocusEnded, LastFocusSummary, Alerts, DirectInvites, Settings}`, `ui.{RegisterOverlay, OverlayFocusSummary, Navigate, CloseOverlayMsg}`, `humanize.{Duration, Clock}`, `markup.Plain`.
- Produces: palette `focus.start`(`집중 모드 시작`, Available `Store.Focus == nil`), `focus.stop`(`집중 모드 끝내기`, Available `Store.Focus != nil`); overlay `ui.OverlayFocusSummary`; `func parseFocusArgs(args string) (api.Opt[int], error)`; `func alertRoute(st *store.Store, a api.Alert) (ui.Route, bool)`.

동작 **[결정]**:
- `focus.start` args: `""` → 생략(서버가 `focus_default_min`), `25m`·`1h30m`(`time.ParseDuration`) 또는 숫자만(분). 60..43200초 밖 → `ui.Toast("집중 시간은 1분~12시간이에요", ui.ToastError)`. 성공 → `ctx.Store.Focus = res`, `ui.Toast("🎯 집중 모드 — <humanize.Duration> 동안 알림을 모아 둘게요", ui.ToastSuccess)`. (상태줄 남은 시간은 core 가 `Store.Focus` 로 그린다.)
- `focus.stop` → `EndFocus` → `ctx.Store.FocusEnded(*sum)` **만**. `409 conflict` → `ui.Toast("집중 모드가 아니에요", ui.ToastInfo)`.
- 요약 오버레이(args = alert id): 제목 `집중 모드 끝`, 첫 줄 `25분 집중했어요 (11:05–11:30)`, 둘째 줄 `멘션 3 · DM 2 · 스레드 1 · 초대 0 · 친구 요청 0 · 기타 1`, 구분선, 항목(≤20, 최신 먼저) `@ 민지 · #개발  @sam 배포 언제?   11:12`(kind 아이콘: mention `@`, dm `✉`, thread_reply `↩`, invite `⌂`, friend_request `+`, 그 외 `•`). `LastFocusSummary == nil` 이면 `Store.Alerts` 에서 id 로 찾은 alert 의 title/body + `요약을 불러오는 중…`; `View` 가 매번 `Store` 를 읽으므로 `focus.ended` 뒤 자동으로 바뀐다. `j/k` 선택, `enter` = `alertRoute` 로 이동 + 닫기, `esc` 닫기. `Modal()==true`, `Bounds` = 가운데 `min(64, W-4) × min(6+len(items), H-2)`.
- `alertRoute`: target `message`/`channel` → `thread_root_id` 있으면 `RouteThread{ID, Arg: channel_id, Anchor: message_id}`, 아니면 `RouteChannel{channel_id, Anchor}`; `post` → `RoutePost`; `space` → `RouteInvite{Arg: "direct", ID: Store.DirectInvites 에서 space_id 일치 id 또는 ""}`; `user` → `RouteProfile`; `notification` → `RouteNotifications`.

- [ ] **Step 1: Write the failing tests**

```go
func summary() api.FocusSummary // started 02:05Z, ended 02:30Z, counts mention 3 dm 2 thread_reply 1 other 1, items 3 alerts
func focusAlert(id string) api.Alert // kind "focus_summary", title "집중 모드 끝 — 멘션 3, DM 2", CreatedAt = clock now

func TestFocusStartArgs(t *testing.T)     // "25m" -> POST /v1/me/focus {"duration_sec":1500}; "" -> body "{}"; "90" -> 5400
func TestFocusStartInvalid(t *testing.T)  // "30s" and "13h" -> no request, toast "집중 시간은 1분~12시간이에요"
func TestFocusStopNoOverlay(t *testing.T) // DELETE -> {"summary":...}; Store.LastFocusSummary != nil; Store.Focus == nil; len(Frame().Overlays)==0
func TestFocusStopNotFocusing(t *testing.T) // 409 conflict -> toast "집중 모드가 아니에요"
func TestFocusSummaryExactlyOnce(t *testing.T) {
	// RunCommand focus.stop (early end) ; h.Event("focus.ended", map[string]any{"summary": sum})
	// h.Event("alert", map[string]any{"alert": focusAlert("A1")}); h.Event("alert", same) // replay same id
	// if n := len(h.Model.Frame().Overlays); n != 1 { t.Fatalf("overlays=%d", n) }; screen contains "멘션 3 · DM 2"
	// h.Press("esc"); h.Event("alert", same) -> len(Frame().Overlays)==0
}
func TestFocusSummaryNaturalEnd(t *testing.T)  // no stop: focus.ended + alert -> exactly 1 overlay
func TestFocusSummaryBeforeSummary(t *testing.T) // alert first (Store.LastFocusSummary nil) -> screen has alert title + "요약을 불러오는 중…"; then focus.ended -> counts line shown
func TestFocusSummaryEnterNavigates(t *testing.T) // select mention item -> enter -> Nav() active tab top == RouteChannel{#개발 id}; overlay closed
func TestFocusSummaryGolden(t *testing.T)     // × Required+44x16+20x6+19x10 (19x10 = TooSmall: overlay not drawn, Check passes)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/fun/ -run Focus -v`
Expected: FAIL — 명령·오버레이 미등록

- [ ] **Step 3: Implement `focus.go`, `focus_summary.go`**

- [ ] **Step 4: Generate goldens, run**

Run: `go test ./internal/ui/fun/ -run TestFocusSummaryGolden -update && go test ./internal/ui/fun/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/fun/
git commit -m "feat(fun): focus mode commands and once-only missed-items summary overlay"
```

---

### Task 18: 전체 화면 이미지 보기 `feed.media` (kitty·iTerm2·하프블록)

**Files:**
- Create: `internal/ui/feed/media.go`
- Test: `internal/ui/feed/media_test.go`, `internal/ui/feed/testdata/TestMediaOverlayGolden/**`

**Interfaces:**
- Consumes: `mediaRenderer`, `loadImage`, `imgrender.{Options, FitContain, ITerm2Inline}`, `(*imgrender.Renderer).{Render, Peek}`, `forwardResync`, `termcaps.{GraphicsKitty, GraphicsITerm2}`, `ctx.Layout`, `ctx.Mode`, `tea.Raw`, `tea.RawMsg`.
- Produces: overlay `"feed.media"` args=`"<postID> <idx>"`.

동작 **[결정]**: `Bounds(body) = body`(전체 본문), 제목 `사진 2/4`. 내부 원점 = `(body.X+1, body.Y+1)`, 단 `ctx.Mode == ModeTiny` 또는 `body.W < 24 || body.H < 6` 이면 `(body.X, body.Y)`(앱 강등 규칙 5.3.7 과 같게). 방식: kitty(`!InTmux`) → `Render` 자리표시자 + `Setup` 을 `tea.Raw`; iTerm2(`!InTmux`) → 내부를 공백으로 비우고 그 자리에 `tea.Raw(ITerm2Inline(img, ox, oy, cols, rows))`(실패하면 하프블록으로 되돌리고 이후 계속 하프블록); 그 외 하프블록. `←`/`→` 이전/다음, `esc` 닫기.

**Raw 를 내는 곳 [결정]** (Task 6 이미지 규칙과 같은 이유 — `View`·`Resize` 는 Cmd 를 낼 수 없고 core 는 그 뒤 `Update` 를 보장하지 않는다): 내부 사각형(원점·`cols`·`rows`)은 `ctx.Layout` 의 본문 사각형과 위 강등 규칙만으로 정해지는 **순수 계산**이므로 `Update` 에서 언제든 구할 수 있다. 그래서 Raw 는 다음 `Update` 들이 **자기 반환값으로** 낸다: (1) `ResultMsg[image.Image]`(사진 도착), (2) `←`/`→`(사진 바뀜, `HandleKey` 반환값), (3) `ui.ModeChangedMsg`·`ui.ThemeChangedMsg`. 오버레이는 마지막으로 내보낸 `(사진, ox, oy, cols, rows, 방식)` 을 기억한다. `View` 는 현재 사각형이 그 기억과 같을 때만 kitty 자리표시자/iTerm2 빈 칸으로 그리고, 다르면(같은 모드 안 크기 변경처럼 Update 없이 Resize 만 된 경우) **하프블록으로 그려 깨짐·빈 화면·엉뚱한 위치의 옛 이미지를 막고** 그 사각형을 want 로 남긴다. 다음 `Update`(어떤 메시지든) 끝의 `flush` 가 want 를 Raw 로 내보내고 기억을 갱신한다. 같은 모드 안 크기 변경 직후 그 Update 가 오기 전까지는 하프블록이 보인다 — core 에 "크기 변경 후 Update" 훅이 없어서 생기는 한계이며, 화면은 깨지지 않는다.

- [ ] **Step 1: Write the failing tests**

```go
func TestMediaOverlayGolden(t *testing.T)     // halfblock; h.Send(OpenOverlayMsg{ID:"feed.media", Args: hanaID+" 0"}) × Required+44x16+20x6
func TestMediaITerm2EmitsRaw(t *testing.T) {
	// 120x40. h.Context().Caps.Graphics = termcaps.GraphicsITerm2 before opening; h.Send(OpenOverlayMsg{feed.media, hanaID+" 0"}) — nothing else sent afterwards
	// some tea.RawMsg in h.TeaMsgs() whose fmt.Sprint(Msg) contains "1337;File=" and "inline=1" (emitted by the ResultMsg Update itself)
	// inner overlay rows in h.Screen() are blank (no "▀")
	// h.Resize(160, 45) (same standard mode => no Update): no new RawMsg; inner rows show half-block "▀" (fallback, not stale blank); h.Check()
	// type nudge struct{}; h.Send(nudge{}) -> a new RawMsg with the new "width="; inner rows blank again
	// h.Resize(200, 60) (mode change => ModeChangedMsg) -> another RawMsg without any extra message
}
func TestMediaKittyEmitsSetupOnce(t *testing.T) // Caps.Graphics = GraphicsKitty: opening emits one RawMsg with "\x1b_G" from the ResultMsg Update (no follow-up message); sending ui.ModeChangedMsg again adds none (same box already sent)
func TestMediaTmuxUsesHalfBlock(t *testing.T)   // Caps{Kitty, InTmux:true} -> no RawMsg, screen has "▀"
func TestMediaNextPrev(t *testing.T)            // 3-image post: "→" -> title "사진 2/3"; "←" -> "사진 1/3"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/feed/ -run Media -v`
Expected: FAIL — overlay `feed.media` 미등록

- [ ] **Step 3: Implement `media.go`**

- [ ] **Step 4: Generate goldens, run**

Run: `go test ./internal/ui/feed/ -run TestMediaOverlayGolden -update && go test ./internal/ui/feed/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/feed/
git commit -m "feat(feed): full-screen media viewer with kitty and iTerm2 support"
```

---

### Task 19: 팔레트 명령·진입점 등록·모드 전환 통합 시험

**선행:** core 31 완료 + CR-S1(`TestPaletteCommandsRun` 은 `h.Send(ui.RunCommandMsg{ID})` 를 앱이 실행해야 통과).

**Files:**
- Create: `internal/ui/feed/palette.go`, `cmd/masil/register_sns.go`
- Test: `internal/ui/feed/palette_test.go`, `internal/ui/feed/modes_test.go`

**Interfaces:**
- Consumes: `palette.{Register, RegisterSource, Source, Item, ItemRoute}`, `ui.{Navigate, OpenRegisteredOverlay}`, `FeedStore.Trends`, `uitest`, `app.NavState`, `layout.{SlotFeed, SlotMain}`.
- Produces: palette commands(ui/feed, 계약 5.6):

| id | Title | Group | KeyHint | Run |
|---|---|---|---|---|
| `post.new` | `새 글 쓰기` | 명령 | `c` | `Navigate(RouteCompose{Arg:"post"})` |
| `story.new` | `새 스토리 올리기` | 명령 | | `Navigate(RouteCompose{Arg:"story"})` |
| `feed.following` | `피드: 팔로잉` | 이동 | `f` | `Navigate(RouteFeed{Arg:"following"})` |
| `feed.discover` | `피드: 전체(발견)` | 이동 | | `Navigate(RouteFeed{Arg:"discover"})` |
| `trends` | `트렌드 Top 10` | 이동 | | `Navigate(RouteTrends)` |
| `notifications` | `알림함` | 이동 | `ctrl+n` | `Navigate(RouteNotifications)` |
| `profile.me` | `내 프로필` | 이동 | | `Navigate(RouteProfile{ID: ctx.Me().ID})` |
| `profile.edit` | `프로필 편집` | 설정 | | `OpenRegisteredOverlay("feed.profile_edit","")` |
| `follow_requests` | `팔로우 요청` | 명령 | | `OpenRegisteredOverlay("feed.follow_requests","")` |

팔레트 `#태그` 소스 **[결정]** (`palette.RegisterSource(hashtagSource{})`, 같은 `palette.go` 의 `init()`; 명령 id 가 아니므로 CA 5.6 명령 표를 늘리지 않는다): `palette.Source.Items` 는 동기 호출이라 네트워크를 쓰지 않는다 — 질의가 `#` 로 시작하고 나머지가 1..50 코드 포인트면 (1) `FeedStore.Trends()`(트렌드 화면·지휘소 피드 열이 이미 받아 둔 Top 10)에서 `Tag` 가 나머지로 시작하는 것들을 `Item{Kind: ItemRoute, Title: "#"+tag, Subtitle: "트렌드 N위", Route: &Route{Kind: RouteHashtag, ID: tag}}` 로, (2) 끝에 늘 `Item{Kind: ItemRoute, Title: "#"+나머지, Subtitle: "태그 피드 열기", Route: …}` 하나를 낸다(정확히 같은 태그가 (1)에 있으면 생략). 서버 해시태그 검색(`SearchHashtags`)이 필요한 접두 제안은 트렌드 화면 `/` 검색(Task 14)이 맡는다. 그 밖 질의에는 항목을 내지 않는다.

`register_sns.go`:

```go
package main

import (
	_ "github.com/patrache/masil/internal/ui/feed"
	_ "github.com/patrache/masil/internal/ui/fun"
)
```

- [ ] **Step 1: Write the failing tests**

```go
func TestPaletteCommandsRun(t *testing.T) // table: h.Send(ui.RunCommandMsg{ID}) -> expected Nav().Feed top / Front / overlay present (9 rows)
func TestPaletteFindsSnsCommands(t *testing.T) // Press("ctrl+k"); Type("새 글") -> screen contains "새 글 쓰기"; Type chosung "ㅌㄹㄷ" after clearing -> "트렌드 Top 10"
func TestPaletteHashtagSource(t *testing.T) // FeedStore.SetTrends(trendsTop10()); ctrl+k; Type("#첫") -> screen contains "#첫눈" and "트렌드"; enter -> Nav().Feed top == {RouteHashtag, ID "첫눈"}; Type("#없는태그") -> single item "#없는태그" "태그 피드 열기"; no HTTP request made by the source
func TestFeedSurfaceAcrossModes(t *testing.T) {
	// 80x24 FrontFeed (widget "피드" tab): Press("j","j","j"); id := SelectedID(inst)
	// 200x60: Frame().Routes[SlotFeed].Key() == "feed//following", same inst, SelectedID==id, Focus().Slot == layout.SlotFeed
	// 120x40: Routes[SlotMain].Kind == RouteFeed (FeedSwapped); 80x24: Nav().Front == app.FrontFeed, SelectedID==id; h.Check() each step
}
func TestNotificationsAcrossModes(t *testing.T) // 120x30 FrontNotifications select 2nd -> 200x60 SlotNotifications same instance & selection -> 120x30 back
func TestAllSnsRoutesHostileSweep(t *testing.T) {
	// routes: feed following/discover, post(hostile), profile(hostile), hashtag, trends, notifications, stories, compose
	// one harness per route, Hostile seed; for every s in uitest.AllSizes(): h.Resize(s.W, s.H); h.Check()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/feed/ -run 'Palette|AcrossModes|HostileSweep' -v`
Expected: FAIL — 팔레트 명령 미등록

- [ ] **Step 3: Implement `palette.go` and `cmd/masil/register_sns.go`**

- [ ] **Step 4: Run the whole sns-fun suite, build and vet**

Run: `go test ./internal/store/feedstore/... ./internal/imgrender/... ./internal/ui/feed/... ./internal/ui/fun/... && go vet ./... && go build ./cmd/masil && go test ./internal/ui/... -run TestTimerRule`
Expected: 모두 `ok`; `go build` 성공; core 의 타이머 정적 검사(`TestTimerRule` 또는 core 가 붙인 이름)가 `ui/feed`·`ui/fun` 위반 0건.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/feed/palette.go internal/ui/feed/palette_test.go internal/ui/feed/modes_test.go cmd/masil/register_sns.go
git commit -m "feat(feed): palette commands, sns registration and cross-mode integration tests"
```

---

### Task 20: 운영자 CLI `masil admin …` (범위 지시서 재배정)

설계서 2장은 첫 출시부터 전역 운영자 기능(신고 목록·게시물/이미지 강제 삭제·계정 정지)을 요구하고, 수단은 "`masil admin …` 명령 또는 TUI 숨김 화면"이면 된다. 계약 2.1·9.2 는 이를 daemon-release 소유(`cmd/masil/cmd_admin.go`, `internal/admincli`)로 두었지만 06 계획은 범위 지시서에 없다는 이유로 만들지 않는다. **범위 지시서가 이 두 경로를 이 계획에 재배정했으므로**(Global Constraints) 여기서 CLI 로 만든다. 오너가 재배정을 원치 않으면 이 Task 를 실행하기 전에 여쭙는다.

**선행(모두 충족해야 시작):**
- **CR-S3** — 오너 승인으로 CA 2.1·7·9.2(와 2.4 명령 소유 줄)의 `cmd/masil/cmd_admin.go`·`internal/admincli` 소유가 sns-fun 으로 갱신돼 있을 것. 갱신 전에는 CA 0장 1 과 충돌하므로 시작하지 않는다.
- core 실제 구현: Task 8(`apitest`)·9(`Admin*` 요청/응답 타입)·11(`api.(*Client).Admin*` 메서드 — 엔드포인트 B 의 운영자 부분), 6(`textwidth`), 7(`humanize.MessageTime`), 4(`config.{Defaults, Paths, ServerURL}`)·5(`auth.{Load, Save, ErrNoCredentials}`·`Credentials.Matches`), 2(`register`·`command`·`Env` — Phase 0 에서 실제 동작). `TestAdminCmdUsageExitCode` 만 추가로 core 35(`run` 의 명령 실행·종료 코드)와 **CR-S2**(package-main 사용법 오류 표지)가 필요하다.
- 위 core Task 가 스텁이면 시험이 실패·패닉한다 — 시험을 고치지 말고 기다린다. TUI 가 없어 sns Task 1–19 와는 코드 의존이 없으므로, 선행만 충족되면 그 Task 들과 순서 무관하게 실행할 수 있다.

**Files:**
- Create: `internal/admincli/admincli.go`, `internal/admincli/table.go`, `cmd/masil/cmd_admin.go`
- Test: `internal/admincli/admincli_test.go`, `internal/admincli/testdata/TestAdminOutput/*.golden`, `cmd/masil/cmd_admin_test.go`

**Interfaces:**
- Consumes: `api.(*Client).{AdminListReports, AdminGetReport, AdminResolveReport, AdminRemoveContent, AdminSuspend, AdminUnsuspend, AdminListUsers, AdminListActions}`, `api.{AdminReportQuery, AdminRemoveRequest, AdminSuspendRequest, AdminUserQuery, AdminActionQuery, PageOpts, Page, Some, UserRef, Handle, UserID, IsCode, Describe, Report, AdminUserView, AdminAction}`, `textwidth.{Sanitize, Width, PadRight, Truncate}`, `humanize.MessageTime`, `apitest.{New, JSON, Error, Requests, Client}`, core `cmd/masil/main.go` 의 `register(command{Name, Usage, Hidden, Run})`·`Env{Paths, Config, Stdin, Stdout, Stderr}`, `auth.{Load, Save, ErrNoCredentials}`, `(*auth.Credentials).Matches`, core `main.go` 의 package-main 사용법 오류 표지(CR-S2 — core 가 이름을 정한 뒤에만), `(*config.Config).ServerURL`, `api.{New, WithToken}`, `github.com/charmbracelet/x/exp/golden`.
- Produces:

```go
package admincli

type IO struct {
	Stdout, Stderr io.Writer
	Now            time.Time
	Loc            *time.Location
}
var ErrUsage = errors.New("admincli: usage")
const Usage = "사용법: masil admin reports|report <id>|resolve <id> <resolution>|remove <type> <id>|suspend @h [--for 7d]|unsuspend @h|users|actions"
func Run(ctx context.Context, c *api.Client, args []string, o IO) error // dispatches the 8 subcommands below
func Table(header []string, rows [][]string) string                    // textwidth-aligned columns, 2-space gap, no trailing spaces, "\n" after each line
func ParseFor(s string) (sec int, err error)                           // "7d" | Go duration ("12h", "1h30m"); result >= 60
```

```go
// cmd/masil/cmd_admin.go (package main)
func init() // register(command{Name: "admin", Usage: "운영자 명령 (reports | report | resolve | remove | suspend | unsuspend | users | actions)", Hidden: true, Run: adminCmd})
func adminCmd(ctx context.Context, env *Env, args []string) error // adminClientFn(env) -> admincli.Run(ctx, c, args, admincli.IO{env.Stdout, env.Stderr, time.Now(), time.Local}); errors.Is(err, admincli.ErrUsage) => return errors.Join(<core usage marker, CR-S2>, err) (before CR-S2: return err unchanged)
var adminClientFn = adminClient                                   // test seam
func adminClient(env *Env) (*api.Client, error)                   // auth.Load(env.Paths); ErrNoCredentials => "로그인하지 않았습니다 — 먼저 'masil login'"; !creds.Matches(env.Config.ServerURL()) => "로그인한 서버(<creds>)와 설정 서버(<config>)가 다릅니다"; else api.New(ServerURL, api.WithToken(creds.Token))
```

하위 명령 **[결정]** (위치 인자를 먼저 떼고 나머지를 `flag.NewFlagSet(<sub>, flag.ContinueOnError)`(출력 `o.Stderr`)로 파싱 — `suspend @h --for 7d` 처럼 플래그가 뒤에 와도 된다. 사용자 인자는 `@handle` → `api.Handle`, 26자 ULID → `api.UserID`, 그 밖은 사용법 오류):

| 명령 | 요청 | 출력(stdout) |
|---|---|---|
| `reports [--status open\|resolved\|dismissed] [--type <target_type>] [--cursor c] [--limit n]` | `AdminListReports` | 표 `ID · 대상 · 사유 · 신고자 · 누적 · 상태 · 시각`; 비면 `신고가 없습니다.`; 다음 페이지가 있으면 끝에 `다음 페이지: masil admin reports --cursor <c>`(+ 같은 필터) |
| `report <id>` | `AdminGetReport` | 아래 상세 블록 |
| `resolve <id> <content_removed\|user_suspended\|no_action> [--note text]` | `AdminResolveReport` | `신고 <id>: <상태> (<처리>)` |
| `remove <message\|post\|story\|guestbook_entry\|upload\|space> <id> --reason text [--report id]` | `AdminRemoveContent` | `삭제했습니다: <type> <id> (조치 <action.id>)` |
| `suspend @h [--for 7d] --reason text [--report id]` | `AdminSuspend`(`--for` 없으면 `DurationSec` 생략 = 무기한) | `정지했습니다: @h — <suspended_until 의 MessageTime 까지 \| 무기한>` |
| `unsuspend @h` | `AdminUnsuspend` | `정지를 풀었습니다: @h` |
| `users [--q text] [--suspended true\|false] [--cursor c] [--limit n]` | `AdminListUsers` | 표 `ID · 사용자 · 이름 · 역할 · 상태 · 열린 신고 · 가입` |
| `actions [--target-type t] [--target id] [--cursor c] [--limit n]` | `AdminListActions` | 표 `ID · 운영자 · 조치 · 대상 · 신고 · 사유 · 시각` |

- `--reason` 은 `remove`·`suspend` 에서 필수(감사 기록 `admin_actions.reason` 이 비지 않게 — 계약 사용법 줄의 확장), 없으면 사용법 오류. `ParseFor`: `^[1-9][0-9]*d$` 는 일×86400, 그 밖은 `time.ParseDuration` 의 정수 초, 60초 미만·0·음수·형식 오류는 `정지 기간은 7d, 12h, 30m 처럼 1분 이상으로 적어 주세요` + 사용법 오류.
- 한국어 값: 사유 `spam 스팸 · harassment 괴롭힘 · illegal 불법 · sexual 성적 콘텐츠 · violence 폭력 · other 기타`; 상태 `open 열림 · resolved 처리됨 · dismissed 기각`; 처리 `content_removed 콘텐츠 삭제 · user_suspended 계정 정지 · no_action 조치 없음`; 조치 `remove_content 삭제 · suspend_user 정지 · unsuspend_user 정지 해제 · resolve_report 신고 처리 · dismiss_report 신고 기각`; 역할 `user 사용자 · admin 운영자`; 사용자 상태 `정상` / `정지(무기한)` / `정지(~<MessageTime>)`.
- 표 칸: `대상` = `<target_type> <target_id>` + (`target_user` 있으면 ` @handle`) + (`!target_exists` 면 ` (삭제됨)`), `신고자`·`운영자`·`사용자` = `@handle`, `누적` = `open_reports_on_target`, `시각`·`가입` = `humanize.MessageTime(o.Now, t, o.Loc)`, `이름` = `Truncate(display_name, 16, "…")`, `사유`(actions) = `Truncate(reason, 30, "…")`. ID 는 자르지 않는다(복사해 다음 명령에 쓰므로). 모든 서버 문자열은 `textwidth.Sanitize` 후 넣는다. `Table` 의 열 폭 = 그 열 모든 칸(머리 포함) `textwidth.Width` 의 최댓값, 마지막 열은 채우지 않는다.
- 상세 블록(`report <id>`; 각 줄 = `"  " + textwidth.PadRight(label, 8) + 값`):

```
신고 <id>
  상태    열림
  대상    post <target_id> @hana
  사유    스팸 — <note>            (note 가 비면 " — …" 생략)
  신고자  @minji · 11:04
  누적    열린 신고 2건
  내용    <snapshot.body 를 Sanitize·한 줄로·Truncate 200; body 가 없으면 snapshot 키를 정렬해 "k=v" 를 " · " 로 이은 것>
  처리    계정 정지 · @sam · 11:05 — <resolution_note>   (status 가 open 이 아닐 때만)
```

- 오류: `403 not_admin` → stderr `운영자 권한이 없습니다.`; `suspend` 의 `409 conflict` → `운영자는 정지할 수 없습니다.`; 그 밖 → stderr `오류: <api.Describe(err)>`. 모두 오류를 돌려준다. 사용법 오류는 `admincli.Run` 이 `ErrUsage` 를 감싸(`fmt.Errorf("%w: …", ErrUsage)`) 돌려준다. 종료 코드는 core `main` 이 정하는데, core `main.go` 는 `admincli` 를 import 할 수 없어(CA 2.2) `ErrUsage` 를 알아보지 못한다 — 그래서 **`cmd_admin.go` 의 `adminCmd` 가 `errors.Is(err, admincli.ErrUsage)` 인 오류를 core 가 CR-S2 로 정하는 package-main 사용법 오류 표지로 바꿔 돌려준다**(`errors.Join(<표지>, err)` — 표지와 원래 오류 둘 다 `errors.Is` 로 남는다). 그 밖 오류는 그대로(→ 종료 코드 1). CR-S2 가 core 에 들어오기 전에는 표지를 참조하지 않고 그대로 돌려준다(사용법 오류도 1 — 알려진 임시 한계). 사용법 오류는 요청을 보내지 않고 stderr 에 `Usage`(또는 위 개별 문구 + `Usage`)를 쓴다.
- 비밀: 토큰을 출력·골든에 남기지 않는다(시험 토큰 `msl_test` 가 골든에 나오면 실패).

- [ ] **Step 1: Write the failing tests**

시험 시계 `2026-11-05T02:05:00Z`, `Loc` = `Asia/Seoul`(`time/tzdata` import). `cmd/masil` 은 06 계획의 `cmd_daemon_test.go` 와 같은 `main` 패키지이므로 시험 도우미 이름에 `admin` 접두사를 붙인다(`testEnv` 와 충돌 방지).

```go
// internal/admincli/admincli_test.go
func run(t *testing.T, srv *apitest.Server, args ...string) (stdout, stderr string, err error) // Run(t.Context(), srv.Client(), args, IO{&out, &errb, clock, seoul})
func reportFix() []api.Report     // R1: post target by hana, reporter minji, reason spam, open, open_reports_on_target 2; R2: user target @dev_kim, reporter junho, reason harassment, note "😡 욕설", target_exists false (Korean 사유/상태 cells + emoji exercise wide-char alignment)
func userFix() []api.AdminUserView // hana normal; junho suspended until 2026-11-12T02:05Z; sam admin
func actionFix() []api.AdminAction // remove_content post P1 by sam reason "illegal content …" (40 runes), suspend_user junho

func TestAdminOutput(t *testing.T) {
	// subtests -> golden.RequireEqual(t, stdout): "reports" (reportFix, next_cursor "c2"), "reports_empty", "report_detail" (R1 with snapshot {"body":"광고 링크 …"}),
	// "report_resolved" (R2 dismissed by sam), "users", "actions"
	// each table line: ansi.StringWidth == ansi.StringWidthWc; no line has trailing spaces; no golden contains "msl_test"
}
func TestTableAlignsWideChars(t *testing.T) // Table([]string{"이름","ID"}, [][]string{{"하나","A"},{"hana","B"},{"👍x","C"}}): the column-2 start cell (StringWidth of prefix) is equal on all 4 lines
func TestAdminRequests(t *testing.T) {
	// table over args -> exactly one recorded request with method, path, query, JSON body:
	// "reports --status resolved --type post"           GET  /v1/admin/reports            status=resolved&target_type=post
	// "report R1"                                       GET  /v1/admin/reports/R1
	// "resolve R1 no_action --note 중복"                POST /v1/admin/reports/R1/resolve {"resolution":"no_action","note":"중복"}
	// "remove post P1 --reason illegal --report R1"     POST /v1/admin/content/remove     {"target_type":"post","target_id":"P1","reason":"illegal","report_id":"R1"}
	// "suspend @hana --for 7d --reason spam"            POST /v1/admin/users/@hana/suspend {"reason":"spam","duration_sec":604800}
	// "suspend @hana --reason spam"                     same path, body has no "duration_sec" key
	// "unsuspend @hana"                                 POST /v1/admin/users/@hana/unsuspend
	// "users --q ha --suspended true"                   GET  /v1/admin/users              q=ha&suspended=true
	// "actions --target-type post --target P1"          GET  /v1/admin/actions            target_type=post&target_id=P1
}
func TestAdminUsageErrors(t *testing.T) // {}, {"bogus"}, {"resolve","R1","maybe"}, {"remove","post","P1"}, {"suspend","hana","--reason","x"}, {"suspend","@hana","--for","30s","--reason","x"} -> errors.Is(err, ErrUsage); stderr contains "사용법: masil admin"; len(srv.Requests())==0
func TestAdminNotAdmin(t *testing.T)    // srv.Error("GET /v1/admin/reports", 403, "not_admin") -> err != nil; stderr == "운영자 권한이 없습니다.\n"
func TestAdminSuspendAdminConflict(t *testing.T) // 409 conflict -> stderr "운영자는 정지할 수 없습니다."
func TestParseFor(t *testing.T)         // "7d"=604800, "12h"=43200, "1h30m"=5400, "90s"=90; "30s", "", "0d", "-1h", "7x" => error

// cmd/masil/cmd_admin_test.go
func adminTestEnv(t *testing.T) (env *Env, stdout, stderr *bytes.Buffer) // temp MASIL_CONFIG_DIR, config.Defaults(), no credentials
func TestAdminCmdNotLoggedIn(t *testing.T)     // adminCmd(ctx, env, {"reports"}) -> error; stderr contains "로그인하지 않았습니다 — 먼저 'masil login'"
func TestAdminCmdServerMismatch(t *testing.T)  // auth.Save(creds{Server:"http://127.0.0.1:9999"}); config server "http://localhost:8787" -> error mentioning both URLs; token never printed
func TestAdminCmdRunsWithClient(t *testing.T)  // adminClientFn = func(*Env) (*api.Client, error) { return srv.Client(), nil } (restored on cleanup); {"users"} -> GET /v1/admin/users recorded
func TestAdminCmdUsageExitCode(t *testing.T)   // ADDED ONLY AFTER CR-S2 + core Task 35: adminClientFn -> srv.Client(); adminCmd(ctx, env, {"bogus"}) => errors.Is(err, <core usage marker>) && errors.Is(err, admincli.ErrUsage); run(["admin","bogus"], env) == 2; run(["admin","reports"]) with 403 not_admin == 1; no request for "bogus"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/admincli/ ./cmd/masil/ -run 'Admin|Table|ParseFor' -v`
Expected: FAIL — `undefined: admincli.Run` / `undefined: adminCmd`

- [ ] **Step 3: Implement `internal/admincli/admincli.go`, `table.go` and `cmd/masil/cmd_admin.go`**

- [ ] **Step 4: Generate goldens, inspect, run**

Run: `go test ./internal/admincli/ -run TestAdminOutput -update && cat internal/admincli/testdata/TestAdminOutput/reports.golden && go test ./internal/admincli/ ./cmd/masil/ -v && go vet ./internal/admincli/ ./cmd/masil/ && go build ./cmd/masil && ! grep -r msl_test internal/admincli/testdata`
Expected: PASS; `reports.golden` 의 열이 한글·이모지 섞인 줄에서도 같은 칸에서 시작하고, 마지막 줄이 `다음 페이지: masil admin reports --cursor c2`; `grep` 출력 없음.

- [ ] **Step 5: Commit**

```bash
git add internal/admincli/ cmd/masil/cmd_admin.go cmd/masil/cmd_admin_test.go
git commit -m "feat(admincli): masil admin reports, resolve, remove, suspend and audit commands"
```
