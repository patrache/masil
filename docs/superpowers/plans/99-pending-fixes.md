# 통합 단계에서 반드시 반영할 보류 수정 사항

갈래 작업이 진행 중인 작업 트리를 동시에 건드리지 않으려고 여기 모아 두고, 통합(F) 단계에서 처리한다.

0. **[보안·높음 — 최우선, 미해결 시 출시 금지] `internal/notify/backend.go` — OS 알림 경유 원격 명령 실행** (자동 보안 검토, 2026-11-05 16:49)
   - `beeep.Notify(n.Title, n.Body, "")` 에 **다른 사용자가 보낸 메시지 내용**(제목·본문)이 그대로 들어감. beeep 은 Windows 에서 PowerShell 스크립트(go-toast 폴백) 안에 문자열을 끼워 넣고, macOS 에선 osascript 스크립트 문자열에 끼워 넣음 → 누구든 `$(...)`, 백틱, `"@`, `]]>`, `"` 가 든 메시지를 보내면 받는 사람 PC 에서 명령 실행 가능.
   - 수정: beeep 사용 중단, 값을 **스크립트가 아니라 데이터로** 넘기는 백엔드로 교체.
     - macOS: `osascript -e 'on run argv' -e 'display notification (item 2 of argv) with title (item 1 of argv)' -e 'end run' -- <title> <body>` 처럼 argv 로 전달(문자열 연결 금지), 또는 동등한 안전 방식.
     - Linux: D-Bus `org.freedesktop.Notifications.Notify` 직접 호출(godbus) 또는 `notify-send -- <title> <body>`(exec 인자 배열, 셸 없음).
     - Windows: PowerShell 폴백 금지. WinRT 토스트 XML 을 만들 땐 제목·본문을 XML 이스케이프, 실행은 스크립트 문자열 보간 없이.
     - 공통: 알림 문자열은 표시 전에 sanitize(제어·bidi 문자 제거 — 1번과 같은 함수) + 길이 제한.
   - (0번 끝에 이어서 0-b 참고)
   - 회귀 시험: 제목/본문에 `$(touch /tmp/pwn)`, `` `id` ``, `"@`, `]]>`, `" & do shell script "id" & "`, 줄바꿈 등을 넣어도 각 OS 백엔드가 만드는 실행 인자/XML 이 비활성(명령으로 해석되지 않음)임을 단언(실제 exec 대신 인자 생성 함수를 시험). 데몬(daemon-release 갈래)도 같은 notify 를 쓰므로 함께 확인.

0-b. **[보안·높음 — 출시 금지] `internal/markup/render.go` — 링크 URL 경유 터미널 이스케이프 주입(OSC 8)** (자동 보안 검토, 2026-11-05 17:05)
   - 메시지 속 링크 URL 이 검증 없이 `Hyperlink(k.url)`(OSC 8) 에 들어감 → URL 에 ESC/BEL/C1 제어 바이트를 넣으면 OSC 시퀀스를 탈출해 받는 사람 터미널에 임의 이스케이프(화면 위조, 클립보드 쓰기 OSC 52, 일부 터미널 기능 악용) 주입.
   - 수정(2중): ① `isWebURL`(또는 `link()`·`bareURL()`)에서 0x20 미만·0x7f·U+0080–U+009F·잘못된 UTF-8 이 하나라도 있으면 링크로 취급하지 않음 — RFC 3986 인쇄 가능 ASCII 만 허용, 나머지는 퍼센트 인코딩. 스킴은 http/https 만. ② `style()` 에서 `url == textwidth.Sanitize(url)` 이고 ESC·BEL 이 없을 때만 Hyperlink 호출. 
   - **전수 점검**: 사용자 입력이 터미널로 나가는 모든 경로(메시지·이름·상태·글·방명록·알림 토스트·이미지 대체 텍스트·kitty/iTerm2 이미지 프로토콜 인자·창 제목 OSC 0/2)가 sanitize 를 거치는지 확인. **확인된 위치 추가**: `internal/api` `Describe(err)` 가 서버 응답의 코드·필드명·사유를 그대로 문장에 넣고, `internal/ui/widgets/report.go` 등이 `errMsg` 를 그대로 렌더 → `api.Describe` 자체에서 서버 제공 문자열을 `textwidth.Sanitize` + 길이 제한(근본 수정), 렌더 쪽도 방어적으로 sanitize. `Describe` 를 쓰는 모든 위젯 점검. 회귀 시험: 서버 오류 응답 `code`/`fields` 키·값에 `[2J`, `]52;...` → Describe 결과와 렌더 출력에 ESC/BEL 없음.
   - 회귀 시험: `https://a.b/\x1b]52;c;ZXZpbA==\x07`, `\x9b`, `\x07`, 잘못된 UTF-8 이 든 URL → 출력에 ESC/BEL/C1 없음 + 일반 텍스트로 표시.

