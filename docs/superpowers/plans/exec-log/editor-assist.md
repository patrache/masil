# v0.1.3-1 — 코드 편집기: 하이라이트 + 자동추천

오너 요청(2026-11-10 16:38): "코드 편집에 하이라이터 및 자동추천 기능 이거 넣어야 한다."
Branch `feat/editor-assist` (origin/main 309d23fb = v0.1.3 에서). push·tag·배포 안 함.
대상은 masil 안 편집기 둘 — 오늘의 문제 내장 편집기(goeditor)와 작업공간 코드 칸. nvim 은 범위 밖.

## 바뀐 것

| 커밋 | 내용 |
|------|------|
| 09527775 feat(codeassist) | 새 패키지 `internal/codeassist`: 보이는 창(+위 60줄)만 chroma 로 렉싱하고 그 글이 바뀔 때까지 캐시, LeetCode 15개 언어 키워드·내장 표(한 파일), 단어 세기(줄 단위 증분 `Index`), 후보 순위. `theme.EditorTokenStyle`(CodeStyle 색, 배경 없음, 이름·텍스트는 FG). config `[ui] code_highlight`, `code_complete` (기본 켜짐) |
| b31093da feat(ui/workspace) | 코드 칸 편집기: 파일 확장자로 언어, 보이는 줄에 문법 색, 입력 모드 자동추천 메뉴(최대 8줄 보임, 커서 줄 아래·자리 없으면 위). 다른 열린 문서의 단어도 후보. 붙여넣기는 메뉴를 거치지 않음. 앱: `ui.CodeCompleter` 에 ctrl+n/ctrl+p 를 하드 전역보다 먼저, Surface 는 `ui.Completer`(메뉴 열린 동안 tab = 넣기) |
| cb933b20 feat(ui/leet) | goeditor 사본에 masil 패치 2개(README 에 기록): `SetSyntaxStyle`, `CompletionMenuVisible`. 내장 편집기가 고른 언어로 색칠(같은 파일을 쓰는 언어 변경 — python3/python — 도 따라감), goeditor 의 타이머 자동 트리거는 끄고 키마다 masil 이 요청·응답 |
| 8c15a026 feat(ui/settings) | 설정 → 화면에 "코드 하이라이트", "코드 자동추천" 켜기/끄기(카탈로그 ko·en). 설정 골든 5개와 가장 넓은 이름 테스트가 새 두 줄을 따라감(의도된 골든 변경) |
| (이 커밋) docs | README.md / README.ko.md 도구 절에 한 줄씩, 이 로그 |

키(두 편집기 같음, 입력 모드):
- 단어 글자 2개부터 메뉴가 열림. `ctrl+n` / `ctrl+space` 로 한 글자·빈 자리에서도 강제로 열기.
- `↑`/`↓`, `ctrl+n`/`ctrl+p` 이동, `tab`/`enter` 넣기, `esc` 는 메뉴만 닫음(다시 `esc` 면 지금처럼 입력 모드를 나감).
- 메뉴가 없으면 `enter` 는 줄바꿈, `tab` 은 지금처럼 다음 창.
- 단어가 아닌 글자(공백·괄호 등)나 커서 이동은 메뉴를 닫음. 지우기는 열린 메뉴를 다시 채움.

성능: 하이라이트는 보이는 창 + 위 60줄만 렉싱하고, 창의 글이 같으면 다시 렉싱하지 않음(키 하나 = 창 한 번).
2만 줄 파일에서 키 7개 + 그리기 7번이 0.14초(`TestLargeFileTyping`). 단어 색인은 바뀐 줄만 다시 셈.
원격 편집(공동 편집)은 버퍼의 지금 글로 그 자리에서 렉싱하므로 색이 맞음(`TestHighlightFollowsRemoteEdit`).

## 테스트 (TDD — 실패 먼저 확인 후 구현)

