// Workspace share frames (docs/contract/workspace.md 2-3): the 20-byte outer header, the kind table and the level
// order. The same table as Go internal/workspace/wire/kinds.go — test/unit/workspace-frame.test.ts reads the contract
// table and compares. The server reads headers and the plaintext JSON of Class=Server kinds only; every other body is
// ciphertext it relays without looking.

export const VERSION = 1;
export const HEADER_LEN = 20;
/** largest frame, header included (workspace.md 2) */
export const MAX_FRAME = 262144;

export const FLAG_MORE = 1 << 0;
export const FLAG_GROUP_START = 1 << 1;

export const SEAT_OWNER = 0;
export const SEAT_SERVER = 254;
export const SEAT_ALL = 255;
export const MAX_SEAT = 8;

/** level names by rank: 0 = none ... 4 = keyboard (workspace.md 3.1) */
export const LEVELS = ["", "viewer", "prompter", "editor", "keyboard"] as const;
export type LevelName = (typeof LEVELS)[number];
/** MinLevel of kinds participants never send (owner / server only) */
export const NO_PEER = -1;

export type FrameClass = "Server" | "Handshake" | "Broadcast" | "ToOwner" | "ToPeer";

export interface KindInfo {
  value: number;
  name: string;
  /** class when a participant sends it (or the only class) */
  cls: FrameClass;
  /** rank in LEVELS a participant needs to send it to the owner; NO_PEER = never */
  minLevel: number;
}

const N = NO_PEER;

export const KIND_TABLE: readonly KindInfo[] = [
  { value: 0x01, name: "srv_hello", cls: "Server", minLevel: N },
  { value: 0x02, name: "srv_peer", cls: "Server", minLevel: N },
  { value: 0x03, name: "ctl_levels", cls: "Server", minLevel: N },
  { value: 0x04, name: "ctl_kick", cls: "Server", minLevel: N },
  { value: 0x05, name: "ctl_end", cls: "Server", minLevel: N },
  { value: 0x06, name: "ctl_attention", cls: "Server", minLevel: N },
  { value: 0x10, name: "hs_commit", cls: "Handshake", minLevel: 0 },
  { value: 0x11, name: "hs_owner_pub", cls: "Handshake", minLevel: N },
  { value: 0x12, name: "hs_reveal", cls: "Handshake", minLevel: 0 },
  { value: 0x13, name: "hs_grant", cls: "ToPeer", minLevel: N },
  { value: 0x14, name: "hs_decline", cls: "Handshake", minLevel: N },
  { value: 0x15, name: "hs_resume", cls: "ToOwner", minLevel: 0 },
  { value: 0x16, name: "hs_confirm", cls: "ToOwner", minLevel: 0 },
  { value: 0x20, name: "term_out", cls: "Broadcast", minLevel: N },
  { value: 0x21, name: "term_key", cls: "Broadcast", minLevel: N },
  { value: 0x22, name: "term_input", cls: "ToOwner", minLevel: 4 },
  { value: 0x23, name: "term_status", cls: "Broadcast", minLevel: N },
  { value: 0x30, name: "prop_submit", cls: "ToOwner", minLevel: 2 },
  { value: 0x31, name: "prop_result", cls: "ToPeer", minLevel: N },
  { value: 0x40, name: "doc_open", cls: "ToOwner", minLevel: 1 },
  { value: 0x41, name: "doc_state", cls: "ToPeer", minLevel: N },
  { value: 0x42, name: "doc_op", cls: "ToOwner", minLevel: 3 },
  { value: 0x43, name: "doc_ack", cls: "ToPeer", minLevel: N },
  { value: 0x44, name: "doc_reject", cls: "ToPeer", minLevel: N },
  { value: 0x45, name: "doc_bcast", cls: "Broadcast", minLevel: N },
  { value: 0x46, name: "doc_save", cls: "ToOwner", minLevel: 3 },
  { value: 0x47, name: "doc_presence", cls: "ToOwner", minLevel: 1 }, // Broadcast when the owner sends it
  { value: 0x48, name: "doc_close", cls: "ToOwner", minLevel: 1 },
  { value: 0x50, name: "tree_req", cls: "ToOwner", minLevel: 1 },
  { value: 0x51, name: "tree_list", cls: "ToPeer", minLevel: N },
  { value: 0x52, name: "fs_req", cls: "ToOwner", minLevel: 3 },
  { value: 0x53, name: "fs_result", cls: "ToPeer", minLevel: N },
  { value: 0x60, name: "roster", cls: "Broadcast", minLevel: N },
  { value: 0x61, name: "rekey", cls: "ToPeer", minLevel: N },
  { value: 0x62, name: "ended", cls: "Broadcast", minLevel: N },
  { value: 0x63, name: "self_level", cls: "ToOwner", minLevel: 1 },
  { value: 0x64, name: "notice", cls: "Broadcast", minLevel: N },
];

