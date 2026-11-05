// Space / channel membership services (server-architecture.md 4.14 membership.ts).
// Task 19: addChannelMembers / removeChannelMember for group_dm (api.md 3.1, 5.6). The private text branch is Task 23;
// joinSpace / removeFromSpace are Task 21 (the invite branches of joinSpace's `via` are Task 22). group_dm system messages (group_member_added/removed) come from Task 20's
// postSystemMessage after the membership batch committed.
// Each write is one db.batch, then publish (server-architecture.md 3.5).
import type { Runtime } from "../env";
import { all, batch, first, jsonList, stmt } from "../lib/db";
import { ApiError, err } from "../lib/errors";
import { canDmMany, getChannelAccess } from "../lib/permissions";
import { publishEvent, publishEvents } from "../lib/realtime";
import { loadUserSummaries, serializeChannels, serializeSpaceDetail, serializeSpaceMembers } from "../lib/serialize";
import { now } from "../lib/time";
import { postSystemMessage } from "./messages";
import type { Channel, SpaceDetail } from "../types/api";
import type { SpaceRemovalReason } from "../types/events";
import type { ChannelRow, SpaceMemberRow, SpaceRow } from "../types/rows";

/** Members of a group DM, creator included (api.md 3.1). */
export const GROUP_DM_MAX_MEMBERS = 10;

function notSupported(): ApiError {
  return err.conflict("conflict", "not_supported");
}

/** 403 dm_not_allowed listing the user ids that failed (api.md 3.1). */
export function dmNotAllowed(userIds: string[]): ApiError {
  return err.forbidden("dm_not_allowed", "dm not allowed", { user_ids: userIds });
}

async function loadChannel(db: D1Database, channelId: string): Promise<ChannelRow> {
  const row = await first<ChannelRow>(db, "SELECT * FROM channels WHERE id = ?1", channelId);
  if (row === null) throw err.notFound("channel");
  return row;
}

/**
 * Adds `userIds` to a channel the actor can read (404 otherwise) and returns it as the actor sees it.
 * group_dm (any member may add): 409 already_member if anyone is already in; 422 limit_exceeded group_dm_members when
 * the total would pass 10; 403 dm_not_allowed (details.user_ids) for every X with !can_dm(actor, X) or a block either way
 * with any current member. 1:1 dm and public text -> 409 conflict not_supported.
 * Events after commit: channel.member_added{channel_id, user} per new member to the existing members,
 * channel.created{channel} to the new members; then one system message group_member_added per new member.
 */
