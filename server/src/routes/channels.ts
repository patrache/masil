// Channels (api.md 5.9) and channel membership (api.md 5.6). requireAuth is a per-route argument (plan 01 Global
// Constraints). Task 19: GET /channels/:channel_id and the group_dm member routes; Task 23: PATCH/DELETE
// /channels/:channel_id, GET /channels/:channel_id/members, PUT /channels/:channel_id/prefs and the private text member
// branch (services/membership.ts); POST /spaces/:space_id/channels lives in routes/spaces.ts. Task 27 adds
// PUT /channels/:channel_id/read. Task 26: GET /channels/:channel_id/pins.
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth, resolveUserRef } from "../lib/auth";
import { all, batch, decodeCursor, encodeCursor, first, parseLimit, stmt } from "../lib/db";
import { err, type FieldError } from "../lib/errors";
import { idParam } from "../lib/params";
import { requireChannelRead } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { publishEvent } from "../lib/realtime";
import { loadUnreadStates, loadUserSummaries, serializeChannels, serializeMessages } from "../lib/serialize";
import { charCount, normalizeText } from "../lib/text";
import { iso, now } from "../lib/time";
import { CHANNEL_NAME_RE, vJson, vQuery, zId, zText, zUserRef } from "../lib/validate";
import { addChannelMembers, GROUP_DM_MAX_MEMBERS, removeChannelMember } from "../services/membership";
import { postSystemMessage } from "../services/messages";
import type { SystemEvent } from "../types/api";
import type { ChannelRow, MessageRow } from "../types/rows";

/** api.md 0.3: group_dm name 0..50 ("" / null clears it). */
const GROUP_DM_NAME_MAX = 50;
/** Upper bound for a text channel position (any order the client wants fits). */
const CHANNEL_POSITION_MAX = 1_000_000;

const AddMembersBody = z.object({ users: z.array(zUserRef).min(1).max(GROUP_DM_MAX_MEMBERS - 1) });

// name is validated per channel kind in the handler (text: CHANNEL_NAME_RE; group_dm: 0..50 free text or null).
const PatchBody = z.object({
  name: z.string().nullable().optional(),
  topic: zText(0, 250).optional(),
  position: z.number().int().min(0).max(CHANNEL_POSITION_MAX).optional(),
});

const MembersQuery = z.object({ cursor: z.string().optional(), limit: z.string().optional() });

const ReadBody = z.object({ message_id: zId });

const PrefsBody = z.object({
  notify: z.enum(["all", "mentions", "none"]).optional(),
  muted: z.boolean().optional(),
});

function isUniqueViolation(e: unknown): boolean {
  return e instanceof Error && /UNIQUE constraint failed/i.test(e.message);
}

async function loadChannelRow(db: D1Database, channelId: string): Promise<ChannelRow> {
  const row = await first<ChannelRow>(db, "SELECT * FROM channels WHERE id = ?1 AND deleted_at IS NULL", channelId);
  if (row === null) throw err.notFound("channel");
  return row;
}

export const channelRoutes = new Hono<AppEnv>();

channelRoutes.get("/channels/:channel_id", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const access = await requireChannelRead(c.env.DB, idParam(c, "channel_id", "channel"), me);
  const [channel] = await serializeChannels(c.env.DB, me, [access.channel]);
  return c.json({ channel });
});

channelRoutes.post("/channels/:channel_id/members", requireAuth, limit("RL_SOCIAL"), vJson(AddMembersBody), async (c) => {
  const auth = getAuth(c);
  const db = c.env.DB;
  // 404 for a channel I cannot read comes before resolving the users (no existence leak through the body).
  await requireChannelRead(db, idParam(c, "channel_id", "channel"), auth.userId);
  const users = await Promise.all(c.req.valid("json").users.map((ref) => resolveUserRef(db, ref, auth)));
  const channel = await addChannelMembers(rt(c), idParam(c, "channel_id", "channel"), auth.userId, users.map((u) => u.id));
  return c.json({ channel });
});

channelRoutes.delete("/channels/:channel_id/members/:user", requireAuth, async (c) => {
  const auth = getAuth(c);
  const db = c.env.DB;
  await requireChannelRead(db, idParam(c, "channel_id", "channel"), auth.userId);
  const target = await resolveUserRef(db, c.req.param("user"), auth);
  await removeChannelMember(rt(c), idParam(c, "channel_id", "channel"), auth.userId, target.id);
  return c.body(null, 204);
});

