// Friends and friend requests (api.md 5.3). requireAuth is a per-route argument (plan 01 Global Constraints).
// Writes commit in one db statement/batch first, then publish (server-architecture.md 3.5).
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, isSuspendedNow, requireAuth, resolveUserRef } from "../lib/auth";
import { all, decodeCursor, encodeCursor, first, parseLimit } from "../lib/db";
import { err } from "../lib/errors";
import { isId, newId } from "../lib/ids";
import { notify } from "../lib/notify";
import { areFriends, isBlockedEither, suspendedNowSql } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { publishEvent } from "../lib/realtime";
import { loadUserSummaries, serializeFriendRequests, serializeFriends } from "../lib/serialize";
import { now } from "../lib/time";
import { vJson, vQuery, zUserRef } from "../lib/validate";
import { acceptPendingRequest, insertFriendRequest, makeFriends, unfriend } from "../services/relations";
import type { FriendRequestRow, UserRow } from "../types/rows";

const PENDING_OUTGOING_MAX = 100; // api.md 5.3: 422 limit_exceeded pending_friend_requests
const REQUEST_LIST_MAX = 200;
/** POST /v1/friends/requests: decision rounds when a crossing request keeps vanishing before its accept batch. */
const CROSSING_ROUNDS = 3;

const RequestBody = z.object({ user: zUserRef });
const RequestsQuery = z.object({ direction: z.enum(["incoming", "outgoing"]) });
const FriendsQuery = z.object({ cursor: z.string().optional(), limit: z.string().optional() });

/** Friend list sort: online/away/dnd (rank 0) before offline (rank 1), then display_name, then user id. */
interface FriendSortRow {
  user_id: string;
  created_at: number;
  display_name: string;
  rk: 0 | 1;
}

/** Cursor key "<rank>:<display_name>" (opaque to clients; api.md 0.6). */
function parseFriendKey(k: string | number): { rk: number; name: string } {
  const m = typeof k === "string" ? /^([01]):([\s\S]*)$/.exec(k) : null;
  if (!m) throw err.invalid({ cursor: "invalid" });
  return { rk: Number(m[1]), name: m[2]! };
}

/** A request id path param that is not a ULID can name no request: 404 (never 400). */
function requestIdParam(raw: string): string {
  if (!isId(raw)) throw err.notFound("friend request");
  return raw;
}

async function friendOf(db: D1Database, me: string, other: string) {
  const row = await first<{ created_at: number }>(
    db,
    "SELECT created_at FROM friendships WHERE user_a = MIN(?1, ?2) AND user_b = MAX(?1, ?2)",
    me,
    other,
  );
  const [friend] = await serializeFriends(db, [{ user_id: other, created_at: row?.created_at ?? now() }]);
  if (!friend) throw err.notFound("user");
  return friend;
}

export const friendRoutes = new Hono<AppEnv>();

// Order: on (rk 0) before offline (rk 1), then display_name, user_id. A friend suspended right now ranks offline, as its
// Friend.presence reads (api.md 3.9) — the raw user_presence.status (a manual dnd holds with no socket) would otherwise
// show through the order and next_cursor.
friendRoutes.get("/friends", requireAuth, vQuery(FriendsQuery), async (c) => {
  const me = getAuth(c).userId;
  const q = c.req.valid("query");
  const n = parseLimit(q.limit, 30, 100);
  const cur = decodeCursor(q.cursor);
  const key = cur === null ? null : parseFriendKey(cur.k);
  const rows = await all<FriendSortRow>(
    c.env.DB,
    `WITH f AS (
       SELECT CASE WHEN fr.user_a = ?1 THEN fr.user_b ELSE fr.user_a END AS user_id, fr.created_at
         FROM friendships fr WHERE fr.user_a = ?1 OR fr.user_b = ?1
     ), r AS (
       SELECT f.user_id, f.created_at, u.display_name,
              CASE WHEN COALESCE(p.status, 'offline') = 'offline' OR ${suspendedNowSql("f.user_id", "?6")} THEN 1 ELSE 0 END AS rk
         FROM f JOIN users u ON u.id = f.user_id
         LEFT JOIN user_presence p ON p.user_id = f.user_id
     )
     SELECT * FROM r
      WHERE ?2 IS NULL
         OR rk > ?2
         OR (rk = ?2 AND (display_name > ?3 OR (display_name = ?3 AND user_id > ?4)))
      ORDER BY rk, display_name, user_id
      LIMIT ?5`,
    me,
    key?.rk ?? null,
    key?.name ?? null,
    cur?.i ?? null,
    n + 1,
    now(),
  );
  const page = rows.slice(0, n);
  const last = page[page.length - 1];
  const next_cursor = rows.length > n && last ? encodeCursor({ k: `${last.rk}:${last.display_name}`, i: last.user_id }) : null;
  return c.json({ items: await serializeFriends(c.env.DB, page), next_cursor });
});

