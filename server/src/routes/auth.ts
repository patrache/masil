// /v1/auth/* (api.md 1.4). signup/login are the only unauthenticated routes here; every other path takes
// requireAuth as a per-route argument (a `.use("/auth/*", requireAuth)` would also gate signup/login).
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppContext, type AppEnv } from "../env";
import { getAuth, isSuspendedNow, requireAuth, SESSION_TTL_MS } from "../lib/auth";
import { dummyVerify, hashPassword, newSessionToken, sha256Hex, verifyPassword } from "../lib/crypto";
import { all, batch, first, run, stmt } from "../lib/db";
import { ApiError, err } from "../lib/errors";
import { isId, newId } from "../lib/ids";
import { checkLimit, clientIp, limit } from "../lib/ratelimit";
import { serializeMe, serializeMeLive, serializeSession } from "../lib/serialize";
import { charCount, normalizeText } from "../lib/text";
import { now } from "../lib/time";
import { vJson, zText } from "../lib/validate";
import { revokeSessions } from "../services/users";
import type { SessionRow, UserRow } from "../types/rows";

export const authRoutes = new Hono<AppEnv>();

const HANDLE_RE = /^[a-z0-9_]{3,20}$/;
const RESERVED_HANDLES = new Set(["me", "admin", "masil", "here", "system", "root", "support"]);
const PASSWORD_MIN = 8;
const PASSWORD_MAX = 128;
const USER_AGENT_MAX = 512;

type SessionClient = SessionRow["client"];

/** NFC + trim + lowercase; then ^[a-z0-9_]{3,20}$ and not reserved. */
const zHandle = z
  .string()
  .transform((s) => normalizeText(s).toLowerCase())
  .refine((h) => HANDLE_RE.test(h) && !RESERVED_HANDLES.has(h), { message: "invalid handle" });

/**
 * Passwords are NFC-normalized (so the same Korean password typed on macOS (NFD) and elsewhere matches) but
 * never trimmed; 8..128 code points.
 */
const zNewPassword = z.string().transform((raw, ctx) => {
  const s = raw.normalize("NFC");
  const n = charCount(s);
  if (n < PASSWORD_MIN) {
    ctx.addIssue({ code: "too_small", origin: "string", minimum: PASSWORD_MIN, inclusive: true, input: s });
    return z.NEVER;
  }
  if (n > PASSWORD_MAX) {
    ctx.addIssue({ code: "too_big", origin: "string", maximum: PASSWORD_MAX, inclusive: true, input: s });
    return z.NEVER;
  }
  return s;
});

const zClient = z.enum(["tui", "daemon", "admin", "other"]);

const SignupBody = z
  .object({
    handle: zHandle,
    password: zNewPassword,
    display_name: zText(1, 32).optional(),
    client: zClient.optional(),
    device_name: zText(0, 64).optional(),
  })
  .superRefine((v, ctx) => {
    if (typeof v.password === "string" && typeof v.handle === "string" && v.password.toLowerCase() === v.handle) {
      ctx.addIssue({ code: "custom", path: ["password"], message: "password must differ from handle", input: v.password });
    }
  });

// Login never reveals whether a handle exists: any string is accepted and an unknown/malformed handle simply
// fails as invalid_credentials (after a dummy hash for timing parity).
const LoginBody = z.object({
  handle: z.string().min(1).max(64).transform((s) => normalizeText(s).toLowerCase()),
  password: z.string().min(1).max(1024).transform((s) => s.normalize("NFC")),
  client: zClient.optional(),
  device_name: zText(0, 64).optional(),
});

// current_password is only compared (any string, like login); new_password follows the signup rules (incl. != handle,
// checked in the handler since the handle comes from the session).
const PasswordBody = z.object({
  current_password: z.string().min(1).max(1024).transform((s) => s.normalize("NFC")),
  new_password: zNewPassword,
});

function invalidCredentials(): ApiError {
  return new ApiError(401, "invalid_credentials", "invalid handle or password");
}

interface NewSession {
  token: string;
  row: SessionRow;
  insert: D1PreparedStatement;
}

async function newSession(c: AppContext, userId: string, client: SessionClient, deviceName: string, t: number): Promise<NewSession> {
  const token = newSessionToken();
  const row: SessionRow = {
    id: newId(t),
    user_id: userId,
    token_hash: await sha256Hex(token),
    client,
    device_name: deviceName,
    ip: clientIp(c),
    user_agent: c.req.header("User-Agent")?.slice(0, USER_AGENT_MAX) ?? null,
    created_at: t,
    last_used_at: t,
    expires_at: t + SESSION_TTL_MS,
    revoked_at: null,
    revoke_reason: null,
  };
  const insert = stmt(
    c.env.DB,
    `INSERT INTO sessions (id, user_id, token_hash, client, device_name, ip, user_agent, created_at, last_used_at, expires_at)
     VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)`,
    row.id,
    row.user_id,
    row.token_hash,
    row.client,
    row.device_name,
    row.ip,
    row.user_agent,
    row.created_at,
    row.last_used_at,
    row.expires_at,
  );
  return { token, row, insert };
}