1. **[보안·중간] `internal/textwidth/sanitize.go` — 유니코드 양방향/서식 문자 스푸핑** (자동 보안 검토, 2026-11-05 15:23)
   - 현재 C0/DEL/C1 만 걸러냄. 남이 보낸 메시지·이름에 섞인 문자로 화면 표시를 속일 수 있음.
   - 수정: `filterRunes` 에서 bidi embedding/override/isolate(U+202A–U+202E, U+2066–U+2069), U+200E, U+200F, U+061C, 보이지 않는 Cf 문자(U+200B, U+2060–U+2064, U+FEFF, U+00AD)도 제거. 원칙: `unicode.Cf` 중 U+200D(ZWJ, 이모지 결합 — 결합 규칙 처리 후까지 유지)와 기존 규칙이 따로 다루는 문자를 제외하고 모두 제거. 각 문자에 대한 시험 사례 추가.

2. **[공급망·경고] `.github/workflows/*.yml` — 변경 가능한 액션 태그** (Semgrep Guardian, 2026-11-05)
   - `actions/checkout@v7`, `actions/setup-go@v7` 등 태그 참조는 소유자가 몰래 다른 커밋으로 옮길 수 있음.
   - 수정: 모든 `uses:` 를 40자 커밋 SHA 로 고정하고 뒤에 `# vX.Y.Z` 주석. SHA 는 `gh api repos/<owner>/<repo>/git/ref/tags/<tag>` 로 실제 조회(기억으로 쓰지 말 것). daemon-release 갈래가 만드는 release 워크플로우(goreleaser 등)도 포함.

3. **[보안·중간] `server/src/app.ts` — 업로드 경로의 본문 크기 제한 우회(DoS)** (자동 보안 검토, 2026-11-05 15:27)
   - `POST /v1/uploads` 는 bodyLimit 에서 빠지는데, `jsonParseCheck` 미들웨어가 모든 경로에서 `await c.req.text()` 로 본문 전체를 메모리에 올림 → 크기 제한 없이 거대한 본문 버퍼링 가능.
   - 수정: `jsonParseCheck` 도 `/v1/uploads` 를 건너뛰거나 스스로 `JSON_BODY_MAX_BYTES` 를 강제. bodyLimit 예외 경로는 버퍼링 전에 Content-Length 확인 + 스트림을 읽으며 바이트 수 세기(업로드는 5MB 상한). 회귀 시험: 거대 본문(Content-Length 위조 포함) → 413, 정상 업로드 → 통과.
   - 서버 갈래(A)가 아직 진행 중이면 A 의 최종 검토에서 먼저 잡힐 수 있음 — 통합 시 이미 고쳐졌는지 확인부터.

4. **[보안·중간] `server/src/lib/permissions.ts` `canViewUserContent` — 정지된 계정의 콘텐츠 노출** (자동 보안 검토, 2026-11-05 15:44)
   - 차단·비공개만 보고 계정 정지(`suspended_at`/`suspended_until`)를 보지 않음 → 운영자가 정지한 계정의 프로필·방명록·스토리 등이 계속 보임.
   - 수정: `u.suspended_at, u.suspended_until` 도 조회해 `isSuspendedNow(row, now())` 면 본인이 아닌 열람자에게 `"hidden"`. 경계 의미는 `postVisibilitySql` 과 동일하게. 이 함수를 쓰는 모든 경로 + 같은 판단을 따로 하는 경로(검색·트렌드·팔로워 목록 등)도 점검. 회귀 시험: 정지 중 → hidden, 정지 만료 후 → ok.

