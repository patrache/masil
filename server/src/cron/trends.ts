// Trends job (data-model.md 6, `*/5 * * * *`): last-24h post_hashtags aggregate replaces the `trends` table (Top 10).
import type { Env } from "../env";
import { all, batch, stmt } from "../lib/db";
import { now } from "../lib/time";

const TRENDS_WINDOW_MS = 86_400_000;
const TRENDS_TOP = 10;

interface TrendAggregate {
  tag: string;
  post_count: number;
  author_count: number;
}

/**
 * Ranking: distinct authors desc, then posts desc, then tag asc. Excludes deleted posts and private or (right now)
 * suspended authors (suspended_until in the past counts as active, like isSuspendedNow). DELETE + INSERTs in one batch.
 */
export async function recomputeTrends(env: Env): Promise<void> {
  const db = env.DB;
  const t = now();
  const top = await all<TrendAggregate>(
    db,
    `SELECT h.tag, COUNT(*) AS post_count, COUNT(DISTINCT h.author_id) AS author_count
       FROM post_hashtags h
       JOIN posts p ON p.id = h.post_id
       JOIN users u ON u.id = h.author_id
      WHERE h.created_at >= ?1 AND p.deleted_at IS NULL AND u.is_private = 0
        AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= ?2))
      GROUP BY h.tag
      ORDER BY author_count DESC, post_count DESC, h.tag ASC
      LIMIT ?3`,
    t - TRENDS_WINDOW_MS,
    t,
    TRENDS_TOP,
  );
  await batch(db, [
    stmt(db, "DELETE FROM trends"),
    ...top.map((r, i) =>
      stmt(
        db,
        "INSERT INTO trends (rank, tag, post_count, author_count, computed_at) VALUES (?1, ?2, ?3, ?4, ?5)",
        i + 1,
        r.tag,
        r.post_count,
        r.author_count,
        t,
      ),
    ),
  ]);
}
