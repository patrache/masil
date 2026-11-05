// Message services (server-architecture.md 4.14 messages.ts). Task 20: createMessage (top-level) and
// postSystemMessage. Task 24: mention / @here resolution (api.md 2.1), notification fan-out, editMessage, deleteMessage.
// Task 25: thread replies (threadRootId, api.md 5.11). Each write is one db.batch, then publish (server-architecture.md 3.5).
import type { Runtime } from "../env";
import { all, batch, first, jsonList, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { newId } from "../lib/ids";
import { fanoutMessageNotifications, notifyMessageMentions } from "../lib/notify";
import { canModerateChannel, getChannelAccess, requireChannelRead } from "../lib/permissions";
import { publishEvent, publishEvents } from "../lib/realtime";
import { serializeMessages } from "../lib/serialize";
import { charCount, extractMentions, hasHereMention, normalizeText, parseDice, rollDice } from "../lib/text";
import { isoOrNull, now } from "../lib/time";
import type { Message, PollInput, RollResult, SystemEvent, ThreadSummary } from "../types/api";
import type { ChannelRow, MessageRow } from "../types/rows";
import { pollInsertStmts } from "./polls";
import { errFields } from "../lib/log";

/** api.md 0.3 message body 1..4000 code points (after NFC + trim). */
export const MESSAGE_BODY_MAX = 4000;

export interface CreateMessageInput {
  channelId: string;
  authorId: string;
  kind: "text" | "me" | "roll" | "poll";
  body: string;
  clientNonce: string;
  threadRootId?: string | null;
  poll?: PollInput;
}

/**
 * Validates the body for `kind` and returns what is stored (api.md 5.10):
 * text / me -> NFC + trim, 1..4000 code points (fields.body too_short / too_long);
 * roll -> a valid dice expression (fields.body invalid) and its RollResult (stored in meta);
 * poll -> body ignored (""), `poll` required (fields.poll required).
 */
export function prepareMessageBody(kind: CreateMessageInput["kind"], rawBody: string, poll?: PollInput): { body: string; roll: RollResult | null } {
  if (kind === "poll") {
    if (poll === undefined) throw err.invalid({ poll: "required" });
    return { body: "", roll: null };
  }
  const body = normalizeText(rawBody);
  const n = charCount(body);
  if (n < 1) throw err.invalid({ body: "too_short" });
  if (n > MESSAGE_BODY_MAX) throw err.invalid({ body: "too_long" });
  if (kind === "roll") {
    const spec = parseDice(body);
    if (spec === null) throw err.invalid({ body: "invalid" });
    return { body, roll: rollDice(spec) };
  }
  return { body, roll: null };
}

// ---------------------------------------------------------------------------
// mentions (api.md 2.1)

/**
 * Explicit `@handle` mentions (not `@here`) that resolve: the user exists, can read the channel (getChannelAccess per
 * candidate, <= 50) and has no block either way with the author. Body order. The author may be among them.
 */
export async function resolveExplicitMentions(db: D1Database, channel: ChannelRow, authorId: string, body: string): Promise<string[]> {
  const handles = extractMentions(body).filter((h) => h !== "here");
  if (handles.length === 0) return [];
  const rows = await all<{ id: string }>(
    db,
    `SELECT u.id FROM json_each(?1) j JOIN users u ON u.handle = j.value
      WHERE NOT EXISTS (SELECT 1 FROM blocks b
                         WHERE (b.blocker_id = ?2 AND b.blocked_id = u.id) OR (b.blocker_id = u.id AND b.blocked_id = ?2))
      ORDER BY j.key`,
    jsonList(handles),
    authorId,
  );
  const access = await Promise.all(rows.map((r) => getChannelAccess(db, channel.id, r.id)));
  return rows.filter((_, i) => access[i]?.canRead === true).map((r) => r.id);
}

/**
 * `@here` expansion in one statement: readers of the channel (public text = live space members; private text =
 * channel_members ∩ live space members; group_dm = channel_members) with user_presence.status online/away, minus the
 * author, block pairs (either way) with the author and the explicit mentions. Never for a 1:1 dm (caller's job).
 */
async function resolveHere(db: D1Database, channel: ChannelRow, authorId: string, explicit: string[]): Promise<string[]> {
  let readers: string;
  if (channel.kind === "text" && channel.is_private === 0) {
    readers = `SELECT sm.user_id FROM space_members sm JOIN spaces s ON s.id = sm.space_id AND s.deleted_at IS NULL
                WHERE sm.space_id = ?2`;
  } else if (channel.kind === "text") {
    readers = `SELECT cm.user_id FROM channel_members cm
                 JOIN space_members sm ON sm.space_id = ?2 AND sm.user_id = cm.user_id
                 JOIN spaces s ON s.id = sm.space_id AND s.deleted_at IS NULL
                WHERE cm.channel_id = ?1`;
  } else {
    readers = "SELECT cm.user_id FROM channel_members cm WHERE cm.channel_id = ?1";
  }
  const rows = await all<{ user_id: string }>(
    db,
    `SELECT r.user_id FROM (${readers}) r
       JOIN user_presence p ON p.user_id = r.user_id AND p.status IN ('online', 'away')
      WHERE r.user_id <> ?3
        AND r.user_id NOT IN (SELECT value FROM json_each(?4))
        AND NOT EXISTS (SELECT 1 FROM blocks b
                         WHERE (b.blocker_id = ?3 AND b.blocked_id = r.user_id) OR (b.blocker_id = r.user_id AND b.blocked_id = ?3))
      ORDER BY r.user_id`,
    channel.id,
    channel.space_id,
    authorId,
    jsonList(explicit),
  );
  return rows.map((r) => r.user_id);
}

/** One INSERT … SELECT FROM json_each for any number of mention rows; an explicit row replaces an @here one. */
function mentionInsertStmt(db: D1Database, messageId: string, channelId: string, userIds: string[], isHere: boolean, inThread: boolean): D1PreparedStatement {
  return stmt(
    db,
    `INSERT INTO message_mentions (message_id, user_id, channel_id, is_here, in_thread)
     SELECT ?1, value, ?2, ?3, ?4 FROM json_each(?5) WHERE true
     ON CONFLICT (message_id, user_id) DO UPDATE SET is_here = MIN(message_mentions.is_here, excluded.is_here)`,
    messageId,
    channelId,
    isHere ? 1 : 0,
    inThread ? 1 : 0,
    jsonList(userIds),
  );
}

function loadChannel(db: D1Database, channelId: string): Promise<ChannelRow | null> {
  return first<ChannelRow>(db, "SELECT * FROM channels WHERE id = ?1", channelId);
}

/** Notification fan-out after commit + publish: awaited in tests, waitUntil otherwise; failures are logged only. */
function scheduleNotifications(rt: Runtime, label: string, work: () => Promise<void>): Promise<void> {
  const p = work().catch((e) => console.error({ msg: "messages background task failed", label, err: errFields(e) }));
  if (rt.env.APP_ENV === "test") return p;
  rt.waitUntil(p);
  return Promise.resolve();
}

function findByNonce(db: D1Database, authorId: string, clientNonce: string): Promise<MessageRow | null> {
  return first<MessageRow>(db, "SELECT * FROM messages WHERE author_id = ?1 AND client_nonce = ?2", authorId, clientNonce);
}

function isUniqueViolation(e: unknown): boolean {
  return e instanceof Error && /UNIQUE constraint failed/i.test(e.message);
}

/**
 * Creates a message in a channel the author may write to (the caller checked requireChannelWrite).
 * Top-level, one batch: (poll -> pollInsertStmts) + messages INSERT + channels.last_message_id/at (only when newer) +
 * the author's channel_reads moved to this message (only when newer) + message_mentions (explicit + @here).
 * Thread reply (threadRootId = a live top-level message of this channel, else 404; poll -> 400 fields.kind), one batch:
 * messages INSERT + root reply_count+1 / last_reply_at + thread_participants upsert (the replier at now; on the first
 * reply also the root author at the root's created_at, so the replier sorts as more recent) + message_mentions
 * (in_thread = 1). The channel's last_message_* and read pointers are left alone (replies never count as unread).
 * Same author + same client_nonce -> the existing message, created:false (pre-check, and uq_messages_nonce for races).
 * After commit: message.created{message} (+ thread.updated{channel_id, root_id, thread} for a reply) to the channel
 * audience (logged), then notifications (fanoutMessageNotifications).
 */
export async function createMessage(rt: Runtime, input: CreateMessageInput): Promise<{ message: Message; created: boolean }> {
  const db = rt.env.DB;
  const rootId = input.threadRootId ?? null;

  const existing = await findByNonce(db, input.authorId, input.clientNonce);
  if (existing !== null) {
    const [message] = await serializeMessages(db, input.authorId, [existing]);
    return { message: message!, created: false };
  }

  if (rootId !== null && input.kind === "poll") throw err.invalid({ kind: "not_allowed" });
  const { body, roll } = prepareMessageBody(input.kind, input.body, input.poll);
  const [channel, root] = await Promise.all([
    loadChannel(db, input.channelId),
    rootId === null ? Promise.resolve(null) : first<MessageRow>(db, "SELECT * FROM messages WHERE id = ?1", rootId),
  ]);
  if (channel === null || channel.deleted_at !== null) throw err.notFound("channel");
  if (rootId !== null && (root === null || root.deleted_at !== null || root.thread_root_id !== null || root.channel_id !== channel.id)) {
    throw err.notFound("message");
  }
  const explicit = await resolveExplicitMentions(db, channel, input.authorId, body);
  const mentionHere = hasHereMention(body);
  const here = mentionHere && channel.kind !== "dm" ? await resolveHere(db, channel, input.authorId, explicit) : [];
  const t = now();
  const id = newId(t);
  const pollId = input.kind === "poll" ? newId(t) : null;
  const row: MessageRow = {
    id,
    channel_id: input.channelId,
    author_id: input.authorId,
    kind: input.kind,
    body,
    meta: roll === null ? null : JSON.stringify(roll),
    thread_root_id: rootId,
    reply_count: 0,
    last_reply_at: null,
    mention_here: mentionHere ? 1 : 0,
    poll_id: pollId,
    client_nonce: input.clientNonce,
    created_at: t,
    edited_at: null,
    deleted_at: null,
    deleted_by: null,
  };

  const stmts: D1PreparedStatement[] = [];
  // polls first: messages.poll_id references polls(id)
  if (pollId !== null) stmts.push(...pollInsertStmts(db, pollId, input.authorId, { type: "message", id }, input.poll!, t));
  stmts.push(
    stmt(
      db,
      `INSERT INTO messages (id, channel_id, author_id, kind, body, meta, thread_root_id, mention_here, poll_id, client_nonce, created_at)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11)`,
      row.id,
      row.channel_id,
      row.author_id,
      row.kind,
      row.body,
      row.meta,
      row.thread_root_id,
      row.mention_here,
      row.poll_id,
      row.client_nonce,
      row.created_at,
    ),
  );
  if (root === null) {
    stmts.push(
      lastMessageStmt(db, input.channelId, id, t),
      stmt(
        db,
        `INSERT INTO channel_reads (user_id, channel_id, last_read_message_id, updated_at) VALUES (?1, ?2, ?3, ?4)
         ON CONFLICT (user_id, channel_id) DO UPDATE SET last_read_message_id = excluded.last_read_message_id, updated_at = excluded.updated_at
          WHERE excluded.last_read_message_id > channel_reads.last_read_message_id`,
        input.authorId,
        input.channelId,
        id,
        t,
      ),
    );
  } else {
    stmts.push(
      stmt(db, "UPDATE messages SET reply_count = reply_count + 1, last_reply_at = MAX(COALESCE(last_reply_at, 0), ?2) WHERE id = ?1", root.id, t),
      stmt(
        db,
        `INSERT INTO thread_participants (root_id, user_id, last_reply_at) VALUES (?1, ?2, ?3)
         ON CONFLICT (root_id, user_id) DO UPDATE SET last_reply_at = MAX(thread_participants.last_reply_at, excluded.last_reply_at)`,
        root.id,
        input.authorId,
        t,
      ),
      // the root author joins with the first reply; a row that already exists (they replied) keeps its later time
      stmt(
        db,
        "INSERT INTO thread_participants (root_id, user_id, last_reply_at) VALUES (?1, ?2, ?3) ON CONFLICT (root_id, user_id) DO NOTHING",
        root.id,
        root.author_id,
        root.created_at,
      ),
    );
  }
  // message_mentions reference messages(id): after the INSERT
  const inThread = root !== null;
  if (explicit.length > 0) stmts.push(mentionInsertStmt(db, id, input.channelId, explicit, false, inThread));
  if (here.length > 0) stmts.push(mentionInsertStmt(db, id, input.channelId, here, true, inThread));

  try {
    await batch(db, stmts);
  } catch (e) {
    // a concurrent request with the same nonce won the insert: the whole batch rolled back, return the winner
    if (!isUniqueViolation(e)) throw e;
    const winner = await findByNonce(db, input.authorId, input.clientNonce);
    if (winner === null) throw e;
    const [message] = await serializeMessages(db, input.authorId, [winner]);
    return { message: message!, created: false };
  }

  // A brand-new message reads the same for everyone (no reactions, no votes): one serialization serves both.
  const [message] = await serializeMessages(db, null, [row]);
  if (root === null) {
    await publishEvent(rt, { channel: input.channelId }, "message.created", { message: message! });
  } else {
    const thread = await loadThreadSummary(db, root.id);
    const events: Array<{ type: "message.created" | "thread.updated"; data: unknown }> = [{ type: "message.created", data: { message: message! } }];
    if (thread !== null) events.push({ type: "thread.updated", data: { channel_id: input.channelId, root_id: root.id, thread } });
    await publishEvents(rt, { channel: input.channelId }, events);
  }
  await scheduleNotifications(rt, "message notifications", () => fanoutMessageNotifications(rt, message!, channel));
  return { message: message!, created: true };
}

/** channels.last_message_id/at move forward only (api.md 5.10: concurrent sends never move it back). */
function lastMessageStmt(db: D1Database, channelId: string, messageId: string, t: number): D1PreparedStatement {
  return stmt(
    db,
    "UPDATE channels SET last_message_id = ?2, last_message_at = ?3 WHERE id = ?1 AND (last_message_id IS NULL OR last_message_id < ?2)",
    channelId,
    messageId,
    t,
  );
}

/**
 * A kind:"system" message (no nonce, body "", meta = the SystemEvent JSON) by `actorId`, top-level in `channelId`.
 * One batch: messages INSERT + channels.last_message_* (only when newer). System messages never count as unread, so the
 * actor's read pointer is left alone. After commit: message.created{message} to the channel audience (logged).
 */
export async function postSystemMessage(rt: Runtime, channelId: string, actorId: string, system: SystemEvent): Promise<void> {
  const db = rt.env.DB;
  const t = now();
  const row: MessageRow = {
    id: newId(t),
    channel_id: channelId,
    author_id: actorId,
    kind: "system",
    body: "",
    meta: JSON.stringify(system),
    thread_root_id: null,
    reply_count: 0,
    last_reply_at: null,
    mention_here: 0,
    poll_id: null,
    client_nonce: null,
    created_at: t,
    edited_at: null,
    deleted_at: null,
    deleted_by: null,
  };
  await batch(db, [
    stmt(
      db,
      `INSERT INTO messages (id, channel_id, author_id, kind, body, meta, created_at)
       VALUES (?1, ?2, ?3, 'system', '', ?4, ?5)`,
      row.id,
      channelId,
      actorId,
      row.meta,
      t,
    ),
    lastMessageStmt(db, channelId, row.id, t),
  ]);
  const [message] = await serializeMessages(db, null, [row]);
  if (message) await publishEvent(rt, { channel: channelId }, "message.created", { message });
}

// ---------------------------------------------------------------------------
// edit / delete (api.md 5.10, 3.7)

async function loadLiveMessage(db: D1Database, messageId: string): Promise<MessageRow> {
  const row = await first<MessageRow>(db, "SELECT * FROM messages WHERE id = ?1", messageId);
  if (row === null || row.deleted_at !== null) throw err.notFound("message");
  return row;
}

/**
 * Author only, kind text / me only (else 403 forbidden); writing must still be allowed (1:1 dm can_dm, 403 read_only).
 * Mentions are resolved again (api.md 2.1): vanished explicit rows deleted, new ones inserted and notified (once per
 * person — dedupe key); @here is not expanded again, and its rows go when the body no longer says @here.
 * One batch, then message.updated{message} to the channel audience.
 */
export async function editMessage(rt: Runtime, messageId: string, actorId: string, body: string): Promise<Message> {
  const db = rt.env.DB;
  const row = await loadLiveMessage(db, messageId);
  const access = await requireChannelRead(db, row.channel_id, actorId);
  if (row.author_id !== actorId) throw err.forbidden("forbidden", "only the author can edit this message");
  if (row.kind !== "text" && row.kind !== "me") throw err.forbidden("forbidden", "this message kind cannot be edited");
  if (!access.canWrite) {
    throw err.forbidden("read_only", "this conversation is read-only", { reason: access.readOnlyReason ?? "not_eligible" });
  }
  const prepared = prepareMessageBody(row.kind, body);
  const channel = access.channel;
  const inThread = row.thread_root_id !== null;

  const [explicit, existing] = await Promise.all([
    resolveExplicitMentions(db, channel, actorId, prepared.body),
    all<{ user_id: string; is_here: number }>(db, "SELECT user_id, is_here FROM message_mentions WHERE message_id = ?1", messageId),
  ]);
  const mentionHere = hasHereMention(prepared.body);
  const prevExplicit = new Set(existing.filter((m) => m.is_here === 0).map((m) => m.user_id));
  const nextExplicit = new Set(explicit);
  const removed = [...prevExplicit].filter((u) => !nextExplicit.has(u));
  const added = explicit.filter((u) => !prevExplicit.has(u));
  const t = now();

  const stmts: D1PreparedStatement[] = [
    stmt(db, "UPDATE messages SET body = ?2, mention_here = ?3, edited_at = ?4 WHERE id = ?1", messageId, prepared.body, mentionHere ? 1 : 0, t),
  ];
  if (removed.length > 0) {
    stmts.push(
      stmt(db, "DELETE FROM message_mentions WHERE message_id = ?1 AND is_here = 0 AND user_id IN (SELECT value FROM json_each(?2))", messageId, jsonList(removed)),
    );
  }
  if (!mentionHere) stmts.push(stmt(db, "DELETE FROM message_mentions WHERE message_id = ?1 AND is_here = 1", messageId));
  if (added.length > 0) stmts.push(mentionInsertStmt(db, messageId, row.channel_id, added, false, inThread));
  await batch(db, stmts);

  const updated: MessageRow = { ...row, body: prepared.body, mention_here: mentionHere ? 1 : 0, edited_at: t };
  const [[forActor], [forEvent]] = await Promise.all([serializeMessages(db, actorId, [updated]), serializeMessages(db, null, [updated])]);
  if (!forActor || !forEvent) throw err.notFound("message");
  await publishEvent(rt, { channel: row.channel_id }, "message.updated", { message: forEvent });
  if (added.length > 0) await scheduleNotifications(rt, "edit mention notifications", () => notifyMessageMentions(rt, forEvent, channel, added));
  return forActor;
}

/** A root's ThreadSummary (participants most recent first, <= 5) even at reply_count 0, for thread.updated. */
async function loadThreadSummary(db: D1Database, rootId: string): Promise<ThreadSummary | null> {
  const [root, parts] = await Promise.all([
    first<{ reply_count: number; last_reply_at: number | null }>(db, "SELECT reply_count, last_reply_at FROM messages WHERE id = ?1", rootId),
    all<{ user_id: string }>(db, "SELECT user_id FROM thread_participants WHERE root_id = ?1 ORDER BY last_reply_at DESC, user_id LIMIT 5", rootId),
  ]);
  if (root === null) return null;
  return { reply_count: root.reply_count, last_reply_at: isoOrNull(root.last_reply_at), participant_ids: parts.map((p) => p.user_id) };
}

/**
 * Author, an owner/admin of the channel's space (canModerateChannel) or a global admin (`asAdmin`); else 403 forbidden.
 * One batch: soft delete (deleted_at, deleted_by, body '') + reactions, pin and mention rows deleted; a reply also
 * decrements its root's reply_count (never below 0). Thread replies of a deleted root stay.
 * After commit: message.deleted{channel_id, message_id, thread_root_id, tombstone} (tombstone = root with replies)
 * and, for a reply, thread.updated{channel_id, root_id, thread} — both to the channel audience.
 */
export async function deleteMessage(rt: Runtime, messageId: string, actorId: string, opts: { asAdmin?: boolean } = {}): Promise<void> {
  const db = rt.env.DB;
  const row = await loadLiveMessage(db, messageId);
  if (opts.asAdmin !== true) {
    const access = await requireChannelRead(db, row.channel_id, actorId);
    if (row.author_id !== actorId && !canModerateChannel(access)) throw err.forbidden("forbidden", "cannot delete this message");
  }
  const t = now();
  const stmts: D1PreparedStatement[] = [
    stmt(db, "UPDATE messages SET deleted_at = ?2, deleted_by = ?3, body = '' WHERE id = ?1 AND deleted_at IS NULL", messageId, t, actorId),
  ];
  if (row.thread_root_id !== null) {
    // only when this batch did the soft delete (a concurrent delete must not decrement twice)
    stmts.push(
      stmt(
        db,
        `UPDATE messages SET reply_count = MAX(reply_count - 1, 0)
          WHERE id = ?1 AND EXISTS (SELECT 1 FROM messages r WHERE r.id = ?2 AND r.deleted_at = ?3 AND r.deleted_by = ?4)`,
        row.thread_root_id,
        messageId,
        t,
        actorId,
      ),
    );
  }
  stmts.push(
    stmt(db, "DELETE FROM message_reactions WHERE message_id = ?1", messageId),
    stmt(db, "DELETE FROM message_pins WHERE message_id = ?1", messageId),
    stmt(db, "DELETE FROM message_mentions WHERE message_id = ?1", messageId),
  );
  const [softDelete] = await batch(db, stmts);
  if (softDelete!.meta.changes === 0) return; // a concurrent delete won; it publishes

  const events: Array<{ type: "message.deleted" | "thread.updated"; data: unknown }> = [
    {
      type: "message.deleted",
      data: {
        channel_id: row.channel_id,
        message_id: row.id,
        thread_root_id: row.thread_root_id,
        tombstone: row.thread_root_id === null && row.reply_count > 0,
      },
    },
  ];
  if (row.thread_root_id !== null) {
    const thread = await loadThreadSummary(db, row.thread_root_id);
    if (thread !== null) events.push({ type: "thread.updated", data: { channel_id: row.channel_id, root_id: row.thread_root_id, thread } });
  }
  await publishEvents(rt, { channel: row.channel_id }, events);
}
