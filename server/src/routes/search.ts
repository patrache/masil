// Search (api.md 5.13, data-model.md 4): GET /search/messages, GET /search/posts, GET /hashtags/search.
// requireAuth is a per-route argument (plan 01 Global Constraints): /hashtags is shared with plan 02 timelines.
import { Hono } from "hono";
import * as z from "zod";
import type { AppEnv } from "../env";
import { getAuth, requireAuth, resolveUserRef } from "../lib/auth";
import { all, decodeCursor, encodeCursor, jsonList, parseLimit } from "../lib/db";
import { err } from "../lib/errors";
import { idFloor, isId } from "../lib/ids";
import { accessibleChannelIds, postVisibilitySql, requireChannelRead, requireSpaceRole } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { buildMessageSearch, highlights } from "../lib/search";
import { loadChannelRefs, serializeMessages, serializePosts } from "../lib/serialize";
import { now } from "../lib/time";
import { charCount, normalizeText } from "../lib/text";
import { vQuery, zId, zText, zUserRef } from "../lib/validate";
import type { MessageRow, PostRow } from "../types/rows";

const MESSAGE_LIMIT_DEFAULT = 20;
const MESSAGE_LIMIT_MAX = 50;
/** data-model.md 4 rule 4 / api.md 5.13: more accessible channels than this -> 422 search_scope. */
const SEARCH_SCOPE_MAX = 500;
/** data-model.md 4 rule 3: the LIKE fallback only looks at messages from the last 90 days … */
const LIKE_WINDOW_MS = 90 * 24 * 60 * 60 * 1000;
/** … and at most this many of the most recent ones per request. */
const LIKE_SCAN_MAX = 5000;
const POST_LIMIT_DEFAULT = 20;
const POST_LIMIT_MAX = 50;
const HASHTAG_LIMIT_DEFAULT = 10;
const HASHTAG_LIMIT_MAX = 20;
/** api.md 5.13: hashtag search only looks at tags used in the last 7 days … */
const HASHTAG_WINDOW_MS = 7 * 24 * 60 * 60 * 1000;
/** … and counts the last 24 hours. */
const HASHTAG_COUNT_MS = 24 * 60 * 60 * 1000;
/** Tag length bound (data-model.md post_hashtags.tag 1..50). */
const HASHTAG_MAX = 50;

const MessageSearchQuery = z.object({
  q: zText(1, 100),
  channel_id: zId.optional(),
  space_id: zId.optional(),
  author: zUserRef.optional(),
  cursor: z.string().optional(),
  limit: z.string().optional(),
});

const PostSearchQuery = z.object({
  q: zText(1, 100),
  author: zUserRef.optional(),
  cursor: z.string().optional(),
  limit: z.string().optional(),
});

/** Tag prefix: NFC + trim, one leading '#' dropped, lowercase (same normalization as extractHashtags), 1..50 code points. */
const zTagPrefix: z.ZodType<string, string> = z.string().transform((raw, ctx) => {
  let s = normalizeText(raw);
  if (s.startsWith("#")) s = s.slice(1);
  s = normalizeText(s.toLowerCase());
  const n = charCount(s);
  if (n < 1) {
    ctx.addIssue({ code: "too_small", origin: "string", minimum: 1, inclusive: true, input: s });
    return z.NEVER;
  }
  if (n > HASHTAG_MAX) {
    ctx.addIssue({ code: "too_big", origin: "string", maximum: HASHTAG_MAX, inclusive: true, input: s });
    return z.NEVER;
  }
  return s;
});

const HashtagSearchQuery = z.object({
  q: zTagPrefix,
  limit: z.string().optional(),
});


/** Newest-first cursor {k: id, i: id}: anything else is 400 fields.cursor. */
function cursorId(raw: string | undefined): string | null {
  const cur = decodeCursor(raw);
  if (cur === null) return null;
  if (typeof cur.k !== "string" || !isId(cur.k) || cur.k !== cur.i) throw err.invalid({ cursor: "invalid" });
  return cur.i;
}

export const searchRoutes = new Hono<AppEnv>();

