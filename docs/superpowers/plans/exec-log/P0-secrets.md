# P0 batch secrets — Task 21·22·23

- 브랜치 `feat/v013-p0-secrets`, 기준 `dev/v0.1.3` `58376da`(wave 3 뒤). push·tag·배포·원격 wrangler 없음. server/ 는 건드리지 않음.
- 시험은 macOS 에서만 실제로 돌았다. Linux·Windows 는 `go vet` 과 `go test -c`(시험 바이너리 컴파일)만 했다.

## Task 21 `internal/secrets` 핵심

- 내 판단: `Secret` 은 계획의 `struct{ v string }` 대신 `struct{ p *string }` 이다. 비공개 필드에 Secret 을 넣은 남의 구조체를 `%v` 로 찍으면 fmt 가 리플렉션으로 문자열을 그대로 보여 주는데, 포인터면 주소만 보인다. 바깥 겉모양(New·Reveal·Masked·IsZero·redacted 출력)은 같다.
- 내 판단: `Masked` 는 바이트가 아니라 글자(rune) 기준으로 마지막 4자를 보인다.
- 내 판단: `Store` 에 계획 밖의 `Fallback()` 읽기를 더했다(시험 `TestConsentSetsFallbackThenBroadcasts` 가 Session 이 됐는지 봐야 해서). `Service = "masil"` 상수도 내보냈다.
- 내 판단: OS 저장소가 한 번 `ErrUnavailable` 을 내면 그 실행 동안 Put·Get 은 OS 를 다시 부르지 않는다(키체인 거절 창이 거듭 뜨지 않게). Delete·DeleteAll 은 늘 OS 를 부른다(로그아웃 지우기가 빠짐없이).
- 내 판단: Put 은 성공한 곳 말고 다른 로컬 자리(파일·세션)에 남은 같은 키 값을 지운다. Get 순서는 세션 → (색인이 file 이면 파일) → OS → 파일.
- 내 판단: `index.json` 은 `{"version":1,"entries":{"<account>":"<backend>"}}` — 값 없이 이름과 보관 위치만. 세션 값은 색인 파일에 쓰지 않는다(재시작 뒤 없어야 하므로). `tools.json` 은 `{"version":1,"values":{...}}`, 0600, 폴더 0700(더 넓으면 좁힘), `.gitignore` 는 `*\n`. 깨졌거나 1 MiB 넘는 파일은 빈 것으로 읽고 경고만 남긴다 — 이때 json 오류 문구는 로그에 쓰지 않는다(파일 글자를 인용할 수 있음).
- 내 판단: `Key` 는 Tool·Server·UserID 에 `/` 와 제어 문자를 받지 않는다(Name 은 `/` 허용 — API 환경/변수 이름). `List`·`DeleteAll` 이 계정 문자열을 거꾸로 풀 수 있게.
- 내 판단: `Open` 은 I/O 를 하지 않는다(시작할 때 키체인 창 없음). 그래서 계획의 "run.go 에서 secrets.Open" 은 `run.go` 의 `openSecrets(o)` 를 `app.New` 가 부르는 모양이다(`app.Options.Secrets`, nil 이면 이 OS 기본 저장소). 하네스와 Run 이 같은 길을 타고 `ctx.Secrets` 는 언제나 nil 이 아니다. `model.go` 에는 줄 둘(옵션 필드·호출)만 더했다.
- 내 판단: `uitest.WithSecrets` 를 쓰려고 Files 밖의 `internal/ui/uitest/harness.go` 를 고쳤다(기본 = `secrettest.NewStore(tb, true)`).
- 내 판단: `secrettest.Values` 가 세션·파일 값을 읽도록 `secrets.ValuesForTest(s)` 를 내보냈다. 시험 밖에서 부르면 panic(`testing.Testing()`, termhost 와 같은 방식).

## Task 22 OS 저장소 셋

