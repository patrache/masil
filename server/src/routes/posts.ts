// Posts (api.md 5.15): POST /posts, GET /posts/:post_id, DELETE /posts/:post_id (plan 02 Task 4),
// GET /posts/:post_id/context, /replies, /quotes (Task 5). Task 6 adds likes and reposts. requireAuth is a per-route argument.
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth } from "../lib/auth";
import { all, decodeCursor, encodeCursor, first, parseLimit } from "../lib/db";
import { err } from "../lib/errors";
import { isId } from "../lib/ids";
import { canViewPost, postVisibilitySql } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { serializePosts } from "../lib/serialize";
import { now } from "../lib/time";
import { vJson, zId, zNonce, zPollInput } from "../lib/validate";
import { createPost, deletePost } from "../services/posts";
import type { PostRow } from "../types/rows";

// Lengths, the 4-image limit (422) and upload ownership are checked in services/posts.ts (createPost).
const CreateBody = z.object({
  client_nonce: zNonce,
  body: z.string().optional(),
  media_upload_ids: z.array(zId).optional(),
  reply_to_id: zId.nullable().optional(),
  quote_of_id: zId.nullable().optional(),
  poll: zPollInput.nullable().optional(),
});

/** A live post the viewer may see (api.md 3.4), else 404. */
async function requireVisiblePost(db: D1Database, viewerId: string, postId: string): Promise<PostRow> {
  const row = isId(postId) ? await first<PostRow>(db, "SELECT * FROM posts WHERE id = ?1", postId) : null;
  if (row === null || row.deleted_at !== null || !(await canViewPost(db, viewerId, row))) throw err.notFound("post");
  return row;
}

/** api.md 5.15: at most 20 ancestors in /context. */
const CONTEXT_MAX = 20;

/** Post-id cursor {k: id, i: id}: anything else is 400 fields.cursor. */
function cursorId(raw: string | undefined): string | null {
  const cur = decodeCursor(raw);
  if (cur === null) return null;
  if (typeof cur.k !== "string" || !isId(cur.k) || cur.k !== cur.i) throw err.invalid({ cursor: "invalid" });
  return cur.i;
}

/** One page of rows fetched with LIMIT n + 1 -> the first n and the cursor of the last one (null when no more). */
function page<T extends { id: string }>(rows: T[], n: number): { rows: T[]; next_cursor: string | null } {
  const kept = rows.slice(0, n);
  const last = kept[kept.length - 1];
  return { rows: kept, next_cursor: rows.length > n && last ? encodeCursor({ k: last.id, i: last.id }) : null };
}

/** A copy shown as a deleted placeholder by serializePosts (structure only: id, reply_to_id, root_id, reply_count). */
function asPlaceholder(row: PostRow, t: number): PostRow {
  return { ...row, deleted_at: row.deleted_at ?? t };
}

export const postRoutes = new Hono<AppEnv>();

postRoutes.post("/posts", requireAuth, limit("RL_POST"), vJson(CreateBody), async (c) => {
  const me = getAuth(c).userId;
  const b = c.req.valid("json");
  const { post, created } = await createPost(rt(c), {
    authorId: me,
    body: b.body ?? "",
    clientNonce: b.client_nonce,
    mediaUploadIds: b.media_upload_ids,
    replyToId: b.reply_to_id ?? null,
    quoteOfId: b.quote_of_id ?? null,
    poll: b.poll ?? null,
  });
  return c.json({ post }, created ? 201 : 200);
});

postRoutes.get("/posts/:post_id", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const row = await requireVisiblePost(c.env.DB, me, c.req.param("post_id"));
  const [post] = await serializePosts(c.env.DB, me, [row]);
  return c.json({ post: post! });
});

postRoutes.delete("/posts/:post_id", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const postId = c.req.param("post_id");
  if (!isId(postId)) throw err.notFound("post");
  await deletePost(rt(c), postId, me);
  return c.body(null, 204);
});

/**
 * Ancestor chain root-first (at most the 20 nearest, one recursive query) + the post itself (404 unless viewable).
 * Ancestors the viewer may not see (deleted, blocked either way, suspended or private non-followed author) become
 * deleted placeholders, which carry no author, content or counts besides reply_count.
 */
postRoutes.get("/posts/:post_id/context", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const row = await requireVisiblePost(db, me, c.req.param("post_id"));
  const vis = postVisibilitySql(me, "p");
  const chain = await all<PostRow & { depth: number; visible: number }>(
    db,
    `WITH RECURSIVE chain(id, depth) AS (
       SELECT reply_to_id, 1 FROM posts WHERE id = ? AND reply_to_id IS NOT NULL
       UNION ALL
       SELECT p.reply_to_id, chain.depth + 1 FROM posts p JOIN chain ON p.id = chain.id
        WHERE p.reply_to_id IS NOT NULL AND chain.depth < ${CONTEXT_MAX}
     )
     SELECT p.*, chain.depth, CASE WHEN ${vis.sql} THEN 1 ELSE 0 END AS visible
       FROM chain JOIN posts p ON p.id = chain.id
      ORDER BY chain.depth DESC`,
    row.id,
    ...vis.params,
  );
  const t = now();
  const ancestors = chain.map(({ depth: _depth, visible, ...r }) => (visible === 1 ? r : asPlaceholder(r, t)));
  const posts = await serializePosts(db, me, [...ancestors, row]);
  const post = posts.pop()!;
  return c.json({ ancestors: posts, post });
});

/**
 * Direct replies, oldest first. Live replies follow api.md 3.4; a deleted reply stays as a placeholder only when it
 * has replies of its own and no block either way separates its author from the viewer. Parent not viewable -> 404.
 */
postRoutes.get("/posts/:post_id/replies", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const parent = await requireVisiblePost(db, me, c.req.param("post_id"));
  const n = parseLimit(c.req.query("limit"), 30, 100);
  const after = cursorId(c.req.query("cursor"));
  const vis = postVisibilitySql(me, "p");
  const rows = await all<PostRow>(
    db,
    `SELECT p.* FROM posts p
      WHERE p.reply_to_id = ? AND (? IS NULL OR p.id > ?)
        AND (${vis.sql}
             OR (p.deleted_at IS NOT NULL AND p.reply_count > 0
                 AND NOT EXISTS (SELECT 1 FROM blocks b
                                  WHERE (b.blocker_id = ? AND b.blocked_id = p.author_id)
                                     OR (b.blocker_id = p.author_id AND b.blocked_id = ?))))
      ORDER BY p.id ASC LIMIT ?`,
    parent.id,
    after,
    after,
    ...vis.params,
    me,
    me,
    n + 1,
  );
  const pg = page(rows, n);
  return c.json({ items: await serializePosts(db, me, pg.rows), next_cursor: pg.next_cursor });
});

/** Viewable posts quoting this one, newest first. Original not viewable -> 404. */
postRoutes.get("/posts/:post_id/quotes", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const orig = await requireVisiblePost(db, me, c.req.param("post_id"));
  const n = parseLimit(c.req.query("limit"), 30, 100);
  const before = cursorId(c.req.query("cursor"));
  const vis = postVisibilitySql(me, "p");
  const rows = await all<PostRow>(
    db,
    `SELECT p.* FROM posts p
      WHERE p.quote_of_id = ? AND (? IS NULL OR p.id < ?) AND ${vis.sql}
      ORDER BY p.id DESC LIMIT ?`,
    orig.id,
    before,
    before,
    ...vis.params,
    n + 1,
  );
  const pg = page(rows, n);
  return c.json({ items: await serializePosts(db, me, pg.rows), next_cursor: pg.next_cursor });
});
