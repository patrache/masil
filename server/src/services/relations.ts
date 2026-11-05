// Relation services (server-architecture.md 4.14 relations.ts). Plan 01 owns everything here except `follow`
// (plan 02). Task 17: makeFriends / unfriend. Each writes in one db.batch, then publishes (server-architecture.md 3.5).
import type { Runtime } from "../env";
import { batch, first, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { notify } from "../lib/notify";
import { publishEvent, publishEvents } from "../lib/realtime";
import { serializeFriendRequests, serializeFriends } from "../lib/serialize";
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
