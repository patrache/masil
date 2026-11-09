# v0.1.3 보안 지적 수정 — p4b (internal/ui/apitool, internal/apitool/shared.go)

출처: 자동 커밋 보안 검토 기록(sec-findings.md)의 (p)(q)(s)(u)(y)(z)(aa)(ab)(ac) + internal/ui/apitool 정화 훑기.

## 상태

- (p) 이미 고침 — `kvtable.go:329` `display` 는 `t.editing && i == t.cur && col == t.col` 일 때만 편집 버퍼를 보이고, 비밀 값은 그 밖에서 늘 가림. 시험 `TestKVTableSecretMaskedWhileEditingKey`.
- (q) 이미 고침 — 편집 버퍼는 `SanitizeInput` 으로 채우고(`kvtable.go:210` `startEdit`) 그리기는 `Sanitize`. 시험 `TestKVTableEditSanitized`.
- (s) 고침 — 모르는 `Body.Kind` 를 탭 줄에 날것으로 그리던 `bodyKindLabel` 정화, 비밀 값 가림(`maskSecret`)의 끝 4글자 정화(환경 화면은 날것이었음), `CollectionFromDoc` 이 서버 열거 밖의 `body_kind` 를 none 으로 읽음. 메서드는 이미 `methodOf` 가 정화(50daa541 전후 P4 검토). 시험 `TestHostileCollectionSanitized`, `TestHostileEnvScreenSanitized`, `TestCollectionFromDocUnknownBodyKind`.
- (u) 고침 — 기본 폴더 `~/Downloads`·앞 점 제거·경로 조각 제거·덮어쓰기 전 확인은 이미 있었고(50daa541), 남은 것은 덮어쓰기의 `O_TRUNC` 가 심볼릭 링크를 따라가던 것. 시험 `TestSaveOverwriteDoesNotFollowSymlink`.
- (y) 고침 — 편집 경로의 "그래도 올리기" 가 보여 준 첫 칸 하나만 보내고 다음 칸을 다시 묻는다(올리기 경로는 이미 칸마다 물음, fdbb3cc5). 시험 `TestHeldShareAnywayOnlyShown`.
- (z) 이미 고침 — `heldToVar` 가 바꾼 연산을 `GateIn` 으로 다시 거르고 걸리면 다시 묻는다(471a0906). 시험 `TestHeldToVarDistinctSecrets`.
- (aa) 고침 — 부모 사슬 선형 처리는 이미 있었고(c4eb7e0c), 폴더 수·깊이 상한을 더함. 시험 `TestCollectionFromDocFolderCaps`, `TestSharedLimitsFolders`, `TestCollectionFromDocFolderChain`(상한에 맞게 기대값 변경).
- (ab) 이미 고침 — `GateIn(base, ops)` 가 행 key·변수 name 을 base/연산에서 찾아 값 검사에 넘김(a90f3b59), UI 는 `GateIn(sd.col, …)` 사용. 시험 `TestGateNamesSetValues`, `TestHeldSetOfExistingRowValue`.
- (ac) 고침 — 아래 내 판단.

## 내 판단

- 내 판단: (s) 공유 문서의 메서드가 HTTP 토큰이 아니어도 `CollectionFromDoc` 은 고쳐 쓰지 않는다 — GET 으로 바꾸면 동료의 깨진 요청이 조용히 보낼 수 있는 GET 이 된다. 보내기 전 `Prepare` 가 `ErrBadMethod` 로 막고 화면은 모두 정화한다. 서버 열거 밖 `body_kind` 는 none 으로 읽는다(서버가 이미 거부하는 값). 가져오기(convert)·로컬 파일 읽기(local.go)의 검사는 내 영역 밖이라 하지 않았다 — 화면 정화와 `Prepare` 로 막힌다.
- 내 판단: (u) 덮어쓰기는 같은 폴더에 0600 임시 파일(`.masil-save-*`)을 만들어 채운 뒤 `os.Rename` 으로 경로를 바꾼다 — 그 자리의 심볼릭 링크는 링크 자체가 바뀌고 가리키던 파일은 그대로다(Windows 의 `os.Rename` 도 바꿔 씀). 새 파일 쓰기가 실패하면 만든 파일을 지운다(전에는 반쯤 쓴 파일이 남았음).
- 내 판단: (y) 편집 경로 질문의 "취소"(esc 포함)와 잘못된 변수 이름은 전처럼 이번 편집의 걸린 칸 **모두**를 이 PC 에만 둔다 — 아무것도 나가지 않으니 동의 우회가 아니고, 칸마다 다시 묻는 것보다 덜 번거롭다.
- 내 판단: (aa) 상한은 폴더 1,000개(요청 상한과 같음), 깊이 32. 동료 문서가 넘으면 거부하지 않고 상한 안으로 읽는다 — 폴더 id 순으로 앞 1,000개만 두고 나머지의 항목은 맨 위로, 32단을 넘는 폴더는 맨 위에서 새 사슬을 시작. 내 편집·올리기가 넘으면 `CheckSharedLimits` 가 거부(화면은 기존 `shared_too_large` 토스트).
- 내 판단: (ac) 설정의 `insecure` 는 계속 공유한다(스펙 C3.7 의 요청 설정, 요청 줄 경고 그대로). 대신 공유 요청이 **남의 것**(foreignInfo: 동료가 마지막으로 바꿨거나 누가 바꿨는지 모름)이고 인증서 검사가 꺼져 있으면 첫 보내기 전에 `insecure_peer`/`insecure_unknown` 으로 묻고(검사 없이 보내기/취소), 승낙은 private_target 과 같은 열쇠(문서/요청/그 버전)로 이번 실행 동안 기억한다 — 동료가 그 요청을 다시 바꾸면 다시 묻는다. 내가 설정 탭에서 직접 끄면 승낙으로 친다. 실행기(컬렉션 실행)는 묻지 않고 승낙 없는 요청을 **검사를 켠 채** 보낸다(자체 서명 인증서면 TLS 실패로 보임). 비밀 변수 사용 여부와 무관하게 묻는다 — 비밀이 없어도 응답을 믿게 되는 문제는 같다.
- 내 판단: 훑기 — internal/ui/apitool 의 그리기 경로(편집기 6탭·URL 줄 두 갈래·트리·초소형·환경 화면·대화창·실행기·응답 탭·카드)를 확인했다. 대화창 본문·선택지·제목, 트리 행, 응답 머리·검사 줄, 실행기 이름, 카드는 이미 정화. 새로 고친 곳은 본문 종류 이름과 비밀 가림 끝 4글자 두 곳.
