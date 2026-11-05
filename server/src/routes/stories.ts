// Stories (api.md 5.17, plan 02 Task 9): POST /stories, GET /stories/feed, GET /users/:user/stories,
// POST /stories/:story_id/view, GET /stories/:story_id/viewers, DELETE /stories/:story_id.
// /users is a shared prefix, so requireAuth is a per-route argument on every route here.
// A story is visible (visibleStorySql) only while unexpired and undeleted, so an expired story disappears at once even
// before the cleanup cron deletes it (data-model.md 8.1).
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth, resolveUserRef } from "../lib/auth";
import { all, batch, decodeCursor, encodeCursor, first, jsonList, parseLimit, run, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { isId, newId } from "../lib/ids";
import { canViewUserContent } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { publishEvent } from "../lib/realtime";
import { loadUserSummaries, serializeStories } from "../lib/serialize";
import { iso, now } from "../lib/time";
import { removeUploads } from "../lib/uploads";
import { vJson, vQuery, zId, zText } from "../lib/validate";
import type { Story, StoryGroup, UserSummary } from "../types/api";
import type { StoryRow, StoryViewRow, UploadRow } from "../types/rows";
import { errFields } from "../lib/log";

/** api.md 0.3: story body 0..200 code points. */
export const STORY_BODY_MAX = 200;
/** api.md 5.17 / meta story_ttl_sec: a story lives 24 hours. */
export const STORY_TTL_MS = 86_400_000;
/** api.md 5.17: at most 30 active stories per user. */
export const ACTIVE_STORIES_MAX = 30;
/** api.md 5.17: at most 100 groups in the feed. */
const FEED_GROUPS_MAX = 100;

const CreateBody = z.object({
  body: zText(0, STORY_BODY_MAX).optional(),
  media_upload_id: zId.optional(),
  bg: z.enum(["accent", "accent2", "accent3", "accent4", "plain"]).optional(),
});
const PageQuery = z.object({ cursor: z.string().optional(), limit: z.string().optional() });

/**
 * SQL fragment + params: story alias `s` is visible to the viewer at `t` — not deleted, unexpired (expires_at > t),
 * author not suspended, no block either way, author public OR the viewer OR an active follower (api.md 3.4).
 * Anonymous `?` placeholders: append `params` in order where the fragment is placed.
 */
function visibleStorySql(viewerId: string, t: number): { sql: string; params: unknown[] } {
  const sql = `(s.deleted_at IS NULL AND s.expires_at > ?
    AND EXISTS (SELECT 1 FROM users su
                 WHERE su.id = s.author_id
                   AND (su.suspended_at IS NULL OR (su.suspended_until IS NOT NULL AND su.suspended_until <= ?))
                   AND (su.is_private = 0 OR su.id = ?
                        OR EXISTS (SELECT 1 FROM follows sf
                                    WHERE sf.follower_id = ? AND sf.followee_id = su.id AND sf.status = 'active')))
    AND NOT EXISTS (SELECT 1 FROM blocks sb
                     WHERE (sb.blocker_id = ? AND sb.blocked_id = s.author_id)
                        OR (sb.blocker_id = s.author_id AND sb.blocked_id = ?)))`;
  return { sql, params: [t, t, viewerId, viewerId, viewerId, viewerId] };
}

/** A story the viewer may see right now, else 404 (missing, deleted, expired or hidden all look the same). */
async function requireVisibleStory(db: D1Database, viewerId: string, storyId: string): Promise<StoryRow> {
  if (!isId(storyId)) throw err.notFound("story");
  const vis = visibleStorySql(viewerId, now());
  const row = await first<StoryRow>(db, `SELECT s.* FROM stories s WHERE s.id = ? AND ${vis.sql}`, storyId, ...vis.params);
  if (row === null) throw err.notFound("story");
  return row;
}

const ACTIVE_COUNT = "(SELECT COUNT(*) FROM stories WHERE author_id = ?2 AND deleted_at IS NULL AND expires_at > ?6)";
const MEDIA_PENDING = `EXISTS (SELECT 1 FROM uploads
                               WHERE id = ?4 AND owner_id = ?2 AND purpose = 'story' AND status = 'pending')`;

export const storyRoutes = new Hono<AppEnv>();

/**
 * Body and/or media (both empty -> fields.body required); the media must be my own pending "story" upload
 * (else fields.media_upload_id invalid); 30 active stories already -> 422 limit_exceeded active_stories. One batch:
 * the INSERT is guarded on both rules (a concurrent create or attach makes it a no-op, then re-checked), then the upload
 * is attached as story:<id>. After commit: story.created (ephemeral) to active followers + me.
 */
storyRoutes.post("/stories", requireAuth, limit("RL_POST"), vJson(CreateBody), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const b = c.req.valid("json");
  const body = b.body ?? "";
  const mediaId = b.media_upload_id ?? null;
  if (body === "" && mediaId === null) throw err.invalid({ body: "required" });

  const checkMedia = async (): Promise<void> => {
    if (mediaId === null) return;
    const up = await first<Pick<UploadRow, "id">>(
      db,
      "SELECT id FROM uploads WHERE id = ?1 AND owner_id = ?2 AND purpose = 'story' AND status = 'pending'",
      mediaId,
      me,
    );
    if (up === null) throw err.invalid({ media_upload_id: "invalid" });
  };
  const checkActive = async (t: number): Promise<void> => {
    const row = await first<{ n: number }>(
      db,
      "SELECT COUNT(*) AS n FROM stories WHERE author_id = ?1 AND deleted_at IS NULL AND expires_at > ?2",
      me,
      t,
    );
    if ((row?.n ?? 0) >= ACTIVE_STORIES_MAX) throw err.limit("active_stories", ACTIVE_STORIES_MAX);
  };

  const t = now();
  await checkMedia();
  await checkActive(t);

  const row: StoryRow = {
    id: newId(t),
    author_id: me,
    body,
    media_upload_id: mediaId,
    bg: b.bg ?? "accent",
    created_at: t,
    expires_at: t + STORY_TTL_MS,
    deleted_at: null,
  };
  const stmts = [
    stmt(
      db,
      `INSERT INTO stories (id, author_id, body, media_upload_id, bg, created_at, expires_at)
       SELECT ?1, ?2, ?3, ?4, ?5, ?6, ?7
        WHERE ${ACTIVE_COUNT} < ${ACTIVE_STORIES_MAX} AND (?4 IS NULL OR ${MEDIA_PENDING})`,
      row.id,
      row.author_id,
      row.body,
      row.media_upload_id,
      row.bg,
      row.created_at,
      row.expires_at,
    ),
  ];
  if (mediaId !== null) {
    stmts.push(
      stmt(
        db,
        `UPDATE uploads SET status = 'attached', attached_to = ?2
          WHERE id = ?1 AND status = 'pending' AND EXISTS (SELECT 1 FROM stories WHERE id = ?3)`,
        mediaId,
        `story:${row.id}`,
        row.id,
      ),
    );
  }
  const results = await batch(db, stmts);
  if ((results[0]!.meta.changes ?? 0) === 0) {
    // lost a race: report whichever rule now fails
    await checkMedia();
    await checkActive(now());
    throw err.limit("active_stories", ACTIVE_STORIES_MAX);
  }

  const [story] = await serializeStories(db, me, [row]);
  await publishEvent(
    rt(c),
    { followers: me },
    "story.created",
    { user_id: me, story_id: row.id, expires_at: iso(row.expires_at) },
    { ephemeral: true, includeSelf: me },
  );
  return c.json({ story: story! }, 201);
});

