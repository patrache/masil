// Workspace share socket helpers (docs/contract/workspace.md, P2 Task 13): open GET /v1/workspace-shares/{id}/ws,
// build binary frames, and wait for frames by kind. The payloads tests send are opaque bytes (the server relays
// ciphertext without looking), except Class=Server kinds, whose JSON the hub reads.
import { KIND_TABLE, buildFrame, kindOf, parseHeader, type Header } from "../src/do/workspace-frame";
import { api } from "./helpers";

export interface WsBinFrame {
  header: Header;
  /** the bytes after the header */
  payload: Uint8Array;
  /** kind name (from the table) */
  kind: string;
}

export interface WsBin {
  ws: WebSocket;
  /** every binary frame received so far, in order */
  frames: WsBinFrame[];
  /** text frames received (the hub never sends one) */
  texts: string[];
  /** send one frame: `kind` by name; from / to default to the owner seat 0 -> everyone 255 */
  send(kind: string, opts?: { from?: number; to?: number; payload?: Uint8Array | string | object; flags?: number; seq?: bigint }): void;
  sendRaw(data: ArrayBuffer | Uint8Array | string): void;
  /** the first not-yet-returned frame of `kind` (matching `pred`), received or arriving within `timeoutMs` */
  next(kind: string, pred?: (f: WsBinFrame) => boolean, timeoutMs?: number): Promise<WsBinFrame>;
  /** frames of `kind` received so far */
  all(kind: string): WsBinFrame[];
  closed: Promise<{ code: number; reason: string }>;
  close(code?: number): void;
}

const enc = new TextEncoder();
const NAMES = new Map(KIND_TABLE.map((k) => [k.value, k.name]));

function nameOf(kind: number): string {
  return NAMES.get(kind) ?? `0x${kind.toString(16)}`;
}

function bytes(p: Uint8Array | string | object | undefined): Uint8Array {
  if (p === undefined) return new Uint8Array(0);
  if (p instanceof Uint8Array) return p;
  return enc.encode(typeof p === "string" ? p : JSON.stringify(p));
}

/** A frame as the wire carries it: `kind` by name, payload bytes / text / JSON. */
export function frame(kind: string, from: number, to: number, payload?: Uint8Array | string | object, flags = 0, seq = 0n): Uint8Array {
  return buildFrame(kindOf(kind), from, to, bytes(payload), flags, seq);
}

/** The JSON body of a plaintext frame (srv_hello, srv_peer, ...). */
export function json(f: WsBinFrame): any {
  return JSON.parse(new TextDecoder().decode(f.payload));
}

/** Upgrade request to the share socket (status only — for 404 checks). */
export function shareWsRaw(token: string, shareId: string): Promise<Response> {
  return api(token).raw(`/v1/workspace-shares/${shareId}/ws`, { headers: { Upgrade: "websocket" } });
}

/** Opens GET /v1/workspace-shares/{shareId}/ws and waits for srv_hello; throws unless 101. */
export async function connectWorkspaceWs(token: string, shareId: string, opts: { hello?: boolean } = {}): Promise<WsBin> {
  const res = await shareWsRaw(token, shareId);
  const ws = res.webSocket;
  if (res.status !== 101 || !ws) throw new Error(`workspace ws upgrade failed: ${res.status} ${await res.text()}`);
  const frames: WsBinFrame[] = [];
  const texts: string[] = [];
  const taken = new Set<number>();
  const waiters = new Set<() => void>();
  let onClose: (v: { code: number; reason: string }) => void = () => {};
  const closed = new Promise<{ code: number; reason: string }>((resolve) => (onClose = resolve));
  ws.addEventListener("message", (e) => {
    if (typeof e.data === "string") {
      texts.push(e.data);
      return;
    }
    const buf = new Uint8Array(e.data as ArrayBuffer).slice().buffer; // a copy owned by this realm
    const header = parseHeader(buf);
    if (header === null) throw new Error(`hub sent a frame that does not parse (${buf.byteLength} bytes)`);
    frames.push({ header, payload: new Uint8Array(buf.slice(20)), kind: nameOf(header.kind) });
    for (const w of [...waiters]) w();
  });
  ws.addEventListener("close", (e) => onClose({ code: e.code, reason: e.reason }));
  ws.binaryType = "arraybuffer"; // the standard default is "blob"
  ws.accept();
  const next = (kind: string, pred: (f: WsBinFrame) => boolean = () => true, timeoutMs = 3000): Promise<WsBinFrame> =>
    new Promise((resolve, reject) => {
      const check = (): boolean => {
        const i = frames.findIndex((f, idx) => !taken.has(idx) && f.kind === kind && pred(f));
        if (i < 0) return false;
        taken.add(i);
        resolve(frames[i]!);
        return true;
      };
      if (check()) return;
      const waiter = () => {
        if (check()) {
          waiters.delete(waiter);
          clearTimeout(timer);
        }
      };
      const timer = setTimeout(() => {
        waiters.delete(waiter);
        reject(new Error(`timed out waiting for ${kind}; got [${frames.map((f) => f.kind).join(", ")}]`));
      }, timeoutMs);
      waiters.add(waiter);
    });
  const client: WsBin = {
    ws,
    frames,
    texts,
    send(kind, o = {}) {
      ws.send(frame(kind, o.from ?? 0, o.to ?? 255, o.payload, o.flags ?? 0, o.seq ?? 0n));
    },
    sendRaw(data) {
      ws.send(data);
    },
    next,
    all: (kind) => frames.filter((f) => f.kind === kind),
    closed,
    close(code = 1000) {
      ws.close(code);
    },
  };
  if (opts.hello !== false) await next("srv_hello");
  return client;
}
