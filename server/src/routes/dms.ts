// DMs and group DMs (api.md 5.6). requireAuth is a per-route argument (plan 01 Global Constraints).
// A DM is a Channel (kind dm | group_dm); membership changes live in channels.ts / services/membership.ts.
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth, resolveUserRef } from "../lib/auth";
import { all, batch, decodeCursor, encodeCursor, first, jsonList, parseLimit, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { newId } from "../lib/ids";
import { canDm } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { publishEvent } from "../lib/realtime";
import { serializeChannels } from "../lib/serialize";
import { now } from "../lib/time";
import { vJson, vQuery, zText, zUserRef } from "../lib/validate";
import { GROUP_DM_MAX_MEMBERS, groupDmDenial } from "../services/membership";
import type { ChannelRow } from "../types/rows";

const CreateDmBody = z.object({
  users: z.array(zUserRef).min(1).max(GROUP_DM_MAX_MEMBERS - 1),
  name: zText(0, 50).nullable().optional(),
});
const ListQuery = z.object({ cursor: z.string().optional(), limit: z.string().optional() });

type DmListRow = ChannelRow & { _sort: number };

export const dmRoutes = new Hono<AppEnv>();

/** My dm/group_dm channels, newest activity first (last_message_at, else created_at); 1:1 DMs with someone I blocked are hidden. */
dmRoutes.get("/dms", requireAuth, vQuery(ListQuery), async (c) => {
  const me = getAuth(c).userId;
  const q = c.req.valid("query");
  const n = parseLimit(q.limit, 30, 100);
  const cur = decodeCursor(q.cursor);
  if (cur !== null && typeof cur.k !== "number") throw err.invalid({ cursor: "invalid" });
  const rows = await all<DmListRow>(
    c.env.DB,
    `SELECT * FROM (
       SELECT c.*, COALESCE(c.last_message_at, c.created_at) AS _sort
         FROM channel_members cm
         JOIN channels c ON c.id = cm.channel_id AND c.kind IN ('dm', 'group_dm') AND c.deleted_at IS NULL
        WHERE cm.user_id = ?1
          AND NOT (c.kind = 'dm' AND EXISTS (
                SELECT 1 FROM channel_members o JOIN blocks b ON b.blocker_id = ?1 AND b.blocked_id = o.user_id
                 WHERE o.channel_id = c.id AND o.user_id <> ?1))
     )
      WHERE ?2 IS NULL OR _sort < ?2 OR (_sort = ?2 AND id < ?3)
      ORDER BY _sort DESC, id DESC
      LIMIT ?4`,
    me,
    cur === null ? null : cur.k,
    cur === null ? null : cur.i,
    n + 1,
  );
  const page = rows.slice(0, n);
  const last = page[page.length - 1];
  const next_cursor = rows.length > n && last ? encodeCursor({ k: last._sort, i: last.id }) : null;
  const items = await serializeChannels(
    c.env.DB,
    me,
    page.map(({ _sort, ...row }) => row),
  );
  return c.json({ items, next_cursor });
});

/**
 * One other user -> the 1:1 DM (existing: 200, new: 201; creating needs can_dm, else 403 dm_not_allowed).
 * 2..9 others -> always a new group DM (201) owned by me; every participant must pass can_dm(me, X) and have no block
 * either way with another participant (groupDmDenial; 403 dm_not_allowed — details.user_ids for can_dm failures, no ids
 * for a block pair). channel.created goes to every participant of a new channel.
 */
dmRoutes.post("/dms", requireAuth, limit("RL_SOCIAL"), vJson(CreateDmBody), async (c) => {
  const auth = getAuth(c);
  const me = auth.userId;
  const db = c.env.DB;
  const body = c.req.valid("json");
  const targets = await Promise.all(body.users.map((ref) => resolveUserRef(db, ref, auth)));
  const others = [...new Set(targets.map((u) => u.id))];
  if (others.includes(me)) throw err.invalid({ users: "invalid" });
  const t = now();

  if (others.length === 1) {
    const other = others[0]!;
    const dmKey = me < other ? `${me}:${other}` : `${other}:${me}`;
    const existing = await first<ChannelRow>(db, "SELECT * FROM channels WHERE dm_key = ?1 AND deleted_at IS NULL", dmKey);
    if (existing !== null) {
      const [channel] = await serializeChannels(db, me, [existing]);
      return c.json({ channel }, 200);
    }
    if (!(await canDm(db, me, other))) throw err.forbidden("dm_not_allowed");
    const id = newId(t);
    // dm_key is unique: a concurrent create makes our insert a no-op and both callers end up on the same row.
    await batch(db, [
      stmt(
        db,
        `INSERT INTO channels (id, kind, created_by, dm_key, member_count, created_at, updated_at)
         VALUES (?1, 'dm', ?2, ?3, 2, ?4, ?4) ON CONFLICT DO NOTHING`,
        id,
        me,
        dmKey,
        t,
      ),
      stmt(
        db,
        `INSERT INTO channel_members (channel_id, user_id, added_by, joined_at)
         SELECT ?1, j.value, ?2, ?3 FROM json_each(?4) j WHERE EXISTS (SELECT 1 FROM channels WHERE id = ?1)`,
        id,
        me,
        t,
        jsonList([me, other]),
      ),
    ]);
    const row = await first<ChannelRow>(db, "SELECT * FROM channels WHERE dm_key = ?1", dmKey);
    if (row === null) throw new Error("dm insert vanished");
    const created = row.id === id;
    const [[forEvent], [channel]] = await Promise.all([serializeChannels(db, null, [row]), serializeChannels(db, me, [row])]);
    if (created) await publishEvent(rt(c), { users: [me, other] }, "channel.created", { channel: forEvent! });
    return c.json({ channel }, created ? 201 : 200);
  }

  const denial = await groupDmDenial(db, me, others, [me]);
  if (denial !== null) throw denial;
  const id = newId(t);
  const members = [me, ...others];
  const name = body.name === undefined || body.name === null || body.name === "" ? null : body.name;
  await batch(db, [
    stmt(
      db,
      `INSERT INTO channels (id, kind, name, owner_id, created_by, member_count, created_at, updated_at)
       VALUES (?1, 'group_dm', ?2, ?3, ?3, ?4, ?5, ?5)`,
      id,
      name,
      me,
      members.length,
      t,
    ),
    stmt(
      db,
      `INSERT INTO channel_members (channel_id, user_id, added_by, joined_at)
       SELECT ?1, j.value, ?2, ?3 FROM json_each(?4) j`,
      id,
      me,
      t,
      jsonList(members),
    ),
  ]);
  const row = await first<ChannelRow>(db, "SELECT * FROM channels WHERE id = ?1", id);
  if (row === null) throw new Error("group dm insert vanished");
  const [[forEvent], [channel]] = await Promise.all([serializeChannels(db, null, [row]), serializeChannels(db, me, [row])]);
  await publishEvent(rt(c), { users: members }, "channel.created", { channel: forEvent! });
  return c.json({ channel }, 201);
});
