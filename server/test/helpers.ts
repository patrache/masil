// Shared test helpers (server-architecture.md 7.2). Later tasks add signup/login/connectWs/makeFriends/makeSpace.
import { SELF } from "cloudflare:test";

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
