// Relation services (server-architecture.md 4.14 relations.ts). Plan 01 owns everything here except `follow`
// (plan 02). Task 17: makeFriends / unfriend. Task 18: applyBlock / removeBlock / approveAllPendingFollows.
// Each writes in one db.batch, then publishes (server-architecture.md 3.5).
import type { Runtime } from "../env";
import { batch, first, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { notify, notifyMany } from "../lib/notify";
import { publishEvent, publishEvents } from "../lib/realtime";
import { loadUserSummaries, serializeFriendRequests, serializeFriends } from "../lib/serialize";
import { now } from "../lib/time";
import type { FriendRequestRow, FriendshipRow } from "../types/rows";

/** friendships keeps one row per pair with user_a < user_b (data-model.md). */
function friendPair(a: string, b: string): [string, string] {
  return a < b ? [a, b] : [b, a];
}

/**
 * Accepts pending request `requestId` between users `a` and `b` (either order; the row says who sent it) and makes them
 * friends, in one batch: the request -> `accepted`, any other pending request between the pair -> `accepted` too
 * (simultaneous crossing requests), and the friendships row — inserted only if this batch really accepted the request.
 * After commit: `friend.request_accepted{request}` + `friend.added{friend: accepter}` to the sender,
 * `friend.added{friend: sender}` to the accepter, inbox `friend_accepted` (no alert) to the sender.
 * 404 when the request is not (any longer) pending between a and b.
 */
export async function makeFriends(rt: Runtime, a: string, b: string, requestId: string): Promise<void> {
  const db = rt.env.DB;
  const req = await first<FriendRequestRow>(
    db,
    `SELECT * FROM friend_requests
      WHERE id = ?1 AND status = 'pending'
        AND ((from_user_id = ?2 AND to_user_id = ?3) OR (from_user_id = ?3 AND to_user_id = ?2))`,
    requestId,
    a,
    b,
  );
  if (req === null) throw err.notFound("friend request");
  const senderId = req.from_user_id;
  const accepterId = req.to_user_id;
  const [lo, hi] = friendPair(senderId, accepterId);
  const t = now();

  const [accepted] = await batch(db, [
    stmt(db, "UPDATE friend_requests SET status = 'accepted', responded_at = ?2 WHERE id = ?1 AND status = 'pending'", requestId, t),
    stmt(
      db,
      `UPDATE friend_requests SET status = 'accepted', responded_at = ?3
        WHERE status = 'pending' AND id <> ?4
          AND ((from_user_id = ?1 AND to_user_id = ?2) OR (from_user_id = ?2 AND to_user_id = ?1))
          AND EXISTS (SELECT 1 FROM friend_requests r WHERE r.id = ?4 AND r.status = 'accepted' AND r.responded_at = ?3)`,
      lo,
      hi,
      t,
      requestId,
    ),
    stmt(
      db,
      `INSERT INTO friendships (user_a, user_b, created_at)
       SELECT ?1, ?2, ?3
        WHERE EXISTS (SELECT 1 FROM friend_requests r WHERE r.id = ?4 AND r.status = 'accepted' AND r.responded_at = ?3)
       ON CONFLICT (user_a, user_b) DO NOTHING`,
      lo,
      hi,
      t,
      requestId,
    ),
  ]);
  if (accepted!.meta.changes === 0) throw err.notFound("friend request"); // accepted/declined/canceled meanwhile

  const friendship = await first<FriendshipRow>(db, "SELECT * FROM friendships WHERE user_a = ?1 AND user_b = ?2", lo, hi);
  const since = friendship?.created_at ?? t;
  const [[request], friends] = await Promise.all([
    serializeFriendRequests(db, [{ ...req, status: "accepted", responded_at: t }]),
    serializeFriends(db, [
      { user_id: accepterId, created_at: since },
      { user_id: senderId, created_at: since },
    ]),
  ]);
  const asAccepter = friends.find((f) => f.user.id === accepterId);
  const asSender = friends.find((f) => f.user.id === senderId);

  const toSender: Array<{ type: "friend.request_accepted" | "friend.added"; data: unknown }> = [];
  if (request) toSender.push({ type: "friend.request_accepted", data: { request } });
  if (asAccepter) toSender.push({ type: "friend.added", data: { friend: asAccepter } });
  await Promise.all([
    publishEvents(rt, { users: [senderId] }, toSender),
    asSender ? publishEvent(rt, { users: [accepterId] }, "friend.added", { friend: asSender }) : Promise.resolve(),
    notify(rt, {
      userId: senderId,
      type: "friend_accepted",
      actorId: accepterId,
      subject: { type: "user", id: accepterId },
      preview: "",
      dedupeKey: `friend_accepted:${requestId}`,
      alert: null,
    }),
  ]);
}

/**
 * Ends the friendship between a and b (no-op when there is none). After commit: `friend.removed{user_id: other}` to
 * each side, except `opts.silentTo` (block cleanup tells only the blocker — events.md 4.5).
 */
export async function unfriend(rt: Runtime, a: string, b: string, opts: { silentTo?: string } = {}): Promise<void> {
  const [lo, hi] = friendPair(a, b);
  const [res] = await batch(rt.env.DB, [stmt(rt.env.DB, "DELETE FROM friendships WHERE user_a = ?1 AND user_b = ?2", lo, hi)]);
  if (res!.meta.changes === 0) return;
  await Promise.all(
    [
      { to: a, other: b },
      { to: b, other: a },
    ]
      .filter((x) => x.to !== opts.silentTo)
      .map((x) => publishEvent(rt, { users: [x.to] }, "friend.removed", { user_id: x.other })),
  );
}

/**
 * A blocks B (api.md 3.2), all in one batch: the `blocks` row (INSERT OR IGNORE), the friendship, pending friend
 * requests both ways -> `canceled`, follows both ways (pending included) deleted — follower/following counters drop
 * only for rows that were `active` (guard before the DELETE, data-model 7) — and pending direct space invites both
 * ways -> `canceled`.
 * After commit, only the blocker hears about it (api.md 5.4, events.md 4.5): `block.updated{blocked:true}`,
 * `friend.removed` if they were friends, and `friend.request_removed` / `space.invite_removed` `{canceled}` for what
 * the cleanup canceled. Nothing goes to B.
 */
export async function applyBlock(rt: Runtime, blockerId: string, blockedId: string): Promise<void> {
  const db = rt.env.DB;
  const [lo, hi] = friendPair(blockerId, blockedId);
  const t = now();
  const activeFollow = "EXISTS (SELECT 1 FROM follows WHERE follower_id = ?1 AND followee_id = ?2 AND status = 'active')";
  const results = await batch(db, [
    stmt(db, "INSERT OR IGNORE INTO blocks (blocker_id, blocked_id, created_at) VALUES (?1, ?2, ?3)", blockerId, blockedId, t),
    stmt(db, "DELETE FROM friendships WHERE user_a = ?1 AND user_b = ?2 RETURNING user_a", lo, hi),
    stmt(
      db,
      `UPDATE friend_requests SET status = 'canceled', responded_at = ?3
        WHERE status = 'pending' AND ((from_user_id = ?1 AND to_user_id = ?2) OR (from_user_id = ?2 AND to_user_id = ?1))
       RETURNING id`,
      blockerId,
      blockedId,
      t,
    ),
    // counters first (guarded on the active row), then the DELETE — for each direction
    stmt(db, `UPDATE users SET following_count = MAX(following_count - 1, 0) WHERE id = ?1 AND ${activeFollow}`, blockerId, blockedId),
    stmt(db, `UPDATE users SET follower_count = MAX(follower_count - 1, 0) WHERE id = ?2 AND ${activeFollow}`, blockerId, blockedId),
    stmt(db, `UPDATE users SET following_count = MAX(following_count - 1, 0) WHERE id = ?1 AND ${activeFollow}`, blockedId, blockerId),
    stmt(db, `UPDATE users SET follower_count = MAX(follower_count - 1, 0) WHERE id = ?2 AND ${activeFollow}`, blockedId, blockerId),
    stmt(
      db,
      "DELETE FROM follows WHERE (follower_id = ?1 AND followee_id = ?2) OR (follower_id = ?2 AND followee_id = ?1)",
      blockerId,
      blockedId,
    ),
    stmt(
      db,
      `UPDATE space_direct_invites SET status = 'canceled', responded_at = ?3
        WHERE status = 'pending' AND ((inviter_id = ?1 AND invitee_id = ?2) OR (inviter_id = ?2 AND invitee_id = ?1))
       RETURNING id`,
      blockerId,
      blockedId,
      t,
    ),
  ]);
  const wasFriend = results[1]!.results.length > 0;
  const canceledRequests = (results[2]!.results as Array<{ id: string }>).map((r) => r.id);
  const canceledInvites = (results[8]!.results as Array<{ id: string }>).map((r) => r.id);

  const events: Array<{ type: "block.updated" | "friend.removed" | "friend.request_removed" | "space.invite_removed"; data: unknown }> = [
    { type: "block.updated", data: { user_id: blockedId, blocked: true } },
  ];
  if (wasFriend) events.push({ type: "friend.removed", data: { user_id: blockedId } });
  for (const id of canceledRequests) events.push({ type: "friend.request_removed", data: { request_id: id, reason: "canceled" } });
  for (const id of canceledInvites) events.push({ type: "space.invite_removed", data: { invite_id: id, reason: "canceled" } });
  await publishEvents(rt, { users: [blockerId] }, events);
}

/** Lifts A's block on B (no-op when there is none). Relations are not restored (api.md 3.2). `block.updated{blocked:false}` to A. */
export async function removeBlock(rt: Runtime, blockerId: string, blockedId: string): Promise<void> {
  await batch(rt.env.DB, [stmt(rt.env.DB, "DELETE FROM blocks WHERE blocker_id = ?1 AND blocked_id = ?2", blockerId, blockedId)]);
  await publishEvent(rt, { users: [blockerId] }, "block.updated", { user_id: blockedId, blocked: false });
}

/**
 * `userId` went public (api.md 3.3): every pending follow request to them becomes `active`, in one batch with the
 * counter statements placed before the status change (plan 02 Task 3 rule) so the counted set and the switched set are
 * the same rows: each requester's following_count + 1 (one statement over the subquery), my follower_count + n.
 * After commit, inbox `follow_accepted` (actor = userId, alert kind `social`) to each requester actually approved.
 */
export async function approveAllPendingFollows(rt: Runtime, userId: string): Promise<void> {
  const db = rt.env.DB;
  const any = await first(db, "SELECT 1 AS x FROM follows WHERE followee_id = ?1 AND status = 'pending' LIMIT 1", userId);
  if (any === null) return;
  const t = now();
  const results = await batch(db, [
    stmt(
      db,
      "UPDATE users SET following_count = following_count + 1 WHERE id IN (SELECT follower_id FROM follows WHERE followee_id = ?1 AND status = 'pending')",
      userId,
    ),
    stmt(
      db,
      "UPDATE users SET follower_count = follower_count + (SELECT COUNT(*) FROM follows WHERE followee_id = ?1 AND status = 'pending') WHERE id = ?1",
      userId,
    ),
    stmt(db, "UPDATE follows SET status = 'active', accepted_at = ?2 WHERE followee_id = ?1 AND status = 'pending' RETURNING follower_id", userId, t),
  ]);
  const approved = (results[2]!.results as Array<{ follower_id: string }>).map((r) => r.follower_id);
  if (approved.length === 0) return;

  const name = (await loadUserSummaries(db, [userId])).get(userId)?.display_name ?? "";
  await notifyMany(
    rt,
    approved.map((followerId) => ({
      userId: followerId,
      type: "follow_accepted" as const,
      actorId: userId,
      subject: { type: "user" as const, id: userId },
      preview: "",
      alert: {
        kind: "social" as const,
        title: `${name} 님이 팔로우 요청을 수락했어요`,
        body: "",
        target: { type: "user" as const, id: userId, channel_id: null, message_id: null, thread_root_id: null, post_id: null, space_id: null },
      },
    })),
  );
}
