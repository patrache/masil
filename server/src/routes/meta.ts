// GET /v1/meta, GET /v1/health (api.md 4). No auth.
import { Hono } from "hono";
import type { AppEnv } from "../env";
import { iso, now } from "../lib/time";

export const API_VERSION = 1;

/** api.md 4 `limits`, verbatim. */
export const META_LIMITS = {
  message_max_chars: 4000,
  post_max_chars: 500,
  post_max_media: 4,
  upload_max_bytes: 5_242_880,
  group_dm_max_members: 10,
  story_ttl_sec: 86_400,
  poll_max_options: 10,
  reactions_per_message: 20,
  watch_topics_max: 50,
} as const;

export const metaRoutes = new Hono<AppEnv>();

metaRoutes.get("/meta", (c) =>
  c.json({
    api_version: API_VERSION,
    server_time: iso(now()),
    min_client_version: c.env.MIN_CLIENT_VERSION,
    limits: META_LIMITS,
  }),
);

metaRoutes.get("/health", (c) => c.json({ ok: true }));
