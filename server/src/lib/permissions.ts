// Permission rules (api.md 3장, server-architecture.md 4.8).
// Every variable-length ID list goes through `jsonList` (one bound param, `json_each`).
import { isSuspendedNow } from "./auth";
import { all, first, jsonList } from "./db";
import { err } from "./errors";
import { now } from "./time";
import type { ChannelRow, PostRow } from "../types/rows";

export type SpaceRole = "owner" | "admin" | "member";

const ROLE_RANK: Record<SpaceRole, number> = { member: 1, admin: 2, owner: 3 };

function friendPair(a: string, b: string): [string, string] {
  return a < b ? [a, b] : [b, a];
}

// ---------------------------------------------------------------------------
// relations

export async function areFriends(db: D1Database, a: string, b: string): Promise<boolean> {
  if (a === b) return false;
  const [lo, hi] = friendPair(a, b);
  return (await first(db, "SELECT 1 AS x FROM friendships WHERE user_a = ?1 AND user_b = ?2", lo, hi)) !== null;
}

export async function isBlockedEither(db: D1Database, a: string, b: string): Promise<boolean> {
  const row = await first(
    db,
    `SELECT 1 AS x FROM blocks
      WHERE (blocker_id = ?1 AND blocked_id = ?2) OR (blocker_id = ?2 AND blocked_id = ?1)
      LIMIT 1`,
    a,
    b,
  );
  return row !== null;
}

export async function hasBlocked(db: D1Database, blocker: string, blocked: string): Promise<boolean> {
  return (await first(db, "SELECT 1 AS x FROM blocks WHERE blocker_id = ?1 AND blocked_id = ?2", blocker, blocked)) !== null;
}

/** Both are members of at least one non-deleted space. */
export async function shareSpace(db: D1Database, a: string, b: string): Promise<boolean> {
  if (a === b) return false;
  const row = await first(
    db,
    `SELECT 1 AS x
       FROM space_members m1
       JOIN space_members m2 ON m2.space_id = m1.space_id AND m2.user_id = ?2
       JOIN spaces s ON s.id = m1.space_id AND s.deleted_at IS NULL
      WHERE m1.user_id = ?1
      LIMIT 1`,
    a,
    b,
  );
  return row !== null;
}

// ---------------------------------------------------------------------------
// DM eligibility (api.md 3.1)

interface DmCandidateRow {
  id: string;
  found: number;
  suspended_at: number | null;
  suspended_until: number | null;
  blocked: number;
  friends: number;
  shared: number;
  policy: string | null;
}

/**
 * can_dm(a, x) for every x: x ≠ a, x exists and is not suspended, no block either way, and
 * (friends OR (share a live space AND both dm_policy = friends_and_spaces)).
 * Output keeps input order (duplicates collapsed).
 */
export async function canDmMany(db: D1Database, a: string, others: string[]): Promise<{ ok: string[]; denied: string[] }> {
  const ids = [...new Set(others)];
  if (ids.length === 0) return { ok: [], denied: [] };
  const rows = await all<DmCandidateRow>(
    db,
    `SELECT j.value AS id,
            (u.id IS NOT NULL) AS found,
            u.suspended_at, u.suspended_until,
            EXISTS (SELECT 1 FROM blocks bl
                     WHERE (bl.blocker_id = ?2 AND bl.blocked_id = j.value)
                        OR (bl.blocker_id = j.value AND bl.blocked_id = ?2)) AS blocked,
            EXISTS (SELECT 1 FROM friendships f
                     WHERE f.user_a = MIN(?2, j.value) AND f.user_b = MAX(?2, j.value)) AS friends,
            EXISTS (SELECT 1 FROM space_members m1
                      JOIN space_members m2 ON m2.space_id = m1.space_id AND m2.user_id = j.value
                      JOIN spaces s ON s.id = m1.space_id AND s.deleted_at IS NULL
                     WHERE m1.user_id = ?2) AS shared,
            COALESCE((SELECT st.dm_policy FROM user_settings st WHERE st.user_id = j.value), 'friends_and_spaces') AS policy
       FROM json_each(?1) j
       LEFT JOIN users u ON u.id = j.value`,
    jsonList(ids),
    a,
  );
  const mine = await first<{ dm_policy: string }>(db, "SELECT dm_policy FROM user_settings WHERE user_id = ?1", a);
  const myPolicyOpen = (mine?.dm_policy ?? "friends_and_spaces") === "friends_and_spaces";
  const t = now();
  const allowed = new Set<string>();
  for (const r of rows) {
    if (r.id === a || !r.found || r.blocked) continue;
    if (isSuspendedNow(r, t)) continue;
    const viaSpace = r.shared && myPolicyOpen && r.policy === "friends_and_spaces";
    if (r.friends || viaSpace) allowed.add(r.id);
  }
  const ok: string[] = [];
  const denied: string[] = [];
  for (const id of ids) (allowed.has(id) ? ok : denied).push(id);
  return { ok, denied };
}

