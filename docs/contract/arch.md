# masil 아키텍처 설계 계약 (설계 JSON · 협업 객체 · 카드 데이터) v1

> **v0.1.3 새 문서 (P3)**: 아키텍처 설계 도구(도구 ID `arch`)의 설계 JSON v1(1장), CollabHub 문서 종류 `arch` 의 객체 사상(2장), 채팅 카드 `arch.design`·`arch.sim` 데이터(3장, P3 Task 26). Go 구현은 `internal/archdoc`, 서버 검증기는 `server/src/lib/collab/arch.ts`(P3 Task 21).

- 상태: 확정 계약 v1. 상위 출처: `docs/superpowers/specs/2026-11-09-masil-v0.1.3-design.md` C2.
- 짝 문서: `collab.md` 6장(`arch` 문서 종류 한도·거절 `detail`), `api.md`(카드 type 표).
- 고칠 때는 이 문서를 먼저 고치고, `internal/archdoc/codec.go` 의 필드 표와 서버 검증기를 같은 커밋에서 맞춘다.

---

## 1. 설계 JSON v1

개인 설계 파일(`~/.config/masil/arch/<이름>.json`), 가져오기·내보내기, 카드 사본이 이 형식이다. JSON 객체 하나, UTF-8.

### 1.1 정규 인코딩

- `nodes`·`boxes`·`edges`·`faults` 는 `id` 의 바이트 순 정렬, 들여쓰기 2칸, 끝에 개행 한 개, HTML 이스케이프 없음. 같은 설계 = 같은 바이트(`(*Design).Encode`).
- 모든 필드를 늘 쓴다(빈 목록은 `[]`). 예외: `traffic` 은 있는 노드(보통 `client`)에만.
- 모든 수는 **정수**로 쓴다(`5.0`·`1e3` 은 거부). NaN·무한대는 형식상 생길 수 없다.

### 1.2 읽기 규칙 (`archdoc.Decode`)

1. 1MB(1,048,576바이트) 초과 → `too_large`. 읽은 설계의 정규 인코딩(1.1)이 1MB 를 넘어도 `too_large` — 빠진 필드를 기본값으로 채우면 입력보다 커질 수 있고, 다시 읽히지 않는 설계를 받으면 안 되므로(저장·카드 사본·공유가 같은 한도를 쓴다). JSON 객체 하나가 아니거나 뒤에 다른 값이 있음 → `syntax`.
2. `schema_version` 이 1 이 아니면 → `schema_version`(다른 검사는 하지 않는다).
3. 모르는 필드는 무시한다(뒤 버전이 더할 수 있게). 빠진 필드는 그 객체의 기본값(노드는 `kind` 의 기본값 `archdoc.Defaults`, 연결은 `DefaultEdge`, `traffic` 은 `DefaultTraffic`, `duration_s` 는 300)을 쓴다. `null` 은 빠진 것과 같다.
4. 목록(`nodes`·`boxes`·`edges`·`faults`)이 1.3 한도보다 길면 그 목록의 항목은 하나도 읽지 않고 `too_many_*` 하나만 낸다(작은 파일이 수십만 개 객체·이슈를 만들지 못하게).
5. 타입이 틀리거나 정수가 아니거나 int64 를 넘는 값 → 그 경로에 `range`.
6. 그 뒤 검증(1.3). 문제가 하나라도 있으면 설계 전체를 거부한다(`*InvalidError{Issues}`, `errors.Is(err, ErrInvalid)`).

### 1.3 검증 (`(*Design).Validate`) — 이슈 코드

