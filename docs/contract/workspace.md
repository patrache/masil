# masil 작업공간 공유 소켓 계약 (WorkspaceHub) v1

> **v0.1.3 새 문서 (2026-11-10, P2 Task 1)**: 작업공간 공유(설계서 B부)의 바이너리 프레임 소켓 `GET /v1/workspace-shares/{share_id}/ws` 의 헤더·프레임 종류·서버 라우팅 규칙·키 교환·봉인·편집 봉투·종료 코드·한도. Go 구현은 `internal/workspace/wire`(헤더·종류 표·조각), `internal/workspace/wscrypto`(키 교환·봉인·서명), `internal/workspace/ot`(편집 연산), 서버 구현은 `server/src/do/workspace-frame.ts`·`server/src/do/workspace-hub.ts`(P2 Task 13).

- 상태: 확정 계약 v1. 상위 출처: `docs/superpowers/specs/2026-11-09-masil-v0.1.3-design.md` B7.2·B8·B9·B10.
- 짝 문서: `api.md`(REST 7개), `events.md`(`workspace_share.*` 이벤트), `data-model.md`(`workspace_shares`·`workspace_share_members`), `collab.md`(같은 이름·뜻의 편집 봉투 — 바이트 형식은 이 문서).
- 서버(`WorkspaceHub` DO)는 **암호문 중계**다. 내용·경로·파일 이름·명령·화면을 저장하지도 기록하지도 않는다. 서버가 보는 것은 겉 헤더(이 문서 2장)와 `Class=Server` 프레임의 평문 JSON(단계표 같은 메타정보)뿐이다.
- 3장 표는 Go `internal/workspace/wire/kinds.go`·서버 `workspace-frame.ts` 의 `KIND_TABLE` 과 같아야 한다(양쪽 시험이 이 표를 읽어 맞춰 본다). 표의 모양(머리·열 순서·`` `이름` `` 표기)을 바꾸면 두 시험을 함께 고친다.

---

## 1. 연결

- `GET /v1/workspace-shares/{share_id}/ws` — 웹소켓 업그레이드. 토큰은 기존처럼 `Authorization: Bearer <token>` 헤더.
- 연결을 받는 사람: 그 공유의 오너(자리 0) 또는 내보내지지 않은 초대 멤버(자리 1..8). 공유가 없거나 끝났거나, 멤버가 아니거나, 멤버와 오너가 차단 관계면 업그레이드 전에 `404`.
- **바이너리 프레임만** 쓴다. 텍스트 프레임이 오면 서버는 `4000 protocol_error` 로 닫는다.
- 웹소켓 메시지 하나 = 프레임 하나. 큰 내용은 2장 조각 규칙으로 나눈다.

## 2. 헤더 배치

모든 프레임은 20바이트 겉 헤더 + 본문이다. 정수는 빅엔디언.

| 바이트 | 이름 | 뜻 |
|---|---|---|
| `[0]` | Version | `1` |
| `[1]` | Kind | 3장 표의 값 |
| `[2]` | Flags | `FlagMore = 1<<0`(조각이 이어짐), `FlagGroupStart = 1<<1`(키프레임 묶음 시작). 나머지 비트 0 |
| `[3]` | From | 보낸 자리 |
| `[4]` | To | 받는 자리 |
| `[5:8]` | 예약 | 언제나 `0` (아니면 해석 실패) |
| `[8:16]` | Seq | u64. 봉인 프레임은 방향별 카운터(6장), 평문 프레임은 0 |
| `[16:20]` | Len | u32. 헤더 **뒤** 바이트 수(암호문 + GCM 태그 16 + 서명 64 포함) |

