// LeetCode daily problem routes (v0.1.3 A10.3, api.md 5.25) — slot owned by P1. Mounted in app.ts
// (`app.route("/v1", leetRoutes)`) by P0 Task 26 so P1 never edits app.ts; each route's api.md 6 row lands with it.
// The server never calls LeetCode and stores no problem body (data-model.md 3.14).
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth } from "../lib/auth";
import { first, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { idParam } from "../lib/params";
import { requireSpaceRole } from "../lib/permissions";
import { checkLimit, limit } from "../lib/ratelimit";
import { isReportableDate, isValidDate, LeetDailyFacts, zCode, zFrontendId, zLang, zSlug } from "../lib/leet";
import { validateCard } from "../lib/cards/index";
import { isoOrNull, now } from "../lib/time";
import { vJson, vQuery, zId } from "../lib/validate";
import { createTextChannel } from "../services/channels";
import {
  dailySpacesOf,
  deleteSolve,
  friendsBoard,
  leetMe,
  spaceBoard,
  ensureDailyRoots,
  loadDaily,
  reportDailyFacts,
  serializeLeetDaily,
  serializeLeetSolve,
  solveCardData,
  upsertSolve,
  upsertSolveCards,
} from "../services/leet";
import type { ChannelRow } from "../types/rows";

export const leetRoutes = new Hono<AppEnv>();

/** The channel name #daily gets when no channel is given (A10.3; name rules as any channel). */
const DAILY_CHANNEL_NAME = "daily";

const SpaceLeetBody = z.object({
  enabled: z.boolean(),
  channel_id: zId.optional(),
});

interface LeetSpaceSettingsRow {
  space_id: string;
  enabled: number;
  channel_id: string | null;
  enabled_by: string | null;
  enabled_at: number | null;
  updated_at: number;
}

function serializeLeetSpaceSettings(spaceId: string, row: LeetSpaceSettingsRow | null) {
  return {
    space_id: spaceId,
    enabled: row?.enabled === 1,
    channel_id: row?.channel_id ?? null,
    enabled_by: row?.enabled_by ?? null,
    enabled_at: isoOrNull(row?.enabled_at),
  };
}

/** A live public text channel of `spaceId`, by id. */
function livePublicChannel(db: D1Database, spaceId: string, channelId: string): Promise<ChannelRow | null> {
  return first<ChannelRow>(
    db,
    "SELECT * FROM channels WHERE id = ?1 AND space_id = ?2 AND kind = 'text' AND is_private = 0 AND deleted_at IS NULL",
    channelId,
    spaceId,
  );
}

/** The space's live public text channel named `daily`. */
function dailyByName(db: D1Database, spaceId: string): Promise<ChannelRow | null> {
  return first<ChannelRow>(
    db,
    "SELECT * FROM channels WHERE space_id = ?1 AND kind = 'text' AND is_private = 0 AND deleted_at IS NULL AND name = ?2",
    spaceId,
    DAILY_CHANNEL_NAME,
  );
}

/**
 * owner·admin (requireSpaceRole admin: non-member 404, member 403). `enabled:true` picks the channel: `channel_id`
 * (a live public text channel of this space, else 404 channel), else the channel remembered from before (still live
 * and public), else the space's public `daily`, else a new `daily` made by createTextChannel (channel.created + the
 * channel_created system message; its 409 / 422 pass through). `enabled:false` keeps the channel and the row's other
 * values. One UPSERT of leet_space_settings. 200 { settings: LeetSpaceSettings }.
 */
leetRoutes.put("/spaces/:space_id/leet", requireAuth, limit("RL_SOCIAL"), vJson(SpaceLeetBody), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const spaceId = idParam(c, "space_id", "space");
  await requireSpaceRole(db, spaceId, me, "admin");
  const b = c.req.valid("json");
  const current = await first<LeetSpaceSettingsRow>(db, "SELECT * FROM leet_space_settings WHERE space_id = ?1", spaceId);
  const t = now();

  if (!b.enabled) {
    await stmt(
      db,
      `INSERT INTO leet_space_settings (space_id, enabled, channel_id, enabled_by, enabled_at, updated_at) VALUES (?1, 0, NULL, NULL, NULL, ?2)
       ON CONFLICT (space_id) DO UPDATE SET enabled = 0, updated_at = excluded.updated_at`,
      spaceId,
      t,
    ).run();
  } else {
    let channel: ChannelRow | null;
    if (b.channel_id !== undefined) {
      channel = await livePublicChannel(db, spaceId, b.channel_id);
      if (channel === null) throw err.notFound("channel");
    } else {
      channel = current?.channel_id ? await livePublicChannel(db, spaceId, current.channel_id) : null;
      channel ??= await dailyByName(db, spaceId);
      if (channel === null) {
        try {
          const made = await createTextChannel(rt(c), spaceId, me, { name: DAILY_CHANNEL_NAME });
          channel = await livePublicChannel(db, spaceId, made.id);
        } catch (e) {
          // a concurrent switch made `daily` first: use it
          channel = await dailyByName(db, spaceId);
          if (channel === null) throw e;
        }
        if (channel === null) throw err.notFound("channel");
      }
    }
    // the same channel already on: keep who / when turned it on
    const keep = current !== null && current.enabled === 1 && current.channel_id === channel.id;
    await stmt(
      db,
      `INSERT INTO leet_space_settings (space_id, enabled, channel_id, enabled_by, enabled_at, updated_at) VALUES (?1, 1, ?2, ?3, ?4, ?4)
       ON CONFLICT (space_id) DO UPDATE SET enabled = 1, channel_id = excluded.channel_id,
         enabled_by = CASE WHEN ?5 THEN leet_space_settings.enabled_by ELSE excluded.enabled_by END,
         enabled_at = CASE WHEN ?5 THEN leet_space_settings.enabled_at ELSE excluded.enabled_at END,
         updated_at = excluded.updated_at`,
      spaceId,
      channel.id,
      me,
      t,
      keep ? 1 : 0,
    ).run();
  }
  const row = await first<LeetSpaceSettingsRow>(db, "SELECT * FROM leet_space_settings WHERE space_id = ?1", spaceId);
  return c.json({ settings: serializeLeetSpaceSettings(spaceId, row) });
});

