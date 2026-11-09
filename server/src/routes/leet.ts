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
import { isReportableDate, LeetDailyFacts } from "../lib/leet";
import { isoOrNull, now } from "../lib/time";
import { vJson, zId } from "../lib/validate";
import { createTextChannel } from "../services/channels";
import { ensureDailyRoots, reportDailyFacts, serializeLeetDaily } from "../services/leet";
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