- 자리: 오너 `SeatOwner = 0`, 참가자 `1..8`, 서버 `SeatServer = 254`, 모두 `SeatAll = 255`.
- 프레임 최대 크기 `MaxFrame = 262144`(256 KiB, **헤더 포함**). 넘으면 해석 실패.
- 해석 실패: 길이 20 미만 또는 `Len ≠ 실제 뒤 바이트 수`(`ErrLength`), 버전 ≠ 1(`ErrVersion`), 256 KiB 초과(`ErrTooLarge`), 예약 바이트 ≠ 0(`ErrReserved`).
- **조각**: 한 논리 메시지가 프레임 하나에 안 들어가면 같은 Kind·From·To 로 나눠 보낸다. 마지막 조각만 `FlagMore` 가 없다. 받는 쪽은 (보낸 자리, Kind) 마다 이어 붙이고, 한 보낸 자리의 모으는 중인 조각 합계가 64 MiB 를 넘으면 버리고 오류로 본다. 봉인 프레임은 조각마다 따로 봉인한다(조각 = 봉인 단위).
- 헤더 20바이트는 봉인의 AAD(추가 인증 데이터)다(6장). 서버가 헤더 한 바이트라도 바꾸면 받는 쪽에서 풀리지 않는다.

## 3. 프레임 종류

Class 뜻:
- `Server` — 평문 JSON. 서버가 만들거나(오너→서버 제어), 서버가 소비한다. 서버가 읽는 유일한 내용.
- `Handshake` — 평문 공개값(공약·공개키). 비밀이 아니다.
- `Broadcast` — 세션 키 `K_s` 로 봉인 + 오너 Ed25519 서명. 오너만 보낸다.
- `ToOwner` — 그 참가자의 P2O 키로 봉인. 참가자만 보낸다.
- `ToPeer` — 그 참가자의 O2P 키로 봉인. 오너만 보낸다.

MinLevel 열: 참가자가 그 종류를 오너에게 보낼 수 있는 최소 단계. `—` 는 참가자가 보내지 않는 종류(보내면 서버·오너가 버린다). `None` 은 단계가 없어도(참가 처리 중) 보낼 수 있다.

| 값 | 이름 | Class | 참가자가 보낼 때 MinLevel |
|---|---|---|---|
| 0x01 | `srv_hello` | Server | — (서버만) |
| 0x02 | `srv_peer` | Server | — |
| 0x03 | `ctl_levels` | Server | — (오너만) |
| 0x04 | `ctl_kick` | Server | — (오너만) |
| 0x05 | `ctl_end` | Server | — (오너만) |
| 0x06 | `ctl_attention` | Server | — (오너만) |
| 0x10 | `hs_commit` | Handshake | None |
| 0x11 | `hs_owner_pub` | Handshake | — (오너만) |
| 0x12 | `hs_reveal` | Handshake | None |
| 0x13 | `hs_grant` | ToPeer | — |
| 0x14 | `hs_decline` | Handshake | — |
| 0x15 | `hs_resume` | ToOwner | None |
| 0x16 | `hs_confirm` | ToOwner | None |
| 0x20 | `term_out` | Broadcast | — |
| 0x21 | `term_key` | Broadcast | — |
| 0x22 | `term_input` | ToOwner | Keyboard |
| 0x23 | `term_status` | Broadcast | — |
| 0x30 | `prop_submit` | ToOwner | Prompter |
| 0x31 | `prop_result` | ToPeer | — |
| 0x40 | `doc_open` | ToOwner | Viewer |
| 0x41 | `doc_state` | ToPeer | — |
| 0x42 | `doc_op` | ToOwner | Editor |
| 0x43 | `doc_ack` | ToPeer | — |
| 0x44 | `doc_reject` | ToPeer | — |
| 0x45 | `doc_bcast` | Broadcast | — |
| 0x46 | `doc_save` | ToOwner | Editor |
| 0x47 | `doc_presence` | ToOwner(참가자→오너) / Broadcast(오너→모두) | Viewer |
| 0x48 | `doc_close` | ToOwner | Viewer |
| 0x50 | `tree_req` | ToOwner | Viewer |
| 0x51 | `tree_list` | ToPeer | — |
| 0x52 | `fs_req` | ToOwner | Editor |
| 0x53 | `fs_result` | ToPeer | — |
| 0x60 | `roster` | Broadcast | — |
| 0x61 | `rekey` | ToPeer | — |
| 0x62 | `ended` | Broadcast | — |
| 0x63 | `self_level` | ToOwner | Viewer |
| 0x64 | `notice` | Broadcast | — |

