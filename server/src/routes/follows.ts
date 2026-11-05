// Follows (api.md 5.5): PUT|DELETE /users/:user/follow, DELETE /users/:user/follower, GET /me/follow-requests,
// POST /me/follow-requests/:user/accept|decline. /users and /me are shared prefixes, so requireAuth is a per-route
// argument (plan 02 Global Constraints). Counters move only with `active` rows (data-model 7): every decrement is
// guarded on the active row and placed before the DELETE, the accept increment is guarded on the pending row and
// placed before the status change — all in one batch.
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth, resolveUserRef } from "../lib/auth";
import { all, batch, decodeCursor, encodeCursor, parseLimit, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { notify } from "../lib/notify";
import { limit } from "../lib/ratelimit";
import { loadUserSummaries } from "../lib/serialize";
import { iso, now } from "../lib/time";
import { vQuery } from "../lib/validate";
import { follow } from "../services/relations";
import type { FollowRow } from "../types/rows";

const PageQuery = z.object({ cursor: z.string().optional(), limit: z.string().optional() });

const ACTIVE_ROW = "EXISTS (SELECT 1 FROM follows WHERE follower_id = ?1 AND followee_id = ?2 AND status = 'active')";
const PENDING_ROW = "EXISTS (SELECT 1 FROM follows WHERE follower_id = ?1 AND followee_id = ?2 AND status = 'pending')";

/**
 * Deletes the follow row follower -> followee (active or pending; `pendingOnly` restricts it to a pending request).
 * Guarded decrements first, so a pending row never moves the counters. Returns whether a row was deleted.
 */
async function deleteFollow(db: D1Database, followerId: string, followeeId: string, pendingOnly = false): Promise<boolean> {
  const results = await batch(db, [
    stmt(db, `UPDATE users SET following_count = MAX(following_count - 1, 0) WHERE id = ?1 AND ${ACTIVE_ROW}`, followerId, followeeId),
    stmt(db, `UPDATE users SET follower_count = MAX(follower_count - 1, 0) WHERE id = ?2 AND ${ACTIVE_ROW}`, followerId, followeeId),
    stmt(
      db,
      `DELETE FROM follows WHERE follower_id = ?1 AND followee_id = ?2${pendingOnly ? " AND status = 'pending'" : ""} RETURNING follower_id`,
      followerId,
      followeeId,
    ),
  ]);
  return results[2]!.results.length > 0;
}

export const followRoutes = new Hono<AppEnv>();

followRoutes.put("/users/:user/follow", requireAuth, limit("RL_SOCIAL"), async (c) => {
  const auth = getAuth(c);
  const target = await resolveUserRef(c.env.DB, c.req.param("user"), auth);
  const state = await follow(rt(c), auth.userId, target.id);
  return c.json({ state });
});

/** Unfollow or cancel my pending request (idempotent: 204 when there is nothing to remove). */
followRoutes.delete("/users/:user/follow", requireAuth, async (c) => {
  const auth = getAuth(c);
  const target = await resolveUserRef(c.env.DB, c.req.param("user"), auth);
  if (target.id === auth.userId) throw err.conflict("conflict", "self");
  await deleteFollow(c.env.DB, auth.userId, target.id);
  return c.body(null, 204);
});

/** Remove someone who follows me, or reject their pending request (idempotent). */
followRoutes.delete("/users/:user/follower", requireAuth, async (c) => {
  const auth = getAuth(c);
  const target = await resolveUserRef(c.env.DB, c.req.param("user"), auth);
  if (target.id === auth.userId) throw err.conflict("conflict", "self");
  await deleteFollow(c.env.DB, target.id, auth.userId);
  return c.body(null, 204);
});

/** Pending requests to me, newest first; cursor {k: created_at, i: follower_id}. */
followRoutes.get("/me/follow-requests", requireAuth, vQuery(PageQuery), async (c) => {
  const me = getAuth(c).userId;
  const q = c.req.valid("query");
  const n = parseLimit(q.limit, 30, 100);
  const cur = decodeCursor(q.cursor);
  if (cur !== null && typeof cur.k !== "number") throw err.invalid({ cursor: "invalid" });
  const rows = await all<FollowRow>(
    c.env.DB,
    `SELECT * FROM follows
      WHERE followee_id = ?1 AND status = 'pending'
        AND (?2 IS NULL OR created_at < ?2 OR (created_at = ?2 AND follower_id < ?3))
      ORDER BY created_at DESC, follower_id DESC
      LIMIT ?4`,
    me,
    cur?.k ?? null,
    cur?.i ?? null,
    n + 1,
  );
  const page = rows.slice(0, n);
  const users = await loadUserSummaries(c.env.DB, page.map((r) => r.follower_id));
  const items = page.flatMap((r) => {
    const user = users.get(r.follower_id);
    return user ? [{ user, created_at: iso(r.created_at) }] : [];
  });
  const last = page[page.length - 1];
  const next_cursor = rows.length > n && last ? encodeCursor({ k: last.created_at, i: last.follower_id }) : null;
  return c.json({ items, next_cursor });
});

/** Pending only (else 404). Counters + status change in one batch, then inbox `follow_accepted` to the requester. */
followRoutes.post("/me/follow-requests/:user/accept", requireAuth, async (c) => {
  const auth = getAuth(c);
  const me = auth.userId;
  const db = c.env.DB;
  const requester = await resolveUserRef(db, c.req.param("user"), auth);
  const t = now();
  const results = await batch(db, [
    stmt(db, `UPDATE users SET following_count = following_count + 1 WHERE id = ?1 AND ${PENDING_ROW}`, requester.id, me),
    stmt(db, `UPDATE users SET follower_count = follower_count + 1 WHERE id = ?2 AND ${PENDING_ROW}`, requester.id, me),
    stmt(
      db,
      "UPDATE follows SET status = 'active', accepted_at = ?3 WHERE follower_id = ?1 AND followee_id = ?2 AND status = 'pending' RETURNING follower_id",
      requester.id,
      me,
      t,
    ),
  ]);
  if (results[2]!.results.length === 0) throw err.notFound("follow request");

  const name = (await loadUserSummaries(db, [me])).get(me)?.display_name ?? auth.handle;
  await notify(rt(c), {
    userId: requester.id,
    type: "follow_accepted",
    actorId: me,
    subject: { type: "user", id: me },
    preview: "",
    alert: {
      kind: "social",
      title: `${name} 님이 팔로우 요청을 수락했어요`,
      body: "",
      target: { type: "user", id: me, channel_id: null, message_id: null, thread_root_id: null, post_id: null, space_id: null },
    },
  });
  return c.body(null, 204);
});

/** Pending only (else 404). The requester is not told. */
followRoutes.post("/me/follow-requests/:user/decline", requireAuth, async (c) => {
  const auth = getAuth(c);
  const requester = await resolveUserRef(c.env.DB, c.req.param("user"), auth);
  if (!(await deleteFollow(c.env.DB, requester.id, auth.userId, true))) throw err.notFound("follow request");
  return c.body(null, 204);
});