export async function addChannelMembers(rt: Runtime, channelId: string, actorId: string, userIds: string[]): Promise<Channel> {
  const db = rt.env.DB;
  const access = await getChannelAccess(db, channelId, actorId);
  if (access === null) throw err.notFound("channel");
  const ch = access.channel;
  if (ch.kind === "dm" || (ch.kind === "text" && ch.is_private === 0)) throw notSupported();
  if (ch.kind === "text") throw notSupported(); // private text channel members: Task 23

  const ids = [...new Set(userIds)];
  if (ids.length === 0) throw err.invalid({ users: "required" });
  const current = (await all<{ user_id: string }>(db, "SELECT user_id FROM channel_members WHERE channel_id = ?1", ch.id)).map((r) => r.user_id);
  const currentSet = new Set(current);
  if (ids.some((id) => currentSet.has(id))) throw err.conflict("already_member");
  if (current.length + ids.length > GROUP_DM_MAX_MEMBERS) throw err.limit("group_dm_members", GROUP_DM_MAX_MEMBERS);

  const { denied } = await canDmMany(db, actorId, ids);
  const blockedWithMember = await all<{ id: string }>(
    db,
    `SELECT j.value AS id FROM json_each(?1) j
      WHERE EXISTS (SELECT 1 FROM blocks b, json_each(?2) m
                     WHERE (b.blocker_id = j.value AND b.blocked_id = m.value)
                        OR (b.blocker_id = m.value AND b.blocked_id = j.value))`,
    jsonList(ids),
    jsonList(current),
  );
  const failed = new Set([...denied, ...blockedWithMember.map((r) => r.id)]);
  if (failed.size > 0) throw dmNotAllowed(ids.filter((id) => failed.has(id)));

  const t = now();
  // The insert re-checks capacity and membership inside the transaction (concurrent adds cannot pass 10).
  const [ins] = await batch(db, [
    stmt(
      db,
      `INSERT INTO channel_members (channel_id, user_id, added_by, joined_at)
       SELECT ?1, j.value, ?2, ?3 FROM json_each(?4) j
        WHERE (SELECT COUNT(*) FROM channel_members WHERE channel_id = ?1) + json_array_length(?4) <= ?5
          AND NOT EXISTS (SELECT 1 FROM channel_members cm, json_each(?4) k WHERE cm.channel_id = ?1 AND cm.user_id = k.value)
       ON CONFLICT DO NOTHING`,
      ch.id,
      actorId,
      t,
      jsonList(ids),
      GROUP_DM_MAX_MEMBERS,
    ),
    stmt(db, "UPDATE channels SET member_count = (SELECT COUNT(*) FROM channel_members WHERE channel_id = ?1), updated_at = ?2 WHERE id = ?1", ch.id, t),
  ]);
  if ((ins.meta.changes ?? 0) === 0) {
    // lost a race: report what is true now
    const n = await first<{ n: number; dup: number }>(
      db,
      `SELECT COUNT(*) AS n, SUM(user_id IN (SELECT value FROM json_each(?2))) AS dup FROM channel_members WHERE channel_id = ?1`,
      ch.id,
      jsonList(ids),
    );
    if ((n?.dup ?? 0) > 0) throw err.conflict("already_member");
    throw err.limit("group_dm_members", GROUP_DM_MAX_MEMBERS);
  }

  const row = await loadChannel(db, ch.id);
  const [[forEvent], [forActor], users] = await Promise.all([
    serializeChannels(db, null, [row]),
    serializeChannels(db, actorId, [row]),
    loadUserSummaries(db, ids),
  ]);
  const added = ids.flatMap((id) => {
    const user = users.get(id);
    return user ? [{ type: "channel.member_added" as const, data: { channel_id: ch.id, user } }] : [];
  });
  await publishEvents(rt, { users: current }, added);
  await publishEvent(rt, { users: ids }, "channel.created", { channel: forEvent! });
  for (const id of ids) {
    if (!users.has(id)) continue;
    await postSystemMessage(rt, ch.id, actorId, { event: "group_member_added", actor_id: actorId, target_user_id: id, data: {} });
  }
  return forActor!;
}

/**
 * Removes `userId` from a channel the actor can read (404 otherwise; 404 too when `userId` is not a member).
 * group_dm: the owner (owner_id) may remove anyone, everyone may remove themselves; otherwise 403 forbidden.
 * 1:1 dm and public text -> 409 conflict not_supported.
 * Events after commit: channel.member_removed{channel_id, user_id} to the remaining members,
 * channel.deleted{channel_id, space_id, reason:"removed"} to the removed user; then (group_dm) the system message
 * group_member_removed (actor = who removed, target = who left; the same id when leaving) for the remaining members.
 */
export async function removeChannelMember(rt: Runtime, channelId: string, actorId: string, userId: string): Promise<void> {
  const db = rt.env.DB;
  const access = await getChannelAccess(db, channelId, actorId);
  if (access === null) throw err.notFound("channel");
  const ch = access.channel;
  if (ch.kind === "dm" || (ch.kind === "text" && ch.is_private === 0)) throw notSupported();
  if (ch.kind === "text") throw notSupported(); // private text channel members: Task 23
  if (userId !== actorId && ch.owner_id !== actorId) throw err.forbidden("forbidden", "only the group owner can remove members");

  const t = now();
  const [del] = await batch(db, [
    stmt(db, "DELETE FROM channel_members WHERE channel_id = ?1 AND user_id = ?2", ch.id, userId),
    stmt(db, "UPDATE channels SET member_count = (SELECT COUNT(*) FROM channel_members WHERE channel_id = ?1), updated_at = ?2 WHERE id = ?1", ch.id, t),
  ]);
  if ((del.meta.changes ?? 0) === 0) throw err.notFound("member");

  await publishEvent(rt, { channel: ch.id }, "channel.member_removed", { channel_id: ch.id, user_id: userId });
  await publishEvent(rt, { users: [userId] }, "channel.deleted", { channel_id: ch.id, space_id: ch.space_id, reason: "removed" });
  if (ch.kind === "group_dm") {
    await postSystemMessage(rt, ch.id, actorId, { event: "group_member_removed", actor_id: actorId, target_user_id: userId, data: {} });
  }
}