| 코드 | 뜻 | 경로 예 |
|---|---|---|
| `too_large` | 1MB 초과 | `""` |
| `syntax` | JSON 객체 하나가 아님 | `""` |
| `schema_version` | 버전이 1 이 아님 | `schema_version` |
| `too_many_nodes` · `too_many_edges` · `too_many_boxes` · `too_many_faults` | 300 · 1,000 · 100 · 50 초과 | `nodes` |
| `depth` | 상자 중첩 3단 초과, 또는 상자 부모 순환 | `boxes[3].parent` |
| `unknown_kind` | 열거값 밖(`kind`·`disk.type`·`params.lb_algo`·`traffic.curve`·`protocol`) | `nodes[0].kind` |
| `dangling_ref` | 없는 상자·노드·연결을 가리킴(`parent`·`params.failover_to`·`from`·`to`·`target`) | `edges[2].to` |
| `duplicate_id` | `id` 가 설계 안 다른 객체와 같음(종류를 가리지 않음) | `nodes[1].id` |
| `range` | 수 범위 밖, 타입 틀림, `id` 모양 틀림, 문자열 64바이트 초과, `label`·`title` 의 금지 문자(아래), `autoscale.enabled` 인데 `min > max` | `nodes[0].instances` |
| `label_too_long` | `label` 40 그래핌, `title` 80 그래핌 초과 | `nodes[0].label` |
| `parent_kind` | 상자 부모 규칙 위반: `region` 은 최상위만, `vpc` 는 최상위 또는 `region` 안, `az` 는 최상위 또는 `vpc` 안 | `boxes[2].parent` |

- `id` 는 `^[A-Za-z0-9_-]{1,62}$`(협업 객체 키 `n:<id>` 가 collab.md 1.2 의 64자 안에 들게). 새 ID 는 `<접두사>_<ULID>`(`n`·`b`·`e`·`f`).
- `label`·`title` 금지 문자: 제어 문자(U+0000–U+001F, U+007F–U+009F), 양방향 표시·내장·재정의·격리(U+061C, U+200E, U+200F, U+202A–U+202E, U+2066–U+2069), 줄·문단 구분(U+2028, U+2029). 결합 문자·ZWJ·이체 선택자는 허용(이모지 시퀀스). 서버 검증기(2.1)도 같은 집합을 거부한다.
- 노드는 아무 상자 안에나, 또는 최상위에 둔다. 장애 `target`: `node_off`·`instances_off` = 노드, `latency` = 노드 또는 연결, `az_off` = `az` 상자, `region_off` = `region` 상자.

### 1.4 필드

최상위: `schema_version`(1), `title`(문자열, 80 그래핌), `seed`(0..2⁶⁴−1, 새 설계는 2⁵³ 미만으로 뽑음 — JavaScript 수로 깨지지 않게), `duration_s`(60..600), `nodes`, `boxes`, `edges`, `faults`.

**노드** (`nodes[]`)

