# 오늘의 문제 터미널 (A) · 풀이 언어 표시 (C) — 실행 기록

브랜치 `release/v0.1.3-1` (worktree `rel0131`), 기준 `9cf544e5`.
오너 지시 17:07 (코테 문제에서도 터미널), 17:10 정정 (B 전체 화면은 하지 않음 — 기존 확대로 충분한지 먼저 봄),
18:22 추가 (l 로 바꾼 언어가 안 보임). B 는 구현하지 않았다. 푸시·태그·배포 없음.
Windows 기기 없음: Windows 는 `GOOS=windows go vet ./...` 와 termhost.Choose 의 Windows 규칙(테스트의 가짜 env)으로만 확인했다.

## 커밋

| 커밋 | 제목 |
|---|---|
| 646e2f40 | feat(leet): a terminal under the editor (t), the solve language always shown |
| 8a5f14a5 | docs(readme): Daily LC terminal (t) and the always-shown solve language |
| (이 기록) | docs(exec-log): leet terminal (A) and the visible solve language (C) |

A 와 C 는 같은 코드(코드 칸 배치·키 안내·`edRect`)를 고쳐서 한 커밋으로 묶었다.

## 검증

- `go test ./...` (-short 없음): 87 패키지 모두 ok.
- `go vet ./...` GOOS=darwin / windows / linux: 깨끗함. `gofmt -l internal/`: 없음.

## A. 오늘의 문제 터미널 (`internal/ui/leet/terminal.go`)

- 키 `t` (문제 화면에서 비어 있던 키) + 명령 팔레트 `leet.terminal` 「오늘의 문제: 터미널 열기」. 팔레트는 앞에 있는
  오늘의 문제 인스턴스(없으면 처음 열린 것)에 `terminalRequestedMsg` 를 보낸다 (작업공간 `wsCommand` 와 같은 방식).
- 셸: `$SHELL`, 없으면 Windows 는 `%COMSPEC%` → `cmd.exe`, 그 밖은 `/bin/sh`. 인자 없음, cwd = 풀이 폴더
  (`solutionDir`, `solutionFile` 이 쓰는 폴더 — 없으면 만든다). `termpane.Launch` (ToolID leet, Mode 지정 없음 →
  `termhost.Choose`): 패널이 되면 코드 칸 편집기 아래, Windows·초소형·`[tools.leet] open = "exec"` 이면 화면 전환,
  tmux + `open = "window"` 이면 팝업. instance id 는 `<인스턴스>#term`.
- 배치 (내 판단): 코드 칸 = 머리 줄(언어·파일) + 편집기 칸 + 「─ 터미널 · zsh ─」 머리 줄 + 터미널.
  터미널이 있으면 아래 절반을 터미널이 쓴다. 칸이 4줄보다 낮으면 포커스가 있는 쪽만 보인다. 위젯 모드는 「코드」 탭에
  같은 배치. 초소형에서 (넓은 화면에서 연 뒤 줄였을 때) 터미널에 포커스가 있으면 편집기처럼 화면을 다 쓴다.
- 포커스: `edFocus` / `termFocus` 중 하나. `e` 는 편집기, `t` 는 터미널로 들어간다 (`ctrl+\ esc` 로 나온 뒤 다시
  들어갈 때 enter 를 보내 키를 다시 잡게 함 — 기존 nvim 칸과 같음). 두 패널에 `SetCellFocus` 로 알린다.
  붙여넣기는 포커스 쪽에만, 앱의 `FocusChangedMsg` 는 터미널의 route 로 바꿔 준다 (작업공간 Surface 와 같음).
- 터미널 안 `ctrl+\ r` / `ctrl+\ s` 는 nvim 칸처럼 실행·제출.
- 4개 한도: Launch 의 `ErrPaneLimit` 과 그 토스트를 그대로 돌려준다 (터미널은 생기지 않음).
- 셸이 끝나면 패널을 치운다 (작업공간처럼). 도구를 닫으면 셸을 끝낸다 (nvim 칸의 기존 규칙과 같음: `CloseTool`).
  하루가 바뀌어 문제가 바뀌어도 터미널은 둔다 (폴더가 같다).