5. **[보안·중간] `internal/api` `escPath` — 경로 조각 `.`/`..` 미차단(클라이언트 측 경로 조작)** (자동 보안 검토, 2026-11-05 15:54)
   - 초대 코드처럼 사용자가 붙여 넣는 값이 `..` 이면 `DELETE /v1/invites/..` 가 정규화되어 다른 엔드포인트로 요청이 갈 수 있음(AcceptInvite/GetInvite, DeleteChannel, RemoveChannelMember, 사용자 ID 참조 등).
   - 수정: `escPath` 에서 빈 조각·`.`·`..` 를 요청 전에 거부하고 `invalid_request` 오류 반환. 회귀 시험: 각 값으로 호출 시 네트워크 요청 0회 + 오류. 서버도 방어적으로 ID/코드 형식 검증(정규식)이 되어 있는지 확인.

6. **[보안·중간] `server/src/routes/auth.ts` `POST /auth/password` — 현재 비밀번호 대입 공격 무제한** (자동 보안 검토, 2026-11-05 16:27)
   - 로그인엔 속도 제한이 있지만 비밀번호 변경 경로는 `verifyPassword` 전에 제한이 없음 → 세션 토큰을 훔친 공격자가 현재 비밀번호를 무제한 대입 가능(PBKDF2 CPU 소모도 큼).
   - 수정: `verifyPassword` 전에 로그인과 같은 제한 적용 — `checkLimit(c.env, "RL_LOGIN", \`ip:${clientIp(c)}\`)` + `checkLimit(c.env, "RL_LOGIN", \`handle:${auth.handle}\`)`(또는 사용자 ID 키). 반복 실패 시 현재 세션 폐기도 검토. 회귀 시험: 한도 초과 → 429(계약의 rate-limit 응답 형식), 정상 변경 → 통과. 같은 패턴(비밀번호 재확인이 필요한 다른 경로, 예: 계정 삭제)도 점검.

7. **[정보·낮음] `server/src/do/topic-hub.ts:68` 등 — 로그 형식 문자열에 변수 삽입** (Semgrep Guardian)
   - `console.error(\`TopicHub ${topic}: ...\`)` — topic·사용자 ID는 서버가 만든 값이라 실제 위험은 낮지만, 로그 위조 방지를 위해 상수 메시지 + 구조화 인자(`console.error("TopicHub deliver failed", { topic, target, reason })`)로 통일. 서버 전체 `console.*` 일괄 점검. 확인된 위치 추가: `server/src/services/posts.ts:45` `console.error(\`posts: ${label} failed\`, e)` → `console.error("posts background task failed", { label, err: e })`.

8. **[보안·중간] `server/src/routes/dms.ts` 그룹 DM 생성 — 참여자끼리의 차단 무시** (자동 보안 검토, 2026-11-05 17:05)
   - 생성 시 "나 ↔ 각 참여자" 만 `canDmMany` 로 확인하고 "참여자 ↔ 참여자" 차단은 안 봄(멤버 추가 경로는 봄) → A 가 B 를 차단했어도 C 가 A·B 를 한 그룹 DM 에 넣어 B 가 A 에게 메시지를 보낼 수 있음.
   - 수정: 생성·추가 공용 헬퍼로 `others` 의 모든 쌍 (j,m)/(m,j) 차단을 insert 전에 검사, 실패 id 목록과 함께 `dmNotAllowed`. 회귀 시험: 참여자 간 차단 있으면 생성 거부(403 + id 목록), 없으면 통과, 추가 경로도 동일.