/**
 * text: the space owner·admin, { name?, topic?, position? } (public/private cannot change); group_dm: any member,
 * { name?, topic? } (position -> 400 not_allowed); 1:1 dm -> 409 conflict not_supported. A text name already used by a
 * live channel of the space -> 409 already_exists (checked before, and uq_channels_space_name for races).
 * When something changed: channel.updated{channel} to the channel audience, then one system message per changed field:
 * text name -> channel_renamed{name}, group_dm name -> group_renamed{name} ({} when cleared), topic ->
 * channel_topic_changed{topic}. position changes post no system message.
 */
channelRoutes.patch("/channels/:channel_id", requireAuth, vJson(PatchBody), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const access = await requireChannelRead(db, idParam(c, "channel_id", "channel"), me);
  const ch = access.channel;
  if (ch.kind === "dm") throw err.conflict("conflict", "not_supported");
  if (ch.kind === "text" && access.spaceRole !== "owner" && access.spaceRole !== "admin") throw err.forbidden("forbidden", "requires admin role");
  const b = c.req.valid("json");

  const fields: Record<string, FieldError> = {};
  let name: string | null | undefined;
  if (b.name !== undefined) {
    if (ch.kind === "text") {
      const n = b.name === null ? "" : normalizeText(b.name);
      if (!CHANNEL_NAME_RE.test(n)) fields.name = "invalid";
      else name = n;
    } else {
      const n = b.name === null ? "" : normalizeText(b.name);
      if (charCount(n) > GROUP_DM_NAME_MAX) fields.name = "too_long";
      else name = n === "" ? null : n;
    }
  }
  if (b.position !== undefined && ch.kind !== "text") fields.position = "not_allowed";
  if (Object.keys(fields).length > 0) throw err.invalid(fields);

  const nameChanged = name !== undefined && name !== ch.name;
  const topicChanged = b.topic !== undefined && b.topic !== ch.topic;
  const positionChanged = b.position !== undefined && b.position !== ch.position;
  if (!nameChanged && !topicChanged && !positionChanged) {
    const [channel] = await serializeChannels(db, me, [ch]);
    return c.json({ channel });
  }
  if (nameChanged && ch.kind === "text") {
    const taken = await first(
      db,
      "SELECT 1 AS x FROM channels WHERE space_id = ?1 AND kind = 'text' AND deleted_at IS NULL AND name = ?2 AND id <> ?3",
      ch.space_id,
      name,
      ch.id,
    );
    if (taken !== null) throw err.conflict("already_exists");
  }

  const sets: string[] = [];
  const params: unknown[] = [ch.id];
  const set = (col: string, v: unknown) => {
    params.push(v);
    sets.push(`${col} = ?${params.length}`);
  };
  if (nameChanged) set("name", name);
  if (topicChanged) set("topic", b.topic);
  if (positionChanged) set("position", b.position);
  set("updated_at", now());
  let row: ChannelRow | null;
  try {
    row = await first<ChannelRow>(db, `UPDATE channels SET ${sets.join(", ")} WHERE id = ?1 AND deleted_at IS NULL RETURNING *`, ...params);
  } catch (e) {
    if (isUniqueViolation(e)) throw err.conflict("already_exists");
    throw e;
  }
  if (row === null) throw err.notFound("channel");

  const [forEvent] = await serializeChannels(db, null, [row]);
  const runtime = rt(c);
  await publishEvent(runtime, { channel: ch.id }, "channel.updated", { channel: forEvent! });
  const systems: SystemEvent[] = [];
  if (nameChanged) {
    systems.push(
      ch.kind === "text"
        ? { event: "channel_renamed", actor_id: me, target_user_id: null, data: { name: name! } }
        : { event: "group_renamed", actor_id: me, target_user_id: null, data: name === null ? {} : { name: name! } },
    );
  }
  if (topicChanged) systems.push({ event: "channel_topic_changed", actor_id: me, target_user_id: null, data: { topic: b.topic! } });
  for (const system of systems) await postSystemMessage(runtime, ch.id, me, system);
  const [channel] = await serializeChannels(db, me, [await loadChannelRow(db, ch.id)]);
  return c.json({ channel });
});

