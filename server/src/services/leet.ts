// LeetCode daily problem services (v0.1.3 A10.3, api.md 5.25, P1 Tasks 3–5). Facts and solve records only — the
// server never calls LeetCode and stores no problem body (data-model.md 3.14).
import type { Runtime } from "../env";
import { all, first, jsonList, stmt } from "../lib/db";
import { ApiError } from "../lib/errors";
import { computeStreak, type LeetDailyCard, type LeetDailyFactsInput, type LeetSolveCard, leetLink, utcDate } from "../lib/leet";
import { loadUserSummaries } from "../lib/serialize";
import { iso, now } from "../lib/time";
import type { UserSummary } from "../types/api";
import { createCardMessage, deleteMessage, updateCardMessage } from "./messages";

/** A NULL-message claim older than this is taken over by the next request (its maker died mid-way). */
export const ROOT_CLAIM_STALE_MS = 30_000;
/** How long a request waits for another request's claim to get its message before leaving that root out. */
const ROOT_WAIT_MS = 2_000;
const ROOT_WAIT_STEP_MS = 25;

export interface LeetDailyRow {
  date: string;
  frontend_id: string;
  slug: string;
  title: string;
  difficulty: "Easy" | "Medium" | "Hard";
  tags_json: string;
  is_paid_only: number;
  first_reporter_id: string | null;
  first_reported_at: number;
  mismatch_count: number;
}

export interface LeetDaily {
  date: string;
  frontend_id: string;
  slug: string;
  title: string;
  difficulty: "Easy" | "Medium" | "Hard";
  tags: string[];
  is_paid_only: boolean;
  first_reporter: UserSummary | null;
  first_reported_at: string;
  mismatch_count: number;
}

export interface LeetDailyRoot {
  space_id: string;
  channel_id: string;
  message_id: string;
}

/** A space with #daily on whose channel is live (and readable by the user the query was for). */
export interface DailySpace {
  space_id: string;
  channel_id: string;
}

export function loadDaily(db: D1Database, date: string): Promise<LeetDailyRow | null> {
  return first<LeetDailyRow>(db, "SELECT * FROM leet_daily WHERE date = ?1", date);
}

/**
 * Stores the first report of `date`; a later report with the same facts changes nothing, a different one only adds 1
 * to mismatch_count (the stored facts are never overwritten here). Returns the stored row.
 */
export async function reportDailyFacts(db: D1Database, reporterId: string, date: string, f: LeetDailyFactsInput): Promise<LeetDailyRow> {
  await stmt(
    db,
    `INSERT INTO leet_daily (date, frontend_id, slug, title, difficulty, tags_json, is_paid_only, first_reporter_id, first_reported_at)
     VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)
     ON CONFLICT (date) DO UPDATE SET mismatch_count = leet_daily.mismatch_count + (
       excluded.frontend_id <> leet_daily.frontend_id OR excluded.slug <> leet_daily.slug OR excluded.title <> leet_daily.title
       OR excluded.difficulty <> leet_daily.difficulty OR excluded.tags_json <> leet_daily.tags_json
       OR excluded.is_paid_only <> leet_daily.is_paid_only)`,
    date,
    f.frontend_id,
    f.slug,
    f.title,
    f.difficulty,
    JSON.stringify(f.tags),
    f.is_paid_only ? 1 : 0,
    reporterId,
    now(),
  ).run();
  const row = await loadDaily(db, date);
  if (row === null) throw new Error(`leet_daily ${date} missing after upsert`);
  return row;
}

export function dailyCardData(row: LeetDailyRow): LeetDailyCard {
  return {
    date: row.date,
    frontend_id: row.frontend_id,
    slug: row.slug,
    title: row.title,
    difficulty: row.difficulty,
    tags: JSON.parse(row.tags_json) as string[],
    is_paid_only: row.is_paid_only === 1,
  };
}

export async function serializeLeetDaily(db: D1Database, row: LeetDailyRow): Promise<LeetDaily> {
  const users = row.first_reporter_id === null ? new Map<string, UserSummary>() : await loadUserSummaries(db, [row.first_reporter_id]);
  return {
    ...dailyCardData(row),
    first_reporter: row.first_reporter_id === null ? null : (users.get(row.first_reporter_id) ?? null),
    first_reported_at: iso(row.first_reported_at),
    mismatch_count: row.mismatch_count,
  };
}