friendRoutes.delete("/friends/:user", requireAuth, async (c) => {
  const auth = getAuth(c);
  const other = await resolveUserRef(c.env.DB, c.req.param("user"), auth);
  if (other.id === auth.userId || !(await areFriends(c.env.DB, auth.userId, other.id))) throw err.notFound("friend");
  await unfriend(rt(c), auth.userId, other.id);
  return c.body(null, 204);
});

/** Pending only, newest first, at most 200. */
friendRoutes.get("/friends/requests", requireAuth, vQuery(RequestsQuery), async (c) => {
  const me = getAuth(c).userId;
  const col = c.req.valid("query").direction === "incoming" ? "to_user_id" : "from_user_id";
  const rows = await all<FriendRequestRow>(
    c.env.DB,
    `SELECT * FROM friend_requests WHERE ${col} = ?1 AND status = 'pending' ORDER BY created_at DESC, id DESC LIMIT ?2`,
    me,
    REQUEST_LIST_MAX,
  );
  return c.json({ items: await serializeFriendRequests(c.env.DB, rows) });
});

friendRoutes.post("/friends/requests", requireAuth, limit("RL_SOCIAL"), vJson(RequestBody), async (c) => {
  const auth = getAuth(c);
  const me = auth.userId;
  const db = c.env.DB;
  const target = await resolveUserRef(db, c.req.valid("json").user, auth);
  if (target.id === me) throw err.conflict("conflict", "self");
  const t = now();
  if (isSuspendedNow(target, t) || (await isBlockedEither(db, me, target.id))) throw err.forbidden("action_not_allowed");

  // Crossing request: accept theirs instead of opening a second one (api.md 5.3). If theirs is no longer pending at the
  // accept (canceled / declined / accepted elsewhere after the read — the accept batch reads that itself), the answer
  // is the one the request would have got had that landed before the read: decide again from the start (pending item
  // 10-a; not 404, which this endpoint does not answer). Each round is a whole fresh decision whose own writes read
  // their own reasons, so the answer is always one a serial order gives. The last round opens a request even if yet
  // another crossing one appeared (the pair can still accept either; makeFriends accepts crossing ones together).
  for (let round = 1; ; round++) {
    if (await areFriends(db, me, target.id)) throw err.conflict("already_exists", "already_friends");
    const pending = await all<FriendRequestRow>(
      db,
      `SELECT * FROM friend_requests
        WHERE status = 'pending' AND ((from_user_id = ?1 AND to_user_id = ?2) OR (from_user_id = ?2 AND to_user_id = ?1))
        ORDER BY created_at, id`,
      me,
      target.id,
    );
    if (pending.some((r) => r.from_user_id === me)) throw err.conflict("already_exists", "pending");
    const theirs = round < CROSSING_ROUNDS ? pending.find((r) => r.from_user_id === target.id) : undefined;
    if (!theirs) break;
    if ((await acceptPendingRequest(rt(c), me, target.id, theirs.id)) === "gone") continue;
    const [[request], friend] = await Promise.all([
      serializeFriendRequests(db, [{ ...theirs, status: "accepted", responded_at: t }]),
      friendOf(db, me, target.id),
    ]);
    return c.json({ request, friend }, 200);
  }

  const row: FriendRequestRow = { id: newId(t), from_user_id: me, to_user_id: target.id, status: "pending", created_at: t, responded_at: null };
  await insertFriendRequest(db, row, PENDING_OUTGOING_MAX);

  const [[request], users] = await Promise.all([serializeFriendRequests(db, [row]), loadUserSummaries(db, [me])]);
  const name = users.get(me)?.display_name ?? auth.handle;
  await Promise.all([
    request ? publishEvent(rt(c), { users: [target.id] }, "friend.request_received", { request }) : Promise.resolve(),
    notify(rt(c), {
      userId: target.id,
      type: "friend_request",
      actorId: me,
      subject: { type: "friend_request", id: row.id },
      preview: "",
      dedupeKey: `friend_request:${row.id}`,
      alert: {
        kind: "friend_request",
        title: `${name} 님의 친구 요청`,
        body: "",
        target: { type: "user", id: me, channel_id: null, message_id: null, thread_root_id: null, post_id: null, space_id: null },
      },
    }),
  ]);
  return c.json({ request, friend: null }, 201);
});