`doc_presence` 는 보내는 쪽에 따라 Class 가 다르다(오너가 보내면 Broadcast, 참가자가 보내면 ToOwner — Go `wire.ClassFor(kind, from)`).

### 3.1 안쪽 메시지

봉인을 푼(또는 평문) 본문은 UTF-8 JSON 객체다. 필드 이름은 snake_case. `[]byte` 필드는 JSON 표준 base64. 단, 터미널 바이트 종류(`term_out`·`term_key`·`term_input`)의 평문은 JSON 이 아니라 `[1바이트 터미널 ID][원래 바이트]` 다.

| 종류 | 본문 |
|---|---|
| `srv_hello` | `{share_id, seat, owner_id, seats:[{seat, user_id}], max_frame}` — 연결 직후 서버가 보냄 |
| `srv_peer` | `{seat, user_id, state:"joined"\|"left"}` — 서버가 오너에게 |
| `ctl_levels` | `{levels:{"<자리>":"viewer"\|…}, verified:{"<자리>":bool}}` |
| `ctl_kick` | `{seat}` |
| `ctl_end` | `{}` |
| `ctl_attention` | `{}` |
| `hs_commit` | `{commit}` |
| `hs_owner_pub`·`hs_reveal` | `{pub}` |
| `hs_confirm` | `{match}` |
| `hs_grant` | `{epoch, session_key, sign_pub, level, keyboard_until?, verified}` |
| `hs_decline` | `{reason}` (`commit`·`declined`·`full`·`expired` 등) |
| `hs_resume` | `{}` |
| `roster` | `{members:[{seat, user_id, level, verified, keyboard_until?, connected}]}` |
| `rekey` | `{epoch, key}` |
| `ended` | `{reason}` |
| `notice` | `{code, seat, arg}` |
| `term_status` | `{term, title, cols, rows, attention, unsaved}` |
| `prop_submit` | `{id, text}` |
| `prop_result` | `{id, action}` |
| `tree_req` | `{dir}` |
| `tree_list` | `{dir, entries:[{name, dir, size}], more, error?}` |
| `doc_open` | `{doc, pending?}` (`pending` = 재접속 때 아직 받음을 못 받은 내 `client_op_id`) |
| `doc_save`·`doc_close` | `{doc}` |
| `doc_state` | `{doc, seq, text, read_only, reason?, error?, pending_seq?}` (`pending_seq` = `doc_open.pending` 이 이미 순번을 받았으면 그 순번) |
| `doc_op` | `{doc, client_op_id, base_seq, ops}` |
| `doc_ack` | `{doc, client_op_id, seq}` |
| `doc_reject` | `{doc, client_op_id, code}` |
| `doc_bcast` | `{doc, seq, author, ops}` (`author` = 자리, 디스크 = `-1`) |
| `doc_presence` | `{doc, seat, cursor, sel_from, sel_to}` |
| `fs_req` | `{op:"create"\|"rename"\|"delete", from, to}` |
| `fs_result` | `{op, from, to, error?}` |
| `self_level` | `{level}` (내리기·나가기 = `""`) |

단계 이름: `""`(없음), `viewer`(보기), `prompter`(제안), `editor`(편집), `keyboard`(키보드). 순서는 이 차례대로 높아진다.

## 4. 서버 라우팅 규칙

