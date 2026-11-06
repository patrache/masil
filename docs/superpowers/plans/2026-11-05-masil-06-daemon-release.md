# masil 알림 데몬 + 릴리스 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** masil 이 꺼져 있어도 멘션·DM·초대 OS 알림을 받게 하는 `masil daemon`(run/install/uninstall/status)과 OS 자동 시작 등록을 만들고, goreleaser·설치 스크립트·Homebrew tap·GitHub Actions·README 로 바이너리 배포 경로를 완성한다. 아울러 설계서 7장의 마일스톤 순서(M1→M2→M3→M4, "앞 단계가 돌아가야 다음으로")를 지키도록 **로컬 `wrangler dev` 관문 G1~G4(+최종 G5)** 를 두고 각 관문이 요구하는 모든 계획의 Task 범위를 정하며(아래 "마일스톤 관문"), G4 에서 두 클라이언트 + 데몬 엔드투엔드와 유휴 데몬의 CPU·메모리 상한(설계서 3.3)을 확인한다.

**Architecture:** 데몬은 core 의 `realtime`(ModeNotify)·`notify`·`instance`·`auth`·`config` 위에 얇게 얹는다. 데몬 고유 로직은 세 덩어리 — (1) `daemon-state.json` 재개 지점, (2) TUI 존재 판정(`tui.lock` 우선, 실패 시 서버 신호 + 3초 유예), (3) alert 처리 큐(무제한·순서 유지, `notify.Notifier.Handle` 위임) — 와 자격 증명 대기 루프(설정 실시간 반영, 데몬이 본 서버 주소를 `daemon-status.json` 에 기록, 실행 파일이 바뀌면 종료 코드 1 로 나가 감독자가 새 버전으로 재시작)다. 자동 시작은 OS별 등록 파일을 **순수 생성 함수**(골든 시험)와 **명령 실행부**(가짜 runner 로 기록 시험)로 나눈다. 릴리스는 태그 푸시에서만 도는 워크플로가 시험 → goreleaser → tap 갱신 → 설치 스모크 순으로 진행한다.

**Tech Stack:** Go 1.26.1 (`charm.land/*/v2` 는 이 범위에서 쓰지 않음), `github.com/coder/websocket` v1.8.15(시험의 `CloseError` 만), `golang.org/x/sys` v0.49.0(`windows/registry`, `windows`), `github.com/charmbracelet/x/exp/golden` v0.1.0, goreleaser v2.19.0, `goreleaser/goreleaser-action@v7.3.0`, `actions/checkout@v7.0.1`, `actions/setup-go@v7.0.0`, `actions/setup-node@v7.1.0`, actionlint v1.7.12, Node 24(wrangler 4.157.0 는 `node >=22.0.0`). **워크플로우의 `uses:` 에는 위 태그를 그대로 쓰지 않는다** — 보류 항목 2(공급망)에 따라 각 태그를 `gh api repos/<owner>/<repo>/git/ref/tags/<tag>` 로 조회한 40자 커밋 SHA 로 고정하고 `# vX.Y.Z` 주석을 붙인다(예: `actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1`). `cmd/masil/workflow_pins_test.go` 가 이를 강제한다.

**Spec:** `docs/superpowers/specs/2026-11-05-masil-design.md` (2장 전역 운영자, 3.3 데스크톱 알림·데몬·"CPU·메모리 사용 최소", 5.1 배포, 5.3 로컬 E2E, 6장 운영 윤허, 7장 마일스톤 순서). 계약: `docs/contract/client-architecture.md` 2.1·3장·4.4·4.6·9.2·10.3, `docs/contract/events.md` 3.2~3.5·4.7·4.9·7장, `docs/contract/server-architecture.md` 7·8장, `docs/superpowers/plans/00-brief.md`.

## Global Constraints

- 모듈 `github.com/patrache/masil`, Go 1.26. `go.mod`·`go.sum` 은 core 만 고친다 — 이 계획은 `go get`/`go mod tidy` 를 하지 않으며 계약 1.2 목록 밖 모듈을 import 하지 않는다.
- 이 계획이 만들고 고치는 파일(계약 2.1 의 daemon-release 소유): `internal/daemon/`, `internal/autostart/`, `cmd/masil/cmd_daemon.go`, `.goreleaser.yaml`, `scripts/install.sh`, `scripts/install.ps1`, `packaging/homebrew/masil.rb.tmpl`, `.github/workflows/release.yml`. 범위 지시서가 추가로 배정한 미배정 파일: `README.md`, `.github/workflows/server-ci.yml`, `.gitignore` 의 `dist/` 한 줄. 관문 시험은 이 계획 소유 경로 안에만 둔다: `internal/daemon/e2egate/`(G1~G3, 데몬 코드에 의존하지 않는 별도 패키지)와 `internal/daemon/e2e_local_test.go`(G4). **core 소유 `.github/workflows/ci.yml`(go vet + go test 3 OS)·`Makefile`·`main.go` 는 고치지 않는다.**
- **운영자 CLI `masil admin`(`internal/admincli/**`, `cmd/masil/cmd_admin.go`·`cmd_admin_test.go`)은 계약 2.1·2.4·9.2 상 daemon-release 소유지만, 범위 지시서 재배정으로 sns-fun 계획(`2026-11-05-masil-05-client-sns-fun.md` Global Constraints·Task 20)이 만든다.** 설계서 2장 "전역 운영자 역할을 첫 출시부터" 를 채우는 유일한 담당이므로 비워 둘 수 없다. 이 계획은 그 파일들을 만들거나 고치지 않는다(이중 소유 방지). **실행 전 확인(차단 조건)**: 05 계획에 `masil admin` 하위 명령 8종(`reports|report <id>|resolve <id> <resolution>|remove <type> <id>|suspend @h [--for 7d]|unsuspend @h|users|actions`, `api.Admin*`, `textwidth` 정렬 표, CLI 출력 골든)을 만드는 Task 20 이 실제로 있는지 `grep -n 'admincli' docs/superpowers/plans/2026-11-05-masil-05-client-sns-fun.md` 로 확인한다. 없거나 05 가 이 재배정을 되돌렸다면 **이 계획을 시작하지 말고 오케스트레이터/오너에게 담당을 명시적으로 정해 달라고 여쭙는다**(어느 계획에도 없는 채로 출시하지 않는다). 같은 `main` 패키지에 05 의 `cmd_admin_test.go`·core 의 `main_test.go` 가 함께 있으므로, 이 계획의 `cmd/masil` 시험 헬퍼 이름은 모두 `daemon` 접두사(`daemonTestEnv`, `stubDaemonFns`)를 붙여 충돌을 피한다.
- 선행 조건: core 의 `config`·`auth`·`instance`·`notify`·`realtime`·`api`·`version`·`textwidth`·`humanize`·`cmd/masil/main.go` 가 **스텁이 아니라 구현 완료** 상태여야 Task 1~5·8 시험이 통과한다(Task 6·7 은 core 의존이 `config.Paths`·`config.WriteFileAtomic` 뿐이라 그것만 구현돼 있으면 된다). 의존 core 패키지가 아직 스텁이면 해당 Task 를 미루고 순서를 바꾸지 않는다. **실행 순서는 번호가 아니라 "마일스톤 관문" 절이 정한다**: Task 14(G1) → Task 15(G2) → Task 16(G3) → Task 1~8(데몬 = 설계서 7장 M4 "데스크톱 알림") → Task 17(G4) → Task 9~13(릴리스) → Task 18(G5). 예전의 "맨 끝 Task 14 한 번" 연동 확인은 이 다섯 관문으로 나뉘었다. **교차 계획 선행 조건(G4)**: Task 17 의 `EndFocus` 뒤 `waitShown(4, "focus_summary")` 는 서버가 **조기 종료(`DELETE /v1/me/focus`)에도** `alert{kind:"focus_summary"}` 를 보낸다고 가정한다(events.md 4.7 "집중 모드 종료 시 1회·항상", CA 9.1 "요약 오버레이의 유일한 트리거"). server-core(01) Task 12 의 Produces 는 "알람 종료일 때만 alert" 라서 01 만 끝난 서버에서는 이 시험이 10초 시간 초과로 **실패하는 것이 정상**이다 — 02 Task 13 의 "고정 기준 맞추기"(종료 사유와 상관없이 alert, 01 Task 12 문구를 대체)가 끝나 `cd server && npx vitest run test/api/status-focus.test.ts -t "early end summary"` 가 PASS 해야 Task 17 을 시작한다(Task 17 선행 확인). **교차 계획 선행 조건(Task 8)**: core(03) Task 35 가 `cmd/masil/main.go` 에 사용법 오류 표지(CA 2.4 종료 코드 2)를 정해 두어야 한다 — Task 8 앞머리의 차단 확인.
- 설정 디렉터리 = `MASIL_CONFIG_DIR` 또는 `os.UserHomeDir()+"/.config/masil"`(모든 OS), 디렉터리 0700·파일 0600, 모든 쓰기는 `config.WriteFileAtomic`. `XDG_CONFIG_HOME` 은 보지 않는다.
- 비밀: 토큰을 코드·골든·로그에 남기지 않는다. 로그에는 `(*auth.Credentials).Redacted()`(`msl_…(redacted)`)만. 시험 토큰 `msl_test…` 가 로그·골든에 나오면 실패로 본다.
- 데몬 규칙(events.md 7장): notify 모드 접속, `alert.suppressed` 는 띄우지 않음, 같은 기기 TUI 가 있으면 띄우지 않음, `tui_connected:false` 뒤 3초 유예, 공유 기록 `alerts-shown.json` 으로 기기 안 중복 방지, 재전송 alert 가 10분보다 오래되면 띄우지 않음, 상태를 쌓지 않음. 판정 자체는 `notify.Decide`(core) — 데몬은 `notify.Input` 만 정확히 채운다.
- 자동 시작 이름(계약 9.2 고정): macOS `~/Library/LaunchAgents/com.patrache.masil.daemon.plist`(`launchctl bootout` 후 `bootstrap gui/<uid>`), Linux `~/.config/systemd/user/masil-daemon.service`(`systemctl --user enable` + `systemctl --user restart` — 계약의 `enable --now` 는 이미 떠 있는 유닛을 다시 시작하지 않아 재설치·업그레이드 때 새 `ExecStart` 가 반영되지 않으므로 `restart` 로 대신한다; `restart` 는 멈춘 유닛도 시작한다), Windows `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 값 이름 `masil-daemon` = `"<exe>" daemon run`.
- 크로스 빌드: `CGO_ENABLED=0`, darwin/linux/windows × amd64/arm64. 릴리스 바이너리의 `config.DefaultServer` 는 `-ldflags -X` 로 **https 주소**를 넣는다(localhost 금지).
- 되돌릴 수 없는 작업은 오너 윤허 후에만, **실행자는 하지 않는다**: 태그 푸시(=릴리스 실행), `patrache/homebrew-tap` 레포 생성, 저장소 비밀/변수 등록(`HOMEBREW_TAP_TOKEN`, `MASIL_DEFAULT_SERVER`, `MASIL_PUBLIC`), 레포 공개 전환, `wrangler deploy`·원격 D1/R2 생성. 계획 안에서는 "오너 윤허 후 오너가 실행" 으로만 적는다.
- 커밋 단계는 실행 환경 지시가 커밋을 허락할 때만 수행한다. `git push` 는 하지 않는다. 커밋 메시지는 영어.
- 문서·CLI 출력·README 는 한국어, 식별자·로그 메시지 키·주석은 영어.
- 이 범위에는 **TUI 화면이 없다**(데몬은 화면이 없고, 설정 화면의 알림 토글은 core 소유). 따라서 화면 골든(uitest `Required` 4크기)은 해당 없음 — 대신 CLI 출력·생성 파일 내용을 `golden.RequireEqual`(경로 `testdata/<tb.Name()>.golden`, 갱신 `-update`)로 고정한다. 관문 Task 14~17 의 수동 확인에서 TUI 를 실제 크기(200×60·120×40·120×30·80×24·50×20)로 띄워 본다.
- 결정적 시험 환경: 시계 `2026-11-05T02:05:00Z`, 표시 지역 `Asia/Seoul`(`time/tzdata` import).
- **업그레이드 뒤 새 바이너리로 갈아타기(한 가지 방식으로 고정)**: macOS·Linux 데몬은 자기를 띄운 실행 파일 경로를 `PollCredentials` 마다 살펴, 그 경로가 가리키는 파일이 바뀌면(`brew upgrade` 의 심볼릭 링크 교체, `install.sh` 의 덮어쓰기) `daemon.ErrRestartRequired` 로 끝나고 `masil daemon run` 은 **종료 코드 1** 로 나간다 → launchd `KeepAlive{SuccessfulExit:false}`·systemd `Restart=on-failure` 가 새 바이너리로 다시 띄운다(Task 4·8). 서버가 최소 버전을 올려 `4010`(`ErrUpgradeRequired`)을 받은 데몬도 같은 감시로 업그레이드를 기다렸다가 갈아탄다. 재설치(`masil daemon install`)는 macOS `bootout`+`bootstrap`, Linux `enable`+`restart` 로 항상 새 등록을 적용한다(Task 7). Windows 는 감독자가 없어 감시를 끄고, 다음 로그인 때 새 버전이 뜬다(Task 10 `install.ps1` 안내).
- **로그 수준(고정)**: `credentials server mismatch`·`session rejected; waiting for new credentials`·`alert display failed`·`config reload failed` = **WARN**, 예상 밖 `realtime.Run` 오류(`unexpected realtime error`) = **ERROR**, `not logged in; waiting for credentials`·`realtime ready`·`alert shown`·`alert skipped`·`desktop setting changed`·`executable replaced; exiting for restart` = **INFO**. `status` 의 `최근 경고` 는 WARN·ERROR 줄만 모은다(Task 5).

## Review Focus

1. **로컬에서 `MASIL_SERVER=…` 로 로그인했는데 자동 시작된 데몬에는 그 환경변수가 없음** → `credentials.server ≠ config.ServerURL()` 이라 조용히 알림이 안 온다. 기대: 데몬이 "서버 불일치" 경고(WARN)를 로그에 한 번 남기고 소켓을 열지 않으며, 자기가 실제로 본 주소를 `daemon-status.json` 에 기록한다. **같은 `MASIL_SERVER` 가 걸린 셸에서** `masil daemon status` 를 쳐도 셸 환경이 아니라 데몬 쪽 주소(실행 중이면 `daemon-status.json`, 아니면 `config.toml` 의 `server` > 내장 기본값 — `MASIL_SERVER` 무시)로 비교해 불일치를 보여 주고 고치는 방법을 안내한다. `masil daemon install` 도 셸의 `MASIL_SERVER` 가 데몬 쪽 주소와 다르면 경고한다. → Task 4 `TestRunServerMismatchStaysOffline`, Task 5 `TestInspect/shell_env_not_seen_by_daemon`, Task 8 `TestDaemonStatus/mismatch`·`TestDaemonStatus/shell_env`·`TestDaemonInstall`.
2. **릴리스 바이너리가 `http://localhost:8787` 기본 서버로 나감**(저장소 변수 누락) → 설치한 모든 사람이 접속 실패. 기대: release 워크플로가 goreleaser 전에 `https://` 가 아니면 실패하고, 빌드 정보에 `DefaultServer=https://…` 가 박혀 있다. → Task 9 Step 4(`go version -m` 확인), Task 12 가드 단계 시험.
3. **Homebrew `Cellar/masil/<버전>/bin/masil` 이나 `go run` 임시 경로가 자동 시작에 등록됨** → `brew upgrade` 뒤 옛 경로가 사라져 데몬이 영영 안 뜬다. 기대: `<prefix>/bin/masil` 심볼릭 링크로 바꿔 등록, `go-build` 임시 경로는 거부, 등록된 실행 파일이 없으면 `status` 가 경고. 업그레이드 뒤 옛 데몬이 `daemon.lock` 을 쥔 채 남지 않고 새 바이너리로 갈아탄다(4010 을 받은 데몬도). → Task 6 `TestStableExecutable`, Task 7 `TestQueryDetectsMissingExe`·`TestInstallLinux`, Task 4 `TestRunRestartsWhenExecutableReplaced`, Task 8 `TestDaemonRunRestartExitsNonZero`.
4. **노트북 잠자기·네트워크 끊김 뒤 재개**에서 오래된 alert 가 쏟아지거나 같은 알림이 두 번 뜸(TUI 와 데몬, 데몬 재시작 전후). 기대: 재전송 alert 는 10분 이하만 1번, 10분 초과는 무시, 같은 `alert.id` 는 기기 안에서 한 번. 재개 때 수백 개가 한꺼번에 와도 `OnEvent` 가 막히지 않고 하나도 버리지 않으며 `mark` 는 seq 순서대로. → Task 3 `TestHandlerReplayAgeAndLedger`·`TestHandlerBurstNoLossInOrder`, Task 4 `TestRunResumePoint`.
5. **공백·`&`·`%`·`"` 가 든 실행 파일 경로**(`/Users/me/My Apps/masil`, `C:\Users\Kim Min\…\masil.exe`) → plist XML 깨짐, systemd `ExecStart` 가 공백에서 잘림, Run 값 따옴표 누락. 기대: 각 형식 규칙대로 이스케이프·인용. → Task 6 골든 `spaces_and_specials`.
6. **유휴 데몬이 조용히 자원을 먹음**(설계서 3.3 "CPU·메모리 사용 최소") — 하루 종일 떠 있는 프로세스라 작은 누수·바쁜 루프도 문제다. 기대: 10분 유휴에 누적 CPU ≤ 2초·RSS ≤ 40 MiB, 비알림 이벤트(`notifications.read`) 300개를 받아도 RSS 증가 ≤ 8 MiB 이고 그 뒤 더 늘지 않으며, 로그 줄·설정 디렉터리 파일이 쌓이지 않는다. → Task 17 Step 5(측정), Task 3 `TestHandlerBurstNoLossInOrder`(큐가 비워짐).
7. **마일스톤을 건너뛴 연동** — 층별 병렬 실행이라 M1(로그인→ready)이 실서버에서 안 되는데 M2·M3 화면을 쌓는 일. 기대: 각 마일스톤 끝에 실제 `wrangler dev` 관문을 통과해야 다음 마일스톤 Task 를 배정한다. → "마일스톤 관문" 절, Task 14~18.

---

## 마일스톤 관문 (설계서 7장 — 계획 사이 실행 순서의 단일 출처)

설계서 7장은 "전부 이번에 만들되, 앞 단계가 돌아가야 다음으로"(M1 기반 → M2 채팅 → M3 SNS → M4 재미·운영)를 요구한다. 여섯 계획은 층(서버 1부·2부, 클라이언트 core·chat·sns-fun, daemon-release) 단위로 짜여 병렬로 돌고, 02(서버 2부)·05(sns-fun)는 M3 와 M4 를 한 계획에 담았다. 그래서 이 계획이 마일스톤마다 로컬 `wrangler dev` 관문을 갖고, 관문마다 **먼저 끝나 있어야 하는 Task** 와 **통과한 뒤에야 배정할 Task** 를 아래 표로 고정한다(같은 마일스톤 안에서는 계획끼리 병렬 — 설계서 7장의 병렬 개발과 순서를 함께 지키는 방식).

| 관문 | 마일스톤 (설계서 7장) | 먼저 끝나 있어야 하는 Task | 범위 안에 끼어 있는 다음 단계 기능 (계획 순서대로 만들되 실서버 확인은 G4) | 관문 Task | 통과한 뒤에야 배정 |
|---|---|---|---|---|---|
| G1 | M1 기반: 계정·인증·세션, 실시간 소켓, TUI 뼈대(4모드·테마 4종·명령 팔레트) | 서버 01 Task 1~27, 02 Task 2 · 클라이언트 03 Task 1~35 | 01 Task 12(집중 모드)·16(상태 메시지)·18(차단)·20(주사위·투표 첨부), 03 Task 15·31(OS 알림·집중 요약 트리거) | Task 14 | 01 Task 28, 04 Task 1~23 |
| G2 | M2 채팅: 친구·DM·그룹 DM, 방·채널·초대, 스레드·리액션·멘션·수정/삭제·고정·안 읽음·입력 중·접속 상태·검색 | G1 + 01 Task 28 · 04 Task 1~23 | 04 Task 11(`/poll`·`/status`·`/focus`·`/roll`·`/me`·`/shrug`), 04 Task 12(신고·차단·투표) | Task 15 | 01 Task 29, 02 Task 1·3~10, 05 Task 1~15·18·19 |
| G3 | M3 SNS: 피드·답글·좋아요·리포스트·인용, 팔로우·프로필, 사진 글·스토리, 해시태그·트렌드, 알림함 | G2 + 01 Task 29, 02 Task 1~10 · 05 Task 1~15·18·19 | 05 Task 10(피드 투표)·12(방명록), 05 Task 7·11 의 신고·차단 | Task 16 | 02 Task 11~18, 05 Task 16·17·20, 06 Task 1~8 |
| G4 | M4 재미·운영: 투표·방명록·상태 메시지·집중 모드·슬래시 명령·데스크톱 알림, 차단·신고·운영자 | G3 + 02 Task 11~18 · 05 Task 16·17·20 · 06 Task 1~8 | — | Task 17 | 06 Task 9~13, 03 Task 36 |
| G5 | 최종(출시 준비 — 태그·배포는 오너 윤허) | G4 + 06 Task 9~13 · 03 Task 36 | — | Task 18 | (오너 윤허 사항만 남음) |

규칙:
1. **G1 의 서버 범위가 01 Task 27 까지인 이유**: TUI 는 `ready` 직후 부트스트랩(events.md 5.3 1단계: `/v1/me`·`/spaces`·`/dms`·`/me/unreads`·`/notifications/unread-count`·`/friends`·`/friends/requests`·`/me/direct-invites`·`/blocks`)을 부르고, 하나라도 404 면 core Task 30 규칙대로 상태줄이 `✕ 오프라인` 에 머문다 — 그러면 "로그인→ready" 를 확인할 수 없다. 이 경로들이 01 Task 16·17·18·19·21·22·27 과 02 Task 2 에 흩어져 있고 01 은 자기 순서대로 가므로 01 Task 1~27 전체를 요구한다. 02 Task 2(알림함)는 02 Task 1(업로드)에 기대지 않으므로(Consumes 에 업로드 없음) 02 의 다른 Task 보다 먼저 한다.
2. 각 계획 **안의** 순서는 원칙적으로 바꾸지 않는다 — 예외는 앞 Task 에 기대지 않음을 확인한 세 개(02 Task 2, 05 Task 18·19)뿐이다(뒤 Task 가 앞 Task 산출물에 기댄다 — 예: 05 Task 7 의 `poll -> Task 10`, 05 Task 11 의 `방명록` 하위 탭). 그래서 "끼어 있는 다음 단계 기능" 열의 Task 는 범위 안에서 순서대로 만들고, 그 기능은 그 관문의 합격 조건에 넣지 않으며 G4 에서 실서버로 확인한다. 05 Task 18(전체 화면 이미지)·19(팔레트 명령·`cmd/masil/register_sns.go`·모드 전환 통합 시험)는 16·17 에 기대지 않아(Consumes·시험 목록 확인) G3 범위로 당긴다 — 특히 19 가 `register_sns.go` 를 만들어야 실제 바이너리에서 피드 화면이 열린다(chat 은 04 Task 4 의 `register_chat.go`).
3. 오케스트레이터는 관문 Gk 를 **통과**(그 관문 Task 의 자동 시험 PASS + 수동 확인이 Expected 와 일치)하기 전에는 "통과한 뒤에야 배정" 열의 Task 를 배정하지 않는다. 이미 진행 중인 같은 마일스톤 Task 는 그대로 둔다.
4. 관문이 실패하면 시험을 고쳐 통과시키지 않는다. 실패한 확인 항목과 그것을 소유한 계획·Task 를 오케스트레이터에 보고하고, 소유 계획이 고친 뒤 같은 관문을 처음부터 다시 돈다.
5. 모든 관문은 로컬 전용이다: `wrangler dev`·`--local` 만, `--remote`·`wrangler deploy` 금지. `RL_SIGNUP`(IP 당 60초 5회) 때문에 가입을 하는 관문 시험은 서로 60초 간격을 둔다.

