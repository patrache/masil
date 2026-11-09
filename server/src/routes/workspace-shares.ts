// Workspace share routes (v0.1.3 B10) — slot owned by P2. Mounted in app.ts (`app.route("/v1", workspaceShareRoutes)`)
// by P0 Task 26; the bindings P2 needs (WORKSPACE_HUB, RL_SHARE, RL_SHARE_WS) already exist. P2 adds its routes here.
import { Hono } from "hono";
import type { AppEnv } from "../env";

export const workspaceShareRoutes = new Hono<AppEnv>();
