// Shared test helpers (server-architecture.md 7.2). Later tasks add makeFriends/makeSpace.
import { SELF } from "cloudflare:test";
import { newId } from "../src/lib/ids";
import type { Me } from "../src/types/api";

export const BASE = "http://masil.test" as const;

const TEST_UA = "masil/0.1.0 (test; test)";

function randomBase36(len: number): string {
  const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz";
  let out = "";
  for (const b of crypto.getRandomValues(new Uint8Array(len))) out += alphabet[b % 36];
  return out;
}

let handleSeq = 0;

/** e.g. "t_k3j9x2ab" — matches ^[a-z0-9_]{3,20}$ and is unique per call (<= 20 chars). */
export function uniqueHandle(prefix = "t"): string {
  handleSeq++;
  const tail = handleSeq.toString(36) + randomBase36(8);
  const p = prefix.toLowerCase().replace(/[^a-z0-9_]/g, "").slice(0, 8) || "t";
  return `${p}_${tail}`.slice(0, 20);
}

// Random 16-bit start per isolate + a counter, so test files running in parallel don't share IPs.
let ipSeq = crypto.getRandomValues(new Uint32Array(1))[0] & 0xffffff;

/** A fresh "10.x.y.z" per call -> sidesteps RL_SIGNUP/RL_LOGIN in tests. */
export function uniqueIp(): string {
  ipSeq = (ipSeq + 1) & 0xffffff;
  return `10.${(ipSeq >> 16) & 0xff}.${(ipSeq >> 8) & 0xff}.${ipSeq & 0xff}`;
}

export interface ApiResult<T> {
  status: number;
  body: T;
  headers: Headers;
}

export interface ApiClient {
  get<T = unknown>(path: string): Promise<ApiResult<T>>;
  post<T = unknown>(path: string, json?: unknown): Promise<ApiResult<T>>;
  put<T = unknown>(path: string, json?: unknown): Promise<ApiResult<T>>;
  patch<T = unknown>(path: string, json?: unknown): Promise<ApiResult<T>>;
  del<T = unknown>(path: string): Promise<ApiResult<T>>;
  raw(path: string, init: RequestInit): Promise<Response>;
}

/** Sets Authorization (when token), the masil User-Agent and a fresh CF-Connecting-IP on every call. */
export function api(token?: string): ApiClient {
  const raw = (path: string, init: RequestInit = {}): Promise<Response> => {
    const headers = new Headers(init.headers);
    if (!headers.has("User-Agent")) headers.set("User-Agent", TEST_UA);
    if (!headers.has("CF-Connecting-IP")) headers.set("CF-Connecting-IP", uniqueIp());
    if (token !== undefined && !headers.has("Authorization")) headers.set("Authorization", `Bearer ${token}`);
    return SELF.fetch(BASE + path, { ...init, headers });
  };
  const call = async <T>(method: string, path: string, json?: unknown): Promise<ApiResult<T>> => {
    const init: RequestInit = { method };
    if (json !== undefined) {
      init.headers = { "Content-Type": "application/json" };
      init.body = JSON.stringify(json);
    }
    const res = await raw(path, init);
    const text = await res.text();
    let body: unknown = null;
    if (text !== "") {
      try {
        body = JSON.parse(text);
      } catch {
        body = text;
      }
    }
    return { status: res.status, body: body as T, headers: res.headers };
  };
  return {
    get: (path) => call("GET", path),
    post: (path, json) => call("POST", path, json),
    put: (path, json) => call("PUT", path, json),
    patch: (path, json) => call("PATCH", path, json),
    del: (path) => call("DELETE", path),
    raw,
  };
}

export interface TestUser {
  token: string;
  user: Me;
  handle: string;
  password: string;
}