---

## 파일 구조

```
internal/daemon/
  state.go            State load/save, resumePoint                           (Task 1)
  presence.go         TUI presence judgment (lock > server signal + 3s grace)(Task 2)
  handler.go          ordered alert queue -> notify sink, ack marking        (Task 3)
  daemon.go           Options, Run, credential loop, session, state saver    (Task 4)
  runstatus.go        daemon-status.json (server the daemon actually sees)   (Task 4)
  exewatch.go         own-executable change watcher -> ErrRestartRequired    (Task 4)
  logfile.go          NewLogger: logs/daemon.log, 5MB -> .1 rotation         (Task 5)
  inspect.go          Report, Inspect, AutostartServer (`masil daemon status`)(Task 5)
  detach_windows.go   Detach: hidden-console self-respawn                    (Task 8)
  detach_other.go     Detach: no-op                                          (Task 8)
  *_test.go, testdata/
internal/autostart/
  autostart.go        Status, names, ErrUnsupported, runner seams, StableExecutable (Task 6)
  render.go           launchdPlist, systemdUnit, runValue, registeredExe parsers    (Task 6)
  install_darwin.go / install_linux.go / install_windows.go / install_other.go     (Task 7)
  *_test.go, testdata/
cmd/masil/cmd_daemon.go (+ cmd_daemon_test.go, testdata/)                   (Task 8)
.goreleaser.yaml, .gitignore(dist/)                                          (Task 9)
scripts/install.sh, scripts/install.ps1                                      (Task 10)
packaging/homebrew/masil.rb.tmpl                                             (Task 11)
.github/workflows/release.yml, .github/workflows/server-ci.yml               (Task 12)
README.md                                                                    (Task 13)
internal/daemon/e2egate/doc.go             package doc only (no build tag)   (Task 14)
internal/daemon/e2egate/gate_test.go (//go:build e2e) G1~G3 gates + helpers  (Task 14·15·16)
internal/daemon/e2e_local_test.go (//go:build e2e)  G4 daemon e2e + flood    (Task 17)
(internal/admincli/, cmd/masil/cmd_admin.go — sns-fun 계획 Task 20 담당, 이 계획은 건드리지 않음)
```

공용 시험 헬퍼(`internal/daemon/helpers_test.go`, Task 1 에서 만들고 이후 재사용):

```go
var t0 = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)

type fakeClock struct{ mu sync.Mutex; now time.Time }
func (c *fakeClock) Now() time.Time
func (c *fakeClock) Set(t time.Time)

// testPaths points MASIL_CONFIG_DIR at t.TempDir(), resolves and creates it.
func testPaths(t *testing.T) config.Paths
```

---

### Task 1: 데몬 상태 파일과 재개 지점

**Files:**
- Create: `internal/daemon/state.go`
- Test: `internal/daemon/state_test.go`, `internal/daemon/helpers_test.go`

**Interfaces:**
- Consumes: `config.ResolvePaths() (Paths, error)`, `config.EnsureDir(p Paths) error`, `config.WriteFileAtomic(path string, data []byte, perm os.FileMode) error`, `config.Paths.DaemonState`, `realtime.ResumePoint{Epoch *string; LastSeq *int64; UseDeviceAck bool}`
- Produces (계약 9.2): `type State struct{ Version int "json:\"version\""; UserID string "json:\"user_id\""; Epoch string "json:\"epoch\""; LastSeq int64 "json:\"last_seq\""; SavedAt time.Time "json:\"saved_at\"" }`. 패키지 내부: `func loadState(p config.Paths) (*State, error)` — 파일 없음 → `(nil, nil)`, 깨짐 → `(nil, err)`; `func saveState(p config.Paths, s State) error` — `Version=1` 강제, 0600 원자적; `func resumePoint(s *State, userID string) realtime.ResumePoint`.

- [ ] **Step 1: Write the failing test**

```go
func TestStateRoundTrip(t *testing.T) {
	p := testPaths(t)
	if s, err := loadState(p); s != nil || err != nil {
		t.Fatalf("missing file: got %v, %v; want nil, nil", s, err)
	}
	in := State{UserID: "01JA0000000000000000000SAM", Epoch: "01JC2000000000000000000000", LastSeq: 1041, SavedAt: t0}
	if err := saveState(p, in); err != nil { t.Fatal(err) }
	got, err := loadState(p)
	if err != nil { t.Fatal(err) }
	in.Version = 1
	if *got != in { t.Fatalf("got %+v want %+v", *got, in) }
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(p.DaemonState)
		if fi.Mode().Perm() != 0o600 { t.Fatalf("perm %v", fi.Mode().Perm()) }
	}
}

func TestStateCorrupt(t *testing.T) {
	p := testPaths(t)
	os.WriteFile(p.DaemonState, []byte("{not json"), 0o600)
	if s, err := loadState(p); s != nil || err == nil {
		t.Fatalf("corrupt: got %v, %v; want nil, error", s, err)
	}
}

func TestResumePoint(t *testing.T) {
	st := &State{Version: 1, UserID: "U1", Epoch: "E1", LastSeq: 41}
	rp := resumePoint(st, "U1")
	if rp.UseDeviceAck || rp.Epoch == nil || *rp.Epoch != "E1" || rp.LastSeq == nil || *rp.LastSeq != 41 {
		t.Fatalf("same user: %+v", rp)
	}
	for name, s := range map[string]*State{
		"nil":        nil,
		"other_user": {Version: 1, UserID: "U2", Epoch: "E1", LastSeq: 41},
		"no_epoch":   {Version: 1, UserID: "U1"},
	} {
		if rp := resumePoint(s, "U1"); !rp.UseDeviceAck || rp.Epoch != nil || rp.LastSeq != nil {
			t.Errorf("%s: want use_device_ack only, got %+v", name, rp)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run 'TestState|TestResumePoint' -v`
Expected: FAIL — `undefined: loadState` (컴파일 오류)

- [ ] **Step 3: Implement `State`, `loadState`, `saveState`, `resumePoint` in `internal/daemon/state.go`**

`encoding/json` + `config.WriteFileAtomic(p.DaemonState, data, 0o600)`. `resumePoint` 는 `s != nil && s.UserID == userID && s.Epoch != ""` 일 때만 epoch/seq, 아니면 `{UseDeviceAck: true}`(계약 3.6).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -run 'TestState|TestResumePoint' -v`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/state.go internal/daemon/state_test.go internal/daemon/helpers_test.go
git commit -m "feat(daemon): persist daemon-state.json and derive resume point"
```

---

### Task 2: TUI 존재 판정 (로컬 락 우선 + 서버 신호 + 3초 유예)

**Files:**
- Create: `internal/daemon/presence.go`
- Test: `internal/daemon/presence_test.go`

**Interfaces:**
- Consumes: `instance.TUIRunning(p config.Paths) (running bool, err error)`, `instance.AcquireTUI(p config.Paths) (release func(), err error)`(시험), `realtime.State.TUIOnDevice *bool`
- Produces (패키지 내부): `const tuiGrace = 3 * time.Second`; `func newPresence(probe func() (bool, error), now func() time.Time) *presence`; `func (p *presence) observe(tuiOnDevice *bool)` — realtime `OnState` 에서 호출, **true→false 전이**일 때만 유예 시작 시각 기록(hello 의 첫 false 는 유예 없음); `func (p *presence) decide() (present bool, wait time.Duration)` — `wait > 0` 이면 그만큼 뒤 다시 판정. 내부 mutex(다른 고루틴에서 호출).

판정 규칙(계약 9.2, events.md 7장):
1. `probe()` 가 오류 없이 답하면 그 값이 최종(`wait=0`).
2. 오류면 서버 신호: 모름 → 없음(놓치느니 중복), true → 있음, false 이고 전이 후 `< tuiGrace` → `(false, tuiGrace-경과)`, 그 외 → 없음.

- [ ] **Step 1: Write the failing test**

```go
func TestPresenceDecide(t *testing.T) {
	errLock := errors.New("probe failed")
	tru, fal := true, false
	cases := []struct {
		name     string
		local    bool
		localErr error
		server   *bool         // final server signal
		flipAgo  time.Duration // >0: server went true->false this long before t0
		want     bool
		wantWait time.Duration
	}{
		{"lock_held_server_unknown", true, nil, nil, 0, true, 0},
		{"lock_held_server_false", true, nil, &fal, 0, true, 0},
		{"lock_free_beats_stale_server_true", false, nil, &tru, 0, false, 0},
		{"lock_free_no_grace_needed", false, nil, &fal, time.Second, false, 0},
		{"lock_error_server_unknown", false, errLock, nil, 0, false, 0},
		{"lock_error_server_true", false, errLock, &tru, 0, true, 0},
		{"lock_error_in_grace", false, errLock, &fal, time.Second, false, 2 * time.Second},
		{"lock_error_after_grace", false, errLock, &fal, 3 * time.Second, false, 0},
		{"lock_error_hello_false_no_grace", false, errLock, &fal, 0, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clk := &fakeClock{now: t0}
			p := newPresence(func() (bool, error) { return c.local, c.localErr }, clk.Now)
			if c.flipAgo > 0 {
				clk.Set(t0.Add(-c.flipAgo - time.Second)); p.observe(&tru)
				clk.Set(t0.Add(-c.flipAgo)); p.observe(&fal)
			} else if c.server != nil {
				p.observe(c.server)
			}
			clk.Set(t0)
			got, wait := p.decide()
			if got != c.want || wait != c.wantWait {
				t.Fatalf("decide() = %v, %v; want %v, %v", got, wait, c.want, c.wantWait)
			}
		})
	}
}

func TestPresenceRealTUILock(t *testing.T) {
	p := testPaths(t)
	pr := newPresence(func() (bool, error) { return instance.TUIRunning(p) }, time.Now)
	if got, _ := pr.decide(); got {
		t.Fatal("no TUI yet, decide() = true")
	}
	release, err := instance.AcquireTUI(p)
	if err != nil { t.Fatal(err) }
	if got, _ := pr.decide(); !got {
		t.Fatal("TUI lock held, decide() = false")
	}
	release()
	if got, _ := pr.decide(); got {
		t.Fatal("TUI lock released, decide() = true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestPresence -v`
Expected: FAIL — `undefined: newPresence`

- [ ] **Step 3: Implement `presence` in `internal/daemon/presence.go`**

필드: `probe`, `now`, `mu sync.Mutex`, `server *bool`, `flippedAt time.Time`. `observe` 는 이전 값이 true 이고 새 값이 false 일 때만 `flippedAt = now()`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -run TestPresence -v -race`
Expected: PASS (`TestPresenceDecide` 9 subtests, `TestPresenceRealTUILock`)

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/presence.go internal/daemon/presence_test.go
git commit -m "feat(daemon): judge local TUI presence via tui.lock with server-signal fallback"
```

---

### Task 3: alert 처리 큐 (순서 유지·유예 재판정·ack)

**Files:**
- Create: `internal/daemon/handler.go`
- Test: `internal/daemon/handler_test.go`

**Interfaces:**
- Consumes: `realtime.Event{Type string; Seq *int64; TS time.Time; Data json.RawMessage; Replayed bool}`, `api.Alert`, `notify.Input`, `notify.Decision`, `notify.RoleDaemon`, `notify.New(b Backend, l *Ledger, log *slog.Logger) *Notifier`, `notify.OpenLedger(p config.Paths) *Ledger`, `(*notify.Ledger).MarkIfNew(alertID string) (bool, error)`, `notify.Recorder`, Task 2 `presence`
- Produces (패키지 내부):
  - `type alertSink interface{ Handle(a api.Alert, in notify.Input) (notify.Decision, error) }` — `*notify.Notifier` 가 만족.
  - `func newHandler(sink alertSink, pr *presence, desktop func() bool, now func() time.Time, after func(time.Duration) <-chan time.Time, mark func(seq int64), log *slog.Logger) *handler`
  - `func (h *handler) onEvent(ev realtime.Event)` — realtime `OnEvent` 에서 호출, **절대 막지 않고 절대 버리지 않음**: 큐는 **용량 제한 없는 슬라이스**(`mu sync.Mutex` + `queue []realtime.Event`)이고, `onEvent` 는 잠금 아래 append 한 뒤 신호 채널 `wake chan struct{}`(용량 1)에 `select { case h.wake <- struct{}{}: default: }` 로 깨우기만 한다. 채널이 꽉 차 버리는 경우·보내다 막히는 경우가 둘 다 없다. (상한을 두지 않는 근거: 재개 재전송은 서버 재전송 창으로 이미 유한하고, notify 모드는 alert·`notification.*`·`focus.*` 만 받는다 — events.md 7장.)
  - `func (h *handler) run(ctx context.Context)` — 작업 고루틴. `wake` 또는 재판정 채널을 기다렸다가 잠금 아래 `queue` 를 통째로 떼어 내(`batch, h.queue = h.queue, nil`) 잠금 밖에서 들어온 순서대로 처리. `ctx.Done()` 에 반환.
  - `desktop func() bool` 은 호출마다 현재 값을 읽는다 — Task 4 가 `atomic.Bool` 의 `Load` 를 넘겨, 세션 도중 `config.toml` 의 `[notify] desktop` 변경이 다음 alert 부터 반영된다.

처리 규칙:
- `alert`: `ev.Data` 를 `struct{ Alert api.Alert "json:\"alert\"" }` 로 직접 풂(`Decode` 의 값/포인터 형태에 의존하지 않음). `pr.decide()` 가 `wait>0` 이면 `after(wait)` 뒤 같은 alert 를 **한 번만** 다시 판정(재판정 때 또 유예면 없음으로 처리). `sink.Handle(a, notify.Input{Role: notify.RoleDaemon, Now: now(), Replayed: ev.Replayed, DesktopEnabled: desktop(), TUIPresent: present})`. 결과는 INFO 로그 `msg="alert shown"`(Popup) 또는 `msg="alert skipped"`, 속성 `alert_id`, `kind`, `decision` — **본문·제목은 로그에 남기지 않음**. `Handle` 이 오류(OS 알림 실패)면 WARN `msg="alert display failed"`(속성 `alert_id`, `kind`, `err`) 후 다음으로.
- `ready`: INFO `msg="realtime ready"`(속성 `resumed`, `resync_required`, `replayed`)로 로그.
- 그 밖의 타입: 파싱하지 않고 버림.
- `ev.Seq != nil` 인 모든 이벤트는 처리(유예 alert 는 유예를 **걸어 둔 시점**) 뒤 `mark(*ev.Seq)` — 순서 보장.

- [ ] **Step 1: Write the failing tests**

```go
type sinkCall struct{ A api.Alert; In notify.Input }
type fakeSink struct{ calls chan sinkCall }
func (f *fakeSink) Handle(a api.Alert, in notify.Input) (notify.Decision, error) {
	f.calls <- sinkCall{a, in}; return notify.Popup, nil
}

// alertEvent builds a logged "alert" event; created is alert.created_at.
func alertEvent(seq int64, id string, created time.Time, replayed bool, suppressed bool, kind string) realtime.Event

// startHandler wires a handler with a fake sink, presence(probe), desktop flag, a manual
// after() whose channels the test fires via the returned trigger, and a mark recorder.
func startHandler(t *testing.T, probe func() (bool, error), desktop bool) (h *handler, sink *fakeSink, pr *presence, fire func(), marks func() []int64)

func TestHandlerFillsInput(t *testing.T) {
	h, sink, _, _, marks := startHandler(t, func() (bool, error) { return false, nil }, true)
	h.onEvent(alertEvent(7, "A1", t0.Add(-time.Minute), true, false, "dm"))
	c := recv(t, sink.calls)
	want := notify.Input{Role: notify.RoleDaemon, Now: t0, Replayed: true, DesktopEnabled: true, TUIPresent: false}
	if c.A.ID != "A1" || c.In != want { t.Fatalf("got %+v / %+v", c.A.ID, c.In) }
	waitFor(t, func() bool { return slices.Equal(marks(), []int64{7}) })
}

func TestHandlerTUIPresentAndDesktopOff(t *testing.T) {
	h, sink, _, _, _ := startHandler(t, func() (bool, error) { return true, nil }, false)
	h.onEvent(alertEvent(8, "A2", t0, false, false, "mention"))
	if c := recv(t, sink.calls); !c.In.TUIPresent || c.In.DesktopEnabled {
		t.Fatalf("input %+v", c.In)
	}
}

func TestHandlerGraceDefersThenRejudges(t *testing.T) {
	tru, fal := true, false
	for _, back := range []bool{false, true} { // server says TUI came back during grace?
		t.Run(fmt.Sprintf("tui_back=%v", back), func(t *testing.T) {
			h, sink, pr, fire, marks := startHandler(t, func() (bool, error) { return false, errors.New("x") }, true)
			pr.observe(&tru); pr.observe(&fal) // flip at t0 -> in grace
			h.onEvent(alertEvent(9, "A3", t0, false, false, "dm"))
			expectNone(t, sink.calls, 100*time.Millisecond)
			waitFor(t, func() bool { return slices.Equal(marks(), []int64{9}) })
			if back { pr.observe(&tru) }
			fire()
			if c := recv(t, sink.calls); c.In.TUIPresent != back { t.Fatalf("TUIPresent=%v", c.In.TUIPresent) }
		})
	}
}

func TestHandlerOrderAndIgnoredTypes(t *testing.T) {
	h, sink, _, _, marks := startHandler(t, func() (bool, error) { return false, nil }, true)
	h.onEvent(realtime.Event{Type: "notification.created", Seq: ptr(int64(10)), Data: json.RawMessage(`{}`)})
	h.onEvent(realtime.Event{Type: "focus.started", Seq: ptr(int64(11)), Data: json.RawMessage(`{}`)})
	h.onEvent(realtime.Event{Type: "pong"}) // ephemeral, no seq
	h.onEvent(alertEvent(12, "A4", t0, false, false, "invite"))
	recv(t, sink.calls)
	expectNone(t, sink.calls, 50*time.Millisecond)
	waitFor(t, func() bool { return slices.Equal(marks(), []int64{10, 11, 12}) })
}

// Uses the real core notifier + ledger + Recorder backend (events.md 4.7 / 7).
func TestHandlerReplayAgeAndLedger(t *testing.T) {
	p := testPaths(t)
	rec := &lockedRecorder{} // mutex-wrapped notify.Backend
	n := notify.New(rec, notify.OpenLedger(p), slog.New(slog.DiscardHandler))
	h := runHandlerWith(t, n, func() (bool, error) { return false, nil })

	h.onEvent(alertEvent(1, "F1", t0.Add(-time.Minute), false, true, "dm"))          // suppressed (focus)
	h.onEvent(alertEvent(2, "S1", t0.Add(-time.Minute), false, false, "focus_summary"))
	h.onEvent(alertEvent(3, "S1", t0.Add(-time.Minute), true, false, "focus_summary")) // replay, same id
	h.onEvent(alertEvent(4, "R9", t0.Add(-9*time.Minute), true, false, "mention"))     // replay 9 min
	h.onEvent(alertEvent(5, "R11", t0.Add(-11*time.Minute), true, false, "mention"))   // replay 11 min
	if isNew, _ := notify.OpenLedger(p).MarkIfNew("T1"); !isNew { t.Fatal("T1 should be new") } // TUI already showed T1
	h.onEvent(alertEvent(6, "T1", t0, false, false, "dm"))

	waitFor(t, func() bool { return len(rec.ids()) == 2 })
	expectStable(t, rec.ids, 100*time.Millisecond)
	if got := rec.ids(); !slices.Equal(got, []string{"S1", "R9"}) { t.Fatalf("shown %v", got) }
}

// A resume after a long sleep replays ~1000 notify-mode events while the sink is slow
// (osascript/beeep). onEvent must never block, nothing may be dropped, marks stay in seq order.
func TestHandlerBurstNoLossInOrder(t *testing.T) {
	slow := &slowSink{delay: 2 * time.Millisecond} // records alert IDs in call order, sleeps per call
	var mu sync.Mutex
	var marks []int64
	h := newHandler(slow, newPresence(func() (bool, error) { return false, nil }, func() time.Time { return t0 }),
		func() bool { return true }, func() time.Time { return t0 }, time.After,
		func(seq int64) { mu.Lock(); marks = append(marks, seq); mu.Unlock() }, slog.New(slog.DiscardHandler))
	go h.run(t.Context())

	const n = 1000
	var wantAlerts []string
	start := time.Now()
	for seq := int64(1); seq <= n; seq++ {
		switch seq % 3 {
		case 0:
			id := fmt.Sprintf("B%04d", seq)
			wantAlerts = append(wantAlerts, id)
			h.onEvent(alertEvent(seq, id, t0, true, false, "dm"))
		case 1:
			h.onEvent(realtime.Event{Type: "notification.created", Seq: ptr(seq), Data: json.RawMessage(`{}`)})
		default:
			h.onEvent(realtime.Event{Type: "focus.started", Seq: ptr(seq), Data: json.RawMessage(`{}`)})
		}
	}
	if d := time.Since(start); d > 200*time.Millisecond { t.Fatalf("onEvent blocked: %v for %d events", d, n) }

	waitForTimeout(t, 10*time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(marks) == n })
	mu.Lock(); defer mu.Unlock()
	for i, s := range marks {
		if s != int64(i+1) { t.Fatalf("marks[%d] = %d, want %d (lost or out of order)", i, s, i+1) }
	}
	if got := slow.ids(); !slices.Equal(got, wantAlerts) { t.Fatalf("sink saw %d alerts, want %d in order", len(got), len(wantAlerts)) }
}
```