1. 텍스트 프레임·헤더 해석 실패·`From ≠ 소켓 자리` → `4000`.
2. `Class=Server` 종류는 자리 0(오너)만 보낼 수 있고 DO 가 소비한다(전달 안 함). `ctl_levels` → 단계표·확인 여부 저장(DO storage) + D1 `max_level`·`verified` 갱신 + `workspace_share.updated` 발행 + 새로 단계가 생긴 자리에 키프레임 캐시 재생. `ctl_kick` → `/kick` 과 같음. `ctl_end` → `endShare(…, "ended")`. `ctl_attention` → 30초에 한 번까지 오너에게 `workspace_attention` alert(`template {id:"workspace.attention", params:{share:title}}`, target `workspace_share`).
3. 오너가 보낸 `To=255` → 단계가 있는 참가자 모두, `To=n` → 그 자리만. 참가자가 보낸 프레임은 `To=0` 만(아니면 버림) + 그 종류의 `MinLevel` 이 그 자리 단계 이하일 때만 오너에게(2차 방어선).
4. 오너가 보낸 `term_key` 에 `FlagGroupStart` 가 있으면 캐시를 그 프레임부터 새로 시작, 이어지는 `term_key`·`term_out` 을 덧붙인다(8 MiB 넘으면 다음 묶음 시작까지 캐시를 비움). 캐시는 메모리에만.
5. 연결 시 서버가 `srv_hello`(평문 JSON) 를 보내고, 자리마다 들고 남 때 오너에게 `srv_peer`.
6. 한도(연결별 1초 창): 참가자 `term_input` 50/초, `doc_op` 30/초, `prop_submit` 10/분, 오너 바이트 2 MiB/초. 넘는 프레임은 버리고 3초 연속 넘으면 `4008`.
7. 오너 소켓이 닫히면 alarm `now+120s`; 오너가 다시 붙으면 alarm 취소. alarm → `endShare(…, "timeout")` + 모두 `4100`.
8. D1 `joined_at`·`left_at` 은 연결·종료 때 갱신.

- 참가 처리 중(단계 없음)인 참가자의 프레임은 MinLevel `None` 종류(`hs_commit`·`hs_reveal`·`hs_resume`·`hs_confirm`)만 오너에게 간다. 오너가 참가 처리 중인 자리에 보내는 `To=n` 프레임(`hs_owner_pub`·`hs_grant`·`hs_decline`)은 단계와 상관없이 그 자리에 간다.
- DO 내부 경로: `/connect`(업그레이드, 같은 사용자 기존 소켓은 `4102` 로 닫음), `POST /end {reason}`(모두 `4100`), `POST /kick {user_id}`(그 소켓 `4101`, 단계표에서 지움).

### 4.1 세부 규칙 (P2 Task 13 구현)

- `srv_hello.seats` = 그 순간 붙어 있는 소켓의 자리(오너 포함, 자리 순). `max_frame` = `262144`.
- `srv_peer`: 참가자 소켓이 붙으면 `joined`, 그 사용자의 마지막 소켓이 닫히거나 내보내지면 `left`. 같은 사용자의 새 소켓이 옛 소켓을 밀어낸 때(`4102`)는 `left` 를 보내지 않는다.
- `ctl_levels` 는 **표 전체**다 — 빠진 자리는 단계 없음. 자리 키가 `1`..`8` 이 아니거나 단계 이름이 목록 밖이면 그 프레임 전체를 버린다. D1 `max_level` 은 지금까지 준 단계 중 가장 높은 것(내려도 그대로), `verified` 는 한 번 참이면 참. D1 행이 실제로 바뀐 때만 `workspace_share.updated` 를 보낸다. 키프레임 재생은 단계가 없다가 생긴 자리에만 한 번.
- 키프레임 캐시는 오너가 `To=255` 로 보낸 `term_key`(묶음 시작)·`term_out` 만 담는다. `To=n` 프레임과 다른 종류는 담지 않는다.
- `ctl_kick {seat}` 은 그 자리 멤버를 D1 에서도 내보낸다(`removed_at`, REST 내보내기와 같음 — 다시 붙지 못함). `ctl_end` 는 `endShare(…, "ended")`.
- 버리는 것(닫지 않음): 표에 없는 종류, 참가자가 보낸 `Class=Server` 종류, 오너가 보낸 `srv_*`, 오너가 보낸 `To=0`·`To=254`(`ctl_*` 제외)·`To=9..253`, 참가자가 보낸 `To≠0`, 단계가 모자란 참가자 프레임, 해석할 수 없는 `ctl_*` JSON.
- 한도 계수기는 연결마다 메모리에 있고, 넘은 초가 연달아 3번이면(넘지 않은 초가 끼면 다시 1부터) `4008`. 오너 바이트 한도는 오너가 보내는 모든 프레임 바이트를 센다.
- 오너의 마지막 소켓이 닫힐 때 alarm 을 건다(다른 오너 소켓이 남아 있으면 걸지 않음). 끝난 공유(REST·`ctl_end`·alarm)는 DO 저장소에 끝남 표시를 남겨 그 뒤의 `/connect` 를 `404` 로 거절한다.

