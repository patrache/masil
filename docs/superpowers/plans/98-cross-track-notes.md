# 갈래 간 전달 사항 (client-core 완료 시점, 2026-11-05) — chat·sns-fun·daemon-release 갈래 필독

client-core(track/client) 가 실제로 정한 이름·규칙이 계획서 문구와 다른 곳. **계획서와 충돌하면 이 파일이 우선.** (원문 근거: client-core 갈래 각 작업 보고)

1. **`api.MeRef`** — 계약 CA 4.3.1 의 `const Me UserRef = "@me"` 는 같은 패키지의 `type Me struct` 와 이름이 겹쳐 컴파일 불가 → 상수는 `api.MeRef`. plan 04 의 `RemoveChannelMember(api.Me)`(채널 나가기)·plan 05 의 `userRef "@me" -> api.Me` 는 모두 **`api.MeRef`** 로 쓴다.
2. **`markup.InlineCode`** — 인라인 코드 조각 종류 상수는 `markup.InlineCode`(블록 쪽이 `markup.Code`).
3. **`/shrug`** — `slash.Parse` 가 이미 `Command.Text` 끝에 `¯\_(ツ)_/¯` 를 붙인다("/shrug 몰라" → `몰라 ¯\_(ツ)_/¯`, "/shrug" → `¯\_(ツ)_/¯`). plan 04 Task 11 은 본문을 **`cmd.Text` 그대로** 보낸다(다시 붙이지 말 것). 계획 시험 기대값은 이미 이와 맞음.
4. **슬래시 명령 이름** — ASCII 글자로만 된 이름만 명령으로 인식. `/없는명령 그대로` 는 일반 텍스트로 전송(plan 04 `TestSlashSendsKinds` 와 일치).
5. **`apitest` 경로** — `Recorded.Path` 는 **이스케이프된** 경로. plan 05 `TestHashtagGolden` 등 디코드된 경로를 기대하는 시험은 `url.PathUnescape` 로 비교.
6. **CR-D 사용법 오류** — `cmd/masil` 의 `errUsage`·`usageErr(err)` 구현 완료. plan 05 `cmd_admin.go` 는 `errors.Is(err, admincli.ErrUsage)` 면 `usageErr(err)` 로 감싸고, plan 06 `cmd_daemon.go` 는 사용법 오류를 `usageErr(...)` 로 돌려준다(종료 코드 2).
7. **알림 라우팅 표** — core `alertTargetRoute`: `friend_request` → `RouteFriends`, `focus_summary`·그 밖 기본값 → `RouteNotifications`. plan 05 의 `alertRoute` 도 **같은 표**를 쓴다(따로 만들지 말고 core 것을 재사용할 수 있으면 재사용).
8. **`notify.Handle` 오류** — 원장(`MarkIfNew`) 실패는 반환하지 않고 WARN 로그(제목·본문 미기록)만 남긴 뒤 알림은 표시. 반환되는 오류는 백엔드 `Show` 실패뿐 → plan 06 데몬의 `alert display failed` 판정과 일치.
9. **realtime 로그** — 클라이언트 자체 로그는 전부 Debug + `"realtime: "` 접두. plan 06 데몬 시험이 찾는 고정 INFO 줄 `realtime ready` 와 혼동 없음.
10. **Composer 자동완성** — `Composer` 에 `Completing()` 없음. chat 은 팝업 상태를 자기 쪽에서 관리하고, 선택 항목 라벨에 `"› "` 접두를 붙여 `SetCompletion` 을 다시 부른다(core 는 `"› "` 로 시작하는 줄을 강조 시작점으로 씀).
11. **palette `Item`** — `Keywords` 필드 없음. 각 소스가 스스로 점수 매겨 `Score` 를 채운다(plan 05 해시태그 소스 점수 0 은 그대로 사용).
12. **`BootstrapMsg`** — 비공개 `client *api.Client` 필드가 있어 `Bootstrap(c)` 가 채움. 시험에서 손으로 만든 `BootstrapMsg` 는 Resyncer 에 nil client 가 간다는 점 유의.
13. **plan 03 Task 36(`ui/e2e` 전체 골든 스윕)** 은 chat·sns-fun 병합 뒤 통합 단계에서 실행 — 각 갈래에서는 하지 않음.
14. **알려진 후속 과제(chat 갈래 권장)** — 로그아웃 상태에서 `masil join <code>` → 로그인 화면 뒤 초대 코드가 사라짐. 로그인 후 이어서 참여하도록 보존.