`recv`/`expectNone`/`waitFor`/`waitForTimeout`/`expectStable`/`ptr`/`lockedRecorder`/`slowSink`/`runHandlerWith` 는 `helpers_test.go` 에 둔다(`recv` 기본 2초 제한, `waitFor` = `waitForTimeout(t, 2*time.Second, …)`, `slowSink` 는 `Handle` 마다 `delay` 만큼 자고 받은 `AlertID` 를 mutex 아래 순서대로 쌓아 `ids()` 로 돌려줌, `lockedRecorder` 는 `notify.Backend` 를 mutex 로 감싼 시험 백엔드로 받은 `OSNotification` 전체를 보관하고 `ids() []string`(각 `AlertID`, 순서대로)·`all() []notify.OSNotification` 을 제공. `runHandlerWith(t, sink, probe)` 는 시계 `t0`, `desktop=true` 로 handler 를 띄움).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/daemon/ -run TestHandler -v`
Expected: FAIL — `undefined: newHandler`

- [ ] **Step 3: Implement `handler` in `internal/daemon/handler.go`**

큐 = 위 Interfaces 의 **무제한 슬라이스 + `wake chan struct{}`(용량 1)**(용량 고정 채널 금지 — 꽉 차면 버리거나 막히는데 둘 다 규칙 위반) + 재판정용 `deferredCh chan deferred`; `run` 은 `select { case <-h.wake: 큐를 떼어 순서대로 처리; case d := <-deferredCh: 재판정; case <-ctx.Done(): return }`. 유예 alert 는 `go func(){ select { case <-after(wait): select { case deferredCh <- d: case <-ctx.Done(): }; case <-ctx.Done(): } }()` — 재판정 전달도 ctx 를 존중. `onEvent` 는 잠금 구간에서 append 외의 일을 하지 않는다.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/daemon/ -run TestHandler -v -race`
Expected: PASS (6 tests). `TestHandlerReplayAgeAndLedger` 가 `[S1 R9]` 만 보여 주고, `TestHandlerBurstNoLossInOrder` 가 1000개 `mark` 를 1..1000 순서대로·alert 333개를 순서대로 확인.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/handler.go internal/daemon/handler_test.go internal/daemon/helpers_test.go
git commit -m "feat(daemon): unbounded ordered alert queue with grace re-judgement and ack marking"
```

---

### Task 4: `daemon.Run` — 단일 인스턴스·자격 증명 대기·세션·상태 저장

**Files:**
- Create: `internal/daemon/daemon.go`, `internal/daemon/runstatus.go`, `internal/daemon/exewatch.go`
- Test: `internal/daemon/daemon_test.go`, `internal/daemon/fakehub_test.go`, `internal/daemon/exewatch_test.go`

**Interfaces:**
- Consumes: `instance.AcquireDaemon(p) (release func(), err error)`, `instance.ErrAlreadyRunning`, `instance.TUIRunning`, `auth.Load(p) (*Credentials, error)`, `auth.ErrNoCredentials`, `auth.Save`, `auth.DeviceID(p) (string, error)`, `(*auth.Credentials).Matches(server string) bool`, `(*auth.Credentials).Redacted() string`, `config.Load(p) (*Config, error)`(+`config.ErrPartial` 허용), `(*config.Config).ServerURL() string`, `config.EnsureDir`, `api.New(baseURL string, opts ...api.Option) *api.Client`, `api.WithToken`, `realtime.New(opts realtime.Options) *Client`, `(*realtime.Client).Run(ctx) error`, `Position() ResumePoint`, `MarkProcessed(seq int64)`, `realtime.ModeNotify`, `realtime.ErrUnauthorized/ErrSuspended/ErrUpgradeRequired`, `realtime.Conn`, `notify.NewOSBackend(appName string) Backend`, Task 1~3.
- Produces (계약 9.2 그대로):
  ```go
  type Options struct {
  	Paths           config.Paths
  	Config          *config.Config
  	Backend         notify.Backend // nil => notify.NewOSBackend("masil")
  	Logger          *slog.Logger   // nil => discard
  	Now             func() time.Time
  	PollCredentials time.Duration  // default 30s
  }
  func Run(ctx context.Context, o Options) error
  ```
  계약 외 공개(같은 계획의 `cmd/masil/cmd_daemon.go` 만 사용): `var ErrRestartRequired = errors.New("daemon: executable replaced; restart required")` — 자기 실행 파일이 바뀌어 감독자(launchd/systemd)가 새 바이너리로 다시 띄워야 할 때 `Run` 이 돌려준다.
  패키지 내부: `type deps struct{ dial func(ctx context.Context, url string, h http.Header) (realtime.Conn, error); after func(time.Duration) <-chan time.Time; tuiRunning func(config.Paths) (bool, error); exe string }`, `func run(ctx context.Context, o Options, d deps) error` — `Run` 은 `run(ctx, o, deps{after: time.After, tuiRunning: instance.TUIRunning, exe: watchedExecutable()})`(dial nil = realtime 기본).
  - `exewatch.go`: `func watchedExecutable() string` — Windows 면 `""`(감독자 없음 → 감시 끔), 그 밖엔 `os.Args[0]` 이 절대 경로면 그것(launchd/systemd 가 등록된 `<prefix>/bin/masil` 심볼릭 링크 경로 그대로 넘김 — 링크를 풀지 않아야 `brew upgrade` 의 링크 교체가 보인다), 아니면 `os.Executable()`. `type exeWatch struct{ path string; base os.FileInfo; pending os.FileInfo }`, `func newExeWatch(path string) *exeWatch`(path `""` 또는 첫 `os.Stat` 실패면 nil = 감시 안 함), `func (w *exeWatch) changed() bool` — `os.Stat`(링크를 따라감) 결과가 `base` 와 `os.SameFile` 이고 크기·수정 시각이 같으면 false; 파일이 없으면(업그레이드 도중) false; 다르면 **두 번 연속 같은 모양(SameFile·크기·수정 시각)으로 보일 때만** true(덮어쓰는 중인 반쪽 파일로 재시작하지 않게 — 첫 번째는 `pending` 에 담아 둠).
  - `runstatus.go`(**데몬 전용 파일, 계약 3.2 표 밖** — 데몬만 쓰고 이 계획의 `Inspect` 만 읽음): `<Dir>/daemon-status.json`, `type runStatus struct{ Version int "json:\"version\""; PID int "json:\"pid\""; UpdatedAt time.Time "json:\"updated_at\""; ConfigServer string "json:\"config_server\""; CredentialsServer string "json:\"credentials_server\""; ServerMismatch bool "json:\"server_mismatch\""; EnvServer bool "json:\"env_server\"" }`(`ConfigServer` = 데몬 프로세스가 자기 환경에서 본 `cfg.ServerURL()`, `CredentialsServer` 는 로그인 안 됐으면 `""`, `EnvServer` = 데몬 자신의 환경에 `MASIL_SERVER` 가 있었는지), `func runStatusPath(p config.Paths) string`, `func writeRunStatus(p config.Paths, s runStatus) error`(`Version=1`, `config.WriteFileAtomic` 0600), `func readRunStatus(p config.Paths) (*runStatus, error)`(없음 → `(nil, nil)`).

`run` 절차:
1. 기본값 채움 → `config.EnsureDir` → `AcquireDaemon` 을 200ms 간격 **최대 3회** 시도(`status` 의 순간 탐침과 겹쳐도 데몬이 포기하지 않게), 계속 실패면 `instance.ErrAlreadyRunning` 반환.
2. `notifier := notify.New(o.Backend, notify.OpenLedger(p), log)`, `deviceID := auth.DeviceID(p)`, `var desktop atomic.Bool`(처음 값 `o.Config.Notify.Desktop`; handler 의 `desktop` 인자 = `desktop.Load`), `xw := newExeWatch(d.exe)`.
3. 루프(ctx 끝날 때까지): 매 바퀴 `config.Load` 로 설정을 다시 읽고(`desktop.Store(cfg.Notify.Desktop)`·서버 주소 반영; `ErrPartial` 은 값 그대로 쓰고, 그 밖의 오류면 WARN `msg="config reload failed"`(1번) 후 이전 설정 유지) `auth.Load`. 판정할 때마다 `runStatus{ConfigServer: cfg.ServerURL(), CredentialsServer: creds 있으면 creds.Server, ServerMismatch: creds 있고 !Matches, EnvServer: os.Getenv("MASIL_SERVER") != ""}` 를 만들어 **직전에 쓴 값과 다를 때만** `writeRunStatus`(시작 직후 첫 판정은 항상 씀). 대기 중에는 `PollCredentials` 마다 `xw.changed()` 도 보고, true 면 아래 3-b.
   - `ErrNoCredentials` → INFO `msg="not logged in; waiting for credentials"`(상태가 바뀔 때만 1번) 후 `PollCredentials` 대기.
   - `!creds.Matches(cfg.ServerURL())` → **WARN** `msg="credentials server mismatch"`(속성 `credentials_server`, `config_server`, 1번) 후 대기. 소켓 열지 않음.
   - `creds.Token == rejectedToken` → 대기(같은 토큰으로 재접속 금지).
   - 그 외 세션: 자식 ctx, `loadState` → `resumePoint(st, creds.UserID)`, `api.New(cfg.ServerURL(), api.WithToken(creds.Token))`, `realtime.New(Options{API, DeviceID, Mode: ModeNotify, Resume, OnEvent: h.onEvent, OnState: func(s){ pr.observe(s.TUIOnDevice) }, Logger, Now, Dial: d.dial})`, handler 의 `mark` = `client.MarkProcessed` + 저장 예약. **감시 고루틴**이 `PollCredentials` 마다 (1) 자격 증명을 다시 읽어 토큰·서버가 바뀌거나 파일이 사라지면 자식 ctx 취소, (2) `config.toml` 을 다시 읽어 `desktop.Store(cfg.Notify.Desktop)` — 값이 바뀌면 INFO `msg="desktop setting changed"`(속성 `desktop`) — 하고 `ServerURL()` 이 바뀌었으면 자식 ctx 취소(새 주소로 다음 바퀴), (3) `xw.changed()` 면 3-b.
   - `Run` 결과: `ErrUnauthorized|ErrSuspended|ErrUpgradeRequired` → **WARN** `msg="session rejected; waiting for new credentials"`(속성 `reason`=`unauthorized|suspended|upgrade_required`, `token`=`Redacted()`), `rejectedToken = creds.Token`, **credentials.json 은 지우지 않음**. `ErrUpgradeRequired` 는 새 토큰이 아니라 새 바이너리가 풀어 주므로 대기 중 `xw.changed()` 가 3-b 로 이어진다(Windows 처럼 감시가 없으면 다음 로그인 때 새 버전이 뜰 때까지 대기). 자식 취소(자격 증명·서버 변경) → 바로 다음 바퀴. 그 밖의 nil 아닌 오류(ctx 취소 제외)는 **ERROR** `msg="unexpected realtime error"`(속성 `err`) 후 `PollCredentials` 대기하고 다음 바퀴.
   - **3-b 실행 파일 교체**: INFO `msg="executable replaced; exiting for restart"`(속성 `exe`), 세션 정리·상태 저장(4) 후 `ErrRestartRequired` 반환 → `masil daemon run` 이 종료 코드 1 로 나가 launchd/systemd 가 새 바이너리로 다시 띄운다(Global Constraints).
4. 상태 저장: `mark` 뒤 5초 디바운스로 `saveState(State{UserID, Epoch: *Position().Epoch, LastSeq: *Position().LastSeq, SavedAt: now()})`, 세션 종료·`Run` 반환 때 한 번 더(Epoch 가 nil 이면 저장 안 함).

가짜 허브(`fakehub_test.go`): `newFakeHub(t) *fakeHub` 가 `dial` 을 제공. 연결마다 hello(`mode:"notify"`, `epoch:"E1"`, `seq:0`, `oldest_seq:1`, `heartbeat_interval_ms:600000`, `heartbeat_timeout_ms:1200000`, `local_tui_connected:false`)를 먼저 내보내고, 클라이언트의 `resume` 프레임을 기록한 뒤 `ready{resumed:false, resync_required:false, replayed:0, seq:0, epoch:"E1"}` 를 보낸다. 메서드: `Dials() []dialRecord{URL string; Auth string}`, `Resumes() []json.RawMessage`, `Push(frame string)`, `CloseWith(code int)` — 읽기 오류로 `websocket.CloseError{Code: websocket.StatusCode(code)}` 를 돌려줌(core realtime 이 `websocket.CloseStatus` 로 코드를 읽는다는 계약 1.4 전제).

- [ ] **Step 1: Write the failing tests**

```go
func TestRunSecondInstance(t *testing.T) {
	p := testPaths(t)
	release, err := instance.AcquireDaemon(p)
	if err != nil { t.Fatal(err) }
	defer release()
	err = run(t.Context(), testOptions(t, p, nil), testDeps(newFakeHub(t)))
	if !errors.Is(err, instance.ErrAlreadyRunning) { t.Fatalf("err = %v", err) }
}

func TestRunWaitsForCredentials(t *testing.T) {
	p, hub, logs := setupRun(t) // starts run() in a goroutine, PollCredentials=50ms, cancels on cleanup
	expectNoDial(t, hub, 200*time.Millisecond)
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
	d := waitDial(t, hub)
	if !strings.Contains(d.URL, "mode=notify") || d.Auth != "Bearer msl_test_token_A" { t.Fatalf("dial %+v", d) }
	if !strings.Contains(logs.String(), `level=INFO msg="not logged in; waiting for credentials"`) { t.Fatal("missing INFO wait log") }
}

func TestRunServerMismatchStaysOffline(t *testing.T) {
	p, hub, logs := setupRun(t)
	saveCreds(t, p, "http://127.0.0.1:9999", "msl_test_token_A") // config server is http://localhost:8787
	expectNoDial(t, hub, 300*time.Millisecond)
	out := logs.String()
	if strings.Count(out, `level=WARN msg="credentials server mismatch"`) != 1 { t.Fatalf("want exactly one WARN mismatch log:\n%s", out) }
	if strings.Contains(out, "msl_test_token_A") { t.Fatal("token leaked into log") }
	rs, err := readRunStatus(p) // what the daemon itself saw, for `masil daemon status`
	if err != nil || rs == nil || rs.ConfigServer != "http://localhost:8787" || rs.CredentialsServer != "http://127.0.0.1:9999" || !rs.ServerMismatch || rs.EnvServer {
		t.Fatalf("run status %+v, %v", rs, err)
	}
}

// [notify] desktop flipped in config.toml while a session is live takes effect without reconnecting.
func TestRunDesktopToggleMidSession(t *testing.T) {
	p, hub, logs, rec, _ := setupRunWithBackend(t)
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
	waitDial(t, hub)
	hub.Push(alertFrame(5, "01JC9ZA0000000000000000005", "dm")) // same shape as TestRunAlertEndToEndAndState's frame
	waitFor(t, func() bool { return len(rec.ids()) == 1 })

	setDesktop(t, p, false) // config.Load -> Notify.Desktop=false -> config.Save
	waitFor(t, func() bool { return strings.Contains(logs.String(), `msg="desktop setting changed" desktop=false`) })
	hub.Push(alertFrame(6, "01JC9ZA0000000000000000006", "dm"))
	expectStable(t, func() []string { return rec.ids() }, 200*time.Millisecond)
	if len(rec.ids()) != 1 { t.Fatalf("desktop=false must not pop up: %v", rec.ids()) }

	setDesktop(t, p, true)
	waitFor(t, func() bool { return strings.Contains(logs.String(), `msg="desktop setting changed" desktop=true`) })
	hub.Push(alertFrame(7, "01JC9ZA0000000000000000007", "dm"))
	waitFor(t, func() bool { return len(rec.ids()) == 2 })
	if len(hub.Dials()) != 1 { t.Fatalf("toggling desktop must not reconnect: %d dials", len(hub.Dials())) }
}

// brew upgrade / install.sh replace the binary: the daemon exits with ErrRestartRequired so
// launchd/systemd relaunch the new one — also when it was parked on 4010 upgrade_required.
func TestRunRestartsWhenExecutableReplaced(t *testing.T) {
	for _, upgradeRequired := range []bool{false, true} {
		t.Run(fmt.Sprintf("after_4010=%v", upgradeRequired), func(t *testing.T) {
			exe := writeTempExe(t, "v1") // absolute temp file standing in for <prefix>/bin/masil
			p, hub, logs, done := setupRunWith(t, exe)
			saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
			waitDial(t, hub)
			if upgradeRequired {
				hub.CloseWith(4010)
				expectNoDial(t, hub, 200*time.Millisecond)
				if !strings.Contains(logs.String(), `level=WARN msg="session rejected; waiting for new credentials" reason=upgrade_required`) { t.Fatal(logs.String()) }
			}
			replaceExe(t, exe, "v2-longer") // write sibling temp file, then os.Rename over exe
			select {
			case err := <-done:
				if !errors.Is(err, ErrRestartRequired) { t.Fatalf("run returned %v", err) }
			case <-time.After(3 * time.Second):
				t.Fatal("daemon kept running on the old binary")
			}
			if !strings.Contains(logs.String(), `level=INFO msg="executable replaced; exiting for restart"`) { t.Fatal(logs.String()) }
		})
	}
}

func TestRunUnauthorizedWaitsForNewToken(t *testing.T) {
	p, hub, logs := setupRun(t)
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
	waitDial(t, hub)
	hub.CloseWith(4001)
	expectNoDial(t, hub, 300*time.Millisecond) // same token: never redial
	if _, err := os.Stat(p.Credentials); err != nil { t.Fatal("credentials.json must not be deleted") }
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_B")
	if d := waitDial(t, hub); d.Auth != "Bearer msl_test_token_B" { t.Fatalf("redial %+v", d) }
	if strings.Contains(logs.String(), "msl_test_token_") { t.Fatal("token leaked into log") }
}

func TestRunAlertEndToEndAndState(t *testing.T) {
	p, hub, _, rec, stop := setupRunWithBackend(t)
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
	waitDial(t, hub)
	waitFor(t, func() bool { return len(hub.Resumes()) == 1 })
	if got := canonical(hub.Resumes()[0]); got != `{"use_device_ack":true}` { t.Fatalf("resume %s", got) }
	hub.Push(`{"type":"alert","seq":5,"ts":"2026-11-05T02:05:00Z","data":{"alert":{"id":"01JC9ZA0000000000000000000","kind":"dm","title":"민지","body":"점심?","actor":null,"target":{"type":"message","id":"M1","channel_id":"C1","message_id":"M1","thread_root_id":null,"post_id":null,"space_id":null},"created_at":"2026-11-05T02:05:00Z","suppressed":false,"suppressed_reason":null}}}`)
	waitFor(t, func() bool { return len(rec.ids()) == 1 })
	stop() // cancel ctx, wait for run() to return nil
	st, err := loadState(p)
	if err != nil || st == nil || st.LastSeq != 5 || st.Epoch != "E1" || st.UserID != "01JA0000000000000000000SAM" {
		t.Fatalf("state %+v, %v", st, err)
	}
}

func TestRunResumePoint(t *testing.T) {
	for name, tc := range map[string]struct{ stateUser, want string }{
		"same_user":  {"01JA0000000000000000000SAM", `{"epoch":"E1","last_seq":41}`},
		"other_user": {"01JA00000000000000000OTHER", `{"use_device_ack":true}`},
	} {
		t.Run(name, func(t *testing.T) {
			p, hub, _ := setupRun(t)
			saveState(p, State{UserID: tc.stateUser, Epoch: "E1", LastSeq: 41, SavedAt: t0})
			saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
			waitFor(t, func() bool { return len(hub.Resumes()) == 1 })
			if got := canonical(hub.Resumes()[0]); got != tc.want { t.Fatalf("resume %s want %s", got, tc.want) }
		})
	}
}
```

`saveCreds` 는 `auth.Save(p, &auth.Credentials{Version: 1, Server, Token, SessionID: "01JA7000000000000000000000", UserID: "01JA0000000000000000000SAM", Handle: "sam", SavedAt: t0})`. resume 프레임 비교는 `data` 객체만(가짜 허브가 `{"type":"resume","data":…}` 의 `data` 를 기록)이고, `canonical(raw json.RawMessage) string` 은 `map[string]any` 로 풀었다가 다시 `json.Marshal`(키 정렬)한 문자열. `setupRun(t) (config.Paths, *fakeHub, *syncBuffer)` 는 `t.Setenv("MASIL_SERVER", "")`(설정 서버 = 기본 `http://localhost:8787`), `PollCredentials: 50*time.Millisecond`, 텍스트 slog → `syncBuffer` 로 `run` 을 고루틴에서 띄우고 정리 때 취소·반환 대기. `setupRunWithBackend(t) (config.Paths, *fakeHub, *syncBuffer, *lockedRecorder, func())` 는 같은 것에 `lockedRecorder` 백엔드와 `stop()`(취소 후 `run` 이 nil 을 돌려줬는지 확인)을 더한다. `setupRunWith(t, exe) (config.Paths, *fakeHub, *syncBuffer, <-chan error)` 는 `deps.exe = exe` 로 띄우고 `run` 의 반환값을 채널로 준다(정리 때 취소). `alertFrame(seq, id, kind) string` 은 `TestRunAlertEndToEndAndState` 의 alert 프레임을 seq·id·kind 만 바꿔 만든다. `setDesktop(t, p, on)` 은 `config.Load` → `Notify.Desktop = on` → `config.Save`. `writeTempExe(t, content)`/`replaceExe(t, path, content)` 는 임시 디렉터리의 파일을 만들고, 같은 디렉터리 임시 파일을 쓴 뒤 `os.Rename` 으로 바꿔 끼운다(inode 가 바뀜).

`internal/daemon/exewatch_test.go`:

```go
func TestExeWatch(t *testing.T) {
	if newExeWatch("") != nil { t.Fatal(`"" must disable watching`) }
	exe := writeTempExe(t, "v1")
	w := newExeWatch(exe)
	if w.changed() { t.Fatal("unchanged file reported as changed") }
	os.Remove(exe)
	if w.changed() { t.Fatal("missing file (mid-upgrade) must not trigger") }
	replaceExe(t, exe, "v2-longer")
	if w.changed() { t.Fatal("first sighting must only arm (settle check)") }
	if !w.changed() { t.Fatal("second identical sighting must trigger") }
	if runtime.GOOS != "windows" { // brew-style symlink swap: path is a link, target changes
		dir := t.TempDir()
		a, b, link := filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "masil")
		os.WriteFile(a, []byte("a"), 0o755); os.WriteFile(b, []byte("bb"), 0o755); os.Symlink(a, link)
		lw := newExeWatch(link)
		os.Remove(link); os.Symlink(b, link)
		lw.changed()
		if !lw.changed() { t.Fatal("symlink retarget not detected") }
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/daemon/ -run TestRun -v`
Expected: FAIL — `undefined: run`

- [ ] **Step 3: Implement `Options`, `Run`, `run`, `deps` in `internal/daemon/daemon.go`** (절차는 위 Interfaces 그대로)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/daemon/ -v -race -count=1`
Expected: PASS (Task 1~4 전부). 이어서 `GOOS=windows go vet ./internal/daemon/ && GOOS=linux go vet ./internal/daemon/` → 출력 없음.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/daemon.go internal/daemon/runstatus.go internal/daemon/exewatch.go internal/daemon/daemon_test.go internal/daemon/exewatch_test.go internal/daemon/fakehub_test.go internal/daemon/helpers_test.go
git commit -m "feat(daemon): notify-mode run loop with credential wait, live config, restart on upgrade"
```

---

### Task 5: 데몬 로그 파일과 상태 점검(`Inspect`)

**Files:**
- Create: `internal/daemon/logfile.go`, `internal/daemon/inspect.go`
- Test: `internal/daemon/logfile_test.go`, `internal/daemon/inspect_test.go`

**Interfaces:**
- Consumes: `config.Paths.LogDir`, `auth.Load`, `(*auth.Credentials).Matches`, `config.Config.Server`, `config.DefaultServer`, `instance.AcquireDaemon`, `instance.ErrAlreadyRunning`, Task 1 `loadState`, Task 4 `readRunStatus`. **`config.(*Config).ServerURL` 는 쓰지 않는다** — CLI 셸의 `MASIL_SERVER` 가 섞여 자동 시작된 데몬이 실제로 보는 주소와 달라지기 때문(Review Focus 1).
- Produces (daemon-release 내부 공개 — 계약 외, `cmd/masil/cmd_daemon.go` 만 사용):
  ```go
  // NewLogger writes slog text to <LogDir>/daemon.log (rotated once to daemon.log.1 when it would exceed 5 MiB)
  // and, if extra != nil, also to extra (foreground --verbose).
  func NewLogger(p config.Paths, extra io.Writer) (*slog.Logger, io.Closer, error)

  type Report struct {
  	Running        bool   // daemon.lock held by another process
  	LoggedIn       bool
  	Handle         string
  	CredServer     string
  	ConfigServer   string // the server the DAEMON uses (see ServerSource), never the CLI shell's MASIL_SERVER
  	ServerSource   string // "daemon": read from daemon-status.json of the running daemon; "config": config.toml server > DefaultServer
  	ShellServer    string // this shell's MASIL_SERVER when set and != ConfigServer (the autostarted daemon cannot see it); "" otherwise
  	ServerMismatch bool   // LoggedIn && !creds.Matches(ConfigServer)
  	DesktopEnabled bool
  	State          *State   // nil if no daemon-state.json
  	Problems       []string // last <= 3 lines of daemon.log containing level=WARN or level=ERROR (from the last 64 KiB)
  }
  func Inspect(p config.Paths, c *config.Config) Report

  // AutostartServer is the server an autostarted daemon (launchd/systemd/Run key: no shell env) will use:
  // c.Server (trailing "/" trimmed) if non-empty, else config.DefaultServer. MASIL_SERVER is ignored on purpose.
  func AutostartServer(c *config.Config) string
  ```
  패키지 내부: `func openRotating(path string, max int64) (*rotatingFile, error)` — `NewLogger` 는 `max = 5 << 20`.

  `ConfigServer` 결정: `Running` 이고 `readRunStatus` 가 값을 주면 그 `ConfigServer`(`ServerSource="daemon"` — 데몬이 셸에서 `MASIL_SERVER` 와 함께 수동으로 떴다면 그 주소가 맞다), 아니면 `AutostartServer(c)`(`ServerSource="config"`). `ServerMismatch` 는 파일의 옛 플래그가 아니라 **지금의** `credentials.json` 으로 다시 계산한다(데몬은 `PollCredentials` 안에 따라잡는다).