## 5. 키 교환 순서·HKDF 정보값·확인 숫자

모두 Go 표준 라이브러리: `crypto/ecdh`(X25519), `crypto/hkdf`(HKDF-SHA256), `crypto/sha256`, `crypto/rand`. 아래 `‖` 는 바이트 이어 붙이기, `0x00` 은 한 바이트 0.

- 키쌍: 참가 시도마다 양쪽이 새 임시 X25519 키쌍을 만든다(공개값 32바이트). 메모리에만 둔다.
- 공약: `Commit(shareID, pub) = SHA-256("masil-ws-commit-v1" ‖ 0x00 ‖ shareID ‖ 0x00 ‖ pub)`(32바이트). 확인은 상수 시간 비교.
- 순서:
  1. 참가자 → 오너 `hs_commit{commit}` (참가자 공개값의 공약만)
  2. 오너 → 참가자 `hs_owner_pub{pub}` (이 참가자 전용 오너 임시 공개값)
  3. 참가자 → 오너 `hs_reveal{pub}` (참가자 공개값)
  4. 오너가 `VerifyCommit` 실패면 `hs_decline{reason:"commit"}` + 기록, 끝.
  5. 양쪽 `Derive`.
  6. 두 사람이 확인 숫자를 비교. 참가자 → 오너 `hs_confirm{match}` (P2O 봉인).
  7. 오너 → 참가자 `hs_grant{epoch, session_key, sign_pub, level, keyboard_until?, verified}` (O2P 봉인).
- `Derive(local, peerPub, transcript)`:
  - `shared = X25519(local, peerPub)`. 32바이트가 모두 0 이면 `ErrWeakKey` 로 거절.
  - `info = "masil-ws-v1" ‖ 0x00 ‖ ShareID ‖ 0x00 ‖ OwnerID ‖ 0x00 ‖ ParticipantID ‖ 0x00 ‖ OwnerPub ‖ ParticipantPub`
  - `okm = HKDF-SHA256(secret = shared, salt = 없음, info, 길이 68)`
  - `O2P = okm[0:32]`, `P2O = okm[32:64]`, `sas = okm[64:68]`
  - `n = BigEndian.Uint32(sas) % 1000000`, 확인 숫자 = `printf("%03d %03d", n/1000, n%1000)` (예 `042 917`)
- 공약 덕분에 중간자는 숫자를 맞추려고 공개값을 여러 번 바꿔 볼 수 없다(참가자가 공개값을 정한 뒤에야 오너 공개값을 본다).
- 확인 생략(`확인 없이 허용`)은 보기·제안 단계에만 허용되고 `verified=false` 로 표시한다. 편집·키보드는 확인 필수.
- 짧은 끊김 뒤 재접속: 같은 공유·같은 실행 안이면 참가자가 남아 있는 P2O 키로 `hs_resume{}` 를 봉인해 보낸다(카운터는 이어서). 오너가 풀리면 숫자 비교 없이 이어진다. masil 을 다시 켜면 처음부터.