const BY_VALUE = new Map(KIND_TABLE.map((k) => [k.value, k]));
const BY_NAME = new Map(KIND_TABLE.map((k) => [k.name, k]));

export function kindInfo(value: number): KindInfo | undefined {
  return BY_VALUE.get(value);
}

/** The kind byte of `name`; throws for a name not in the table (a programming error). */
export function kindOf(name: string): number {
  const k = BY_NAME.get(name);
  if (!k) throw new Error(`unknown workspace frame kind ${name}`);
  return k.value;
}

export const K = {
  srvHello: kindOf("srv_hello"),
  srvPeer: kindOf("srv_peer"),
  ctlLevels: kindOf("ctl_levels"),
  ctlKick: kindOf("ctl_kick"),
  ctlEnd: kindOf("ctl_end"),
  ctlAttention: kindOf("ctl_attention"),
  termOut: kindOf("term_out"),
  termKey: kindOf("term_key"),
  termInput: kindOf("term_input"),
  propSubmit: kindOf("prop_submit"),
  docOp: kindOf("doc_op"),
} as const;

/** Rank of a level name ("" = 0), or -1 for a name not in LEVELS. */
export function levelRank(name: unknown): number {
  return typeof name === "string" ? (LEVELS as readonly string[]).indexOf(name) : -1;
}

/** May a participant at `rank` send `kind` to the owner (workspace.md 4 rule 3)? Unknown kinds: never. */
export function peerAllowed(kind: number, rank: number): boolean {
  const k = BY_VALUE.get(kind);
  return k !== undefined && k.minLevel !== NO_PEER && rank >= k.minLevel;
}

export interface Header {
  kind: number;
  flags: number;
  from: number;
  to: number;
  seq: bigint;
  len: number;
}

/**
 * The outer header of one frame, or null when it does not parse (workspace.md 2): shorter than the header, version
 * not 1, reserved bytes not 0, larger than MAX_FRAME, or Len not equal to the bytes after the header.
 */
export function parseHeader(buf: ArrayBuffer): Header | null {
  if (buf.byteLength < HEADER_LEN || buf.byteLength > MAX_FRAME) return null;
  const v = new DataView(buf);
  if (v.getUint8(0) !== VERSION) return null;
  if (v.getUint8(5) !== 0 || v.getUint8(6) !== 0 || v.getUint8(7) !== 0) return null;
  const len = v.getUint32(16);
  if (len !== buf.byteLength - HEADER_LEN) return null;
  return { kind: v.getUint8(1), flags: v.getUint8(2), from: v.getUint8(3), to: v.getUint8(4), seq: v.getBigUint64(8), len };
}

/** One frame: header (Seq given, Len filled in) + payload. */
export function buildFrame(kind: number, from: number, to: number, payload: Uint8Array, flags = 0, seq = 0n): Uint8Array {
  const out = new Uint8Array(HEADER_LEN + payload.byteLength);
  const v = new DataView(out.buffer);
  v.setUint8(0, VERSION);
  v.setUint8(1, kind);
  v.setUint8(2, flags);
  v.setUint8(3, from);
  v.setUint8(4, to);
  v.setBigUint64(8, seq);
  v.setUint32(16, payload.byteLength);
  out.set(payload, HEADER_LEN);
  return out;
}

const enc = new TextEncoder();
const dec = new TextDecoder("utf-8", { fatal: true, ignoreBOM: false });

/** A server-made plaintext JSON frame (srv_hello, srv_peer) from SEAT_SERVER to `to`. */
export function serverFrame(kind: number, to: number, body: unknown): Uint8Array {
  return buildFrame(kind, SEAT_SERVER, to, enc.encode(JSON.stringify(body)));
}

/** The plaintext JSON object of a Class=Server frame's payload, or null when it is not one. */
export function jsonPayload(buf: ArrayBuffer): Record<string, unknown> | null {
  try {
    const v: unknown = JSON.parse(dec.decode(new Uint8Array(buf, HEADER_LEN)));
    return v !== null && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
  } catch {
    return null;
  }
}