- 내 판단: macOS `security -i` 의 따옴표 규칙은 이 세션에서 실제 `/usr/bin/security` 에 읽기 전용 명령(`show-keychain-info` 에 없는 경로)을 넣어 확인했다 — 작은따옴표 안에서 `\'`·`\\` 가 풀리고, POSIX 식 `'\''` 이어붙이기는 안 된다. 그래서 값은 `'...'` 안에서 `\` → `\\`, `'` → `\'` 로 적는다. 키체인 항목은 하나도 만들지 않았다. `-i` 모드의 종료 코드는 마지막 명령의 OSStatus 아래 바이트다(확인함).
- 내 판단: 줄바꿈·CR·NUL 이 든 값은 한 줄 명령으로 안전하게 못 넘기므로 macOS 백엔드가 오류로 거절한다(OS 를 부르지 않음).
- 내 판단: `security` 종료 코드 44 = `ErrNotFound`, 그 밖의 실패(51 거절, 45, 128 취소, 36, 도구 없음 등)는 모두 `ErrUnavailable` → 저장소가 동의를 묻는다. 도구 출력은 오류에 넣지 않는다.
- 내 판단: Windows 조각 나누기와 Linux Secret Service 흐름은 OS 와 상관없는 파일(`credman.go`, `secretservice.go`)에 두고 가짜(`credAPI`, `dbusConn`)로 모든 OS 에서 시험한다. 그래서 `TestCredManChunks`·`TestSecretServiceFlow` 는 계획의 `backend_windows_test.go`·`backend_linux_test.go` 가 아니라 `credman_test.go`·`secretservice_test.go` 에 있다(이 Mac 에서 실제로 돈다). OS 파일에는 syscall·godbus 어댑터와 `defaultOS` 만 있고, 그 시험 파일은 기본 백엔드 이름·오류 번호 대응·버스 없음 확인만 한다.
- 내 판단: Windows 는 계획의 `syscall.NewLazyDLL` 대신 `golang.org/x/sys/windows.NewLazySystemDLL("advapi32.dll")`(시스템 폴더에서만 읽음)을 쓴다. `CRED_PERSIST_LOCAL_MACHINE`(이 PC·이 사용자, 로밍 안 함). 값 ≤ 2,560바이트는 머리 항목 하나, 넘으면 `masil/<account>#0…` 조각 + 머리 항목(Comment `masil:chunked`, 내용 = 조각 수), 최대 64조각. 조각을 먼저 쓰고 머리를 나중에 쓴다. `ERROR_NOT_FOUND` = 없음, `ERROR_NO_SUCH_LOGON_SESSION` = `ErrUnavailable`.
- 내 판단: Linux 는 `DBUS_SESSION_BUS_ADDRESS` 가 비면 바로 `ErrUnavailable`(dbus-launch 자동 실행 안 함, `$XDG_RUNTIME_DIR/bus` 로 따로 찾지도 않음 — 계획 문구 그대로). 같은 항목이 있으면 `SetSecret`, 없으면 기본 모음(`ReadAlias("default")`, 없으면 unavailable)에 `CreateItem(replace=true)`. 잠긴 항목·모음은 `Unlock` 의 Prompt 를 `Completed` 신호까지 기다린다. D-Bus 실패는 모두 `ErrUnavailable` 로 감싼다.
- 실기 확인(macOS 실제 키체인 저장·허용 창·거절, 재시작 뒤 다시 안 묻기 / Linux GNOME Keyring·헤드리스)은 `00-index.md` 4절 오너 확인 #3·#4 에 이미 있어 줄을 더하지 않았다.

## Task 23 비밀값 화면 · 누출 시험 도우미 · import 그래프 시험

