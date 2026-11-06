// Timeline queries shared by routes/timelines.ts (following / discover) and routes/users.ts (profile post tabs)
// (plan 02 Task 7, api.md 5.16 / 5.2). Sources are SQL fragments yielding TimelineRow columns
// (item_id, type, post_id, reposter_id, at); readTimeline pages them by item_id (ULID, newest first).
// All fragments use anonymous `?` placeholders (postVisibilitySql does), params listed in SQL order.
import { all, decodeCursor, encodeCursor } from "../lib/db";
import { err } from "../lib/errors";
import { isId } from "../lib/ids";
import { postVisibilitySql } from "../lib/permissions";
import { serializeTimelineItems } from "../lib/serialize";
import { now } from "../lib/time";
import type { TimelineItem } from "../types/api";
import type { TimelineRow } from "../types/rows";

export interface SqlPart {
  sql: string;
  params: unknown[];
}

const SQL_ALIAS = /^[A-Za-z_][A-Za-z0-9_]*$/;

function checkAlias(...aliases: string[]): void {
  for (const a of aliases) if (!SQL_ALIAS.test(a)) throw new Error(`timeline sql: invalid alias ${JSON.stringify(a)}`);
}

/** `col` is the viewer or one of the viewer's `active` followees. */
export function followingSetSql(viewerId: string, col: string): SqlPart {
  return {
    sql: `(${col} = ? OR ${col} IN (SELECT followee_id FROM follows WHERE follower_id = ? AND status = 'active'))`,
    params: [viewerId, viewerId],
  };
}

/** No block either way between the viewer and user column `col`. */
function notBlockedSql(viewerId: string, col: string, alias: string): SqlPart {
  return {
    sql: `NOT EXISTS (SELECT 1 FROM blocks ${alias}
                       WHERE (${alias}.blocker_id = ? AND ${alias}.blocked_id = ${col})
                          OR (${alias}.blocker_id = ${col} AND ${alias}.blocked_id = ?))`,
    params: [viewerId, viewerId],
  };
}

/**
 * The reposter (reposts alias `r`) is visible to the viewer: no block either way, not suspended now.
 * Also used for the "newer repost of the same post" dedupe so that a hidden newer repost never hides a visible one.
 */
function reposterVisibleSql(viewerId: string, r: string, t: number): SqlPart {
  const blocked = notBlockedSql(viewerId, `${r}.user_id`, `${r}_rb`);
  return {
    sql: `(${blocked.sql}
      AND EXISTS (SELECT 1 FROM users ${r}_ru
                   WHERE ${r}_ru.id = ${r}.user_id
                     AND (${r}_ru.suspended_at IS NULL OR (${r}_ru.suspended_until IS NOT NULL AND ${r}_ru.suspended_until <= ?))))`,
    params: [...blocked.params, t],
  };
}

/**
 * A repost row (alias `r`) of post `p` may be shown to the viewer (events.md 4.8 for timeline.item_created applied to
 * REST): the original passes api.md 3.4 (postVisibilitySql), the reposter is visible, and when the original quotes
 * another post, that quoted post's author and the viewer have no block either way.
 */
export function repostVisibilitySql(viewerId: string, r = "r", p = "p"): SqlPart {
  checkAlias(r, p);
  const t = now();
  const vis = postVisibilitySql(viewerId, p);
  const reposter = reposterVisibleSql(viewerId, r, t);
  const quoted = notBlockedSql(viewerId, `${p}_qp.author_id`, `${p}_qb`);
  return {
    sql: `(${vis.sql}
      AND ${reposter.sql}
      AND (${p}.quote_of_id IS NULL
           OR NOT EXISTS (SELECT 1 FROM posts ${p}_qp WHERE ${p}_qp.id = ${p}.quote_of_id AND NOT (${quoted.sql}))))`,
    params: [...vis.params, ...reposter.params, ...quoted.params],
  };
}

/**
 * Reposts by `reposterSet` (alias `r`) that are the newest visible repost of their post among that set
 * (api.md 5.16: a post reposted several times appears once, as its latest repost).
 */
export function latestRepostSql(viewerId: string, r: string, reposterSet: (col: string) => SqlPart): SqlPart {
  checkAlias(r);
  const r2 = `${r}_n`;
  const set = reposterSet(`${r2}.user_id`);
  const visible = reposterVisibleSql(viewerId, r2, now());
  return {
    sql: `NOT EXISTS (SELECT 1 FROM reposts ${r2}
                       WHERE ${r2}.post_id = ${r}.post_id AND ${r2}.id > ${r}.id AND ${set.sql} AND ${visible.sql})`,
    params: [...set.params, ...visible.params],
  };
}

// ---------------------------------------------------------------------------
// paging

export interface TimelinePage {
  limit: number;
  /** item_id upper bound (exclusive) from the cursor; null = from the newest. */
  before: string | null;
  /** api.md 5.16 `since`: only items newer than this id; next_cursor is null and gap reports overflow. */
  since: string | null;
}

/** cursor {k: item_id, i: item_id}; `since` must be an ID. `since` wins over `cursor` when both are given. */
export function timelinePage(q: { cursor?: string; since?: string }, limit: number): TimelinePage {
  if (q.since !== undefined && q.since !== "") {
    if (!isId(q.since)) throw err.invalid({ since: "invalid" });
    return { limit, before: null, since: q.since };
  }
  const cur = decodeCursor(q.cursor);
  if (cur !== null && !isId(cur.i)) throw err.invalid({ cursor: "invalid" });
  return { limit, before: cur?.i ?? null, since: null };
}

/** The item-id range for one UNION branch (`col` = that branch's item id column). */
export function rangeSql(page: TimelinePage, col: string): SqlPart {
  if (page.since !== null) return { sql: `${col} > ?`, params: [page.since] };
  if (page.before !== null) return { sql: `${col} < ?`, params: [page.before] };
  return { sql: "1 = 1", params: [] };
}

export interface TimelineResult {
  items: TimelineItem[];
  next_cursor: string | null;
  gap: boolean;
}

/**
 * Runs `source` (UNION ALL of branches selecting item_id, type, post_id, reposter_id, at, each already bounded by
 * rangeSql) newest first, limit + 1 rows, and serializes the page.
 * pageSince rule: with `since`, gap = more than `limit` newer rows exist and next_cursor = null; otherwise gap = false.
 */
export async function readTimeline(db: D1Database, viewerId: string, source: SqlPart, page: TimelinePage): Promise<TimelineResult> {
  const rows = await all<TimelineRow>(
    db,
    `SELECT item_id, type, post_id, reposter_id, at FROM (${source.sql}) ORDER BY item_id DESC LIMIT ?`,
    ...source.params,
    page.limit + 1,
  );
  const more = rows.length > page.limit;
  const slice = rows.slice(0, page.limit);
  const items = await serializeTimelineItems(db, viewerId, slice);
  if (page.since !== null) return { items, next_cursor: null, gap: more };
  const last = slice[slice.length - 1];
  return { items, next_cursor: more && last ? encodeCursor({ k: last.item_id, i: last.item_id }) : null, gap: false };
}
