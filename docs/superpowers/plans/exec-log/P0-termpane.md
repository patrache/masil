# P0 batch "termpane" 실행 기록 (Task 16·16a·20)

- 브랜치 `feat/v013-p0-termpane`, 기준 `dev/v0.1.3` 58376da, 2026-11-09.
- 계획: `docs/superpowers/plans/2026-11-09-masil-v013-P0.md` Task 16·16a·20.
- 시험은 macOS(darwin/arm64)에서만 실제로 돌렸다. Linux·Windows 는 `GOOS=linux go vet`·`GOOS=windows go vet` 만 했다.

## Task 16 — termpane 화면 · 접두키 · 키 독점 · 즉시 끊기 등록부

- 내 판단: `termpane.shared_badge` 의 `max` 는 계획의 8 이 아니라 9 로 했다. `● 공유 중` 은 textwidth 로 9칸이라 생성기가 8 을 거절했다(en `● Shared` 는 8칸).
- 내 판단: `termpane.input_hint` 와 `prefix_hint` 에 `key` 인자를 더했다(`input_hint(program, key)`, `prefix_hint(key)`). `[keys] "termhost.escape"` 를 바꾸면 안내 글자도 따라가야 해서다. 기본 키에서 글자는 계획과 같다.
- 내 판단: 키맵 `termhost.escape` 의 기본 키는 `ctrl+\` 와 `ctrl+4` 두 개(도움말 표기는 `ctrl+\`)이고, 날 바이트 0x1c(Mod 없음)는 새 메서드 `(*keymap.Keymap).MatchesEscape` 가 받는다(`ctrl+\` 가 묶여 있을 때만). 키맵 파일에는 이 행과 메서드만 더했다.
- 내 판단: `ctrl+\ esc`(나가기)는 포커스를 다른 칸으로 옮기지 않고, 패널이 키보드를 놓는 것으로 했다(`GrabsKeys()==false`, 그 칸은 그대로 포커스). 위젯·초소형처럼 칸이 하나뿐인 모드에서도 같은 뜻이 되도록. 다시 들어가기는 그 칸에서 `enter`·`i` 또는 클릭이다. 상태줄 도움에 `enter 패널에 입력` 이 뜬다.
- 내 판단: `ctrl+\` 다음 h/j/k/l·화살표(포커스 이동)와 `p`(팔레트)는 패널이 앱에 부탁하는 msg 로 했다 — `ui.ShellActionMsg{Action}`·`ui.ShellAction`(`ShellFocusLeft/Down/Up/Right`, `ShellPalette`)를 `internal/ui/component.go` 에 더했다. `t` 는 `ui.ToolSurfaceMsg{Toggle}`, `z` 는 `Options.SingleCell` 이면 `ui.ToolSurfaceMsg{Zoom}`, 아니면 `Options.OnPrefix(ctx,"z")`.
- 내 판단: 접두키 결과에 `PrefixArm`(접두키를 눌러 기다리기 시작함, `Cmd` = 2초 타이머)을 맨 끝에 더했고, 타이머 msg 는 `(*Prefix).Update(msg) bool` 이 받는다. 계획 목록에는 "무장" 을 알리는 값과 시간 초과를 받는 길이 없었다. `(*Prefix).Cancel()` 도 더했다.
- 내 판단: `termpane.ExitedMsg` 는 Task 16a 목록에 있지만 패널이 스스로 끝남을 알리는 데 필요해 Task 16 의 `pane.go` 에서 정의했다. 패널은 `Init` 에서 `Handle.Done()` 을 기다리는 고루틴을 하나 두고 `ctx.Post` 로 보낸다.
- 내 판단: 초소형(테두리 없음)에서는 공유 중이거나 끝났을 때 패널 첫 줄을 제목 줄(`<제목> · ● 공유 중` / `… · 끝남 · 종료 코드 n`)로 쓰고 프로그램에는 한 줄 작은 크기를 준다. 초소형에는 맥락 줄이 없어서다. 19×10 은 TooSmall 이라 아무 패널도 그리지 않으므로 공유 표시 시험은 그 크기에서 "부서지지 않음" 만 본다.
- 내 판단: 테두리 제목의 공유 표시는 패널이 미리 자른다 — 제목을 (안쪽 폭 − 6)칸에 맞춰 먼저 자르고 ` · ● 공유 중` 을 붙인다. 앱의 테두리 제목 자르기(안쪽 폭 − 4)와 위젯 맥락 줄(아이콘·` ▾`)이 표시를 자르지 않게 하려는 것이다. 앱 쪽 그리기는 고치지 않았다.
- 내 판단: 키 독점은 앱 `routeKey` 의 맨 앞(공유 중 `ctrl+\` 대기 처리 바로 다음)에서 본다. 오버레이가 떠 있거나 로그인 화면이면 독점하지 않는다(종료 확인 같은 대화상자가 키를 받아야 하므로). 붙여넣기는 원래부터 포커스 컴포넌트로만 가서 따로 고치지 않았다.
- 내 판단: 공유 중(`ui.KillSwitchActive()`) 앱 전역 `ctrl+\` 는 오버레이보다 먼저 본다("masil 어느 화면에서든"). 다음 키가 `x` 면 `ui.KillAll`, 그 밖의 키는 버리고 취소, 2초가 지나면 취소. 패널이 키를 쥐고 있으면 패널의 접두키가 같은 일을 한다.
- 내 판단: 앱 `model.go` 에 두 줄을 더했다 — `escGen` 필드와 dispatch 맨 앞의 `m.grabMsg(msg)` 훅(`keys.go`). Files 목록 밖이다.
- 내 판단: 패널 도움말(`ctrl+\ ?`)은 `pane.go` 안의 작은 오버레이로 그렸다(예약 키 표, 도구의 `PrefixKeys`, `kbd_limits`, `kbd_nvim`). 앱 도움말(`?`)에는 `termhost.escape` 줄을 더하지 않았다(도움말 골든·`TestHelpToolRowsOnlyWithFlags` 그대로).
- 내 판단: 마우스 모드에서는 누름뿐 아니라 끌기·놓기도 프로그램에 보내려고 `Pane` 이 `ui.DragHandler` 도 구현한다. 마우스 모드가 아니면 휠은 보기 스크롤, 클릭은 다시 키보드 쥐기.
- 내 판단: 보기 스크롤 중에는 모든 키를 패널이 삼킨다(esc·q·end 끝, pgup/pgdn·위아래·home 이동). 휠을 끝까지 내리면 저절로 끝난다.
- 내 판단: Files 목록 밖 시험 파일 — `internal/ui/app/killswitch_test.go`(`TestGlobalKillWhileSharing`), `internal/ui/killswitch_test.go`, `internal/ui/keymap/keymap_test.go` 끝의 `TestMatchesEscapeVariants`.
- 새 골든: `internal/ui/termpane/testdata/TestPaneGolden/{200x60,120x40,80x24,50x20,40x14,shared_120x40}/pane.golden` 6개. 기존 골든 변화 0.

## Task 16a — `termpane.Launch` · `termpanetest`

- 내 판단: `opts.OnExit` 는 pane 방식에서만 불린다(패널 컴포넌트가 `ExitedMsg` 를 받을 때). exec·window 방식에는 그 msg 를 받을 컴포넌트가 없으므로 도구가 방송된 `ExitedMsg` 를 `InstanceID` 로 직접 받는다. 계획의 "OnExit 는 그대로 불리고" 를 "New 에 그대로 넘긴다" 로 읽었다.
- 내 판단: pane 방식의 `ExitedMsg` 는 패널 컴포넌트가 `Init` 된 뒤에야 온다(앱이 도구 인스턴스를 만들 때 Init 함). 도구가 `Launched.View` 를 자기 컴포넌트 안에 넣어 쓰면 그 `Init` 도 불러야 한다(문서 주석에 적음).
- 내 판단: `Launch` 가 `req.ToolID` 가 비었으면 `opts.ToolID` 로 채운다(`CurrentEnv` 의 `[tools.<id>] open` 을 읽으려고).
- 내 판단: 줄어든 방식 토스트 `termpane.downgraded(mode)` 의 mode 문구는 기존 `tools.mode_pane`·`tools.mode_window`(패널·외부 창)를 쓴다. 원한 방식은 `req.Mode`, 없으면 도구 설정 `open`.
- 내 판단: 패널 시작 색은 `ctx.Theme.P.FG/BG` 를 `PaneOptions.FG/BG` 로 넘긴다(OSC 10/11 답).
- 내 판단: `termpanetest` 와 실제 `Launch` 가 같은 토스트를 내도록 `termpane.DowngradedToast(t, req, env)`·`termpane.PaneLimitToast(t, max)` 와 형 `termpane.LaunchFunc` 를 내보냈다(계획 목록 밖, 더하기만).
- 내 판단: 시험 이음매 — `export_hook.go` 의 `SetLaunchForTest(f LaunchFunc) (restore func())`(시험 바이너리 밖이면 panic, `atomic.Pointer` 로 바꿔 끼움), `export_test.go` 의 `SetLaunchEnvForTest`·`SetStartPaneForTest`·`SetExecForTest`·`SetPopupForTest`(이 패키지 시험 전용).
- 내 판단: 가짜(`termpanetest.Fake`)는 기본 환경을 macOS·패널 켬·tmux 밖·초소형은 레이아웃에서·도구 설정 `open` 으로 둔다. pane 방식의 `Launched.View` 는 실제 `termpane.Pane` 이고, 그 안의 가짜 Handle 이 `"<program> pane WxH"` 를 그린다(키는 `Fake.Keys(instanceID)` 로 확인 — 계획 목록 밖, 더하기만). 가짜 Handle 의 `Done()` 은 nil 이라 끝은 `Fake.Exit` 로만 온다(같은 끝이 두 번 오지 않게). 패널 상한 토스트의 수는 `SetLimit` 값이다.
- 내 판단: `TestLaunchCwdMsg`·`TestLaunchNotifyMsg`·`TestLaunchExitedMsgAllModes` 의 pane 부분과 `TestLaunchChoosesMode` 의 pane 부분은 실제 PTY(`/bin/sh`·`/bin/cat`)로 돌고 Windows 에서는 건너뛴다. 상한 시험은 `StartPane` 이음매가 `ErrPaneLimit` 를 돌려주게 해서 본다.
- `go test -race ./internal/ui/...`: `internal/ui/e2e` 를 뺀 모든 패키지 통과. `internal/ui/e2e` 는 `-race` 에서 크기 훑기 시험이 기본 10분 제한을 넘어 시간 초과로 끝났다(데이터 경합 보고는 없음). `-race` 없이는 통과한다. 기준 커밋에서 같은 시간 초과가 나는지는 돌려 보지 않았다.

## Task 20 — 끌 때 확인 · 패널 안 masil 경고 · 기능을 켠 셸 무변화

- 내 판단: 종료 길은 `ctrl+c` 두 번과 팔레트 `종료` 둘이다(`/quit` 슬래시 명령은 없음). 둘 다 `requestQuit` 을 지난다. 팔레트 `종료` 는 패널이 없으면 예전 그대로 "종료할까요?" 확인(`*widgets.confirmDialog`, `theme_test` 그대로)이고, 패널이 있으면 바로 패널 확인을 띄운다(확인 두 번을 피함). 그래서 `commands.go` 의 `quit` 실행을 `quitRequestMsg{confirm: true}` 로 바꿨다 — Files 목록 밖이고, 한글 리터럴 4개는 `quit.go` 로 옮겨 패키지 수는 그대로다.
- 내 판단: 기본 선택 "아니오" 는 `widgets.Confirm` 을 만든 뒤 `→` 키를 한 번 넣어 맞췄다(`widgets` 는 고치지 않음). 확인 창 단추 문구는 `termpane.quit_yes`(함께 끄기)·`quit_no`(아니오), 제목 `termpane.quit_title`.
- 내 판단: 목록은 프로그램마다 한 번, 처음 시작한 순서, `count_item` 을 `", "` 로 잇는다(`lazygit 2개, nvim 1개`).
- 내 판단: 확인 창이 떠 있는 동안 `ctrl+c` 두 번은 두 번째 창을 띄우지 않는다(`Model.quitAsk`).
- 내 판단: "예" 는 `termhost.CloseAll(2초)` 를 부르고 `tea.QuitMsg` 를 돌려주는 Cmd 하나다(Cmd 고루틴에서 기다림). 시험 이음매는 `app.SetPanesForTest(running, closeAll)`(export_test.go).
- 내 판단: `MASIL_PANE=1` 경고 토스트는 `run.go` 의 `(*Model).nestedPaneWarning` 이 만들고 `Init` 이 한 번 낸다(시험 하네스는 `runProgram` 을 거치지 않으므로 `Init` 에 묶었다 — `model.go` 한 줄).
- 내 판단: A8.3 "상태줄에 안 읽은 수" 는 표준 모드에서 도구 표면이 앞에 있고 활성 항목이 도구 인스턴스(`RouteTool`)일 때만 오른쪽 맨 앞에 `💬 안 읽음 n`(`termpane.chat_unread`, `Store.TotalUnread` 의 안 읽음 수, 0 이면 없음)으로 보인다. 허브가 앞에 있을 때는 넣지 않았다 — 넣으면 Task 19 의 `TestHubGolden/120x40/*` 골든 3개가 바뀌기 때문이다. 허브에서도 보일지는 오너·다음 wave 판단으로 남긴다. 문구 키는 `tools.toml` 이 아니라 이 Task 의 `termpane.toml` 에 두었다(secrets 배치가 `tools.toml` 을 고칠 수 있어 병합 충돌을 피함).
- 내 판단: 이 때문에 Task 16 에서 만든 `TestPaneGolden/{120x40,shared_120x40}/pane.golden` 두 개의 상태줄이 바뀌어 `-args -update` 로 다시 만들었다(이 배치에서 새로 만든 골든, 기존 골든 아님).
- 내 판단: `TestFeaturesOnShellUnchanged` 는 `TestShellGoldens` 의 세 화면을 `uitest.AllSizes()` 모두에서 `Styled()`(색 포함 문자열)로 비교한다.
- 내 판단: Files 목록 밖 — `internal/ui/app/commands.go`(위), `model.go`(`quitRequestMsg` 처리 한 줄, `quitAsk` 필드, `Init` 의 경고 한 줄), `export_test.go`(`SetPanesForTest`).

## 미룬 것 (오너·다음 wave 확인)

- 오너 확인 목록 1번(`ctrl+\` 실기 표: 한국어 자판 ₩, macOS Terminal·iTerm2·Ghostty, Windows Terminal, GNOME Terminal) — 사람이 실기기로 확인할 일. 자동 시험은 `ctrl+\`·`ctrl+4`·0x1c 세 표기만 본다.
- `internal/ui/e2e` 의 `-race` 시간 초과(아래 리뷰 반영의 시간 비교 줄).
- 시험은 macOS 에서만 실제로 돌았다(Linux·Windows 는 vet 만).

## 리뷰 반영 (P0 review)

- 내 판단: pane 방식의 끝 감시는 `Launch` 가 패널을 만들 때 바로 시작한다(`(*Pane).watchExit`). 도구가 `Launched.View` 를 자기 컴포넌트 안에 두고 `Init` 을 부르지 않아도 `ExitedMsg` 가 온다. `Init` 은 그대로 두되 두 번째 감시를 만들지 않는다(`New` 로 직접 만든 패널은 지금처럼 `Init` 에서 시작). 위 Task 16a 의 "Init 해야 온다" 줄은 이것으로 바뀌었다.
- 내 판단: `Options.OnExit` 는 pane 방식 전용으로 문서에 못박았다(exec·window 에는 msg 를 받을 패널이 없어 ctx 와 함께 부를 곳이 없다). 모든 방식에서 할 일은 방송되는 `ExitedMsg` 를 받아서 한다. 계획·계약 문서는 고치지 않았다(계약에는 `OnExit` 이 없다).
- 내 판단: 패널의 `View` 는 이제 읽기만 한다 — 크기는 `Resize` 에서만, 포커스 보고는 `Update` 에서만 보낸다. 포커스는 `ui.FocusChangedMsg` 의 `RouteKey` 가 자기 도구 인스턴스 경로(`tool/<InstanceID>/<ToolID>`)인지로 정한다(처음 값은 참). 숨겨지면(`ctrl+\ t` 등) 프로그램에 포커스 아웃이 가고, 숨은 동안 터미널 blur/focus 가 와도 포커스 인을 보내지 않는다. 칸이 여럿인 도구(P2 작업공간)를 위해 `(*Pane).SetCellFocus(bool)` 를 더했다(계획 목록 밖, 더하기만).
- 내 판단: 초소형의 공유 표시는 패널 첫 줄이 아니라 탭 줄의 `도` 칸 점(`●`, `ui.Badge{Dot}`)으로 옮겼다(A7.7 "초소형은 탭 줄에"). 패널 `Title` 이 초소형·공유 중일 때 `Badge{Dot: true}` 를 돌려주고, 앱 `tabbar.go` 가 열린 도구 인스턴스 중 하나라도 그러면 `도` 칸에 점을 단다. 그래서 공유를 켜고 꺼도 프로그램 크기는 그대로다. 초소형에서 첫 줄 제목은 프로그램이 끝난 뒤(`끝남 · 종료 코드 n`)에만 쓴다. `TestPaneGolden` 골든은 바뀌지 않았다.
- 내 판단: 화면에 그리는 프로그램 이름(`input_hint`·`exit_confirm`·끌 때 목록)을 `textwidth.Sanitize` 로 거른다.
- 내 판단: `TestFeaturesOnShellUnchanged` 는 두 번 그릴 때 모두 `installStubTools` 로 다섯 도구와 도구 경로를 등록한다(실제 바이너리처럼). 제품 코드는 바꾸지 않았고 시험은 그대로 통과했다.
- 내 판단: A7.5 "masil 전역 키와 겹치게 바꾸면 경고" — `keymap.Load` 가 `termhost.escape` 와 전역 키(`app.quit`·`palette.open`·`search.global`·`notifications.open`·`window.prefix`·`focus.next`·`focus.prev`·`tools.toggle`, 곧 포커스 컴포넌트보다 먼저 판단하는 키)가 같은 키를 나누면 오류를 하나 내고, 바꾼 쪽(`termhost.escape` 먼저)을 기본값으로 되돌린다. 앱은 기존대로 시작 토스트 "키 설정 일부를 무시했어요" 를 띄운다. 경고만 하고 겹친 채로 두지 않은 것은, 겹치면 공유 중 즉시 끊기 접두키와 전역 키가 서로를 먹기 때문이다.
- 내 판단: A8.3 안 읽은 수는 표준 모드에서 도구 표면이 앞에 있고 도구 인스턴스가 하나라도 열려 있으면 허브가 앞에 있어도 보인다(허브만 있고 도구가 없으면 0.1.2 상태줄 그대로). 그래서 Task 19 의 0.1.3 골든 `internal/ui/tools/testdata/TestHubGolden/120x40/{hub_open,open_menu}.golden` 두 개의 상태줄이 바뀌어 `-args -update` 로 다시 만들었다(상태줄 한 줄씩, `💬 안 읽음 3 · ` 이 더해짐). 0.1.2 골든 변화는 없다.
- 내 판단: `MASIL_PANE` 은 `Run` 만 읽는다(`optionsFromEnv` → `Options.NestedPane`). 시험 하네스는 환경 변수를 읽지 않고 `uitest.WithNestedPane()` 으로만 켠다. `MASIL_PANE=1 go test -count=1 ./internal/ui/app/` 통과를 확인했다.
- `-race` 의 `internal/ui/e2e` 시간 비교: 기준 커밋 58376da 와 이 배치의 리뷰 전 끝(3fb9f05)을 `git archive` 로 임시 폴더에 풀어 `go test -race -count=1 -timeout 30m ./internal/ui/e2e` 를 같은 때 나란히 돌렸다. 둘 다 30분 제한에 걸려 실패했고, 멈춘 곳도 같다(`TestSweepHeights/home` 이 1분 9~10초째 돌던 중). 걸린 시간도 같다(real 1812.16초 / 1811.61초, user 1528.34초 / 1529.21초). 데이터 경합 보고는 둘 다 0건. 그래서 이 시간 초과는 기준 커밋부터 있던 것이고 이 배치가 만든 느려짐이 아니다. `-race` 없이 e2e 는 통과한다(162초).
