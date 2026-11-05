// Message services (server-architecture.md 4.14 messages.ts). Task 20: createMessage (top-level) and
// postSystemMessage. Mention resolution / notification fan-out, editMessage and deleteMessage are Task 24;
// thread replies (threadRootId) are Task 25. Each write is one db.batch, then publish (server-architecture.md 3.5).
import type { Runtime } from "../env";
import { batch, first, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { newId } from "../lib/ids";
import { publishEvent } from "../lib/realtime";
import { serializeMessages } from "../lib/serialize";
import { charCount, normalizeText, parseDice, rollDice } from "../lib/text";
import { now } from "../lib/time";
import type { Message, PollInput, RollResult, SystemEvent } from "../types/api";
import type { MessageRow } from "../types/rows";
import { pollInsertStmts } from "./polls";

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

function findByNonce(db: D1Database, authorId: string, clientNonce: string): Promise<MessageRow | null> {
  return first<MessageRow>(db, "SELECT * FROM messages WHERE author_id = ?1 AND client_nonce = ?2", authorId, clientNonce);
}

function isUniqueViolation(e: unknown): boolean {
  return e instanceof Error && /UNIQUE constraint failed/i.test(e.message);
}

/**
 * Creates a top-level message in a channel the author may write to (the caller checked requireChannelWrite).
 * One batch: (poll -> pollInsertStmts) + messages INSERT + channels.last_message_id/at (only when newer) + the author's
 * channel_reads moved to this message (only when newer). Mention rows: Task 24.
 * Same author + same client_nonce -> the existing message, created:false (pre-check, and uq_messages_nonce for races).
 * After commit: message.created{message} to the channel audience (logged).
 */
export async function createMessage(rt: Runtime, input: CreateMessageInput): Promise<{ message: Message; created: boolean }> {
  const db = rt.env.DB;
  if (input.threadRootId !== undefined && input.threadRootId !== null) throw new Error("thread replies are not implemented yet (plan 01 Task 25)");

  const existing = await findByNonce(db, input.authorId, input.clientNonce);
  if (existing !== null) {
    const [message] = await serializeMessages(db, input.authorId, [existing]);
    return { message: message!, created: false };
  }

  const { body, roll } = prepareMessageBody(input.kind, input.body, input.poll);
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
    thread_root_id: null,
    reply_count: 0,
    last_reply_at: null,
    mention_here: 0,
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
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, NULL, 0, ?7, ?8, ?9)`,
      row.id,
      row.channel_id,
      row.author_id,
      row.kind,
      row.body,
      row.meta,
      row.poll_id,
      row.client_nonce,
      row.created_at,
    ),
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
  await publishEvent(rt, { channel: input.channelId }, "message.created", { message: message! });
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
