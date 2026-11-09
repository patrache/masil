// Shared test helpers (server-architecture.md 7.2).
import { env, SELF } from "cloudflare:test";
import { afterAll, beforeAll } from "vitest";
import { DOC_TYPES } from "../src/lib/collab-doctypes";
import { newId } from "../src/lib/ids";
import type { Doc, Me, PollInput, Post, SpaceDetail, Upload } from "../src/types/api";

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

/**
 * Runs `attempt` so that every rate-limit hit it makes lands in one limiter window. Miniflare's limiter (the one
 * vitest-pool-workers runs) counts in fixed windows aligned to the wall clock — `epoch = Math.floor(Date.now() /
 * (period * 1e3))` in miniflare/dist/src/workers/ratelimit/ratelimit-object.worker.js — and the first hit of a new
 * window deletes every counter of that period. So a test that spends N hits and expects the (N+1)th to be 429 fails
 * whenever a boundary falls between them. When `attempt` throws and the clock crossed a `periodSec` boundary during
 * it, it is run again from scratch (it must make its own fresh users / IPs / keys); the retry starts just after the
 * boundary, so it has the whole window. A failure within one window is rethrown at once: a crossing can only reset
 * counts, never cause an early 429, so this hides no real failure.
 */
export async function inOneRateWindow<T>(periodSec: 10 | 60, attempt: () => Promise<T>, now: () => number = Date.now): Promise<T> {
  const windowOf = (t: number) => Math.floor(t / (periodSec * 1000));
  for (let tries = 1; ; tries++) {
    const before = windowOf(now());
    try {
      return await attempt();
    } catch (e) {
      if (tries >= 3 || windowOf(now()) === before) throw e;
    }
  }
}

/**
 * Polls `probe` every `stepMs` until `ok(value)` holds or `timeoutMs` has passed, and returns the last value (the
 * caller asserts on it, so a timeout fails with the real state). For state the server writes after the request or
 * frame the test awaited — a socket close handled by the hub, a D1 row written in the background: a fixed sleep
 * before reading it is flaky under full-suite load (v0.1.1 S3). The default 3000 ms (as WsClient.next) stays under
 * vitest's default testTimeout of 5000 ms (vitest.config.ts sets none), so the runner never kills the test before the
 * caller's assertion runs.
 */
