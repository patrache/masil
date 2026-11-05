// Shared test helpers (server-architecture.md 7.2).
import { env, SELF } from "cloudflare:test";
import { newId } from "../src/lib/ids";
import type { Me, PollInput, Post, SpaceDetail, Upload } from "../src/types/api";

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