// ---- spaces (plan 01 Task 21) ----

/** api.md 3.5 limits. */
export const SPACE_MEMBERS_MAX = 5000;
export const OWNED_SPACES_MAX = 100;

function isUniqueViolation(e: unknown): boolean {
  return e instanceof Error && /UNIQUE constraint failed/i.test(e.message);
}

type JoinFactsRow = SpaceRow & { _banned: number; _member: number };

/**
 * `userId` joins a live space (404 otherwise) as a member. Pre-checks in this order: ban 403 banned_from_space,
 * already a member 409 already_member, member_count >= 5000 422 limit_exceeded space_members.
 * One batch: space_members INSERT + spaces.member_count+1 (a concurrent join of the same user fails the PK -> 409).
 * After commit: space.joined{space} to the user (their own view), space.member_joined{space_id, member} to the other
 * members, then the system message member_joined in the default channel.
 * `via` carries the invite whose use is recorded in the same batch — that branch is plan 01 Task 22.
 */
export async function joinSpace(rt: Runtime, spaceId: string, userId: string, via: { inviteCode?: string; directInviteId?: string }): Promise<SpaceDetail> {
  if (via.inviteCode !== undefined || via.directInviteId !== undefined) throw new Error("invite joins are not implemented yet (plan 01 Task 22)");
  const db = rt.env.DB;
  const facts = await first<JoinFactsRow>(
    db,
    `SELECT s.*,
            EXISTS (SELECT 1 FROM space_bans b WHERE b.space_id = s.id AND b.user_id = ?2) AS _banned,
            EXISTS (SELECT 1 FROM space_members m WHERE m.space_id = s.id AND m.user_id = ?2) AS _member
       FROM spaces s WHERE s.id = ?1 AND s.deleted_at IS NULL`,
    spaceId,
    userId,
  );
  if (facts === null) throw err.notFound("space");
  if (facts._banned) throw err.forbidden("banned_from_space");
  if (facts._member) throw err.conflict("already_member");
  if (facts.member_count >= SPACE_MEMBERS_MAX) throw err.limit("space_members", SPACE_MEMBERS_MAX);

  const t = now();
  try {
    await batch(db, [
      stmt(db, "INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?1, ?2, 'member', ?3)", spaceId, userId, t),
      stmt(db, "UPDATE spaces SET member_count = member_count + 1, updated_at = ?2 WHERE id = ?1", spaceId, t),
    ]);
  } catch (e) {
    if (isUniqueViolation(e)) throw err.conflict("already_member");
    throw e;
  }

  const row = await first<SpaceRow>(db, "SELECT * FROM spaces WHERE id = ?1", spaceId);
  if (row === null) throw err.notFound("space");
  const memberRow: SpaceMemberRow = { space_id: spaceId, user_id: userId, role: "member", joined_at: t };
  const [detail, [member]] = await Promise.all([serializeSpaceDetail(db, userId, row), serializeSpaceMembers(db, [memberRow])]);
  await publishEvent(rt, { users: [userId] }, "space.joined", { space: detail });
  if (member) await publishEvent(rt, { space: spaceId }, "space.member_joined", { space_id: spaceId, member }, { exclude: [userId] });
  if (row.default_channel_id !== null) {
    await postSystemMessage(rt, row.default_channel_id, userId, { event: "member_joined", actor_id: userId, target_user_id: userId, data: {} });
  }
  return detail;
}