/**
 * Spaces `userId` belongs to (live space) with #daily on and a live text channel the user can read (public, or a
 * channel member). With `spaceIds`, only those. Space join order.
 */
export function dailySpacesOf(db: D1Database, userId: string, spaceIds?: readonly string[]): Promise<DailySpace[]> {
  return all<DailySpace>(
    db,
    `SELECT ls.space_id, ls.channel_id
       FROM leet_space_settings ls
       JOIN space_members sm ON sm.space_id = ls.space_id AND sm.user_id = ?1
       JOIN spaces s ON s.id = ls.space_id AND s.deleted_at IS NULL
       JOIN channels c ON c.id = ls.channel_id AND c.space_id = ls.space_id AND c.kind = 'text' AND c.deleted_at IS NULL
      WHERE ls.enabled = 1
        AND (c.is_private = 0 OR EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = c.id AND cm.user_id = ?1))
        AND (?2 IS NULL OR ls.space_id IN (SELECT value FROM json_each(?2)))
      ORDER BY sm.joined_at, ls.space_id`,
    userId,
    spaceIds === undefined ? null : jsonList(spaceIds),
  );
}

interface RootRow {
  message_id: string | null;
  claimed_at: number;
  live: number | null;
}

function loadRoot(db: D1Database, channelId: string, date: string): Promise<RootRow | null> {
  return first<RootRow>(
    db,
    `SELECT r.message_id, r.claimed_at, (SELECT m.deleted_at IS NULL FROM messages m WHERE m.id = r.message_id) AS live
       FROM leet_daily_roots r WHERE r.channel_id = ?1 AND r.date = ?2`,
    channelId,
    date,
  );
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

export interface EnsureRootsOptions {
  /** only these spaces (still subject to the membership / #daily rules) */
  spaceIds?: readonly string[];
  /**
   * Runs once, before the first root this request actually creates (the route counts RL_MESSAGE there). A throw
   * releases the claim and propagates.
   */
  beforeCreate?: () => Promise<void>;
}

/**
 * The #daily root of `date` in each space of dailySpacesOf(reporter), made when missing — at most one per channel per
 * day (leet_daily_roots):
 *   INSERT OR IGNORE a claim (message_id NULL); the request whose insert changed a row creates the card
 *   (createCardMessage, author = reporter, body = leetLink(stored slug), data = the stored facts) and fills
 *   message_id; on failure it deletes its NULL claim and rethrows.
 *   A claim left NULL for more than 30 s, or a root whose message was deleted, is taken over with a compare-and-set
 *   on (message_id, claimed_at). Another request's fresh claim is waited for (<= 2 s); still unfinished -> left out.
 * Returns the roots that have a live message, in dailySpacesOf order.
 */
export async function ensureDailyRoots(rt: Runtime, reporterId: string, date: string, facts: LeetDailyRow, opts: EnsureRootsOptions = {}): Promise<LeetDailyRoot[]> {
  const db = rt.env.DB;
  const spaces = await dailySpacesOf(db, reporterId, opts.spaceIds);
  let counted = false;
  const create = async (channelId: string): Promise<string> => {
    try {
      if (!counted && opts.beforeCreate) {
        counted = true;
        await opts.beforeCreate();
      }
      const m = await createCardMessage(rt, {
        channelId,
        authorId: reporterId,
        rootId: null,
        card: { type: "leet.daily", v: 1, data: dailyCardData(facts) },
        body: leetLink(facts.slug),
      });
      await stmt(db, "UPDATE leet_daily_roots SET message_id = ?3 WHERE channel_id = ?1 AND date = ?2 AND message_id IS NULL", channelId, date, m.id).run();
      return m.id;
    } catch (e) {
      await stmt(db, "DELETE FROM leet_daily_roots WHERE channel_id = ?1 AND date = ?2 AND message_id IS NULL", channelId, date).run();
      throw e;
    }
  };

  const out: LeetDailyRoot[] = [];
  for (const s of spaces) {
    const t = now();
    const claim = await stmt(
      db,
      "INSERT OR IGNORE INTO leet_daily_roots (channel_id, date, message_id, claimed_at) VALUES (?1, ?2, NULL, ?3)",
      s.channel_id,
      date,
      t,
    ).run();
    if ((claim.meta.changes ?? 0) === 1) {
      out.push({ ...s, message_id: await create(s.channel_id) });
      continue;
    }
    const deadline = Date.now() + ROOT_WAIT_MS;
    for (;;) {
      const row = await loadRoot(db, s.channel_id, date);
      if (row === null) break; // the claimer failed and released it; the next request makes it
      if (row.message_id !== null && row.live === 1) {
        out.push({ ...s, message_id: row.message_id });
        break;
      }
      const stale = row.message_id !== null || row.claimed_at < now() - ROOT_CLAIM_STALE_MS;
      if (stale) {
        const steal = await stmt(
          db,
          `UPDATE leet_daily_roots SET message_id = NULL, claimed_at = ?3
            WHERE channel_id = ?1 AND date = ?2 AND message_id IS ?4 AND claimed_at = ?5`,
          s.channel_id,
          date,
          now(),
          row.message_id,
          row.claimed_at,
        ).run();
        if ((steal.meta.changes ?? 0) === 1) {
          out.push({ ...s, message_id: await create(s.channel_id) });
          break;
        }
        continue; // someone else took it over: look again
      }
      if (Date.now() >= deadline) break;
      await sleep(ROOT_WAIT_STEP_MS);
    }
  }
  return out;
}

// ---------------------------------------------------------------------------
// solves (P1 Task 4)

export interface LeetSolveRow {
  user_id: string;
  date: string;
  slug: string;
  lang: string;
  passed_at: number;
  runtime_percentile: number | null;
  submission_id: string | null;
}

export interface LeetSolve {
  date: string;
  slug: string;
  lang: string;
  passed_at: string;
  runtime_percentile: number | null;
  counts_for_streak: boolean;
}

export interface LeetSolveCardRef {
  space_id: string;
  message_id: string;
}

/** A day is filled only by a record the server got within that UTC day (JA26). */
export function countsForStreak(row: Pick<LeetSolveRow, "date" | "passed_at">): boolean {
  return utcDate(row.passed_at) === row.date;
}

export function serializeLeetSolve(row: LeetSolveRow): LeetSolve {
  return {
    date: row.date,
    slug: row.slug,
    lang: row.lang,
    passed_at: iso(row.passed_at),
    runtime_percentile: row.runtime_percentile,
    counts_for_streak: countsForStreak(row),
  };
}

export interface SolveInput {
  slug: string;
  lang: string;
  frontend_id: string;
  runtime_percentile: number | null;
  submission_id: string | null;
}

/**
 * Upserts my record of `date`: the first record keeps its server time (passed_at) and slug; a re-PUT updates only
 * lang, runtime_percentile and submission_id. Returns the stored row.
 */
export async function upsertSolve(db: D1Database, me: string, date: string, s: SolveInput): Promise<LeetSolveRow> {
  await stmt(
    db,
    `INSERT INTO leet_solves (user_id, date, slug, lang, passed_at, runtime_percentile, submission_id) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)
     ON CONFLICT (user_id, date) DO UPDATE SET lang = excluded.lang, runtime_percentile = excluded.runtime_percentile,
       submission_id = excluded.submission_id`,
    me,
    date,
    s.slug,
    s.lang,
    now(),
    s.runtime_percentile,
    s.submission_id,
  ).run();
  const row = await first<LeetSolveRow>(db, "SELECT * FROM leet_solves WHERE user_id = ?1 AND date = ?2", me, date);
  if (row === null) throw new Error(`leet_solves ${me}/${date} missing after upsert`);
  return row;
}

export function solveCardData(row: LeetSolveRow, frontendId: string, code: string | null): LeetSolveCard {
  return {
    date: row.date,
    frontend_id: frontendId,
    slug: row.slug,
    lang: row.lang,
    runtime_percentile: row.runtime_percentile,
    code: code === null ? null : { lang: row.lang, text: code },
    spoiler: true,
  };
}

/** My live leet.solve card under `rootId`, if any. */
function mySolveCard(db: D1Database, rootId: string, me: string): Promise<{ id: string } | null> {
  return first<{ id: string }>(
    db,
    `SELECT id FROM messages
      WHERE thread_root_id = ?1 AND author_id = ?2 AND card_json IS NOT NULL
        AND json_extract(card_json, '$.type') = 'leet.solve' AND deleted_at IS NULL
      ORDER BY id LIMIT 1`,
    rootId,
    me,
  );
}

/**
 * For each shared space (the caller checked they are all dailySpacesOf(me)): the day's root (ensureDailyRoots for that
 * one space, so a missing root is made by me), then my leet.solve card under it — rewritten with updateCardMessage
 * when it exists (message.updated), else created with createCardMessage (message.created + thread.updated).
 * `beforeWrite` runs once before the first card or root this call writes. A root another request is still making
 * after the wait (ensureDailyRoots gave none) -> 503 service_unavailable (retryable); cards already written stay.
 */
export async function upsertSolveCards(
  rt: Runtime,
  me: string,
  date: string,
  daily: LeetDailyRow,
  card: LeetSolveCard,
  shareSpaceIds: readonly string[],
  beforeWrite?: () => Promise<void>,
): Promise<LeetSolveCardRef[]> {
  const db = rt.env.DB;
  let counted = false;
  const once = async () => {
    if (counted || !beforeWrite) return;
    counted = true;
    await beforeWrite();
  };
  const out: LeetSolveCardRef[] = [];
  const body = leetLink(card.slug);
  for (const spaceId of shareSpaceIds) {
    const [root] = await ensureDailyRoots(rt, me, date, daily, { spaceIds: [spaceId], beforeCreate: once });
    // another request is still making that root (rare, <= 2 s waited): retryable
    if (!root) throw new ApiError(503, "service_unavailable", "the day's #daily root is being created; retry");
    const existing = await mySolveCard(db, root.message_id, me);
    await once();
    const input = { type: "leet.solve" as const, v: 1, data: card };
    const m = existing
      ? await updateCardMessage(rt, existing.id, input, body)
      : await createCardMessage(rt, { channelId: root.channel_id, authorId: me, rootId: root.message_id, card: input, body });
    out.push({ space_id: spaceId, message_id: m.id });
  }
  return out;
}

/** Deletes my record of `date` and every live leet.solve card of mine for that day (deleteMessage: message.deleted). */
export async function deleteSolve(rt: Runtime, me: string, date: string): Promise<void> {
  const db = rt.env.DB;
  const cards = await all<{ id: string }>(
    db,
    `SELECT id FROM messages
      WHERE author_id = ?1 AND card_json IS NOT NULL AND deleted_at IS NULL
        AND json_extract(card_json, '$.type') = 'leet.solve' AND json_extract(card_json, '$.data.date') = ?2`,
    me,
    date,
  );
  await stmt(db, "DELETE FROM leet_solves WHERE user_id = ?1 AND date = ?2", me, date).run();
  for (const c of cards) await deleteMessage(rt, c.id, me);
}

// ---------------------------------------------------------------------------
// streaks and boards (P1 Task 5)

/** api.md 5.25: GET /v1/leet/me lists this many UTC days back (today included). */
export const RECENT_DAYS = 30;

/** Counted days (utcDate(passed_at) = date) per user, one query for any number of users. */
async function filledDatesOf(db: D1Database, userIds: readonly string[]): Promise<Map<string, string[]>> {
  const out = new Map<string, string[]>(userIds.map((u) => [u, []]));
  if (userIds.length === 0) return out;
  const rows = await all<{ user_id: string; date: string }>(
    db,
    `SELECT user_id, date FROM leet_solves
      WHERE user_id IN (SELECT value FROM json_each(?1))
        AND strftime('%Y-%m-%d', passed_at / 1000, 'unixepoch') = date`,
    jsonList(userIds),
  );
  for (const r of rows) out.get(r.user_id)?.push(r.date);
  return out;
}

export interface LeetMe {
  current: number;
  best: number;
  recent: LeetSolve[];
  daily_spaces: DailySpace[];
}

export async function leetMe(db: D1Database, me: string): Promise<LeetMe> {
  const t = now();
  const today = utcDate(t);
  const from = utcDate(t - (RECENT_DAYS - 1) * 86_400_000);
  const [filled, recent, spaces] = await Promise.all([
    filledDatesOf(db, [me]),
    all<LeetSolveRow>(db, "SELECT * FROM leet_solves WHERE user_id = ?1 AND date >= ?2 AND date <= ?3 ORDER BY date DESC", me, from, today),
    dailySpacesOf(db, me),
  ]);
  const { current, best } = computeStreak(filled.get(me) ?? [], today);
  return { current, best, recent: recent.map(serializeLeetSolve), daily_spaces: spaces };
}

export type BoardPeriod = "week" | "month" | "all";

/** First UTC day of the period that contains `nowMs`: ISO week (Monday) / calendar month / null for all. */
export function periodStart(period: BoardPeriod, nowMs: number): string | null {
  if (period === "all") return null;
  const d = new Date(nowMs);
  if (period === "month") return `${utcDate(nowMs).slice(0, 7)}-01`;
  const sinceMonday = (d.getUTCDay() + 6) % 7;
  return utcDate(nowMs - sinceMonday * 86_400_000);
}

export interface LeetBoardEntry {
  user: UserSummary;
  solved: number;
  streak: number;
}

/**
 * The space's board: current members, not suspended now, with live leet.solve cards of their own in the space's #daily
 * channel whose data.date is in the period. solved = distinct card days; streak = the member's current streak over
 * all their records. Order: solved desc, streak desc, handle asc. No #daily channel -> [].
 */
export async function spaceBoard(db: D1Database, spaceId: string, period: BoardPeriod): Promise<LeetBoardEntry[]> {
  const t = now();
  const since = periodStart(period, t);
  const rows = await all<{ user_id: string; solved: number }>(
    db,
    `SELECT m.author_id AS user_id, COUNT(DISTINCT json_extract(m.card_json, '$.data.date')) AS solved
       FROM leet_space_settings ls
       JOIN channels c ON c.id = ls.channel_id AND c.space_id = ls.space_id AND c.deleted_at IS NULL
       JOIN messages m ON m.channel_id = c.id AND m.deleted_at IS NULL AND m.thread_root_id IS NOT NULL
                      AND m.card_json IS NOT NULL AND json_extract(m.card_json, '$.type') = 'leet.solve'
       JOIN space_members sm ON sm.space_id = ls.space_id AND sm.user_id = m.author_id
       JOIN users u ON u.id = m.author_id
      WHERE ls.space_id = ?1
        AND (?2 IS NULL OR json_extract(m.card_json, '$.data.date') >= ?2)
        AND NOT (u.suspended_at IS NOT NULL AND (u.suspended_until IS NULL OR u.suspended_until > ?3))
      GROUP BY m.author_id`,
    spaceId,
    since,
    t,
  );
  const ids = rows.map((r) => r.user_id);
  const [users, filled] = await Promise.all([loadUserSummaries(db, ids), filledDatesOf(db, ids)]);
  const today = utcDate(t);
  const items: LeetBoardEntry[] = [];
  for (const r of rows) {
    const user = users.get(r.user_id);
    if (!user) continue;
    items.push({ user, solved: r.solved, streak: computeStreak(filled.get(r.user_id) ?? [], today).current });
  }
  return items.sort((a, b) => b.solved - a.solved || b.streak - a.streak || (a.user.handle < b.user.handle ? -1 : a.user.handle > b.user.handle ? 1 : 0));
}

export interface LeetFriendStreak {
  user: UserSummary;
  current: number;
  best: number;
}

/**
 * Me plus my friends whose leet_share_with_friends is on (a missing settings row reads as on) and who are not suspended
 * now. Streaks only. Order: current desc, best desc, handle asc.
 */
export async function friendsBoard(db: D1Database, me: string): Promise<LeetFriendStreak[]> {
  const t = now();
  const rows = await all<{ id: string }>(
    db,
    `SELECT f.id FROM (
        SELECT user_b AS id FROM friendships WHERE user_a = ?1
        UNION SELECT user_a AS id FROM friendships WHERE user_b = ?1) f
       JOIN users u ON u.id = f.id
       LEFT JOIN user_settings us ON us.user_id = f.id
      WHERE COALESCE(us.leet_share_with_friends, 1) = 1
        AND NOT (u.suspended_at IS NOT NULL AND (u.suspended_until IS NULL OR u.suspended_until > ?2))`,
    me,
    t,
  );
  const ids = [me, ...rows.map((r) => r.id).filter((id) => id !== me)];
  const [users, filled] = await Promise.all([loadUserSummaries(db, ids, {}), filledDatesOf(db, ids)]);
  const today = utcDate(t);
  const items: LeetFriendStreak[] = [];
  for (const id of ids) {
    const user = users.get(id);
    if (!user) continue;
    const { current, best } = computeStreak(filled.get(id) ?? [], today);
    items.push({ user, current, best });
  }
  return items.sort((a, b) => b.current - a.current || b.best - a.best || (a.user.handle < b.user.handle ? -1 : a.user.handle > b.user.handle ? 1 : 0));
}