| 필드 | 타입 | 범위·값 |
|---|---|---|
| `id` | 문자열 | 1.3 |
| `kind` | 문자열 | `client` `cdn` `lb` `api` `worker` `cache` `db_primary` `db_replica` `queue` `object_storage` `search` `external` |
| `label` | 문자열 | 40 그래핌 |
| `pos` | `[x, y]` | 각 −100,000..100,000(세계 칸, 노드 크기 12×4) |
| `parent` | 문자열 | 상자 `id` 또는 `""` |
| `provider` · `instance_type` · `region` | 문자열 | 64바이트(값은 검사하지 않음 — 뜻이 있는 `provider` 는 `aws`·`gcp`·`azure`·`custom`·`""`). `region` `""` = 감싼 리전 상자를 따름 |
| `cpu_cores` | 정수 | 1..448 |
| `memory_mib` | 정수 | 128..24,576,000 |
| `instances` | 정수 | 1..1,000 |
| `autoscale.enabled` | 참·거짓 | |
| `autoscale.min` · `autoscale.max` | 정수 | 1..1,000 |
| `autoscale.target_cpu_permille` | 정수 | 1..1,000 |
| `autoscale.warmup_s` · `autoscale.cooldown_s` | 정수 | 0..3,600 |
| `nic_mbps` | 정수 | 1..400,000 |
| `disk.type` | 문자열 | `ssd` `ssd_fast` `hdd` |
| `disk.size_gb` | 정수 | 0..65,536 |
| `disk.iops` | 정수 | 0..256,000 |
| `cpu_micros_per_req` | 정수 | 1..60,000,000 (코어 1개 기준 요청당 CPU 시간 µs) |
| `timeout_ms` | 정수 | 1..600,000 |
| `retries` | 정수 | 0..10 |
| `queue_cap` | 정수 | 0..10,000,000 |
| `custom_hourly_micro_usd` | 정수 | 0..10¹² (사업자 `custom` 의 시간당 가격, 백만분의 1달러) |
| `params.hit_permille` | 정수 | 0..1,000 (CDN·cache 적중률) |
| `params.working_set_mib` | 정수 | 0..24,576,000 |
| `params.ttl_s` | 정수 | 0..31,536,000 |
| `params.lb_algo` | 문자열 | `round_robin` `least_conn` `""` |
| `params.health_interval_s` | 정수 | 0..3,600 |
| `params.read_iops_milli` · `params.write_iops_milli` | 정수 | 0..1,000,000 (요청당 IOPS × 1000) |
| `params.pool_max` | 정수 | 0..100,000 |
| `params.failover_to` | 문자열 | 노드 `id`(자기 자신 아님) 또는 `""` |
| `params.failover_s` | 정수 | 0..3,600 |
| `params.repl_lag_ms` | 정수 | 0..600,000 |
| `params.max_rps` | 정수 | 0..1,000,000,000 |
| `params.retention_msgs` | 정수 | 0..1,000,000,000 |
| `params.fixed_latency_ms` | 정수 | 0..600,000 |
| `params.error_permille` · `params.index_write_permille` | 정수 | 0..1,000 |
| `traffic.rps` | 정수 | 0..10,000,000 |
| `traffic.read_permille` | 정수 | 0..1,000 |
| `traffic.req_kb` · `traffic.resp_kb` | 정수 | 0..102,400 |
| `traffic.curve` | 문자열 | `constant` `ramp` `spike` `wave` |
| `traffic.spike_mult` | 정수 | 1..100 |
| `traffic.spike_at_s` · `traffic.spike_dur_s` | 정수 | 0..600 |

**상자** (`boxes[]`): `id`, `kind`(`region` `vpc` `az`), `label`(40 그래핌), `region_code`(64바이트), `rect`(`[x, y, w, h]`, x·y −100,000..100,000, w·h 1..100,000), `parent`(상자 `id` 또는 `""`).

**연결** (`edges[]`): `id`, `from`·`to`(노드 `id`), `protocol`(`http1` `http2` `grpc` `tcp` `async`), `mbps`(0..400,000, 0 = 양 끝 NIC), `latency_micros`(−1..60,000,000, −1 = 위상 기본값), `read_permille`·`write_permille`(0..1,000, 경로 비율).

**장애** (`faults[]`): `id`, `at_s`(0..600), `kind`(`node_off` `instances_off` `latency` `az_off` `region_off`), `target`, `count`(0..1,000), `add_ms`(0..600,000), `dur_s`(0..600, 0 = 끝까지).

기본값(빠진 필드): 노드는 `archdoc.Defaults(kind)`(API 요청당 CPU 8,000µs, 인스턴스 2, CPU 2, 메모리 4,096MiB, 디스크 `ssd` 100GB 3,000 IOPS, 타임아웃 2,000ms, 재시도 0, 대기열 1,000 등 — 부품별 값은 `design.go`), 연결은 `http1`·`latency_micros` −1·비율 1,000/1,000, 상자는 `rect` `[0, 0, 32, 18]`, 그 밖은 0·`""`.

---

## 2. 협업 객체 사상 (CollabHub `arch`)

공유 설계(`collab_docs.type = "arch"`)는 collab.md 1장의 객체 묶음이다. Go 는 `archdoc.Objects`(`map[객체 키]map[필드]JSON`), `(*Design).ToObjects`, `archdoc.FromObjects` 로 옮긴다.

### 2.1 객체

