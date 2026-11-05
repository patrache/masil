// Notification inbox (api.md 5.18). Rows are written by lib/notify.ts; every unread_count here, in
// notification.created and in notifications.read comes from unreadNotificationCount (one definition).
// Rows whose actor I blocked or whose actor is suspended right now are hidden from the list and from the count
// (inboxRowVisibleSql — one rule for both).
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth } from "../lib/auth";
import { all, decodeCursor, encodeCursor, jsonList, parseLimit, run } from "../lib/db";
import { err } from "../lib/errors";
import { inboxRowVisibleSql, unreadNotificationCount } from "../lib/notify";
import { publishEvent } from "../lib/realtime";
import { serializeNotifications } from "../lib/serialize";
import { now } from "../lib/time";
import { vJson, vQuery, zId } from "../lib/validate";
import type { NotificationRow } from "../types/rows";

export const notificationRoutes = new Hono<AppEnv>();

const READ_IDS_MAX = 100;

const ListQuery = z.object({
  cursor: z.string().optional(),
  limit: z.string().optional(),
  unread_only: z.enum(["true", "false", "1", "0"]).optional(),
});

/** Exactly one of {ids: 1..100} | {all: true} | {before: ID}. */
const ReadBody = z
  .object({
    ids: z.array(zId).min(1).max(READ_IDS_MAX).optional(),
    all: z.literal(true).optional(),
    before: zId.optional(),
  })
  .refine((b) => [b.ids, b.all, b.before].filter((v) => v !== undefined).length === 1, {
    message: "exactly one of ids, all, before",
  });

/** Newest first by id; cursor {k: id, i: id}. */
notificationRoutes.get("/notifications", requireAuth, vQuery(ListQuery), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const qy = c.req.valid("query");
  const n = parseLimit(qy.limit, 30, 100);
  const cursor = decodeCursor(qy.cursor);
  if (cursor !== null && (typeof cursor.k !== "string" || cursor.k !== cursor.i)) throw err.invalid({ cursor: "invalid" });
  const unreadOnly = qy.unread_only === "true" || qy.unread_only === "1";

  const rows = await all<NotificationRow>(
    db,
    `SELECT n.* FROM notifications n
      WHERE n.user_id = ?1
        AND (?2 = 0 OR n.read_at IS NULL)
        AND (?3 IS NULL OR n.id < ?3)
        AND ${inboxRowVisibleSql("?1", "?4")}
      ORDER BY n.id DESC
      LIMIT ${n + 1}`,
    me,
    unreadOnly ? 1 : 0,
    cursor?.i ?? null,
    now(),
  );
  const page = rows.slice(0, n);
  const last = page.at(-1);
  const next_cursor = rows.length > n && last ? encodeCursor({ k: last.id, i: last.id }) : null;
  const [items, unread_count] = await Promise.all([serializeNotifications(db, page), unreadNotificationCount(db, me)]);
  return c.json({ items, next_cursor, unread_count });
});

notificationRoutes.get("/notifications/unread-count", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  return c.json({ unread_count: await unreadNotificationCount(c.env.DB, me) });
});

notificationRoutes.post("/notifications/read", requireAuth, vJson(ReadBody), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const body = c.req.valid("json");
  const ids = body.ids === undefined ? null : [...new Set(body.ids)];
  const before = body.before ?? null;
  const allRead = body.all === true;

  await run(
    db,
    `UPDATE notifications SET read_at = ?2
      WHERE user_id = ?1 AND read_at IS NULL
        AND (?3 IS NULL OR id IN (SELECT value FROM json_each(?3)))
        AND (?4 IS NULL OR id <= ?4)`,
    me,
    now(),
    ids === null ? null : jsonList(ids),
    before,
  );
  const unread_count = await unreadNotificationCount(db, me);
  await publishEvent(rt(c), { users: [me] }, "notifications.read", { ids, all: allRead, before, unread_count });
  return c.json({ unread_count });
});