9. **[보안·중간] `server/src/services/membership.ts` — 강퇴·강등된 사람의 초대가 계속 유효** (자동 보안 검토, 2026-11-05 17:34)
   - 직접 초대 수락 시 초대한 사람이 아직 방 멤버인지 안 봄 → 강퇴당한 사람이 보내 둔 초대로 계속 사람을 들일 수 있음. 초대 링크도 만든 사람이 강등·강퇴돼도 살아 있음.
   - 수정: `joinSpace` 직접 초대 분기의 사전 확인과 조건부 UPDATE 둘 다에 `EXISTS(space_members WHERE space_id=?2 AND user_id=inviter_id)`. `removeFromSpace` 에서 그 사람이 보낸 대기 중 직접 초대 취소. 초대 링크는 만든 사람이 강퇴되면 폐기, 강등돼 초대 권한을 잃으면 폐기(권한 규칙은 계약 api.md 기준). 회귀 시험: 강퇴 후 초대 수락 → 거부, 링크 사용 → 거부, 정상 경우 통과.

10. **[보안·중간] `server/src/services/relations.ts` 팔로우 — 차단 확인과 기록 사이 경쟁 상태(TOCTOU)** (자동 보안 검토, 2026-11-05 18:37)
   - `isSuspendedNow(followee)`·`isBlockedEither` 를 먼저 읽고, 그 뒤 별도 배치에서 `INSERT INTO follows ... SELECT ... FROM users u WHERE u.id=?2 ON CONFLICT DO NOTHING` 실행 → 확인과 기록 사이에 상대가 차단하면 차단된 상태로 팔로우(및 팔로워 수 증가)가 남음.
   - 수정: 차단·정지 조건을 쓰기 문 자체에 넣기 — `INSERT ... SELECT` 와 `willInsertActive` 카운터 갱신 조건 모두에 `AND NOT EXISTS (SELECT 1 FROM blocks WHERE (blocker_id=?1 AND blocked_id=?2) OR (blocker_id=?2 AND blocked_id=?1))` + 정지 조건(`postVisibilitySql` 과 같은 경계 의미). 삽입 0건이면 사전 확인 결과와 무관하게 `forbidden("action_not_allowed")` 로 판정(이미 팔로우 중인 멱등 경우는 구분). 반대로 차단 시 기존 팔로우 양방향 삭제 + 카운터 감소가 같은 배치인지도 확인. 같은 "읽고-확인-쓰기" 패턴(친구 요청, DM 생성, 방 참여, 반응 등) 서버 전체 점검.
   - 회귀 시험: 차단 행이 있는 상태에서 쓰기 문 직접 실행 → 0건·카운터 불변, 정상 팔로우 → 통과, 재팔로우 멱등.

11. **[보안·중간] `server/src/services/polls.ts` `vote()`(·취소) — 읽기 전용 대화에서도 투표 가능** (자동 보안 검토, 2026-11-05 19:23)
   - 메시지 투표는 `requireChannelRead` 만 확인 → 차단 등으로 읽기 전용이 된 1:1 DM 에서도 투표해 상대에게 `poll.updated` 를 보낼 수 있음(차단 우회 신호).
   - 계약 보완: api.md 5.19 투표 항목에 "메시지 투표는 리액션과 같은 규칙 — 읽기 권한 + 1:1 DM 은 `can_dm`(읽기 전용이면 `403 read_only`)" 명시. 글 투표는 글 상호작용 규칙(차단 시 `403 action_not_allowed`)을 따름.
   - 수정: `vote()`·투표 취소에서 메시지 투표면 리액션 경로와 같은 권한 헬퍼 사용(not-found → 404 유지), 글 투표면 차단 확인. `closePoll` 은 만든 사람만이라 그대로 두되 만든 사람이 채널에서 빠졌으면 거부할지 확인. 회귀 시험: 읽기 전용 1:1 DM 투표 → 403 read_only, 차단한 작성자의 글 투표 → 403, 정상 → 200.

