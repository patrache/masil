// LeetCode daily problem routes (v0.1.3 A10.3) — slot owned by P1. Mounted in app.ts (`app.route("/v1", leetRoutes)`)
// by P0 Task 26 so P1 never edits app.ts; P1 adds its routes here (and their api.md 6 rows in the same commit).
import { Hono } from "hono";
import type { AppEnv } from "../env";

export const leetRoutes = new Hono<AppEnv>();
