// Relation services (server-architecture.md 4.14 relations.ts). Plan 01 owns everything here except `follow`
// (plan 02). Task 17: makeFriends / unfriend. Task 18: applyBlock / removeBlock / approveAllPendingFollows.
// Each writes in one db.batch, then publishes (server-architecture.md 3.5).
import type { Runtime } from "../env";
import { batch, first, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { isSuspendedNow } from "../lib/auth";
import { notify, notifyMany } from "../lib/notify";
import { blockedEitherSql, isBlockedEither, suspendedNowSql } from "../lib/permissions";
import { publishEvent, publishEvents } from "../lib/realtime";
import { loadUserSummaries, serializeFriendRequests, serializeFriends } from "../lib/serialize";
import { now } from "../lib/time";
import type { FollowRow, FriendRequestRow, FriendshipRow, UserRow } from "../types/rows";

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
 * 404 when the request is not (any longer) pending between a and b; 403 action_not_allowed when a block either way is
 * in place at the accept, or the sender (the accepter's other side) is suspended now (api.md 3.2 / 3.9) — checked in
 * every write statement of the batch (pending item 10), so a block / suspension committing after the caller's
 * pre-check writes nothing, even for a second accept of the same request sharing the first one's now(). The 403 / 404
 * choice is read in the same batch.
 */
export async function makeFriends(rt: Runtime, a: string, b: string, requestId: string): Promise<void> {
  if ((await acceptPendingRequest(rt, a, b, requestId)) === "gone") throw err.notFound("friend request");
}

/**
 * makeFriends without the 404: "gone" when the request is not (any longer) pending between a and b — at the first read
 * or, read in the accept batch itself, at the accept (accepted / declined / canceled meanwhile); the 403 still throws.
 * For a caller whose answer to "gone" is not 404 (the crossing auto-accept of POST /v1/friends/requests).
 */
export async function acceptPendingRequest(rt: Runtime, a: string, b: string, requestId: string): Promise<"accepted" | "gone"> {
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
  if (req === null) return "gone";
  const senderId = req.from_user_id;
  const accepterId = req.to_user_id;
  const [lo, hi] = friendPair(senderId, accepterId);
  const t = now();

  // Every write statement carries the block / suspension rule itself (pending item 10). Statements 2 and 3 are also
  // gated on "the request was accepted at t", but that token alone is not enough: two accepts of the same request can
  // share t (Date.now() in Workers advances only on I/O), and a block committing between them deletes the friendship
  // while leaving the already-accepted request alone — so the second accept's token is still true under the block.
  const allowed = (sender: string, at: string) =>
    `NOT ${blockedEitherSql("?1", "?2")} AND NOT ${suspendedNowSql(sender, at)}`;
  const results = await batch(db, [
    // ?1 sender, ?2 accepter, ?3 t, ?4 request id
    stmt(
      db,
      `UPDATE friend_requests SET status = 'accepted', responded_at = ?3
        WHERE id = ?4 AND status = 'pending' AND ${allowed("?1", "?3")}`,
      senderId,
      accepterId,
      t,
      requestId,
    ),
    // ?1 lo, ?2 hi, ?3 t, ?4 request id, ?5 sender — crossing requests between the pair
    stmt(
      db,
      `UPDATE friend_requests SET status = 'accepted', responded_at = ?3
        WHERE status = 'pending' AND id <> ?4
          AND ((from_user_id = ?1 AND to_user_id = ?2) OR (from_user_id = ?2 AND to_user_id = ?1))
          AND EXISTS (SELECT 1 FROM friend_requests r WHERE r.id = ?4 AND r.status = 'accepted' AND r.responded_at = ?3)
          AND ${allowed("?5", "?3")}`,
      lo,
      hi,
      t,
      requestId,
      senderId,
    ),
    // same params as the crossing UPDATE
    stmt(
      db,
      `INSERT INTO friendships (user_a, user_b, created_at)
       SELECT ?1, ?2, ?3
        WHERE EXISTS (SELECT 1 FROM friend_requests r WHERE r.id = ?4 AND r.status = 'accepted' AND r.responded_at = ?3)
          AND ${allowed("?5", "?3")}
       ON CONFLICT (user_a, user_b) DO NOTHING`,
      lo,
      hi,
      t,
      requestId,
      senderId,
    ),
    // why statement 1 may have written nothing, read inside the same transaction (a lift right after cannot change it)
    stmt(db, `SELECT ${blockedEitherSql("?1", "?2")} AS blocked, ${suspendedNowSql("?1", "?3")} AS suspended`, senderId, accepterId, t),
  ]);
  if (results[0]!.meta.changes === 0) {
    // a block landed after the pre-check (its cleanup may also have canceled the request) or the sender is suspended
    // -> 403 like the pre-check; otherwise the request was accepted / declined / canceled meanwhile -> 404
    const why = (results[3]!.results as Array<{ blocked: number; suspended: number }>)[0];
    if (why?.blocked || why?.suspended) throw err.forbidden("action_not_allowed");
    return "gone";
  }

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
  return "accepted";
}

/**
 * Inserts pending friend request `row` (from_user_id -> to_user_id). The pending-outgoing cap is checked in the INSERT
 * itself, so a concurrent request cannot slip past `maxPending`; so are the block rule (either way) and the target's
 * suspension (pending item 10: the caller's pre-check is a plain read, and a block committing after it would leave a
 * pending request its cleanup is meant to cancel). When nothing went in: 403 action_not_allowed (blocked / target
 * suspended now), 409 already_exists `pending` (the sender already has one pending to this user), else 422
 * limit_exceeded `pending_friend_requests` — the reason read in the INSERT's own batch.
 */
export async function insertFriendRequest(db: D1Database, row: FriendRequestRow, maxPending: number): Promise<void> {
  // The reason for a 0-row INSERT is read in the same batch (one transaction), so a block / suspension lifted right
  // after it cannot turn the 403 into a 409 / 422.
  const [ins, reason] = await batch(db, [
    stmt(
      db,
      `INSERT INTO friend_requests (id, from_user_id, to_user_id, status, created_at)
       SELECT ?1, ?2, ?3, 'pending', ?4
        WHERE (SELECT COUNT(*) FROM friend_requests WHERE from_user_id = ?2 AND status = 'pending') < ?5
          AND NOT ${blockedEitherSql("?2", "?3")} AND NOT ${suspendedNowSql("?3", "?4")}
       ON CONFLICT DO NOTHING
       RETURNING id`,
      row.id,
      row.from_user_id,
      row.to_user_id,
      row.created_at,
      maxPending,
    ),
    stmt(
      db,
      `SELECT ${blockedEitherSql("?1", "?2")} AS blocked, ${suspendedNowSql("?2", "?3")} AS suspended,
              EXISTS (SELECT 1 FROM friend_requests WHERE from_user_id = ?1 AND to_user_id = ?2 AND status = 'pending') AS dup`,
      row.from_user_id,
      row.to_user_id,
      row.created_at,
    ),
  ]);
  if (ins!.results.length > 0) return;
  const why = (reason!.results as Array<{ blocked: number; suspended: number; dup: number }>)[0];
  if (why?.blocked || why?.suspended) throw err.forbidden("action_not_allowed");
  if (why?.dup) throw err.conflict("already_exists", "pending");
  throw err.limit("pending_friend_requests", maxPending);
}

/**
 * Ends the friendship between a and b, in one batch with its cleanup (D3, api.md 3.5): the friendships row DELETE and
 * every pending direct space invite between the two, either way -> `canceled` (a direct invite needs the invitee to be
 * the inviter's friend, so none may outlive the friendship; joinSpace re-checks it too). No-op when there is neither.
 * After commit: `friend.removed{user_id: other}` to each side if they were friends, and per canceled invite
 * `space.invite_removed{invite_id, reason:"canceled"}` to its invitee + inviter (events.md) — except `opts.silentTo`
 * (block cleanup tells only the blocker — events.md 4.5).
 */
export async function unfriend(rt: Runtime, a: string, b: string, opts: { silentTo?: string } = {}): Promise<void> {
  const db = rt.env.DB;
  const [lo, hi] = friendPair(a, b);
  const [res, canceled] = await batch(db, [
    stmt(db, "DELETE FROM friendships WHERE user_a = ?1 AND user_b = ?2 RETURNING user_a", lo, hi),
    stmt(
      db,
      `UPDATE space_direct_invites SET status = 'canceled', responded_at = ?3
        WHERE status = 'pending' AND ((inviter_id = ?1 AND invitee_id = ?2) OR (inviter_id = ?2 AND invitee_id = ?1))
       RETURNING id`,
      a,
      b,
      now(),
    ),
  ]);
  const wasFriend = res!.results.length > 0;
  const inviteIds = (canceled!.results as Array<{ id: string }>).map((r) => r.id);
  const to = [
    { to: a, other: b },
    { to: b, other: a },
  ].filter((x) => x.to !== opts.silentTo);
  await Promise.all(
    to.map((x) =>
      publishEvents(rt, { users: [x.to] }, [
        ...(wasFriend ? [{ type: "friend.removed" as const, data: { user_id: x.other } }] : []),
        ...inviteIds.map((id) => ({ type: "space.invite_removed" as const, data: { invite_id: id, reason: "canceled" } })),
      ]),
    ),
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

/** Desktop-alert target pointing at a user profile. */
function userTarget(id: string) {
  return { type: "user" as const, id, channel_id: null, message_id: null, thread_root_id: null, post_id: null, space_id: null };
}

/**
 * `followerId` follows `followeeId` (api.md 5.5). Self -> 409 conflict `self`; followee suspended or a block either
 * way -> 403 action_not_allowed; an existing row (active or pending) is returned as is, with no notification.
 * A new row is `pending` for a private followee, else `active` (accepted_at = now). The status is decided inside the
 * batch from the followee's current is_private, and the counter statements come before the INSERT, guarded so they
 * move only when this batch really inserts an active row (data-model 7: counters count active rows only).
 * The block / suspension rule is in the INSERT and the counter guards too (pending item 10): one landing between the
 * pre-check and the batch writes nothing -> 403 action_not_allowed, while a concurrent follow of the same pair that
 * won the race is reported as its state (idempotent) — both read in the INSERT's own batch (pending item 10-a).
 * After commit: inbox `follow` (dedupe `follow:<followee>:<follower>`) or `follow_request`
 * (dedupe `follow_request:<followee>:<follower>`), alert kind `social`.
 */
export async function follow(rt: Runtime, followerId: string, followeeId: string): Promise<"active" | "pending"> {
  const db = rt.env.DB;
  if (followerId === followeeId) throw err.conflict("conflict", "self");
  const t = now();
  const followee = await first<UserRow>(db, "SELECT * FROM users WHERE id = ?1", followeeId);
  if (followee === null) throw err.notFound("user");
  if (isSuspendedNow(followee, t) || (await isBlockedEither(db, followerId, followeeId))) throw err.forbidden("action_not_allowed");

  const existing = await first<Pick<FollowRow, "status">>(db, "SELECT status FROM follows WHERE follower_id = ?1 AND followee_id = ?2", followerId, followeeId);
  if (existing !== null) return existing.status;

  // The block / suspension rule is part of every write (pending item 10): the pre-check above is a plain read, and a
  // block committing after it would otherwise leave the follow (and the counter bump) that its cleanup is meant to
  // remove. Params: ?1 follower, ?2 followee, ?3 now.
  const allowed = `NOT ${blockedEitherSql("?1", "?2")} AND NOT ${suspendedNowSql("?2", "?3")}`;
  // "No row for the pair yet" covers the active guard (NOT EXISTS ... status='active') and also keeps the counters
  // still when a concurrent request already inserted a row this INSERT will skip.
  const willInsertActive = `EXISTS (SELECT 1 FROM users fe WHERE fe.id = ?2 AND fe.is_private = 0)
    AND NOT EXISTS (SELECT 1 FROM follows WHERE follower_id = ?1 AND followee_id = ?2)
    AND ${allowed}`;
  const results = await batch(db, [
    stmt(db, `UPDATE users SET following_count = following_count + 1 WHERE id = ?1 AND ${willInsertActive}`, followerId, followeeId, t),
    stmt(db, `UPDATE users SET follower_count = follower_count + 1 WHERE id = ?2 AND ${willInsertActive}`, followerId, followeeId, t),
    stmt(
      db,
      `INSERT INTO follows (follower_id, followee_id, status, created_at, accepted_at)
       SELECT ?1, ?2, CASE WHEN u.is_private = 1 THEN 'pending' ELSE 'active' END, ?3, CASE WHEN u.is_private = 1 THEN NULL ELSE ?3 END
         FROM users u WHERE u.id = ?2 AND ${allowed}
       ON CONFLICT (follower_id, followee_id) DO NOTHING
       RETURNING status`,
      followerId,
      followeeId,
      t,
    ),
    // why the INSERT may have written nothing, read inside the same transaction (pending item 10-a: a block lifted or
    // landing right after the batch cannot change the answer)
    stmt(
      db,
      `SELECT (SELECT status FROM follows WHERE follower_id = ?1 AND followee_id = ?2) AS status,
              ${blockedEitherSql("?1", "?2")} AS blocked, ${suspendedNowSql("?2", "?3")} AS suspended`,
      followerId,
      followeeId,
      t,
    ),
  ]);
  const inserted = (results[2]!.results as Array<Pick<FollowRow, "status">>)[0];
  if (inserted === undefined) {
    // Nothing went in: either a block / suspension was in place at the INSERT (403, whatever the pre-check said), or a
    // concurrent follow of the same pair won the race (idempotent: report the row the INSERT ran into).
    const current = (results[3]!.results as Array<{ status: FollowRow["status"] | null; blocked: number; suspended: number }>)[0];
    if (current === undefined || current.blocked || current.suspended || current.status === null) throw err.forbidden("action_not_allowed");
    return current.status;
  }

  const name = (await loadUserSummaries(db, [followerId])).get(followerId)?.display_name ?? "";
  const active = inserted.status === "active";
  await notify(rt, {
    userId: followeeId,
    type: active ? "follow" : "follow_request",
    actorId: followerId,
    subject: { type: "user", id: followerId },
    preview: "",
    dedupeKey: `${active ? "follow" : "follow_request"}:${followeeId}:${followerId}`,
    alert: { kind: "social", title: active ? `${name} 님이 팔로우했어요` : `${name} 님의 팔로우 요청`, body: "", target: userTarget(followerId) },
  });
  return inserted.status;
}
