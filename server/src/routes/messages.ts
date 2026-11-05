// Messages (api.md 5.10). requireAuth is a per-route argument (plan 01 Global Constraints).
// Task 20: send, list, get. Task 24 adds PATCH/DELETE, Task 25 threads, Task 26 reactions and pins.
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth } from "../lib/auth";
import { all, first, parseLimit } from "../lib/db";
import { err, type FieldError } from "../lib/errors";
import { requireChannelRead, requireChannelWrite } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { serializeMessages } from "../lib/serialize";
import { vJson, vQuery, zId, zNonce, zPollInput } from "../lib/validate";
import { createMessage } from "../services/messages";
import type { MessageRow } from "../types/rows";

const SendBody = z.object({
  client_nonce: zNonce,
  kind: z.enum(["text", "me", "roll", "poll"]).optional(),
  body: z.string().optional(), // validated per kind in services/messages.ts (prepareMessageBody)
  poll: zPollInput.optional(),
});

const ListQuery = z.object({
  before: zId.optional(),
  after: zId.optional(),
  around: zId.optional(),
  limit: z.string().optional(),
});

const LIST_DEFAULT = 50;
const LIST_MAX = 100;

// Top-level messages that the list shows: deleted ones only as tombstones of roots with replies (api.md 5.10).
const VISIBLE = "channel_id = ?1 AND thread_root_id IS NULL AND (deleted_at IS NULL OR reply_count > 0)";

export const messageRoutes = new Hono<AppEnv>();

messageRoutes.post("/channels/:channel_id/messages", requireAuth, limit("RL_MESSAGE"), vJson(SendBody), async (c) => {
  const me = getAuth(c).userId;
  const access = await requireChannelWrite(c.env.DB, c.req.param("channel_id"), me);
  const b = c.req.valid("json");
  const kind = b.kind ?? "text";
  if (b.body === undefined && kind !== "poll") throw err.invalid({ body: "required" });
  const { message, created } = await createMessage(rt(c), {
    channelId: access.channel.id,
    authorId: me,
    kind,
    body: b.body ?? "",
    clientNonce: b.client_nonce,
    poll: b.poll,
  });
  return c.json({ message }, created ? 201 : 200);
});

/**
 * Top-level messages, always ID ascending (api.md 5.10). At most one of before / after / around (else 400).
 * none: the latest `limit`; before: older than it; after: newer than it; around: ceil(limit/2) at-or-before the anchor
 * plus floor(limit/2) after it. has_more_before / has_more_after say whether visible messages exist past either end.
 */
messageRoutes.get("/channels/:channel_id/messages", requireAuth, vQuery(ListQuery), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const q = c.req.valid("query");
  const anchors = (["before", "after", "around"] as const).filter((k) => q[k] !== undefined);
  if (anchors.length > 1) {
    const fields: Record<string, FieldError> = {};
    for (const k of anchors) fields[k] = "not_allowed";
    throw err.invalid(fields);
  }
  const n = parseLimit(q.limit, LIST_DEFAULT, LIST_MAX);
  const access = await requireChannelRead(db, c.req.param("channel_id"), me);
  const ch = access.channel.id;

  const older = (anchor: string | null, inclusive: boolean, count: number) =>
    all<MessageRow>(
      db,
      `SELECT * FROM messages WHERE ${VISIBLE} AND (?2 IS NULL OR id ${inclusive ? "<=" : "<"} ?2) ORDER BY id DESC LIMIT ?3`,
      ch,
      anchor,
      count,
    );
  const newer = (anchor: string, count: number) =>
    all<MessageRow>(db, `SELECT * FROM messages WHERE ${VISIBLE} AND id > ?2 ORDER BY id ASC LIMIT ?3`, ch, anchor, count);
  const exists = async (op: "<" | ">" | "<=" | ">=", id: string): Promise<boolean> =>
    (await first<{ x: number }>(db, `SELECT 1 AS x FROM messages WHERE ${VISIBLE} AND id ${op} ?2 LIMIT 1`, ch, id)) !== null;

  let rows: MessageRow[];
  let hasBefore: boolean;
  let hasAfter: boolean;
  if (q.after !== undefined) {
    const got = await newer(q.after, n + 1);
    rows = got.slice(0, n);
    hasAfter = got.length > n;
    hasBefore = await exists(rows.length > 0 ? "<" : "<=", rows[0]?.id ?? q.after);
  } else if (q.around !== undefined) {
    const nBefore = Math.ceil(n / 2);
    const nAfter = Math.floor(n / 2);
    const [down, up] = await Promise.all([older(q.around, true, nBefore + 1), nAfter > 0 ? newer(q.around, nAfter + 1) : Promise.resolve([])]);
    hasBefore = down.length > nBefore;
    rows = [...down.slice(0, nBefore).reverse(), ...up.slice(0, nAfter)];
    hasAfter = nAfter > 0 ? up.length > nAfter : await exists(">", q.around);
  } else {
    const got = await older(q.before ?? null, false, n + 1);
    hasBefore = got.length > n;
    rows = got.slice(0, n).reverse();
    const last = rows[rows.length - 1];
    hasAfter = q.before === undefined ? false : await exists(last ? ">" : ">=", last?.id ?? q.before);
  }

  const items = await serializeMessages(db, me, rows);
  return c.json({ items, has_more_before: hasBefore, has_more_after: hasAfter });
});

/** One message (thread replies too) for a reader of its channel; deleted ones only as tombstones of roots with replies. */
messageRoutes.get("/messages/:message_id", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const row = await first<MessageRow>(db, "SELECT * FROM messages WHERE id = ?1", c.req.param("message_id"));
  if (row === null) throw err.notFound("message");
  await requireChannelRead(db, row.channel_id, me);
  if (row.deleted_at !== null && !(row.thread_root_id === null && row.reply_count > 0)) throw err.notFound("message");
  const [message] = await serializeMessages(db, me, [row]);
  if (!message) throw err.notFound("message");
  return c.json({ message });
});
