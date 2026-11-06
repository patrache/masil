// User lookup (api.md 5.2, plan 01 part): GET /users/search, GET /users?ids=, GET /users/:user.
// Plan 02 appends /users/:user/posts|followers|following here. /users is a shared prefix (users, plan 02 follows,
// stories, guestbook), so requireAuth is a per-route argument. /users/search is registered before /users/:user.
import { Hono } from "hono";
import * as z from "zod";
import type { AppEnv } from "../env";
import { getAuth, requireAuth, resolveUserRef } from "../lib/auth";
import { all, decodeCursor, encodeCursor, parseLimit } from "../lib/db";
import { err } from "../lib/errors";
import { isId } from "../lib/ids";
import { canViewUserContent, postVisibilitySql } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { loadUserSummaries, serializeProfile, toUserSummary } from "../lib/serialize";
import { now } from "../lib/time";
import { vQuery, zText } from "../lib/validate";
import { rangeSql, readTimeline, repostVisibilitySql, timelinePage, type SqlPart } from "../services/timeline";
import type { UserRow } from "../types/rows";

const IDS_MAX = 100;
const SEARCH_LIMIT_DEFAULT = 10;
const SEARCH_LIMIT_MAX = 20;

const SearchQuery = z.object({ q: zText(1, 100), limit: z.string().optional() });
const IdsQuery = z.object({ ids: z.string() });
const PageQuery = z.object({ cursor: z.string().optional(), limit: z.string().optional() });
const PostsTabQuery = PageQuery.extend({ filter: z.enum(["posts", "replies", "media"]).optional() });


export const userRoutes = new Hono<AppEnv>();

/**
 * handle prefix (case-insensitive; a leading "@" is ignored) OR display_name substring. Blocked either way and
 * currently suspended users are excluded. Order: exact handle, handle prefix, display-name match; then handle.
 */
userRoutes.get("/users/search", requireAuth, limit("RL_SEARCH"), vQuery(SearchQuery), async (c) => {
  const me = getAuth(c).userId;
  const query = c.req.valid("query");
  const n = parseLimit(query.limit, SEARCH_LIMIT_DEFAULT, SEARCH_LIMIT_MAX);
  const q = query.q;
  const handleQ = (q.startsWith("@") ? q.slice(1) : q).toLowerCase();
  const t = now();
  const rows = await all<UserRow>(
    c.env.DB,
    `SELECT u.* FROM users u
      WHERE (substr(u.handle, 1, length(?1)) = ?1 OR instr(lower(u.display_name), lower(?2)) > 0)
        AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= ?4))
        AND NOT EXISTS (SELECT 1 FROM blocks b
                         WHERE (b.blocker_id = ?3 AND b.blocked_id = u.id) OR (b.blocker_id = u.id AND b.blocked_id = ?3))
      ORDER BY CASE WHEN u.handle = ?1 THEN 0 WHEN substr(u.handle, 1, length(?1)) = ?1 THEN 1 ELSE 2 END, u.handle
      LIMIT ?5`,
    handleQ,
    q,
    me,
    t,
    n,
  );
  return c.json({ items: rows.map((r) => toUserSummary(r, t)) });
});

/** Up to 100 ids, input order, duplicates once; unknown ids are simply absent. */
userRoutes.get("/users", requireAuth, vQuery(IdsQuery), async (c) => {
  const ids = [
    ...new Set(
      c.req
        .valid("query")
        .ids.split(",")
        .map((s) => s.trim())
        .filter((s) => s !== ""),
    ),
  ];
  if (ids.length === 0 || ids.length > IDS_MAX || !ids.every(isId)) throw err.invalid({ ids: "invalid" });
  const byId = await loadUserSummaries(c.env.DB, ids);
  return c.json({ items: ids.flatMap((id) => byId.get(id) ?? []) });
});

/** Profile; a block either way still answers 200 (can_dm=false, can_view_posts=false). */
userRoutes.get("/users/:user", requireAuth, async (c) => {
  const auth = getAuth(c);
  const row = await resolveUserRef(c.env.DB, c.req.param("user"), auth);
  return c.json({ user: await serializeProfile(c.env.DB, auth.userId, row) });
});

/**
 * GET /users/:user/followers | /following (api.md 5.2, 3.3): `active` rows only, newest accepted first
 * (cursor {k: accepted_at, i: user_id}). Block either way with the owner -> 404; private owner and I am neither the
 * owner nor an active follower -> 403 private_account. Users in a block relation with me are left out of the list.
 */