export async function waitFor<T>(
  probe: () => Promise<T> | T,
  ok: (value: T) => boolean,
  { timeoutMs = 3000, stepMs = 25 }: { timeoutMs?: number; stepMs?: number } = {},
): Promise<T> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const value = await probe();
    if (ok(value) || Date.now() >= deadline) return value;
    await new Promise<void>((r) => setTimeout(r, stepMs));
  }
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
  opts: {
    mode?: "full" | "notify";
    deviceId?: string;
    resume?: { epoch: string | null; last_seq: number | null };
    /** extra upgrade request headers (e.g. Accept-Language, v0.1.3) */
    headers?: Record<string, string>;
  } = {},
): Promise<WsClient> {
  const mode = opts.mode ?? "full";
  const deviceId = opts.deviceId ?? newId();
  const res = await api(token).raw(`/v1/ws?mode=${mode}&device_id=${encodeURIComponent(deviceId)}`, {
    headers: { ...opts.headers, Upgrade: "websocket" },
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

// ---- relations ----

/** a -> b friend request + b accepts, through the API (a crossing pending request from b auto-accepts with 200). */
export async function makeFriends(a: TestUser, b: TestUser): Promise<void> {
  const r = await api(a.token).post<{ request: { id: string } }>("/v1/friends/requests", { user: `@${b.handle}` });
  if (r.status === 200) return;
  if (r.status !== 201) throw new Error(`friend request ${a.handle} -> ${b.handle} failed: ${r.status} ${JSON.stringify(r.body)}`);
  const acc = await api(b.token).post(`/v1/friends/requests/${r.body.request.id}/accept`);
  if (acc.status !== 200) throw new Error(`friend accept ${b.handle} failed: ${acc.status} ${JSON.stringify(acc.body)}`);
}

// ---- spaces ----

/**
 * `owner` creates a space through the API, makes one invite link (no use cap) and every member accepts it.
 * Returns the space as the owner sees it after the joins, and its default channel (`일반`).
 */
export async function makeSpace(owner: TestUser, members: TestUser[] = []): Promise<{ space: SpaceDetail; generalChannelId: string }> {
  const created = await api(owner.token).post<{ space: SpaceDetail }>("/v1/spaces", { name: `방 ${randomBase36(6)}` });
  if (created.status !== 201) throw new Error(`space create failed: ${created.status} ${JSON.stringify(created.body)}`);
  const spaceId = created.body.space.id;
  if (members.length > 0) {
    const inv = await api(owner.token).post<{ invite: { code: string } }>(`/v1/spaces/${spaceId}/invites`, { max_uses: null });
    if (inv.status !== 201) throw new Error(`invite create failed: ${inv.status} ${JSON.stringify(inv.body)}`);
    for (const m of members) {
      const r = await api(m.token).post(`/v1/invites/${inv.body.invite.code}/accept`);
      if (r.status !== 200) throw new Error(`invite accept ${m.handle} failed: ${r.status} ${JSON.stringify(r.body)}`);
    }
  }
  const detail = await api(owner.token).get<{ space: SpaceDetail }>(`/v1/spaces/${spaceId}`);
  if (detail.status !== 200) throw new Error(`space get failed: ${detail.status} ${JSON.stringify(detail.body)}`);
  return { space: detail.body.space, generalChannelId: detail.body.space.default_channel_id };
}

// ---- room documents (v0.1.3, api.md 5.24) ----

let docSeq = 0;

/** POST /v1/spaces/{spaceId}/docs as `owner` (a member of that space); throws unless 201. */
export async function makeDoc(owner: TestUser, spaceId: string, type: "arch" | "apicol" = "arch", snapshot?: unknown): Promise<Doc> {
  const body: Record<string, unknown> = { type, title: `문서 ${++docSeq}` };
  if (snapshot !== undefined) body.snapshot = snapshot;
  const r = await api(owner.token).post<{ doc: Doc }>(`/v1/spaces/${spaceId}/docs`, body);
  if (r.status !== 201) throw new Error(`doc create failed: ${r.status} ${JSON.stringify(r.body)}`);
  return r.body.doc;
}

export interface DocClient {
  ws: WebSocket;
  /** every frame received so far (parsed), in order */
  frames: any[];
  /** the hello the server answered with (after the `hello{since}` connectDoc sent) */
  hello: any;
  send(frame: object | string): void;
  /** the first not-yet-returned frame with this `t` (matching `pred`), already received or arriving within `timeoutMs` */
  next(t: string, pred?: (f: any) => boolean, timeoutMs?: number): Promise<any>;
  /** resolves with the close code once the server closes the socket */
  closed: Promise<{ code: number; reason: string }>;
  close(code?: number): void;
}

/**
 * Opens GET /v1/docs/{docId}/ws (collab.md 2), sends `hello{since}` (default null) and waits for the server's hello.
 * `since: "skip"` sends no hello (the caller drives the handshake; `hello` is then null). Throws unless 101.
 */
export async function connectDoc(
  token: string,
  docId: string,
  opts: { since?: number | null | "skip"; headers?: Record<string, string> } = {},
): Promise<DocClient> {
  const res = await api(token).raw(`/v1/docs/${docId}/ws`, { headers: { ...opts.headers, Upgrade: "websocket" } });
  const ws = res.webSocket;
  if (res.status !== 101 || !ws) throw new Error(`doc ws upgrade failed: ${res.status} ${await res.text()}`);
  const frames: any[] = [];
  const taken = new Set<number>();
  const waiters = new Set<() => void>();
  let onClose: (v: { code: number; reason: string }) => void = () => {};
  const closed = new Promise<{ code: number; reason: string }>((resolve) => (onClose = resolve));
  ws.addEventListener("message", (e) => {
    if (typeof e.data !== "string") return;
    frames.push(JSON.parse(e.data));
    for (const w of [...waiters]) w();
  });
  ws.addEventListener("close", (e) => onClose({ code: e.code, reason: e.reason }));
  ws.accept();
  const next = (t: string, pred: (f: any) => boolean = () => true, timeoutMs = 3000): Promise<any> =>
    new Promise((resolve, reject) => {
      const check = (): boolean => {
        const i = frames.findIndex((f, idx) => !taken.has(idx) && f.t === t && pred(f));
        if (i < 0) return false;
        taken.add(i);
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
        reject(new Error(`timed out waiting for ${t}; got [${frames.map((f) => f.t).join(", ")}]`));
      }, timeoutMs);
      waiters.add(waiter);
    });
  const client: DocClient = {
    ws,
    frames,
    hello: null,
    send(frame) {
      ws.send(typeof frame === "string" ? frame : JSON.stringify(frame));
    },
    next,
    closed,
    close(code = 1000) {
      ws.close(code);
    },
  };
  if (opts.since !== "skip") {
    client.send({ t: "hello", since: opts.since ?? null });
    client.hello = await next("hello");
  }
  return client;
}

// ---- SNS fixtures (plan 02 Task 1; every later SNS test uses these) ----

/** A valid 1x1 PNG (70 bytes). */
export const PNG_1X1: Uint8Array = Uint8Array.from(
  atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="),
  (ch) => ch.charCodeAt(0),
);

/** POST /v1/uploads?purpose=… with PNG_1X1; throws unless 201. */
export async function uploadPng(t: TestUser, purpose: "avatar" | "post" | "story"): Promise<Upload> {
  const res = await api(t.token).raw(`/v1/uploads?purpose=${purpose}`, {
    method: "POST",
    headers: { "Content-Type": "image/png" },
    body: PNG_1X1,
  });
  const text = await res.text();
  if (res.status !== 201) throw new Error(`upload ${purpose} by ${t.handle} failed: ${res.status} ${text}`);
  return (JSON.parse(text) as { upload: Upload }).upload;
}

/** POST /v1/posts with a fresh client_nonce; throws unless 201. */
export async function createPost(
  t: TestUser,
  body: string,
  extra: { media_upload_ids?: string[]; reply_to_id?: string; quote_of_id?: string; poll?: PollInput } = {},
): Promise<Post> {
  const r = await api(t.token).post<{ post: Post }>("/v1/posts", { body, client_nonce: crypto.randomUUID(), ...extra });
  if (r.status !== 201) throw new Error(`post by ${t.handle} failed: ${r.status} ${JSON.stringify(r.body)}`);
  return r.body.post;
}

/** PUT /v1/users/@b/follow as a; returns `state` ("pending" for a private b); throws unless 200. */
export async function follow(a: TestUser, b: TestUser): Promise<"active" | "pending"> {
  const r = await api(a.token).put<{ state: "active" | "pending" }>(`/v1/users/@${b.handle}/follow`);
  if (r.status !== 200) throw new Error(`follow ${a.handle} -> ${b.handle} failed: ${r.status} ${JSON.stringify(r.body)}`);
  return r.body.state;
}

/** Makes t a global operator directly in D1 (the role is read per request by requireAuth). */
export async function makeAdmin(t: TestUser): Promise<void> {
  await env.DB.prepare("UPDATE users SET role = 'admin' WHERE id = ?").bind(t.user.id).run();
}

let watchSeq = 0;

/** Sends a `watch` frame (full replace) and waits for its ok reply. */
export async function watch(c: WsClient, topics: string[]): Promise<void> {
  const ref = `w-${++watchSeq}`;
  c.send({ type: "watch", ref, data: { topics } });
  const reply = await c.next("reply", (d) => d?.ref === ref);
  if (reply.data.ok !== true) throw new Error(`watch ${ref} failed: ${JSON.stringify(reply.data.error)}`);
}

/**
 * The P0 CollabHub tests exercise sequencing, limits and sockets with generic objects ("n1", field "cpu", 16 KB
 * strings) that the `arch` field rules (P3 Task 21, src/lib/collab/arch.ts) refuse. Calling this at the top of such a
 * file turns the arch validateOp off for that file only; the arch rules have their own tests
 * (unit/arch-doctype.test.ts, realtime/collab-arch.test.ts).
 */
export function archRulesOff(): void {
  const real = DOC_TYPES.arch.validateOp;
  beforeAll(() => {
    DOC_TYPES.arch.validateOp = () => null;
  });
  afterAll(() => {
    DOC_TYPES.arch.validateOp = real;
  });
}
