// Bearer session auth (api.md 1.3, server-architecture.md 4.6).
// requireAuth is attached per route (never `.use(prefix, requireAuth)` — see plan 01 Global Constraints).
import type { MiddlewareHandler } from "hono";
import type { AppContext, AppEnv, AuthContext, Env } from "../env";
import type { UserRow } from "../types/rows";
import { sha256Hex } from "./crypto";
import { first, run } from "./db";
import { err } from "./errors";
import { isId } from "./ids";
import { checkLimit } from "./ratelimit";
import { now } from "./time";

export const SESSION_TTL_MS = 90 * 24 * 60 * 60 * 1000; // sliding: last_used_at + 90 days
const TOUCH_AFTER_MS = 60 * 60 * 1000; // refresh last_used_at/expires_at at most once per hour

const TOKEN_RE = /^msl_[A-Za-z0-9_-]{43}$/;
const BEARER_RE = /^Bearer[ \t]+(\S+)[ \t]*$/i;
const HANDLE_REF_RE = /^@[a-z0-9_]{3,20}$/;

interface SessionAuthRow {
  session_id: string;
  last_used_at: number;
  user_id: string;
  handle: string;
  role: "user" | "admin";
  suspended_at: number | null;
  suspended_until: number | null;
  suspend_reason: string | null;
}

/** Suspended right now (suspended_until in the past counts as active again). */
export function isSuspendedNow(u: { suspended_at: number | null; suspended_until: number | null }, nowMs: number): boolean {
  return u.suspended_at !== null && (u.suspended_until === null || u.suspended_until > nowMs);
}

async function lookupSession(env: Env, token: string, nowMs: number): Promise<SessionAuthRow | null> {
  if (!TOKEN_RE.test(token)) return null;
  const hash = await sha256Hex(token);
  const row = await first<SessionAuthRow>(
    env.DB,
    `SELECT s.id AS session_id, s.last_used_at, u.id AS user_id, u.handle, u.role,
            u.suspended_at, u.suspended_until, u.suspend_reason
       FROM sessions s JOIN users u ON u.id = s.user_id
      WHERE s.token_hash = ?1 AND s.revoked_at IS NULL AND s.expires_at > ?2`,
    hash,
    nowMs,
  );
  if (row === null) return null;
  if (isSuspendedNow(row, nowMs)) throw err.suspended(row.suspended_until, row.suspend_reason ?? "");
  return row;
}

function toAuth(row: SessionAuthRow): AuthContext {
  return { userId: row.user_id, sessionId: row.session_id, handle: row.handle, role: row.role };
}

/** null for unknown/revoked/expired tokens; throws err.suspended for suspended users. */
export async function authenticateToken(env: Env, token: string): Promise<AuthContext | null> {
  const row = await lookupSession(env, token, now());
  return row === null ? null : toAuth(row);
}

/** Bearer -> session -> user; 401/403; sets c.var.auth; then RL_API; touches last_used_at (>= 1h old). */
export const requireAuth: MiddlewareHandler<AppEnv> = async (c, next) => {
  const m = BEARER_RE.exec(c.req.header("Authorization") ?? "");
  if (!m) throw err.unauthorized();
  const t = now();
  const row = await lookupSession(c.env, m[1], t);
  if (row === null) throw err.unauthorized();
  const auth = toAuth(row);
  c.set("auth", auth);
  await checkLimit(c.env, "RL_API", `u:${auth.userId}`);
  if (t - row.last_used_at >= TOUCH_AFTER_MS) {
    await run(
      c.env.DB,
      "UPDATE sessions SET last_used_at = ?1, expires_at = ?2 WHERE id = ?3 AND revoked_at IS NULL",
      t,
      t + SESSION_TTL_MS,
      auth.sessionId,
    );
  }
  await next();
};

/** After requireAuth: global operators only (403 not_admin). */
export const requireAdmin: MiddlewareHandler<AppEnv> = async (c, next) => {
  if (getAuth(c).role !== "admin") throw err.forbidden("not_admin");
  await next();
};

export function getAuth(c: AppContext): AuthContext {
  const auth = c.get("auth");
  if (!auth) throw err.unauthorized(); // route forgot requireAuth: fail closed
  return auth;
}

/** "@handle" | ULID | "@me" -> user row; 404 if missing or malformed. */
export async function resolveUserRef(db: D1Database, ref: string, me: AuthContext): Promise<UserRow> {
  let row: UserRow | null = null;
  const lower = ref.toLowerCase();
  if (lower === "@me") row = await first<UserRow>(db, "SELECT * FROM users WHERE id = ?1", me.userId);
  else if (HANDLE_REF_RE.test(lower)) row = await first<UserRow>(db, "SELECT * FROM users WHERE handle = ?1", lower.slice(1));
  else if (isId(ref)) row = await first<UserRow>(db, "SELECT * FROM users WHERE id = ?1", ref);
  if (row === null) throw err.notFound("user");
  return row;
}
