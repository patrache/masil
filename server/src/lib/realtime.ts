// Realtime publishing (server-architecture.md 3.5, 4.11; events.md 6).
// This file so far: audience types + online audience resolution. publishEvent/publishEvents/deliverToUsers follow (Task 11).
import type { Env } from "../env";
import { all, jsonList } from "./db";

export type Audience =
  | { users: string[] }
  | { channel: string }
  | { space: string }
  | { followers: string } // active followers of userId (blocked pairs removed)
  | { presenceWatchers: string } // friends + dm/group_dm co-members of userId
  | { topic: string }; // "post:<id>" -> TopicHub

/** Per-recipient post visibility (events.md 4.8, api.md 3.4). A recipient R passes iff
 *  - no block in either direction between R and every id in `authors`, and
 *  - for every id P in `privateAuthors`: R === P or R is an active follower of P.
 *  Required for timeline.item_created (authors = post author + quoted author + reposter)
 *  and post.reply_created (authors = reply author + parent author). */
export interface VisibilitySpec {
  authors: string[];
  privateAuthors: string[];
}

export interface PublishOptions {
  ephemeral?: boolean; // default false (logged). events.md marks each type
  exclude?: string[]; // user ids to skip
  includeSelf?: string; // add this user even if not in audience (e.g. author for followers audience)
  visibleTo?: VisibilitySpec; // SNS content events: filter recipients (followers audience in SQL, topic audience inside TopicHub)
}

type SqlPart = { sql: string; params: unknown[] };

/** Builds a statement with numbered placeholders; `p(v)` binds v and returns its `?N`. */
function binder() {
  const params: unknown[] = [];
  return {
    params,
    p(v: unknown): string {
      params.push(v);
      return `?${params.length}`;
    },
  };
}

/** Candidate user ids for an audience (one SELECT of `user_id`, no online filter). Deleted channels/spaces yield nothing. */
function candidatesSql(audience: Exclude<Audience, { topic: string }>, p: (v: unknown) => string): string {
  if ("users" in audience) {
    return `SELECT value AS user_id FROM json_each(${p(jsonList(audience.users))})`;
  }
  if ("channel" in audience) {
    const ch = p(audience.channel);
    // public text: members of the (live) space; private text: channel_members ∩ current space_members (live space);
    // dm/group_dm: channel_members. The channel itself must not be deleted.
    return `SELECT sm.user_id FROM channels c
              JOIN spaces s ON s.id = c.space_id AND s.deleted_at IS NULL
              JOIN space_members sm ON sm.space_id = c.space_id
             WHERE c.id = ${ch} AND c.deleted_at IS NULL AND c.kind = 'text' AND c.is_private = 0
            UNION
            SELECT cm.user_id FROM channels c
              JOIN spaces s ON s.id = c.space_id AND s.deleted_at IS NULL
              JOIN channel_members cm ON cm.channel_id = c.id
              JOIN space_members sm ON sm.space_id = c.space_id AND sm.user_id = cm.user_id
             WHERE c.id = ${ch} AND c.deleted_at IS NULL AND c.kind = 'text' AND c.is_private = 1
            UNION
            SELECT cm.user_id FROM channels c
              JOIN channel_members cm ON cm.channel_id = c.id
             WHERE c.id = ${ch} AND c.deleted_at IS NULL AND c.kind IN ('dm', 'group_dm')`;
  }
  if ("space" in audience) {
    return `SELECT sm.user_id FROM space_members sm
              JOIN spaces s ON s.id = sm.space_id AND s.deleted_at IS NULL
             WHERE sm.space_id = ${p(audience.space)}`;
  }
  if ("followers" in audience) {
    const u = p(audience.followers);
    return `SELECT f.follower_id AS user_id FROM follows f
             WHERE f.followee_id = ${u} AND f.status = 'active'
               AND NOT EXISTS (SELECT 1 FROM blocks b
                                WHERE (b.blocker_id = f.follower_id AND b.blocked_id = ${u})
                                   OR (b.blocker_id = ${u} AND b.blocked_id = f.follower_id))`;
  }
  const u = p(audience.presenceWatchers);
  return `SELECT CASE WHEN fr.user_a = ${u} THEN fr.user_b ELSE fr.user_a END AS user_id
            FROM friendships fr WHERE fr.user_a = ${u} OR fr.user_b = ${u}
          UNION
          SELECT cm2.user_id FROM channel_members cm1
            JOIN channels c ON c.id = cm1.channel_id AND c.kind IN ('dm', 'group_dm') AND c.deleted_at IS NULL
            JOIN channel_members cm2 ON cm2.channel_id = cm1.channel_id AND cm2.user_id <> ${u}
           WHERE cm1.user_id = ${u}`;
}

/** events.md 4.8 / server-architecture.md 4.11 visibility filter over recipient column `col`. */
function visibilitySql(col: string, v: VisibilitySpec, p: (v: unknown) => string): string {
  const authors = p(jsonList(v.authors));
  const priv = p(jsonList(v.privateAuthors));
  return `NOT EXISTS (SELECT 1 FROM blocks b, json_each(${authors}) a
                       WHERE (b.blocker_id = ${col} AND b.blocked_id = a.value)
                          OR (b.blocker_id = a.value AND b.blocked_id = ${col}))
      AND NOT EXISTS (SELECT 1 FROM json_each(${priv}) pa
                       WHERE pa.value <> ${col}
                         AND NOT EXISTS (SELECT 1 FROM follows f
                                          WHERE f.follower_id = ${col} AND f.followee_id = pa.value AND f.status = 'active'))`;
}

function buildAudienceQuery(
  audience: Exclude<Audience, { topic: string }>,
  t: number,
  opts: { exclude?: string[]; notifyOk?: boolean; visibleTo?: VisibilitySpec },
): SqlPart {
  const b = binder();
  const cand = candidatesSql(audience, b.p);
  const nowP = b.p(t);
  const where = [`p.user_id IN (${cand})`, opts.notifyOk ? `(p.online_until > ${nowP} OR p.notify_online_until > ${nowP})` : `p.online_until > ${nowP}`];
  if (opts.exclude && opts.exclude.length > 0) where.push(`p.user_id NOT IN (SELECT value FROM json_each(${b.p(jsonList(opts.exclude))}))`);
  if (opts.visibleTo) where.push(visibilitySql("p.user_id", opts.visibleTo, b.p));
  return { sql: `SELECT p.user_id FROM user_presence p WHERE ${where.join(" AND ")}`, params: b.params };
}

/** Online recipients for an audience. notifyOk=false (default): online_until > now (full/TUI only).
 *  notifyOk=true: online_until > now OR notify_online_until > now. Exposed for tests and alert targeting. */
export async function resolveOnlineAudience(
  env: Env,
  audience: Exclude<Audience, { topic: string }>,
  opts: { exclude?: string[]; notifyOk?: boolean; visibleTo?: VisibilitySpec } = {},
): Promise<string[]> {
  if ("users" in audience && audience.users.length === 0) return [];
  const q = buildAudienceQuery(audience, Date.now(), opts);
  const rows = await all<{ user_id: string }>(env.DB, q.sql, ...q.params);
  return rows.map((r) => r.user_id);
}