**시험 벡터** (`internal/workspace/wscrypto/testdata/vector.txt` 의 두 줄 = SAS·O2P, Python `cryptography` 로 따로 계산해 맞춰 봄): 오너 X25519 비밀 스칼라 = 바이트 `01 02 … 20`(1..32), 참가자 = `21 22 … 40`(33..64), `ShareID="S1"`, `OwnerID="U_O"`, `ParticipantID="U_P"` 일 때

```
SAS = 801 609
O2P = 0f19574e010dac772aabb7ba48fab7ff83a2481f3a4be61a70b074c85ae3f7a0
P2O = accd2bbf46cd31e372b0157adbd157efc0bd33146868b07cb31e90976a379bf9
```

## 6. 봉인·논스·서명

AES-256-GCM(`crypto/aes`+`crypto/cipher`), Ed25519(`crypto/ed25519`). 압축하지 않는다.

- 키:
  - `K_s` — 세션 키(무작위 32바이트, epoch 번호와 함께). 오너가 공유 시작 때 메모리에만 만들고 `hs_grant`·`rekey` 로 나눠 준다.
  - `O2P`·`P2O` — 참가자마다 5장에서 뽑은 방향별 키.
  - 오너 서명 키 — 공유 전용 임시 Ed25519. 공개키는 `hs_grant.sign_pub`.
- 방향과 키:

  | Class | 키 | 방향 값 `dir` |
  |---|---|---|
  | `Broadcast` | `K_s` + 오너 서명 | `0` |
  | `ToPeer` | 그 참가자의 `O2P` | `1` |
  | `ToOwner` | 그 참가자의 `P2O` | `2` |

- 논스 12바이트 = `dir(1) ‖ 0x00 0x00 0x00 ‖ seq(8, BE)`. `seq` 는 (키, 방향)마다 1 부터 하나씩 늘어나는 카운터이고 헤더 `Seq` 에 그대로 싣는다.
- 봉인: 보내는 쪽이 헤더의 `Seq = 다음 카운터`, `Len = 평문 길이 + 16 + extra`(`extra` = 서명이면 64, 아니면 0)를 채운 뒤, AAD = 그 헤더 20바이트로 GCM 봉인. 프레임 = 헤더 ‖ 암호문(태그 포함) [‖ 서명].
- 오너 → 모두(`Broadcast`): `K_s` 로 봉인(extra 64) 뒤 서명. 서명 대상 = 헤더 ‖ 암호문 전체, 서명 64바이트를 끝에 붙인다. 받는 쪽은 서명부터 검사하고(`ErrSignature`), 끝 64바이트를 떼어 GCM 을 연다. 참가자는 모두 `K_s` 를 알기 때문에 서명 없이는 참가자 하나가 오너인 척할 수 있다.
- 받는 쪽 되풀이 거절: (키, 방향)마다 마지막으로 연 `seq` 를 기억하고 `seq ≤ 마지막` 이면 `ErrReplay` 로 버린다. GCM 실패는 `ErrAuth`. 실패해도 마지막 `seq` 는 바뀌지 않는다.
- 복호화·서명 실패 프레임은 버리고 기록한다. 짧은 시간에 반복되면 오너에게 경고하고 공유를 멈출지 묻는다.
- 키 교체: 카운터가 `RekeyAfter = 2^40` 에 이르면, 그리고 내보내기·키보드 회수 때 오너가 새 `K_s`(epoch + 1)를 만들어 남은 참가자에게만 `rekey{epoch, key}`(O2P 봉인)로 보낸다. 새 epoch 의 `Broadcast` 카운터는 1 부터 다시. 내보낸 참가자는 새 epoch 프레임을 풀지 못한다.

