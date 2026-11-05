// Polls (server-architecture.md 4.14 polls.ts). Plan 01 creates pollInsertStmts only; vote / closePoll are plan 02.
import { stmt } from "../lib/db";
import type { PollInput } from "../types/api";

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