- `internal/codeassist`: Go·Python 조각의 스팬(키워드·함수 이름·숫자·주석·문자열, 줄을 빈틈없이 덮음), 15개 언어 모두
  `return` 이 키워드(PHP 는 `<?php` 없이), 창 캐시(2만 줄에서 창 밖은 읽지 않음, 같은 창은 다시 렉싱 안 함), 확장자 → 언어,
  후보 순위(대소문자 일치 접두 먼저 → 빈도 → 짧은 것 → 가나다/abc, 커서 아래 단어 제외, 여러 버퍼 합산), 증분 색인.
- `internal/theme`: `TestEditorTokenStyle`.
- `internal/config`: 기본값, 옛 파일은 켜짐 유지, 끄기 저장.
- `internal/ui/workspace/editor`: 하이라이트·끄기·원격 편집 후 색, 2글자 규칙, 최대 8개 보임, **넣기는 op 하나·메뉴 이동은 op 0**,
  메뉴 없을 때 enter = 줄바꿈 / 있을 때 넣기, **esc 는 메뉴 닫기 → 다음 esc 가 입력 모드 나가기**, ctrl+n·ctrl+space 강제,
  끄기, 다른 버퍼 단어, 붙여넣기, 큰 파일 속도.
- `internal/ui/workspace`: `TestGuestCompletionSendsOnlyOnAccept`(참가자: 메뉴 이동 중 doc_op 0개, 넣기에 1개, 소유자 글과 일치,
  esc 순서), `TestCodeAssistSwitches`(설정 스위치), `TestCtrlNInCodeCell`(입력 모드 ctrl+n 은 메뉴, 일반 모드는 여전히 알림).
- `internal/third_party/goeditor`: 패치 두 개.
- `internal/ui/leet`: `TestGoeditorHighlight`(키워드 색, 끄기), `TestGoeditorCompletion`(2글자, tab 넣기, esc 순서, ctrl+n, enter 줄바꿈).
- `internal/ui/settings`: 두 토글 저장, 골든 갱신.

실행 결과(macOS, Go 1.26.6):
- `go vet ./...`, `GOOS=windows go vet ./...`, `GOOS=linux go vet ./...` — 모두 깨끗.
- **전체 `go test -count=1 ./...` (-short 없음) — 85개 패키지 ok, 실패 0.** 그 전 한 번의 전체 실행에서
  `internal/ui/leet` `TestModeSwitchKeepsBothTools` 가 "Cmd blocked > 1s" 로 한 번 실패 — nvim 패널 + 가짜 lazygit
  (셸 스크립트 실행) 경로라 내장 편집기 코드를 거치지 않고, 단독 5회·전체 재실행에서 통과: 부하 때의 시간 초과로 봄.
- `go test -race` (codeassist, ui/workspace/..., ui/leet, third_party/goeditor) — ok.
- `go generate ./internal/i18n` — 재생성(zh-Hans 누락 경고는 기존 것). 한글 리터럴 래칫(`legacy_literals.txt`)·
  `pseudo_pending.txt` 변경 없음 — 새 Go 코드에 한글 리터럴 없음.

## 내 판단 (오너가 정하지 않은 것)

- **자동추천은 LSP 없이 — 오너 요구에 못 미침.** 오너는 16:41 에 "lsp 수준의 추천이 있어야 한다." 라고 분명히 요구했다.
  이번 패치는 LSP 수준이 아니라 **키워드 + 버퍼 단어 추천**이다(LSP 연동은 들어 있지 않음 — 아래 오너 확인 참고).
  후보 = 그 언어의 키워드·내장 이름 + 버퍼에 이미 있는 단어(작업공간은 다른 열린 문서까지).
  순위는 대소문자까지 맞는 접두 → 대소문자 무시 접두 → 빈도 → 짧은 것 → abc. 커서 아래 단어 자체는 빼고, 두 글자 미만 단어와
  숫자로 시작하는 것은 세지 않음.
