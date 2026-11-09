# P0 batch "base" 실행 기록 (Task 1·2)

- 브랜치 `feat/v013-p0-base`, 2026-11-09.
- 계획: `docs/superpowers/plans/2026-11-09-masil-v013-P0.md` Task 1·2.

## Task 1 — Go 1.26.6 · 고정 의존성 · 제3자 고지

- 내 판단: `x/vt` v0.1.0 이 끌어오는 `golang.org/x/exp/ordered` v0.1.0 은 피할 수 없는 새 간접 의존성이라 그대로 받았고, goeditor v0.19.0 이 요구해 `regexp2/v2` 가 v2.2.1 → v2.7.1 로 올랐다(MVS). 기존 골든 파일은 바뀌지 않았다.
- 내 판단: ultraviolet 은 MVS 결과 v0.0.0-20261008173134-6b8d4baf91b4 그대로 남았다(올리지 않음).
- 내 판단: `scripts/notices.sh` 는 `go list -m all` 전부가 아니라 go.mod 의 require(직접·간접) + Go 표준 라이브러리만 싣는다. `go list -m all` 에는 의존성의 시험 전용 모듈(testify 등, 바이너리에 안 들어가고 내려받지도 않음)이 섞이기 때문이고, `TestThirdPartyNoticesCoversModules` 도 같은 범위를 검사한다.

## Task 2 — 계약 문서

- 내 판단: 이름 버전 101번째의 오류는 계획 본문의 "400" 대신 기존 계약 api.md 0.5 표(`limit_exceeded` = 422)를 따라 `422 limit_exceeded {what:"named_versions", limit:100}` 로 적었다. 0.5 의 `what` 목록에 `named_versions` 를 더했다.
- 내 판단: `alert-templates.json` 에는 계획 Task 8 Step 1 표 전체(16개)를 지금 넣었다 — `doc.comment_mention` 은 Task 29 에서 쓰이지만 표에 있어 함께 넣었다. body 값(none/verbatim)은 지금 서버 코드의 알림 body 와 대조해 표와 같음을 확인했다.
- 내 판단: api.md 6장 표 행은 Task 26·28·29 몫이라 건드리지 않았다(limiter 계약 시험이 6장만 대조). 0.7 표의 namespace 7111·7112·7113 은 계획 색인 그대로.
- 내 판단: `collab.md` 의 `put` 이 이미 있는(삭제 안 된) 객체에 오면 "준 필드만 덮어쓰고 나머지 필드와 `type` 은 그대로" 로 정했다(계획의 "이미 있으면 필드 덮어씀" 을 필드 LWW 에 맞춰 가장 좁게 읽음).
- 내 판단: 없는 객체에 대한 `set`·`del`·`undel` 은 오류 없이 버린다(no-op, 순번은 씀). 프레임 하나 = 순번 하나, 한 프레임 안에서는 뒤 op 가 이긴다.
- 내 판단: 스냅숏 정규형 = `objects` 를 `id` 바이트 순 정렬, `deleted` 는 true 일 때만 싣고, 삭제된 객체도 필드를 그대로 싣는다. 벡터의 `want.seq` 는 적용된 마지막 순번이고 거절 배치는 순번을 쓰지 않는다.
- 내 판단: 벡터 형식에 거절 배치 `{ops, reject:"invalid"}`(seq 없음)를 더해 "모르는 op 거절" 을 표현했다. 벡터는 공통 모양 검사만 보고 종류별 한도·`validateOp` 는 쓰지 않는다. 벡터 13개는 scratchpad 의 참조 reducer 로 교차 확인했다.
- 내 판단: `collab.md` 의 `reject` 순서에서 잠금(`locked`)·속도(`rate_limited`) 검사를 공통 단계(1단계)에 넣었다 — 계획은 공통 → `validateOp` → 한도만 적어, 그 밖 코드의 자리를 가장 앞으로 정함.
- 내 판단: i18n 설계서 6.4.3 은 계획대로 영역 목록에서 `claude.toml` 을 빼고 `tools.toml` 을 더했다(JX8 주석 한 줄).