export async function canDm(db: D1Database, a: string, b: string): Promise<boolean> {
  if (a === b) return false;
  return (await canDmMany(db, a, [b])).ok.length === 1;
}

// ---------------------------------------------------------------------------
// spaces (api.md 3.5)

/** Role in a non-deleted space; null if not a member or the space is deleted. */
export async function getSpaceRole(db: D1Database, spaceId: string, userId: string): Promise<SpaceRole | null> {
  const row = await first<{ role: SpaceRole }>(
    db,
    `SELECT m.role FROM space_members m
       JOIN spaces s ON s.id = m.space_id AND s.deleted_at IS NULL
      WHERE m.space_id = ?1 AND m.user_id = ?2`,
    spaceId,
    userId,
  );
  return row?.role ?? null;
}

/** 404 if not a member (or space deleted); 403 forbidden if below `min`. Returns actual role. */
export async function requireSpaceRole(db: D1Database, spaceId: string, userId: string, min: SpaceRole): Promise<SpaceRole> {
  const role = await getSpaceRole(db, spaceId, userId);
  if (role === null) throw err.notFound("space");
  if (ROLE_RANK[role] < ROLE_RANK[min]) throw err.forbidden("forbidden", `requires ${min} role`);
  return role;
}

// ---------------------------------------------------------------------------
// channels (api.md 3.5 / 3.7)

export interface ChannelAccess {
  channel: ChannelRow;
  spaceRole: SpaceRole | null;
  canRead: boolean;
  canWrite: boolean;
  readOnlyReason: "not_eligible" | "blocked" | null;
  otherUserId: string | null; // 1:1 dm partner
}

type ChannelAccessRow = ChannelRow & {
  _space_role: SpaceRole | null;
  _is_member: number;
  _other: string | null;
};

/**
 * null when the channel is missing/deleted or the user cannot read it.
 * text (public AND private): space not deleted + user is a current space member (+ channel_members if private).
 * dm / group_dm: channel_members.
 */
export async function getChannelAccess(db: D1Database, channelId: string, userId: string): Promise<ChannelAccess | null> {
  const row = await first<ChannelAccessRow>(
    db,
    `SELECT c.*,
            (SELECT m.role FROM space_members m
               JOIN spaces s ON s.id = m.space_id AND s.deleted_at IS NULL
              WHERE c.kind = 'text' AND m.space_id = c.space_id AND m.user_id = ?2) AS _space_role,
            EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = c.id AND cm.user_id = ?2) AS _is_member,
            (SELECT cm.user_id FROM channel_members cm
              WHERE c.kind = 'dm' AND cm.channel_id = c.id AND cm.user_id <> ?2 LIMIT 1) AS _other
       FROM channels c
      WHERE c.id = ?1 AND c.deleted_at IS NULL`,
    channelId,
    userId,
  );
  if (row === null) return null;
  const { _space_role: spaceRole, _is_member: isMember, _other: other, ...channel } = row;

  if (channel.kind === "text") {
    if (spaceRole === null) return null;
    if (channel.is_private === 1 && !isMember) return null;
    return { channel, spaceRole, canRead: true, canWrite: true, readOnlyReason: null, otherUserId: null };
  }
  if (!isMember) return null;
  if (channel.kind === "group_dm") {
    return { channel, spaceRole: null, canRead: true, canWrite: true, readOnlyReason: null, otherUserId: null };
  }
  // 1:1 dm: writing requires can_dm each time; otherwise read-only.
  const canWrite = other !== null && (await canDm(db, userId, other));
  let readOnlyReason: ChannelAccess["readOnlyReason"] = null;
  if (!canWrite) readOnlyReason = other !== null && (await hasBlocked(db, userId, other)) ? "blocked" : "not_eligible";
  return { channel, spaceRole: null, canRead: true, canWrite, readOnlyReason, otherUserId: other };
}