| 객체 키(`Op.id`) | `Op.type` | 필드 |
|---|---|---|
| `meta` | `meta` | `title`, `seed`, `duration_s` |
| `n:<노드 id>` | `node` | 1.4 노드 표에서 `id` 를 뺀 전부(`kind` 포함). `traffic.*` 는 트래픽이 있는 노드에만 |
| `b:<상자 id>` | `box` | `kind`, `label`, `region_code`, `rect`, `parent` |
| `e:<연결 id>` | `edge` | `from`, `to`, `protocol`, `mbps`, `latency_micros`, `read_permille`, `write_permille` |
| `f:<장애 id>` | `fault` | `at_s`, `kind`, `target`, `count`, `add_ms`, `dur_s` |

- 필드 이름은 1.4 의 JSON 이름을 점으로 편 것(`autoscale.min`, `disk.iops`, `params.hit_permille`, `traffic.rps`). 값은 1.4 와 같은 JSON 값.
- 위치는 한 필드 `pos`(`[x, y]`), 상자 사각형은 한 필드 `rect`(`[x, y, w, h]`) — 둘이 동시에 옮길 때 x 와 y 가 서로 다른 사람 값으로 섞이지 않게.
- 만들 때는 `put{id: 키, type, fields: 모든 필드}`, 고칠 때는 `set` 하나에 필드 하나, 지우기 `del`, 되살리기 `undel`. 노드를 지울 때 그 노드에 닿은 연결·장애도 같은 프레임에서 `del` 한다(못 해도 2.3 이 숨긴다).
- `id` 는 1.3 규칙(`^[A-Za-z0-9_-]{1,62}$`)이라 키는 collab.md 1.2 의 64자 안에 든다. 서버 검증기(`server/src/lib/collab/arch.ts`)의 객체 키 정규식은 정확히 `^(meta|[nbef]:[A-Za-z0-9_-]{1,62})$` 이다 — 새 ID 가 `n_01JA…` 처럼 대문자 ULID 이므로 P3 계획 Task 21 의 `^(meta|[nbef]:[0-9a-z_]{1,40})$` 는 이 줄로 대체된다.
- 서버의 `label`·`title` 값 검사는 1.3 과 같다: 그래핌 한도(40·80)와 1.3 금지 문자 집합. 클라이언트 `Validate` 를 통과한 설계는 공유(`arch.share`)에서 거부되지 않는다.

### 2.2 로컬 변경 (`archdoc.Change`)

| `ChangeKind` | collab op | 로컬 적용(`Objects.Apply`) |
|---|---|---|
| `ChangePut` | `put`(type = `ObjType(키)`) | 없으면 만들고, 있으면 준 필드만 덮음. `Value` = 필드 JSON 객체 |
| `ChangeSet` | `set` | 살아 있는 객체의 필드 하나. 지운·없는 객체면 버림. `Value` nil = 그 필드 지움(로컬만) |
| `ChangeDel` | `del` | 객체를 지움 |
| `ChangeRestore` | `undel` | 없을 때만 `Value`(필드 JSON 객체)로 되살림 — 서버는 필드를 갖고 있으므로 `undel` 만 보낸다 |

`DiffObjects(a, b)` 는 a 를 b 로 만드는 변경을 `(객체 키, 필드)` 순으로 낸다(새 객체 = `ChangePut`, 사라진 객체 = `ChangeDel`, 바뀐 필드 = `ChangeSet`).

### 2.3 관대한 적재 (`FromObjects`)

검증을 거치지 않은 공유 문서·오프라인 재전송·되돌리기가 만든 상태도 모든 클라이언트에서 **같은** 설계가 되게, 결정적으로 고친다(입력 순서와 무관 — 객체 키 정렬 순으로 읽는다). 고친 것마다 이슈(경로 = `<객체 키>.<필드>`)를 돌려준다.

