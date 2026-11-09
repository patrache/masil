// Workspace share routes (v0.1.3 B10, api.md 5.25) — slot owned by P2. Mounted in app.ts (`app.route("/v1",
// workspaceShareRoutes)`) by P0 Task 26; the bindings P2 needs (WORKSPACE_HUB, RL_SHARE, RL_SHARE_WS) already exist.
// Create / invite take RL_SHARE; the "10 per 60 minutes" create window is counted in D1 by the service (api.md 0.7).
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth, resolveUserRef } from "../lib/auth";
import { err } from "../lib/errors";
import { idParam } from "../lib/params";
import { limit } from "../lib/ratelimit";
import { vJson, vQuery, zText, zUserRef } from "../lib/validate";
import { isBlockedEither } from "../lib/permissions";
import {
  addInvites,
  createShare,
  endShare,
  listShares,
  removeMember,
  serializeShare,
  shareAccess,
  workspaceHub,
  WORKSPACE_LIMITS,
} from "../services/workspace-shares";

const Users = z.array(zUserRef).min(1).max(WORKSPACE_LIMITS.members);

const CreateBody = z.object({ title: zText(1, 40), users: Users });
const InviteBody = z.object({ users: Users });
const ListQuery = z.object({
  role: z.enum(["owner", "invited"]).optional(),
  status: z.enum(["live", "ended"]).optional(),
});

export const workspaceShareRoutes = new Hono<AppEnv>();

/** UserRefs -> user ids, in order (404 for an unknown one). */
async function resolveAll(db: D1Database, refs: string[], me: ReturnType<typeof getAuth>): Promise<string[]> {
  const out: string[] = [];
  for (const ref of refs) out.push((await resolveUserRef(db, ref, me)).id);
  return out;
}

workspaceShareRoutes.post("/workspace-shares", requireAuth, limit("RL_SHARE"), vJson(CreateBody), async (c) => {
  const auth = getAuth(c);
  const body = c.req.valid("json");
  const users = await resolveAll(c.env.DB, body.users, auth);
  const share = await createShare(rt(c), auth.userId, body.title, users);
  return c.json({ share }, 201);
});

workspaceShareRoutes.get("/workspace-shares", requireAuth, vQuery(ListQuery), async (c) => {
  const q = c.req.valid("query");
  const items = await listShares(c.env.DB, getAuth(c).userId, q.role ?? "owner", q.status ?? "live");
  return c.json({ items });
});

workspaceShareRoutes.get("/workspace-shares/:share_id", requireAuth, async (c) => {
  const access = await shareAccess(c.env.DB, idParam(c, "share_id", "workspace share"), getAuth(c).userId);
  if (access === null) throw err.notFound("workspace share");
  return c.json({ share: await serializeShare(c.env.DB, access.row) });
});

workspaceShareRoutes.post("/workspace-shares/:share_id/invites", requireAuth, limit("RL_SHARE"), vJson(InviteBody), async (c) => {
  const auth = getAuth(c);
  const shareId = idParam(c, "share_id", "workspace share");
  // 404 / 403 before the refs are resolved, so a non-member cannot probe handles through this path
  const access = await shareAccess(c.env.DB, shareId, auth.userId);
  if (access === null) throw err.notFound("workspace share");
  if (access.role !== "owner") throw err.forbidden();
  const users = await resolveAll(c.env.DB, c.req.valid("json").users, auth);
  return c.json({ share: await addInvites(rt(c), shareId, auth.userId, users) });
});

workspaceShareRoutes.delete("/workspace-shares/:share_id/participants/:user", requireAuth, async (c) => {
  const auth = getAuth(c);
  const shareId = idParam(c, "share_id", "workspace share");
  if ((await shareAccess(c.env.DB, shareId, auth.userId)) === null) throw err.notFound("workspace share");
  const target = await resolveUserRef(c.env.DB, c.req.param("user"), auth);
  await removeMember(rt(c), shareId, auth.userId, target.id);
  return c.body(null, 204);
});

workspaceShareRoutes.delete("/workspace-shares/:share_id", requireAuth, async (c) => {
  const auth = getAuth(c);
  const shareId = idParam(c, "share_id", "workspace share");
  const access = await shareAccess(c.env.DB, shareId, auth.userId);
  if (access === null) throw err.notFound("workspace share");
  if (access.role !== "owner") throw err.forbidden();
  await endShare(rt(c), shareId, "ended");
  return c.body(null, 204);
});

/**
 * workspace.md 1: requireAuth -> RL_SHARE_WS -> a live share I own (seat 0) or am a member of that was not removed
 * (my seat), with no block either way between me and the owner — else 404 before the upgrade. The hub gets the upgrade
 * with X-Masil-Auth / X-Masil-Seat / X-Masil-Share set by us (the global middleware already dropped client copies).
 */
workspaceShareRoutes.get("/workspace-shares/:share_id/ws", requireAuth, limit("RL_SHARE_WS"), async (c) => {
  const upgrade = c.req.header("Upgrade");
  if ((upgrade ?? "").toLowerCase() !== "websocket") throw err.invalid({ upgrade: upgrade === undefined ? "required" : "invalid" });
  const auth = getAuth(c);
  const shareId = idParam(c, "share_id", "workspace share");
  const access = await shareAccess(c.env.DB, shareId, auth.userId);
  if (access === null || access.row.status !== "live") throw err.notFound("workspace share");
  let seat = 0;
  if (access.role === "member") {
    if (await isBlockedEither(c.env.DB, access.row.owner_id, auth.userId)) throw err.notFound("workspace share");
    const m = await c.env.DB.prepare("SELECT seat FROM workspace_share_members WHERE share_id = ?1 AND user_id = ?2 AND removed_at IS NULL")
      .bind(shareId, auth.userId)
      .first<{ seat: number }>();
    if (m === null) throw err.notFound("workspace share");
    seat = m.seat;
  }
  const headers = new Headers(c.req.raw.headers);
  headers.set("X-Masil-Auth", JSON.stringify({ userId: auth.userId, sessionId: auth.sessionId }));
  headers.set("X-Masil-Seat", String(seat));
  headers.set("X-Masil-Share", JSON.stringify({ share_id: shareId, owner_id: access.row.owner_id, title: access.row.title }));
  return workspaceHub(rt(c), shareId).fetch(new Request("https://hub/connect", { method: "GET", headers }));
});
