// App assembly (server-architecture.md 5). Global middleware is mounted with app.use("*", …) only — never
// under "/v1/*" — and route modules attach requireAuth per route (plan 01 Global Constraints).
import { Hono, type MiddlewareHandler } from "hono";
import { bodyLimit } from "hono/body-limit";
import type { AppEnv } from "./env";
import { ApiError, notFoundHandler, onError } from "./lib/errors";
import { newId } from "./lib/ids";
import { clientVersionGate } from "./lib/version";
import { authRoutes } from "./routes/auth";
import { blockRoutes } from "./routes/blocks";
import { channelRoutes } from "./routes/channels";
import { dmRoutes } from "./routes/dms";
import { followRoutes } from "./routes/follows";
import { friendRoutes } from "./routes/friends";
import { inviteRoutes } from "./routes/invites";
import { meRoutes } from "./routes/me";
import { messageRoutes } from "./routes/messages";
import { metaRoutes } from "./routes/meta";
import { notificationRoutes } from "./routes/notifications";
import { postRoutes } from "./routes/posts";
import { presenceRoutes } from "./routes/presence";
import { searchRoutes } from "./routes/search";
import { spaceRoutes } from "./routes/spaces";
import { timelineRoutes } from "./routes/timelines";
import { uploadRoutes } from "./routes/uploads";
import { userRoutes } from "./routes/users";
import { wsRoutes } from "./routes/ws";

export const JSON_BODY_MAX_BYTES = 64 * 1024;

const JSON_CONTENT_TYPE_RE = /^application\/([a-z-.]+\+)?json(;\s*[a-zA-Z0-9-]+=([^;]+))*$/i;

/** Sets c.var.requestId and the X-Request-Id response header (ULID). */
export const requestId: MiddlewareHandler<AppEnv> = async (c, next) => {
  const id = newId();
  c.set("requestId", id);
  c.header("X-Request-Id", id);
  await next();
  if (!c.res.headers.has("X-Request-Id")) {
    try {
      c.res.headers.set("X-Request-Id", id);
    } catch {
      // immutable headers (e.g. a Response passed through from a Durable Object) — leave as is
    }
  }
};

/** Drops any incoming X-Masil-* header: those are only set by the Worker itself for internal hops. */
export const stripInternalHeaders: MiddlewareHandler<AppEnv> = async (c, next) => {
  const internal: string[] = [];
  for (const name of c.req.raw.headers.keys()) if (name.toLowerCase().startsWith("x-masil-")) internal.push(name);
  if (internal.length > 0) {
    const headers = new Headers(c.req.raw.headers);
    for (const name of internal) headers.delete(name);
    c.req.raw = new Request(c.req.raw, { headers });
  }
  await next();
};

const limitJsonBody = bodyLimit({
  maxSize: JSON_BODY_MAX_BYTES,
  onError: () => {
    throw new ApiError(413, "payload_too_large", `request body exceeds ${JSON_BODY_MAX_BYTES} bytes`);
  },
});

/** POST /v1/uploads carries raw image bytes (never JSON) and enforces its own 5MB guard after auth. */
const isUploadPost = (method: string, path: string) => method === "POST" && path === "/v1/uploads";

/** 64KB body cap for everything except POST /v1/uploads (that route enforces its own 5MB guard). */
export const jsonBodyLimit: MiddlewareHandler<AppEnv> = async (c, next) => {
  if (isUploadPost(c.req.method, c.req.path)) return next();
  return limitJsonBody(c, next);
};

/**
 * A JSON-typed, non-empty body that does not parse -> 400 invalid_json (before any route runs).
 * Skips POST /v1/uploads: that body is uncapped here, so reading it would let an unauthenticated client make the
 * isolate buffer and parse megabytes before auth; the route itself rejects a non-image Content-Type with 415.
 */
export const jsonParseCheck: MiddlewareHandler<AppEnv> = async (c, next) => {
  if (isUploadPost(c.req.method, c.req.path)) return next();
  const contentType = c.req.header("Content-Type");
  if (c.req.raw.body && contentType && JSON_CONTENT_TYPE_RE.test(contentType)) {
    const text = await c.req.text(); // cached by HonoRequest; c.req.json() later reuses it
    if (text.trim() !== "") {
      try {
        JSON.parse(text);
      } catch {
        throw new ApiError(400, "invalid_json", "request body is not valid JSON");
      }
    }
  }
  await next();
};

export function createApp(): Hono<AppEnv> {
  const app = new Hono<AppEnv>();
  app.use("*", requestId);
  app.use("*", stripInternalHeaders);
  app.use("*", clientVersionGate);
  app.use("*", jsonBodyLimit);
  app.use("*", jsonParseCheck);
  app.onError(onError);
  app.notFound(notFoundHandler);
  // Route modules in server-architecture.md section 2 order; each task adds its own line.
  app.route("/v1", metaRoutes);
  app.route("/v1", authRoutes);
  app.route("/v1", meRoutes);
  app.route("/v1", presenceRoutes);
  app.route("/v1", userRoutes);
  app.route("/v1", friendRoutes);
  app.route("/v1", blockRoutes);
  app.route("/v1", followRoutes);
  app.route("/v1", dmRoutes);
  app.route("/v1", spaceRoutes);
  app.route("/v1", inviteRoutes);
  app.route("/v1", channelRoutes);
  app.route("/v1", messageRoutes);
  app.route("/v1", searchRoutes);
  app.route("/v1", uploadRoutes);
  app.route("/v1", postRoutes);
  app.route("/v1", timelineRoutes);
  app.route("/v1", notificationRoutes);
  app.route("/v1", wsRoutes);
  return app;
}
