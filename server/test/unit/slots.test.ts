// P0 Task 26: assembly slots other lines fill in — the leet (P1) and workspace-shares (P2) routers are mounted (no
// routes yet: 404), and the bindings P2 needs already exist, so P1 / P2 never touch app.ts, index.ts, wrangler.jsonc or
// lib/ratelimit.ts (plan index 2).
import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { createApp } from "../../src/app";
import { LIMITER_PERIOD_SEC } from "../../src/lib/ratelimit";
import { api, signup } from "../helpers";

const sources = import.meta.glob("../../src/app.ts", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

describe("server assembly slots", () => {
  it("leet and workspace-shares routers are mounted and serve nothing yet", async () => {
    const appSrc = Object.values(sources)[0]!;
    expect(appSrc).toContain('app.route("/v1", leetRoutes)');
    expect(appSrc).toContain('app.route("/v1", workspaceShareRoutes)');
    expect(createApp()).toBeDefined();
    const u = await signup();
    expect((await api(u.token).get("/v1/leet/daily")).status).toBe(404);
    expect((await api(u.token).post("/v1/workspace-shares", {})).status).toBe(404);
  });

  it("bindings: WORKSPACE_HUB, COLLAB_HUB, RL_DOC, RL_SHARE, RL_SHARE_WS", async () => {
    const e = env as unknown as Record<string, unknown>;
    for (const name of ["WORKSPACE_HUB", "COLLAB_HUB", "RL_DOC", "RL_SHARE", "RL_SHARE_WS"]) expect(e[name], name).toBeDefined();
    expect(LIMITER_PERIOD_SEC.RL_DOC).toBe(60);
    expect(LIMITER_PERIOD_SEC.RL_SHARE).toBe(60);
    expect(LIMITER_PERIOD_SEC.RL_SHARE_WS).toBe(60);
    const res = await env.WORKSPACE_HUB.getByName("slot").fetch("http://do/x");
    expect(res.status).toBe(501);
  });
});