/**
 * Unexpired stories of me + my active followees that I may see, grouped per author: my group first, then groups with an
 * unseen story, then the rest, each part by latest_at desc (ties: newest story id); at most 100 groups; stories inside a
 * group oldest first. Two queries at the same `t`: the ordered group list, then the stories of those authors.
 */
storyRoutes.get("/stories/feed", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const t = now();
  const vis = visibleStorySql(me, t);
  const groups = await all<{ author_id: string }>(
    db,
    `SELECT s.author_id,
            MAX(s.created_at) AS latest_at,
            MAX(s.id) AS latest_id,
            MAX(CASE WHEN s.author_id <> ?
                      AND NOT EXISTS (SELECT 1 FROM story_views v WHERE v.story_id = s.id AND v.viewer_id = ?)
                     THEN 1 ELSE 0 END) AS has_unseen
       FROM stories s
      WHERE (s.author_id = ?
             OR EXISTS (SELECT 1 FROM follows f WHERE f.follower_id = ? AND f.followee_id = s.author_id AND f.status = 'active'))
        AND ${vis.sql}
      GROUP BY s.author_id
      ORDER BY (s.author_id = ?) DESC, has_unseen DESC, latest_at DESC, latest_id DESC
      LIMIT ?`,
    me,
    me,
    me,
    me,
    ...vis.params,
    me,
    FEED_GROUPS_MAX,
  );
  if (groups.length === 0) return c.json({ items: [] as StoryGroup[] });
  const authorIds = groups.map((g) => g.author_id);
  const rows = await all<StoryRow>(
    db,
    `SELECT s.* FROM stories s
      WHERE s.author_id IN (SELECT value FROM json_each(?)) AND ${vis.sql}
      ORDER BY s.created_at ASC, s.id ASC`,
    jsonList(authorIds),
    ...vis.params,
  );
  const stories = await serializeStories(db, me, rows);
  const byAuthor = new Map<string, Story[]>();
  for (const s of stories) {
    const list = byAuthor.get(s.author.id);
    if (list) list.push(s);
    else byAuthor.set(s.author.id, [s]);
  }
  const items: StoryGroup[] = [];
  for (const id of authorIds) {
    const list = byAuthor.get(id);
    if (!list || list.length === 0) continue;
    items.push({
      user: list[0]!.author,
      stories: list,
      has_unseen: list.some((s) => !s.seen),
      latest_at: list[list.length - 1]!.created_at,
    });
  }
  return c.json({ items });
});

