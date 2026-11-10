// Workers Rate Limiting bindings (api.md 0.7, server-architecture.md 4.7).
// Limits are per Cloudflare location and approximate — abuse protection, not an exact cap.
import type { MiddlewareHandler } from "hono";
import type { AppContext, AppEnv, Env } from "../env";
import { err } from "./errors";

export type LimiterName =
  | "RL_SIGNUP"
  | "RL_LOGIN"
  | "RL_MESSAGE"
  | "RL_POST"
  | "RL_REACT"
  | "RL_UPLOAD"
  | "RL_SOCIAL"
  | "RL_SEARCH"
  | "RL_WS"
  | "RL_API"
  | "RL_DOC" // v0.1.3: room document / version / comment writes (api.md 0.7)
  | "RL_SHARE" // v0.1.3: P2 workspace share create / invite (the 60-minute window is counted in D1 by P2)
  | "RL_SHARE_WS"; // v0.1.3: P2 workspace share socket connects

/** Must equal `simple.period` of each binding in wrangler.jsonc (Retry-After / details.retry_after_sec). */
export const LIMITER_PERIOD_SEC: Record<LimiterName, 10 | 60> = {
  RL_SIGNUP: 60,
  RL_LOGIN: 60,
  RL_MESSAGE: 10,
  RL_POST: 60,
  RL_REACT: 10,
  RL_UPLOAD: 60,
  RL_SOCIAL: 60,
  RL_SEARCH: 60,
  RL_WS: 60,
  RL_API: 60,
  RL_DOC: 60,
  RL_SHARE: 60,
  RL_SHARE_WS: 60,
};

/** Counts one hit for `key`; throws 429 rate_limited when the binding says no. */
export async function checkLimit(env: Env, name: LimiterName, key: string): Promise<void> {
  const { success } = await env[name].limit({ key });
  if (!success) throw err.rateLimited(name, LIMITER_PERIOD_SEC[name]);
}

/** CF-Connecting-IP header, "0.0.0.0" if absent. */
export function clientIp(c: AppContext): string {
  return c.req.header("CF-Connecting-IP") || "0.0.0.0";
}

/**
 * Property key under which limit() records its limiter name on the middleware it returns. The api.md 6 contract test
 * (test/unit/contract-limiters.test.ts) reads it off createApp().routes, so a limiter reaches the table check however
 * the middleware was built (inline, hoisted into a constant, or returned by a wrapper).
 */
export const LIMITER_OF: unique symbol = Symbol("masil.limiter");

/** Default key: `u:${auth.userId}` — use after requireAuth. */
export function limit(name: LimiterName, key?: (c: AppContext) => string): MiddlewareHandler<AppEnv> {
  const mw: MiddlewareHandler<AppEnv> = async (c, next) => {
    const k = key ? key(c) : `u:${c.get("auth").userId}`;
    await checkLimit(c.env, name, k);
    await next();
  };
  return Object.defineProperty(mw, LIMITER_OF, { value: name });
}
