// Blocks (api.md 5.4). requireAuth is a per-route argument (plan 01 Global Constraints). The cleanup batch and the
// blocker-only events live in services/relations.ts (applyBlock / removeBlock).
import { Hono } from "hono";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth, resolveUserRef } from "../lib/auth";
import { all } from "../lib/db";
import { err } from "../lib/errors";
import { limit } from "../lib/ratelimit";
import { loadUserSummaries } from "../lib/serialize";
import { iso } from "../lib/time";
import { applyBlock, removeBlock } from "../services/relations";
import type { BlockRow } from "../types/rows";

export const blockRoutes = new Hono<AppEnv>();

/** Everyone I blocked, newest first (no paging). */
blockRoutes.get("/blocks", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const rows = await all<BlockRow>(c.env.DB, "SELECT * FROM blocks WHERE blocker_id = ?1 ORDER BY created_at DESC, blocked_id DESC", me);
  const users = await loadUserSummaries(c.env.DB, rows.map((r) => r.blocked_id));
  const items = rows.flatMap((r) => {
    const user = users.get(r.blocked_id);
    return user ? [{ user, created_at: iso(r.created_at) }] : [];
  });
  return c.json({ items });
});

blockRoutes.put("/blocks/:user", requireAuth, limit("RL_SOCIAL"), async (c) => {
  const auth = getAuth(c);
  const target = await resolveUserRef(c.env.DB, c.req.param("user"), auth);
  if (target.id === auth.userId) throw err.conflict("conflict", "self");
  await applyBlock(rt(c), auth.userId, target.id);
  return c.body(null, 204);
});

blockRoutes.delete("/blocks/:user", requireAuth, async (c) => {
  const auth = getAuth(c);
  const target = await resolveUserRef(c.env.DB, c.req.param("user"), auth);
  if (target.id === auth.userId) throw err.conflict("conflict", "self");
  await removeBlock(rt(c), auth.userId, target.id);
  return c.body(null, 204);
});