/**
 * Takes `userId` out of a live space (permission checks are the caller's: api.md 3.5). One batch for every reason, each
 * statement gated on the membership row so a lost race writes nothing: the private text channels of the space lose
 * this user's channel_members rows (member_count - 1 each), (banned) a space_bans row, then the space_members row and
 * spaces.member_count - 1. 404 when the user was not a member.
 * After commit: space.member_left{space_id, user_id, reason} to the members + the removed user, space.removed to the
 * removed user, channel.member_removed per private channel left to its remaining members, then the system message
 * member_left (reason "left") or member_kicked (kicked and banned) in the default channel.
 */
export async function removeFromSpace(
  rt: Runtime,
  spaceId: string,
  userId: string,
  actorId: string,
  reason: SpaceRemovalReason,
  banReason?: string,
): Promise<void> {
  const db = rt.env.DB;
  const space = await first<SpaceRow>(db, "SELECT * FROM spaces WHERE id = ?1 AND deleted_at IS NULL", spaceId);
  if (space === null) throw err.notFound("space");
  const t = now();
  const privateOfSpace = "SELECT id FROM channels WHERE space_id = ?1 AND kind = 'text' AND is_private = 1";
  const isMember = "EXISTS (SELECT 1 FROM space_members WHERE space_id = ?1 AND user_id = ?2)";
  const stmts: D1PreparedStatement[] = [
    // which private channels this removal takes the user out of (read inside the transaction, before the deletes)
    stmt(db, `SELECT channel_id FROM channel_members WHERE user_id = ?2 AND channel_id IN (${privateOfSpace}) AND ${isMember}`, spaceId, userId),
    stmt(
      db,
      `UPDATE channels SET member_count = MAX(member_count - 1, 0), updated_at = ?3
        WHERE id IN (${privateOfSpace})
          AND EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = channels.id AND cm.user_id = ?2)
          AND ${isMember}`,
      spaceId,
      userId,
      t,
    ),
    stmt(db, `DELETE FROM channel_members WHERE user_id = ?2 AND channel_id IN (${privateOfSpace}) AND ${isMember}`, spaceId, userId),
  ];
  if (reason === "banned") {
    stmts.push(
      stmt(
        db,
        `INSERT INTO space_bans (space_id, user_id, banned_by, reason, created_at)
         SELECT ?1, ?2, ?3, ?4, ?5 WHERE ${isMember}
         ON CONFLICT (space_id, user_id) DO UPDATE SET banned_by = excluded.banned_by, reason = excluded.reason, created_at = excluded.created_at`,
        spaceId,
        userId,
        actorId,
        banReason ?? "",
        t,
      ),
    );
  }
  const deleteIdx = stmts.length;
  stmts.push(
    stmt(db, "DELETE FROM space_members WHERE space_id = ?1 AND user_id = ?2", spaceId, userId),
    stmt(db, "UPDATE spaces SET member_count = MAX(member_count - 1, 0), updated_at = ?2 WHERE id = ?1 AND changes() > 0", spaceId, t),
  );
  const results = await batch(db, stmts);
  if ((results[deleteIdx]!.meta.changes ?? 0) === 0) throw err.notFound("member");
  const leftChannels = (results[0]!.results as Array<{ channel_id: string }>).map((r) => r.channel_id);

  await publishEvent(rt, { space: spaceId }, "space.member_left", { space_id: spaceId, user_id: userId, reason }, { includeSelf: userId });
  await publishEvent(rt, { users: [userId] }, "space.removed", { space_id: spaceId, reason });
  for (const channelId of leftChannels) {
    await publishEvent(rt, { channel: channelId }, "channel.member_removed", { channel_id: channelId, user_id: userId });
  }
  if (space.default_channel_id !== null) {
    const system =
      reason === "left"
        ? { event: "member_left" as const, actor_id: userId, target_user_id: userId, data: {} }
        : { event: "member_kicked" as const, actor_id: actorId, target_user_id: userId, data: {} };
    await postSystemMessage(rt, space.default_channel_id, system.actor_id, system);
  }
}
