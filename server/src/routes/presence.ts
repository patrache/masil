// Presence and focus mode (api.md 5.1): PUT /me/presence, GET /presence, POST|GET|DELETE /me/focus.
// The state machine lives in the user's UserHub (events.md 6.3); these routes validate and call its RPCs.
// requireAuth is attached per route (plan 01 Global Constraints: /me is shared with other modules).
import { Hono } from "hono";
import * as z from "zod";
import type { AppEnv } from "../env";
import { getAuth, requireAuth } from "../lib/auth";
import { first } from "../lib/db";
import { err } from "../lib/errors";
import { isId } from "../lib/ids";
import { presenceVisible } from "../lib/permissions";
import { loadPresenceItems } from "../lib/serialize";
import { vJson, vQuery } from "../lib/validate";
import { FOCUS_MAX_SEC, FOCUS_MIN_SEC } from "../do/user-hub";

const PRESENCE_IDS_MAX = 100;

const PresenceBody = z.object({ mode: z.enum(["auto", "away", "dnd"]) });
const PresenceQuery = z.object({ user_ids: z.string() });
const FocusBody = z.object({ duration_sec: z.int().min(FOCUS_MIN_SEC).max(FOCUS_MAX_SEC).optional() });

export const presenceRoutes = new Hono<AppEnv>();

presenceRoutes.put("/me/presence", requireAuth, vJson(PresenceBody), async (c) => {
  const me = getAuth(c).userId;
  const presence = await c.env.USER_HUB.getByName(me).setPresenceMode(me, c.req.valid("json").mode);
  return c.json({ presence });
});

presenceRoutes.get("/presence", requireAuth, vQuery(PresenceQuery), async (c) => {
  const me = getAuth(c).userId;
  const ids = [
    ...new Set(
      c.req
        .valid("query")
        .user_ids.split(",")
        .map((s) => s.trim())
        .filter((s) => s !== ""),
    ),
  ];
  if (ids.length === 0 || ids.length > PRESENCE_IDS_MAX || !ids.every(isId)) throw err.invalid({ user_ids: "invalid" });
  const visible = await presenceVisible(c.env.DB, me, ids);
  const items = await loadPresenceItems(
    c.env.DB,
    ids.filter((id) => visible.has(id)),
  );
  return c.json({ items });
});

presenceRoutes.post("/me/focus", requireAuth, vJson(FocusBody), async (c) => {
  const me = getAuth(c).userId;
  let durationSec = c.req.valid("json").duration_sec;
  if (durationSec === undefined) {
    const s = await first<{ focus_default_min: number }>(c.env.DB, "SELECT focus_default_min FROM user_settings WHERE user_id = ?1", me);
    durationSec = (s?.focus_default_min ?? 25) * 60; // 1..720 min -> always within 60..43200 s
  }
  const focus = await c.env.USER_HUB.getByName(me).startFocus(me, durationSec);
  return c.json({ focus });
});

presenceRoutes.get("/me/focus", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const state = await c.env.USER_HUB.getByName(me).getPresenceState(me);
  return c.json({ focus: state.focus });
});

presenceRoutes.delete("/me/focus", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const summary = await c.env.USER_HUB.getByName(me).endFocus(me);
  if (summary === null) throw err.conflict("conflict", "not_focusing");
  return c.json({ summary });
});