1. 모르는 필드는 무시. 지워진 객체는 없는 것과 같다.
2. 필드 값의 타입이 틀리거나 범위 밖이면 그 필드만 기본값(노드 = `Defaults(kind)`, 연결 = `DefaultEdge`, 상자 = 2.1 의 기본) — `range`·`label_too_long`·`unknown_kind`. `autoscale.enabled` 인데 `min > max` 면 둘 다 기본값.
3. `kind` 가 모르는 값인 노드·상자·장애는 뺀다(`unknown_kind`).
4. `id` 는 종류를 가리지 않고 하나여야 하는데(1.3) `n:x` 와 `b:x` 는 서로 다른 협업 키다 — 같은 `id` 는 노드 → 상자 → 연결 → 장애 순으로 먼저 것만 남기고 뒤 것은 뺀다(경로 = 객체 키, `duplicate_id`). 그 뒤 한도 초과: 상자 100·노드 300 을 넘는 것은 ID 순 뒤에서 버린다(`too_many_*`).
5. 없는 상자를 가리키는 상자 `parent` 는 `""`(`dangling_ref`). 상자 부모 순환은 순환 안에서 **가장 작은 ID** 의 `parent` 를 `""` 로(`depth`). 그 뒤 3단보다 깊은 상자는 ID 순으로 하나씩, 3단 안에 들 때까지 조부모로 올린다(`depth`). 마지막으로 종류 규칙(1.3 `parent_kind`)을 어긴 상자(예: `vpc` 안 `vpc`, `region` 안 `az`)는 최상위로 옮긴다(`parent_kind`) — 최상위 `vpc`·`az` 는 설계 점검이 경고한다.
6. 없는 상자를 가리키는 노드 `parent`, 없는 노드·자기 자신을 가리키는 `params.failover_to` 는 `""`(`dangling_ref`).
7. 끝점이 없는 연결, 대상이 없거나 종류가 맞지 않는 장애는 뺀다(경로 = 객체 키, `dangling_ref`). 그 뒤 연결 1,000·장애 50 초과분을 ID 순 뒤에서 버린다.
8. 결과는 늘 1.3 검증을 통과한다(`TestFromObjectsAlwaysValid`). 단 1MB 정규 인코딩 한도(1.2)는 보장하지 않는다 — 내보내기·카드 사본은 `Encode` 크기를 따로 본다(`Store.Save` 처럼).

### 2.4 서버 검증 (`server/src/lib/collab/arch.ts`, P3 Task 21)

CollabHub 는 op 프레임마다 op 하나씩 이 검사를 거친 뒤 적용한다(collab.md 3.3 — 하나라도 거절되면 프레임 전체를 버리고 보낸 사람에게만 `reject`). 거절 `code` 는 P0 의 닫힌 집합(`invalid`·`limit_exceeded`) 안이고, 이유는 `detail` 이다. 필드 표·범위는 2.1·1.4 와 같고 TS 상수 하나(`FIELDS`)가 서버 쪽 사본이다.

| `detail` | `code` | 언제 |
|---|---|---|
| `arch_bad_object` | `invalid` | 객체 키가 `^(meta\|[nbef]:[A-Za-z0-9_-]{1,62})$` 밖, 또는 `put` 의 `type` 이 키 접두사와 다름(`meta`→`meta`, `n:`→`node`, `b:`→`box`, `e:`→`edge`, `f:`→`fault`). `set` 은 이미 있는 객체의 `type` 이 접두사와 달라도 이것 |
| `arch_bad_field` | `invalid` | 그 객체 종류에 없는 필드 이름(2.1 표 밖) |
| `arch_bad_value` | `invalid` | 1.4 의 JSON 타입·정수·범위·열거값 밖, 64바이트 문자열 초과, `label` 40·`title` 80 그래핌 초과, `label`·`title` 의 1.3 금지 문자, 짝 없는 서로게이트. `null` 은 받는다(필드를 비움 = 2.3 의 기본값) |
| `arch_cycle` | `invalid` | 상자 `parent` 를 바꾸면(`put`·`set`) 그 상자가 자기 조상이 됨 |
| `arch_depth` | `invalid` | 상자 `parent` 를 바꾸면 그 상자가 3단보다 깊어짐(위쪽 사슬만 본다 — 아래 상자까지 깊어지는 경우는 2.3 5단계가 고친다) |
| `arch_limit` | `limit_exceeded` | 새 객체 `put`(또는 지운 객체 `undel`)이 살아 있는 노드 300·연결 1,000·상자 100·장애 50·`meta` 1 을 넘김. 적용 뒤 한도 검사(P0, `detail:"limit_<type>"`)도 같은 `maxObjects` 로 따로 돈다 |