/**
 * text only (dm/group_dm -> 409 conflict not_supported), the space owner·admin, never the default channel (409
 * conflict default_channel). Soft delete. The {channel} audience drops deleted channels, so the audience user ids
 * (public: space_members; private: channel_members ∩ space_members) are read before the batch and
 * channel.deleted{channel_id, space_id, reason:"deleted"} goes to {users} after the commit (plan 01 Global Constraints).
 */
channelRoutes.delete("/channels/:channel_id", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const access = await requireChannelRead(db, idParam(c, "channel_id", "channel"), me);
  const ch = access.channel;
  if (ch.kind !== "text") throw err.conflict("conflict", "not_supported");
  if (access.spaceRole !== "owner" && access.spaceRole !== "admin") throw err.forbidden("forbidden", "requires admin role");
  const space = await first<{ default_channel_id: string | null }>(db, "SELECT default_channel_id FROM spaces WHERE id = ?1", ch.space_id);
  if (space?.default_channel_id === ch.id) throw err.conflict("conflict", "default_channel");

  const audience = (
    ch.is_private === 1
      ? await all<{ user_id: string }>(
          db,
          `SELECT cm.user_id FROM channel_members cm
             JOIN space_members sm ON sm.space_id = ?2 AND sm.user_id = cm.user_id
            WHERE cm.channel_id = ?1`,
          ch.id,
          ch.space_id,
        )
      : await all<{ user_id: string }>(db, "SELECT user_id FROM space_members WHERE space_id = ?1", ch.space_id)
  ).map((r) => r.user_id);
  const t = now();
  const [res] = await batch(db, [
    stmt(
      db,
      `UPDATE channels SET deleted_at = ?2, updated_at = ?2
        WHERE id = ?1 AND kind = 'text' AND deleted_at IS NULL
          AND NOT EXISTS (SELECT 1 FROM spaces s WHERE s.id = channels.space_id AND s.default_channel_id = channels.id)`,
      ch.id,
      t,
    ),
  ]);
  if ((res!.meta.changes ?? 0) === 0) throw err.notFound("channel");
  await publishEvent(rt(c), { users: audience }, "channel.deleted", { channel_id: ch.id, space_id: ch.space_id, reason: "deleted" });
  return c.body(null, 204);
});

/**
 * Read access. Public text: the space members; private text: channel_members ∩ space_members; dm/group_dm:
 * channel_members. Join order (joined_at, then user id), cursor {k: joined_at, i: user id}.
 */
channelRoutes.get("/channels/:channel_id/members", requireAuth, vQuery(MembersQuery), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const ch = (await requireChannelRead(db, idParam(c, "channel_id", "channel"), me)).channel;
  const qy = c.req.valid("query");
  const n = parseLimit(qy.limit);
  const cursor = decodeCursor(qy.cursor);
  if (cursor !== null && typeof cursor.k !== "number") throw err.invalid({ cursor: "invalid" });

  // ?1 = space id (public text) or channel id, ?2 = space id (private text), cursor binds after them
  const params: unknown[] = [];
  let source: string;
  if (ch.kind === "text" && ch.is_private === 0) {
    params.push(ch.space_id);
    source = "SELECT user_id, joined_at FROM space_members WHERE space_id = ?1";
  } else if (ch.kind === "text") {
    params.push(ch.id, ch.space_id);
    source = `SELECT cm.user_id, cm.joined_at FROM channel_members cm
                JOIN space_members sm ON sm.space_id = ?2 AND sm.user_id = cm.user_id
               WHERE cm.channel_id = ?1`;
  } else {
    params.push(ch.id);
    source = "SELECT user_id, joined_at FROM channel_members WHERE channel_id = ?1";
  }
  let after = "";
  if (cursor !== null) {
    params.push(cursor.k, cursor.i);
    const k = `?${params.length - 1}`;
    const i = `?${params.length}`;
    after = `WHERE joined_at > ${k} OR (joined_at = ${k} AND user_id > ${i})`;
  }
  const rows = await all<{ user_id: string; joined_at: number }>(
    db,
    `SELECT user_id, joined_at FROM (${source}) ${after} ORDER BY joined_at, user_id LIMIT ${n + 1}`,
    ...params,
  );
  const page = rows.slice(0, n);
  const last = page.at(-1);
  const next = rows.length > n && last ? encodeCursor({ k: last.joined_at, i: last.user_id }) : null;
  const users = await loadUserSummaries(
    db,
    page.map((r) => r.user_id),
  );
  const items = page.flatMap((r) => {
    const user = users.get(r.user_id);
    return user ? [{ user, joined_at: iso(r.joined_at) }] : [];
  });
  return c.json({ items, next_cursor: next });
});

