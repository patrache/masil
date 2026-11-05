// Posts (api.md 5.15): POST /posts, GET /posts/:post_id, DELETE /posts/:post_id (plan 02 Task 4).
// Tasks 5 / 6 add replies, context, quotes, likes and reposts to this module. requireAuth is a per-route argument.
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth } from "../lib/auth";
import { first } from "../lib/db";
import { err } from "../lib/errors";
import { isId } from "../lib/ids";
import { canViewPost } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { serializePosts } from "../lib/serialize";
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
