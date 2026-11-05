// Polls (server-architecture.md 4.14 polls.ts). Plan 01 creates pollInsertStmts; vote / closePoll / getPoll are
// plan 02 Task 11. Every write is one statement or one db.batch; poll.updated goes out only after it committed.
import type { Runtime } from "../env";
import { batch, first, stmt } from "../lib/db";
import { ApiError, err } from "../lib/errors";
import { isId } from "../lib/ids";
import { canViewPost, requireChannelRead } from "../lib/permissions";
import { publishEvent } from "../lib/realtime";
import { serializePolls } from "../lib/serialize";
import { now } from "../lib/time";
import type { Poll, PollInput } from "../types/api";
import type { MessageRow, PollRow, PostRow } from "../types/rows";

/**
 * Statements that create a poll and its options (to run inside the caller's db.batch, BEFORE the row that references
 * polls.id). The input is already validated (zPollInput: question 1..200, 2..10 options of 1..80, duration_sec
 * 60..604800 | null). closes_at = nowMs + duration_sec*1000, or NULL (until closed manually). Option idx = array index.
 * Options go in with one statement over json_each (no per-option binds).
 */
export function pollInsertStmts(
  db: D1Database,
  pollId: string,
  creatorId: string,
  ctx: { type: "message" | "post"; id: string },
  input: PollInput,
  nowMs: number,
): D1PreparedStatement[] {
  const closesAt = input.duration_sec === undefined || input.duration_sec === null ? null : nowMs + input.duration_sec * 1000;
  return [
    stmt(
      db,
      `INSERT INTO polls (id, creator_id, context_type, context_id, question, multi, voter_count, closes_at, closed_at, created_at)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, 0, ?7, NULL, ?8)`,
      pollId,
      creatorId,
      ctx.type,
      ctx.id,
      input.question,
      input.multi === true ? 1 : 0,
      closesAt,
      nowMs,
    ),
    stmt(
      db,
      `INSERT INTO poll_options (poll_id, idx, text, vote_count)
       SELECT ?1, CAST(j.key AS INTEGER), j.value, 0 FROM json_each(?2) j`,
      pollId,
      JSON.stringify(input.options),
    ),
  ];
}

// ---- voting and closing (plan 02 Task 11, api.md 5.19) ----

/** A poll is open while not closed manually and closes_at (if any) is still ahead. `?N` binds nowMs. */
const OPEN_SQL = (pollParam: string, nowParam: string) =>
  `EXISTS (SELECT 1 FROM polls WHERE id = ${pollParam} AND closed_at IS NULL AND (closes_at IS NULL OR closes_at > ${nowParam}))`;

function isClosed(row: PollRow, nowMs: number): boolean {
  return row.closed_at !== null || (row.closes_at !== null && row.closes_at <= nowMs);
}

/** Where poll.updated goes: message polls -> the channel audience (logged); post polls -> topic post:<id> (ephemeral). */
type PollTarget = { kind: "message"; channelId: string } | { kind: "post"; postId: string };

/**
 * The poll row and its event target when userId may read the message / post it belongs to (api.md 5.19: same as the
 * read permission of that message / post), else 404 not_found (existence hidden).
 */
async function requirePollAccess(db: D1Database, pollId: string, userId: string): Promise<{ poll: PollRow; target: PollTarget }> {
  const poll = isId(pollId) ? await first<PollRow>(db, "SELECT * FROM polls WHERE id = ?1", pollId) : null;
  if (poll === null) throw err.notFound("poll");
  if (poll.context_type === "message") {
    const msg = await first<Pick<MessageRow, "channel_id" | "deleted_at">>(
      db,
      "SELECT channel_id, deleted_at FROM messages WHERE id = ?1 AND poll_id = ?2",
      poll.context_id,
      poll.id,
    );
    if (msg === null || msg.deleted_at !== null) throw err.notFound("poll");
    try {
      await requireChannelRead(db, msg.channel_id, userId);
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) throw err.notFound("poll");
      throw e;
    }
    return { poll, target: { kind: "message", channelId: msg.channel_id } };
  }
  const post = await first<PostRow>(db, "SELECT * FROM posts WHERE id = ?1 AND poll_id = ?2", poll.context_id, poll.id);
  if (post === null || !(await canViewPost(db, userId, post))) throw err.notFound("poll");
  return { poll, target: { kind: "post", postId: post.id } };
}

/** poll.updated with my_votes [] (events.md 4.2 / 4.8), published after the batch committed. */
async function publishPollUpdated(rt: Runtime, pollId: string, target: PollTarget): Promise<void> {
  const poll = (await serializePolls(rt.env.DB, null, [pollId])).get(pollId);
  if (poll === undefined) return;
  if (target.kind === "message") await publishEvent(rt, { channel: target.channelId }, "poll.updated", { poll });
  else await publishEvent(rt, { topic: `post:${target.postId}` }, "poll.updated", { poll }, { ephemeral: true });
}

async function viewerPoll(db: D1Database, pollId: string, userId: string): Promise<Poll> {
  const poll = (await serializePolls(db, userId, [pollId])).get(pollId);
  if (poll === undefined) throw err.notFound("poll");
  return poll;
}

