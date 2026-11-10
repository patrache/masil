// Room document state and its deterministic merge (docs/contract/collab.md 1, 3.3, 5; v0.1.3 design C1.1 JC1):
// objects with fields, field-level last-writer-wins in server sequence order, delete wins until undel. The Go client
// (internal/collab Doc.Apply) applies the same docs/contract/collab-vectors.json and must land on the same canonical
// snapshot. Tables are Maps and field values are kept as their JSON text, so a name like "__proto__" can never reach an
// object's prototype (collab.md 1.2) — names are also refused by the shape check before anything is applied.
import type { DocTypeSpec, RejectReason } from "./collab-doctypes";

export type Json = null | boolean | number | string | Json[] | { [k: string]: Json };

export interface SnapshotObject {
  id: string;
  type: string;
  fields: Record<string, Json>;
  deleted?: boolean;
}

export interface Snapshot {
  seq: number;
  objects: SnapshotObject[];
}

export type Op =
  | { op: "put"; id: string; type: string; fields: Record<string, Json> }
  | { op: "set"; id: string; field: string; value: Json }
  | { op: "del"; id: string }
  | { op: "undel"; id: string }
  | { op: "reset"; snapshot: Snapshot };

/** What one op changed: `field` "" = the object as a whole (put creating it, del, undel, reset). */
export interface Change {
  id: string;
  field: string;
  removed: boolean; // the object is deleted / gone after the change
}

// ---- names (collab.md 1.2) ----

const NAME_RE = /^[A-Za-z0-9_.:-]{1,64}$/;
const RESERVED = new Set(["__proto__", "constructor", "prototype"]);

export function isName(v: unknown): v is string {
  return typeof v === "string" && NAME_RE.test(v) && !RESERVED.has(v);
}

export const CLIENT_OP_ID_RE = /^[A-Za-z0-9_-]{1,64}$/;
export const OPS_MAX = 200;

const hasOwn = (o: object, k: string) => Object.prototype.hasOwnProperty.call(o, k);

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return v !== null && typeof v === "object" && !Array.isArray(v);
}

/** A fields object whose own keys all follow the name rule; returns [name, JSON text] pairs, or null. */
function parseFields(v: unknown): Array<[string, string]> | null {
  if (!isPlainObject(v)) return null;
  const out: Array<[string, string]> = [];
  for (const k of Object.keys(v)) {
    if (!isName(k)) return null;
    out.push([k, JSON.stringify(v[k] as Json)]);
  }
  return out;
}

// ---- state ----

interface Obj {
  type: string;
  fields: Map<string, string>; // name -> JSON text of the value
  deleted: boolean;
  bytes: number; // UTF-8 bytes of this object's canonical JSON, -1 = not computed
}

export function utf8Len(s: string): number {
  let n = 0;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c < 0x80) n += 1;
    else if (c < 0x800) n += 2;
    else if (c >= 0xd800 && c <= 0xdbff && i + 1 < s.length) {
      const d = s.charCodeAt(i + 1);
      if (d >= 0xdc00 && d <= 0xdfff) {
        n += 4;
        i++;
      } else n += 3;
    } else n += 3;
  }
  return n;
}

function objectText(id: string, o: Obj): string {
  const names = [...o.fields.keys()].sort();
  const fields = names.map((k) => `${JSON.stringify(k)}:${o.fields.get(k)!}`).join(",");
  return `{"id":${JSON.stringify(id)},"type":${JSON.stringify(o.type)},"fields":{${fields}}${o.deleted ? ',"deleted":true' : ""}}`;
}

function cloneObj(o: Obj): Obj {
  return { type: o.type, fields: new Map(o.fields), deleted: o.deleted, bytes: o.bytes };
}

/** One frame's undo record: the objects as they were before the frame touched them (undefined = absent). */
interface Undo {
  seq: number;
  saved: Map<string, Obj | undefined>;
  full: Map<string, Obj> | null; // the whole table before a reset
}

export class DocState {
  seq = 0;
  private objects = new Map<string, Obj>();
  private counts = new Map<string, number>(); // live (not deleted) objects per type
  private undo: Undo | null = null;

  static fromSnapshot(s: Snapshot): DocState {
    const st = new DocState();
    st.seq = s.seq;
    st.replaceAll(s.objects);
    return st;
  }

  /** Parses canonical snapshot text this module produced (trusted). */
  static fromText(text: string): DocState {
    return DocState.fromSnapshot(JSON.parse(text) as Snapshot);
  }

  private replaceAll(objects: SnapshotObject[]): void {
    this.objects = new Map();
    for (const o of objects) {
      const fields = new Map<string, string>();
      for (const k of Object.keys(o.fields)) fields.set(k, JSON.stringify(o.fields[k]));
      this.objects.set(o.id, { type: o.type, fields, deleted: o.deleted === true, bytes: -1 });
    }
    this.recount();
  }