/**
 * A client reports the facts it fetched for `date` (today or yesterday, UTC; else 400 fields.date "invalid"). The first
 * report is stored, the same again is kept, a different one counts a mismatch (reportDailyFacts). Then the #daily root
 * of that day is made in every space of the reporter that has #daily on (ensureDailyRoots — one per channel per day,
 * from the stored facts); the first root this request creates also counts RL_MESSAGE once.
 * 200 { daily: LeetDaily, roots: LeetDailyRoot[] }.
 */
leetRoutes.put("/leet/daily/:date", requireAuth, limit("RL_SOCIAL"), vJson(LeetDailyFacts), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const date = c.req.param("date");
  if (!isReportableDate(date, now())) throw err.invalid({ date: "invalid" });
  const stored = await reportDailyFacts(db, me, date, c.req.valid("json"));
  const roots = await ensureDailyRoots(rt(c), me, date, stored, {
    beforeCreate: () => checkLimit(c.env, "RL_MESSAGE", `u:${me}`),
  });
  return c.json({ daily: await serializeLeetDaily(db, stored), roots });
});

/** At most this many spaces per solve share (api.md 5.25). */
const SHARE_SPACES_MAX = 10;

const LeetSolveBody = z.strictObject({
  status: z.literal("accepted"),
  slug: zSlug,
  lang: zLang,
  frontend_id: zFrontendId,
  runtime_percentile: z.number().min(0).max(100).nullable().optional(),
  submission_id: z.string().regex(/^[0-9]{1,20}$/).nullable().optional(),
  share_space_ids: z.array(zId).max(SHARE_SPACES_MAX).optional(),
  code: zCode.nullable().optional(),
});

