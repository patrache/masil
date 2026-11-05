// D1 helpers (server-architecture.md 4.4).
// - `undefined` params bind as `null` (D1 rejects undefined).
// - Variable-length ID lists go through `jsonList` as ONE param: `IN (SELECT value FROM json_each(?))`
//   (D1 caps bound params at 100 per query).

function bindParams(params: unknown[]): unknown[] {
  return params.map((p) => (p === undefined ? null : p));
}

export function stmt(db: D1Database, sql: string, ...params: unknown[]): D1PreparedStatement {
  const s = db.prepare(sql);
  return params.length > 0 ? s.bind(...bindParams(params)) : s;
}

export function first<T>(db: D1Database, sql: string, ...params: unknown[]): Promise<T | null> {
  return stmt(db, sql, ...params).first<T>();
}

export async function all<T>(db: D1Database, sql: string, ...params: unknown[]): Promise<T[]> {
  const res = await stmt(db, sql, ...params).all<T>();
  return res.results;
}

export function run(db: D1Database, sql: string, ...params: unknown[]): Promise<D1Result> {
  return stmt(db, sql, ...params).run();
}

/** Atomic: D1 runs a batch as one transaction (all or nothing). */
export function batch(db: D1Database, stmts: D1PreparedStatement[]): Promise<D1Result[]> {
  return db.batch(stmts);
}

/** For IN-lists: `WHERE id IN (SELECT value FROM json_each(?))` with jsonList(ids) as the single param. */
export function jsonList(ids: readonly string[]): string {
  return JSON.stringify(ids);
}

export interface Cursor {
  k: string | number;
  i: string;
}

export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}

// TODO(Task 5): replace with err.invalid({ cursor: "invalid" }) / err.invalid({ limit: ... }).
function invalidCursor(): never {
  throw new Error("invalid_cursor");
}

function invalidLimit(): never {
  throw new Error("invalid_limit");
}

function toBase64Url(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function fromBase64Url(s: string): string {
  let b64 = s.replace(/-/g, "+").replace(/_/g, "/");
  b64 += "=".repeat((4 - (b64.length % 4)) % 4);
  const bin = atob(b64); // throws on invalid characters
  const bytes = Uint8Array.from(bin, (ch) => ch.charCodeAt(0));
  return new TextDecoder("utf-8", { fatal: true, ignoreBOM: false }).decode(bytes);
}

/** Opaque cursor: base64url(JSON {k, i}) without padding. */
export function encodeCursor(c: Cursor): string {
  return toBase64Url(JSON.stringify({ k: c.k, i: c.i }));
}

/** `undefined`/empty → null; anything malformed throws (invalid cursor). */
export function decodeCursor(s: string | undefined): Cursor | null {
  if (s === undefined || s === "") return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(fromBase64Url(s));
  } catch {
    invalidCursor();
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) invalidCursor();
  const { k, i } = parsed as { k?: unknown; i?: unknown };
  const kOk = typeof k === "string" || (typeof k === "number" && Number.isFinite(k));
  if (!kOk || typeof i !== "string") invalidCursor();
  return { k: k as string | number, i };
}

/** Missing/empty → def; non-integer or < 1 throws; above max is clamped to max. */
export function parseLimit(raw: string | undefined, def = 30, max = 100): number {
  if (raw === undefined || raw === "") return def;
  if (!/^\d+$/.test(raw)) invalidLimit();
  const n = Number(raw);
  if (n < 1) invalidLimit();
  return n > max ? max : n;
}