- **넣기는 커서 아래 단어 전체를 바꿈**(goeditor 의 `InsertCompletion` 과 같은 규칙으로 두 편집기를 맞춤). 단어 한가운데서
  넣으면 커서 뒤 나머지도 바뀐다 — `u` 로 되돌릴 수 있음. vim 의 ctrl+n 처럼 접두만 바꾸게 하려면 goeditor 사본을 더 고쳐야 함.
- **입력 모드의 ctrl+n 은 편집기 것**: 원래 ctrl+n 은 하드 전역(알림)이라 편집기까지 오지 않았다. 스펙대로 메뉴를 열려고
  `ui.CodeCompleter` 를 두어, 코드 편집기가 입력 모드이고 자동추천이 켜져 있을 때만 ctrl+n/ctrl+p 를 먼저 준다. 일반 모드·
  자동추천 꺼짐·읽기 전용이면 ctrl+n 은 여전히 알림.
- **tab**: 메뉴가 열려 있을 때만 편집기가 받음(`ui.Completer`, 채팅 자동완성과 같은 길). 메뉴가 없으면 지금처럼 다음 창.
- **색**: masil 테마의 코드 블록 색(CodeStyle)을 배경 없이 쓰고, 이름·일반 텍스트는 FG(코드 블록은 Acc3 — 편집기에서는
  거의 모든 글이 Acc3 가 되어 키워드가 안 보임).
- **하이라이트 범위**: 보이는 창 + 위 60줄만 렉싱. 60줄보다 위에서 시작한 아주 긴 주석·여러 줄 문자열은 창에서 코드로
  칠해질 수 있음(전체 파일을 키마다 렉싱하지 않는 대가). goeditor(오늘의 문제)는 원래부터 보이는 창만 렉싱함.
- **설정 위치**: 설정 → 화면 절에 두 줄, config `[ui]` 표(`code_highlight`, `code_complete`). 라벨 ko "코드 하이라이트" /
  "코드 자동추천"(오너 표현), en "Code highlighting" / "Code completion".
- **메뉴는 로컬**: 작업공간에서 메뉴 상태·이동은 아무에게도 안 보내고, 넣은 글만 보통 편집(OT op 하나)으로 간다.
- 앱 안 `?` 도움말은 편집기의 vim 키를 나열하지 않으므로(접두키만) 바꾸지 않음. 오늘의 문제 편집기 맨 아래 안내
  "esc esc 나가기" 는 메뉴가 열린 경우 esc 가 하나 더 필요하지만 문구는 그대로 둠.

## 오너 확인

- [ ] 오너는 "lsp 수준의 추천"을 요구함 — 이번 패치는 LSP 없음(키워드·버퍼 단어). LSP 연동을 이번 0.1.3-1 에 넣을지,
  다음 패치로 미룰지 결정 필요. (README.md / README.ko.md 도구 절도 "no LSP" / "LSP 없음" 이라고 적혀 있음.)
- [ ] 넣기가 커서 뒤 단어 나머지까지 바꾸는 규칙이 괜찮은지(아니면 접두만 바꾸기로).
- [ ] 입력 모드에서 ctrl+n 이 알림 대신 자동추천인 것.
- [ ] 라벨 "코드 하이라이트" / "코드 자동추천", 설정 → 화면 위치.
- [ ] 실제 터미널에서 색·메뉴 모양 확인(테스트는 가짜 화면) — Windows 실기기 미확인(vet·컴파일만).
- [ ] fix/tools-nav 와 합칠 때 `internal/ui/app/keys.go`, `internal/ui/component.go` 충돌 여부.

## 리뷰 반영

리뷰 7건을 하나씩 직접 재현·확인한 뒤 반영함(모두 타당, 건너뛴 것 없음). 동작 버그 4건은 TDD — 실패하는 테스트를 먼저 보고 고침.