## 7. 편집 봉투 (OT)

문서마다 오너 PC 가 순번을 매기는 곳(Sequencer)이다. 이름·뜻은 `collab.md` 의 협업 프레임과 같다(`client_op_id`, `base_seq`, `seq`, 받음·거절).

- 위치 단위 = 유니코드 문자(rune). 줄 끝은 `\n` 으로 다루고 저장 때 원래 방식으로 되돌린다. 바이트 정규화(NFC 등)는 하지 않는다.
- `ops` 형식(ot.js `TextOperation` 과 같은 JSON 배열): 양의 정수 = 그만큼 그대로 두기(retain), 음의 정수 = 그만큼 지우기(delete), 문자열 = 넣기(insert). 예 `[3,"ab",-2,5]` = 앞 3글자 두고 `ab` 넣고 2글자 지우고 5글자 둠. 연산의 retain+delete 합 = 적용 전 문서 길이. 읽을 때 0·빈 문자열은 버리고 이웃한 같은 종류는 합친다.
- 흐름:
  - 참가자는 편집을 먼저 자기 화면에 적용하고 `doc_op{doc, client_op_id, base_seq, ops}` 를 보낸다. `base_seq` = 그 편집이 기준으로 한 확정 순번. **한 번에 하나만** 확정을 기다리고, 그동안 친 것은 하나로 합성해 다음에 보낸다.
  - 오너는 권한(편집 단계 이상, 만료 안 됨)을 보고, `base_seq` 이후 확정된 편집들에 맞춰 변환한 뒤 다음 순번을 매긴다. 보낸 사람에게 `doc_ack{doc, client_op_id, seq}`, 모두에게 `doc_bcast{doc, seq, author, ops}`(변환된 연산). 보낸 사람은 자기 `doc_bcast` 를 무시하고 `doc_ack` 로 확정한다.
  - 거절은 `doc_reject{doc, client_op_id, code}` — `code`: `stale_base`(오너가 보관한 기록 5,000개보다 오래됨), `future_base`, `base_len`, `level`(권한 없음), `read_only`, `too_large`. 거절받은 쪽은 `doc_open` 으로 `doc_state` 를 다시 받아 대기·버퍼를 버리고 그 상태에서 다시 시작한다.
  - 같은 위치 넣기 동률: 먼저 순번을 받은 쪽이 앞에 온다.
  - `client_op_id` = `접두어-무작위16진수16자리-카운터`. 무작위 부분은 편집 상태(`ot.Client`)를 새로 만들 때마다 새로 뽑는다(`crypto/rand`). 그래서 같은 참가자가 문서를 닫았다 다시 열거나 masil 을 다시 켜도, 오너가 아직 기억하는 예전 id 와 겹치지 않는다. id 는 **보낸 자리 안에서만** 유일하면 된다.
  - **멱등**: 오너는 문서마다 보관 기록(5,000개) 안의 (보낸 자리, `client_op_id`) 쌍을 기억한다. 보낸 자리는 메시지 안의 값이 아니라 오너가 아는 그 소켓의 자리(헤더 `From`, 서버가 확인함)다. 같은 자리에서 이미 순번을 받은 `client_op_id` 가 다시 오면 상태를 바꾸지 않고 처음 `seq` 로 `doc_ack` 만 다시 보낸다. 다른 자리가 같은 문자열을 보내면 별개의 편집이다(남의 id 를 흉내 내 남의 편집을 막을 수 없다).
  - 재접속: 참가자는 대기 중인 편집의 `client_op_id` 를 `doc_open{doc, pending}` 에 싣고, `doc_state` 가 올 때까지 아무것도 보내지 않으며(친 것은 버퍼에 모음) 그사이 오는 `doc_ack`·`doc_bcast` 는 버린다. 오너는 `doc_state{…, pending_seq}` 로 답한다(그 편집이 이미 순번을 받았으면 그 순번, 아니면 0). 참가자는 받은 `text` 와 마지막으로 확정했던 문서의 차이(Diff)에 맞춰, `pending_seq = 0` 이면 대기 + 버퍼를, 아니면 버퍼만 변환해 `text` 위에 다시 얹고 새 `client_op_id` 의 `doc_op` 로 보낸다. 그래서 받음을 잃은 편집이 두 번 들어가지 않는다.
  - 디스크 변경은 오너가 차이를 편집으로 바꿔 `author = -1` 로 순번을 매긴다(저장 안 된 편집과 3방향 합치기).