- [ ] **Step 1: Write the failing tests**

```go
func TestRotatingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.log")
	f, err := openRotating(path, 100)
	if err != nil { t.Fatal(err) }
	f.Write(bytes.Repeat([]byte("a"), 80))
	f.Write(bytes.Repeat([]byte("b"), 40)) // would exceed 100 -> rotate first
	f.Close()
	old, _ := os.ReadFile(path + ".1")
	cur, _ := os.ReadFile(path)
	if len(old) != 80 || string(cur) != strings.Repeat("b", 40) { t.Fatalf("old=%d cur=%q", len(old), cur) }
}

func TestInspect(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		p := testPaths(t)
		t.Setenv("MASIL_SERVER", "")
		cfg := config.Defaults() // Server empty/default -> AutostartServer = http://localhost:8787
		r := Inspect(p, cfg)
		if r.Running || r.LoggedIn || r.State != nil || !r.DesktopEnabled || r.ConfigServer != "http://localhost:8787" || r.ServerSource != "config" {
			t.Fatalf("empty: %+v", r)
		}

		saveCreds(t, p, "http://127.0.0.1:9999", "msl_test_token_A")
		saveState(p, State{UserID: "01JA0000000000000000000SAM", Epoch: "E1", LastSeq: 1041, SavedAt: t0})
		log, closer, _ := NewLogger(p, nil)
		log.Warn("credentials server mismatch", "credentials_server", "http://127.0.0.1:9999")
		log.Info("realtime ready")
		closer.Close()
		release, _ := instance.AcquireDaemon(p)
		defer release()

		r = Inspect(p, cfg)
		if !r.Running || !r.LoggedIn || r.Handle != "sam" || !r.ServerMismatch || r.State.LastSeq != 1041 {
			t.Fatalf("report %+v", r)
		}
		if len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "credentials server mismatch") { t.Fatalf("problems %q", r.Problems) }
	})

	// Review Focus 1: logged in with MASIL_SERVER=local, autostarted daemon can't see it,
	// and `masil daemon status` is run from that same shell.
	t.Run("shell_env_not_seen_by_daemon", func(t *testing.T) {
		p := testPaths(t)
		t.Setenv("MASIL_SERVER", "http://127.0.0.1:9999")
		cfg := config.Defaults()
		saveCreds(t, p, "http://127.0.0.1:9999", "msl_test_token_A")
		r := Inspect(p, cfg) // daemon not running -> predict the autostart view
		if r.ConfigServer != "http://localhost:8787" || r.ServerSource != "config" || !r.ServerMismatch || r.ShellServer != "http://127.0.0.1:9999" {
			t.Fatalf("shell env leaked into daemon view: %+v", r)
		}

		// A running daemon that recorded what it actually sees wins over the prediction.
		release, _ := instance.AcquireDaemon(p)
		defer release()
		writeRunStatus(p, runStatus{ConfigServer: "http://localhost:8787", CredentialsServer: "http://127.0.0.1:9999", ServerMismatch: true, UpdatedAt: t0})
		if r := Inspect(p, cfg); r.ServerSource != "daemon" || !r.ServerMismatch { t.Fatalf("running daemon view: %+v", r) }
		writeRunStatus(p, runStatus{ConfigServer: "http://127.0.0.1:9999", CredentialsServer: "http://127.0.0.1:9999", EnvServer: true, UpdatedAt: t0}) // started by hand in that shell
		if r := Inspect(p, cfg); r.ServerSource != "daemon" || r.ServerMismatch || r.ShellServer != "" { t.Fatalf("hand-started daemon: %+v", r) }
	})

	t.Run("autostart_server", func(t *testing.T) {
		t.Setenv("MASIL_SERVER", "http://ignored.example")
		c := config.Defaults()
		c.Server = "https://masil.example.workers.dev/"
		if got := AutostartServer(c); got != "https://masil.example.workers.dev" { t.Fatalf("AutostartServer = %q", got) }
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/daemon/ -run 'TestRotatingFile|TestInspect' -v`
Expected: FAIL — `undefined: openRotating`

- [ ] **Step 3: Implement `NewLogger`/`openRotating` in `logfile.go` and `Report`/`Inspect` in `inspect.go`**

