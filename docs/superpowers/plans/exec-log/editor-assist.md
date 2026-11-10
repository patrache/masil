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

- **자동추천은 LSP 없이** — 후보 = 그 언어의 키워드·내장 이름 + 버퍼에 이미 있는 단어(작업공간은 다른 열린 문서까지).
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

- [ ] 넣기가 커서 뒤 단어 나머지까지 바꾸는 규칙이 괜찮은지(아니면 접두만 바꾸기로).
- [ ] 입력 모드에서 ctrl+n 이 알림 대신 자동추천인 것.
- [ ] 라벨 "코드 하이라이트" / "코드 자동추천", 설정 → 화면 위치.
- [ ] 실제 터미널에서 색·메뉴 모양 확인(테스트는 가짜 화면) — Windows 실기기 미확인(vet·컴파일만).
- [ ] fix/tools-nav 와 합칠 때 `internal/ui/app/keys.go`, `internal/ui/component.go` 충돌 여부.