function followListRoute(side: "followers" | "following") {
  // followers of X: rows followee_id = X, listing follower_id; following of X: rows follower_id = X, listing followee_id
  const ownerCol = side === "followers" ? "followee_id" : "follower_id";
  const otherCol = side === "followers" ? "follower_id" : "followee_id";
  userRoutes.get(`/users/:user/${side}`, requireAuth, vQuery(PageQuery), async (c) => {
    const auth = getAuth(c);
    const db = c.env.DB;
    const owner = await resolveUserRef(db, c.req.param("user"), auth);
    const access = await canViewUserContent(db, auth.userId, owner.id);
    if (access === "hidden") throw err.notFound("user");
    if (access === "private") throw err.forbidden("private_account");
    const q = c.req.valid("query");
    const n = parseLimit(q.limit, 30, 100);
    const cur = decodeCursor(q.cursor);
    if (cur !== null && typeof cur.k !== "number") throw err.invalid({ cursor: "invalid" });
    const rows = await all<{ user_id: string; k: number }>(
      db,
      `SELECT f.${otherCol} AS user_id, COALESCE(f.accepted_at, f.created_at) AS k
         FROM follows f
        WHERE f.${ownerCol} = ?1 AND f.status = 'active'
          AND NOT EXISTS (SELECT 1 FROM blocks b
                           WHERE (b.blocker_id = ?2 AND b.blocked_id = f.${otherCol}) OR (b.blocker_id = f.${otherCol} AND b.blocked_id = ?2))
          AND (?3 IS NULL OR COALESCE(f.accepted_at, f.created_at) < ?3
               OR (COALESCE(f.accepted_at, f.created_at) = ?3 AND f.${otherCol} < ?4))
        ORDER BY k DESC, user_id DESC
        LIMIT ?5`,
      owner.id,
      auth.userId,
      cur?.k ?? null,
      cur?.i ?? null,
      n + 1,
    );
    const page = rows.slice(0, n);
    const users = await loadUserSummaries(db, page.map((r) => r.user_id));
    const last = page[page.length - 1];
    const next_cursor = rows.length > n && last ? encodeCursor({ k: last.k, i: last.user_id }) : null;
    return c.json({ items: page.flatMap((r) => users.get(r.user_id) ?? []), next_cursor });
  });
}
followListRoute("followers");
followListRoute("following");

/**
 * GET /users/:user/posts?filter=posts|replies|media (api.md 5.2, plan 02 Task 7). Block either way -> 404, private and
 * not an active follower -> 403 private_account. Every post passes api.md 3.4 (postVisibilitySql). `posts` (default):
 * the owner's top-level posts and quotes + the owner's reposts filtered like the following timeline
 * (repostVisibilitySql: original viewable, quoted author not blocked either way); `replies`: replies only;
 * `media`: the owner's posts with at least one non-removed image. Newest item first, cursor {k: item_id, i: item_id}.
 */
userRoutes.get("/users/:user/posts", requireAuth, vQuery(PostsTabQuery), async (c) => {
  const auth = getAuth(c);
  const db = c.env.DB;
  const owner = await resolveUserRef(db, c.req.param("user"), auth);
  const access = await canViewUserContent(db, auth.userId, owner.id);
  if (access === "hidden") throw err.notFound("user");
  if (access === "private") throw err.forbidden("private_account");
  const q = c.req.valid("query");
  const page = timelinePage({ cursor: q.cursor }, parseLimit(q.limit, 30, 100));
  const vis = postVisibilitySql(auth.userId, "p");
  const range = rangeSql(page, "p.id");
  const postBranch = (where: string): SqlPart => ({
    sql: `SELECT p.id AS item_id, 'post' AS type, p.id AS post_id, NULL AS reposter_id, p.created_at AS at
            FROM posts p
           WHERE p.author_id = ? AND ${where} AND ${vis.sql} AND ${range.sql}`,
    params: [owner.id, ...vis.params, ...range.params],
  });
  const filter = q.filter ?? "posts";
  let source: SqlPart;
  if (filter === "replies") {
    source = postBranch("p.reply_to_id IS NOT NULL");
  } else if (filter === "media") {
    source = postBranch(
      `EXISTS (SELECT 1 FROM post_media pm JOIN uploads u ON u.id = pm.upload_id WHERE pm.post_id = p.id AND u.status <> 'removed')`,
    );
  } else {
    const top = postBranch("p.reply_to_id IS NULL");
    const repostVis = repostVisibilitySql(auth.userId, "r", "rp");
    const repostRange = rangeSql(page, "r.id");
    source = {
      sql: `${top.sql}
            UNION ALL
            SELECT r.id AS item_id, 'repost' AS type, r.post_id AS post_id, r.user_id AS reposter_id, r.created_at AS at
              FROM reposts r JOIN posts rp ON rp.id = r.post_id
             WHERE r.user_id = ? AND ${repostVis.sql} AND ${repostRange.sql}`,
      params: [...top.params, owner.id, ...repostVis.params, ...repostRange.params],
    };
  }
  const { items, next_cursor } = await readTimeline(db, auth.userId, source, page);
  return c.json({ items, next_cursor });
});
