// Space / channel membership services (server-architecture.md 4.14 membership.ts).
// Task 19: addChannelMembers / removeChannelMember for group_dm (api.md 3.1, 5.6); Task 23 adds the private text branch;
// joinSpace / removeFromSpace are Task 21; the invite branches of joinSpace's `via` are Task 22. group_dm system messages (group_member_added/removed) come from Task 20's
// postSystemMessage after the membership batch committed.
// Each write is one db.batch, then publish (server-architecture.md 3.5).
import type { Runtime } from "../env";
import { all, batch, first, jsonList, stmt } from "../lib/db";
import { ApiError, err } from "../lib/errors";
import { blockedEitherSql, canDmMany, getChannelAccess, suspendedNowSql } from "../lib/permissions";
import { publishEvent, publishEvents } from "../lib/realtime";
import { loadUserSummaries, serializeChannels, serializeSpaceDetail, serializeSpaceMembers } from "../lib/serialize";
import { now } from "../lib/time";
import { postSystemMessage } from "./messages";
import type { Channel, SpaceDetail } from "../types/api";
import type { SpaceRemovalReason } from "../types/events";
import type { ChannelRow, SpaceDirectInviteRow, SpaceInviteRow, SpaceMemberRow, SpaceRow } from "../types/rows";

/** Members of a group DM, creator included (api.md 3.1). */
export const GROUP_DM_MAX_MEMBERS = 10;

function notSupported(): ApiError {
  return err.conflict("conflict", "not_supported");
}

/** 403 dm_not_allowed listing the users X with !can_dm(actor, X), in request order (api.md 3.1). */
export function dmNotAllowed(userIds: string[]): ApiError {
  return err.forbidden("dm_not_allowed", "dm not allowed", { user_ids: userIds });
}

/**
 * 403 dm_not_allowed for a block between two would-be members of a group DM (api.md 3.1, owner decision D2): names
 * nobody and says nothing about direction — details {} — so the actor cannot learn who blocked whom (api.md 0.5).
 */
export function dmBlockedPair(): ApiError {
  return err.forbidden("dm_not_allowed", "some participants cannot be in one conversation");
}

/**
 * Group DM eligibility (api.md 3.1), shared by create (`existing` = [creator]) and member add (`existing` = the current
 * members). Returns the error to throw, or null when allowed:
 * - any X in `newIds` with !can_dm(actor, X) -> dmNotAllowed(those ids, `newIds` order) — the 1:1 rule, which does not
 *   say whether a block was the reason;
 * - otherwise, a block (either direction) linking some X with anyone else who would be in the room (an existing member
 *   or another of `newIds` — every pair, so the actor cannot put two people who blocked each other into one
 *   conversation) -> dmBlockedPair(), which names nobody.
 */
export async function groupDmDenial(db: D1Database, actorId: string, newIds: string[], existing: string[]): Promise<ApiError | null> {
  const ids = [...new Set(newIds)];
  if (ids.length === 0) return null;
  const { denied } = await canDmMany(db, actorId, ids);
  if (denied.length > 0) return dmNotAllowed(denied);
  const blocked = await all<{ id: string }>(
    db,
    `SELECT j.value AS id FROM json_each(?1) j
      WHERE EXISTS (SELECT 1 FROM blocks b, json_each(?2) m
                     WHERE m.value <> j.value
                       AND ((b.blocker_id = j.value AND b.blocked_id = m.value)
                         OR (b.blocker_id = m.value AND b.blocked_id = j.value)))`,
    jsonList(ids),
    jsonList([...new Set([...existing, ...ids])]),
  );
  return blocked.length > 0 ? dmBlockedPair() : null;
}

async function loadChannel(db: D1Database, channelId: string): Promise<ChannelRow> {
  const row = await first<ChannelRow>(db, "SELECT * FROM channels WHERE id = ?1", channelId);
  if (row === null) throw err.notFound("channel");
  return row;
}