| # | 지적 | 처리 | 테스트 |
|---|------|------|--------|
| 1 | 짧은 창(예: 50x10, 커서가 중간)에서 메뉴가 8줄보다 적게 그려지는데 `moveMenu` 는 8줄 기준으로 스크롤 → 안 보이는 단어가 선택·넣기됨 | `drawMenu` 가 실제 그린 줄 수 `n` 으로 창(`top`)을 선택 항목에 맞춰 다시 잡고 `e.as.top` 에 저장 | `TestCompletionShortPaneKeepsSelectionVisible`(아래로 6번·위로 9번 모두 선택 항목이 화면에 있음) |
| 2 | 공동 편집자의 원격 op 마다 `refreshMenu` 가 선택을 0 으로 되돌림 | `fillMenu(keep)`: `ApplyRemote` 에서는 선택한 단어가 새 목록에 있으면 그 자리를 유지(없으면 0). 내가 타이핑할 때는 지금처럼 0 | `TestRemoteEditKeepsSelection` |
| 3 | `SetReadOnly(true)`(참가자 등급 하락) 후에도 메뉴가 열린 채 일반 모드에서 그려지고 다음 enter/tab/esc 를 먹음 | `SetReadOnly(true)` 가 메뉴를 닫음. 방어선: `menuLive()`(입력 모드·편집 가능·자동추천 켜짐)가 아니면 `HandleKey` 가 메뉴를 닫고 키를 그대로 넘기고, `drawMenu` 는 그리지 않음 | `TestReadOnlyClosesMenu` |
| 4 | 실행 중 테마 바꾸기 후 작업공간 코드 칸이 옛 테마 토큰 색을 유지 | `Editor.SetTheme(th)`(바뀌면 `e.th` 교체·토큰 색 캐시 비움). `codeCell.applyAssist` 가 `ctx.Theme` 으로 부름(그리기·키마다). 기존부터 있던 바탕 글자색 고착도 같이 풀림 | `TestSetThemeRecolorsTokens` |
| 5 | 로그가 "LSP 없이"를 내 판단으로 적었으나 오너는 16:41 "lsp 수준의 추천이 있어야 한다." 라고 요구함 | 내 판단 항목에 오너 요구를 인용하고 이번 패치는 키워드·버퍼 단어 추천(LSP 수준 아님)이라고 명시. 오너 확인에 미결 항목 추가 | — |
| 6 | 오늘의 문제 편집기에서 언어 변경(python3 → python, 같은 .py) 때 키워드·하이라이트를 다시 적용하는 규칙이 테스트 안 됨 | 테스트 추가: python3 에서 `nonl` → `nonlocal` 메뉴, `l` 로 python 으로 바꾼 뒤 편집기가 유지되고 키워드 색이 나오며 `nonl` 메뉴가 안 뜸. `instance.go` 의 `g.setLang(...)` 호출을 빼면 실패함을 확인(뮤턴트 검사) | `TestGoeditorLanguageChangeKeepsEditor` |
| 7 | 키 표의 `ctrl+n` = 알림에 코드 편집기 예외가 없음 | README.md / README.ko.md 키 표 행에 예외 표기(도구 절 링크). 앱 안 `?` 도움말(i18n 카탈로그)은 선택 사항이라 이번엔 바꾸지 않음 | — |

커밋: 4738e531 fix(ui/editor) (코드 1–4·6), (이 커밋) docs (5·7, 이 절).

실행 결과(macOS):
- `go vet ./...`, `GOOS=windows go vet ./...` — 깨끗.
- 전체 `go test -count=1 ./...` (-short 없음): 첫 실행에서 `internal/ui/leet` `TestModeSwitchKeepsBothTools` 가 "Cmd blocked > 1s"
  로 한 번 실패(앞 절에 적은 것과 같은 부하 시간 초과, 이번 변경과 무관한 nvim 패널 + 가짜 lazygit 경로) — 바로 다시 돌린 전체
  실행은 85개 패키지 ok, 실패 0. 그 테스트 단독 `-count=5` 도 ok.
- `go test -race` (ui/workspace/..., ui/leet) — ok.