`Running` 판정: `instance.AcquireDaemon` 이 `ErrAlreadyRunning` 이면 true, 성공이면 즉시 release 후 false(데몬 쪽 3회 재시도가 이 순간 탐침과의 경합을 흡수 — Task 4). 로그 파일·디렉터리는 0600/0700. `Problems` 는 `level=WARN`·`level=ERROR` 만 — Global Constraints 의 로그 수준 표가 어떤 메시지가 여기에 오는지 정한다(`not logged in` 같은 INFO 는 오지 않음). `ShellServer` = `strings.TrimRight(os.Getenv("MASIL_SERVER"), "/")` 가 비어 있지 않고 `ConfigServer` 와 다를 때만.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/daemon/ -run 'TestRotatingFile|TestInspect' -v -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/logfile.go internal/daemon/inspect.go internal/daemon/logfile_test.go internal/daemon/inspect_test.go
git commit -m "feat(daemon): rotating daemon.log and status report from the daemon's own view"
```

---

### Task 6: 자동 시작 등록 파일 생성기와 안정 실행 경로

**Files:**
- Create: `internal/autostart/autostart.go`, `internal/autostart/render.go`
- Test: `internal/autostart/render_test.go`, `internal/autostart/testdata/TestLaunchdPlist/*.golden`, `testdata/TestSystemdUnit/*.golden`, `testdata/TestRunValue/*.golden`

**Interfaces:**
- Consumes: 없음(표준 라이브러리 `encoding/xml` 의 `EscapeText`, `path/filepath`)
- Produces (계약 9.2 + 같은 패키지 공개 상수/함수):
  ```go
  type Status struct{ Installed, Running bool; Detail string } // 계약
  const (
  	Label        = "com.patrache.masil.daemon"
  	UnitName     = "masil-daemon.service"
  	RunValueName = "masil-daemon"
  )
  var ErrUnsupported = errors.New("autostart: not supported on this OS")
  // StableExecutable maps a Homebrew Cellar path (<prefix>/Cellar/masil/<ver>/bin/masil) to <prefix>/bin/masil
  // when that exists, and rejects `go run` temp binaries (path containing "go-build") with a Korean error.
  func StableExecutable(exe string) (string, error)
  ```
  패키지 내부(OS 무관, 모든 OS 에서 시험):
  ```go
  func launchdPlist(exe, configDir string) []byte // configDir == "" => no EnvironmentVariables
  func systemdUnit(exe, configDir string) []byte
  func runValue(exe string) string                // `"<exe>" daemon run`
  func plistExe(content []byte) string            // ProgramArguments[0], XML-unescaped; "" if absent
  func unitExe(content []byte) string             // first ExecStart word, unquoted/unescaped
  func runValueExe(v string) string
  // seams for Task 7 (swapped by tests)
  var (
  	homeDir = os.UserHomeDir
  	getuid  = os.Getuid
  	run     = func(name string, args ...string) ([]byte, error) { return exec.Command(name, args...).CombinedOutput() }
  	lookPath = exec.LookPath // Linux Install checks for systemctl (Task 7)
  )
  ```

정확한 형식(골든 `default` 케이스와 같아야 함):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.patrache.masil.daemon</string>
	<key>ProgramArguments</key>
	<array>
		<string>/opt/homebrew/bin/masil</string>
		<string>daemon</string>
		<string>run</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
</dict>
</plist>
```
(`configDir != ""` 이면 `</dict>` 직전에 `EnvironmentVariables` → `MASIL_CONFIG_DIR` 딕셔너리.)

```ini
[Unit]
Description=masil notification daemon
Documentation=https://github.com/patrache/masil

[Service]
Type=simple
ExecStart="/home/me/.local/bin/masil" daemon run
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
```
(`configDir != ""` 이면 `RestartSec` 다음 줄 `Environment="MASIL_CONFIG_DIR=<dir>"`. 인용 규칙: `\` → `\\`, `"` → `\"`, `%` → `%%`.) `KeepAlive{SuccessfulExit:false}`·`Restart=on-failure` 는 "이미 실행 중이면 종료 코드 0" 과 짝이다 — 두 번째 데몬이 재시작 고리를 만들지 않는다. 반대로 "실행 파일이 바뀌었으면 종료 코드 1"(`ErrRestartRequired`, Task 4·8)은 이 두 설정 덕분에 새 바이너리로 다시 뜬다(launchd 는 기본 10초 간격 제한, systemd 는 `RestartSec=10`).

- [ ] **Step 1: Write the failing tests**

```go
func TestLaunchdPlist(t *testing.T) {
	for name, c := range map[string]struct{ exe, dir string }{
		"default":             {"/opt/homebrew/bin/masil", ""},
		"config_dir":          {"/Users/me/.local/bin/masil", "/Users/me/.config/masil-work"},
		"spaces_and_specials": {"/Users/me/My Apps & Tools/<masil>", "/Users/me/설정 폴더"},
	} {
		t.Run(name, func(t *testing.T) {
			out := launchdPlist(c.exe, c.dir)
			golden.RequireEqual(t, out)
			if got := plistExe(out); got != c.exe { t.Fatalf("plistExe round-trip = %q", got) }
		})
	}
}

func TestSystemdUnit(t *testing.T) {
	for name, c := range map[string]struct{ exe, dir string }{
		"default":             {"/home/me/.local/bin/masil", ""},
		"config_dir":          {"/home/me/.local/bin/masil", "/home/me/.config/masil-work"},
		"spaces_and_specials": {`/home/me/my apps/100% "masil"\bin/masil`, "/home/me/설정 폴더"},
	} {
		t.Run(name, func(t *testing.T) {
			out := systemdUnit(c.exe, c.dir)
			golden.RequireEqual(t, out)
			if got := unitExe(out); got != c.exe { t.Fatalf("unitExe round-trip = %q", got) }
		})
	}
}

func TestRunValue(t *testing.T) {
	for name, exe := range map[string]string{
		"default":             `C:\Users\me\AppData\Local\Programs\masil\masil.exe`,
		"spaces_and_specials": `C:\Users\Kim Min\AppData\Local\Programs\masil & co\masil.exe`,
	} {
		t.Run(name, func(t *testing.T) {
			v := runValue(exe)
			golden.RequireEqual(t, v)
			if got := runValueExe(v); got != exe { t.Fatalf("runValueExe round-trip = %q", got) }
		})
	}
}

func TestStableExecutable(t *testing.T) {
	prefix := t.TempDir()
	cellar := filepath.Join(prefix, "Cellar", "masil", "0.1.0", "bin", "masil")
	link := filepath.Join(prefix, "bin", "masil")
	os.MkdirAll(filepath.Dir(cellar), 0o755); os.WriteFile(cellar, nil, 0o755)
	if got, _ := StableExecutable(cellar); got != cellar { t.Fatalf("no link yet: %q", got) }
	os.MkdirAll(filepath.Dir(link), 0o755); os.Symlink(cellar, link)
	if got, _ := StableExecutable(cellar); got != link { t.Fatalf("cellar -> %q, want %q", got, link) }
	if got, _ := StableExecutable("/usr/local/bin/masil"); got != "/usr/local/bin/masil" { t.Fatal(got) }
	_, err := StableExecutable("/var/folders/x/T/go-build1234/b001/exe/masil")
	if err == nil || !strings.Contains(err.Error(), "go run") { t.Fatalf("go-build: %v", err) }
}
```

(`TestStableExecutable` 의 심볼릭 링크 부분은 Windows 에서 `t.Skip` — `runtime.GOOS == "windows"`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/autostart/ -v`
Expected: FAIL — `undefined: launchdPlist`

- [ ] **Step 3: Implement `autostart.go` and `render.go`**

`launchdPlist` 는 `xml.EscapeText` 로 문자열 값을 이스케이프하고 탭 들여쓰기. 파서 셋은 생성기와 짝(정규식 1개 + 역이스케이프). `StableExecutable` 은 `filepath.ToSlash` 로 `/Cellar/masil/` 를 찾고 `<prefix>/bin/masil` 이 `os.Stat` 되면 그 경로.

- [ ] **Step 4: Generate goldens, inspect them, then run**

Run: `go test ./internal/autostart/ -run 'TestLaunchdPlist|TestSystemdUnit|TestRunValue' -update && cat internal/autostart/testdata/TestLaunchdPlist/default.golden internal/autostart/testdata/TestSystemdUnit/default.golden && plutil -lint internal/autostart/testdata/TestLaunchdPlist/*.golden`
Expected: `default.golden` 두 개가 위 형식과 글자 그대로 같고, `plutil` 이 세 파일 모두 `OK`. `spaces_and_specials` 골든에 `&amp;`·`&lt;`(plist), `%%`·`\"`·`\\`(unit), 따옴표로 감싼 경로(Run 값)가 보인다.
Run: `go test ./internal/autostart/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/autostart/autostart.go internal/autostart/render.go internal/autostart/render_test.go internal/autostart/testdata
git commit -m "feat(autostart): launchd/systemd/run-key renderers and stable executable path"
```

---

### Task 7: OS별 `Install` / `Uninstall` / `Query`

**Files:**
- Create: `internal/autostart/install_darwin.go`, `install_linux.go`, `install_windows.go`, `install_other.go`(`//go:build !darwin && !linux && !windows`)- Test: `internal/autostart/install_darwin_test.go`, `install_linux_test.go`, `install_windows_test.go`

**Interfaces:**
- Consumes: Task 6 생성기·파서·시드(`homeDir`, `getuid`, `run`, `lookPath`), `config.Paths.Dir`, `config.WriteFileAtomic`, `golang.org/x/sys/windows/registry`(`OpenKey`, `CreateKey`, `CURRENT_USER`, `SET_VALUE|QUERY_VALUE`, `(Key).SetStringValue`, `GetStringValue`, `DeleteValue`, `Close`, `ErrNotExist`)
- Produces (계약 9.2): `func Install(exe string, p config.Paths) error`, `func Uninstall() error`, `func Query() (Status, error)`. Windows 내부 시드: `var runKey runKeyStore = registryRunKey{}`, `type runKeyStore interface{ Get(name string) (string, error); Set(name, value string) error; Delete(name string) error }`(없는 값은 `registry.ErrNotExist`).

공통 규칙: `exe` 는 절대 경로여야 하고 존재해야 한다(아니면 오류). `configDir` = `os.Getenv("MASIL_CONFIG_DIR") != ""` 일 때만 `p.Dir`. 설치는 **멱등**(다시 하면 덮어쓰고 다시 로드). `Query` 는 등록된 실행 파일이 없으면 `Detail` 끝에 ` · 실행 파일 없음: <exe>`.

| OS | Install 명령 순서(시드 `run` 에 기록) | Uninstall | Query |
|---|---|---|---|
| darwin | plist 쓰기(0644) → `launchctl bootout gui/<uid>/com.patrache.masil.daemon`(실패 무시) → `launchctl bootstrap gui/<uid> <plist>` | bootout(실패 무시) → plist 삭제(없으면 무시) | Installed=plist 존재, Running=`launchctl print gui/<uid>/<Label>` 성공 && 출력에 `state = running`, Detail=`launchd <plist 경로>` |
| linux | `systemctl` 이 `lookPath`(시드, 기본 `exec.LookPath`) 에 없으면 한국어 오류(`systemd 사용자 세션을 찾지 못했습니다 — 'masil daemon run' 을 셸 시작 파일에 넣어 실행하세요`) → unit 쓰기(0644) → `systemctl --user daemon-reload` → `systemctl --user enable masil-daemon.service` → `systemctl --user restart masil-daemon.service`(이미 떠 있던 옛 데몬도 새 `ExecStart` 로 갈아탐 — `enable --now` 는 실행 중인 유닛을 다시 시작하지 않음) | `systemctl --user disable --now masil-daemon.service`(실패 무시) → 파일 삭제 → `daemon-reload` | Installed=파일 존재, Running=`systemctl --user is-active masil-daemon.service` 출력 `active`, Detail=`systemd --user <unit 경로>` |
| windows | `MASIL_CONFIG_DIR` 가 설정돼 있으면 오류(`Windows 자동 시작은 MASIL_CONFIG_DIR 를 지원하지 않습니다`) → `runKey.Set("masil-daemon", runValue(exe))` | `runKey.Delete`(없으면 무시) | Installed=값 존재, Running=false(→ `status` 는 `Inspect.Running` 사용), Detail=`HKCU\…\Run\masil-daemon` |
| other | `ErrUnsupported` | `ErrUnsupported` | `Status{}`, `ErrUnsupported` |

- [ ] **Step 1: Write the failing tests** (darwin 판 — linux·windows 판은 같은 모양으로 위 표의 명령/값을 단언)

```go
// fakeEnv: temp HOME, uid 501, and a recording runner. Lines starting with a prefix in okPrefixes
// succeed with the mapped output; everything else fails like a not-loaded launchd job.
func fakeEnv(t *testing.T, okPrefixes map[string]string) (home string, calls *[]string) {
	prevHome, prevUID, prevRun := homeDir, getuid, run
	t.Cleanup(func() { homeDir, getuid, run = prevHome, prevUID, prevRun })
	home = t.TempDir()
	homeDir = func() (string, error) { return home, nil }
	getuid = func() int { return 501 }
	var got []string
	run = func(name string, args ...string) ([]byte, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		got = append(got, line)
		for p, out := range okPrefixes {
			if strings.HasPrefix(line, p) { return []byte(out), nil }
		}
		return []byte("Boot-out failed: 3: No such process"), errors.New("exit status 3")
	}
	return home, &got
}

func TestInstallDarwin(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	home, calls := fakeEnv(t, map[string]string{"launchctl bootstrap ": ""})
	exe := writeExe(t) // temp executable file, absolute path
	if err := Install(exe, config.Paths{Dir: "/ignored"}); err != nil { t.Fatal(err) }
	plist := filepath.Join(home, "Library/LaunchAgents/com.patrache.masil.daemon.plist")
	b, _ := os.ReadFile(plist)
	if !bytes.Equal(b, launchdPlist(exe, "")) { t.Fatal("plist content mismatch") }
	want := []string{
		"launchctl bootout gui/501/com.patrache.masil.daemon",
		"launchctl bootstrap gui/501 " + plist,
	}
	if !slices.Equal(*calls, want) { t.Fatalf("calls %q", *calls) }
	if err := Install(exe, config.Paths{}); err != nil { t.Fatal("reinstall must be idempotent:", err) }
}

func TestQueryDetectsMissingExe(t *testing.T) {
	home, _ := fakeEnv(t, map[string]string{})
	exe := writeExe(t)
	Install(exe, config.Paths{})
	os.Remove(exe)
	st, err := Query()
	if err != nil || !st.Installed || st.Running || !strings.HasSuffix(st.Detail, " · 실행 파일 없음: "+exe) {
		t.Fatalf("status %+v, %v (home %s)", st, err, home)
	}
}

func TestUninstallDarwin(t *testing.T) {
	home, calls := fakeEnv(t, map[string]string{})
	Install(writeExe(t), config.Paths{})
	*calls = nil
	if err := Uninstall(); err != nil { t.Fatal(err) }
	if _, err := os.Stat(filepath.Join(home, "Library/LaunchAgents/com.patrache.masil.daemon.plist")); !os.IsNotExist(err) { t.Fatal("plist left behind") }
	if err := Uninstall(); err != nil { t.Fatal("second uninstall must be a no-op:", err) }
}
```

`TestQueryDetectsMissingExe` 는 `launchctl print` 가 실패(=로드 안 됨)하므로 `Running=false`. Linux 판 `TestInstallLinux`(`install_linux_test.go`, 시드 `lookPath` 로 `systemctl` 존재를 흉내)는 기록된 명령이 정확히 `systemctl --user daemon-reload`, `systemctl --user enable masil-daemon.service`, `systemctl --user restart masil-daemon.service` 순서이고 **두 번째 Install 도 같은 세 줄을 다시 낸다**(재설치·업그레이드 때 실행 중 유닛 재시작)를 단언하며, `enable --now` 가 나오면 실패로 본다(시드는 Task 6 의 `lookPath`). Windows 판은 `runKey` 를 `map` 기반 가짜로 바꿔 `Set("masil-daemon", "\"C:\\…\\masil.exe\" daemon run")` 을 단언한다.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/autostart/ -run 'Install|Uninstall|Query' -v`
Expected: FAIL — `undefined: Install`

- [ ] **Step 3: Implement the four platform files** (위 표 그대로)

- [ ] **Step 4: Run tests and cross-compile checks**

Run: `go test ./internal/autostart/ -v -race`
Expected: PASS (darwin 판; linux·windows 판은 core `ci.yml` 의 3 OS 행렬에서 돈다)
Run: `for os in linux windows freebsd; do GOOS=$os go vet ./internal/autostart/ && GOOS=$os go test -c -o /dev/null ./internal/autostart/ || echo FAIL $os; done`
Expected: 출력 없음(세 OS 모두 시험까지 컴파일됨)

- [ ] **Step 5: Commit**

```bash
git add internal/autostart/install_*.go
git commit -m "feat(autostart): launchd, systemd --user and HKCU Run registration"
```

---

### Task 8: `masil daemon run|install|uninstall|status` 와 Windows 숨김 재실행

(이 Task 는 Global Constraints 의 `masil admin` 실행 전 확인을 이미 통과했다는 전제다 — `cmd/masil` 에 `cmd_admin.go` 를 만들지 않는다.)

**실행 전 확인(차단 조건) — 사용법 오류 표지**: CA 2.4 는 `masil` 의 종료 코드를 0/1/2(usage)로 정하고, core(03) Task 35 의 `run(ctx, args, env) int` 가 그 변환을 맡는다. 등록 명령의 `Run` 은 `error` 만 돌려주므로, 사용법 오류를 2 로 내려면 core 가 `main` 패키지에 표지를 정해 두어야 한다. 이 계획은 그 표지를 **`errUsage`**(`var errUsage = errors.New("usage")`, `run` 이 `errors.Is(err, errUsage)` 면 2) 로 가정한다. 시작 전에 `grep -nE 'errUsage|usageErr|ErrUsage' cmd/masil/main.go cmd/masil/cmd_core.go` 로 확인하고 — (a) 다른 이름·형태(예: `usageError` 타입)면 이 Task 의 `errUsage` 를 그 이름으로 바꿔 쓴다, (b) 표지가 없으면 **이 Task 를 시작하지 말고** 오케스트레이터에 core 가 표지를 정해 달라고 요청한다(이 계획은 core 소유 `main.go` 를 고치지 않는다; 표지 없이 만들면 `masil daemon` 사용법 오류가 종료 코드 1 로 나가 CA 2.4 위반). 05 Task 20 의 `admincli.ErrUsage` 도 같은 core 표지로 감싸야 2 가 된다 — 05 소관이지만 같은 요청에 함께 적는다.

**Files:**
- Create: `cmd/masil/cmd_daemon.go`, `internal/daemon/detach_windows.go`, `internal/daemon/detach_other.go`
- Test: `cmd/masil/cmd_daemon_test.go`, `cmd/masil/testdata/TestDaemonStatus/*.golden`, `internal/daemon/detach_test.go`

**Interfaces:**
- Consumes: core `main.go` 의 `register(c command)`, `command{Name, Usage string; Hidden bool; Run func(ctx context.Context, env *Env, args []string) error}`, `Env{Paths config.Paths; Config *config.Config; Stdin io.Reader; Stdout, Stderr io.Writer}`, 사용법 오류 표지 `errUsage`(위 차단 확인)와 `run(ctx context.Context, args []string, env *Env) int`(03 Task 35 — 시험에서 종료 코드 2 확인); `daemon.Run`, `daemon.Options`, `daemon.ErrRestartRequired`, `daemon.NewLogger`, `daemon.Inspect`, `daemon.Report`, `daemon.AutostartServer`, `autostart.Install/Uninstall/Query/StableExecutable/Status`, `instance.ErrAlreadyRunning`, `notify.NewOSBackend`, `textwidth.PadRight(s string, w int) string`, `humanize.MessageTime(now, t time.Time, loc *time.Location) string`, `golang.org/x/sys/windows`(`CREATE_NO_WINDOW`)
- Produces:
  - `internal/daemon`: `func Detach(args []string) (detached bool, err error)` — Windows: 환경변수 `MASIL_DAEMON_CHILD != "1"` 이면 자기 자신(`os.Executable`)을 같은 인자 + `MASIL_DAEMON_CHILD=1` 로 `SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}` 실행 후 `(true, nil)`; 그 외 `(false, nil)`. 다른 OS: 항상 `(false, nil)`.
  - `cmd/masil`(main 패키지 내부): `func init()` 에서 `register(command{Name: "daemon", Usage: "알림 데몬 (run | install | uninstall | status)", Run: daemonCmd})`; `func daemonCmd(ctx context.Context, env *Env, args []string) error`; `func renderStatus(w io.Writer, r daemon.Report, as autostart.Status, asErr error, now time.Time, loc *time.Location)`; 시험용 시드 `var (daemonRunFn = daemon.Run; autostartInstallFn = autostart.Install; autostartUninstallFn = autostart.Uninstall; autostartQueryFn = autostart.Query; executableFn = os.Executable; detachFn = daemon.Detach; nowFn = time.Now)`.

하위 명령:
- `run [--foreground] [--verbose]`(`flag.NewFlagSet(..., flag.ContinueOnError)`): `--foreground` 가 없으면 `detachFn(os.Args[1:])` — 분리됐으면 `백그라운드에서 데몬을 시작했습니다 (로그: <LogDir>/daemon.log)` 출력 후 nil. 로거 `daemon.NewLogger(env.Paths, verbose ? env.Stderr : nil)`, `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`, `daemonRunFn(ctx, daemon.Options{Paths, Config, Logger})`. `ErrAlreadyRunning` → `데몬이 이미 실행 중입니다.` 출력 후 **nil(종료 코드 0)**. `daemon.ErrRestartRequired` → `env.Stderr` 에 `실행 파일이 바뀌어 종료합니다 — 자동 시작이 새 버전으로 다시 띄웁니다.` 후 **그 오류를 그대로 반환(종료 코드 1)** — launchd `SuccessfulExit:false`·systemd `on-failure` 가 재시작하는 조건이다.
- `install`: `executableFn` → `autostart.StableExecutable` → `autostartInstallFn(exe, env.Paths)` → `자동 시작을 등록했습니다: <Query().Detail>`. 로그인 안 됐으면 `아직 로그인하지 않았습니다 — 'masil login' 뒤부터 알림이 옵니다.`, `notify.desktop=false` 면 `config.toml 의 [notify] desktop 이 꺼져 있어 알림이 뜨지 않습니다.` 를 덧붙인다. 셸에 `MASIL_SERVER` 가 있고 `daemon.AutostartServer(env.Config)` 와 다르면(끝 `/` 무시) `⚠ 셸의 MASIL_SERVER(<값>)는 자동 시작된 데몬에 전달되지 않습니다 — 데몬은 <AutostartServer> 에 접속합니다. config.toml 의 server 를 같은 주소로 맞추세요.` 를 덧붙인다(Review Focus 1). 등록은 그대로 진행한다(경고만).
- `uninstall`: `자동 시작을 해제했습니다.` (Windows 는 `지금 실행 중인 데몬은 다음 로그인부터 뜨지 않습니다.` 추가)
- `status`: `renderStatus(env.Stdout, daemon.Inspect(...), as, asErr, nowFn(), time.Local)`.
- 인자 없음·모르는 하위 명령·하위 명령의 플래그 파싱 오류(`run --nope` 등): `env.Stderr` 에 `사용법: masil daemon run|install|uninstall|status` 후 `fmt.Errorf("%w: masil daemon %s", errUsage, strings.Join(args, " "))` 반환 → core `run` 이 **종료 코드 2**(CA 2.4). 실행 실패(`install` 의 `go run` 경로 거부, `ErrRestartRequired` 등)는 표지 없는 일반 오류 → 1.

`status` 형식(각 줄 = `"  " + textwidth.PadRight(label, 11) + 값`; 골든 `ok` 와 글자 그대로):

```
masil 알림 데몬
  자동 시작  등록됨 · launchd /Users/sam/Library/LaunchAgents/com.patrache.masil.daemon.plist
  실행       실행 중
  계정       @sam · http://localhost:8787
  알림       켜짐
  동기화     11:04 저장 · seq 1041
```
- 미등록 `등록 안 됨 — masil daemon install`, 멈춤 `멈춤`, 로그인 안 됨 `로그인 안 됨 — masil login`, 알림 끔 `꺼짐 (config.toml [notify] desktop = false)`, 상태 파일 없음 `기록 없음`.
- 서버 불일치면 마지막에 `  ⚠ 로그인한 서버(<CredServer>)와 데몬이 쓰는 서버(<ConfigServer>)가 달라 알림을 받지 않습니다.` + 다음 줄 `    같은 서버로 다시 로그인하거나 config.toml 의 server 를 맞추세요.` (`ConfigServer` 는 Task 5 의 데몬 쪽 주소 — 셸의 `MASIL_SERVER` 가 아님.)
- `ShellServer != ""` 면 (불일치 줄 다음에) `  ⚠ 이 셸의 MASIL_SERVER(<ShellServer>)는 자동 시작된 데몬에 전달되지 않습니다.`
- `Problems` 가 있으면 `  최근 경고` 다음에 줄마다 `    <줄>`. `asErr == autostart.ErrUnsupported` 면 자동 시작 칸 `이 OS 는 지원하지 않음`.

- [ ] **Step 1: Write the failing tests**

```go
func TestDaemonStatus(t *testing.T) {
	seoul, _ := time.LoadLocation("Asia/Seoul")
	now := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	st := &daemon.State{Version: 1, UserID: "01JA0000000000000000000SAM", Epoch: "E1", LastSeq: 1041, SavedAt: now.Add(-time.Minute)}
	ok := daemon.Report{Running: true, LoggedIn: true, Handle: "sam", CredServer: "http://localhost:8787", ConfigServer: "http://localhost:8787", DesktopEnabled: true, State: st}
	installed := autostart.Status{Installed: true, Running: true, Detail: "launchd /Users/sam/Library/LaunchAgents/com.patrache.masil.daemon.plist"}
	cases := map[string]struct {
		r     daemon.Report
		as    autostart.Status
		asErr error
	}{
		"ok":          {ok, installed, nil},
		"fresh":       {daemon.Report{DesktopEnabled: true, ConfigServer: "http://localhost:8787"}, autostart.Status{}, nil},
		"mismatch":    {withMismatch(ok, "http://127.0.0.1:9999"), installed, nil},
		// logged in via MASIL_SERVER=127.0.0.1:9999 in this shell; the daemon uses the default
		"shell_env":   {withShellServer(withMismatch(ok, "http://127.0.0.1:9999"), "http://127.0.0.1:9999"), installed, nil},
		"desktop_off": {withDesktop(ok, false), installed, nil},
		"stale_exe":   {ok, autostart.Status{Installed: true, Detail: installed.Detail + " · 실행 파일 없음: /opt/homebrew/Cellar/masil/0.1.0/bin/masil"}, nil},
		"unsupported": {ok, autostart.Status{}, autostart.ErrUnsupported},
		"problems":    {withProblems(ok, `time=2026-11-05T11:04:00+09:00 level=WARN msg="session rejected; waiting for new credentials" reason=unauthorized token="msl_…(redacted)"`), installed, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var b strings.Builder
			renderStatus(&b, c.r, c.as, c.asErr, now, seoul)
			golden.RequireEqual(t, b.String())
		})
	}
}

// CA 2.4: usage errors exit 2 (not 1) through core's usage marker.
func TestDaemonUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"bogus"}, {"run", "--nope"}} {
		env, _, stderr := daemonTestEnv(t)
		if err := daemonCmd(t.Context(), env, args); !errors.Is(err, errUsage) { t.Fatalf("%v: err = %v, want errUsage", args, err) }
		if !strings.Contains(stderr.String(), "사용법: masil daemon run|install|uninstall|status") { t.Fatal(stderr.String()) }
		env, _, _ = daemonTestEnv(t)
		if code := run(t.Context(), append([]string{"daemon"}, args...), env); code != 2 { t.Fatalf("%v: exit code %d, want 2", args, code) }
	}
	stubDaemonFns(t)
	executableFn = func() (string, error) { return "/var/folders/x/T/go-build99/b001/exe/masil", nil }
	env, _, _ := daemonTestEnv(t)
	if code := run(t.Context(), []string{"daemon", "install"}, env); code != 1 { t.Fatalf("runtime failure exit code %d, want 1", code) }
}

func TestDaemonRunAlreadyRunningExitsZero(t *testing.T) {
	stubDaemonFns(t) // restores every *Fn seed on cleanup
	daemonRunFn = func(context.Context, daemon.Options) error { return instance.ErrAlreadyRunning }
	env, stdout, _ := daemonTestEnv(t)
	if err := daemonCmd(t.Context(), env, []string{"run", "--foreground"}); err != nil { t.Fatal(err) }
	if !strings.Contains(stdout.String(), "데몬이 이미 실행 중입니다.") { t.Fatal(stdout.String()) }
}

// Upgrade path: a replaced binary must exit NON-zero so KeepAlive{SuccessfulExit:false} /
// Restart=on-failure relaunch the new version.
func TestDaemonRunRestartExitsNonZero(t *testing.T) {
	stubDaemonFns(t)
	daemonRunFn = func(context.Context, daemon.Options) error { return daemon.ErrRestartRequired }
	env, _, stderr := daemonTestEnv(t)
	if err := daemonCmd(t.Context(), env, []string{"run", "--foreground"}); !errors.Is(err, daemon.ErrRestartRequired) { t.Fatalf("err = %v", err) }
	if !strings.Contains(stderr.String(), "실행 파일이 바뀌어 종료합니다") { t.Fatal(stderr.String()) }
}

func TestDaemonInstall(t *testing.T) {
	stubDaemonFns(t)
	t.Setenv("MASIL_SERVER", "")
	var gotExe string
	autostartInstallFn = func(exe string, _ config.Paths) error { gotExe = exe; return nil }
	executableFn = func() (string, error) { return "/var/folders/x/T/go-build99/b001/exe/masil", nil }
	env, _, _ := daemonTestEnv(t)
	if err := daemonCmd(t.Context(), env, []string{"install"}); err == nil || !strings.Contains(err.Error(), "go run") {
		t.Fatalf("go-build path must be refused: %v", err)
	}
	executableFn = func() (string, error) { return "/usr/local/bin/masil", nil }
	env, stdout, _ := daemonTestEnv(t) // not logged in, config.toml server unset
	if err := daemonCmd(t.Context(), env, []string{"install"}); err != nil { t.Fatal(err) }
	if gotExe != "/usr/local/bin/masil" || !strings.Contains(stdout.String(), "'masil login' 뒤부터 알림이 옵니다") {
		t.Fatalf("exe=%q out=%q", gotExe, stdout.String())
	}
	if strings.Contains(stdout.String(), "MASIL_SERVER") { t.Fatal("no shell override -> no warning") }

	// Review Focus 1: the shell override will not reach launchd/systemd.
	t.Setenv("MASIL_SERVER", "http://127.0.0.1:9999")
	env, stdout, _ = daemonTestEnv(t)
	if err := daemonCmd(t.Context(), env, []string{"install"}); err != nil { t.Fatal(err) }
	if !strings.Contains(stdout.String(), "셸의 MASIL_SERVER(http://127.0.0.1:9999)는 자동 시작된 데몬에 전달되지 않습니다") ||
		!strings.Contains(stdout.String(), "데몬은 http://localhost:8787 에 접속합니다") {
		t.Fatalf("missing MASIL_SERVER warning: %q", stdout.String())
	}
}
```

헬퍼(`cmd_daemon_test.go` 안, 같은 `main` 패키지의 core·sns-fun 시험과 이름이 겹치지 않게 `daemon` 접두사): `daemonTestEnv(t) (*Env, *bytes.Buffer, *bytes.Buffer)` — `MASIL_CONFIG_DIR` 를 임시 디렉터리로, `config.Load` 한 `Env`; `stubDaemonFns(t)` — 모든 `*Fn` 시드를 저장했다가 정리 때 되돌림; `withMismatch(r, credServer)`(`CredServer`·`ServerMismatch=true`), `withShellServer(r, s)`, `withDesktop(r, on)`, `withProblems(r, lines...)`.

`internal/daemon/detach_test.go`: Windows 가 아니면 `Detach` 가 `(false, nil)`; Windows 에서 `t.Setenv("MASIL_DAEMON_CHILD", "1")` 이면 `(false, nil)`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/masil/ -run TestDaemon -v`
Expected: FAIL — `undefined: renderStatus`

- [ ] **Step 3: Implement `cmd/masil/cmd_daemon.go` and `internal/daemon/detach_*.go`**

- [ ] **Step 4: Generate goldens, inspect, run**

Run: `go test ./cmd/masil/ -run TestDaemonStatus -update && cat cmd/masil/testdata/TestDaemonStatus/ok.golden cmd/masil/testdata/TestDaemonStatus/mismatch.golden cmd/masil/testdata/TestDaemonStatus/shell_env.golden`
Expected: `ok.golden` 이 위 예시와 글자 그대로 같고(서울 11:04), `shell_env.golden` 에 `데몬이 쓰는 서버(http://localhost:8787)` 불일치 두 줄과 `이 셸의 MASIL_SERVER(http://127.0.0.1:9999)` 줄이 있고, 어떤 골든에도 `msl_test` 가 없다(`grep -r msl_test cmd/masil/testdata` 출력 없음).
Run: `go test ./cmd/masil/ ./internal/daemon/ -v -race && GOOS=windows go vet ./cmd/masil/ ./internal/daemon/`
Expected: PASS, vet 출력 없음

- [ ] **Step 5: Manual smoke on macOS (실제 launchd, 로컬 전용·되돌릴 수 있음)**

Run: `go build -o /tmp/masil-smoke/masil ./cmd/masil && /tmp/masil-smoke/masil daemon install && /tmp/masil-smoke/masil daemon status && launchctl print gui/$(id -u)/com.patrache.masil.daemon | grep -m1 'state =' && /tmp/masil-smoke/masil daemon uninstall && /tmp/masil-smoke/masil daemon status`
Expected: `자동 시작을 등록했습니다: launchd …plist`, 첫 `status` 에 `등록됨`·`실행 중`, `state = running`, 해제 뒤 `status` 에 `등록 안 됨 — masil daemon install`·`멈춤`. (스모크 뒤 `/tmp/masil-smoke` 는 지운다.)
업그레이드 갈아타기 확인(같은 스모크 안, `uninstall` 전에): `P1=$(launchctl print gui/$(id -u)/com.patrache.masil.daemon | awk '/pid =/{print $3; exit}') && go build -o /tmp/masil-smoke/masil.new ./cmd/masil && mv /tmp/masil-smoke/masil.new /tmp/masil-smoke/masil && sleep 15 && launchctl print gui/$(id -u)/com.patrache.masil.daemon | awk '/pid =/{print $3; exit}' && grep -c 'executable replaced; exiting for restart' ~/.config/masil/logs/daemon.log`
Expected: 두 번째 pid 가 `P1` 과 다르고(launchd 가 새 바이너리로 다시 띄움), 로그 줄 수 ≥ 1. (스모크 셸에서 `MASIL_CONFIG_DIR` 를 바꿨다면 그 디렉터리의 `logs/daemon.log`.)

- [ ] **Step 6: Commit**

```bash
git add cmd/masil/cmd_daemon.go cmd/masil/cmd_daemon_test.go cmd/masil/testdata/TestDaemonStatus internal/daemon/detach_windows.go internal/daemon/detach_other.go internal/daemon/detach_test.go
git commit -m "feat(cmd): masil daemon run/install/uninstall/status"
```

---

### Task 9: goreleaser 설정

**Files:**
- Create: `.goreleaser.yaml`
- Modify: `.gitignore` (없으면 만들고, `dist/` 한 줄이 없을 때만 추가)

**Interfaces:**
- Consumes: `cmd/masil`(진입점), ldflags 대상 `github.com/patrache/masil/internal/version.Version|Commit|Date`, `github.com/patrache/masil/internal/config.DefaultServer`
- Produces: 릴리스 산출물 이름(Task 10·11·12 가 의존) — `masil_darwin_amd64.tar.gz`, `masil_darwin_arm64.tar.gz`, `masil_linux_amd64.tar.gz`, `masil_linux_arm64.tar.gz`, `masil_windows_amd64.zip`, `masil_windows_arm64.zip`(각 아카이브 최상위에 `masil`/`masil.exe` + `README.md`), `checksums.txt`(sha256, `<hex>  <파일명>`). 이름에 버전이 없으므로 `releases/latest/download/<이름>` 이 항상 최신을 가리킨다.

설정 요지(goreleaser v2.19.0 문서로 확인한 키):
```yaml
version: 2
project_name: masil
before:
  hooks: ["go mod download"]          # never `go mod tidy` (core owns go.mod)
builds:
  - id: masil
    main: ./cmd/masil
    binary: masil
    env: [CGO_ENABLED=0]
    goos: [darwin, linux, windows]
    goarch: [amd64, arm64]
    flags: [-trimpath]
    mod_timestamp: "{{ .CommitTimestamp }}"
    ldflags:
      - -s -w
      - -X github.com/patrache/masil/internal/version.Version={{ .Version }}
      - -X github.com/patrache/masil/internal/version.Commit={{ .ShortCommit }}
      - -X github.com/patrache/masil/internal/version.Date={{ .Date }}
      - -X github.com/patrache/masil/internal/config.DefaultServer={{ envOrDefault "MASIL_DEFAULT_SERVER" "http://localhost:8787" }}
archives:
  - ids: [masil]
    name_template: "masil_{{ .Os }}_{{ .Arch }}"
    formats: [tar.gz]
    format_overrides: [{ goos: windows, formats: [zip] }]
    files: [README.md]
checksum: { name_template: checksums.txt, algorithm: sha256 }
snapshot: { version_template: "{{ incpatch .Version }}-snapshot" }
changelog: { use: git, sort: asc, filters: { exclude: ["^docs:", "^test:", "^chore:"] } }
release:
  github: { owner: patrache, name: masil }
  prerelease: auto
  draft: false
```
(라이선스 파일은 공개 전환 때 오너가 정하므로 `files` 에 넣지 않는다.)

- [ ] **Step 1: Write the failing check**

Run: `go run github.com/goreleaser/goreleaser/v2@v2.19.0 check`
Expected: FAIL — 설정 파일 없음(`could not find a config file` 류). (goreleaser v2.19.0 의 go.mod 는 `go 1.27.3` 이라 `GOTOOLCHAIN=auto` 가 툴체인을 한 번 내려받는다 — 정상.)

- [ ] **Step 2: Write `.goreleaser.yaml` (위 요지) and add `dist/` to `.gitignore`**

- [ ] **Step 3: Validate config**

Run: `go run github.com/goreleaser/goreleaser/v2@v2.19.0 check`
Expected: `1 configuration file(s) validated`, 경고(deprecated) 없음

- [ ] **Step 4: Snapshot build with an https default and verify artifacts (Review Focus 2)**

Run: `MASIL_DEFAULT_SERVER=https://masil.example.workers.dev go run github.com/goreleaser/goreleaser/v2@v2.19.0 release --snapshot --clean && ls dist/*.tar.gz dist/*.zip dist/checksums.txt && go version -m dist/masil_linux_amd64*/masil | grep -E 'CGO_ENABLED=0|DefaultServer=https://masil.example.workers.dev' && unzip -l dist/masil_windows_amd64.zip | grep -E 'masil.exe|README.md' && X=$(mktemp -d) && tar -xzf dist/masil_darwin_arm64.tar.gz -C "$X" && "$X/masil" version`
Expected: 아카이브 6개 + `checksums.txt`, `go version -m` 이 `CGO_ENABLED=0` 과 `-X github.com/patrache/masil/internal/config.DefaultServer=https://masil.example.workers.dev` 를 보여 줌, zip 안에 `masil.exe`·`README.md`, 마지막 `masil version` 출력에 `0.0.1-snapshot`(darwin/arm64 기기 기준).

- [ ] **Step 5: Commit**

```bash
git add .goreleaser.yaml .gitignore
git commit -m "build: goreleaser config for darwin/linux/windows amd64/arm64"
```

---

### Task 10: 설치 스크립트 (`install.sh`, `install.ps1`)

**Files:**
- Create: `scripts/install.sh`(POSIX sh), `scripts/install.ps1`

**Interfaces:**
- Consumes: Task 9 산출물 이름과 `checksums.txt` 형식
- Produces: 사용법(README·release 스모크가 의존)
  - `curl -fsSL https://raw.githubusercontent.com/patrache/masil/main/scripts/install.sh | sh`
  - `irm https://raw.githubusercontent.com/patrache/masil/main/scripts/install.ps1 | iex`
  - 환경변수: `MASIL_VERSION`(예 `0.1.0`, 없으면 최신), `MASIL_INSTALL_DIR`(sh 기본 `$HOME/.local/bin`, ps1 기본 `$env:LOCALAPPDATA\Programs\masil`), `MASIL_DOWNLOAD_BASE`(기본: 버전 지정 시 `https://github.com/patrache/masil/releases/download/v$MASIL_VERSION`, 아니면 `https://github.com/patrache/masil/releases/latest/download`; `file://` 도 허용 — 로컬 시험용), sh 만 `MASIL_OS`/`MASIL_ARCH`(감지 덮어쓰기, 시험용).

`install.sh` 동작: `uname -s`(Darwin→darwin, Linux→linux, 그 외 → `지원하지 않는 OS: <값> — Windows 는 install.ps1 을 쓰세요` 종료 1), `uname -m`(x86_64|amd64→amd64, arm64|aarch64→arm64, 그 외 종료 1), `curl -fsSL`(없으면 `wget -qO`), 임시 디렉터리(`trap` 으로 정리)에 아카이브·`checksums.txt` 받기, `sha256sum` 또는 `shasum -a 256` 으로 검증(불일치 → `체크섬 불일치: <파일>` 종료 1; 받기 실패 → `내려받기 실패: <URL> (레포가 아직 비공개이거나 버전이 없습니다)` 종료 1), 풀어서 `install -m 0755` 로 설치, 완료 줄 `masil 설치 완료: <경로> (<아카이브 이름>)`, PATH 에 없으면 `export PATH="<dir>:$PATH"` 안내, 다음 단계 `masil signup` / `masil daemon install` 안내. 설치 경로에 이미 `masil` 이 있었으면(=업그레이드) 대신 `알림 데몬을 자동 시작으로 등록해 두었다면 새 버전으로 곧 다시 뜹니다 — 확인: masil daemon status (안 바뀌면 masil daemon install 을 다시 실행)` 를 출력한다(데몬은 실행 파일 교체를 감지해 종료 코드 1 로 나가고 launchd/systemd 가 다시 띄움 — Global Constraints). sudo 를 부르지 않는다. `install.ps1` 은 같은 흐름(`RuntimeInformation.OSArchitecture` X64/Arm64, `Invoke-WebRequest`, `Get-FileHash -Algorithm SHA256`, 임시 디렉터리에 `Expand-Archive -Force`, 사용자 PATH 에 없으면 `[Environment]::SetEnvironmentVariable('Path', …, 'User')`), 실패 시 `exit 1`. 실행 중인 `masil.exe`(데몬)는 Windows 가 덮어쓰기를 막으므로, 기존 파일이 있으면 먼저 `masil.exe.old` 로 이름을 바꾼 뒤(실행 중이어도 이름 바꾸기는 됨; 이전 `.old` 는 지우기를 시도하고 실패하면 둠) 새 파일을 복사하고, `실행 중인 알림 데몬은 다음 로그인부터 새 버전으로 뜹니다.` 를 출력한다(Windows 는 감독자가 없어 자동 갈아타기 없음).

