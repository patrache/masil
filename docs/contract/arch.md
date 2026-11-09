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

1. 1MB(1,048,576바이트) 초과 → `too_large`. JSON 객체 하나가 아니거나 뒤에 다른 값이 있음 → `syntax`.
2. `schema_version` 이 1 이 아니면 → `schema_version`(다른 검사는 하지 않는다).
3. 모르는 필드는 무시한다(뒤 버전이 더할 수 있게). 빠진 필드는 그 객체의 기본값(노드는 `kind` 의 기본값 `archdoc.Defaults`, 연결은 `DefaultEdge`, `traffic` 은 `DefaultTraffic`, `duration_s` 는 300)을 쓴다. `null` 은 빠진 것과 같다.
4. 타입이 틀리거나 정수가 아니거나 int64 를 넘는 값 → 그 경로에 `range`.
5. 그 뒤 검증(1.3). 문제가 하나라도 있으면 설계 전체를 거부한다(`*InvalidError{Issues}`, `errors.Is(err, ErrInvalid)`).

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
| `range` | 수 범위 밖, 타입 틀림, `id` 모양 틀림, 문자열 64바이트 초과, `autoscale.enabled` 인데 `min > max` | `nodes[0].instances` |
| `label_too_long` | `label` 40 그래핌, `title` 80 그래핌 초과 | `nodes[0].label` |
| `parent_kind` | 상자 부모 규칙 위반: `region` 은 최상위만, `vpc` 는 최상위 또는 `region` 안, `az` 는 최상위 또는 `vpc` 안 | `boxes[2].parent` |

- `id` 는 `^[A-Za-z0-9_-]{1,62}$`(협업 객체 키 `n:<id>` 가 collab.md 1.2 의 64자 안에 들게). 새 ID 는 `<접두사>_<ULID>`(`n`·`b`·`e`·`f`).
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
- `id` 는 1.3 규칙(`^[A-Za-z0-9_-]{1,62}$`)이라 키는 collab.md 1.2 의 64자 안에 든다.

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
4. 한도 초과: 상자 100·노드 300 을 넘는 것은 ID 순 뒤에서 버린다(`too_many_*`).
5. 없는 상자를 가리키는 상자 `parent` 는 `""`(`dangling_ref`). 상자 부모 순환은 순환 안에서 **가장 작은 ID** 의 `parent` 를 `""` 로(`depth`). 그 뒤 3단보다 깊은 상자는 ID 순으로 하나씩, 3단 안에 들 때까지 조부모로 올린다(`depth`). 종류 규칙(1.3 `parent_kind`)은 고치지 않는다 — 설계 점검이 경고한다.
6. 없는 상자를 가리키는 노드 `parent`, 없는 노드·자기 자신을 가리키는 `params.failover_to` 는 `""`(`dangling_ref`).
7. 끝점이 없는 연결, 대상이 없거나 종류가 맞지 않는 장애는 뺀다(경로 = 객체 키, `dangling_ref`). 그 뒤 연결 1,000·장애 50 초과분을 ID 순 뒤에서 버린다.
