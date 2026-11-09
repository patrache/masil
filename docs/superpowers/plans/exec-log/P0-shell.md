# P0 batch "shell" 실행 기록 (Task 9·10·12·25)

- 브랜치 `feat/v013-p0-shell` (기준 `dev/v0.1.3` 5b4d9d2), 2026-11-09.
- 계획: `docs/superpowers/plans/2026-11-09-masil-v013-P0.md` Task 9·10·12·25. 클라이언트 쪽만(server/ 변경 없음). push·tag·배포 없음.

## 커밋

- Task 9 3d367b8 — alert 템플릿 카탈로그 렌더·서버 문구 대체·`ui.RegisterAlertTarget`
- Task 10 8fb2db3 — 막히지 않는 FIFO 실시간 다리·넘침 재동기화·`ui.Poster`
- Task 12 10e78a3 — exec 프로그램이 터미널을 쥔 동안 OS 알림·돌아온 뒤 요약 토스트
- Task 25 (이 커밋) — 카드 렌더러 등록부·채팅 카드 그리기·body 대체

## Task 9 — alert 템플릿 · RegisterAlertTarget

- 내 판단: 생성기 param 규칙은 `alerts` 영역만 완화했다 — 언어마다 쓰는 param ⊆ 선언, 모든 언어의 합집합 = 선언(서버 Task 8 규칙과 같음, ko `doc.comment_mention` 은 `{space}` 를 뺌). `internal/i18n/gen` 은 Files 목록 밖이다.
- 내 판단: 계획의 "알림함 enter" 는 ToastAlert 의 enter·클릭으로 옮겼다 — 알림함 목록은 `api.Notification` 이라 alert 가 아니다.
- 내 판단: 등록 안 된 target, 또는 등록된 함수가 nil Cmd 를 돌려주면 지금의 알림함 경로로 간다(기존 시험·골든 그대로).
- 내 판단: 본문 출처는 카탈로그에 `<id>_body` 키가 있으면 템플릿, 없으면 서버 `a.Body`(Sanitize). Go 쪽에 body 종류 표를 따로 두지 않았다.
- 내 판단: zh-Hans alerts 문구는 서버 초안을 그대로 옮겼다.
- 내 판단: 데몬 `ledgerSink` 의 언어는 시작할 때와 설정 다시 읽기 때 `setLang` 으로 따라간다.
- 내 판단: api testdata 샘플(`Alert`·`FocusSummary`)에 `template` 키를 더하고 `AlertTemplate` 샘플을 새로 넣었다(엄격 왕복 시험).
- 내 판단: `ui/fun/focus_summary.go` 는 아직 `a.Title` 을 쓴다 — Files 목록 밖(P5 몫).

## Task 10 — 실시간 다리 큐 · Poster

- 내 판단: 넘치면 큐는 "재동기화 표식이 배달될 때까지 이벤트를 버림" 상태로 머문다(상태 msg 만 남은 하나를 바꿈). 그래서 20번 push 가 정확히 상태 하나 + resync 하나로 배달된다.
- 내 판단: 부트스트랩이 도는 중에 온 `resyncNeededMsg` 는 기존 `bootAgain` 을 써서 많아야 한 번 더 돈다 — 시험 기대 GET /v1/me 3번(처음 1 + 비행 중 1 + bootAgain 1). 계획의 "두 번→한 묶음" 글자 그대로와 다른 까닭: 비행 중에 버려진 이벤트가 스냅숏보다 뒤일 수 있다.
- 내 판단: post 줄도 같은 한도 4096 을 쓰고, 꽉 차면 가장 오래된 일반 post 를 버린다(slog.Warn). Coalesce 키가 같은 것은 제자리에서 바꾼다.
- 내 판단: UI 가 막힌 동안 핑이 계속 나가는 시험은 3초가 아니라 1초(하트비트 20ms)로 줄였다.
- 내 판단: `resyncNeededMsg` 가 비공개라 재동기화·핑 시험은 package app 내부 시험으로 썼다.
- 내 판단: `Run` 전에 부른 `ctx.Post` 는 배달할 프로그램이 없어 debug 로그를 남기고 버린다.

## Task 12 — exec 중 OS 알림 · 돌아온 뒤 토스트