/** Recipient only (anyone else: 404). */
friendRoutes.post("/friends/requests/:request_id/accept", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const id = requestIdParam(c.req.param("request_id"));
  const req = await first<FriendRequestRow>(db, "SELECT * FROM friend_requests WHERE id = ?1 AND to_user_id = ?2 AND status = 'pending'", id, me);
  if (req === null) throw err.notFound("friend request");
  // api.md 3.2 / 3.9: no friendship across a block either way or with a sender suspended now (makeFriends re-checks
  // both inside its batch)
  const sender = await first<Pick<UserRow, "suspended_at" | "suspended_until">>(
    db,
    "SELECT suspended_at, suspended_until FROM users WHERE id = ?1",
    req.from_user_id,
  );
  if ((sender !== null && isSuspendedNow(sender, now())) || (await isBlockedEither(db, me, req.from_user_id))) {
    throw err.forbidden("action_not_allowed");
  }
  await makeFriends(rt(c), me, req.from_user_id, id);
  return c.json({ friend: await friendOf(db, me, req.from_user_id) });
});

/** Recipient only. friend.request_removed{declined} to both; the sender gets no inbox row. */
friendRoutes.post("/friends/requests/:request_id/decline", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const id = requestIdParam(c.req.param("request_id"));
  const row = await first<FriendRequestRow>(
    c.env.DB,
    "UPDATE friend_requests SET status = 'declined', responded_at = ?3 WHERE id = ?1 AND to_user_id = ?2 AND status = 'pending' RETURNING *",
    id,
    me,
    now(),
  );
  if (row === null) throw err.notFound("friend request");
  await publishEvent(rt(c), { users: [row.from_user_id, row.to_user_id] }, "friend.request_removed", { request_id: row.id, reason: "declined" });
  return c.body(null, 204);
});

/** Sender only. friend.request_removed{canceled} to both. */
friendRoutes.delete("/friends/requests/:request_id", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const id = requestIdParam(c.req.param("request_id"));
  const row = await first<FriendRequestRow>(
    c.env.DB,
    "UPDATE friend_requests SET status = 'canceled', responded_at = ?3 WHERE id = ?1 AND from_user_id = ?2 AND status = 'pending' RETURNING *",
    id,
    me,
    now(),
  );
  if (row === null) throw err.notFound("friend request");
  await publishEvent(rt(c), { users: [row.from_user_id, row.to_user_id] }, "friend.request_removed", { request_id: row.id, reason: "canceled" });
  return c.body(null, 204);
});
