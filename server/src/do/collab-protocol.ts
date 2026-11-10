// Document socket frames and limits (docs/contract/collab.md 3-4). Pure: the CollabHub feeds frames in and sends what
// comes out. Server -> client frames are built by string concatenation around stored JSON text (ops, snapshots), so a
// document is never parsed and re-serialized just to be forwarded.
import { CLIENT_OP_ID_RE, applyFrame, parseOps, utf8Len, type Change, type DocState, type Op } from "../lib/collab-reducer";
import type { DocTypeSpec, RejectReason } from "../lib/collab-doctypes";

export const COLLAB_LIMITS = {
  clientFrameBytes: 65_536,
  opsPerSecond: 20,
  presencePerSecond: 10,
  /** consecutive seconds over opsPerSecond -> close 4008 */
  rateStrikes: 3,
  selectionMax: 200,
} as const;

export const CollabClose = {
  normal: 1000,
  unsupported: 1003,
  tooBig: 1009,
  internal: 1011,
  unauthorized: 4001,
  suspended: 4003,
  rateLimited: 4008,
  kicked: 4100,
} as const;

export type RejectCode = "invalid" | "too_large" | "limit_exceeded" | "locked" | "forbidden" | "rate_limited";

export interface Presence {
  cursor: unknown;
  selection: string[];
  dragging: unknown;
}

export type ClientFrame =
  | { t: "hello"; since: number | null }
  | { t: "op"; clientOpId: string; ops: Op[] | null /* null = breaks the shape rules (reject invalid) */ }
  | { t: "presence"; patch: Partial<Presence> }
  | { t: "ignored" }; // not JSON, unknown t, or an op frame without a usable client_op_id (nothing to answer to)

const isObj = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === "object" && !Array.isArray(v);
const has = (o: object, k: string) => Object.prototype.hasOwnProperty.call(o, k);

/** collab.md 3.1. */
export function parseClientFrame(text: string): ClientFrame {
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch {
    return { t: "ignored" };
  }
  if (!isObj(raw)) return { t: "ignored" };
  switch (raw.t) {
    case "hello": {
      const s = raw.since;
      if (s === null || s === undefined) return { t: "hello", since: null };
      return typeof s === "number" && Number.isInteger(s) && s >= 0 ? { t: "hello", since: s } : { t: "hello", since: null };
    }
    case "op": {
      const id = raw.client_op_id;
      if (typeof id !== "string" || !CLIENT_OP_ID_RE.test(id)) return { t: "ignored" };
      const baseOk = typeof raw.base_seq === "number" && Number.isInteger(raw.base_seq) && raw.base_seq >= 0;
      return { t: "op", clientOpId: id, ops: baseOk ? parseOps(raw.ops) : null };
    }
    case "presence": {
      const patch: Partial<Presence> = {};
      if (has(raw, "cursor")) patch.cursor = raw.cursor ?? null;
      if (has(raw, "dragging")) patch.dragging = raw.dragging ?? null;
      if (has(raw, "selection")) {
        const sel = raw.selection;
        if (!Array.isArray(sel) || sel.length > COLLAB_LIMITS.selectionMax || !sel.every((x) => typeof x === "string" && x.length <= 64)) return { t: "ignored" };
        patch.selection = sel as string[];
      }
      return { t: "presence", patch };
    }
    default:
      return { t: "ignored" };
  }
}

// ---- server -> client ----

const q = (s: string) => JSON.stringify(s);

export function helloSnapshotFrame(seq: number, snapshotText: string): string {
  return `{"t":"hello","seq":${seq},"snapshot":${snapshotText}}`;
}

/** `entries` are `{"seq":..,"author":..,"ops":..}` texts (opEntry). */
export function helloOpsFrame(seq: number, entries: string[]): string {
  return `{"t":"hello","seq":${seq},"ops":[${entries.join(",")}]}`;
}

export function opEntry(seq: number, author: string, opsText: string): string {
  return `{"seq":${seq},"author":${q(author)},"ops":${opsText}}`;
}

export function opFrame(seq: number, author: string, clientOpId: string | null, opsText: string): string {
  return `{"t":"op","seq":${seq},"author":${q(author)}${clientOpId === null ? "" : `,"client_op_id":${q(clientOpId)}`},"ops":${opsText}}`;
}

export function ackFrame(clientOpId: string, seq: number): string {
  return `{"t":"ack","client_op_id":${q(clientOpId)},"seq":${seq}}`;
}

export function rejectFrame(clientOpId: string, code: RejectCode, detail?: string): string {
  return JSON.stringify(detail === undefined ? { t: "reject", client_op_id: clientOpId, code } : { t: "reject", client_op_id: clientOpId, code, detail });
}

export function presenceFrame(user: string, p: Presence): string {
  return JSON.stringify({ t: "presence", user, cursor: p.cursor ?? null, selection: p.selection, dragging: p.dragging ?? null });
}

export function kickedFrame(reason: string): string {
  return JSON.stringify({ t: "kicked", reason });
}

const DETAIL_RE = /^[a-z0-9_]{1,40}$/;

/** A doc type's RejectReason as a reject frame; a malformed `detail` is dropped (clients treat it as code only). */
export function reasonFrame(clientOpId: string, r: RejectReason): string {
  return rejectFrame(clientOpId, r.code, r.detail !== undefined && DETAIL_RE.test(r.detail) ? r.detail : undefined);
}

/**
 * collab.md 3.3 steps 2-3 for one op frame whose common checks passed: on success the state is at `seq` and the
 * caller sequences `opsText`; on refusal nothing changed and `frame` is the reject for the sender.
 */
export function decideOp(
  state: DocState,
  spec: DocTypeSpec,
  seq: number,
  clientOpId: string,
  ops: Op[],
): { ok: true; opsText: string; changes: Change[] } | { ok: false; frame: string } {
  const r = applyFrame(state, spec, seq, ops);
  if (!r.ok) return { ok: false, frame: reasonFrame(clientOpId, r.reason) };
  return { ok: true, opsText: JSON.stringify(ops), changes: r.changes };
}

export { utf8Len as utf8Bytes };