/** GET /polls/:poll_id — the poll as userId sees it (my_votes filled); 404 without read access to its message / post. */
export async function getPoll(rt: Runtime, pollId: string, userId: string): Promise<Poll> {
  const { poll } = await requirePollAccess(rt.env.DB, pollId, userId);
  return viewerPoll(rt.env.DB, poll.id, userId);
}

/**
 * PUT (optionIdxs) / DELETE (null) /polls/:poll_id/vote. A vote replaces the user's previous one. Closed poll -> 409
 * poll_closed. Option list: single -> exactly 1, multi -> 1..option count, distinct, 0 <= idx < option count, else
 * 400 fields.option_idxs. One batch: undo the old votes (counters guarded by the existing rows), then insert the new
 * ones, every statement guarded by "still open" — so two concurrent votes by the same user leave voter_count 1.
 */
export async function vote(rt: Runtime, pollId: string, userId: string, optionIdxs: number[] | null): Promise<Poll> {
  const db = rt.env.DB;
  const { poll, target } = await requirePollAccess(db, pollId, userId);
  const t = now();
  if (isClosed(poll, t)) throw err.conflict("poll_closed");

  if (optionIdxs !== null) {
    const count = (await first<{ n: number }>(db, "SELECT COUNT(*) AS n FROM poll_options WHERE poll_id = ?1", poll.id))?.n ?? 0;
    const distinct = new Set(optionIdxs).size === optionIdxs.length;
    const inRange = optionIdxs.every((i) => Number.isInteger(i) && i >= 0 && i < count);
    const sizeOk = poll.multi === 1 ? optionIdxs.length >= 1 && optionIdxs.length <= count : optionIdxs.length === 1;
    if (!distinct || !inRange || !sizeOk) throw err.invalid({ option_idxs: "invalid" });
  }

  const open = OPEN_SQL("?1", "?3");
  const stmts: D1PreparedStatement[] = [
    stmt(db, `SELECT ${open} AS open`, poll.id, userId, t),
    stmt(
      db,
      `UPDATE poll_options SET vote_count = MAX(vote_count - 1, 0)
        WHERE poll_id = ?1 AND idx IN (SELECT option_idx FROM poll_votes WHERE poll_id = ?1 AND user_id = ?2) AND ${open}`,
      poll.id,
      userId,
      t,
    ),
    stmt(
      db,
      `UPDATE polls SET voter_count = MAX(voter_count - 1, 0)
        WHERE id = ?1 AND EXISTS (SELECT 1 FROM poll_votes WHERE poll_id = ?1 AND user_id = ?2) AND ${open}`,
      poll.id,
      userId,
      t,
    ),
    stmt(db, `DELETE FROM poll_votes WHERE poll_id = ?1 AND user_id = ?2 AND ${open}`, poll.id, userId, t),
  ];
  if (optionIdxs !== null) {
    const idxs = JSON.stringify([...optionIdxs].sort((x, y) => x - y));
    stmts.push(
      stmt(
        db,
        `INSERT INTO poll_votes (poll_id, user_id, option_idx, created_at)
         SELECT ?1, ?2, value, ?3 FROM json_each(?4) WHERE ${open}`,
        poll.id,
        userId,
        t,
        idxs,
      ),
      stmt(
        db,
        `UPDATE poll_options SET vote_count = vote_count + 1 WHERE poll_id = ?1 AND idx IN (SELECT value FROM json_each(?4)) AND ${open}`,
        poll.id,
        userId,
        t,
        idxs,
      ),
      stmt(db, `UPDATE polls SET voter_count = voter_count + 1 WHERE id = ?1 AND ${open}`, poll.id, userId, t),
    );
  }
  const results = await batch(db, stmts);
  const wasOpen = (results[0]!.results[0] as { open: number } | undefined)?.open === 1;
  if (!wasOpen) throw err.conflict("poll_closed"); // closed between the check above and the batch
  const removed = results[3]!.meta.changes > 0;
  const added = optionIdxs !== null && results[4]!.meta.changes > 0;
  if (removed || added) await publishPollUpdated(rt, poll.id, target);
  return viewerPoll(db, poll.id, userId);
}

/**
 * POST /polls/:poll_id/close — creator only (else 403 forbidden, after the 404 read check). An already closed poll
 * (manually or by closes_at) is returned as is without an event; otherwise closed_at = now and poll.updated goes out.
 */
export async function closePoll(rt: Runtime, pollId: string, userId: string): Promise<Poll> {
  const db = rt.env.DB;
  const { poll, target } = await requirePollAccess(db, pollId, userId);
  if (poll.creator_id !== userId) throw err.forbidden("forbidden", "only the poll creator can close it");
  const t = now();
  if (!isClosed(poll, t)) {
    const res = await stmt(db, `UPDATE polls SET closed_at = ?2 WHERE id = ?1 AND ${OPEN_SQL("?1", "?2")}`, poll.id, t).run();
    if (res.meta.changes > 0) await publishPollUpdated(rt, poll.id, target);
  }
  return viewerPoll(db, poll.id, userId);
}