/** POST /v1/auth/signup with a fresh handle (and IP); throws unless 201. */
export async function signup(opts: { handle?: string; password?: string; displayName?: string } = {}): Promise<TestUser> {
  const handle = opts.handle ?? uniqueHandle();
  const password = opts.password ?? `pw-${randomBase36(12)}`;
  const body: Record<string, unknown> = { handle, password };
  if (opts.displayName !== undefined) body.display_name = opts.displayName;
  const r = await api().post<{ token: string; user: Me }>("/v1/auth/signup", body);
  if (r.status !== 201) throw new Error(`signup ${handle} failed: ${r.status} ${JSON.stringify(r.body)}`);
  return { token: r.body.token, user: r.body.user, handle: r.body.user.handle, password };
}

/** POST /v1/auth/login; throws unless 200. */
export async function login(handle: string, password: string): Promise<{ token: string }> {
  const r = await api().post<{ token: string }>("/v1/auth/login", { handle, password });
  if (r.status !== 200) throw new Error(`login ${handle} failed: ${r.status} ${JSON.stringify(r.body)}`);
  return { token: r.body.token };
}

// ---- WebSocket (events.md 3) ----

export interface WsFrame {
  type: string;
  seq: number | null;
  data: any;
}

export interface WsClient {
  ws: WebSocket;
  /** Every text frame received so far, in order (hello, replayed events, ready, ...). `pong` has seq/data null. */
  frames: WsFrame[];
  send(frame: object | string): void;
  /** The first not-yet-returned frame of `type` (matching `pred`), already received or arriving within `timeoutMs`. */
  next(type: string, pred?: (data: any) => boolean, timeoutMs?: number): Promise<WsFrame>;
  close(code?: number): void;
}

/** Opens /v1/ws via fetch with Upgrade header, accepts, waits for hello, sends resume, waits for ready. */
export async function connectWs(
  token: string,
  opts: { mode?: "full" | "notify"; deviceId?: string; resume?: { epoch: string | null; last_seq: number | null } } = {},
): Promise<WsClient> {
  const mode = opts.mode ?? "full";
  const deviceId = opts.deviceId ?? newId();
  const res = await api(token).raw(`/v1/ws?mode=${mode}&device_id=${encodeURIComponent(deviceId)}`, {
    headers: { Upgrade: "websocket" },
  });
  const ws = res.webSocket;
  if (res.status !== 101 || !ws) throw new Error(`ws upgrade failed: ${res.status} ${await res.text()}`);

  const frames: WsFrame[] = [];
  const taken = new Set<number>(); // indexes already returned by next()
  const waiters = new Set<() => void>();
  ws.addEventListener("message", (e) => {
    if (typeof e.data !== "string") return;
    const f = JSON.parse(e.data) as Partial<WsFrame>;
    frames.push({ type: String(f.type), seq: f.seq ?? null, data: f.data ?? null });
    for (const w of [...waiters]) w();
  });
  ws.accept();

  /** Waits (without consuming) for a frame of `type`. */
  const waitFor = (type: string, pred: (data: any) => boolean, timeoutMs: number, consume: boolean): Promise<WsFrame> =>
    new Promise((resolve, reject) => {
      const check = (): boolean => {
        const i = frames.findIndex((f, idx) => !taken.has(idx) && f.type === type && pred(f.data));
        if (i < 0) return false;
        if (consume) taken.add(i);
        resolve(frames[i]);
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
        reject(new Error(`timed out waiting for ${type}; got [${frames.map((f) => f.type).join(", ")}]`));
      }, timeoutMs);
      waiters.add(waiter);
    });

  const client: WsClient = {
    ws,
    frames,
    send(frame) {
      ws.send(typeof frame === "string" ? frame : JSON.stringify(frame));
    },
    next(type, pred = () => true, timeoutMs = 3000) {
      return waitFor(type, pred, timeoutMs, true);
    },
    close(code = 1000) {
      ws.close(code);
    },
  };

  await waitFor("hello", () => true, 3000, false);
  client.send({ type: "resume", data: opts.resume ?? { epoch: null, last_seq: null } });
  await waitFor("ready", () => true, 3000, false);
  return client;
}