/** api.md 3.4: block either way -> 404, private and not an active follower -> 403 private_account. Oldest first. */
storyRoutes.get("/users/:user/stories", requireAuth, async (c) => {
  const auth = getAuth(c);
  const db = c.env.DB;
  const owner = await resolveUserRef(db, c.req.param("user"), auth);
  const access = await canViewUserContent(db, auth.userId, owner.id);
  if (access === "hidden") throw err.notFound("user");
  if (access === "private") throw err.forbidden("private_account");
  const vis = visibleStorySql(auth.userId, now());
  const rows = await all<StoryRow>(
    db,
    `SELECT s.* FROM stories s WHERE s.author_id = ? AND ${vis.sql} ORDER BY s.created_at ASC, s.id ASC`,
    owner.id,
    ...vis.params,
  );
  return c.json({ items: await serializeStories(db, auth.userId, rows) });
});

/** Marks a visible story seen (idempotent). The author's own views are not recorded. */
storyRoutes.post("/stories/:story_id/view", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const story = await requireVisibleStory(db, me, c.req.param("story_id"));
  if (story.author_id !== me) {
    await run(db, "INSERT OR IGNORE INTO story_views (story_id, viewer_id, viewed_at) VALUES (?1, ?2, ?3)", story.id, me, now());
  }
  return c.body(null, 204);
});

/** Author only (403 for other viewers, 404 when not visible). viewed_at desc, cursor {k: viewed_at, i: viewer_id}. */
storyRoutes.get("/stories/:story_id/viewers", requireAuth, vQuery(PageQuery), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const story = await requireVisibleStory(db, me, c.req.param("story_id"));
  if (story.author_id !== me) throw err.forbidden();
  const q = c.req.valid("query");
  const n = parseLimit(q.limit, 30, 100);
  const cur = decodeCursor(q.cursor);
  if (cur !== null && (typeof cur.k !== "number" || !isId(cur.i))) throw err.invalid({ cursor: "invalid" });
  const rows = await all<StoryViewRow>(
    db,
    `SELECT * FROM story_views
      WHERE story_id = ?1 AND (?2 IS NULL OR viewed_at < ?2 OR (viewed_at = ?2 AND viewer_id < ?3))
      ORDER BY viewed_at DESC, viewer_id DESC LIMIT ?4`,
    story.id,
    cur?.k ?? null,
    cur?.i ?? null,
    n + 1,
  );
  const kept = rows.slice(0, n);
  const last = kept[kept.length - 1];
  const users = await loadUserSummaries(db, kept.map((r) => r.viewer_id));
  const items = kept.flatMap((r): Array<{ user: UserSummary; viewed_at: string }> => {
    const user = users.get(r.viewer_id);
    return user ? [{ user, viewed_at: iso(r.viewed_at) }] : [];
  });
  const next_cursor = rows.length > n && last ? encodeCursor({ k: last.viewed_at, i: last.viewer_id }) : null;
  return c.json({ items, next_cursor });
});

/**
 * Author only (403 for other viewers, 404 when not visible): deleted_at = now, then the media leave R2
 * (removeUploads; a failure is logged and left to cleanup), then story.deleted (ephemeral) to active followers + me.
 */
storyRoutes.delete("/stories/:story_id", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const story = await requireVisibleStory(db, me, c.req.param("story_id"));
  if (story.author_id !== me) throw err.forbidden();
  const res = await run(db, "UPDATE stories SET deleted_at = ?2 WHERE id = ?1 AND deleted_at IS NULL", story.id, now());
  if ((res.meta.changes ?? 0) === 0) return c.body(null, 204); // a concurrent delete won; it does the rest
  if (story.media_upload_id !== null) {
    try {
      await removeUploads(c.env, [story.media_upload_id]);
    } catch (e) {
      console.error("stories: media removal failed", { story_id: story.id, err: errFields(e) });
    }
  }
  await publishEvent(
    rt(c),
    { followers: me },
    "story.deleted",
    { user_id: me, story_id: story.id },
    { ephemeral: true, includeSelf: me },
  );
  return c.body(null, 204);
});
