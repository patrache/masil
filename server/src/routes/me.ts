// Me, profile, settings and status message (api.md 5.1): GET /me, PATCH /me/profile, GET|PATCH /me/settings,
// PUT|DELETE /me/status, GET /me/unreads (Task 27). /me is shared with presence.ts and plan 02 follows.ts, so requireAuth is attached per
// route (plan 01 Global Constraints). Writes commit in one db.batch, then publish user.updated / settings.updated.
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppContext, type AppEnv } from "../env";
import { getAuth, requireAuth } from "../lib/auth";
import { batch, first, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { isId } from "../lib/ids";
import { accessibleChannelIds } from "../lib/permissions";
import { publishEvent } from "../lib/realtime";
import { loadUnreadStates, serializeMe, serializeMeLive, toUserSummary } from "../lib/serialize";
import { isSingleEmoji, normalizeText } from "../lib/text";
import { now } from "../lib/time";
import { removeUploads } from "../lib/uploads";
import { vJson, zText } from "../lib/validate";
import { approveAllPendingFollows } from "../services/relations";
import type { UserRow } from "../types/rows";

const STATUS_EMOJI_MAX_BYTES = 16; // data-model.md users.status_emoji
const STATUS_EXPIRES_MIN_SEC = 60;
const STATUS_EXPIRES_MAX_SEC = 2_592_000; // 30 days

const ProfileBody = z.object({
  display_name: zText(1, 32).optional(),
  bio: zText(0, 160).optional(),
  avatar_upload_id: z.string().nullable().optional(), // ownership/purpose/status checked in the handler
});

const zBool = z.boolean().optional();

const SettingsBody = z.object({
  dm_policy: z.enum(["friends_and_spaces", "friends_only"]).optional(),
  is_private: zBool,
  notify: z
    .object({ dm: zBool, mention: zBool, invite: zBool, friend_request: zBool, thread_reply: zBool, social: zBool })
    .optional(),
  focus_default_min: z.int().min(1).max(720).optional(),
});

const StatusBody = z.object({
  emoji: z
    .string()
    .transform((s) => normalizeText(s))
    .refine((s) => isSingleEmoji(s, STATUS_EMOJI_MAX_BYTES), { message: "one emoji" })
    .nullable()
    .optional(),
  text: zText(0, 64),
  expires_in_sec: z.int().min(STATUS_EXPIRES_MIN_SEC).max(STATUS_EXPIRES_MAX_SEC).nullable().optional(),
});

/** settings.notify key -> user_settings column. */
const NOTIFY_COLUMNS = {
  dm: "notify_dm",
  mention: "notify_mention",
  invite: "notify_invite",
  friend_request: "notify_friend_request",
  thread_reply: "notify_thread_reply",
  social: "notify_social",
} as const;

export const meRoutes = new Hono<AppEnv>();

async function loadMyRow(db: D1Database, userId: string): Promise<UserRow> {
  const row = await first<UserRow>(db, "SELECT * FROM users WHERE id = ?1", userId);
  if (row === null) throw err.unauthorized(); // session outlived its user: fail closed
  return row;
}

/** user.updated{user} to friends + dm/group_dm co-members + my own connections (events.md 4.5). */
function publishUserUpdated(c: AppContext, row: UserRow): Promise<void> {
  return publishEvent(rt(c), { presenceWatchers: row.id }, "user.updated", { user: toUserSummary(row) }, { includeSelf: row.id });
}

/** "UPDATE <table> SET a = ?1, b = ?2 ... WHERE <key> = ?N"; column names come from this module only, values are bound. */
function updateStmt(db: D1Database, table: "users" | "user_settings", key: "id" | "user_id", id: string, cols: Record<string, unknown>): D1PreparedStatement {
  const names = Object.keys(cols);
  const sets = names.map((n, i) => `${n} = ?${i + 1}`).join(", ");
  return stmt(db, `UPDATE ${table} SET ${sets} WHERE ${key} = ?${names.length + 1}`, ...names.map((n) => cols[n]), id);
}

meRoutes.get("/me", requireAuth, async (c) => {
  return c.json({ user: await serializeMeLive(c.env, getAuth(c).userId) });
});

meRoutes.patch("/me/profile", requireAuth, vJson(ProfileBody), async (c) => {
  const me = getAuth(c).userId;
  const body = c.req.valid("json");
  const db = c.env.DB;
  const cur = await loadMyRow(db, me);

  const binds: unknown[] = [];
  const p = (v: unknown): string => {
    binds.push(v);
    return `?${binds.length}`;
  };
  const sets: string[] = [];
  const stmts: D1PreparedStatement[] = [];
  if (body.display_name !== undefined) sets.push(`display_name = ${p(body.display_name)}`);
  if (body.bio !== undefined) sets.push(`bio = ${p(body.bio)}`);
  let attachId: string | null = null;
  if (body.avatar_upload_id === null) {
    sets.push("avatar_upload_id = NULL");
  } else if (body.avatar_upload_id !== undefined && body.avatar_upload_id !== cur.avatar_upload_id) {
    const id = body.avatar_upload_id;
    const ok =
      isId(id) &&
      (await first(db, "SELECT 1 AS x FROM uploads WHERE id = ?1 AND owner_id = ?2 AND purpose = 'avatar' AND status = 'pending'", id, me)) !== null;
    if (!ok) throw err.invalid({ avatar_upload_id: "invalid" });
    attachId = id;
    const tag = `avatar:${me}`;
    stmts.push(stmt(db, "UPDATE uploads SET status = 'attached', attached_to = ?2 WHERE id = ?1 AND status = 'pending'", id, tag));
    // Points at the upload only if this batch really attached it (the pending row may have been cleaned up since the check).
    sets.push(
      `avatar_upload_id = COALESCE((SELECT u.id FROM uploads u WHERE u.id = ${p(id)} AND u.status = 'attached' AND u.attached_to = ${p(tag)}), avatar_upload_id)`,
    );
  }
  if (sets.length === 0) return c.json({ user: await serializeMeLive(c.env, me) });
  sets.push(`updated_at = ${p(now())}`);
  stmts.push(stmt(db, `UPDATE users SET ${sets.join(", ")} WHERE id = ${p(me)} RETURNING *`, ...binds));
  const results = await batch(db, stmts);
  const row = (results[results.length - 1].results[0] as UserRow | undefined) ?? (await loadMyRow(db, me));

  // The previous avatar is dropped only once the user row really moved off it.
  if (cur.avatar_upload_id !== null && row.avatar_upload_id !== cur.avatar_upload_id) await removeUploads(c.env, [cur.avatar_upload_id]);
  await publishUserUpdated(c, row);
  if (attachId !== null && row.avatar_upload_id !== attachId) throw err.invalid({ avatar_upload_id: "invalid" });
  return c.json({ user: await serializeMeLive(c.env, me) });
});

meRoutes.get("/me/settings", requireAuth, async (c) => {
  return c.json({ settings: (await serializeMe(c.env.DB, getAuth(c).userId)).settings });
});

meRoutes.patch("/me/settings", requireAuth, vJson(SettingsBody), async (c) => {
  const me = getAuth(c).userId;
  const body = c.req.valid("json");
  const db = c.env.DB;
  const cur = await loadMyRow(db, me);
  const t = now();

  const settingCols: Record<string, unknown> = {};
  if (body.dm_policy !== undefined) settingCols.dm_policy = body.dm_policy;
  if (body.focus_default_min !== undefined) settingCols.focus_default_min = body.focus_default_min;
  for (const [k, col] of Object.entries(NOTIFY_COLUMNS) as Array<[keyof typeof NOTIFY_COLUMNS, string]>) {
    const v = body.notify?.[k];
    if (v !== undefined) settingCols[col] = v ? 1 : 0;
  }
  const privacyChanged = body.is_private !== undefined && (body.is_private ? 1 : 0) !== cur.is_private;
  if (Object.keys(settingCols).length === 0 && !privacyChanged) return c.json({ settings: (await serializeMe(db, me)).settings });

  const stmts: D1PreparedStatement[] = [];
  if (Object.keys(settingCols).length > 0) {
    // signup always creates the row; the upsert only guards against a missing one
    stmts.push(stmt(db, "INSERT INTO user_settings (user_id, updated_at) VALUES (?1, ?2) ON CONFLICT (user_id) DO NOTHING", me, t));
    stmts.push(updateStmt(db, "user_settings", "user_id", me, { ...settingCols, updated_at: t }));
  }
  if (privacyChanged) stmts.push(updateStmt(db, "users", "id", me, { is_private: body.is_private ? 1 : 0, updated_at: t }));
  await batch(db, stmts);

  const meNow = await serializeMe(db, me);
  await publishEvent(rt(c), { users: [me] }, "settings.updated", { settings: meNow.settings });
  if (privacyChanged) await publishUserUpdated(c, await loadMyRow(db, me));
  // is_private true -> false also approves every pending follow request (api.md 3.3), after the settings commit.
  if (privacyChanged && body.is_private === false) await approveAllPendingFollows(rt(c), me);
  return c.json({ settings: meNow.settings });
});

meRoutes.put("/me/status", requireAuth, vJson(StatusBody), async (c) => {
  const me = getAuth(c).userId;
  const body = c.req.valid("json");
  const t = now();
  const expiresAt = body.expires_in_sec == null ? null : t + body.expires_in_sec * 1000;
  const row = await first<UserRow>(
    c.env.DB,
    `UPDATE users SET status_emoji = ?1, status_text = ?2, status_expires_at = ?3, updated_at = ?4 WHERE id = ?5 RETURNING *`,
    body.emoji ?? null,
    body.text,
    expiresAt,
    t,
    me,
  );
  if (row === null) throw err.unauthorized();
  await publishUserUpdated(c, row);
  const status = toUserSummary(row, t).status; // just set with a future (or no) expiry -> never null
  return c.json({ status: status ?? { emoji: row.status_emoji, text: row.status_text ?? "", expires_at: null } });
});

meRoutes.delete("/me/status", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const row = await first<UserRow>(
    c.env.DB,
    "UPDATE users SET status_emoji = NULL, status_text = NULL, status_expires_at = NULL, updated_at = ?1 WHERE id = ?2 RETURNING *",
    now(),
    me,
  );
  if (row === null) throw err.unauthorized();
  await publishUserUpdated(c, row);
  return c.body(null, 204);
});

/**
 * Unread state of every channel I can read that has something unread or a mention (api.md 5.1), in channel id order.
 * loadUnreadStates applies the cost rule (only channels whose last_message_id is past the pointer are counted).
 */
meRoutes.get("/me/unreads", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const ids = await accessibleChannelIds(db, me);
  const states = await loadUnreadStates(db, me, ids);
  const items = ids.flatMap((id) => {
    const st = states.get(id);
    return st !== undefined && (st.unread_count > 0 || st.mention_count > 0) ? [st] : [];
  });
  return c.json({ items });
});