/**
 * Scope: channel_id (readable, else 404) > space_id (member, else 404) > every accessible channel (> 500 -> 422).
 * Excludes deleted messages and authors the viewer blocked. FTS when every term has 3+ code points, else LIKE over
 * the newest 5000 in-scope messages of the last 90 days. Newest first.
 */
searchRoutes.get("/search/messages", requireAuth, limit("RL_SEARCH"), vQuery(MessageSearchQuery), async (c) => {
  const auth = getAuth(c);
  const me = auth.userId;
  const db = c.env.DB;
  const query = c.req.valid("query");
  const n = parseLimit(query.limit, MESSAGE_LIMIT_DEFAULT, MESSAGE_LIMIT_MAX);
  const before = cursorId(query.cursor);

  let scope: string[];
  if (query.channel_id !== undefined) {
    scope = [(await requireChannelRead(db, query.channel_id, me)).channel.id];
  } else if (query.space_id !== undefined) {
    await requireSpaceRole(db, query.space_id, me, "member");
    scope = await accessibleChannelIds(db, me, { spaceId: query.space_id });
  } else {
    scope = await accessibleChannelIds(db, me);
  }
  if (scope.length > SEARCH_SCOPE_MAX) throw err.limit("search_scope", SEARCH_SCOPE_MAX);
  const authorId = query.author !== undefined ? (await resolveUserRef(db, query.author, auth)).id : null;
  if (scope.length === 0) return c.json({ items: [], next_cursor: null });

  const search = buildMessageSearch(query.q);
  // ?2 scope, ?3 me, ?4 author (or null), ?5 cursor id (or null), ?6 page size + 1
  const filters = `m.channel_id IN (SELECT value FROM json_each(?2))
        AND m.deleted_at IS NULL
        AND m.author_id NOT IN (SELECT blocked_id FROM blocks WHERE blocker_id = ?3)
        AND (?4 IS NULL OR m.author_id = ?4)
        AND (?5 IS NULL OR m.id < ?5)`;
  let rows: MessageRow[];
  if (search.mode === "fts") {
    rows = await all<MessageRow>(
      db,
      `SELECT m.* FROM messages_fts f JOIN messages m ON m.rowid = f.rowid
        WHERE messages_fts MATCH ?1 AND ${filters}
        ORDER BY m.id DESC LIMIT ?6`,
      search.match,
      jsonList(scope),
      me,
      authorId,
      before,
      n + 1,
    );
  } else {
    // Every term must appear (one param for any number of terms). instr(lower(), lower()), not LIKE: D1 rejects LIKE
    // patterns over 50 bytes, and lower() folds ASCII only, exactly like LIKE's case-insensitivity.
    rows = await all<MessageRow>(
      db,
      `SELECT * FROM (SELECT m.* FROM messages m
                        WHERE ${filters} AND m.id >= ?7
                        ORDER BY m.id DESC LIMIT ${LIKE_SCAN_MAX}) m
        WHERE NOT EXISTS (SELECT 1 FROM json_each(?1) t WHERE instr(lower(m.body), lower(t.value)) = 0)
        ORDER BY m.id DESC LIMIT ?6`,
      JSON.stringify(search.terms),
      jsonList(scope),
      me,
      authorId,
      before,
      n + 1,
      idFloor(now() - LIKE_WINDOW_MS),
    );
  }

  const page = rows.slice(0, n);
  const [messages, refs] = await Promise.all([serializeMessages(db, me, page), loadChannelRefs(db, page.map((r) => r.channel_id))]);
  const items = messages.flatMap((message) => {
    const channel = refs.get(message.channel_id);
    return channel ? [{ message, channel, highlights: highlights(message.body, search.terms) }] : [];
  });
  const last = page[page.length - 1];
  const next_cursor = rows.length > n && last ? encodeCursor({ k: last.id, i: last.id }) : null;
  return c.json({ items, next_cursor });
});

/**
 * Posts the viewer may see (api.md 3.4 via postVisibilitySql: not deleted, author not suspended, no block either way,
 * private authors only for themselves / active followers), replies included. FTS when every term has 3+ code points,
 * else LIKE over the newest 5000 visible posts of the last 90 days. Optional author filter (unknown user -> 404).
 * Newest first, cursor {k: id, i: id}.
 */
