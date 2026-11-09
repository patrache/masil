// LeetCode daily problem services (v0.1.3 A10.3, api.md 5.25, P1 Tasks 3–5). Facts and solve records only — the
// server never calls LeetCode and stores no problem body (data-model.md 3.14).
import type { Runtime } from "../env";
import { all, first, jsonList, stmt } from "../lib/db";
import { type LeetDailyCard, type LeetDailyFactsInput, leetLink } from "../lib/leet";
import { loadUserSummaries } from "../lib/serialize";
import { iso, now } from "../lib/time";
import type { UserSummary } from "../types/api";
import { createCardMessage } from "./messages";

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