- [ ] **Step 1: Write the failing check** (Task 9 의 `dist/` 가 있다는 전제)

Run: `T=$(mktemp -d); MASIL_DOWNLOAD_BASE="file://$PWD/dist" MASIL_INSTALL_DIR="$T/bin" sh scripts/install.sh; echo "exit=$?"`
Expected: FAIL — `sh: scripts/install.sh: No such file or directory`, `exit=127`

- [ ] **Step 2: Write `scripts/install.sh` and `scripts/install.ps1`**

- [ ] **Step 3: Verify the happy path, tamper path and unsupported OS**

Run: `sh -n scripts/install.sh && T=$(mktemp -d) && MASIL_DOWNLOAD_BASE="file://$PWD/dist" MASIL_INSTALL_DIR="$T/bin" sh scripts/install.sh && "$T/bin/masil" version`
Expected: `masil 설치 완료: <T>/bin/masil (masil_darwin_arm64.tar.gz)`(기기 아키텍처에 맞는 이름), PATH 안내, `masil version` 이 `0.0.1-snapshot` 포함, 종료 0
Run: `MASIL_DOWNLOAD_BASE="file://$PWD/dist" MASIL_INSTALL_DIR="$T/bin" sh scripts/install.sh | grep '새 버전으로 곧 다시 뜹니다'` (같은 `$T` 에 두 번째 설치 = 업그레이드)
Expected: 그 안내 줄 1개, 종료 0
Run: `C=$(mktemp -d) && cp dist/*.tar.gz dist/*.zip "$C/" && sed 's/^./0/' dist/checksums.txt > "$C/checksums.txt" && MASIL_DOWNLOAD_BASE="file://$C" MASIL_INSTALL_DIR="$(mktemp -d)" sh scripts/install.sh; echo "exit=$?"`
Expected: `체크섬 불일치: masil_darwin_arm64.tar.gz`(기기 아키텍처에 맞는 이름), `exit=1`, 설치 디렉터리에 `masil` 없음
Run: `MASIL_OS=FreeBSD MASIL_DOWNLOAD_BASE="file://$PWD/dist" sh scripts/install.sh; echo "exit=$?"; MASIL_OS=Linux MASIL_ARCH=aarch64 MASIL_DOWNLOAD_BASE="file://$PWD/dist" MASIL_INSTALL_DIR="$(mktemp -d)" sh scripts/install.sh 2>&1 | grep '설치 완료'`
Expected: `지원하지 않는 OS: FreeBSD …`, `exit=1`; 두 번째는 `masil 설치 완료: … (masil_linux_arm64.tar.gz)` — 덮어쓴 OS·아키텍처로 아카이브를 고르고 체크섬까지 통과(스크립트는 설치한 바이너리를 실행하지 않는다).
`install.ps1`: `pwsh` 가 있으면 `pwsh -NoProfile -Command '$null = [System.Management.Automation.Language.Parser]::ParseFile("scripts/install.ps1", [ref]$null, [ref]$e); if ($e) { $e; exit 1 }'` → 출력 없음. 없으면 Task 12 의 Windows 설치 스모크 잡이 검증한다(로컬에 pwsh 를 새로 설치하지 않는다).

- [ ] **Step 4: Commit**

```bash
git add scripts/install.sh scripts/install.ps1
git commit -m "build: checksum-verified install scripts for unix and windows"
```

---

### Task 11: Homebrew tap formula 템플릿

**Files:**
- Create: `packaging/homebrew/masil.rb.tmpl`

**Interfaces:**
- Consumes: Task 9 산출물 이름·`checksums.txt`
- Produces: 자리표시자 — Task 12 의 tap 잡이 `sed` 로 채운다: `__VERSION__`, `__SHA256_DARWIN_ARM64__`, `__SHA256_DARWIN_AMD64__`, `__SHA256_LINUX_ARM64__`, `__SHA256_LINUX_AMD64__`. 결과 파일 경로(tap 레포 안) `Formula/masil.rb`, 설치 명령 `brew install patrache/tap/masil`.

goreleaser 의 `brews` 는 v2.16 부터 폐기(v2.19.0 deprecations 문서 확인)라 쓰지 않고, 계약이 정한 손수 템플릿을 쓴다. 내용 요지: `class Masil < Formula`, `desc "터미널 채팅 + SNS"`, `homepage "https://github.com/patrache/masil"`, `version "__VERSION__"`, `on_macos`/`on_linux` × `on_arm`/`on_intel` 블록마다 `url "https://github.com/patrache/masil/releases/download/v__VERSION__/masil_<os>_<arch>.tar.gz"` + `sha256 "…"`, `def install; bin.install "masil"; end`, `def caveats` = `알림 데몬 자동 시작: masil daemon install` + `brew upgrade 뒤 등록된 데몬은 새 버전으로 스스로 다시 뜹니다 — 'masil daemon status' 로 확인하고, 안 바뀌었으면 'masil daemon install' 을 다시 실행하세요.` + `제거 전에 'masil daemon uninstall' 을 먼저 실행하세요.`, `test do assert_match version.to_s, shell_output("#{bin}/masil version") end`. `license` 줄은 공개 전환 때 오너가 정할 때까지 넣지 않는다. `service do` 는 쓰지 않는다(`masil daemon install` 과 이중 등록 방지).

- [ ] **Step 1: Write the failing check**

Run: `sed -e 's/__VERSION__/0.0.1/g' packaging/homebrew/masil.rb.tmpl`
Expected: FAIL — `No such file or directory`

- [ ] **Step 2: Write `packaging/homebrew/masil.rb.tmpl`**

- [ ] **Step 3: Render from the snapshot checksums (Task 12 와 같은 sed 식) and syntax-check**

Run:
```bash
sha() { awk -v f="masil_$1.tar.gz" '$2==f{print $1}' dist/checksums.txt; }
sed -e "s/__VERSION__/0.0.1/g" \
    -e "s/__SHA256_DARWIN_ARM64__/$(sha darwin_arm64)/" -e "s/__SHA256_DARWIN_AMD64__/$(sha darwin_amd64)/" \
    -e "s/__SHA256_LINUX_ARM64__/$(sha linux_arm64)/"   -e "s/__SHA256_LINUX_AMD64__/$(sha linux_amd64)/" \
    packaging/homebrew/masil.rb.tmpl > /tmp/masil.rb && ruby -c /tmp/masil.rb && ! grep -n '__[A-Z0-9_]*__' /tmp/masil.rb && grep -c 'sha256 "[0-9a-f]\{64\}"' /tmp/masil.rb
```
Expected: `Syntax OK`, 남은 자리표시자 없음, `4`

- [ ] **Step 4: Owner-approved tap setup (실행자는 하지 않음 — 오너 윤허 후 오너가 실행)**

계획 문서에만 남긴다: `gh repo create patrache/homebrew-tap --public --description "Homebrew tap for masil"`(레포 공개 전환 시점), tap 레포 `contents: write` 만 가진 fine-grained PAT 를 `gh secret set HOMEBREW_TAP_TOKEN -R patrache/masil`, 배포 주소가 정해진 뒤 `gh variable set MASIL_DEFAULT_SERVER -R patrache/masil --body https://masil.<account>.workers.dev`, 레포 공개 후 `gh variable set MASIL_PUBLIC -R patrache/masil --body true`. 이 단계가 끝나기 전에는 Task 12 의 tap·스모크 잡이 알림만 남기고 건너뛴다.

- [ ] **Step 5: Commit**

```bash
git add packaging/homebrew/masil.rb.tmpl
git commit -m "build: homebrew tap formula template"
```

---

### Task 12: GitHub Actions — 서버 CI 와 태그 전용 릴리스

**Files:**
- Create: `.github/workflows/server-ci.yml`, `.github/workflows/release.yml`

> **SHA 고정 (보류 항목 2):** 아래에 태그로 적힌 액션(`actions/checkout@v7.0.1`, `actions/setup-node@v7.1.0`, `actions/setup-go@v7.0.0`, `goreleaser/goreleaser-action@v7.3.0`, `actions/upload-artifact@v7.0.2`, `actions/download-artifact@v8.0.2`)은 버전을 가리킬 뿐이다. 실제 `uses:` 는 `owner/repo@<40자 커밋 SHA> # vX.Y.Z` 로 쓴다(SHA 는 `gh api` 로 조회, 기억으로 쓰지 않음). 태그로 쓰면 `TestWorkflowActionsPinnedToSHA` 가 실패한다.

**Interfaces:**
- Consumes: Task 9~11 산출물, `server/` 의 `npm ci`·`npx tsc --noEmit`·`npm test`(server-architecture 8장; `package-lock.json` 은 server 계획이 커밋), core `ci.yml`(Go 시험은 거기서 — 이 계획은 고치지 않음)
- Produces: 저장소 설정 이름(README·Task 11 이 의존) — 비밀 `HOMEBREW_TAP_TOKEN`, 변수 `MASIL_DEFAULT_SERVER`, `MASIL_PUBLIC`.

`server-ci.yml`: `on: push(branches: [main], paths: [server/**, .github/workflows/server-ci.yml])` + `pull_request`(같은 paths), `permissions: contents: read`, 잡 하나(ubuntu-latest, `defaults.run.working-directory: server`): `actions/checkout@v7.0.1` → `actions/setup-node@v7.1.0`(`node-version: 24`, `cache: npm`, `cache-dependency-path: server/package-lock.json`) → `npm ci` → `npx tsc --noEmit` → `npm test`.

`release.yml` — **오너가 `v*` 태그를 푸시할 때만** 돈다(`workflow_dispatch`·`push: branches` 없음, 파일 첫 주석에 명시):
```yaml
# Runs ONLY when the owner pushes a version tag (e.g. `git tag v0.1.0 && git push origin v0.1.0`).
on:
  push:
    tags: ["v*.*.*"]
permissions:
  contents: read
concurrency:
  group: release-${{ github.ref }}   # block style: `${{ }}` inside a `{ … }` flow mapping is a YAML parse error
  cancel-in-progress: false
```
잡:
1. `go-test`(matrix ubuntu/macos/windows-latest): checkout → `actions/setup-go@v7.0.0`(`go-version-file: go.mod`) → `go vet ./...` → `go test ./...`.
2. `server-test`: server-ci 와 같은 단계.
3. `goreleaser`(needs 1·2, `permissions: contents: write`, env `MASIL_DEFAULT_SERVER: ${{ vars.MASIL_DEFAULT_SERVER }}`): checkout(`fetch-depth: 0`) → setup-go → **가드 단계**:
   ```bash
   case "$MASIL_DEFAULT_SERVER" in
     https://*) ;;
     *) echo "::error::repo variable MASIL_DEFAULT_SERVER must be an https:// URL (owner sets it after the approved deploy)"; exit 1 ;;
   esac
   ```
   → `goreleaser/goreleaser-action@v7.3.0`(`distribution: goreleaser`, `version: v2.19.0`, `args: release --clean`, env `GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}`) → `actions/upload-artifact@v7.0.2` 로 `dist/checksums.txt` 업로드(이름 `checksums`; tap 잡은 `actions/download-artifact@v8.0.2`).
4. `homebrew-tap`(needs 3, `if: ${{ !contains(github.ref_name, '-') }}` — 사전 릴리스 제외, env `TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}`): 첫 단계 `[ -n "$TAP_TOKEN" ] || { echo "::notice::HOMEBREW_TAP_TOKEN not set; skipping tap update"; echo skip=1 >> "$GITHUB_OUTPUT"; }` → (skip 아니면) masil checkout, checksums 내려받기, `actions/checkout@v7.0.1`(`repository: patrache/homebrew-tap`, `token: ${{ env.TAP_TOKEN }}`, `path: tap`), Task 11 Step 3 의 `sed` 식으로 `tap/Formula/masil.rb` 생성(`VERSION=${GITHUB_REF_NAME#v}`, `mkdir -p tap/Formula`), 그다음 커밋 단계(러너에는 git 사용자 정보가 없고, 같은 태그 재실행이면 바뀐 게 없으므로 둘 다 처리):
   ```bash
   git -C tap config user.name "github-actions[bot]"
   git -C tap config user.email "41898282+github-actions[bot]@users.noreply.github.com"
   git -C tap add Formula/masil.rb
   if git -C tap diff --cached --quiet; then
     echo "::notice::Formula/masil.rb unchanged for ${VERSION}; nothing to push"
   else
     git -C tap commit -m "masil ${VERSION}"
     git -C tap push
   fi
   ```
5. `install-smoke`(`needs: [goreleaser, homebrew-tap]` — 4 가 건너뛰어도 돌도록 `if: ${{ always() && needs.goreleaser.result == 'success' && vars.MASIL_PUBLIC == 'true' && !contains(github.ref_name, '-') }}`, matrix ubuntu/macos/windows-latest): unix 는 `MASIL_VERSION=${GITHUB_REF_NAME#v} sh scripts/install.sh` 후 `~/.local/bin/masil version`, windows 는 `pwsh -File scripts/install.ps1` 후 `& "$env:LOCALAPPDATA\Programs\masil\masil.exe" version`. 둘 다 출력에 `${GITHUB_REF_NAME#v}` 포함 확인.

- [ ] **Step 1: Write the failing lint**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/release.yml .github/workflows/server-ci.yml`
Expected: FAIL — 파일 없음 오류

- [ ] **Step 2: Write both workflow files**

- [ ] **Step 3: Lint and check the trigger + guard**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/release.yml .github/workflows/server-ci.yml && echo LINT_OK`
Expected: `LINT_OK` (오류 없음)
Run: `ruby -ryaml -e 'w=YAML.load_file(".github/workflows/release.yml"); on=w["on"]||w[true]; abort("bad trigger: #{on.inspect}") unless on.keys==["push"] && on["push"].keys==["tags"]; c=w["concurrency"]; abort("bad concurrency: #{c.inspect}") unless c.is_a?(Hash) && c["group"]=="release-${{ github.ref }}" && c["cancel-in-progress"]==false; puts "TAG_ONLY"'`
Expected: `TAG_ONLY` (YAML 이 깨졌으면 Ruby 가 `Psych::SyntaxError` 로 멈춤)
Run: `grep -nF 'github-actions[bot]@users.noreply.github.com' .github/workflows/release.yml && grep -nF 'diff --cached --quiet' .github/workflows/release.yml && ! grep -nF 'commit -am' .github/workflows/release.yml`
Expected: 앞의 두 grep 이 각 1줄, `commit -am` 없음(tap 커밋은 사용자 정보 설정 + 변경 없을 때 건너뛰기)
Run: `for v in "" "http://localhost:8787" "https://masil.example.workers.dev"; do MASIL_DEFAULT_SERVER="$v" sh -c 'case "$MASIL_DEFAULT_SERVER" in https://*) ;; *) exit 1 ;; esac' && echo "ok:$v" || echo "blocked:$v"; done`
Expected: `blocked:`, `blocked:http://localhost:8787`, `ok:https://masil.example.workers.dev` (워크플로의 가드와 같은 식)

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/server-ci.yml .github/workflows/release.yml
git commit -m "ci: server vitest workflow and tag-only release workflow"
```

---

### Task 13: README (설치·사용법)

**Files:**
- Create: `README.md`

**Interfaces:**
- Consumes: Task 8 명령·출력, Task 10 설치 명령·환경변수, Task 11 `brew install patrache/tap/masil`, 계약 3.1·3.3(설정 경로·키), 5.2.1(모드 중단점), 5.5(기본 키), server-architecture 8장(로컬 개발 명령), sns-fun(05) Task 20 의 `masil admin` 하위 명령·플래그·오류 문구(`admincli.Usage`), data-model 2장(운영자 지정)
- Produces: 사용자 문서(goreleaser 아카이브에 포함)

한국어. 섹션과 반드시 들어갈 사실:
1. **masil** 한 줄 소개(터미널 채팅 + SNS, 설계서 1장) + "개발 중 비공개 — 공개 전환 전에는 설치 스크립트·Homebrew 가 동작하지 않음".
2. **설치**: `curl -fsSL …/install.sh | sh`, Windows `irm …/install.ps1 | iex`, `brew install patrache/tap/masil`, 직접 내려받기(Releases 의 `masil_<os>_<arch>` + `checksums.txt` 검증), 환경변수 `MASIL_VERSION`·`MASIL_INSTALL_DIR`.
3. **시작하기**: `masil signup`, `masil login`, `masil`, `masil join <코드>`.
4. **화면 모드**: 지휘소 ≥200×50, 표준 ≥100×40, 컴팩트 ≥100칸·40줄 미만, 위젯 44~99칸·24줄 이상, 초소형 그 밖, `ctrl+w 1~5` 고정·`ctrl+w 0` 자동.
5. **주요 키**: `ctrl+k` 이동, `ctrl+f` 검색, `ctrl+n` 알림, `?` 도움말, `f` 피드, `t`/`enter` 스레드, `i` 입력, `esc` 뒤로, `ctrl+c` 두 번 종료, j/k·화살표.
6. **알림 데몬**: `masil daemon install`(macOS launchd·Linux systemd --user·Windows 시작프로그램), `masil daemon status`(출력 예 = Task 8 `ok` 골든), `masil daemon uninstall`, `masil daemon run --foreground --verbose`(문제 확인), TUI 가 떠 있으면 데몬은 알리지 않음, 집중 모드 중엔 보류 후 끝날 때 요약 1번, 로그 `~/.config/masil/logs/daemon.log`. **업그레이드**: `brew upgrade`·설치 스크립트 재실행 뒤 macOS·Linux 의 등록된 데몬은 실행 파일 교체를 감지해 새 버전으로 스스로 다시 뜬다(`masil daemon status` 로 확인, 안 되면 `masil daemon install` 재실행 — 재등록이 실행 중 데몬을 다시 띄운다). Windows 는 다음 로그인부터 새 버전.
7. **설정**: `~/.config/masil/`(모든 OS, `MASIL_CONFIG_DIR` 로 변경), `config.toml` 의 `server`·`theme`·`[notify] desktop`, `MASIL_SERVER` 우선순위 — **자동 시작된 데몬은 셸 환경변수를 보지 않으므로 서버를 바꿀 땐 `config.toml` 의 `server` 를 쓸 것**(Review Focus 1; `masil daemon install`·`status` 가 셸의 `MASIL_SERVER` 와 데몬 쪽 주소가 다르면 경고한다).
8. **제거**: `masil daemon uninstall` 을 **먼저**, 그다음 `brew uninstall masil` 또는 바이너리·`~/.config/masil` 삭제.
9. **운영자(전역 admin)** — 설계서 2장 "첫 출시부터"(공개 가입·공개 이미지라 불법 콘텐츠 대응 수단이 필요): 사용법 한 줄 `masil admin reports|report <id>|resolve <id> <resolution>|remove <type> <id>|suspend @h [--for 7d]|unsuspend @h|users|actions` 과 하위 명령별 예시 — `masil admin reports [--status open|resolved|dismissed] [--type <대상 종류>]`, `masil admin report <id>`, `masil admin resolve <id> content_removed|user_suspended|no_action [--note <메모>]`, `masil admin remove message|post|story|guestbook_entry|upload|space <id> --reason <사유> [--report <신고 id>]`, `masil admin suspend @handle [--for 7d|12h|30m] --reason <사유> [--report <신고 id>]`(`--for` 없으면 무기한), `masil admin unsuspend @handle`, `masil admin users [--q <검색어>] [--suspended true|false]`, `masil admin actions [--target-type <종류>] [--target <id>]`(05 Task 20 표와 글자 그대로; `remove`·`suspend` 는 `--reason` 필수). **숨김(Hidden) 명령이라 `masil help` 목록에 나오지 않는다**고 명시. 운영자가 아니면 `운영자 권한이 없습니다.` 가 나온다. **운영자 지정**: 승격 API 는 없고(data-model 2장) 오너가 D1 에 직접 `UPDATE users SET role = 'admin' WHERE handle = '<handle>';` 를 한 번 실행한다. 로컬 개발에서는 `cd server && npx wrangler d1 execute DB --local --command "UPDATE users SET role = 'admin' WHERE handle = '<handle>'"`. 운영(원격) D1 에서의 실행은 **오너 승인 후 오너가** 한다고만 적고 `--remote` 명령 줄은 적지 않는다(Global Constraints — 되돌리기 어려운 원격 작업).
10. **문제 해결**: macOS 알림이 안 뜨면 시스템 설정 → 알림 → "스크립트 편집기" 허용(beeep 가 `osascript` 사용), Linux 는 알림 데몬(D-Bus) 필요, iTerm2 "Ambiguous characters are double-width" 끄기(계약 8.2), `status` 의 서버 불일치 경고.
11. **로컬 개발**: `cd server && npm install && npx wrangler d1 migrations apply DB --local && npx wrangler dev --test-scheduled`, `MASIL_SERVER=http://localhost:8787 go run ./cmd/masil`, `go test ./...`, `cd server && npm test`. 배포(`wrangler deploy`)는 오너 승인 사항이라 문서에 명령을 적지 않는다.
12. **라이선스**: 공개 전환 때 결정.

- [ ] **Step 1: Write the failing check**

Run: `for s in 'install.sh | sh' 'install.ps1 | iex' 'brew install patrache/tap/masil' 'masil daemon install' 'masil daemon uninstall' 'masil daemon status' 'MASIL_CONFIG_DIR' 'MASIL_SERVER' 'wrangler dev' 'masil admin reports' 'masil admin suspend @handle' 'unsuspend' 'masil help' "SET role = 'admin'"; do grep -qF "$s" README.md || echo "MISSING: $s"; done`
Expected: FAIL — `grep: README.md: No such file or directory` 와 MISSING 14줄

- [ ] **Step 2: Write `README.md`**

- [ ] **Step 3: Re-run the check and secret scan**

Run: 위 Step 1 명령 + `grep -nE 'msl_[A-Za-z0-9]{6,}|wrangler deploy|--remote' README.md`
Expected: MISSING 없음, 두 번째 grep 출력 없음(토큰·배포 명령·원격 D1 명령이 README 에 없음)
Run: `go build -o /tmp/masil-readme ./cmd/masil && /tmp/masil-readme help | grep -c admin; rm -f /tmp/masil-readme`
Expected: `0` — README 의 "help 에 나오지 않음" 문장이 실제 동작과 맞다(05 Task 20 이 `Hidden: true` 로 등록).

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: README with install, usage, daemon and operator guide"
```

---

### Task 14: 관문 G1 — M1 기반 (계정·인증·세션·실시간 소켓·TUI 뼈대)

**실행 시점**: 이 계획에서 가장 먼저 — "마일스톤 관문" 표 G1 행의 선행(서버 01 Task 1~27·02 Task 2, 클라이언트 03 Task 1~35)이 끝났을 때. 통과 전에는 01 Task 28·04(chat) 를 배정하지 않는다. 원격 명령(`--remote`, `wrangler deploy`) 금지.

**Files:**
- Create: `internal/daemon/e2egate/doc.go`(`// Package e2egate holds the tag-gated local milestone gates (plan 06 Task 14–16).` + `package e2egate` — 빌드 태그 없음, 태그 없는 `go vet ./...` 가 빈 패키지 오류를 내지 않게), `internal/daemon/e2egate/gate_test.go`(`//go:build e2e`, `package e2egate_test`)

**Interfaces:**
- Consumes: `api.New`, `api.WithToken`, `(*api.Client).{Health, Signup, Login, Logout, GetMe}`, `api.{SignupRequest, LoginRequest, AuthResult, IsCode}`, `realtime.{New, Options, ModeFull, ModeNotify, ResumePoint, Event, ErrUnauthorized}`, `(*realtime.Client).{Run, MarkProcessed, Typing}`, `store.Bootstrap`, `store.BootstrapMsg`, `ids.New`. **데몬 패키지(`internal/daemon`)는 import 하지 않는다** — 데몬 Task 1~8 은 G3 뒤에 만든다.
- Produces (e2egate 시험 내부, Task 15·16 이 재사용): `e2eServer(t) string`(`MASIL_E2E_SERVER` 없으면 `t.Skip`), `const e2ePassword = "e2e-pass-1234"`, `type user struct{ API *api.Client; Handle, UserID, Token string }`, `signup(t, srv, prefix string) user`(handle = `prefix + "_" + ids.New()[18:26]` 소문자 — `^[a-z0-9_]{3,20}$` 안), `type stream struct{ client *realtime.Client; … }`, `connect(t, u user, mode realtime.Mode) *stream`, `(s *stream).next(t, typ string, preds ...func(json.RawMessage) bool) realtime.Event`(버퍼에서 아직 안 쓴 첫 일치 이벤트를 10초까지 기다려 소비 — 타입이 다른 이벤트의 도착 순서에 기대지 않음), `(s *stream).wait(t, d time.Duration) error`(`Run` 반환값), `match(path, want string) func(json.RawMessage) bool`(점 경로, 예 `"message.id"`), `must(t, err)`.