/**
 * Adds `userIds` to a channel the actor can read (404 otherwise) and returns it as the actor sees it.
 * private text (api.md 3.5: the space owner·admin or any member of that channel — reading it already requires being a
 * member): see addPrivateTextMembers.
 * group_dm (any member may add): 409 already_member if anyone is already in; 422 limit_exceeded group_dm_members when
 * the total would pass 10; 403 dm_not_allowed — details.user_ids for every X with !can_dm(actor, X), or details {} (no ids)
 * for a block either way between X and any current member or another user being added (groupDmDenial). The batch
 * re-checks capacity, membership and blocks against the members it sees, so concurrent adds cannot get past any of them
 * (the loser gets the same errors).
 * 1:1 dm and public text -> 409 conflict not_supported.
 * Events after commit: channel.member_added{channel_id, user} per new member to the existing members,
 * channel.created{channel} to the new members; then one system message group_member_added per new member.
 */
export async function addChannelMembers(rt: Runtime, channelId: string, actorId: string, userIds: string[]): Promise<Channel> {
  const db = rt.env.DB;
  const access = await getChannelAccess(db, channelId, actorId);
  if (access === null) throw err.notFound("channel");
  const ch = access.channel;
  if (ch.kind === "dm" || (ch.kind === "text" && ch.is_private === 0)) throw notSupported();

  const ids = [...new Set(userIds)];
  if (ids.length === 0) throw err.invalid({ users: "required" });
  if (ch.kind === "text") return addPrivateTextMembers(rt, ch, actorId, ids);
  const current = (await all<{ user_id: string }>(db, "SELECT user_id FROM channel_members WHERE channel_id = ?1", ch.id)).map((r) => r.user_id);
  const currentSet = new Set(current);
  if (ids.some((id) => currentSet.has(id))) throw err.conflict("already_member");
  if (current.length + ids.length > GROUP_DM_MAX_MEMBERS) throw err.limit("group_dm_members", GROUP_DM_MAX_MEMBERS);

  const denial = await groupDmDenial(db, actorId, ids, current);
  if (denial !== null) throw denial;

  const t = now();
  // The batch re-checks capacity, membership and blocks against the members it sees inside the transaction: the
  // pre-checks above are plain reads, so concurrent adds would otherwise pass 10, or put two users who block each other
  // into the room (each request checked against the same members and missed the other's insert; pending item 8).
  // Statement 0 reads the same facts the INSERT gates on, in the same transaction, so a refused insert reports why.
  const room = "SELECT user_id FROM channel_members WHERE channel_id = ?1 UNION SELECT value FROM json_each(?2)";
  const [facts, ins] = await batch(db, [
    stmt(
      db,
      `SELECT j.value AS id,
              (SELECT COUNT(*) FROM channel_members WHERE channel_id = ?1) AS n,
              EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = ?1 AND cm.user_id = j.value) AS dup,
              EXISTS (SELECT 1 FROM blocks b
                       WHERE (b.blocker_id = j.value AND b.blocked_id IN (${room}))
                          OR (b.blocked_id = j.value AND b.blocker_id IN (${room}))) AS blocked
         FROM json_each(?2) j`,
      ch.id,
      jsonList(ids),
    ),
    stmt(
      db,
      `INSERT INTO channel_members (channel_id, user_id, added_by, joined_at)
       SELECT ?1, j.value, ?3, ?4 FROM json_each(?2) j
        WHERE (SELECT COUNT(*) FROM channel_members WHERE channel_id = ?1) + json_array_length(?2) <= ?5
          AND NOT EXISTS (SELECT 1 FROM channel_members cm, json_each(?2) k WHERE cm.channel_id = ?1 AND cm.user_id = k.value)
          AND NOT EXISTS (SELECT 1 FROM blocks b
                           WHERE (b.blocker_id IN (SELECT value FROM json_each(?2)) AND b.blocked_id IN (${room}))
                              OR (b.blocked_id IN (SELECT value FROM json_each(?2)) AND b.blocker_id IN (${room})))
       ON CONFLICT DO NOTHING`,
      ch.id,
      jsonList(ids),
      actorId,
      t,
      GROUP_DM_MAX_MEMBERS,
    ),
    stmt(db, "UPDATE channels SET member_count = (SELECT COUNT(*) FROM channel_members WHERE channel_id = ?1), updated_at = ?2 WHERE id = ?1", ch.id, t),
  ]);
  if ((ins!.meta.changes ?? 0) === 0) {
    // lost a race: report what the insert saw (same order as the pre-checks)
    const seen = facts!.results as Array<{ id: string; n: number; dup: number; blocked: number }>;
    if (seen.some((f) => f.dup)) throw err.conflict("already_member");
    if ((seen[0]?.n ?? 0) + ids.length > GROUP_DM_MAX_MEMBERS) throw err.limit("group_dm_members", GROUP_DM_MAX_MEMBERS);
    throw dmBlockedPair(); // a block with a member the insert saw: named to nobody (D2)
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
 * Private text branch of addChannelMembers (api.md 3.5, 5.6): every target must be a current member of the channel's
 * space (404 otherwise, before anything else), none may already be in the channel (409 already_member). No member cap
 * beyond the space's own. One batch: channel_members INSERT (gated per row on the space membership, skipping rows that
 * appeared meanwhile; RETURNING tells who actually went in) + member_count recount.
 * Events after commit: channel.member_added{channel_id, user} per new member to the members before the add,
 * channel.created{channel} to the new members. No system message (group_dm only).
 */
async function addPrivateTextMembers(rt: Runtime, ch: ChannelRow, actorId: string, ids: string[]): Promise<Channel> {
  const db = rt.env.DB;
  const facts = await all<{ id: string; in_space: number; in_channel: number }>(
    db,
    `SELECT j.value AS id,
            EXISTS (SELECT 1 FROM space_members sm WHERE sm.space_id = ?2 AND sm.user_id = j.value) AS in_space,
            EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = ?3 AND cm.user_id = j.value) AS in_channel
       FROM json_each(?1) j`,
    jsonList(ids),
    ch.space_id,
    ch.id,
  );
  if (facts.some((f) => !f.in_space)) throw err.notFound("member");
  if (facts.some((f) => f.in_channel)) throw err.conflict("already_member");
  const before = (
    await all<{ user_id: string }>(
      db,
      `SELECT cm.user_id FROM channel_members cm
         JOIN space_members sm ON sm.space_id = ?2 AND sm.user_id = cm.user_id
        WHERE cm.channel_id = ?1`,
      ch.id,
      ch.space_id,
    )
  ).map((r) => r.user_id);

  const t = now();
  const [ins] = await batch(db, [
    stmt(
      db,
      `INSERT INTO channel_members (channel_id, user_id, added_by, joined_at)
       SELECT ?1, j.value, ?2, ?3 FROM json_each(?4) j
        WHERE EXISTS (SELECT 1 FROM space_members sm WHERE sm.space_id = ?5 AND sm.user_id = j.value)
       ON CONFLICT DO NOTHING
       RETURNING user_id`,
      ch.id,
      actorId,
      t,
      jsonList(ids),
      ch.space_id,
    ),
    stmt(db, "UPDATE channels SET member_count = (SELECT COUNT(*) FROM channel_members WHERE channel_id = ?1), updated_at = ?2 WHERE id = ?1", ch.id, t),
  ]);
  const insertedSet = new Set((ins!.results as Array<{ user_id: string }>).map((r) => r.user_id));
  const inserted = ids.filter((id) => insertedSet.has(id));
  if (inserted.length === 0) throw err.conflict("already_member"); // lost a race to a concurrent add

  const row = await loadChannel(db, ch.id);
  const [[forEvent], [forActor], users] = await Promise.all([
    serializeChannels(db, null, [row]),
    serializeChannels(db, actorId, [row]),
    loadUserSummaries(db, inserted),
  ]);
  const added = inserted.flatMap((id) => {
    const user = users.get(id);
    return user ? [{ type: "channel.member_added" as const, data: { channel_id: ch.id, user } }] : [];
  });
  await publishEvents(rt, { users: before }, added);
  await publishEvent(rt, { users: inserted }, "channel.created", { channel: forEvent! });
  return forActor!;
}

/**
 * Removes `userId` from a channel the actor can read (404 otherwise; 404 too when `userId` is not a member).
 * group_dm: the owner (owner_id) may remove anyone, everyone may remove themselves; otherwise 403 forbidden.
 * private text: the space owner·admin may remove anyone, everyone may remove themselves (@me); otherwise 403 forbidden.
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
  if (userId !== actorId) {
    if (ch.kind === "group_dm" && ch.owner_id !== actorId) throw err.forbidden("forbidden", "only the group owner can remove members");
    // private text: removing someone else takes the space owner·admin (api.md 3.5); members may only add
    if (ch.kind === "text" && access.spaceRole !== "owner" && access.spaceRole !== "admin") {
      throw err.forbidden("forbidden", "requires admin role");
    }
  }

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

export type InviteInvalidReason = "revoked" | "expired" | "exhausted";

/**
 * An invite link only counts while its creator still holds owner·admin in its space (api.md 3.5, pending item 9):
 * removal and demotion revoke the creator's links, and every use re-checks this (preview, the joinSpace pre-check and
 * its conditional UPDATE), so a link that outlived its creator's right (a path that skipped the revoke, rows from
 * before the fix, a link inserted in the race with a removal) behaves as revoked. `t` is the space_invites table or alias.
 */
export function linkIssuerCanInvite(t: string): string {
  return `EXISTS (SELECT 1 FROM space_members lm WHERE lm.space_id = ${t}.space_id AND lm.user_id = ${t}.created_by AND lm.role IN ('owner', 'admin'))`;
}

/** A space_invites row loaded with `${linkIssuerCanInvite(t)} AS _issuer_ok`. */
export type InviteCheckRow = SpaceInviteRow & { _issuer_ok: number };

/**
 * Why an invite link can no longer be used (revoked > expired > exhausted), or null while it is usable. A link whose
 * creator lost owner·admin (or left the space) counts as revoked.
 */
export function inviteInvalidReason(row: InviteCheckRow, nowMs: number = now()): InviteInvalidReason | null {
  if (row.revoked_at !== null || !row._issuer_ok) return "revoked";
  if (row.expires_at !== null && row.expires_at <= nowMs) return "expired";
  if (row.max_uses !== null && row.use_count >= row.max_uses) return "exhausted";
  return null;
}

/**
 * A direct invite only counts while its inviter is still a member of the space (pending item 9): removeFromSpace
 * cancels their pending invites, and joinSpace re-checks this in the pre-check and in the conditional UPDATE, so an
 * invite that outlived its inviter (a removal path that skipped the cancel, rows from before the fix) still fails.
 * For a statement over space_direct_invites whose space id is bound as ?2.
 */
const INVITER_IS_MEMBER = "EXISTS (SELECT 1 FROM space_members im WHERE im.space_id = ?2 AND im.user_id = space_direct_invites.inviter_id)";

/**
 * For a statement over space_direct_invites aliased `d`: its inviter is still a member of its space. A pending invite
 * whose inviter is gone cannot be accepted (joinSpace), so it is left out of the invitee's list and does not count as
 * the pending invite that blocks a fresh one (pending item 9).
 */
export const D_INVITER_IS_MEMBER = "EXISTS (SELECT 1 FROM space_members im WHERE im.space_id = d.space_id AND im.user_id = d.inviter_id)";

/**
 * Writes a pending direct invite `inviterId` -> `inviteeId` into `spaceId`, in one batch (pending item 9): a stale
 * pending invite to the same person whose inviter is no longer a member becomes canceled, then the INSERT — both gated
 * on the inviter still being a member, so an inviter removed after the caller's role check writes nothing (404).
 * Both are also gated on the relation rules (pending item 10, api.md 3.2 / 3.5 / 3.9): the two are still friends, no
 * block either way, the invitee not suspended now. The caller's pre-checks are plain reads, and a block committing
 * after them (its cleanup deletes the friendship and cancels pending invites both ways) would otherwise leave a fresh
 * pending invite behind -> 403 action_not_allowed, nothing written.
 * A concurrent invite to the same person fails the unique index -> 409 already_exists.
 * Returns the new row and the stale invites it canceled (the caller publishes space.invite_removed for those).
 */
export async function insertDirectInvite(
  db: D1Database,
  spaceId: string,
  inviterId: string,
  inviteeId: string,
  t: number,
  id: string,
): Promise<{ row: SpaceDirectInviteRow; stale: Array<{ id: string; inviter_id: string }> }> {
  const inviterIsMember = "EXISTS (SELECT 1 FROM space_members WHERE space_id = ?1 AND user_id = ?2)";
  const related = `EXISTS (SELECT 1 FROM friendships f WHERE f.user_a = MIN(?2, ?3) AND f.user_b = MAX(?2, ?3))
    AND NOT ${blockedEitherSql("?2", "?3")} AND NOT ${suspendedNowSql("?3", "?4")}`;
  const gate = `${inviterIsMember} AND ${related}`;
  let results: D1Result[];
  try {
    results = await batch(db, [
      stmt(
        db,
        `UPDATE space_direct_invites AS d SET status = 'canceled', responded_at = ?4
          WHERE d.space_id = ?1 AND d.invitee_id = ?3 AND d.status = 'pending' AND NOT ${D_INVITER_IS_MEMBER} AND ${gate}
         RETURNING id, inviter_id`,
        spaceId,
        inviterId,
        inviteeId,
        t,
      ),
      stmt(
        db,
        `INSERT INTO space_direct_invites (id, space_id, inviter_id, invitee_id, status, created_at, responded_at)
         SELECT ?5, ?1, ?2, ?3, 'pending', ?4, NULL WHERE ${gate}
         RETURNING *`,
        spaceId,
        inviterId,
        inviteeId,
        t,
        id,
      ),
    ]);
  } catch (e) {
    if (isUniqueViolation(e)) throw err.conflict("already_exists"); // a concurrent invite to the same person
    throw e;
  }
  const row = (results[1]!.results as SpaceDirectInviteRow[])[0] ?? null;
  if (row === null) {
    const member = await first(db, "SELECT 1 AS x FROM space_members WHERE space_id = ?1 AND user_id = ?2", spaceId, inviterId);
    if (member === null) throw err.notFound("space"); // the inviter left / was removed after the role check
    throw err.forbidden("action_not_allowed"); // unfriended / blocked / invitee suspended after the pre-checks
  }
  return { row, stale: results[0]!.results as Array<{ id: string; inviter_id: string }> };
}

/** The invite link `code` of `spaceId` with its issuer check; 404 when missing. */
async function loadInviteOf(db: D1Database, spaceId: string, code: string): Promise<InviteCheckRow> {
  const row = await first<InviteCheckRow>(
    db,
    `SELECT *, ${linkIssuerCanInvite("space_invites")} AS _issuer_ok FROM space_invites WHERE code = ?1 AND space_id = ?2`,
    code,
    spaceId,
  );
  if (row === null) throw err.notFound("invite");
  return row;
}

/**
 * `userId` joins a live space (404 otherwise) as a member. Pre-checks in this order: (inviteCode) the link is usable
 * and its creator still owner·admin 410 invite_invalid / (directInviteId) a pending invite to this user from a current member of the space 404, then
 * ban 403 banned_from_space, already a member 409 already_member, member_count >= 5000 422 limit_exceeded
 * space_members (pre-check only: concurrent joins may pass the cap by one or two, which keeps the invite use and the
 * join in one batch).
 * One batch (plan 01 Task 22, exactly these statements in this order):
 *   via invite: [invite UPDATE (use_count+1 / status accepted) gated on still being usable — link: its creator still
 *   owner·admin; direct: still pending and its inviter still a member] ->
 *   space_members INSERT gated on changes() > 0 -> spaces.member_count+1 gated on changes() > 0.
 *   via {}: the INSERT and the member_count UPDATE without the gates (Task 21).
 * A first statement that changed nothing means the invite was used up / answered meanwhile and nothing was written:
 * 410 with the re-read reason (link) or 404 (direct). A concurrent join of the same user fails the PK and rolls the
 * whole batch back -> 409 already_member.
 * After commit (only when a row went in): space.joined{space} to the user (their own view), space.member_joined
 * {space_id, member} to the other members, (direct) space.invite_removed{invite_id, reason:"accepted"} to the invitee
 * and the inviter, then the system message member_joined in the default channel.
 */
export async function joinSpace(rt: Runtime, spaceId: string, userId: string, via: { inviteCode?: string; directInviteId?: string }): Promise<SpaceDetail> {
  const db = rt.env.DB;
  let direct: SpaceDirectInviteRow | null = null;
  if (via.inviteCode !== undefined) {
    const reason = inviteInvalidReason(await loadInviteOf(db, spaceId, via.inviteCode));
    if (reason !== null) throw err.inviteInvalid(reason);
  } else if (via.directInviteId !== undefined) {
    direct = await first<SpaceDirectInviteRow>(
      db,
      `SELECT * FROM space_direct_invites
        WHERE id = ?1 AND space_id = ?2 AND invitee_id = ?3 AND status = 'pending' AND ${INVITER_IS_MEMBER}`,
      via.directInviteId,
      spaceId,
      userId,
    );
    if (direct === null) throw err.notFound("invite");
  }
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
  const gated = via.inviteCode !== undefined || via.directInviteId !== undefined;
  const stmts: D1PreparedStatement[] = [];
  if (via.inviteCode !== undefined) {
    stmts.push(
      stmt(
        db,
        `UPDATE space_invites SET use_count = use_count + 1
          WHERE code = ?1 AND space_id = ?2 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?3)
            AND (max_uses IS NULL OR use_count < max_uses) AND ${linkIssuerCanInvite("space_invites")}`,
        via.inviteCode,
        spaceId,
        t,
      ),
    );
  } else if (via.directInviteId !== undefined) {
    stmts.push(
      stmt(
        db,
        `UPDATE space_direct_invites SET status = 'accepted', responded_at = ?3
          WHERE id = ?1 AND space_id = ?2 AND invitee_id = ?4 AND status = 'pending' AND ${INVITER_IS_MEMBER}`,
        via.directInviteId,
        spaceId,
        t,
        userId,
      ),
    );
  }
  const gate = gated ? " WHERE changes() > 0" : "";
  stmts.push(
    stmt(db, `INSERT INTO space_members (space_id, user_id, role, joined_at) SELECT ?1, ?2, 'member', ?3${gate}`, spaceId, userId, t),
    stmt(db, `UPDATE spaces SET member_count = member_count + 1, updated_at = ?2 WHERE id = ?1${gated ? " AND changes() > 0" : ""}`, spaceId, t),
  );
  let results: D1Result[];
  try {
    results = await batch(db, stmts);
  } catch (e) {
    if (isUniqueViolation(e)) throw err.conflict("already_member");
    throw e;
  }
  if (gated && (results[0]!.meta.changes ?? 0) === 0) {
    if (via.inviteCode !== undefined) {
      throw err.inviteInvalid(inviteInvalidReason(await loadInviteOf(db, spaceId, via.inviteCode), t) ?? "exhausted");
    }
    throw err.notFound("invite");
  }
  if ((results[gated ? 1 : 0]!.meta.changes ?? 0) !== 1) throw err.conflict("already_member");

  const row = await first<SpaceRow>(db, "SELECT * FROM spaces WHERE id = ?1", spaceId);
  if (row === null) throw err.notFound("space");
  const memberRow: SpaceMemberRow = { space_id: spaceId, user_id: userId, role: "member", joined_at: t };
  const [detail, [member]] = await Promise.all([serializeSpaceDetail(db, userId, row), serializeSpaceMembers(db, [memberRow])]);
  await publishEvent(rt, { users: [userId] }, "space.joined", { space: detail });
  if (member) await publishEvent(rt, { space: spaceId }, "space.member_joined", { space_id: spaceId, member }, { exclude: [userId] });
  if (direct !== null) {
    await publishEvent(rt, { users: [direct.invitee_id, direct.inviter_id] }, "space.invite_removed", { invite_id: direct.id, reason: "accepted" });
  }
  if (row.default_channel_id !== null) {
    await postSystemMessage(rt, row.default_channel_id, userId, { event: "member_joined", actor_id: userId, target_user_id: userId, data: {} });
  }
  return detail;
}

/**
 * Takes `userId` out of a live space (permission checks are the caller's: api.md 3.5). One batch for every reason, each
 * statement gated on the membership row so a lost race writes nothing: the private text channels of the space lose
 * this user's channel_members rows (member_count - 1 each), (banned) a space_bans row, this user's pending direct
 * invites to the space become canceled and their invite links revoked (pending item 9), then the space_members row
 * and spaces.member_count - 1. 404 when the user was not a member.
 * After commit: space.member_left{space_id, user_id, reason} to the members + the removed user, space.removed to the
 * removed user, channel.member_removed per private channel left to its remaining members, space.invite_removed
 * {invite_id, reason:"canceled"} per canceled direct invite to its invitee + the removed user, then the system message
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
  // pending item 9: what this user issued stops admitting people with them — their pending direct invites are
  // canceled and their invite links revoked (a link needs owner·admin, a direct invite needs a member: api.md 3.5)
  const cancelIdx = stmts.length;
  stmts.push(
    stmt(
      db,
      `UPDATE space_direct_invites SET status = 'canceled', responded_at = ?3
        WHERE space_id = ?1 AND inviter_id = ?2 AND status = 'pending' AND ${isMember}
       RETURNING id, invitee_id`,
      spaceId,
      userId,
      t,
    ),
    stmt(db, `UPDATE space_invites SET revoked_at = ?3 WHERE space_id = ?1 AND created_by = ?2 AND revoked_at IS NULL AND ${isMember}`, spaceId, userId, t),
  );
  const deleteIdx = stmts.length;
  stmts.push(
    stmt(db, "DELETE FROM space_members WHERE space_id = ?1 AND user_id = ?2", spaceId, userId),
    stmt(db, "UPDATE spaces SET member_count = MAX(member_count - 1, 0), updated_at = ?2 WHERE id = ?1 AND changes() > 0", spaceId, t),
  );
  const results = await batch(db, stmts);
  if ((results[deleteIdx]!.meta.changes ?? 0) === 0) throw err.notFound("member");
  const leftChannels = (results[0]!.results as Array<{ channel_id: string }>).map((r) => r.channel_id);
  const canceledInvites = results[cancelIdx]!.results as Array<{ id: string; invitee_id: string }>;

  await publishEvent(rt, { space: spaceId }, "space.member_left", { space_id: spaceId, user_id: userId, reason }, { includeSelf: userId });
  await publishEvent(rt, { users: [userId] }, "space.removed", { space_id: spaceId, reason });
  for (const channelId of leftChannels) {
    await publishEvent(rt, { channel: channelId }, "channel.member_removed", { channel_id: channelId, user_id: userId });
  }
  for (const inv of canceledInvites) {
    await publishEvent(rt, { users: [inv.invitee_id, userId] }, "space.invite_removed", { invite_id: inv.id, reason: "canceled" });
  }
  if (space.default_channel_id !== null) {
    const system =
      reason === "left"
        ? { event: "member_left" as const, actor_id: userId, target_user_id: userId, data: {} }
        : { event: "member_kicked" as const, actor_id: actorId, target_user_id: userId, data: {} };
    await postSystemMessage(rt, space.default_channel_id, system.actor_id, system);
  }
}
