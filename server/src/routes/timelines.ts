// Timelines (api.md 5.16): GET /timeline/following, GET /timeline/discover (plan 02 Task 7).
// Task 8: GET /hashtags/:tag/posts, GET /trends. /timeline is this module's own prefix; /hashtags is shared with
// routes/search.ts (/hashtags/search), so the Task 8 routes take requireAuth as a per-route argument.
import { Hono } from "hono";
import * as z from "zod";
import type { AppEnv } from "../env";
import { getAuth, requireAuth } from "../lib/auth";
import { all, decodeCursor, encodeCursor, parseLimit } from "../lib/db";
import { err } from "../lib/errors";
import { isId } from "../lib/ids";
import { postVisibilitySql } from "../lib/permissions";
import { serializePosts } from "../lib/serialize";
import { normalizeTag } from "../lib/text";
import { isoOrNull } from "../lib/time";
import { vQuery } from "../lib/validate";
import { followingSetSql, latestRepostSql, rangeSql, readTimeline, repostVisibilitySql, timelinePage, type SqlPart } from "../services/timeline";
import type { PostRow, TrendRow } from "../types/rows";

const TimelineQuery = z.object({ cursor: z.string().optional(), limit: z.string().optional(), since: z.string().optional() });
const PageQuery = z.object({ cursor: z.string().optional(), limit: z.string().optional() });

export const timelineRoutes = new Hono<AppEnv>();
timelineRoutes.use("/timeline/*", requireAuth);

/**
 * Me + my active followees: their top-level posts and quotes (no replies) that pass api.md 3.4, plus their reposts
 * that pass repostVisibilitySql, each post only once as its latest such repost.
 */
timelineRoutes.get("/timeline/following", vQuery(TimelineQuery), async (c) => {
  const me = getAuth(c).userId;
  const q = c.req.valid("query");
  const page = timelinePage(q, parseLimit(q.limit, 30, 100));
  const authors = followingSetSql(me, "p.author_id");
  const vis = postVisibilitySql(me, "p");
  const postRange = rangeSql(page, "p.id");
  const reposters = followingSetSql(me, "r.user_id");
  const repostVis = repostVisibilitySql(me, "r", "rp");
  const latest = latestRepostSql(me, "r", (col) => followingSetSql(me, col));
  const repostRange = rangeSql(page, "r.id");
  const source: SqlPart = {
    sql: `SELECT p.id AS item_id, 'post' AS type, p.id AS post_id, NULL AS reposter_id, p.created_at AS at
            FROM posts p
           WHERE ${authors.sql} AND p.reply_to_id IS NULL AND ${vis.sql} AND ${postRange.sql}
          UNION ALL
          SELECT r.id AS item_id, 'repost' AS type, r.post_id AS post_id, r.user_id AS reposter_id, r.created_at AS at
            FROM reposts r JOIN posts rp ON rp.id = r.post_id
           WHERE ${reposters.sql} AND ${repostVis.sql} AND ${latest.sql} AND ${repostRange.sql}`,
    params: [
      ...authors.params,
      ...vis.params,
      ...postRange.params,
      ...reposters.params,
      ...repostVis.params,
      ...latest.params,
      ...repostRange.params,
    ],
  };
  return c.json(await readTimeline(c.env.DB, me, source, page));
});

/** Top-level posts and quotes of public accounts that pass api.md 3.4 (not suspended, no block), newest first; no reposts. */
timelineRoutes.get("/timeline/discover", vQuery(TimelineQuery), async (c) => {
  const me = getAuth(c).userId;
  const q = c.req.valid("query");
  const page = timelinePage(q, parseLimit(q.limit, 30, 100));
  const vis = postVisibilitySql(me, "p");
  const range = rangeSql(page, "p.id");
  const source: SqlPart = {
    // reply_to_id IS NULL AND deleted_at IS NULL matches the partial index idx_posts_top.
    sql: `SELECT p.id AS item_id, 'post' AS type, p.id AS post_id, NULL AS reposter_id, p.created_at AS at
            FROM posts p
           WHERE p.reply_to_id IS NULL AND p.deleted_at IS NULL AND ${range.sql}
             AND EXISTS (SELECT 1 FROM users du WHERE du.id = p.author_id AND du.is_private = 0)
             AND ${vis.sql}`,
    params: [...range.params, ...vis.params],
  };
  return c.json(await readTimeline(c.env.DB, me, source, page));
});

/**
 * api.md 5.16: posts tagged {tag} (normalized like stored tags: leading '#' dropped, NFC, lowercase), newest first.
 * Public authors only — a private account's posts never appear, even for followers or the author — and api.md 3.4.
 */
timelineRoutes.get("/hashtags/:tag/posts", requireAuth, vQuery(PageQuery), async (c) => {
  const me = getAuth(c).userId;
  const tag = normalizeTag(c.req.param("tag"));
  if (tag === null) throw err.invalid({ tag: "invalid" });
  const q = c.req.valid("query");
  const n = parseLimit(q.limit, 30, 100);
  const cur = decodeCursor(q.cursor);
  if (cur !== null && !isId(cur.i)) throw err.invalid({ cursor: "invalid" });
  const vis = postVisibilitySql(me, "p");
  // (tag, post_id) primary key: equality on tag + post_id range/order.
  const rows = await all<PostRow>(
    c.env.DB,
    `SELECT p.* FROM post_hashtags h
       JOIN posts p ON p.id = h.post_id
       JOIN users u ON u.id = p.author_id AND u.is_private = 0
      WHERE h.tag = ? AND ${cur !== null ? "h.post_id < ?" : "1 = 1"} AND ${vis.sql}
      ORDER BY h.post_id DESC LIMIT ?`,
    tag,
    ...(cur !== null ? [cur.i] : []),
    ...vis.params,
    n + 1,
  );
  const page = rows.slice(0, n);
  const items = await serializePosts(c.env.DB, me, page);
  const last = page[page.length - 1];
  return c.json({ items, next_cursor: rows.length > n && last ? encodeCursor({ k: last.id, i: last.id }) : null });
});

/** api.md 5.16: Top 10 from the last trends cron run (data-model.md 6); computed_at null before the first run. */
timelineRoutes.get("/trends", requireAuth, async (c) => {
  const rows = await all<TrendRow>(c.env.DB, "SELECT rank, tag, post_count, author_count, computed_at FROM trends ORDER BY rank");
  const computedAt = rows.length > 0 ? Math.max(...rows.map((r) => r.computed_at)) : null;
  return c.json({
    items: rows.map((r) => ({ rank: r.rank, tag: r.tag, post_count: r.post_count, author_count: r.author_count })),
    computed_at: isoOrNull(computedAt),
  });
});
