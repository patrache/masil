# P0 batch "termhost" 실행 기록 (Task 11·13·14·15·17)

- 브랜치 `feat/v013-p0-termhost`, 기준 `dev/v0.1.3` c58fc9f, 2026-11-09.
- 계획: `docs/superpowers/plans/2026-11-09-masil-v013-P0.md` Task 11·13·14·15·17.
- 시험은 macOS(darwin/arm64)에서만 실제로 돌렸다. Linux·Windows 는 `GOOS=linux go vet`·`GOOS=windows go vet`(빌드 태그 확인)만 했다.

## Task 11 — termhost 겉모양 · 화면 전환(exec) · Exclusive

- 내 판단: 도구 설정 접근자(`ToolString`·`ToolBool`·`ToolOpen`·`ToolSection`·`ToolConfigSection`)는 `config.go` 가 아니라 새 파일 `internal/config/tools.go` 에 두고, 시험도 새 파일 `tools_test.go` 에 두었다. `config.go` 에는 `Terminal`·`Tools` 필드와 기본값만 더했다. i18n 줄기가 같은 `config.go` 에 `[ui] language` 를 더하므로 병합 충돌을 줄이려는 것이다.
- 내 판단: `Defaults()` 의 `Terminal.Embedded` 는 true, `Tools` 는 nil 이다. 그래서 `Save` 하면 config.toml 에 `[terminal] embedded = true` 가 새로 써진다(`[tools]` 는 안 써짐).
- 내 판단: `Choose` 규칙의 읽는 법 — 도구 설정 `open = "exec"` 는 늘 exec, `open = "pane"` 은 auto 와 같게(1~3번 규칙이 그대로 먹음), `open = "window"` 는 4번 규칙. 허브 메뉴처럼 `req.Mode` 를 직접 준 경우가 도구 설정보다 먼저다. 직접 준 pane 은 Windows·`embedded = false`·`TERM=dumb`·초소형에서 exec 로 내려가고 downgraded=true, 직접 준 window 는 tmux 3.2 미만·tmux 밖·Windows 에서 exec 로 내려가고 downgraded=true. 설정 `open = "window"` 인데 Windows 이면 1번 규칙이 먼저라 exec·downgraded=false.
- 내 판단: `CurrentEnv` 의 tmux 버전은 패키지 변수 `detectTmuxVersion` 으로 읽고, Task 13 의 `popup.go` 가 `init()` 에서 `TmuxVersion` 으로 바꿔 끼운다(Task 11 이 Task 13 보다 먼저라서). `$TMUX` 가 있을 때만 `tmux -V` 를 부른다.
- 내 판단: `ExitInfo.Err` 는 프로그램을 아예 못 돌렸을 때만 채운다(Code -1). 0 이 아닌 종료는 Code 만, 신호로 죽으면 Code -1·Err nil.
- 내 판단: `Exec` 시험을 위해 내부 이음매 `prepareExec`·`newProcessCommand` 를 두었다(tea 프로그램 없이 Run·콜백을 직접 부름).

## Task 13 — 외부 창 (tmux 팝업)

- 내 판단: `Request.Env` 는 tmux 버전과 관계없이 닿도록 `-- env K=V … <program>` 으로 넘긴다(팝업은 masil 이 아니라 tmux 서버의 환경에서 뜸). `Dir` 이 비면 `-d` 를 빼고, 제목이 비면(Sanitize 뒤) `-T` 를 뺀다.
- 내 판단: `Popup` 은 `$TMUX` 만 보고 `ErrNoTmux` 를 낸다. 3.2 미만 판정은 `Choose` 몫이라 `Popup` 은 다시 보지 않는다. 채널에는 tmux 명령 자체의 종료 코드가 온다(계획 시험 그대로).
- 내 판단: macOS 는 새로 쓴 실행 파일을 처음 실행할 때 수백 ms 검사한다. 그래서 시험의 가짜 tmux 는 설치 직후 한 번 미리 실행해 둔다(300ms 제한은 그대로).

## Task 14 — 내장 패널 엔진