- [ ] **Step 1: Start the local server (background)**

Run: `cd server && npm install && npx wrangler d1 migrations apply DB --local && npx wrangler dev --test-scheduled` (백그라운드), 그다음 `curl -s http://localhost:8787/v1/health`
Expected: `{"ok":true}`. (관문 Task 14~17 은 같은 방식으로 서버를 띄운다. 끝나면 자기가 띄운 wrangler dev 만 종료.)

- [ ] **Step 2: Write the gate helpers and the G1 test**

```go
//go:build e2e

package e2egate_test

type stream struct {
	client *realtime.Client
	mu     sync.Mutex
	evs    []realtime.Event
	used   []bool
	done   chan struct{}
	err    error // realtime.Run result, valid after done is closed
}

func connect(t *testing.T, u user, mode realtime.Mode) *stream {
	t.Helper()
	s := &stream{done: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	s.client = realtime.New(realtime.Options{
		API: u.API, DeviceID: ids.New(), Mode: mode, Resume: realtime.ResumePoint{UseDeviceAck: true},
		Logger: slog.New(slog.DiscardHandler),
		OnEvent: func(ev realtime.Event) {
			s.mu.Lock(); s.evs = append(s.evs, ev); s.used = append(s.used, false); s.mu.Unlock()
			if ev.Seq != nil { s.client.MarkProcessed(*ev.Seq) }
		},
	})
	go func() { s.err = s.client.Run(ctx); close(s.done) }()
	t.Cleanup(func() { cancel(); <-s.done })
	return s
}

// M1: account, auth, server-side sessions, realtime socket (full + notify), and the
// REST bootstrap the TUI runs right after `ready` (events.md 5.3 step 1).
func TestGateM1AuthRealtime(t *testing.T) {
	srv := e2eServer(t)
	ctx := t.Context()
	must(t, api.New(srv).Health(ctx))
	a := signup(t, srv, "g1a")
	if me, err := a.API.GetMe(ctx); err != nil || me.Handle != a.Handle { t.Fatalf("me %+v, %v", me, err) }

	// a second device logs in -> its own revocable session
	lr, err := api.New(srv).Login(ctx, api.LoginRequest{Handle: a.Handle, Password: e2ePassword, Client: "tui"}); must(t, err)
	second := user{API: api.New(srv, api.WithToken(lr.Token)), Handle: a.Handle, UserID: a.UserID, Token: lr.Token}
	full := connect(t, second, realtime.ModeFull)
	full.next(t, "ready")
	// every bootstrap endpoint answers; otherwise the TUI status bar stays "✕ 오프라인" (core Task 30)
	if msg, ok := store.Bootstrap(second.API)().(store.BootstrapMsg); !ok || msg.Err != nil || msg.Me == nil { t.Fatalf("bootstrap: %+v", msg) }
	notify := connect(t, a, realtime.ModeNotify)
	notify.next(t, "ready")

	// logout revokes only that session: session.revoked, then the socket ends with ErrUnauthorized (4001)
	must(t, second.API.Logout(ctx))
	full.next(t, "session.revoked", match("reason", "logout"))
	if err := full.wait(t, 10*time.Second); !errors.Is(err, realtime.ErrUnauthorized) { t.Fatalf("revoked socket: %v", err) }
	if _, err := second.API.GetMe(ctx); !api.IsCode(err, "unauthorized") { t.Fatalf("revoked token still works: %v", err) }
	if _, err := a.API.GetMe(ctx); err != nil { t.Fatalf("other session must survive: %v", err) }
	if _, err := api.New(srv).Login(ctx, api.LoginRequest{Handle: a.Handle, Password: "wrong-pass-0000"}); !api.IsCode(err, "invalid_credentials") {
		t.Fatalf("wrong password: %v", err)
	}
}
```
(`signup` 은 `api.New(srv).Signup(ctx, api.SignupRequest{Handle, Password: e2ePassword, Client: "tui"})` 의 `AuthResult` 로 `user{API: api.New(srv, api.WithToken(r.Token)), Handle: r.User.Handle, UserID: r.User.ID, Token: r.Token}` 를 만든다. `match` 는 `ev.Data` 를 `map[string]any` 로 풀어 점 경로를 따라간 값의 `fmt.Sprint` 가 `want` 인지 본다. 시험은 토큰을 출력하지 않는다.)

- [ ] **Step 3: Run the G1 gate test**

Run: `go vet ./internal/daemon/e2egate/ && MASIL_E2E_SERVER=http://localhost:8787 go test -tags e2e ./internal/daemon/e2egate/ -run TestGateM1 -v -count=1`
Expected: vet 출력 없음, `TestGateM1AuthRealtime` PASS. 실패하면 "마일스톤 관문" 규칙 4(어느 확인이 깨졌는지 + 소유 계획·Task 를 보고, 시험은 고치지 않음). `MASIL_E2E_SERVER` 없이 돌리면 SKIP.

- [ ] **Step 4: Manual TUI skeleton check against wrangler dev (tmux, 로컬 전용)**

```bash
S=$(mktemp -d); go build -o "$S/masil" ./cmd/masil; export MASIL_SERVER=http://localhost:8787
tmux new-session -d -s g1 -x 120 -y 40 "MASIL_CONFIG_DIR=$S/a MASIL_SERVER=$MASIL_SERVER $S/masil signup"
# (사람) tmux attach -t g1 → 가입 화면에서 handle g1_<rand>·비밀번호 입력 → 메인 화면, 상태줄 ● 온라인 확인 후 detach
for sz in 200x60 120x40 120x30 80x24 50x20; do tmux resize-window -t g1 -x "${sz%x*}" -y "${sz#*x}"; sleep 1; tmux capture-pane -p -t g1 > "$S/g1-$sz.txt"; done
grep -H -o '지휘소\|표준\|컴팩트\|위젯\|초소형' "$S"/g1-*.txt | sort -u
```
이어서 사람이 120×40 에서: `ctrl+k`(명령 팔레트 열림·퍼지 검색) → `테마` 로 A→B→C→D 차례로 바꿔 매번 캡처(`$S/g1-theme-<A..D>.txt`), `grep '^theme' "$S/a/config.toml"` → 마지막 테마; wrangler dev 를 멈췄다가 다시 띄움(TUI 는 그대로); `ctrl+c` 두 번 종료 뒤 `MASIL_CONFIG_DIR="$S/a" "$S/masil"` 재실행.
Expected: 가입 직후 메인 화면·상태줄 `● 온라인`(= 로그인→`ready`→부트스트랩 성공), 처음 테두리가 이중선(`╔`, 기본 D 레트로 앰버). 크기별 캡처의 모드 pill 이 200×60 `지휘소`, 120×40 `표준`, 120×30 `컴팩트`, 80×24 `위젯`, 50×20 `초소형`(CA 5.2.1 표)이고 어느 캡처에도 테두리가 끊기거나 겹친 줄이 없음. 테마 4종이 즉시 바뀌고 `config.toml` 에 저장됨. 서버를 멈추면 상태줄이 `◌ 연결 중…`/`✕ 오프라인`, 다시 띄우면 TUI 재시작 없이 `● 온라인` 으로 돌아옴(재접속). 재실행 때 로그인 화면 없이 바로 메인(세션 유지), `ls -l "$S/a/credentials.json"` 이 `-rw-------`. 끝나면 `tmux kill-session -t g1`(자기 세션만), `$S` 삭제.

- [ ] **Step 5: Report the gate**

`TestGateM1AuthRealtime` PASS 줄과 Step 4 의 Expected 일치 여부(캡처 파일 목록)를 오케스트레이터에 보고한다. **G1 통과** 보고 뒤에만 오케스트레이터가 01 Task 28 · 04 Task 1~23 을 배정한다.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/e2egate/doc.go internal/daemon/e2egate/gate_test.go
git commit -m "test(e2e): M1 gate — auth, sessions and realtime against wrangler dev"
```

---

### Task 15: 관문 G2 — M2 채팅

**실행 시점**: G1 통과 + 01 Task 28 + 04 Task 1~23 완료 뒤. 통과 전에는 01 Task 29·02 Task 1·3~10·05 Task 1~15·18·19 를 배정하지 않는다. 04 Task 11·12 의 `/poll`·`/status`·`/focus`·`/roll`·신고·차단·투표는 이 관문 합격 조건이 아니다(G4).

**Files:**
- Modify: `internal/daemon/e2egate/gate_test.go`(시험 함수 덧붙이기)

**Interfaces:**
- Consumes: Task 14 헬퍼; `(*api.Client).{SendFriendRequest, AcceptFriendRequest, OpenDM, ListDMs, SendMessage, ReplyThread, GetThread, EditMessage, DeleteMessage, AddReaction, Pin, ListPins, CreateSpace, CreateInvite, AcceptInvite, ListUnreads, MarkRead, SearchMessages}`, `api.{Handle, UserRef, OpenDMRequest, SendMessageRequest, ThreadReplyRequest, ThreadQuery, CreateSpaceRequest, CreateInviteRequest, MessageSearchQuery, Some}`, `(*realtime.Client).Typing`, 이벤트 `friend.request_received`·`presence.updated`·`message.created`·`thread.updated`·`reaction.added`·`message.updated`·`message.pinned`·`message.deleted`·`typing`·`alert`(events.md 4장)
- Produces: 없음(검증)

- [ ] **Step 1: Write the G2 test**

```go
// M2: friends, DM/group DM, spaces/channels/invites, threads, reactions, mentions, edit/delete,
// pins, unread, typing, presence, search — all delivered live (no refresh).
func TestGateM2Chat(t *testing.T) {
	srv := e2eServer(t)
	ctx := t.Context()
	a, b, c := signup(t, srv, "g2a"), signup(t, srv, "g2b"), signup(t, srv, "g2c")
	text := func(body string) api.SendMessageRequest { return api.SendMessageRequest{ClientNonce: ids.New(), Kind: "text", Body: body} }
	bs := connect(t, b, realtime.ModeFull)
	bs.next(t, "ready")

	fr, err := a.API.SendFriendRequest(ctx, api.Handle(b.Handle)); must(t, err)
	bs.next(t, "friend.request_received")
	_, err = b.API.AcceptFriendRequest(ctx, fr.Request.ID); must(t, err)
	as := connect(t, a, realtime.ModeFull)
	as.next(t, "ready")
	bs.next(t, "presence.updated", match("user_id", a.UserID), match("status", "online"))

	dm, _, err := a.API.OpenDM(ctx, api.OpenDMRequest{Users: []api.UserRef{api.Handle(b.Handle)}}); must(t, err)
	m1, _, err := a.API.SendMessage(ctx, dm.ID, text("안녕 밥 👍")); must(t, err)
	bs.next(t, "message.created", match("message.id", m1.ID))
	bs.next(t, "alert", match("alert.kind", "dm"))
	// anti-goal: no DM bombs from strangers (no friendship, no shared space)
	if _, _, err := c.API.OpenDM(ctx, api.OpenDMRequest{Users: []api.UserRef{api.Handle(b.Handle)}}); !api.IsCode(err, "dm_not_allowed") { t.Fatalf("stranger DM: %v", err) }

	sp, err := a.API.CreateSpace(ctx, api.CreateSpaceRequest{Name: "관문방"}); must(t, err)
	inv, err := a.API.CreateInvite(ctx, sp.ID, api.CreateInviteRequest{MaxUses: api.Some(2)}); must(t, err)
	_, err = b.API.AcceptInvite(ctx, inv.Code); must(t, err)
	_, err = c.API.AcceptInvite(ctx, inv.Code); must(t, err)
	gen := sp.DefaultChannelID
	root, _, err := c.API.SendMessage(ctx, gen, text("@"+b.Handle+" 회의 몇 시?")); must(t, err)
	bs.next(t, "message.created", match("message.id", root.ID))
	bs.next(t, "alert", match("alert.kind", "mention"))
	reply, _, err := b.API.ReplyThread(ctx, root.ID, api.ThreadReplyRequest{ClientNonce: ids.New(), Kind: "text", Body: "3시요"}); must(t, err)
	as.next(t, "thread.updated", match("root_id", root.ID))
	if tw, err := c.API.GetThread(ctx, root.ID, api.ThreadQuery{}); err != nil || len(tw.Items) != 1 { t.Fatalf("thread %+v, %v", tw, err) }
	must(t, a.API.AddReaction(ctx, root.ID, "👍"))
	bs.next(t, "reaction.added", match("message_id", root.ID), match("emoji", "👍"))
	_, err = c.API.EditMessage(ctx, root.ID, "@"+b.Handle+" 회의 4시로 바꿀까요?"); must(t, err)
	bs.next(t, "message.updated", match("message.id", root.ID))
	must(t, a.API.Pin(ctx, root.ID))
	bs.next(t, "message.pinned", match("message_id", root.ID))
	if pins, err := c.API.ListPins(ctx, gen); err != nil || len(pins) != 1 { t.Fatalf("pins %d, %v", len(pins), err) }
	must(t, b.API.DeleteMessage(ctx, reply.ID))
	as.next(t, "message.deleted", match("message_id", reply.ID))
	as.client.Typing(gen, nil)
	bs.next(t, "typing", match("channel_id", gen), match("user_id", a.UserID))

	unread := func() int {
		us, err := a.API.ListUnreads(ctx); must(t, err)
		for _, u := range us { if u.ChannelID == gen { return u.UnreadCount } }
		return 0
	}
	if unread() < 1 { t.Fatal("c's message must be unread for a") }
	_, err = a.API.MarkRead(ctx, gen, root.ID); must(t, err)
	if n := unread(); n != 0 { t.Fatalf("unread after MarkRead = %d", n) }
	if hits, err := b.API.SearchMessages(ctx, api.MessageSearchQuery{Q: "회의", SpaceID: sp.ID}); err != nil || len(hits.Items) == 0 { t.Fatalf("search: %v", err) }

	g, _, err := a.API.OpenDM(ctx, api.OpenDMRequest{Users: []api.UserRef{api.Handle(b.Handle), api.Handle(c.Handle)}}); must(t, err) // friend + shared-space member
	if g.Kind != "group_dm" { t.Fatalf("group dm kind %q", g.Kind) }
	if page, err := b.API.ListDMs(ctx, api.PageOpts{}); err != nil || !slices.ContainsFunc(page.Items, func(ch api.Channel) bool { return ch.ID == g.ID }) { t.Fatalf("group dm not listed for b: %v", err) }
}
```

- [ ] **Step 2: Run the G2 gate test**

Run: `MASIL_E2E_SERVER=http://localhost:8787 go test -tags e2e ./internal/daemon/e2egate/ -run 'TestGateM[12]' -v -count=1` (G1 시험도 회귀로 함께 — 가입 4명이라 `RL_SIGNUP`(IP 당 60초 5회) 안; 직전 관문 실행과 60초 간격)
Expected: 두 시험 PASS. 실패 시 규칙 4.

- [ ] **Step 3: Manual two-TUI chat check (tmux, 로컬 전용)**

```bash
S=$(mktemp -d); go build -o "$S/masil" ./cmd/masil; export MASIL_SERVER=http://localhost:8787
tmux new-session -d -s alice -x 120 -y 40 "MASIL_CONFIG_DIR=$S/alice MASIL_SERVER=$MASIL_SERVER $S/masil signup"
tmux new-session -d -s bob   -x 80  -y 24 "MASIL_CONFIG_DIR=$S/bob   MASIL_SERVER=$MASIL_SERVER $S/masil signup"
```
진행(사람이 attach 해 조작, 단계마다 `tmux capture-pane -p -t <이름> > "$S/<이름>-<n>.txt"`): 가입 → alice `ctrl+k` → bob 친구 요청 → bob 수락 → DM 3줄(한글·이모지 `👍`) → alice 가 방 만들고 초대 코드 발급 → bob 이 `ctrl+c` 두 번 뒤 `MASIL_CONFIG_DIR=$S/bob $S/masil join <코드>` → alice 가 `#일반` 에 `@<bob>` 멘션 → bob 이 그 메시지에서 스레드 열고 답글 → alice 리액션·수정·고정, bob 이 자기 답글 삭제 → alice 가 입력창에 글자를 치는 동안 bob 화면 → bob `ctrl+f` 로 검색.
Expected: 모든 메시지·리액션·수정·삭제 묘비·고정·"답글 N개"·안 읽음/멘션 배지·입력 중 표시·친구 접속 점이 **새로고침 없이** 상대 화면에 나타남, bob(80×24 위젯)은 말풍선이 세로로 쌓이고 내 메시지가 오른쪽 정렬, alice(120×40 표준)는 스레드가 대화창 아래 분할로 열림, 캡처 어디에도 테두리 끊김·겹침·한글 폭 어긋남이 없음, 검색 결과에 멘션 메시지가 보임. 끝나면 자기 tmux 세션만 종료, `$S` 삭제.

- [ ] **Step 4: Report the gate** — PASS 줄과 Step 3 일치 여부를 보고. **G2 통과** 뒤에만 01 Task 29 · 02 Task 1·3~10 · 05 Task 1~15·18·19 배정.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/e2egate/gate_test.go
git commit -m "test(e2e): M2 gate — chat flows delivered live against wrangler dev"
```

---

### Task 16: 관문 G3 — M3 SNS

**실행 시점**: G2 통과 + 01 Task 29 · 02 Task 1~10 · 05 Task 1~15·18·19 완료 뒤. 통과 전에는 02 Task 11~18 · 05 Task 16·17·20 · 06 Task 1~8 을 배정하지 않는다. 05 Task 10(피드 투표)·12(방명록)·신고·차단은 이 관문 합격 조건이 아니다(G4).

**Files:**
- Modify: `internal/daemon/e2egate/gate_test.go`

**Interfaces:**
- Consumes: Task 14 헬퍼; `(*api.Client).{Follow, Upload, FetchMedia, CreatePost, GetPost, ListReplies, Like, Repost, FollowingTimeline, HashtagPosts, Trends, CreateStory, StoryFeed, ViewStory, ListNotifications, GetUser}`, `api.{CreatePostRequest, CreateStoryRequest, TimelineQuery, NotificationQuery, PageOpts}`, 이벤트 `timeline.item_created`·`story.created`·`notification.created`, 로컬 크론 트리거 `GET /__scheduled?cron=*/5+*+*+*+*`(server-architecture 8장)
- Produces: 없음(검증)

- [ ] **Step 1: Write the G3 test**

```go
// valid 1x1 RGB PNG (69 bytes, CRCs checked)
var onePixelPNG = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0x00, 0x00, 0x03, 0x01, 0x01, 0x00, 0xc9, 0xfe, 0x92, 0xef, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82}

// M3: follow, photo post, live following timeline, reply/quote/like/repost, stories,
// hashtag feed + trends, notification inbox, profile.
func TestGateM3SNS(t *testing.T) {
	srv := e2eServer(t)
	ctx := t.Context()
	a, b := signup(t, srv, "g3a"), signup(t, srv, "g3b")
	as, bs := connect(t, a, realtime.ModeFull), connect(t, b, realtime.ModeFull)
	as.next(t, "ready"); bs.next(t, "ready")

	if st, err := b.API.Follow(ctx, api.Handle(a.Handle)); err != nil || st != "active" { t.Fatalf("follow %q, %v", st, err) }
	as.next(t, "notification.created", match("notification.type", "follow"))
	up, err := a.API.Upload(ctx, "post", "image/png", bytes.NewReader(onePixelPNG), int64(len(onePixelPNG))); must(t, err)
	tag := "관문" + strings.ToLower(ids.New()[20:])
	p, _, err := a.API.CreatePost(ctx, api.CreatePostRequest{ClientNonce: ids.New(), Body: "관문 첫 글 #" + tag, MediaUploadIDs: []string{up.ID}}); must(t, err)
	bs.next(t, "timeline.item_created", match("item.post.id", p.ID)) // pushed to followers (events.md 4.8)
	tl, err := b.API.FollowingTimeline(ctx, api.TimelineQuery{}); must(t, err)
	if len(tl.Items) == 0 || tl.Items[0].Post.ID != p.ID { t.Fatalf("following timeline top %+v", tl.Items) }
	blob, err := b.API.FetchMedia(ctx, tl.Items[0].Post.Media[0].URL, ""); must(t, err)
	if !bytes.Equal(blob.Data, onePixelPNG) { t.Fatal("media bytes differ") }

	_, err = b.API.Like(ctx, p.ID); must(t, err)
	as.next(t, "notification.created", match("notification.type", "post_like"))
	_, _, err = b.API.CreatePost(ctx, api.CreatePostRequest{ClientNonce: ids.New(), Body: "답글이에요", ReplyToID: p.ID}); must(t, err)
	_, _, err = b.API.CreatePost(ctx, api.CreatePostRequest{ClientNonce: ids.New(), Body: "인용해요", QuoteOfID: p.ID}); must(t, err)
	_, err = b.API.Repost(ctx, p.ID); must(t, err)
	got, err := a.API.GetPost(ctx, p.ID); must(t, err)
	if got.LikeCount != 1 || got.ReplyCount != 1 || got.QuoteCount != 1 || got.RepostCount != 1 { t.Fatalf("counts %+v", got) }
	if rs, err := a.API.ListReplies(ctx, p.ID, api.PageOpts{}); err != nil || len(rs.Items) != 1 { t.Fatalf("replies: %v", err) }

	st, err := a.API.CreateStory(ctx, api.CreateStoryRequest{Body: "관문 스토리"}); must(t, err)
	bs.next(t, "story.created", match("user_id", a.UserID))
	groups, err := b.API.StoryFeed(ctx); must(t, err)
	if len(groups) == 0 || groups[0].User.ID != a.UserID || !groups[0].HasUnseen { t.Fatalf("story feed %+v", groups) }
	must(t, b.API.ViewStory(ctx, st.ID))
	if groups, _ = b.API.StoryFeed(ctx); groups[0].HasUnseen { t.Fatal("story still unseen after view") }

	if hp, err := b.API.HashtagPosts(ctx, tag, api.PageOpts{}); err != nil || !slices.ContainsFunc(hp.Items, func(x api.Post) bool { return x.ID == p.ID }) { t.Fatalf("hashtag feed: %v", err) }
	resp, err := http.Get(srv + "/__scheduled?cron=*/5+*+*+*+*"); must(t, err); resp.Body.Close()
	if tr, err := b.API.Trends(ctx); err != nil || !slices.ContainsFunc(tr.Items, func(x api.Trend) bool { return x.Tag == tag }) { t.Fatalf("trends: %v", err) }

	np, err := a.API.ListNotifications(ctx, api.NotificationQuery{}); must(t, err)
	seen := map[string]bool{}
	for _, n := range np.Items { seen[string(n.Type)] = true }
	for _, want := range []string{"follow", "post_like", "post_reply", "post_quote", "post_repost"} {
		if !seen[want] { t.Errorf("inbox lacks %s: %v", want, seen) }
	}
	prof, err := b.API.GetUser(ctx, api.Handle(a.Handle)); must(t, err)
	if prof.PostCount < 1 || prof.Relationship == nil || prof.Relationship.Following != "active" { t.Fatalf("profile %+v", prof) }
}
```
(`RL_POST` 은 사용자당 60초 10회 — a 는 글 1·스토리 1, b 는 답글 1·인용 1 이라 여유가 있다.)

- [ ] **Step 2: Run the G3 gate test**

Run: `MASIL_E2E_SERVER=http://localhost:8787 go test -tags e2e ./internal/daemon/e2egate/ -run TestGateM3 -v -count=1` (G1~G3 를 한 번에 돌리면 가입 6명이라 `RL_SIGNUP` 에 걸린다 — 앞 관문 회귀는 60초 뒤 `-run 'TestGateM[12]'` 로 따로, 최종 회귀는 Task 18)
Expected: PASS. 실패 시 규칙 4.