- 상태 줄: 터미널이 키를 잡으면 termpane 의 「⌨ zsh 에 입력 중 · ctrl+t 나가기」. 아래 키 안내에 `t 터미널`.
- 보안 메모 (내 판단): 작업공간은 믿을 수 없는 저장소 때문에 로그인 셸을 `$HOME` 에서 띄운다. 여기 폴더는 사용자 자신의
  풀이 폴더라 그 규칙이 필요 없고, 일반 대화형 셸(로그인 셸 아님)을 그 폴더에서 바로 띄운다. 쿠키는 시작 때 이미
  환경에서 빠져 있다 (`takeEnvCookies`), 그래서 셸 환경에도 없다.

## C. 풀이 언어 표시 + l 토스트

- 코드 칸 머리 줄 `CodeTitle` 「Python3 · 2026-11-05-1606-….py」 (모든 넓은 모드와 위젯의 코드 탭).
- 키 안내 끝 `LangHint` 「l 언어: Python3」: 안내가 좁으면 안내를 먼저 자르고 언어는 남긴다 (`joinHint`).
  그래서 `hints_wide` 에서 「l 언어」를 뺐다 (끝에 붙으므로). 초소형은 새 줄 `TinyMore` 「t 터미널 · l 언어: Go」.
- `l`: 토스트 `LangSwitched` 「언어: Go → Python3」.
- 편집기가 열려 있으면: 같은 파일이면 (python3 ↔ python) `setLang` 이 강조 언어를 바꾼다 — 확인했다
  (python3 → chroma "python", python → "python2", 테스트로 고정). 파일이 바뀌면 예전에는 편집기를 닫기만 했다
  (자리 비움). 이제 닫고 새 언어의 파일로 다시 연다. 내장 편집기는 바로 연다. nvim 칸은 **옛 nvim 의 종료
  (ExitedMsg) 가 온 뒤에** 연다: 실제 termhost 의 Close 는 신호만 보내고 종료는 나중에 오는데, 그 종료 메시지는
  같은 instance id 라 바로 띄운 새 nvim 을 끝난 것으로 만들어 버리고, 옛 패널이 한도에서 빠지기 전이기도 하다
  (발견해서 고침 — 처음엔 Sequence 로 바로 열었음; `TestNoRawTimersInUI` 도 tea.Sequence 를 막는다).
  그 사이 사용자가 e 로 직접 열었으면 옛 종료는 무시한다. 포커스는 있던 자리(편집기·터미널·문제)에 그대로 둔다.
- 설정 대화상자의 언어 바꾸기도 같은 `langChangedMsg` 라 편집기가 같이 따라간다 (토스트는 l 에만 — 대화상자에는
  값이 바로 보임).

## i18n

새 키 (ko + en): `cmd_terminal`, `code_title`, `lang_hint` (max 20), `lang_switched`, `term_title` (max 30),
`tiny_more` (max 30). 바꾼 키: `hints_wide` (t 추가, l 은 끝으로), `hints_widget` (t 추가, en 은 56칸에 맞게
「← → tabs」 로 줄임). zh-Hans 는 leetcode 영역이 비어 있어 대체 언어로 나간다 (기존과 같음).

## 테스트

- `internal/ui/leet/terminal_test.go`: t 가 `$SHELL` 을 풀이 폴더에서 패널로 연다 · 키가 셸로 감 (r 포함) ·
  ctrl+\ r 실행 · ctrl+\ esc 뒤 r 은 도구의 실행 · t 다시 = 같은 셸 / 편집기 아래 배치, e·t 로 포커스 이동,
  키가 섞이지 않음, 크기 바꿔도 화면 정합 / 패널 한도 토스트 / 셸 종료 시 패널 치움, 도구 닫으면 셸 끝남 (한도 1로 확인) /
  팔레트 명령 / Windows·초소형은 화면 전환 / `userShell` 표.