  private recount(): void {
    this.counts = new Map();
    for (const o of this.objects.values()) if (!o.deleted) this.counts.set(o.type, (this.counts.get(o.type) ?? 0) + 1);
  }

  private adjust(o: Obj | undefined, by: number): void {
    if (o === undefined || o.deleted) return;
    this.counts.set(o.type, (this.counts.get(o.type) ?? 0) + by);
  }

  /** Before mutating `id` inside a frame: remember how it was (once per frame). */
  private save(id: string): void {
    const u = this.undo;
    if (u === null || u.full !== null || u.saved.has(id)) return;
    const o = this.objects.get(id);
    u.saved.set(id, o === undefined ? undefined : cloneObj(o));
  }

  /** Starts a frame that `rollback()` can undo completely. */
  begin(): void {
    this.undo = { seq: this.seq, saved: new Map(), full: null };
  }

  commit(): void {
    this.undo = null;
  }

  rollback(): void {
    const u = this.undo;
    if (u === null) return;
    if (u.full !== null) this.objects = u.full;
    else
      for (const [id, o] of u.saved) {
        if (o === undefined) this.objects.delete(id);
        else this.objects.set(id, o);
      }
    this.seq = u.seq;
    this.undo = null;
    this.recount();
  }

  get(id: string): { type: string; fields: Record<string, Json>; deleted: boolean } | undefined {
    const o = this.objects.get(id);
    if (o === undefined) return undefined;
    const fields: Record<string, Json> = Object.create(null);
    for (const [k, v] of o.fields) fields[k] = JSON.parse(v) as Json;
    return { type: o.type, fields, deleted: o.deleted };
  }

  count(type: string): number {
    return this.counts.get(type) ?? 0;
  }

  /** Bytes of one field's JSON value, or -1 when absent. */
  fieldBytes(id: string, field: string): number {
    const v = this.objects.get(id)?.fields.get(field);
    return v === undefined ? -1 : utf8Len(v);
  }

  /** UTF-8 bytes of snapshotText() (collab.md 5 DocStateView.bytes), from per-object cached sizes. */
  bytes(): number {
    let n = utf8Len(`{"seq":${this.seq},"objects":[]}`);
    let k = 0;
    for (const [id, o] of this.objects) {
      if (o.bytes < 0) o.bytes = utf8Len(objectText(id, o));
      n += o.bytes;
      k++;
    }
    return n + (k > 0 ? k - 1 : 0);
  }

  /** Canonical snapshot (collab.md 1.1): objects by id byte order, fields by name, deleted only when true. */
  snapshotText(): string {
    const ids = [...this.objects.keys()].sort();
    return `{"seq":${this.seq},"objects":[${ids.map((id) => objectText(id, this.objects.get(id)!)).join(",")}]}`;
  }

  // ---- mutations (only through applyOps) ----

  /** @internal */
  _apply(op: Op, out: Change[]): void {
    switch (op.op) {
      case "put": {
        const o = this.objects.get(op.id);
        if (o !== undefined && o.deleted) return; // a deleted object ignores put
        this.save(op.id);
        if (o === undefined) {
          const created: Obj = { type: op.type, fields: new Map(), deleted: false, bytes: -1 };
          for (const k of Object.keys(op.fields)) created.fields.set(k, JSON.stringify(op.fields[k]));
          this.objects.set(op.id, created);
          this.adjust(created, 1);
          out.push({ id: op.id, field: "", removed: false });
          for (const k of created.fields.keys()) out.push({ id: op.id, field: k, removed: false });
        } else {
          for (const k of Object.keys(op.fields)) {
            o.fields.set(k, JSON.stringify(op.fields[k])); // type stays
            out.push({ id: op.id, field: k, removed: false });
          }
          o.bytes = -1;
        }
        return;
      }
      case "set": {
        const o = this.objects.get(op.id);
        if (o === undefined || o.deleted) return;
        this.save(op.id);
        o.fields.set(op.field, JSON.stringify(op.value));
        o.bytes = -1;
        out.push({ id: op.id, field: op.field, removed: false });
        return;
      }
      case "del":
      case "undel": {
        const o = this.objects.get(op.id);
        if (o === undefined) return;
        this.save(op.id);
        this.adjust(o, -1);
        o.deleted = op.op === "del";
        o.bytes = -1;
        this.adjust(o, 1);
        out.push({ id: op.id, field: "", removed: o.deleted });
        return;
      }
      case "reset": {
        if (this.undo !== null && this.undo.full === null) {
          // restoring the saved entries over the old table would not undo a reset: keep the whole old table
          const before = new Map(this.objects);
          for (const [id, o] of this.undo.saved) {
            if (o === undefined) before.delete(id);
            else before.set(id, o);
          }
          this.undo.full = before;
        }
        const prev = this.objects;
        for (const id of prev.keys()) out.push({ id, field: "", removed: true });
        this.replaceAll(op.snapshot.objects);
        for (const id of this.objects.keys()) out.push({ id, field: "", removed: this.objects.get(id)!.deleted });
        return;
      }
    }
  }
}

