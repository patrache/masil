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
import { canViewUserContent } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { loadUserSummaries, serializeProfile, toUserSummary } from "../lib/serialize";
import { now } from "../lib/time";
import { vQuery, zText } from "../lib/validate";
import type { UserRow } from "../types/rows";

const IDS_MAX = 100;
const SEARCH_LIMIT_DEFAULT = 10;
const SEARCH_LIMIT_MAX = 20;

const SearchQuery = z.object({ q: zText(1, 100), limit: z.string().optional() });
const IdsQuery = z.object({ ids: z.string() });
const PageQuery = z.object({ cursor: z.string().optional(), limit: z.string().optional() });

/** Escapes LIKE wildcards for `... LIKE ? ESCAPE '\'`. */
function likeEscape(s: string): string {
  return s.replace(/[\\%_]/g, (ch) => `\\${ch}`);
}

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
      WHERE (u.handle LIKE ?1 ESCAPE '\\' OR u.display_name LIKE ?2 ESCAPE '\\')
        AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= ?4))
        AND NOT EXISTS (SELECT 1 FROM blocks b
                         WHERE (b.blocker_id = ?3 AND b.blocked_id = u.id) OR (b.blocker_id = u.id AND b.blocked_id = ?3))
      ORDER BY CASE WHEN u.handle = ?5 THEN 0 WHEN u.handle LIKE ?1 ESCAPE '\\' THEN 1 ELSE 2 END, u.handle
      LIMIT ?6`,
    `${likeEscape(handleQ)}%`,
    `%${likeEscape(q)}%`,
    me,
    t,
    handleQ,
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