- `reset`(새 문서의 첫 스냅숏, 버전 복원)은 스냅숏 전체를 같은 규칙으로 본다(지운 객체는 개수에 안 셈). 새 문서의 스냅숏이 거절되면 `POST /v1/spaces/{id}/docs` 는 400 `fields.snapshot`.
- 가리키는 대상이 없는 참조(`parent`·`from`·`to`·`target`·`params.failover_to`)와 상자 종류 규칙(`parent_kind`)은 거절하지 않는다 — 동시 삭제로 늘 생길 수 있고 2.3 이 모든 클라이언트에서 같게 고친다.
- `maxDocBytes` 1,048,576 · `maxFieldBytes` 16,384 는 P0 검사 그대로(`too_large`).

### 2.5 댓글 앵커 (P3 Task 25)

방 문서의 댓글(api.md 5.24 `DocComment`, 서버는 P0)은 `anchor`(1..128) 문자열로 설계의 한 곳을 가리킨다. 서버는 모양을 보지 않고 저장만 한다 — 아래 규칙은 클라이언트끼리의 약속이다.

| 앵커 | 뜻 |
|---|---|
| `node:<id>` | 노드(`id` 는 1.3 규칙 `^[A-Za-z0-9_-]{1,62}$` — 협업 키의 `n:` 접두사가 아니라 설계 ID) |
| `edge:<id>` | 연결 |
| `box:<id>` | 상자 |
| `at:<x>,<y>` | 빈 자리의 세계 좌표(정수, 각 −1,000,000..1,000,000, 앞 `+`·앞자리 0 없는 정규 10진) |

- 그 밖의 모양(뒤 버전이 단 앵커 포함)은 지우지 않고 패널의 "위치 모름" 묶음에 둔다.
- 가리키는 노드·연결·상자가 지금 설계에 없으면(지움·동시 삭제) 댓글은 남고 "삭제된 노드에 단 댓글" 묶음에 보인다.
- 답글은 첫 댓글의 `anchor` 를 그대로 싣고 `root_id` 로 묶는다. 해결은 첫 댓글에 `PATCH {resolved}`.
- 캔버스의 `✎n`(설계서의 `💬n` — archdraw 골든이 이미 `✎` 로 고정, 1칸이라 작은 노드에도 들어감)은 해결 안 된 묶음에서 지우지 않은 댓글 수(노드 앵커만 — 연결·상자·빈 자리 댓글은 패널에만 보인다).

## 3. 채팅 카드 데이터 (`arch.design`·`arch.sim`, P3 Task 26)

`kind:"card"` 메시지(api.md 2 카드 규칙)의 `card.data`. 둘 다 `v` 1, 사용자가 보내고(`serverOnly: false`), 서버 검사기는 `server/src/lib/cards/arch.ts`(zod `strictObject` — 모르는 키는 `invalid`). `JSON.stringify(data)` 16,384바이트 초과 → `fields.card.data = "too_long"`, 모양 오류 → `"invalid"`, 모르는 `v` → `fields.card.v = "invalid"`(P0 공통 검사). 서버는 그리지도, 엔진을 돌리지도 않는다.

### 3.1 공통 필드