export async function requireChannelRead(db: D1Database, channelId: string, userId: string): Promise<ChannelAccess> {
  const access = await getChannelAccess(db, channelId, userId);
  if (access === null || !access.canRead) throw err.notFound("channel");
  return access;
}

export async function requireChannelWrite(db: D1Database, channelId: string, userId: string): Promise<ChannelAccess> {
  const access = await requireChannelRead(db, channelId, userId);
  if (!access.canWrite) {
    throw err.forbidden("read_only", "this conversation is read-only", { reason: access.readOnlyReason ?? "not_eligible" });
  }
  return access;
}

/** owner/admin of the space (text channels only); dm pin rule is handled by the caller. */
export function canModerateChannel(access: ChannelAccess): boolean {
  return access.channel.kind === "text" && (access.spaceRole === "owner" || access.spaceRole === "admin");
}

/**
 * Channels the user can read (data-model.md 4장 4번): public text channels of live spaces the user belongs to,
 * private text channels where the user is also in channel_members, and the user's dm/group_dm channels.
 * With `spaceId`, only that space's text channels.
 */
export async function accessibleChannelIds(db: D1Database, userId: string, opts: { spaceId?: string } = {}): Promise<string[]> {
  const textPart = `
    SELECT c.id
      FROM space_members m
      JOIN spaces s ON s.id = m.space_id AND s.deleted_at IS NULL
      JOIN channels c ON c.space_id = m.space_id AND c.kind = 'text' AND c.deleted_at IS NULL
     WHERE m.user_id = ?1 ${opts.spaceId !== undefined ? "AND m.space_id = ?2" : ""}
       AND (c.is_private = 0
            OR EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = c.id AND cm.user_id = ?1))`;
  if (opts.spaceId !== undefined) {
    return (await all<{ id: string }>(db, `${textPart} ORDER BY c.id`, userId, opts.spaceId)).map((r) => r.id);
  }
  const dmPart = `
    SELECT c.id
      FROM channel_members cm
      JOIN channels c ON c.id = cm.channel_id AND c.kind IN ('dm', 'group_dm') AND c.deleted_at IS NULL
     WHERE cm.user_id = ?1`;
  return (await all<{ id: string }>(db, `SELECT id FROM (${textPart} UNION ALL ${dmPart}) ORDER BY id`, userId)).map((r) => r.id);
}

// ---------------------------------------------------------------------------
// posts & user content (api.md 3.3 / 3.4)

const SQL_ALIAS = /^[A-Za-z_][A-Za-z0-9_]*$/;

/**
 * SQL fragment + params enforcing api.md 3.4 for posts alias `alias` and the viewer:
 * not deleted, author not suspended (now), no block either way, author public OR viewer = author OR active follower.
 * Uses anonymous `?` placeholders — append `params` in order where the fragment is placed.
 */
export function postVisibilitySql(viewerId: string, alias = "p"): { sql: string; params: unknown[] } {
  if (!SQL_ALIAS.test(alias)) throw new Error(`postVisibilitySql: invalid alias ${JSON.stringify(alias)}`);
  const a = alias;
  const sql = `(${a}.deleted_at IS NULL
    AND EXISTS (SELECT 1 FROM users ${a}_vu
                 WHERE ${a}_vu.id = ${a}.author_id
                   AND (${a}_vu.suspended_at IS NULL OR (${a}_vu.suspended_until IS NOT NULL AND ${a}_vu.suspended_until <= ?))
                   AND (${a}_vu.is_private = 0 OR ${a}_vu.id = ?
                        OR EXISTS (SELECT 1 FROM follows ${a}_vf
                                    WHERE ${a}_vf.follower_id = ? AND ${a}_vf.followee_id = ${a}_vu.id AND ${a}_vf.status = 'active')))
    AND NOT EXISTS (SELECT 1 FROM blocks ${a}_vb
                     WHERE (${a}_vb.blocker_id = ? AND ${a}_vb.blocked_id = ${a}.author_id)
                        OR (${a}_vb.blocker_id = ${a}.author_id AND ${a}_vb.blocked_id = ?)))`;
  return { sql, params: [now(), viewerId, viewerId, viewerId, viewerId] };
}