12. **[개인정보·중간] `internal/imgrender/decode.go` — WebP·GIF 업로드의 메타데이터(위치 등) 미제거** (자동 보안 검토, 2026-11-05 23:30, sns-fun 갈래 커밋)
   - JPEG/PNG 는 재인코딩으로 EXIF 가 빠지지만 GIF·WebP 는 "정상 이미지인지"만 보고 원본 그대로 업로드 → 휴대폰 사진의 EXIF GPS·XMP·기기 정보가 다른 사용자에게 그대로 노출.
   - 수정: WebP 는 RIFF 청크를 다시 써서 `VP8 `·`VP8L`·`ALPH`·`ANIM`·`ANMF` 만 남기고 `EXIF`·`XMP `·`ICCP` 제거 + VP8X 플래그 비트 정리(청크 크기·패딩·RIFF 전체 크기 재계산). GIF 는 주석 확장(0xFE)과 NETSCAPE2.0 이외의 애플리케이션 확장(0xFF) 제거. 어렵거나 형식이 이상하면 업로드 거부(통과시키지 말 것). 서버 쪽에도 같은 원칙이 필요한지 계약(api.md 업로드) 확인 — 클라이언트만 믿으면 수정된 클라이언트로 우회 가능하므로, 최소한 서버가 WebP/GIF 의 EXIF/XMP 청크를 거부하거나 제거하는지 검토.
   - 회귀 시험: EXIF·XMP·ICCP 청크가 든 WebP(단순·VP8X·애니메이션), 주석·임의 앱 확장이 든 GIF → 결과 바이트에 해당 청크/확장 없음 + `image.DecodeConfig` 성공 + 원본과 같은 크기·프레임 수. 잘린/거짓 청크 크기 → 거부.

13. **[보안·중간] `internal/imgrender/decode.go` — 압축 폭탄 판정의 바이트 추정 과소(검증·디코더 차이)** (자동 보안 검토, 2026-11-06 10:32)
   - 픽셀당 바이트를 `cfg.ColorModel` 로 추정하는데 PNG 는 tRNS 가 있으면 Gray→NRGBA(4B), Gray16→NRGBA64(8B) 로 승격돼 실제 메모리가 4~8배 → 한도 우회.
   - 수정: `ct == image/png` 면 Gray/Alpha 를 4, Gray16/Alpha16 을 8 로 계산(또는 IHDR 비트 깊이·색 형식과 IDAT 앞 tRNS 를 직접 읽어 최악값). 팔레트·기타 모델도 디코더가 실제로 만드는 타입 기준 최악값. 회귀 시험: tRNS 달린 8/16비트 그레이 PNG 로 한도 경계 → 거부, 일반 PNG → 통과.

## 서버 보안 수정 워크플로우(2026-11-06) 이후 남은 틈 — 통합 단계에서 마무리

- 8-a. `docs/contract/api.md` 6장 요약표: `POST /v1/channels/{channel_id}/members` 의 추가 리미터 칸에 `RL_SOCIAL(POST)` 반영.
- 8-b. 그룹 DM 경쟁 시험이 o 의 RL_SOCIAL 토큰 30개 중 28개를 씀 → 시험 안에서 리미터를 넉넉히 하거나 사용자를 나눠 깨지기 쉬움 해소. 차단 게이트의 두 번째 절(기존/동시 멤버가 새로 들어오는 사람을 차단)을 결정적으로 시험.
- 9-a. `server/src/services/membership.ts:499` 강퇴/추방/탈퇴 시 그 사람이 만든 링크 폐기 문장에 고정 시험 없음(지워도 전체 통과) → "admin 이 링크 생성 → 강퇴 → 다른 링크로 재가입 → 다시 admin → 옛 링크 410" 시험 추가.
- 9-b. 방에 다른 경로로 이미 들어온 사람에게 걸린 대기 중 직접 초대 처리(검증자 메모 (b)) 확인·정리.
- 10-a. `insertFriendRequest`·`makeFriends` 의 0건 사유 판정이 트랜잭션 밖 재조회라 드물게 오류 코드가 다를 수 있음(쓰기는 안전) — 낮음, 가능하면 정리.
- 4-a. UserHub 이벤트 로그(최대 24시간/2000개)에 남은 정지 계정의 알림 프레임이 재개 시 재전송됨 — 차단과 같은 동작. 계약 api.md 3.9 에 "재생 로그는 소급 삭제하지 않음" 으로 명시하거나, 재생 시 정지·차단 대상 알림을 거르기(권장: 재생 시 거르기, 비용 작으면).

## 오너 결정 (2026-11-06) — 계약 갱신 + 구현 + 시험

