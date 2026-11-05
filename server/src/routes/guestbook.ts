// Guestbook (api.md 5.20, plan 02 Task 12): GET|POST /users/:user/guestbook, DELETE /guestbook/:entry_id.
// /users is a shared prefix, so requireAuth is a per-route argument on every route here.
// Secret entries are masked by serializeGuestbook (hidden for anyone but the owner and the author); the list itself
// drops deleted entries, entries by suspended authors and entries whose author has a block with the viewer.
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, isSuspendedNow, requireAuth, resolveUserRef } from "../lib/auth";
import { all, decodeCursor, encodeCursor, first, parseLimit, run } from "../lib/db";
import { err } from "../lib/errors";
import { isId, newId } from "../lib/ids";
import { notify } from "../lib/notify";
import { areFriends, canViewUserContent, guestbookEntryVisibilitySql, isBlockedEither } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { loadUserSummaries, serializeGuestbook } from "../lib/serialize";
import { plainSnippet } from "../lib/text";
import { now } from "../lib/time";
import { vJson, vQuery, zText } from "../lib/validate";
import type { GuestbookRow } from "../types/rows";

/** api.md 0.3: guestbook body 1..300 code points. */
export const GUESTBOOK_BODY_MAX = 300;

const CreateBody = z.object({
  body: zText(1, GUESTBOOK_BODY_MAX),
  is_secret: z.boolean().optional(),
});
const PageQuery = z.object({ cursor: z.string().optional(), limit: z.string().optional() });

export const guestbookRoutes = new Hono<AppEnv>();

/** api.md 3.4 owner content rules: block either way -> 404, private and not an active follower -> 403. Newest first. */
guestbookRoutes.get("/users/:user/guestbook", requireAuth, vQuery(PageQuery), async (c) => {
  const auth = getAuth(c);
  const db = c.env.DB;
  const owner = await resolveUserRef(db, c.req.param("user"), auth);
  const access = await canViewUserContent(db, auth.userId, owner.id);
  if (access === "hidden") throw err.notFound("user");
  if (access === "private") throw err.forbidden("private_account");
  const q = c.req.valid("query");
  const n = parseLimit(q.limit, 30, 100);
  const cur = decodeCursor(q.cursor);
  if (cur !== null && (typeof cur.k !== "string" || cur.k !== cur.i || !isId(cur.i))) throw err.invalid({ cursor: "invalid" });
  const vis = guestbookEntryVisibilitySql(auth.userId);
  const rows = await all<GuestbookRow>(
    db,
    `SELECT g.* FROM guestbook_entries g
      WHERE g.owner_id = ? AND (? IS NULL OR g.id < ?) AND ${vis.sql}
      ORDER BY g.id DESC LIMIT ?`,
    owner.id,
    cur?.i ?? null,
    cur?.i ?? null,
    ...vis.params,
    n + 1,
  );
  const page = rows.slice(0, n);
  const last = page.at(-1);
  const next_cursor = rows.length > n && last ? encodeCursor({ k: last.id, i: last.id }) : null;
  return c.json({ items: await serializeGuestbook(db, auth.userId, page), next_cursor });
});

/**
 * Anyone may write in their own guestbook. Owner suspended or a block either way -> 403 action_not_allowed; a private
 * owner accepts only active followers and friends (else 403 private_account). After the INSERT the owner gets a
 * guestbook_entry notification (notify skips my own guestbook).
 */
guestbookRoutes.post("/users/:user/guestbook", requireAuth, limit("RL_POST"), vJson(CreateBody), async (c) => {
  const auth = getAuth(c);
  const me = auth.userId;
  const db = c.env.DB;
  const owner = await resolveUserRef(db, c.req.param("user"), auth);
  const b = c.req.valid("json");
  const t = now();
  if (owner.id !== me) {
    if (isSuspendedNow(owner, t) || (await isBlockedEither(db, me, owner.id))) throw err.forbidden("action_not_allowed");
    if (owner.is_private === 1) {
      const following = await first(
        db,
        "SELECT 1 AS x FROM follows WHERE follower_id = ?1 AND followee_id = ?2 AND status = 'active'",
        me,
        owner.id,
      );
      if (following === null && !(await areFriends(db, me, owner.id))) throw err.forbidden("private_account");
    }
  }

  const row: GuestbookRow = {
    id: newId(t),
    owner_id: owner.id,
    author_id: me,
    body: b.body,
    is_secret: b.is_secret === true ? 1 : 0,
    created_at: t,
    deleted_at: null,
    deleted_by: null,
  };
  await run(
    db,
    `INSERT INTO guestbook_entries (id, owner_id, author_id, body, is_secret, created_at)
     VALUES (?1, ?2, ?3, ?4, ?5, ?6)`,
    row.id,
    row.owner_id,
    row.author_id,
    row.body,
    row.is_secret,
    row.created_at,
  );

  const [entry] = await serializeGuestbook(db, me, [row]);
  if (owner.id !== me) {
    const name = (await loadUserSummaries(db, [me])).get(me)?.display_name ?? auth.handle;
    await notify(rt(c), {
      userId: owner.id,
      type: "guestbook_entry",
      actorId: me,
      subject: { type: "guestbook_entry", id: row.id },
      preview: plainSnippet(row.body, 100),
      dedupeKey: `guestbook_entry:${row.id}`,
      alert: {
        kind: "social",
        title: `${name} 님의 방명록`,
        body: plainSnippet(row.body, 200),
        target: { type: "user", id: owner.id, channel_id: null, message_id: null, thread_root_id: null, post_id: null, space_id: null },
      },
    });
  }
  return c.json({ entry: entry! }, 201);
});

/**
 * Author or owner: soft delete (body '', deleted_at, deleted_by). A third party who can see the entry -> 403; one who
 * cannot (missing, already deleted, owner content hidden/private, author blocked or suspended) -> 404.
 */
guestbookRoutes.delete("/guestbook/:entry_id", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const entryId = c.req.param("entry_id");
  if (!isId(entryId)) throw err.notFound("guestbook entry");
  const entry = await first<GuestbookRow>(db, "SELECT * FROM guestbook_entries WHERE id = ?1 AND deleted_at IS NULL", entryId);
  if (entry === null) throw err.notFound("guestbook entry");
  if (entry.author_id !== me && entry.owner_id !== me) {
    const vis = guestbookEntryVisibilitySql(me);
    const listed = await first(db, `SELECT 1 AS x FROM guestbook_entries g WHERE g.id = ? AND ${vis.sql}`, entry.id, ...vis.params);
    if (listed === null || (await canViewUserContent(db, me, entry.owner_id)) !== "ok") throw err.notFound("guestbook entry");
    throw err.forbidden();
  }
  await run(
    db,
    "UPDATE guestbook_entries SET body = '', deleted_at = ?2, deleted_by = ?3 WHERE id = ?1 AND deleted_at IS NULL",
    entry.id,
    now(),
    me,
  );
  return c.body(null, 204);
});
