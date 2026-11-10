// Doc type `arch` (docs/contract/collab.md 6, docs/contract/arch.md 2): the architecture design document. The object
// keys, field names and value ranges below are the single TS copy of arch.md 2.1 / 1.4 (the Go side is
// internal/archdoc/codec.go); change arch.md first, then both tables in the same commit.
//
// Every refusal is { code: "invalid" | "limit_exceeded", detail } (arch.md 2.4):
//   arch_bad_object  object key outside ^(meta|[nbef]:[A-Za-z0-9_-]{1,62})$, or a put whose type is not the key's
//   arch_bad_field   a field name the object type does not have
//   arch_bad_value   a value of the wrong JSON type, out of range, a string over its byte or grapheme cap, or a
//                    label / title holding a forbidden character (arch.md 1.3)
//   arch_depth       a box would sit more than 3 levels deep
//   arch_cycle       a box would become its own ancestor
//   arch_limit       (limit_exceeded) creating or restoring an object past node 300 / edge 1000 / box 100 / fault 50
// Dangling references (a parent, endpoint or target that does not exist) are let through: concurrent deletes make them
// unavoidable and the client's lenient load (arch.md 2.3) hides them the same way everywhere.
import type { DocStateView, DocTypeSpec, Json, Op, RejectReason } from "../collab-doctypes";
import { utf8Len } from "../collab-reducer";

const KEY_RE = /^(meta|[nbef]:[A-Za-z0-9_-]{1,62})$/;

const TYPE_OF_PREFIX: Record<string, string> = { n: "node", b: "box", e: "edge", f: "fault" };

const MAX_OBJECTS = { node: 300, edge: 1000, box: 100, fault: 50, meta: 1 } as const;
const MAX_DEPTH = 3;
const COORD = 100_000;
const SHORT_TEXT = 64;

/** The object type a key belongs to ("" = not an arch key). */
export function typeOfKey(key: string): string {
  if (!KEY_RE.test(key)) return "";
  return key === "meta" ? "meta" : (TYPE_OF_PREFIX[key[0]!] ?? "");
}

// ---- value checks ----

type Check = (v: Json) => boolean;

const int =
  (lo: number, hi: number): Check =>
  (v) =>
    typeof v === "number" && Number.isInteger(v) && v >= lo && v <= hi;

const bool: Check = (v) => typeof v === "boolean";