- [ ] **Step 3: Manual feed check (tmux, 로컬 전용)**

Task 15 Step 3 과 같은 방식으로 alice(120×40)·bob(80×24)을 띄운다. 진행: alice 가 bob 을 팔로우(`ctrl+k` → `@<bob>` 프로필 → `+`, 칩이 `[팔로잉 ✓]` — 친구라고 팔로우가 되는 게 아님) → alice 가 피드(`f`)의 **`팔로잉` 탭(`2`)** 을 띄워 둠(발견 탭은 푸시가 없고 30초 폴링이라 쓰지 않음) → bob 이 피드(`c`)에서 `#관문` 태그 글 작성 → alice 팔로잉 탭에 새로고침 없이 표시 → alice 가 좋아요·답글 → bob `ctrl+n` 알림함에 표시 → bob 스토리 작성 → alice 피드 상단 스토리 링 → alice 가 `#관문` 태그를 열어 태그 피드, 팔레트 `트렌드 Top 10` → 200×60 으로 키워 지휘소 피드 열·알림 창이 동시에 보이는지.
Expected: 위 단계가 모두 새로고침 없이 보이고, 120×40·80×24·200×60 캡처 어디에도 테두리 끊김·겹침이 없음(사진 글은 하프블록 `▀` 로 렌더). 끝나면 자기 세션만 종료.

- [ ] **Step 4: Report the gate** — **G3 통과** 뒤에만 02 Task 11~18 · 05 Task 16·17·20 · 06 Task 1~8 배정.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/e2egate/gate_test.go
git commit -m "test(e2e): M3 gate — feed, follow, stories, trends and inbox against wrangler dev"
```

---

### Task 17: 관문 G4 — M4 재미·운영 (데몬 E2E · 유휴 자원 측정 · 운영자 · 두 클라이언트)

**실행 시점**: G3 통과 + 02 Task 11~18 · 05 Task 16·17·20 · 06 Task 1~8 완료 뒤. 원격 명령(`--remote`, `wrangler deploy`) 금지.

**선행 확인(차단 조건 — 교차 계획)**: Step 3 의 `EndFocus` 뒤 `waitShown(4, "focus_summary")` 는 **조기 종료에도** 서버가 `alert{kind:"focus_summary", suppressed:false}` 를 보낸다는 가정이다(events.md 4.7·api.md 5.1·CA 9.1). server-core(01) Task 12 의 Produces 는 "알람 종료일 때만 alert" 라서 01 만으로는 이 시험이 10초 시간 초과로 실패한다. 02 Task 13 의 "고정 기준 맞추기"(종료 사유와 상관없이 alert — 01 Task 12 문구를 대체)가 반영돼 있어야 한다. 시작 전에 `cd server && npx vitest run test/api/status-focus.test.ts -t "early end summary"` 와 `npm run smoke`(02 Task 18 의 11단계가 조기 종료 `focus_summary` 를 확인)를 돌려 둘 다 PASS 인지 본다. 실패하면 이 Task 를 시작하지 말고 02 Task 13(또는 01 Task 12) 소유자에게 넘기도록 오케스트레이터에 보고한다 — 이 시험의 `waitShown(4, …)` 를 빼거나 느슨하게 고치지 않는다.

**Files:**
- Create: `internal/daemon/e2e_local_test.go`(`//go:build e2e`)

**Interfaces:**
- Consumes: `api.New`, `api.WithToken`, `(*api.Client).Signup(ctx, api.SignupRequest) (*api.AuthResult, error)`, `SendFriendRequest(ctx, api.UserRef) (*api.FriendRequestResult, error)`, `AcceptFriendRequest(ctx, requestID string) (*api.Friend, error)`, `OpenDM(ctx, api.OpenDMRequest) (*api.Channel, bool, error)`, `SendMessage(ctx, channelID string, api.SendMessageRequest) (*api.Message, bool, error)`, `CreatePost(ctx, api.CreatePostRequest) (*api.Post, bool, error)`, `StartFocus(ctx, api.Opt[int]) (*api.Focus, error)`, `EndFocus(ctx) (*api.FocusSummary, error)`, `MarkNotificationsRead(ctx, api.MarkNotificationsRead) (int, error)`, `api.Handle(h string) api.UserRef`, `api.Some[T]`, `ids.New() string`, `auth.Save`, `auth.Load`, `config.ResolvePaths`, `instance.AcquireTUI`, `daemon.Run`, Task 3 `lockedRecorder`, Task 4 `msg="realtime ready"` 로그
- Produces: 없음(검증)

- [ ] **Step 1: Run the server-side M4 checks**

Run: wrangler dev 를 Task 14 Step 1 처럼 띄운 뒤 `cd server && npm run smoke`
Expected: `✓` 14줄 + `SMOKE OK`(02 Task 18 — 투표·방명록·상태·집중 조기 종료 요약 alert·신고·운영자 삭제·정지/해제를 실서버 경로로 확인), 그리고 위 선행 확인의 vitest PASS.

- [ ] **Step 2: Write the daemon e2e test and the flood helper** (`MASIL_E2E_SERVER` 없으면 `t.Skip`)

```go
//go:build e2e

func TestLocalDaemonE2E(t *testing.T) {
	srv := os.Getenv("MASIL_E2E_SERVER")
	if srv == "" { t.Skip("set MASIL_E2E_SERVER=http://localhost:8787") }
	alice, bob := signupE2E(t, srv, "e2ea"), signupE2E(t, srv, "e2eb") // unique handles, returns *api.Client + handle + userID + token

	// bob's daemon: temp config dir, bob's credentials, recorder backend, logs captured
	p := testPaths(t)
	t.Setenv("MASIL_SERVER", srv)
	saveCredsFor(t, p, srv, bob)
	rec, logs := &lockedRecorder{}, &syncBuffer{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, Options{Paths: p, Config: mustLoad(t, p), Backend: rec, Logger: slog.New(slog.NewTextHandler(logs, nil)), PollCredentials: time.Second}) }()
	t.Cleanup(func() { cancel(); <-done })
	waitForTimeout(t, 15*time.Second, func() bool { return strings.Contains(logs.String(), "realtime ready") })
	waitShown := shownWaiter(t, rec, logs) // func(n int, kind string) notify.OSNotification

	// 1) friend request -> friend_request alert
	fr, err := alice.API.SendFriendRequest(ctx, api.Handle(bob.Handle))
	must(t, err)
	waitShown(1, "friend_request")
	_, err = bob.API.AcceptFriendRequest(ctx, fr.Request.ID); must(t, err)

	// 2) DM -> dm alert, body visible in the OS notification
	ch, _, err := alice.API.OpenDM(ctx, api.OpenDMRequest{Users: []api.UserRef{api.Handle(bob.Handle)}}); must(t, err)
	_, _, err = alice.API.SendMessage(ctx, ch.ID, api.SendMessageRequest{ClientNonce: ids.New(), Kind: "text", Body: "안녕 밥"}); must(t, err)
	if n := waitShown(2, "dm"); !strings.Contains(n.Body, "안녕 밥") { t.Fatalf("dm body %q", n.Body) }

	// 3) TUI present on bob's device -> no popup
	release, err := instance.AcquireTUI(p); must(t, err)
	_, _, err = alice.API.SendMessage(ctx, ch.ID, api.SendMessageRequest{ClientNonce: ids.New(), Kind: "text", Body: "TUI 켜져 있지?"}); must(t, err)
	expectStableCount(t, rec, 2, 4*time.Second)
	release()

	// 4) post mentioning bob -> mention alert
	_, _, err = alice.API.CreatePost(ctx, api.CreatePostRequest{ClientNonce: ids.New(), Body: "@" + bob.Handle + " 첫 글 #masil"}); must(t, err)
	waitShown(3, "mention")

	// 5) focus mode: suppressed while focusing, one summary at the end.
	// Early end (DELETE /v1/me/focus) must also emit alert{focus_summary} — needs plan 02 Task 13 (see 선행 확인).
	_, err = bob.API.StartFocus(ctx, api.Some(600)); must(t, err)
	_, _, err = alice.API.SendMessage(ctx, ch.ID, api.SendMessageRequest{ClientNonce: ids.New(), Kind: "text", Body: "집중 중?"}); must(t, err)
	expectStableCount(t, rec, 3, 4*time.Second)
	_, err = bob.API.EndFocus(ctx); must(t, err)
	waitShown(4, "focus_summary")
	expectStableCount(t, rec, 4, 3*time.Second)

	if strings.Contains(logs.String(), bob.Token) { t.Fatal("token leaked into daemon log") }
}

// Flood helper for the idle-footprint measurement (Step 5): sends 300 non-alert events
// (notifications.read) to the notify socket of the daemon whose config dir is MASIL_FP_DIR.
func TestFootprintFlood(t *testing.T) {
	dir, srv := os.Getenv("MASIL_FP_DIR"), os.Getenv("MASIL_E2E_SERVER")
	if dir == "" || srv == "" { t.Skip("set MASIL_FP_DIR and MASIL_E2E_SERVER") }
	t.Setenv("MASIL_CONFIG_DIR", dir)
	p, err := config.ResolvePaths(); must(t, err)
	creds, err := auth.Load(p); must(t, err)
	c := api.New(srv, api.WithToken(creds.Token))
	for i := 0; i < 300; i++ { // RL_API is 600/60s
		_, err := c.MarkNotificationsRead(t.Context(), api.MarkNotificationsRead{All: true}); must(t, err)
	}
}
```
(`shownWaiter(t, rec, logs)` 가 돌려준 함수는 `rec` 에 n번째 표시가 올 때까지 10초 기다려 그 `OSNotification` 을 돌려주고, 데몬 로그의 `msg="alert shown"` 줄이 정확히 n개이며 마지막 줄에 `kind=<kind>` 가 있는지 확인한다. `rec` 는 Task 3 의 `lockedRecorder`(전체 `OSNotification` 보관), `expectStableCount(t, rec, n, d)` 는 d 동안 개수가 n 그대로인지 확인.)

- [ ] **Step 3: Run the e2e test against wrangler dev**

Run: `MASIL_E2E_SERVER=http://localhost:8787 go test -tags e2e ./internal/daemon/ -run TestLocalDaemonE2E -v -count=1`
Expected: PASS (`friend_request` → `dm`(본문 `안녕 밥`) → TUI 있음 무알림 → `mention` → 집중 중 무알림 → `focus_summary` 정확히 1번). 태그 없는 `go test ./internal/daemon/` 에는 이 시험이 컴파일되지 않는다. `focus_summary` 에서만 시간 초과면 선행 확인(02 Task 13)이 빠진 것 — 시험을 고치지 말고 보고.

- [ ] **Step 4: Operator CLI against wrangler dev (로컬 D1 만)**

```bash
S=$(mktemp -d); go build -o "$S/masil" ./cmd/masil; export MASIL_SERVER=http://localhost:8787
r() { LC_ALL=C tr -dc a-z0-9 </dev/urandom | head -c 8; }
OPS=ops_$(r); OPW="ops-pass-$(r)"; BAD=bad_$(r); BPW="bad-pass-$(r)"
for u in "$OPS:$OPW" "$BAD:$BPW"; do curl -fsS -o /dev/null -X POST "$MASIL_SERVER/v1/auth/signup" -H 'content-type: application/json' -d "{\"handle\":\"${u%%:*}\",\"password\":\"${u#*:}\"}"; done
printf '%s\n' "$OPW" | MASIL_CONFIG_DIR="$S/ops" "$S/masil" login --handle "$OPS" --password-stdin
MASIL_CONFIG_DIR="$S/ops" "$S/masil" admin reports; echo "exit=$?"
MASIL_CONFIG_DIR="$S/ops" "$S/masil" help | grep -c admin
(cd server && npx wrangler d1 execute DB --local --command "UPDATE users SET role = 'admin' WHERE handle = '$OPS'")
MASIL_CONFIG_DIR="$S/ops" "$S/masil" admin users --q "$BAD"
MASIL_CONFIG_DIR="$S/ops" "$S/masil" admin suspend "@$BAD" --for 1h --reason "gate check"
printf '%s\n' "$BPW" | MASIL_CONFIG_DIR="$S/bad" "$S/masil" login --handle "$BAD" --password-stdin; echo "exit=$?"
MASIL_CONFIG_DIR="$S/ops" "$S/masil" admin unsuspend "@$BAD"
MASIL_CONFIG_DIR="$S/ops" "$S/masil" admin actions
MASIL_CONFIG_DIR="$S/ops" "$S/masil" admin suspend "@$BAD"; echo "exit=$?"
```
Expected: 승격 전 `운영자 권한이 없습니다.`·`exit=1`; `help` 출력에 `admin` 없음(`0`, Hidden); 승격 뒤 `users` 표에 `$BAD`, `정지했습니다: @bad_… — … 까지`, 정지된 계정 로그인 실패(`exit=1`, 정지 안내), `정지를 풀었습니다: @bad_…`, `actions` 표에 `정지`·`정지 해제` 두 줄, `--reason` 없는 `suspend` 는 사용법 오류 `exit=2`(core 표지 — Task 8 차단 확인과 같은 규칙). 비밀번호·토큰이 출력에 없음. 신고 목록·처리(`reports`/`report`/`resolve`/`remove`)는 Step 1 의 스모크(13단계)와 Step 6 의 TUI 신고로 확인한다.

- [ ] **Step 5: Idle daemon footprint (설계서 3.3 "CPU·메모리 사용 최소", 약 17분)**

상한 **[결정]**: 10분 유휴 누적 CPU ≤ 2.0초, RSS ≤ 40 MiB; 비알림 이벤트 300개 뒤 RSS 증가 ≤ 8 MiB, 그 뒤 5분 유휴 동안 RSS 증가 ≤ 1 MiB(누수 없음); 데몬 로그 줄 수가 홍수 동안 늘지 않음(비알림 이벤트는 로그·상태를 남기지 않음 — events.md 7장 "상태를 쌓지 않는다"), `daemon-state.json` ≤ 512 바이트, 설정 디렉터리 파일 목록은 `daemon-state.json` 외에 늘지 않음.

```bash
S=$(mktemp -d); go build -o "$S/masil" ./cmd/masil; export MASIL_SERVER=http://localhost:8787
rss() { ps -o rss= -p "$1" | tr -d ' '; }                                        # KiB (macOS·Linux)
cpu() { if [ "$(uname)" = Darwin ]; then ps -o time= -p "$1" | awk -F: '{print $1*60+$2}'; else ps -o times= -p "$1" | tr -d ' '; fi; }  # seconds
H=fp_$(LC_ALL=C tr -dc a-z0-9 </dev/urandom | head -c 8); PW="fp-pass-$(LC_ALL=C tr -dc a-z0-9 </dev/urandom | head -c 12)"
curl -fsS -o /dev/null -X POST "$MASIL_SERVER/v1/auth/signup" -H 'content-type: application/json' -d "{\"handle\":\"$H\",\"password\":\"$PW\"}"
printf '%s\n' "$PW" | MASIL_CONFIG_DIR="$S/fp" "$S/masil" login --handle "$H" --password-stdin
MASIL_CONFIG_DIR="$S/fp" "$S/masil" daemon run --foreground > "$S/fp-out.txt" 2>&1 & PID=$!
until grep -q 'realtime ready' "$S/fp/logs/daemon.log" 2>/dev/null; do sleep 1; done; sleep 30
snap() { echo "$1 rss_kib=$(rss $PID) cpu_s=$(cpu $PID) log_lines=$(wc -l < "$S/fp/logs/daemon.log") state_bytes=$(wc -c < "$S/fp/daemon-state.json" 2>/dev/null || echo 0)" | tee -a "$S/footprint.txt"; ls -A "$S/fp" > "$S/files-$1.txt"; }
snap A; sleep 600; snap B                                                            # 10 min idle
MASIL_FP_DIR="$S/fp" MASIL_E2E_SERVER=$MASIL_SERVER go test -tags e2e ./internal/daemon/ -run TestFootprintFlood -count=1
sleep 60; snap C; sleep 300; snap D
diff "$S/files-A.txt" "$S/files-D.txt"; grep -c 'msg="alert shown"' "$S/fp/logs/daemon.log"
kill -TERM $PID; wait $PID; cat "$S/footprint.txt"
```
Expected: `cpu_s(B) − cpu_s(A) ≤ 2.0`, `rss_kib(B) ≤ 40960`, `rss_kib(C) − rss_kib(B) ≤ 8192`, `rss_kib(D) − rss_kib(C) ≤ 1024`, `log_lines` 가 A=B=C=D, `state_bytes(D) ≤ 512`, `diff` 출력이 없거나 `> daemon-state.json` 한 줄, `alert shown` 0, `TestFootprintFlood` PASS. `footprint.txt` 네 줄을 관문 보고에 그대로 붙인다. 상한을 넘으면 G4 실패로 보고하고(규칙 4), 원인은 `go test ./internal/daemon/ -run TestHandlerBurstNoLossInOrder -memprofile mem.out` 와 `--verbose` 로그로 좁힌다 — 상한을 올려 통과시키지 않는다(바꾸려면 오너 확인).

- [ ] **Step 6: Manual two-client + real daemon check (tmux, 로컬 전용)**

```bash
S=$(mktemp -d); go build -o "$S/masil" ./cmd/masil; export MASIL_SERVER=http://localhost:8787
MASIL_CONFIG_DIR="$S/alice" "$S/masil" signup   # handle e2e_alice_<rand>
MASIL_CONFIG_DIR="$S/bob"   "$S/masil" signup   # handle e2e_bob_<rand>
tmux new-session -d -s alice -x 120 -y 40 "MASIL_CONFIG_DIR=$S/alice MASIL_SERVER=$MASIL_SERVER $S/masil"
tmux new-session -d -s bob   -x 80  -y 24 "MASIL_CONFIG_DIR=$S/bob   MASIL_SERVER=$MASIL_SERVER $S/masil"
tmux new-session -d -s bobd  -x 120 -y 20 "MASIL_CONFIG_DIR=$S/bob   MASIL_SERVER=$MASIL_SERVER $S/masil daemon run --foreground --verbose"
```
진행(사람이 `tmux attach -t <이름>` 으로 조작): alice 가 `ctrl+k` → bob 친구 요청 → bob 수락 → 서로 DM 3줄 주고받기(한글·이모지 `👍` 포함) → **M4 재미 기능**: alice 가 DM 에서 `/me 춤춘다`, `/shrug`, `/roll 2d6`, `/poll` 로 투표 생성(04 Task 11 사용법) → bob 투표 → 막대그래프 갱신, bob `/status 🎧 듣는 중` → alice 화면 상태 메시지, bob 이 alice 프로필 방명록 작성(05 Task 12), alice 가 피드 투표 글 작성 → bob 투표(05 Task 10), bob `/focus 1m` → 1분 뒤 집중 요약 오버레이가 **한 번** 뜸, bob 이 alice 의 DM 한 줄을 신고(04 Task 12 메뉴 → core `ReportDialog`) → Step 4 의 운영자 셸에서 `masil admin reports` 에 그 신고가 보임 → **alice 가 bob 을 팔로우**(`ctrl+k` → `@e2e_bob_…` 프로필 → `+`, 칩이 `[팔로잉 ✓]` 로 바뀜 — 친구라고 팔로우가 되는 게 아님) → alice 가 피드(`f`)의 **`팔로잉` 탭(`2`; `1` 은 `피드`=발견)** 을 띄워 둠 → bob 이 피드(`f`/위젯 `피드` 탭)에서 `c` 로 글 작성 → **alice 의 팔로잉 탭에** 새로고침 없이 표시(`timeline.item_created` 는 작성자의 active 팔로워와 작성자 본인에게만 간다 — events.md 4.8, api.md `GET /v1/timeline/following`; 발견 탭은 푸시가 없고 30초 폴링이라 이 확인에 쓰지 않는다) → bob TUI 종료(`ctrl+c` 두 번) → alice 가 DM 1줄 → **bob 데몬이 실제 macOS 알림**(제목 alice, 본문 그 DM)을 띄움.
증거 수집: 각 단계 후 `tmux capture-pane -p -t alice > "$S/alice-<n>.txt"`(bob 도 동일), 마지막에 `MASIL_CONFIG_DIR="$S/bob" "$S/masil" daemon status` 와 `grep -c 'msg="alert shown"' "$S/bob/logs/daemon.log"`.
Expected: 캡처한 화면에 테두리가 끊기거나 겹친 줄이 없음(120×40 표준·80×24 위젯), 슬래시 명령 결과·투표 막대·상태 메시지·방명록이 새로고침 없이 양쪽에 나타남, 집중 요약 오버레이 정확히 1번, DM 과 (팔로우 뒤) bob 의 글이 alice 의 대화창·팔로잉 탭에 새로고침 없이 나타남, bob TUI 가 떠 있는 동안 데몬 로그에 `alert shown` 이 늘지 않고 TUI 종료 뒤 DM 에서 1 늘어남, OS 알림 1개가 눈으로 확인됨, `status` 가 `실행 중`·`@e2e_bob_…` 표시. 끝나면 `tmux kill-session -t alice -t bob -t bobd`(자기가 띄운 세션만), wrangler dev 백그라운드 작업 종료, `$S` 삭제.

- [ ] **Step 7: Report the gate** — Step 1·3 PASS 줄, Step 4·6 일치 여부, Step 5 `footprint.txt` 네 줄을 보고. **G4 통과** 뒤에만 06 Task 9~13 · 03 Task 36 배정.

- [ ] **Step 8: Commit**

```bash
git add internal/daemon/e2e_local_test.go
git commit -m "test(daemon): M4 gate — daemon e2e and idle footprint flood against wrangler dev"
```

---

### Task 18: 관문 G5 — 최종(출시 준비)

**실행 시점**: G4 통과 + 06 Task 9~13 · 03 Task 36 완료 뒤. 이 관문은 새 파일을 만들지 않는다. 태그 푸시·배포·레포 공개는 여기서도 하지 않는다(Global Constraints — 오너 윤허 후 오너).

- [ ] **Step 1: Full suites on the final tree**

Run: `go vet ./... && go test ./... -count=1 && (cd server && npx tsc --noEmit && npm test)`
Expected: 모두 PASS(03 Task 36 의 전체 제품 골든·무결성 스윕 포함).

- [ ] **Step 2: All gates once more, in order, on the final tree**

Run: wrangler dev 를 새로 띄우고(`--local` 마이그레이션부터) `MASIL_E2E_SERVER=http://localhost:8787 go test -tags e2e ./internal/daemon/e2egate/ -run TestGateM1 -count=1`, 60초 뒤 `-run TestGateM2`, 60초 뒤 `-run TestGateM3`, 60초 뒤 `go test -tags e2e ./internal/daemon/ -run TestLocalDaemonE2E -count=1`, 그다음 `cd server && npm run smoke`
Expected: 전부 PASS / `SMOKE OK` — 뒤 마일스톤 작업이 앞 마일스톤을 깨지 않았다.

- [ ] **Step 3: Release artifacts from the final tree**

Run: Task 9 Step 4(스냅숏 빌드·`go version -m`)와 Task 10 Step 3(설치 스크립트 정상·변조·지원 안 함 경로)을 다시 실행
Expected: 각 Task 의 Expected 그대로. 이어서 `dist/` 는 지운다(`.gitignore` 대상).

- [ ] **Step 4: Report** — 세 Step 의 결과를 오케스트레이터에 보고한다. 남은 일은 오너 윤허 사항뿐이다(Task 11 Step 4 의 tap·비밀·변수 등록, `wrangler deploy`·원격 D1/R2, 운영자 지정의 원격 실행, 태그 푸시, 레포 공개 전환) — 실행자는 하지 않는다.