- 내 판단: 토스트에 프로그램 이름을 쓰려고 `termhost.ExclusiveProgram()` 을 더하고 `exec.go` 가 요청을 기록하게 했다 — Files 목록 밖.
- 내 판단: `termhost.SetExclusiveForTest(on bool, req ...Request) (restore func())` 는 가변 인자로 만들었다(요청 없이도 부를 수 있게). 시험 밖에서 부르면 panic.
- 내 판단: `ExecReturnedMsg` 는 package app 에 있어 도구가 import 할 수 없다. 그래서 토스트는 그 msg 가 아니라 `termhost.Exclusive()` 의 true→false 가장자리를 매 Update 맨 앞에서 보고 띄운다(`ExecReturnedMsg` 는 받아도 아무 일 없음).
- 내 판단: 멘션 수는 alert kind `mention` 으로 센다. 메시지 수는 남이 쓴 `message.created` 만 센다. 둘 다 0 이면 토스트를 띄우지 않는다.
- 내 판단: exec 중 알림은 `notify.Decide`(RoleTUI, TerminalFocusKnown=false)를 그대로 따르고, 데스크톱 설정은 매 Update 에 atomic 으로 찍어 둔 값을 realtime 고루틴이 읽는다. 같은 alert 를 UI 가 나중에 받아도 공유 ledger 가 OS 알림을 하나로 막는다.

## Task 25 — 카드 등록부 · 채팅 훅

- 내 판단: `Registry.Cards` 자리는 Task 18 이 만들 자리지만 아직 합쳐지지 않아 Task 25 에서 더했다(`clone` 복사 포함). tools batch 와 `internal/ui/registry.go` 에서 충돌할 수 있다 — 한 필드 더하기라 합칠 때 둘 다 남기면 된다.
- 내 판단: 카드 픽스처는 기존 골든이 바뀌지 않게 따로 부르는 시드 `fixtures.Cards(s)`(#카드 채널, SpaceNova 9번 자리)에 두었다. `fixtures.Messages(ChCards)` 는 텍스트 1·`arch.design` 카드(준호)·`api.result` 카드(sam) 3개.
- 내 판단: api `TestSendMessageCard` 의 빨간 단계는 `undefined: api.Card` 컴파일 실패였다(따로 실행 실패를 보지 않음). api testdata 샘플(Message·MessageSearchHit·MessageWindow·ThreadWindow)에 `card` 키를 더하고 `Card` 샘플을 넣었다.
- 내 판단: 계획의 "`textwidth.Fit(width)` 로 한 번 더 자름" 은 채팅의 `fit`(자르기만, 채우기 없음)으로 했다 — `textwidth.Fit` 은 줄 수·채움까지 하는 다른 함수다.
- 내 판단: 카드 그리기는 세 밀도 모두 렌더러를 쓴다 — 기본·버블은 `Lines` 전부, line 밀도(`compactBody`)와 minimal(초소형) 밀도는 `Lines` 의 첫 줄. 렌더러가 좁은 그림을 ctx·width 로 정한다는 계획에 맞췄다. 스레드 제목처럼 순수 글자 미리보기(`plainBody`)는 서버 `body` 를 그대로 쓴다.
- 내 판단: 카드 키 훅(`msgPane.cardKey`)은 계획의 "msgpane.go 키 처리 맨 앞" 을 채널·스레드 뷰 `HandleKey` 에서 composer·escDrag 다음, 나머지 채팅 키 앞으로 정했다(`channel.go`·`thread.go` 한 줄씩 — Files 목록 밖). 그래서 카드 `Key` 는 글자 입력 시작(`startsComposing`)보다 먼저 키를 받는다.
- 내 판단: `e` 는 등록 여부와 상관없이 모든 카드 메시지에서 조용히 삼킨다(토스트 없음). 동작 메뉴의 수정 항목은 기존 `editable`(kind text·me 만)이 이미 빼고 있어 따로 고치지 않았다. 지운 카드 메시지는 일반 메시지처럼 다룬다(훅 없음).
- 내 판단: `enter` 는 `Open` 이 있을 때만 `Open`, 없으면(또는 등록 안 된 type) 지금처럼 스레드. `t` 는 늘 스레드.