const LONE_SURROGATE = /[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/;
// arch.md 1.3: C0 / DEL / C1 controls, bidi marks, embeddings, overrides and isolates, line and paragraph separators
const FORBIDDEN = /[\u0000-\u001f\u007f-\u009f\u061c\u200e\u200f\u202a-\u202e\u2066-\u2069\u2028\u2029]/;

/** A string of at most maxBytes UTF-8 bytes (0 = no cap). */
const str =
  (maxBytes: number): Check =>
  (v) =>
    typeof v === "string" && !LONE_SURROGATE.test(v) && (maxBytes === 0 || utf8Len(v) <= maxBytes);

const oneOf =
  (...values: string[]): Check =>
  (v) =>
    typeof v === "string" && values.includes(v);

let segmenter: Intl.Segmenter | undefined;

function graphemes(s: string, limit: number): number {
  if (s.length <= limit) return s.length; // every grapheme is at least one UTF-16 unit
  segmenter ??= new Intl.Segmenter(undefined, { granularity: "grapheme" });
  let n = 0;
  for (const _ of segmenter.segment(s)) n++;
  return n;
}

/** A label or title: at most maxG graphemes, none of the forbidden characters. */
const text =
  (maxG: number): Check =>
  (v) =>
    typeof v === "string" && !LONE_SURROGATE.test(v) && !FORBIDDEN.test(v) && graphemes(v, maxG) <= maxG;

/** [x, y] (n = 2) or [x, y, w, h] (n = 4): x, y in -100,000..100,000, w, h in 1..100,000. */
const coords =
  (n: 2 | 4): Check =>
  (v) =>
    Array.isArray(v) && v.length === n && v.every((c, i) => (i < 2 ? int(-COORD, COORD)(c) : int(1, COORD)(c)));

const seed: Check = (v) => typeof v === "number" && Number.isInteger(v) && v >= 0 && v <= 18_446_744_073_709_551_615;

// ---- the field table (arch.md 2.1, ranges from arch.md 1.4) ----

const NODE_KINDS = ["client", "cdn", "lb", "api", "worker", "cache", "db_primary", "db_replica", "queue", "object_storage", "search", "external"];

const FIELDS: Record<string, Record<string, Check>> = {
  meta: {
    title: text(80),
    seed,
    duration_s: int(60, 600),
  },
  node: {
    kind: oneOf(...NODE_KINDS),
    label: text(40),
    pos: coords(2),
    parent: str(SHORT_TEXT),
    provider: str(SHORT_TEXT),
    instance_type: str(SHORT_TEXT),
    region: str(SHORT_TEXT),
    cpu_cores: int(1, 448),
    memory_mib: int(128, 24_576_000),
    instances: int(1, 1000),
    "autoscale.enabled": bool,
    "autoscale.min": int(1, 1000),
    "autoscale.max": int(1, 1000),
    "autoscale.target_cpu_permille": int(1, 1000),
    "autoscale.warmup_s": int(0, 3600),
    "autoscale.cooldown_s": int(0, 3600),
    nic_mbps: int(1, 400_000),
    "disk.type": oneOf("ssd", "ssd_fast", "hdd"),
    "disk.size_gb": int(0, 65_536),
    "disk.iops": int(0, 256_000),
    cpu_micros_per_req: int(1, 60_000_000),
    timeout_ms: int(1, 600_000),
    retries: int(0, 10),
    queue_cap: int(0, 10_000_000),
    custom_hourly_micro_usd: int(0, 1_000_000_000_000),
    "params.hit_permille": int(0, 1000),
    "params.working_set_mib": int(0, 24_576_000),
    "params.ttl_s": int(0, 31_536_000),
    "params.lb_algo": oneOf("", "round_robin", "least_conn"),
    "params.health_interval_s": int(0, 3600),
    "params.read_iops_milli": int(0, 1_000_000),
    "params.write_iops_milli": int(0, 1_000_000),
    "params.pool_max": int(0, 100_000),
    "params.failover_to": str(SHORT_TEXT),
    "params.failover_s": int(0, 3600),
    "params.repl_lag_ms": int(0, 600_000),
    "params.max_rps": int(0, 1_000_000_000),
    "params.retention_msgs": int(0, 1_000_000_000),
    "params.fixed_latency_ms": int(0, 600_000),
    "params.error_permille": int(0, 1000),
    "params.index_write_permille": int(0, 1000),
    "traffic.rps": int(0, 10_000_000),
    "traffic.read_permille": int(0, 1000),
    "traffic.req_kb": int(0, 102_400),
    "traffic.resp_kb": int(0, 102_400),
    "traffic.curve": oneOf("constant", "ramp", "spike", "wave"),
    "traffic.spike_mult": int(1, 100),
    "traffic.spike_at_s": int(0, 600),
    "traffic.spike_dur_s": int(0, 600),
  },
  box: {
    kind: oneOf("region", "vpc", "az"),
    label: text(40),
    region_code: str(SHORT_TEXT),
    rect: coords(4),
    parent: str(SHORT_TEXT),
  },
  edge: {
    from: str(SHORT_TEXT),
    to: str(SHORT_TEXT),
    protocol: oneOf("http1", "http2", "grpc", "tcp", "async"),
    mbps: int(0, 400_000),
    latency_micros: int(-1, 60_000_000),
    read_permille: int(0, 1000),
    write_permille: int(0, 1000),
  },
  fault: {
    at_s: int(0, 600),
    kind: oneOf("node_off", "instances_off", "latency", "az_off", "region_off"),
    target: str(SHORT_TEXT),
    count: int(0, 1000),
    add_ms: int(0, 600_000),
    dur_s: int(0, 600),
  },
};

const invalid = (detail: string): RejectReason => ({ code: "invalid", detail });
const LIMIT: RejectReason = { code: "limit_exceeded", detail: "arch_limit" };

/** One field of an object of type `type`: null (accepted), arch_bad_field or arch_bad_value. null clears a field. */
function checkField(type: string, field: string, value: Json): RejectReason | null {
  const table = FIELDS[type]!;
  if (!Object.prototype.hasOwnProperty.call(table, field)) return invalid("arch_bad_field");
  if (value === null) return null;
  return table[field]!(value) ? null : invalid("arch_bad_value");
}

type Lookup = (key: string) => { type: string; fields: Record<string, Json>; deleted: boolean } | undefined;

/**
 * Box `id` taking parent `parent` (a box id, "" = top level): arch_cycle when the box would be its own ancestor,
 * arch_depth when it would sit more than MAX_DEPTH levels deep. Only the chain above is walked (the state view has no
 * child index); a box moved with children under it is lifted by the client's lenient load (arch.md 2.3 step 5).
 */
function checkParent(get: Lookup, id: string, parent: Json | undefined): RejectReason | null {
  if (typeof parent !== "string" || parent === "") return null;
  let level = 1;
  let cur = parent;
  const seen = new Set<string>();
  while (cur !== "") {
    const key = "b:" + cur;
    if (key === id || seen.has(key)) return invalid("arch_cycle");
    seen.add(key);
    const o = get(key);
    if (o === undefined || o.deleted) break; // a dangling parent: top level
    if (++level > MAX_DEPTH) return invalid("arch_depth");
    const p = o.fields.parent;
    cur = typeof p === "string" ? p : "";
  }
  return null;
}

/** Live objects of `type` would grow by one: arch_limit past its maxObjects. */
function overLimit(state: DocStateView, type: string): boolean {
  const max = (MAX_OBJECTS as Record<string, number>)[type];
  return max !== undefined && state.count(type) >= max;
}

/** A whole snapshot (reset, and the first state of a new document). */
function checkSnapshot(op: Extract<Op, { op: "reset" }>): RejectReason | null {
  const byKey = new Map<string, { type: string; fields: Record<string, Json>; deleted: boolean }>();
  const counts: Record<string, number> = {};
  for (const o of op.snapshot.objects) {
    const type = typeOfKey(o.id);
    if (type === "" || o.type !== type) return invalid("arch_bad_object");
    for (const [k, v] of Object.entries(o.fields)) {
      const r = checkField(type, k, v);
      if (r !== null) return r;
    }
    const deleted = o.deleted === true;
    byKey.set(o.id, { type, fields: o.fields, deleted });
    if (!deleted) {
      counts[type] = (counts[type] ?? 0) + 1;
      const max = (MAX_OBJECTS as Record<string, number>)[type];
      if (max !== undefined && counts[type]! > max) return LIMIT;
    }
  }
  const get: Lookup = (k) => byKey.get(k);
  for (const [key, o] of byKey) {
    if (o.type !== "box" || o.deleted) continue;
    const r = checkParent(get, key, o.fields.parent);
    if (r !== null) return r;
  }
  return null;
}

export function validateArchOp(op: Op, state: DocStateView): RejectReason | null {
  if (op.op === "reset") return checkSnapshot(op);
  const keyType = typeOfKey(op.id);
  if (keyType === "") return invalid("arch_bad_object");
  const cur = state.get(op.id);
  switch (op.op) {
    case "put": {
      if (op.type !== keyType) return invalid("arch_bad_object");
      for (const [k, v] of Object.entries(op.fields)) {
        const r = checkField(keyType, k, v);
        if (r !== null) return r;
      }
      if (cur === undefined && overLimit(state, keyType)) return LIMIT;
      if (keyType === "box" && Object.prototype.hasOwnProperty.call(op.fields, "parent")) {
        return checkParent((k) => state.get(k), op.id, op.fields.parent);
      }
      return null;
    }
    case "set": {
      const type = cur?.type ?? keyType;
      if (type !== keyType) return invalid("arch_bad_object");
      const r = checkField(type, op.field, op.value);
      if (r !== null) return r;
      if (type === "box" && op.field === "parent") return checkParent((k) => state.get(k), op.id, op.value);
      return null;
    }
    case "undel":
      if (cur !== undefined && cur.deleted && overLimit(state, cur.type)) return LIMIT;
      return null;
    case "del":
      return null;
  }
}

export const docType: DocTypeSpec = {
  type: "arch",
  maxDocBytes: 1_048_576,
  maxObjects: { ...MAX_OBJECTS },
  maxFieldBytes: 16_384,
  validateOp: validateArchOp,
};