## 8. 종료 코드

| 코드 | 이름 | 언제 | 클라이언트 동작 |
|---|---|---|---|
| 4000 | protocol_error | 텍스트 프레임, 헤더 해석 실패, `From ≠ 소켓 자리`, 256 KiB 초과 | 다시 붙지 않고 오류를 보여 줌 |
| 4008 | rate_limited | 프레임 한도 초과가 3초 이어짐 | 10초 뒤 재접속 |
| 4100 | share_ended | 오너가 끝냄·오너 2분 넘게 끊김·다른 기기에서 끝냄 | 재접속 금지, `공유가 끝났어요` |
| 4101 | kicked | 오너가 내보냄 | 재접속 금지 |
| 4102 | replaced | 같은 사용자가 다른 소켓으로 붙음 | 재접속 금지(새 소켓이 이어받음) |

## 9. 한도

| 항목 | 값 |
|---|---|
| 프레임 최대 | 256 KiB(헤더 포함), 조각 모으기 보낸 자리당 64 MiB |
| 참가자 | 공유당 최대 8명, 사용자당 동시 공유 3개 |
| DO 프레임 한도(연결별) | 참가자 `term_input` 50프레임/초, `doc_op` 30프레임/초, `prop_submit` 10건/분, 오너 출력 2 MiB/초. 넘으면 버리고 3초 연속이면 `4008`. 오너 클라이언트도 같은 한도를 따로 지킨다 |
| 오너 끊김 유예 | 2분 뒤 공유 끝(`timeout`) |
| 참가 요청 | 5분 만료 |
| 공동 편집 | 문서 ≤ 512 KiB, 읽기 전용 512 KiB 초과 ~ 2 MiB, 2 MiB 초과·바이너리(앞 8 KiB 에 NUL 이 있거나 전체가 올바른 UTF-8 이 아님)는 열지 않음, 줄 끝 섞임은 읽기 전용, 동시 열린 문서 20 |
| 외부 변경 확인 | 열린 문서의 디스크 변경을 0.5초마다 확인(크기·수정 시각이 다르면 해시로 실제 변경만) |
| 자동 저장 | 편집이 멈춘 지 2초 뒤(오너가 켬, 기본 끔) |
| 트리 | 폴더당 5,000 항목(넘으면 `more` 에 나머지 수) |
| 제안 | 4,000자, 대기 20건, 사람당 1분 10건 |
| 키보드 | 10분 / 30분(기본) / 1시간 / 2시간(최대). 오너 15분 무입력 → 일시 정지. 참가자 60초 넘게 끊김 → 편집 단계로 |
| 터미널 출력 | 50ms 또는 32 KiB 묶음, 키프레임 10초마다·새 참가자 때, 참가자 스크롤백 5,000줄, DO 키프레임 캐시 8 MiB |
| 편집 기록 | 문서당 5,000개 보관(그보다 오래된 `base_seq` 는 `stale_base`) |
| 키 교체 | 방향별 카운터 `2^40` |
| 오너 PC 기록 | 기록 파일(jsonl) 30일 뒤 지움 |
| 끝난 공유 | `status='ended'` 이고 끝난 지 30일 지난 D1 `workspace_shares` 행을 지움(멤버 행은 CASCADE) |