function isHandleUniqueViolation(e: unknown): boolean {
  return e instanceof Error && /UNIQUE constraint failed: users\.handle/.test(e.message);
}

authRoutes.post(
  "/auth/signup",
  limit("RL_SIGNUP", (c) => `ip:${clientIp(c)}`),
  vJson(SignupBody),
  async (c) => {
    const body = c.req.valid("json");
    const db = c.env.DB;
    if (await first(db, "SELECT 1 AS x FROM users WHERE handle = ?1", body.handle)) throw err.conflict("handle_taken");
    const t = now();
    const userId = newId(t);
    const passwordHash = await hashPassword(body.password);
    const session = await newSession(c, userId, body.client ?? "tui", body.device_name ?? "", t);
    try {
      await batch(db, [
        stmt(
          db,
          `INSERT INTO users (id, handle, display_name, password_hash, created_at, updated_at)
           VALUES (?1, ?2, ?3, ?4, ?5, ?5)`,
          userId,
          body.handle,
          body.display_name ?? body.handle,
          passwordHash,
          t,
        ),
        stmt(db, "INSERT INTO user_settings (user_id, updated_at) VALUES (?1, ?2)", userId, t),
        stmt(db, "INSERT INTO user_presence (user_id, updated_at) VALUES (?1, ?2)", userId, t),
        session.insert,
      ]);
    } catch (e) {
      if (isHandleUniqueViolation(e)) throw err.conflict("handle_taken"); // lost a race with a concurrent signup
      throw e;
    }
    const user = await serializeMe(db, userId);
    return c.json({ token: session.token, session: serializeSession(session.row, session.row.id), user }, 201);
  },
);

authRoutes.post("/auth/login", vJson(LoginBody), async (c) => {
  const body = c.req.valid("json");
  const db = c.env.DB;
  await checkLimit(c.env, "RL_LOGIN", `ip:${clientIp(c)}`);
  await checkLimit(c.env, "RL_LOGIN", `handle:${body.handle}`);
  const user = await first<UserRow>(db, "SELECT * FROM users WHERE handle = ?1", body.handle);
  if (user === null) {
    await dummyVerify();
    throw invalidCredentials();
  }
  if (!(await verifyPassword(body.password, user.password_hash))) throw invalidCredentials();
  const t = now();
  if (isSuspendedNow(user, t)) throw err.suspended(user.suspended_until, user.suspend_reason ?? "");
  const session = await newSession(c, user.id, body.client ?? "tui", body.device_name ?? "", t);
  await session.insert.run();
  const me = await serializeMeLive(c.env, user.id);
  return c.json({ token: session.token, session: serializeSession(session.row, session.row.id), user: me }, 200);
});

/** My active sessions, newest first (no pagination). */
authRoutes.get("/auth/sessions", requireAuth, async (c) => {
  const auth = getAuth(c);
  const rows = await all<SessionRow>(
    c.env.DB,
    `SELECT * FROM sessions WHERE user_id = ?1 AND revoked_at IS NULL AND expires_at > ?2
      ORDER BY created_at DESC, id DESC`,
    auth.userId,
    now(),
  );
  return c.json({ items: rows.map((r) => serializeSession(r, auth.sessionId)) });
});

/** Revokes the current session (logout); its sockets get session.revoked and close 4001. */
authRoutes.post("/auth/logout", requireAuth, async (c) => {
  const auth = getAuth(c);
  await revokeSessions(rt(c), auth.userId, [auth.sessionId], "logout");
  return c.body(null, 204);
});

/** Revokes one of my active sessions (the current one too); someone else's, unknown or already dead -> 404. */
authRoutes.delete("/auth/sessions/:session_id", requireAuth, async (c) => {
  const auth = getAuth(c);
  const id = c.req.param("session_id");
  if (!isId(id)) throw err.notFound("session");
  const row = await first<{ id: string }>(
    c.env.DB,
    "SELECT id FROM sessions WHERE id = ?1 AND user_id = ?2 AND revoked_at IS NULL AND expires_at > ?3",
    id,
    auth.userId,
    now(),
  );
  if (row === null) throw err.notFound("session");
  await revokeSessions(rt(c), auth.userId, [row.id], "revoked");
  return c.body(null, 204);
});

/** Password change: verify the current one (401 invalid_credentials), store the new hash, revoke every other session. */
authRoutes.post("/auth/password", requireAuth, vJson(PasswordBody), async (c) => {
  const auth = getAuth(c);
  const body = c.req.valid("json");
  const db = c.env.DB;
  if (body.new_password.toLowerCase() === auth.handle) throw err.invalid({ new_password: "invalid" });
  const user = await first<Pick<UserRow, "password_hash">>(db, "SELECT password_hash FROM users WHERE id = ?1", auth.userId);
  if (user === null) throw err.unauthorized();
  if (!(await verifyPassword(body.current_password, user.password_hash))) throw invalidCredentials();
  const passwordHash = await hashPassword(body.new_password);
  await run(db, "UPDATE users SET password_hash = ?1, updated_at = ?2 WHERE id = ?3", passwordHash, now(), auth.userId);
  await revokeSessions(rt(c), auth.userId, "all", "password_changed", auth.sessionId);
  return c.body(null, 204);
});
