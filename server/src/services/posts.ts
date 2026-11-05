// Post services (server-architecture.md 4.14 posts.ts, api.md 5.15): createPost / deletePost with their counters.
// Each write is one db.batch; events and notifications go out only after it committed (server-architecture.md 3.5).
// Plan 02 Task 4: top-level posts (media, hashtags, mentions, poll, nonce idempotency) and deletion.
// Plan 02 Task 5: the reply / quote target (step 3 of createPost): validation, counters, notifications, events.
import type { Runtime } from "../env";
import { all, batch, first, jsonList, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { newId } from "../lib/ids";
import { notifyMany, type NotifyInput } from "../lib/notify";
import { canViewPost } from "../lib/permissions";
import { publishEvent, type VisibilitySpec } from "../lib/realtime";
import { eventParentAuthorIds, loadUserSummaries, serializePosts } from "../lib/serialize";
import { charCount, extractHashtags, extractMentions, normalizeText, plainSnippet } from "../lib/text";
import { iso, now } from "../lib/time";
import { removeUploads } from "../lib/uploads";
import type { Alert, PollInput, Post, TimelineItem } from "../types/api";
import type { PostRow, UploadRow, UserRow } from "../types/rows";
import { pollInsertStmts } from "./polls";

/** api.md 0.3: post body 0..500 code points (1..500 without media, poll or quote). */
export const POST_BODY_MAX = 500;
/** api.md 0.3: at most 4 images per post. */
export const POST_MEDIA_MAX = 4;

export interface CreatePostInput {
  authorId: string;
  body: string;
  clientNonce: string;
  mediaUploadIds?: string[];
  replyToId?: string | null;
  quoteOfId?: string | null;
  poll?: PollInput | null;
}

function findByNonce(db: D1Database, authorId: string, clientNonce: string): Promise<PostRow | null> {
  return first<PostRow>(db, "SELECT * FROM posts WHERE author_id = ?1 AND client_nonce = ?2", authorId, clientNonce);
}

function isUniqueViolation(e: unknown): boolean {
  return e instanceof Error && /UNIQUE constraint failed/i.test(e.message);
}

/** Notifications after commit: awaited in tests, waitUntil otherwise; failures are logged only. */
function scheduleNotifications(rt: Runtime, label: string, work: () => Promise<void>): Promise<void> {
  const p = work().catch((e) => console.error(`posts: ${label} failed`, e));
  if (rt.env.APP_ENV === "test") return p;
  rt.waitUntil(p);
  return Promise.resolve();
}

function postTarget(postId: string): Alert["target"] {
  return { type: "post", id: postId, channel_id: null, message_id: null, thread_root_id: null, post_id: postId, space_id: null };
}

/**
 * The uploads must be the author's own, purpose "post", status "pending", each listed once (else 400
 * fields.media_upload_ids = invalid). One query for the whole list.
 */
async function checkMedia(db: D1Database, authorId: string, ids: string[]): Promise<void> {
  if (ids.length === 0) return;
  if (new Set(ids).size !== ids.length) throw err.invalid({ media_upload_ids: "invalid" });
  const rows = await all<Pick<UploadRow, "id">>(
    db,
    `SELECT id FROM uploads
      WHERE id IN (SELECT value FROM json_each(?1)) AND owner_id = ?2 AND purpose = 'post' AND status = 'pending'`,
    jsonList(ids),
    authorId,
  );
  if (rows.length !== ids.length) throw err.invalid({ media_upload_ids: "invalid" });
}

/**
 * `@handle` mentions that resolve (body order, <= 50): the user exists, is not suspended now, has no block either way
 * with the author and can view the new post — a private author's post only for the author or an active follower.
 */
async function resolvePostMentions(db: D1Database, author: UserRow, body: string, t: number): Promise<string[]> {
  const handles = extractMentions(body);
  if (handles.length === 0) return [];
  const rows = await all<{ id: string }>(
    db,
    `SELECT u.id FROM json_each(?1) j JOIN users u ON u.handle = j.value
      WHERE (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= ?3))
        AND NOT EXISTS (SELECT 1 FROM blocks b
                         WHERE (b.blocker_id = ?2 AND b.blocked_id = u.id) OR (b.blocker_id = u.id AND b.blocked_id = ?2))
        AND (?4 = 0 OR u.id = ?2
             OR EXISTS (SELECT 1 FROM follows f WHERE f.follower_id = u.id AND f.followee_id = ?2 AND f.status = 'active'))
      ORDER BY j.key`,
    jsonList(handles),
    author.id,
    t,
    author.is_private,
  );
  return [...new Set(rows.map((r) => r.id))];
}

interface PostTarget {
  post: PostRow;
  author: UserRow;
}

/**
 * The reply parent / quoted original (api.md 5.15): missing, deleted or not viewable by the author (api.md 3.4 — includes
 * blocks either way and private authors the poster does not actively follow) -> 404; quoting a private account's post
 * -> 403 action_not_allowed (whoever the viewer is).
 */
async function loadTarget(db: D1Database, viewerId: string, targetId: string | null, isQuote: boolean): Promise<PostTarget | null> {
  if (targetId === null) return null;
  const post = await first<PostRow>(db, "SELECT * FROM posts WHERE id = ?1", targetId);
  if (post === null || post.deleted_at !== null || !(await canViewPost(db, viewerId, post))) throw err.notFound("post");
  const author = await first<UserRow>(db, "SELECT * FROM users WHERE id = ?1", post.author_id);
  if (author === null) throw err.notFound("post");
  if (isQuote && author.is_private === 1) throw err.forbidden("action_not_allowed", "posts of private accounts cannot be quoted");
  return { post, author };
}

/**
 * Creates a post (api.md 5.15). Order:
 * (1) same author + same client_nonce -> the existing post, created:false (pre-check, and uq_posts_nonce for races);
 * (2) validation — reply + quote together -> fields.reply_to_id not_allowed, poll on a reply -> fields.poll not_allowed,
 *     more than 4 images -> 422 limit_exceeded post_media, body NFC + trim 0..500 (1..500 without media / poll / quote),
 *     uploads not mine / not purpose post / not pending / repeated -> fields.media_upload_ids invalid;
 * (3) reply / quote target: missing / deleted / not viewable -> 404, quoting a private account's post -> 403
 *     action_not_allowed; root_id = parent.root_id ?? parent.id;
 * (4) mentions that can view the post;
 * (5) one batch: poll -> posts -> post_media (request order = position) -> uploads attached -> post_hashtags ->
 *     post_mentions -> author post_count + 1 -> parent reply_count + 1 / quoted quote_count + 1;
 * (6) after commit: top-level posts and quotes -> timeline.item_created (ephemeral) to active followers + the author,
 *     replies -> post.reply_created (ephemeral) on topic post:<parent>, both through the visibility filter (authors =
 *     poster + target author + any reply-parent author the event Post names); post.stats on the target's topic; then post_reply / post_quote to the target author
 *     and post_mention to the remaining mentioned users.
 */
export async function createPost(rt: Runtime, input: CreatePostInput): Promise<{ post: Post; created: boolean }> {
  const db = rt.env.DB;

  // (1) idempotency
  const existing = await findByNonce(db, input.authorId, input.clientNonce);
  if (existing !== null) {
    const [post] = await serializePosts(db, input.authorId, [existing]);
    return { post: post!, created: false };
  }

  // (2) validation
  const replyToId = input.replyToId ?? null;
  const quoteOfId = input.quoteOfId ?? null;
  const poll = input.poll ?? null;
  const mediaIds = input.mediaUploadIds ?? [];
  if (replyToId !== null && quoteOfId !== null) throw err.invalid({ reply_to_id: "not_allowed" });
  if (replyToId !== null && poll !== null) throw err.invalid({ poll: "not_allowed" });
  if (mediaIds.length > POST_MEDIA_MAX) throw err.limit("post_media", POST_MEDIA_MAX);
  const body = normalizeText(input.body);
  const n = charCount(body);
  const hasAttachment = mediaIds.length > 0 || poll !== null || quoteOfId !== null;
  if (n < (hasAttachment ? 0 : 1)) throw err.invalid({ body: "too_short" });
  if (n > POST_BODY_MAX) throw err.invalid({ body: "too_long" });
  const [author] = await Promise.all([
    first<UserRow>(db, "SELECT * FROM users WHERE id = ?1", input.authorId),
    checkMedia(db, input.authorId, mediaIds),
  ]);
  if (author === null) throw err.notFound("user");

  // (3) reply / quote target
  const target = await loadTarget(db, author.id, replyToId ?? quoteOfId, quoteOfId !== null);

  // (4) mentions
  const t = now();
  const mentionIds = await resolvePostMentions(db, author, body, t);
  const hashtags = extractHashtags(body);

  // (5) one batch
  const id = newId(t);
  const pollId = poll !== null ? newId(t) : null;
  const row: PostRow = {
    id,
    author_id: author.id,
    body,
    reply_to_id: replyToId,
    root_id: replyToId !== null ? (target!.post.root_id ?? target!.post.id) : null,
    quote_of_id: quoteOfId,
    poll_id: pollId,
    like_count: 0,
    repost_count: 0,
    reply_count: 0,
    quote_count: 0,
    client_nonce: input.clientNonce,
    created_at: t,
    deleted_at: null,
    deleted_by: null,
  };
  const stmts: D1PreparedStatement[] = [];
  // polls first: posts.poll_id references polls(id)
  if (pollId !== null) stmts.push(...pollInsertStmts(db, pollId, author.id, { type: "post", id }, poll!, t));
  stmts.push(
    stmt(
      db,
      `INSERT INTO posts (id, author_id, body, reply_to_id, root_id, quote_of_id, poll_id, client_nonce, created_at)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)`,
      row.id,
      row.author_id,
      row.body,
      row.reply_to_id,
      row.root_id,
      row.quote_of_id,
      row.poll_id,
      row.client_nonce,
      row.created_at,
    ),
  );
  if (mediaIds.length > 0) {
    stmts.push(
      stmt(
        db,
        "INSERT INTO post_media (post_id, position, upload_id) SELECT ?1, CAST(j.key AS INTEGER), j.value FROM json_each(?2) j ORDER BY j.key",
        id,
        jsonList(mediaIds),
      ),
      stmt(
        db,
        "UPDATE uploads SET status = 'attached', attached_to = ?2 WHERE id IN (SELECT value FROM json_each(?1)) AND status = 'pending'",
        jsonList(mediaIds),
        `post:${id}`,
      ),
    );
  }
  // rowid order = extractHashtags / body order (serializePosts reads ORDER BY rowid)
  if (hashtags.length > 0) {
    stmts.push(
      stmt(
        db,
        "INSERT INTO post_hashtags (tag, post_id, author_id, created_at) SELECT j.value, ?1, ?2, ?3 FROM json_each(?4) j ORDER BY j.key",
        id,
        author.id,
        t,
        jsonList(hashtags),
      ),
    );
  }
  if (mentionIds.length > 0) {
    stmts.push(
      stmt(db, "INSERT INTO post_mentions (post_id, user_id) SELECT ?1, j.value FROM json_each(?2) j ORDER BY j.key", id, jsonList(mentionIds)),
    );
  }
  stmts.push(stmt(db, "UPDATE users SET post_count = post_count + 1 WHERE id = ?1", author.id));
  // deletePost decrements these for a live reply / quote regardless of the target's state, so increment the same way.
  if (replyToId !== null) stmts.push(stmt(db, "UPDATE posts SET reply_count = reply_count + 1 WHERE id = ?1", replyToId));
  if (quoteOfId !== null) stmts.push(stmt(db, "UPDATE posts SET quote_count = quote_count + 1 WHERE id = ?1", quoteOfId));

  try {
    await batch(db, stmts);
  } catch (e) {
    // The whole batch rolled back. Same nonce raced in -> return the winner. An upload attached by a concurrent
    // post (post_media.upload_id UNIQUE) -> the media list is no longer valid.
    if (!isUniqueViolation(e)) throw e;
    const winner = await findByNonce(db, input.authorId, input.clientNonce);
    if (winner === null) {
      if (/post_media/i.test((e as Error).message)) throw err.invalid({ media_upload_ids: "invalid" });
      throw e;
    }
    const [post] = await serializePosts(db, input.authorId, [winner]);
    return { post: post!, created: false };
  }

  // (6) after commit
  const related = target === null ? [author] : [author, target.author];
  const privateAuthors = [...new Set(related.filter((u) => u.is_private === 1).map((u) => u.id))];
  const [[post], [eventPost]] = await Promise.all([
    serializePosts(db, author.id, [row]),
    serializePosts(db, null, [row], { privateOk: privateAuthors }),
  ]);
  // Reply-parent authors the event names (a reply's parent, a quoted reply's parent) join the block filter too.
  const visibleTo: VisibilitySpec = {
    authors: [...new Set([...related.map((u) => u.id), ...eventParentAuthorIds(eventPost!)])],
    privateAuthors,
  };
  if (replyToId === null) {
    // top-level posts and quotes
    const item: TimelineItem = { id: row.id, type: "post", post: eventPost!, reposted_by: null, at: iso(row.created_at) };
    await publishEvent(rt, { followers: author.id }, "timeline.item_created", { item }, { ephemeral: true, includeSelf: author.id, visibleTo });
  } else {
    await publishEvent(
      rt,
      { topic: `post:${replyToId}` },
      "post.reply_created",
      { parent_id: replyToId, post: eventPost! },
      { ephemeral: true, visibleTo },
    );
  }
  if (target !== null) await publishStats(rt, target.post.id);

  const name = author.display_name;
  const snippet = plainSnippet(body, 200);
  const inputs: NotifyInput[] = [];
  // Recipients of post_reply / post_quote get that notification instead of post_mention.
  const notifiedOtherwise = new Set<string>();
  if (target !== null && target.author.id !== author.id) {
    const isReply = replyToId !== null;
    notifiedOtherwise.add(target.author.id);
    inputs.push({
      userId: target.author.id,
      type: isReply ? "post_reply" : "post_quote",
      actorId: author.id,
      subject: { type: "post", id: row.id },
      preview: snippet,
      context: { post_id: row.id },
      dedupeKey: `${isReply ? "post_reply" : "post_quote"}:${row.id}`,
      alert: {
        kind: "social",
        title: isReply ? `${name} 님의 답글` : `${name} 님이 글을 인용했어요`,
        body: snippet,
        target: postTarget(row.id),
      },
    });
  }
  for (const uid of mentionIds) {
    if (uid === author.id || notifiedOtherwise.has(uid)) continue;
    inputs.push({
      userId: uid,
      type: "post_mention",
      actorId: author.id,
      subject: { type: "post", id: row.id },
      preview: snippet,
      context: { post_id: row.id },
      dedupeKey: `post_mention:${row.id}:${uid}`,
      alert: { kind: "mention", title: `${name} 님이 글에서 멘션했어요`, body: snippet, target: postTarget(row.id) },
    });
  }
  if (inputs.length > 0) await scheduleNotifications(rt, "post notifications", () => notifyMany(rt, inputs));
  return { post: post!, created: true };
}

/** post.stats for a post whose counters just changed (topic watchers only). */
async function publishStats(rt: Runtime, postId: string): Promise<void> {
  const p = await first<Pick<PostRow, "id" | "like_count" | "repost_count" | "reply_count" | "quote_count">>(
    rt.env.DB,
    "SELECT id, like_count, repost_count, reply_count, quote_count FROM posts WHERE id = ?1",
    postId,
  );
  if (p === null) return;
  await publishEvent(
    rt,
    { topic: `post:${p.id}` },
    "post.stats",
    { post_id: p.id, like_count: p.like_count, repost_count: p.repost_count, reply_count: p.reply_count, quote_count: p.quote_count },
    { ephemeral: true },
  );
}

const LIVE_POST = "EXISTS (SELECT 1 FROM posts WHERE id = ?1 AND deleted_at IS NULL)";

/**
 * Soft-deletes a post (api.md 5.15). Missing or already deleted -> 404. Not the author (and not asAdmin): 403 forbidden
 * when the actor can view it, else 404. One batch, every counter guarded on the post still being live and placed before
 * the soft delete: author post_count - 1, parent reply_count - 1, quoted quote_count - 1; then body '' + deleted_at /
 * deleted_by, post_hashtags / post_mentions / reposts rows removed (post_media rows stay; their uploads become removed).
 * After commit: the media leave R2, post.deleted (ephemeral) to the post topic and to active followers + the author,
 * post.stats to the parent / quoted post topics.
 */
export async function deletePost(rt: Runtime, postId: string, actorId: string, opts: { asAdmin?: boolean } = {}): Promise<void> {
  const db = rt.env.DB;
  const row = await first<PostRow>(db, "SELECT * FROM posts WHERE id = ?1", postId);
  if (row === null || row.deleted_at !== null) throw err.notFound("post");
  if (row.author_id !== actorId && opts.asAdmin !== true) {
    if (await canViewPost(db, actorId, row)) throw err.forbidden();
    throw err.notFound("post");
  }

  const t = now();
  const results = await batch(db, [
    stmt(db, `UPDATE users SET post_count = MAX(post_count - 1, 0) WHERE id = ?2 AND ${LIVE_POST}`, row.id, row.author_id),
    stmt(
      db,
      `UPDATE posts SET reply_count = MAX(reply_count - 1, 0)
        WHERE id = (SELECT reply_to_id FROM posts WHERE id = ?1 AND deleted_at IS NULL)`,
      row.id,
    ),
    stmt(
      db,
      `UPDATE posts SET quote_count = MAX(quote_count - 1, 0)
        WHERE id = (SELECT quote_of_id FROM posts WHERE id = ?1 AND deleted_at IS NULL)`,
      row.id,
    ),
    stmt(db, "UPDATE posts SET body = '', deleted_at = ?2, deleted_by = ?3 WHERE id = ?1 AND deleted_at IS NULL RETURNING id", row.id, t, actorId),
    stmt(db, "DELETE FROM post_hashtags WHERE post_id = ?1", row.id),
    stmt(db, "DELETE FROM post_mentions WHERE post_id = ?1", row.id),
    stmt(db, "DELETE FROM reposts WHERE post_id = ?1", row.id),
  ]);
  if (results[3]!.results.length === 0) return; // a concurrent delete won; it does the rest

  const media = await all<{ upload_id: string }>(db, "SELECT upload_id FROM post_media WHERE post_id = ?1", row.id);
  try {
    await removeUploads(rt.env, media.map((m) => m.upload_id));
  } catch (e) {
    console.error("posts: media removal failed", row.id, e); // rows stay non-removed; retried by cleanup
  }

  await publishEvent(rt, { topic: `post:${row.id}` }, "post.deleted", { post_id: row.id }, { ephemeral: true });
  await publishEvent(rt, { followers: row.author_id }, "post.deleted", { post_id: row.id }, { ephemeral: true, includeSelf: row.author_id });
  if (row.reply_to_id !== null) await publishStats(rt, row.reply_to_id);
  if (row.quote_of_id !== null) await publishStats(rt, row.quote_of_id);
}

// ---------------------------------------------------------------------------
// Likes and reposts (plan 02 Task 6, api.md 5.15). The caller (routes/posts.ts) has already checked that the post is
// live and viewable (404 otherwise). Counters use the guard pattern inside one batch so a concurrent duplicate toggle
// moves them once; the returned count is read in the same batch, after the write.

export interface LikeState {
  post_id: string;
  liked: boolean;
  like_count: number;
}

export interface RepostState {
  post_id: string;
  reposted: boolean;
  repost_count: number;
}

const LIKED = "EXISTS (SELECT 1 FROM post_likes WHERE post_id = ?1 AND user_id = ?2)";
const REPOSTED = "EXISTS (SELECT 1 FROM reposts WHERE post_id = ?1 AND user_id = ?2)";
const POST_LIVE = "EXISTS (SELECT 1 FROM posts WHERE id = ?1 AND deleted_at IS NULL)";

/** post_like / post_repost to the post author (notify skips self and block pairs); deduped per (post, actor). */
function reactionNotification(post: PostRow, actor: UserRow, type: "post_like" | "post_repost"): NotifyInput {
  const snippet = plainSnippet(post.body, 200);
  return {
    userId: post.author_id,
    type,
    actorId: actor.id,
    subject: { type: "post", id: post.id },
    preview: snippet,
    context: { post_id: post.id },
    dedupeKey: `${type}:${post.id}:${actor.id}`,
    alert: {
      kind: "social",
      title: type === "post_like" ? `${actor.display_name} 님이 글을 좋아해요` : `${actor.display_name} 님이 글을 리포스트했어요`,
      body: snippet,
      target: postTarget(post.id),
    },
  };
}

/**
 * PUT (on = true) / DELETE (on = false) /posts/:id/like. Only a real state change publishes post.stats on the post topic
 * and (on like) the post_like notification; unliking never removes that notification.
 */
export async function setLike(rt: Runtime, post: PostRow, userId: string, on: boolean): Promise<LikeState> {
  const db = rt.env.DB;
  const t = now();
  const stmts = on
    ? [
        stmt(db, `UPDATE posts SET like_count = like_count + 1 WHERE id = ?1 AND deleted_at IS NULL AND NOT ${LIKED}`, post.id, userId),
        stmt(db, `INSERT OR IGNORE INTO post_likes (post_id, user_id, created_at) SELECT ?1, ?2, ?3 WHERE ${POST_LIVE}`, post.id, userId, t),
      ]
    : [
        stmt(db, `UPDATE posts SET like_count = MAX(like_count - 1, 0) WHERE id = ?1 AND ${LIKED}`, post.id, userId),
        stmt(db, "DELETE FROM post_likes WHERE post_id = ?1 AND user_id = ?2", post.id, userId),
      ];
  const results = await batch(db, [...stmts, stmt(db, "SELECT like_count FROM posts WHERE id = ?1", post.id)]);
  const changed = results[1]!.meta.changes > 0;
  const like_count = (results[2]!.results[0] as { like_count: number } | undefined)?.like_count ?? 0;

  if (changed) {
    await publishStats(rt, post.id);
    if (on) {
      const actor = await first<UserRow>(db, "SELECT * FROM users WHERE id = ?1", userId);
      if (actor !== null) await scheduleNotifications(rt, "like notification", () => notifyMany(rt, [reactionNotification(post, actor, "post_like")]));
    }
  }
  return { post_id: post.id, liked: on, like_count };
}

/**
 * PUT (on = true) / DELETE (on = false) /posts/:id/repost. Reposting a private account's post -> 403 action_not_allowed
 * (anyone, the author included); undoing an existing repost is always allowed. A new repost row (id = newId) publishes
 * timeline.item_created (type "repost", ephemeral) to the reposter's active followers + the reposter through the
 * visibility filter (authors = reposter + post author + quoted author), post.stats on the post topic and post_repost.
 */
export async function setRepost(rt: Runtime, post: PostRow, userId: string, on: boolean): Promise<RepostState> {
  const db = rt.env.DB;
  const t = now();
  let author: UserRow | null = null;
  if (on) {
    author = await first<UserRow>(db, "SELECT * FROM users WHERE id = ?1", post.author_id);
    if (author === null) throw err.notFound("post");
    if (author.is_private === 1) throw err.forbidden("action_not_allowed", "posts of private accounts cannot be reposted");
  }
  const repostId = newId(t);
  const stmts = on
    ? [
        stmt(db, `UPDATE posts SET repost_count = repost_count + 1 WHERE id = ?1 AND deleted_at IS NULL AND NOT ${REPOSTED}`, post.id, userId),
        stmt(
          db,
          `INSERT OR IGNORE INTO reposts (id, post_id, user_id, created_at) SELECT ?3, ?1, ?2, ?4 WHERE ${POST_LIVE}`,
          post.id,
          userId,
          repostId,
          t,
        ),
      ]
    : [
        stmt(db, `UPDATE posts SET repost_count = MAX(repost_count - 1, 0) WHERE id = ?1 AND ${REPOSTED}`, post.id, userId),
        stmt(db, "DELETE FROM reposts WHERE post_id = ?1 AND user_id = ?2", post.id, userId),
      ];
  const results = await batch(db, [...stmts, stmt(db, "SELECT * FROM posts WHERE id = ?1", post.id)]);
  const changed = results[1]!.meta.changes > 0;
  const fresh = (results[2]!.results[0] as PostRow | undefined) ?? post;
  const repost_count = fresh.repost_count;

  if (changed) {
    if (on) {
      const quotedAuthorId =
        post.quote_of_id === null
          ? null
          : ((await first<{ author_id: string }>(db, "SELECT author_id FROM posts WHERE id = ?1", post.quote_of_id))?.author_id ?? null);
      const relatedIds = [...new Set([userId, post.author_id, ...(quotedAuthorId !== null ? [quotedAuthorId] : [])])];
      const [related, summaries] = await Promise.all([
        all<Pick<UserRow, "id" | "is_private">>(db, "SELECT id, is_private FROM users WHERE id IN (SELECT value FROM json_each(?1))", jsonList(relatedIds)),
        loadUserSummaries(db, [userId]),
      ]);
      const privateAuthors = related.filter((u) => u.is_private === 1).map((u) => u.id);
      const [eventPost] = await serializePosts(db, null, [fresh], { privateOk: privateAuthors });
      const reposter = summaries.get(userId);
      if (eventPost !== undefined && reposter !== undefined) {
        const item: TimelineItem = { id: repostId, type: "repost", post: eventPost, reposted_by: reposter, at: iso(t) };
        // Reply-parent authors the event names join the block filter too (serializePosts, events.md 4.8).
        const visibleTo: VisibilitySpec = { authors: [...new Set([...relatedIds, ...eventParentAuthorIds(eventPost)])], privateAuthors };
        await publishEvent(rt, { followers: userId }, "timeline.item_created", { item }, { ephemeral: true, includeSelf: userId, visibleTo });
      }
    }
    await publishStats(rt, post.id);
    if (on) {
      const actor = await first<UserRow>(db, "SELECT * FROM users WHERE id = ?1", userId);
      if (actor !== null) await scheduleNotifications(rt, "repost notification", () => notifyMany(rt, [reactionNotification(post, actor, "post_repost")]));
    }
  }
  return { post_id: post.id, reposted: on, repost_count };
}