- **D1. 정지 계정 프로필**: 본인 아닌 열람자에게 `GET /v1/users/{user}` 는 핸들·표시 이름·`suspended=true` 만 — 소개(bio)·상태 메시지·아바타·카운트는 숨김(null/0). 클라이언트 프로필 화면은 "정지된 계정" 표시. api.md 3.9·5.2 갱신.
- **D2. 그룹 DM 차단 쌍**: 생성·멤버 추가 거절 시 **누가 누구를 차단했는지 드러내지 않음** — 응답에서 차단 쌍의 id 목록 제거(`403 dm_not_allowed`, details 없음 또는 일반 문구). 나와 상대의 DM 허용 조건 불충족은 기존 규칙대로. api.md 0장 "차단 여부를 드러내지 않음" 과 일치하도록 5.x 갱신. 클라이언트 문구: "함께 대화할 수 없는 참여자가 있어요".
- **D3. 친구 끊기 → 직접 초대 취소**: 친구 끊기 시 두 사람 사이의 대기 중 방 직접 초대(양방향) 취소. 수락 시점에도 친구 관계 재확인(조건부 UPDATE 안에서). api.md 3.5 갱신.
- **D4. 집중 모드 시작 시각 정확히**: D1 `user_presence.focus_started_at` 열 추가(새 마이그레이션 파일, 기존 마이그레이션 수정 금지), `serializeMe`·presence 응답이 그 값을 사용. data-model.md·api.md 갱신.

## 오너 결정 (2026-11-06 10:39) — 출시

- **D5. 서버 주소**: Cloudflare 기본 주소 `https://masil.<계정 서브도메인>.workers.dev` 사용(별도 도메인 없음). 실제 서브도메인은 배포 시 확인해 `MASIL_DEFAULT_SERVER` 변수로 등록.
- **D6. 라이선스**: MIT. 루트 `LICENSE`(저작권자 patrache), goreleaser 아카이브·Homebrew formula 의 license 필드 `MIT`.

0-c. **[보안·중간] `internal/markup/render.go` `hyperlinkable` — 링크 표시 글자와 실제 주소 불일치(링크 위장)** (자동 보안 검토, 2026-11-06 14:11, integ-cli 0-b 수정 커밋)
   - `hyperlinkable` 이 ESC/BEL·Cf·공백만 거르고, `textwidth.Sanitize` 가 화면 표시에서 지우는 문자(VS15/16, U+1F3FB–1F3FF, U+20E3, U+E0020–E007F 태그 문자)나 규칙 7 이 바꾸는 문자는 통과 → 화면에 보이는 주소와 OSC 8 로 실제 열리는 주소가 달라질 수 있음.
   - 수정: `norm.NFC.String(u) == textwidth.Sanitize(u)` 일 때만 하이퍼링크(정규화 차이만 허용). 표시 글자는 항상 실제 대상 주소에서 만든다. 회귀 시험: 위 각 문자가 든 URL → 하이퍼링크 아님(일반 텍스트), 정상 URL·한글 경로 → 하이퍼링크.

## 0-d. WebP 디코드 전 비용 검사의 크기 불일치 (internal/imgrender/vp8lscan.go, cost.go) — 2026-11-08 자동 보안 리뷰
- vp8lscan: VP8L 헤더의 w/h로 서브이미지 디코드·`make([]byte, 4*w*h)`를 하는데, 이게 한도 검사 전에 일어난다. VP8X 캔버스 크기와 VP8L 헤더 크기가 달라도 막지 않는다.
- cost.go webpCost: 비용 추정에 쓰는 크기와 실제 디코더가 쓰는 크기가 다를 수 있다(parser differential).
- 고칠 것: vp8lHuffmanBytes가 기대 크기(cfg.Width/Height)를 받아 VP8L 헤더 크기가 다르면 오류(alphHuffmanBytes처럼, x/image/webp의 VP8X 검사와 동일). webpCost에서 VP8X가 있고 VP8L 크기와 다르면 바로 오류. 서브이미지 디코드 전 4*tileW*tileH가 남은 바이트 예산 안인지 확인. 회귀 시험: VP8X 작게+VP8L 크게, 서브이미지 타일 과대.