/**
 * My accepted solve of `date` (today or yesterday, UTC). Checks first, writes nothing on failure: every shared space
 * must have #daily on with me in it (else 404 space), that day's facts must be known when sharing (else 404 daily),
 * and the card data must pass the leet.solve schema and the 16 KB cap (400 fields.card.data). Then: the record
 * (upsertSolve — passed_at of the first record kept) and my card in each shared space's root thread
 * (upsertSolveCards; the first root / card this request writes counts RL_MESSAGE once).
 * 200 { solve: LeetSolve, cards: { space_id, message_id }[] }.
 */
leetRoutes.put("/leet/solves/:date", requireAuth, limit("RL_SOCIAL"), vJson(LeetSolveBody), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const date = c.req.param("date");
  if (!isReportableDate(date, now())) throw err.invalid({ date: "invalid" });
  const b = c.req.valid("json");
  const share = [...new Set(b.share_space_ids ?? [])];
  const code = b.code ?? null;
  let daily = null;
  if (share.length > 0) {
    const ok = await dailySpacesOf(db, me, share);
    if (ok.length !== share.length) throw err.notFound("space");
    daily = await loadDaily(db, date);
    if (daily === null) throw err.notFound("daily");
  }
  const input = { slug: b.slug, lang: b.lang, frontend_id: b.frontend_id, runtime_percentile: b.runtime_percentile ?? null, submission_id: b.submission_id ?? null };
  // the card as it will be written (slug stays the first record's): validated before anything is stored
  const existing = await first<{ slug: string }>(db, "SELECT slug FROM leet_solves WHERE user_id = ?1 AND date = ?2", me, date);
  const preview = solveCardData({ user_id: me, date, passed_at: 0, ...input, slug: existing?.slug ?? input.slug }, b.frontend_id, code);
  if (share.length > 0) validateCard({ type: "leet.solve", v: 1, data: preview }, true);

  const row = await upsertSolve(db, me, date, input);
  const cards =
    daily === null
      ? []
      : await upsertSolveCards(rt(c), me, date, daily, solveCardData(row, b.frontend_id, code), share, () => checkLimit(c.env, "RL_MESSAGE", `u:${me}`));
  return c.json({ solve: serializeLeetSolve(row), cards });
});

/** Deletes my record of `date` (any real day) and my solve cards of that day (message.deleted). 204, also when none. */
leetRoutes.delete("/leet/solves/:date", requireAuth, limit("RL_SOCIAL"), async (c) => {
  const me = getAuth(c).userId;
  const date = c.req.param("date");
  if (!isValidDate(date)) throw err.invalid({ date: "invalid" });
  await deleteSolve(rt(c), me, date);
  return c.body(null, 204);
});

/** My streaks (current, best), my records of the last 30 UTC days (newest first) and my #daily spaces. */
leetRoutes.get("/leet/me", requireAuth, async (c) => {
  return c.json(await leetMe(c.env.DB, getAuth(c).userId));
});

const BoardQuery = z.object({ period: z.enum(["week", "month", "all"]).optional() });

/**
 * Members only (non-member 404). The space's board for `period` (default week): who left solve cards in its #daily
 * (current, unsuspended members), solved days and current streak (spaceBoard). 200 { period, items }.
 */
leetRoutes.get("/spaces/:space_id/leet/board", requireAuth, vQuery(BoardQuery), async (c) => {
  const me = getAuth(c).userId;
  const spaceId = idParam(c, "space_id", "space");
  await requireSpaceRole(c.env.DB, spaceId, me, "member");
  const period = c.req.valid("query").period ?? "week";
  return c.json({ period, items: await spaceBoard(c.env.DB, spaceId, period) });
});

/** Me + friends who share (leet_share_with_friends): streaks only. 200 { items }. */
leetRoutes.get("/leet/friends/board", requireAuth, async (c) => {
  return c.json({ items: await friendsBoard(c.env.DB, getAuth(c).userId) });
});