searchRoutes.get("/search/posts", requireAuth, limit("RL_SEARCH"), vQuery(PostSearchQuery), async (c) => {
  const auth = getAuth(c);
  const me = auth.userId;
  const db = c.env.DB;
  const query = c.req.valid("query");
  const n = parseLimit(query.limit, POST_LIMIT_DEFAULT, POST_LIMIT_MAX);
  const before = cursorId(query.cursor);
  const authorId = query.author !== undefined ? (await resolveUserRef(db, query.author, auth)).id : null;
  const search = buildMessageSearch(query.q);

  // Anonymous `?` placeholders throughout (postVisibilitySql's fragment uses them), params pushed in SQL order.
  const vis = postVisibilitySql(me, "p");
  const conds = ["p.deleted_at IS NULL", vis.sql];
  const condParams: unknown[] = [...vis.params];
  if (authorId !== null) {
    conds.push("p.author_id = ?");
    condParams.push(authorId);
  }
  if (before !== null) {
    conds.push("p.id < ?");
    condParams.push(before);
  }

  let rows: PostRow[];
  if (search.mode === "fts") {
    rows = await all<PostRow>(
      db,
      `SELECT p.* FROM posts_fts f JOIN posts p ON p.rowid = f.rowid
        WHERE posts_fts MATCH ? AND ${conds.join(" AND ")}
        ORDER BY p.id DESC LIMIT ?`,
      search.match,
      ...condParams,
      n + 1,
    );
  } else {
    // Every term must appear (one param for any number of terms); instr, not LIKE (see /search/messages).
    rows = await all<PostRow>(
      db,
      `SELECT * FROM (SELECT p.* FROM posts p
                        WHERE ${conds.join(" AND ")} AND p.id >= ?
                        ORDER BY p.id DESC LIMIT ${LIKE_SCAN_MAX}) p
        WHERE NOT EXISTS (SELECT 1 FROM json_each(?) t WHERE instr(lower(p.body), lower(t.value)) = 0)
        ORDER BY p.id DESC LIMIT ?`,
      ...condParams,
      idFloor(now() - LIKE_WINDOW_MS),
      JSON.stringify(search.terms),
      n + 1,
    );
  }

  const page = rows.slice(0, n);
  const posts = await serializePosts(db, me, page);
  const items = posts.map((post) => ({ post, highlights: highlights(post.body, search.terms) }));
  const last = page[page.length - 1];
  const next_cursor = rows.length > n && last ? encodeCursor({ k: last.id, i: last.id }) : null;
  return c.json({ items, next_cursor });
});

/**
 * Tag prefix match over tags used in the last 7 days, with the same exclusions as the trends job (data-model.md 6):
 * deleted posts, suspended (right now) and private authors. Prefix as a range (tag >= q AND tag < q || U+10FFFF), so
 * no LIKE escaping. Sorted by uses in the last 24h (0 included) desc, then tag asc.
 */
searchRoutes.get("/hashtags/search", requireAuth, limit("RL_SEARCH"), vQuery(HashtagSearchQuery), async (c) => {
  const db = c.env.DB;
  const query = c.req.valid("query");
  const n = parseLimit(query.limit, HASHTAG_LIMIT_DEFAULT, HASHTAG_LIMIT_MAX);
  const t = now();
  const items = await all<{ tag: string; post_count_24h: number }>(
    db,
    `SELECT h.tag, SUM(h.created_at >= ?2) AS post_count_24h
       FROM post_hashtags h
       JOIN posts p ON p.id = h.post_id
       JOIN users u ON u.id = h.author_id
      WHERE h.tag >= ?1 AND h.tag < ?1 || char(1114111)
        AND h.created_at >= ?3
        AND p.deleted_at IS NULL
        AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= ?4))
        AND u.is_private = 0
      GROUP BY h.tag
      ORDER BY post_count_24h DESC, h.tag ASC
      LIMIT ?5`,
    query.q,
    t - HASHTAG_COUNT_MS,
    t - HASHTAG_WINDOW_MS,
    t,
    n,
  );
  return c.json({ items: items.map((i) => ({ tag: i.tag, post_count_24h: i.post_count_24h })) });
});