/**
 * My notify / muted for a channel I can read (data-model.md 3.4 channel_prefs; a missing field keeps its current value,
 * a missing row starts from the kind default). channel.prefs_updated{channel_id, my} to my connections.
 */
channelRoutes.put("/channels/:channel_id/prefs", requireAuth, vJson(PrefsBody), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const ch = (await requireChannelRead(db, idParam(c, "channel_id", "channel"), me)).channel;
  const b = c.req.valid("json");
  if (b.notify !== undefined || b.muted !== undefined) {
    const defaultNotify = ch.kind === "text" ? "mentions" : "all";
    const muted = b.muted === undefined ? null : b.muted ? 1 : 0;
    await stmt(
      db,
      `INSERT INTO channel_prefs (user_id, channel_id, notify, muted, updated_at)
       VALUES (?1, ?2, COALESCE(?3, ?4), COALESCE(?5, 0), ?6)
       ON CONFLICT (user_id, channel_id) DO UPDATE SET
         notify = COALESCE(?3, channel_prefs.notify), muted = COALESCE(?5, channel_prefs.muted), updated_at = ?6`,
      me,
      ch.id,
      b.notify ?? null,
      defaultNotify,
      muted,
      now(),
    ).run();
  }
  const [channel] = await serializeChannels(db, me, [ch]);
  const my = channel!.my;
  await publishEvent(rt(c), { users: [me] }, "channel.prefs_updated", { channel_id: ch.id, my });
  return c.json({ my });
});

/** Pinned live messages of a channel I can read, most recently pinned first (api.md 5.12; at most 50, no paging). */
channelRoutes.get("/channels/:channel_id/pins", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const ch = (await requireChannelRead(db, idParam(c, "channel_id", "channel"), me)).channel;
  const rows = await all<MessageRow>(
    db,
    `SELECT m.* FROM message_pins p JOIN messages m ON m.id = p.message_id AND m.deleted_at IS NULL
      WHERE p.channel_id = ?1
      ORDER BY p.pinned_at DESC, p.rowid DESC`,
    ch.id,
  );
  const items = await serializeMessages(db, me, rows);
  return c.json({ items });
});

/**
 * Mark read up to `message_id` (api.md 5.9): it must be a top-level message of this channel (thread replies and other
 * channels' ids -> 400 fields.message_id "invalid"). The pointer only moves forward: a late/older request leaves it
 * as it is (conditional upsert). 200 {state} + read_state.updated{state} to my connections.
 */
channelRoutes.put("/channels/:channel_id/read", requireAuth, vJson(ReadBody), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const ch = (await requireChannelRead(db, idParam(c, "channel_id", "channel"), me)).channel;
  const messageId = c.req.valid("json").message_id;
  const target = await first(db, "SELECT 1 AS x FROM messages WHERE id = ?1 AND channel_id = ?2 AND thread_root_id IS NULL", messageId, ch.id);
  if (target === null) throw err.invalid({ message_id: "invalid" });
  await stmt(
    db,
    `INSERT INTO channel_reads (user_id, channel_id, last_read_message_id, updated_at) VALUES (?1, ?2, ?3, ?4)
     ON CONFLICT (user_id, channel_id) DO UPDATE SET last_read_message_id = excluded.last_read_message_id, updated_at = excluded.updated_at
      WHERE excluded.last_read_message_id > channel_reads.last_read_message_id`,
    me,
    ch.id,
    messageId,
    now(),
  ).run();
  const state = (await loadUnreadStates(db, me, [ch.id])).get(ch.id);
  if (state === undefined) throw err.notFound("channel");
  await publishEvent(rt(c), { users: [me] }, "read_state.updated", { state });
  return c.json({ state });
});