| 필드 | 모양 | 뜻 |
|---|---|---|
| `title` | 문자열 1..80 코드 포인트, 1.3 금지 문자·짝 없는 서로게이트 없음 | 설계 제목(클라이언트가 그래핌 경계에서 자르고 `…`) |
| `space_id` | ID \| `null` | 방 문서의 방 |
| `doc_id` | ID \| `null` | 방 문서 — `space_id` 와 함께만 |
| `version_no` | 정수 ≥ 1 \| `null` | 그 문서의 버전 번호(모르면 `null`) |
| `cost_usd_month` | 정수 ≥ 0 | 월 비용 추정(달러) |
| `price_asof` | `YYYY-MM` | 가격표 기준 연월 |
| `engine_version` | 정수 ≥ 1 | 숫자를 만든 엔진(`archsim.EngineVersion`) |
| `design` | 설계 JSON v1(1장, `schema_version: 1` 만 서버가 봄) \| `null` | 사본 — `doc_id` 가 `null` 일 때만. 개인 설계 카드는 늘 사본을 싣는다(받는 사람이 열 길이 그것뿐). 방 문서에서 만든 카드는 **절대** 사본을 싣지 않는다 — 문서의 방(`GetDoc`)을 아직 모르면 카드를 만들지 않고 `card_doc_loading` 안내 후 `GetDoc` 을 다시 부른다 |

### 3.2 type 별 필드

- `arch.design`: `node_count` 정수 0..300, `mini` 문자열 배열 ≤ 8(각 ≤ 60칸, 1.3 금지 문자 없음 — `archexport.Mini`; 서버의 칸 계산은 동아시아 넓은 글자·이모지 범위만 2칸으로 보는 근사).
- `arch.sim`: `seed` uint64 10진 문자열(`^(0|[1-9][0-9]{0,19})$`, ≤ 2^64−1), `max_rps`(분석의 클라이언트 초당 요청), `read_permille` 0..1000(첫 클라이언트의 읽기 비율), `p50_ms`·`p99_ms`, `error_permille` 0..1000 — 모두 정수 ≥ 0. `bottleneck` `{ target_label: 1..80, limit: cpu|iops|nic|pool|ext_limit|queue|link, util_permille ≥ 0 }` \| `null`. `challenge` `{ date: YYYY-MM-DD, scenario: ^[a-z0-9_-]{1,64}$, score ≥ 0 }` \| `null`(일일 도전, Task 28).

### 3.3 클라이언트

- `body`(대체 문구, 보낸 사람의 언어): `📐 <제목> · 노드 <n> · p99 <x>ms · 약 $<1,234>/월(추정)`, sim 은 `📐 <제목> 시뮬 · p99 <x>ms · 에러 <y.y>% · 약 $<1,234>/월(추정)`. 렌더러가 없는 클라이언트(0.1.2, 도구 꺼짐 아님 — 렌더러는 늘 등록)는 이것만 본다.
- 그리기: 받은 데이터를 다시 검사한다(위 범위 + `v == 1`). 하나라도 어긋나면 `body` 를 그린다. 문자열은 `Sanitize`, 미니 그림은 8줄·60칸으로 다시 자른다. 비용 줄은 자르지 않고 줄바꿈한다(`cost_short`). `engine_version` 이 지금 엔진과 다르면 `engine_mismatch` 를 덧붙인다. 위젯·초소형은 제목 한 줄.
- `enter`: 도구 꺼짐 → 스레드. `doc_id` 가 있고 그 방이 내 Store 에 있음 → `arch:doc:<id>`. 아니면 사본이 있음 → `arch:msg:<message_id>`(읽기 전용 사본, `copySink`; 재시작 뒤엔 `GET /v1/messages/{id}` 로 다시 읽음). 둘 다 아님 → 스레드.
- `R`(shift+r, `arch.sim` — `r` 은 채팅의 리액션이라 카드가 가져가지 않는다; 키맵이 묶은 키도 가져가지 않는다): 사본을 카드의 시드로 다시 분석해 `p50`·`p99`·에러·비용·`max_rps` 가 모두 같으면 `✓`, 아니면 `✕` 와 지금 숫자. 다시 돌릴 것이 없으면 키를 가져가지 않고(채팅에 넘김) 안내 줄에도 보이지 않는다.
- 보내기(`arch.send_card`): text·dm·group_dm 대화(최근 순) → 올라갈 카드 그대로 미리보기 → `POST /v1/channels/{id}/messages {kind:"card"}`. 사본까지 16KB 를 넘으면 보내지 않고 `card_too_big`("방에 올려 공유하세요").