export async function canViewPost(db: D1Database, viewerId: string, post: PostRow): Promise<boolean> {
  if (post.deleted_at !== null) return false;
  const vis = postVisibilitySql(viewerId, "p");
  return (await first(db, `SELECT 1 AS x FROM posts p WHERE p.id = ? AND ${vis.sql}`, post.id, ...vis.params)) !== null;
}

/**
 * Owner's SNS content (posts, stories, guestbook, follow lists) for anyone but the owner: missing owner, owner suspended
 * now (api.md 3.4 rule 2 / 3.9 — same boundary as postVisibilitySql: suspended_until <= now counts as active again) or
 * block either way -> hidden; private + not active follower -> private.
 */
export async function canViewUserContent(db: D1Database, viewerId: string, ownerId: string): Promise<"ok" | "private" | "hidden"> {
  if (viewerId === ownerId) return "ok";
  const row = await first<{ is_private: number; suspended_at: number | null; suspended_until: number | null; blocked: number; following: number }>(
    db,
    `SELECT u.is_private, u.suspended_at, u.suspended_until,
            EXISTS (SELECT 1 FROM blocks b
                     WHERE (b.blocker_id = ?1 AND b.blocked_id = u.id) OR (b.blocker_id = u.id AND b.blocked_id = ?1)) AS blocked,
            EXISTS (SELECT 1 FROM follows f
                     WHERE f.follower_id = ?1 AND f.followee_id = u.id AND f.status = 'active') AS following
       FROM users u WHERE u.id = ?2`,
    viewerId,
    ownerId,
  );
  if (row === null || row.blocked || isSuspendedNow(row, now())) return "hidden";
  if (row.is_private === 1 && !row.following) return "private";
  return "ok";
}

/**
 * SQL fragment + params: guestbook alias `g` is listed for the viewer — not deleted, author not suspended now (same
 * boundary as postVisibilitySql), no block either way between the viewer and the author. The owner side is checked
 * separately with canViewUserContent. Anonymous `?` placeholders: append `params` in order.
 */
export function guestbookEntryVisibilitySql(viewerId: string, alias = "g"): { sql: string; params: unknown[] } {
  if (!SQL_ALIAS.test(alias)) throw new Error(`guestbookEntryVisibilitySql: invalid alias ${JSON.stringify(alias)}`);
  const a = alias;
  const sql = `(${a}.deleted_at IS NULL
    AND EXISTS (SELECT 1 FROM users ${a}_gu
                 WHERE ${a}_gu.id = ${a}.author_id
                   AND (${a}_gu.suspended_at IS NULL OR (${a}_gu.suspended_until IS NOT NULL AND ${a}_gu.suspended_until <= ?)))
    AND NOT EXISTS (SELECT 1 FROM blocks ${a}_gb
                     WHERE (${a}_gb.blocker_id = ? AND ${a}_gb.blocked_id = ${a}.author_id)
                        OR (${a}_gb.blocker_id = ${a}.author_id AND ${a}_gb.blocked_id = ?)))`;
  return { sql, params: [now(), viewerId, viewerId] };
}

// ---------------------------------------------------------------------------
// presence (api.md 3.6)

/** Subset of targetIds whose presence the viewer may see: self, friends, shared live space, shared dm/group_dm. */
export async function presenceVisible(db: D1Database, viewerId: string, targetIds: string[]): Promise<Set<string>> {
  const ids = [...new Set(targetIds)];
  if (ids.length === 0) return new Set();
  const rows = await all<{ id: string }>(
    db,
    `SELECT j.value AS id
       FROM json_each(?1) j
      WHERE j.value = ?2
         OR EXISTS (SELECT 1 FROM friendships f
                     WHERE f.user_a = MIN(?2, j.value) AND f.user_b = MAX(?2, j.value))
         OR EXISTS (SELECT 1 FROM space_members m1
                      JOIN space_members m2 ON m2.space_id = m1.space_id AND m2.user_id = j.value
                      JOIN spaces s ON s.id = m1.space_id AND s.deleted_at IS NULL
                     WHERE m1.user_id = ?2)
         OR EXISTS (SELECT 1 FROM channel_members c1
                      JOIN channel_members c2 ON c2.channel_id = c1.channel_id AND c2.user_id = j.value
                      JOIN channels c ON c.id = c1.channel_id AND c.kind IN ('dm', 'group_dm') AND c.deleted_at IS NULL
                     WHERE c1.user_id = ?2)`,
    jsonList(ids),
    viewerId,
  );
  return new Set(rows.map((r) => r.id));
}
