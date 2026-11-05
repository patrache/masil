// Space / channel membership services (server-architecture.md 4.14 membership.ts).
// Task 19: addChannelMembers / removeChannelMember for group_dm (api.md 3.1, 5.6). The private text branch is Task 23;
// joinSpace / removeFromSpace are Task 21. group_dm system messages (group_member_added/removed) come from Task 20's
// postSystemMessage after the membership batch committed.
// Each write is one db.batch, then publish (server-architecture.md 3.5).
import type { Runtime } from "../env";
import { all, batch, first, jsonList, stmt } from "../lib/db";
import { ApiError, err } from "../lib/errors";
import { canDmMany, getChannelAccess } from "../lib/permissions";
import { publishEvent, publishEvents } from "../lib/realtime";
import { loadUserSummaries, serializeChannels } from "../lib/serialize";
import { now } from "../lib/time";
import { postSystemMessage } from "./messages";
import type { Channel } from "../types/api";
import type { ChannelRow } from "../types/rows";

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