/** collab.md 1.1: applies one sequenced frame (already shape-checked) and returns what changed. Deterministic. */
export function applyOps(state: DocState, seq: number, ops: Op[]): Change[] {
  const out: Change[] = [];
  for (const op of ops) state._apply(op, out);
  state.seq = seq;
  return out;
}

// ---- shape checks (collab.md 3.3 step 1) ----

/** One op of a client frame, normalized (known keys only), or null when it breaks the shape / name rules. */
export function parseOp(raw: unknown, allowReset = false): Op | null {
  if (!isPlainObject(raw)) return null;
  switch (raw.op) {
    case "put": {
      if (!isName(raw.id) || !isName(raw.type)) return null;
      const fields = parseFields(raw.fields);
      if (fields === null) return null;
      const f: Record<string, Json> = Object.create(null);
      for (const [k, text] of fields) f[k] = JSON.parse(text) as Json;
      return { op: "put", id: raw.id, type: raw.type, fields: f };
    }
    case "set":
      if (!isName(raw.id) || !isName(raw.field) || !hasOwn(raw, "value")) return null;
      return { op: "set", id: raw.id, field: raw.field, value: raw.value as Json };
    case "del":
    case "undel":
      if (!isName(raw.id)) return null;
      return { op: raw.op, id: raw.id };
    case "reset": {
      if (!allowReset) return null;
      const snapshot = parseSnapshot(raw.snapshot);
      return snapshot === null ? null : { op: "reset", snapshot };
    }
    default:
      return null;
  }
}

/** A client frame's `ops` (1..200, no reset), or null. */
export function parseOps(raw: unknown, allowReset = false): Op[] | null {
  if (!Array.isArray(raw) || raw.length < 1 || raw.length > OPS_MAX) return null;
  const out: Op[] = [];
  for (const r of raw) {
    const op = parseOp(r, allowReset);
    if (op === null) return null;
    out.push(op);
  }
  return out;
}

/** A Snapshot from untrusted JSON (POST docs `snapshot`, reset): name rules, unique ids; `seq` defaults to 0. */
export function parseSnapshot(raw: unknown): Snapshot | null {
  if (!isPlainObject(raw) || !Array.isArray(raw.objects)) return null;
  const seq = raw.seq === undefined ? 0 : raw.seq;
  if (typeof seq !== "number" || !Number.isInteger(seq) || seq < 0) return null;
  const seen = new Set<string>();
  const objects: SnapshotObject[] = [];
  for (const o of raw.objects as unknown[]) {
    if (!isPlainObject(o) || !isName(o.id) || !isName(o.type) || seen.has(o.id)) return null;
    if (o.deleted !== undefined && typeof o.deleted !== "boolean") return null;
    const fields = parseFields(o.fields);
    if (fields === null) return null;
    seen.add(o.id);
    const f: Record<string, Json> = Object.create(null);
    for (const [k, text] of fields) f[k] = JSON.parse(text) as Json;
    objects.push(o.deleted === true ? { id: o.id, type: o.type, fields: f, deleted: true } : { id: o.id, type: o.type, fields: f });
  }
  return { seq, objects };
}

// ---- one frame, atomically (collab.md 3.3 steps 2-3) ----

/** The doc type's limits on the state as it stands (after a frame was applied), or null. */
export function limitReason(state: DocState, spec: DocTypeSpec, changes: Change[]): RejectReason | null {
  if (state.bytes() > spec.maxDocBytes) return { code: "too_large" };
  for (const [type, max] of Object.entries(spec.maxObjects)) {
    if (state.count(type) > max) return { code: "limit_exceeded", detail: `limit_${type}` };
  }
  for (const c of changes) {
    if (c.field !== "" && state.fieldBytes(c.id, c.field) > spec.maxFieldBytes) return { code: "too_large" };
  }
  return null;
}

/**
 * validateOp per op (on the state so far, first refusal wins), then apply, then the type's limits — all or nothing:
 * any refusal rolls the state back and returns the reason (nothing is sequenced). On success the state is at `seq`.
 */
export function applyFrame(state: DocState, spec: DocTypeSpec, seq: number, ops: Op[]): { ok: true; changes: Change[] } | { ok: false; reason: RejectReason } {
  state.begin();
  const changes: Change[] = [];
  for (const op of ops) {
    const r = spec.validateOp(op, state);
    if (r !== null) {
      state.rollback();
      return { ok: false, reason: r };
    }
    state._apply(op, changes);
  }
  state.seq = seq;
  const r = limitReason(state, spec, changes);
  if (r !== null) {
    state.rollback();
    return { ok: false, reason: r };
  }
  state.commit();
  return { ok: true, changes };
}