- `internal/ui/leet/lang_test.go`: 모든 크기에서 l 전후 언어 표시 + 토스트 / 위젯 코드 탭 머리 줄 / l 이 열린
  편집기를 .py 로 바꾸고 강조 언어가 바뀜, python3 → python 은 같은 파일에 강조만 바뀜.
- 골든 (의도한 변경만): `TestProblemGolden`, `TestEditorGoeditorGolden`, `TestResultGoldens`, `TestShareGolden` —
  코드 칸 머리 줄 한 줄, 키 안내의 `t 터미널`·`l 언어: Go`, 초소형의 `t 터미널 · l 언어: Go` 줄.

## 오너 확인

- 배치: 터미널을 코드 칸 아래 절반에 둔 것 (전체 화면 B 는 하지 않음 — 기존 `ctrl+\ z` / `ctrl+w z` 확대 그대로).
- 도구를 닫으면 셸도 끝나는 것 (nvim 칸과 같은 규칙).
- en `hints_widget` 문구를 「← → tabs」 로 줄인 것.

## 리뷰 반영

리뷰 지적 3건 (A·C 만 — 전체 화면 B 는 오너가 취소). 셋 다 확인 후 반영, 건너뛴 것 없음.

1. **(major) nvim 을 연 채 l 로 언어를 바꾸면 새 nvim 이 키를 가져감** — 확인됨. l 이 도구에 닿으려면
   ctrl+\ esc 로 nvim 에서 나와 있어야 하는데 그때도 edFocus 는 true 라 `reopenEditorMsg{focus: true}` 가
   되고, 새 termpane 은 grab=true 로 시작해 다음 l 이 nvim 의 커서 이동이 되었다 (Go → Python3 다음에서 멈춤).
   고침: nvim 칸이고 옛 nvim 이 키를 갖고 있지 않았으면 `focus=false` 로 다시 연다
   (`focus: in.edFocus && (!pane || p.GrabsKeys())`) — 도구가 키를 갖고, e 로 새 nvim 에 들어간다.
   TDD: `TestLangSwitchReopensNvim` 에 「Exit 뒤 l 이 다시 언어를 바꾸고 (l 언어: Python), nvim 키에 l 이
   없다」 를 더해 실패 확인 후 고침.
2. **(minor) 셸이 키를 가진 동안 맨 아래 안내가 문제 화면 키** — 확인됨 (`e 편집기 · t 터미널 · r 실행 …` 인데
   그 키들은 셸로 감). 고침: `hints()` 에서 `termFocus && term.GrabsKeys()` 를 먼저 보고 새 i18n 키
   `term_hint` (ko 「ctrl+\ r 실행 · ctrl+\ s 제출 · ctrl+\ esc 나가기」, en 「ctrl+\ r run · ctrl+\ s submit ·
   ctrl+\ esc leave」) 를 쓴다. 끝의 `l 언어: X` 는 joinHint 로 그대로. TDD: `TestTerminalKeyOpensShell` 에
   셸 안에서 ctrl+\ r 안내 + `l 언어: Go` 단언 (실패 확인). 그 테스트의 「t 다시 들어간 뒤 t 터미널 안내」
   단언은 새 동작과 어긋나므로 「ctrl+\ esc 로 나온 뒤 t 터미널 안내, 셸 안내 없음」 으로 옮겼다.
3. **(minor) leet 터미널에서 ctrl+t 로 나가는 테스트 없음** — 확인됨 (동작은 이미 맞음: app 의 grabbing
   특례). 테스트만 추가: `toolsnav_test.go` `TestCtrlTLeavesLeetTerminal` — ctrl+t, ctrl+ㅅ, BaseCode 't' 의
   ctrl+ㅅ 셋 다 도구 화면을 떠나고 셸 키에는 `l s` 만 남는다. 처음부터 통과 (회귀 고정용).

검증: `go test ./...` (-short 없이) 통과, `GOOS=windows go vet ./...` 통과.