- 내 판단: `SendKey`·`SendPaste`·`SendMouse`·`SetFocus` 는 잠금을 잡지 않고 입력 줄(4096)에 넣기만 하고, 패널의 입력 고루틴이 순서대로 에뮬레이터에 넘긴다. 처음처럼 호출하는 쪽이 잠금을 잡으면 `-race` 에서 키 1만 번이 1.9초 걸려 "1초 안" 기준을 못 맞췄고, 화면 그리기가 잠금을 오래 쥐면 UI 고루틴이 기다리게 되기 때문이다. 줄이 차면 버리고 개수만 기록한다.
- 내 판단: x/vt 의 응답 파이프를 멈출 때 `Emulator.Close` 를 부르지 않고 빈 쓰기로 읽는 고루틴을 깨운다. `Close` 는 막혀 있는 `Read` 와 `closed` 필드를 두고 경합(-race 가 잡음)하기 때문이다. 멈춘 뒤에는 에뮬레이터에 아무것도 넘기지 않는다(`closed` 표시).
- 내 판단: `Close()` 는 기다리지 않는다 — SIGHUP 을 보내고 바로 돌아오며, 2초 뒤에도 살아 있으면 고루틴이 SIGKILL 을 보낸다. 끝은 `Done()` 으로 안다. `CloseAll(grace)` 은 grace 만큼 기다린 뒤 SIGKILL 하고 최대 3초 더 기다린다.
- 내 판단: `Done()` 은 ExitInfo 를 한 번 보내고 닫힌다(두 번째부터 받는 쪽은 0 값).
- 내 판단: 자식이 끝나면 마지막 출력을 다 읽도록 최대 1초 기다린 뒤 PTY 를 닫는다(손자 프로세스가 PTY 를 쥐고 있어도 Done 이 늦어도 2초 안에 옴).
- 내 판단: `Running()` 은 프로그램 경로의 base 이름(`lazygit`, `nvim`)을 시작 순서대로 돌려준다.
- 내 판단: OSC 9 중 `9;4;…`(ConEmu 진행률 보고)는 알림으로 치지 않는다. 같은 종류 알림이 1초 안에 또 오면 버린다(미뤄서 보내지 않음).
- 내 판단: `OnCwd` 는 `file:` URL 이고 경로가 `/` 로 시작할 때만 부른다(호스트 이름은 보지 않음).
- 내 판단: ultraviolet 을 직접 import 하면 go.mod 에서 그 줄이 indirect 에서 direct 로 바뀌어야 해서, x/vt 가 다시 내보내는 형(`vt.KeyPressEvent`·`vt.MouseClick` 등)과 제네릭 `zeroOf` 로 셀·스타일 형을 이름 없이 다뤘다. go.mod·go.sum 은 바꾸지 않았다.
- 내 판단: 크기가 0 이하인 `StartPane` 은 80×24 로 시작한다.
- 내 판단: Files 목록 밖에 시험 파일 두 개를 더 만들었다 — `pane_windows_test.go`(Windows 전용 `TestStartPaneUnsupportedOnWindows`, 빌드 태그 시험)와 `helpers_test.go`(시험 도우미). 둘 다 이 패키지 안의 새 파일이다.
- 내 판단: `TestPaneEnv` 는 `env` 전체 대신 `echo "T=$TERM …"` 한 줄로 확인한다(전체 환경은 화면을 넘침).

## Task 15 — 갈래 · 입력 주입 · 재생 전용 화면

- 내 판단: 갈래 버퍼는 고리 대신 1 MiB 상한의 바이트 조각으로 두었다(넘치면 그 갈래를 떼고 쌓인 바이트를 버림 — 계획의 동작과 같음).
- 내 판단: `Inject` 는 `SendKey` 가 결국 닿는 쓰기 줄(`in`)에 바로 넣는다. 그래서 같은 순간의 `SendKey` 와 `Inject` 사이 순서는 보장하지 않는다(각 출처 안의 순서는 지킴).
- 내 판단: 대체 화면 중의 snapshot 은 받는 쪽을 대체 화면으로 바꾸고 그 화면만 그린다. 대체 화면 아래의 주 화면 내용은 옮기지 않으므로, 자식이 대체 화면을 나가면 재생 화면의 주 화면은 비어 있다(P2 가 알아 둘 것).
- 내 판단: `Replay` 는 질의 응답을 버리는 고루틴을 하나 두고, `Replay` 가 쓰레기 수집될 때 `runtime.AddCleanup` 으로 멈춘다(겉모양에 `Close` 가 없어서).
- 내 판단: 갈래가 붙은 채 패널이 끝나면 모든 갈래를 떼어 `Shared()` 가 false 가 된다. 끝난 패널의 `AttachTap` 은 snapshot 만 주고 아무것도 붙이지 않는다.

## Task 17 — 레이아웃 `SlotTool` · `ToolOpen` · `Zoomed`

- 내 판단: 지휘소에서 `Zoomed` 는 `ToolOpen` 이 거짓이어도 도구 자리(폭 = chat+feed+right)를 만든다(넓게 보기는 도구 표면이 앞에 있을 때만 켜지므로).
- 내 판단: 표준 `Zoomed` + 스레드 열림이면 사이드바만 없애고 Main/스레드 위아래 나눔은 전체 폭으로 그대로 둔다.
- 내 판단: 새 시험은 새 파일이 아니라 `layout_test.go` 끝에 붙였고, 다이어그램 골든에서 `SlotTool` 글자는 `o` 다. 새 골든 파일 5개(`tool_open_200x60`, `tool_zoomed_200x60`, `tool_open_120x40`, `tool_zoomed_120x40`, `tool_zoomed_80x24`)만 생겼고 기존 골든은 그대로다.
