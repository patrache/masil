// Timelines (api.md 5.16): GET /timeline/following, GET /timeline/discover (plan 02 Task 7).
// Task 8 appends /hashtags/:tag/posts and /trends. /timeline is this module's own prefix.
import { Hono } from "hono";
import * as z from "zod";
import type { AppEnv } from "../env";
import { getAuth, requireAuth } from "../lib/auth";
import { parseLimit } from "../lib/db";
import { postVisibilitySql } from "../lib/permissions";
import { vQuery } from "../lib/validate";
import { followingSetSql, latestRepostSql, rangeSql, readTimeline, repostVisibilitySql, timelinePage, type SqlPart } from "../services/timeline";

const TimelineQuery = z.object({ cursor: z.string().optional(), limit: z.string().optional(), since: z.string().optional() });

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
