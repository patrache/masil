// GET /v1/ws (events.md 1.1, server-architecture.md 3.4): authenticate, rate limit, validate, then hand the
// upgrade to the user's UserHub with the AuthContext in X-Masil-Auth (the client's copy is never trusted) and the
// connection language in X-Masil-Lang (v0.1.3, i18n 7.3).
import { Hono } from "hono";
import * as z from "zod";
import type { AppEnv } from "../env";
import { getAuth, requireAuth } from "../lib/auth";
import { err } from "../lib/errors";
import { langOf } from "../lib/i18n";
import { limit } from "../lib/ratelimit";
import { vQuery, zId } from "../lib/validate";

const WsQuery = z.object({
  mode: z.enum(["full", "notify"]).default("full"),
  device_id: zId,
});

export const wsRoutes = new Hono<AppEnv>();

wsRoutes.get("/ws", requireAuth, limit("RL_WS"), vQuery(WsQuery), async (c) => {
  const upgrade = c.req.header("Upgrade");
  if ((upgrade ?? "").toLowerCase() !== "websocket") throw err.invalid({ upgrade: upgrade === undefined ? "required" : "invalid" });
  const auth = getAuth(c);
  const headers = new Headers(c.req.raw.headers);
  headers.delete("X-Masil-Auth"); // never trust a client copy (the global middleware strips it too)
  headers.set("X-Masil-Auth", JSON.stringify(auth));
  headers.set("X-Masil-Lang", langOf(c.req.header("Accept-Language"))); // api.md 0.1: kept in the connection's memory only
  return c.env.USER_HUB.getByName(auth.userId).fetch(new Request(c.req.raw.url, { method: "GET", headers }));
});
