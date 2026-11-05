// Search (api.md 5.13, data-model.md 4). Task 28: GET /search/messages. Task 29 adds /search/posts and
// /hashtags/search here. requireAuth is a per-route argument (plan 01 Global Constraints).
import { Hono } from "hono";
import * as z from "zod";
import type { AppEnv } from "../env";
import { getAuth, requireAuth, resolveUserRef } from "../lib/auth";
import { all, decodeCursor, encodeCursor, jsonList, parseLimit } from "../lib/db";
import { err } from "../lib/errors";
import { idFloor, isId } from "../lib/ids";
import { accessibleChannelIds, requireChannelRead, requireSpaceRole } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { buildMessageSearch, highlights } from "../lib/search";
import { loadChannelRefs, serializeMessages } from "../lib/serialize";
import { now } from "../lib/time";
import { vQuery, zId, zText, zUserRef } from "../lib/validate";
import type { MessageRow } from "../types/rows";

const MESSAGE_LIMIT_DEFAULT = 20;
const MESSAGE_LIMIT_MAX = 50;
/** data-model.md 4 rule 4 / api.md 5.13: more accessible channels than this -> 422 search_scope. */
const SEARCH_SCOPE_MAX = 500;
/** data-model.md 4 rule 3: the LIKE fallback only looks at messages from the last 90 days … */
const LIKE_WINDOW_MS = 90 * 24 * 60 * 60 * 1000;
/** … and at most this many of the most recent ones per request. */
const LIKE_SCAN_MAX = 5000;

const MessageSearchQuery = z.object({
  q: zText(1, 100),
  channel_id: zId.optional(),
  space_id: zId.optional(),
  author: zUserRef.optional(),
  cursor: z.string().optional(),
  limit: z.string().optional(),
});

/** Escapes LIKE wildcards for `... LIKE ? ESCAPE '\'`. */
function likeEscape(s: string): string {
  return s.replace(/[\\%_]/g, (ch) => `\\${ch}`);
}

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
    // Every escaped term must appear: no json_each row may fail its LIKE (one param for any number of terms).
    rows = await all<MessageRow>(
      db,
      `SELECT * FROM (SELECT m.* FROM messages m
                        WHERE ${filters} AND m.id >= ?7
                        ORDER BY m.id DESC LIMIT ${LIKE_SCAN_MAX}) m
        WHERE NOT EXISTS (SELECT 1 FROM json_each(?1) t WHERE m.body NOT LIKE '%' || t.value || '%' ESCAPE '\\')
        ORDER BY m.id DESC LIMIT ?6`,
      JSON.stringify(search.terms.map(likeEscape)),
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