- 내 판단: 도구가 비밀값 키를 만들 수 있게 `ui.Context.SecretScope() (server, userID, ok)` 와 `ui.Context.SecretKey(tool, name) (secrets.Key, bool)` 를 새 파일 `internal/ui/secretkey.go` 에 더했다(서버 = API 기본 URL 의 host, 없으면 설정 서버; 사용자 = `ctx.Me().ID`). 설정 [지우기]·로그아웃 지우기도 이것을 쓴다. 계획 Files 밖.
- 내 판단: 동의 대화상자는 ↑/1 = ①, ↓/2 = ②(기본), enter/space 확인. esc 는 아무것도 정하지 않고 `ConsentMsg{Choice: FallbackUnset}` 을 방송한다(도구가 기다림을 끝낼 수 있게). 확인은 `SetFallback` 을 먼저 부른 뒤 닫기 + 방송.
- 내 판단: 동의 문장은 줄바꿈으로 전문을 보이고, 높이가 모자라면 빈 줄 → 바닥 안내 순으로 뺀다. 문장과 선택지는 빼지 않는다.
- 내 판단: 입력 대화상자는 붙여넣기를 받고(쿠키는 보통 붙여넣음) 줄바꿈·제어 문자는 버린다. 칸에는 글자 수만큼 `•` 만 보이고(칸보다 길면 끝쪽), enter 뒤 버퍼를 지운다. 깨진 args 는 아무것도 열지 않는다.
- 내 판단: 로그아웃 선택지는 계획의 `overlays.go` 가 아니라 `widgets.ConfirmCheck`(새 파일 `internal/ui/widgets/confirmcheck.go`, Confirm 내부 도우미 재사용) + `commands.go` 의 `logoutDialog` 로 했다. 로그아웃 대화상자가 실제로 `commands.go` 에 있다. 기존 골든을 지키려고 **기능 플래그가 하나라도 켜졌을 때만** 선택지를 보인다(모두 꺼짐 = 0.1.2 Confirm 그대로). space/x 로 켜고 끈다(기본 꺼짐). 지우기는 POST /v1/auth/logout 뒤 같은 고루틴에서 `DeleteAll(server, user)` — 범위는 세션이 끝나기 전에 읽는다. 실패는 로그만.
- 내 판단: `logoutConfirmedMsg` 에 `wipeTools` 칸을 더하고 `model.go` 의 한 줄(`m.logoutConfirmed(msg.wipeTools)`)과 `sync.go` 의 `logoutConfirmed` 를 고쳤다.
- 내 판단: 설정 "도구 로그인" 묶음은 `sections` 표의 로그아웃 바로 앞에 자리만 두고, 줄이 하나라도 있을 때만 왼쪽 목록·머리에 그린다(제목은 카탈로그 `tools.logins_section`). 줄은 그 도구가 등록돼 있고 플래그가 켜져 있고 `describe` 가 ok 일 때만. 줄 모양 `<도구 이름> · <계정> · <보관 위치> · [지우기]`, 보관 위치는 그 도구 첫 값 이름의 `Where`(OS 를 부르지 않음). 긴 줄(일반 파일 경고)은 자르지 않고 줄바꿈한다(`row.wrapSub`, 이 줄에만).
- 내 판단: [지우기] 는 바로 지우지 않고 확인 대화상자(`{tool} 로그인을 지울까요?`)를 먼저 띄운다. 확인 뒤 forget 이 있으면 forget 만, 없으면 그 도구·서버·사용자의 모든 이름을 고루틴에서 Delete. 실패하면 줄 아래 `지우지 못했어요`.
- 내 판단: `RegisterToolLogin` 은 빈 ID·nil describe·중복을 panic 으로 막는다. `ResetToolLoginsForTest()` 는 restore 함수를 돌려준다(`t.Cleanup(settings.ResetToolLoginsForTest())`).
- 내 판단: `AssertNoLeak` 이 헤더를 보도록 `apitest.Recorded` 에 `Header http.Header` 를 더했다(Files 밖, 칸 하나 + 기록 한 줄, masil 토큰은 가림). 값은 날것·URL 쿼리/경로 이스케이프·JSON 이스케이프 꼴까지 찾고, 보고에는 요청 번호와 자리만 적는다(값·경로를 되풀이하지 않음). WebSocket 프레임은 apitest 가 기록하지 않아 보지 않는다.
- 내 판단: `TestImportGraph` 는 `go list -deps` 대신 `go list -json <패키지>` 의 `Deps`(전이)·`Imports`(직접)를 읽는다. 도구 접두사 표 `toolPackagePrefixes` 는 각 줄기가 한 줄씩 더한다. 시험이 정말 잡는지 `internal/api` 에 `secrets` 를 import 하는 임시 파일로 실패를 확인하고 지웠다.
- 내 판단: 새 문구는 모두 `tools.toml`(ko·en)에. `consent_question`·`backend_file` 은 `no_truncate`+`legal`, `logins_forget_body`·`logout_wipe` 는 `no_truncate`. zh-Hans 는 비움(경고만).
