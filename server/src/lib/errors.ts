// Error envelope (api.md 0.5, server-architecture.md 4.2):
// { "error": { "code": string, "message": string, "details": object } }
import { HTTPException } from "hono/http-exception";
import type { AppContext } from "../env";
import { isoOrNull } from "./time";
import { errFields } from "./log";

export type ErrorCode =
  | "invalid_json"
  | "invalid_request"
  | "unauthorized"
  | "invalid_credentials"
  | "account_suspended"
  | "forbidden"
  | "not_admin"
  | "dm_not_allowed"
  | "action_not_allowed"
  | "read_only"
  | "private_account"
  | "banned_from_space"
  | "not_found"
  | "handle_taken"
  | "already_exists"
  | "already_member"
  | "conflict"
  | "poll_closed"
  | "invite_invalid"
  | "payload_too_large"
  | "unsupported_media_type"
  | "limit_exceeded"
  | "upgrade_required"
  | "rate_limited"
  | "internal_error"
  | "service_unavailable";

export type ErrorStatus = 400 | 401 | 403 | 404 | 409 | 410 | 413 | 415 | 422 | 426 | 429 | 500 | 503;

export type FieldError = "required" | "too_long" | "too_short" | "invalid" | "not_allowed";

export type ForbiddenCode =
  | "forbidden"
  | "not_admin"
  | "dm_not_allowed"
  | "action_not_allowed"
  | "read_only"
  | "private_account"
  | "banned_from_space";

export type ConflictCode = "handle_taken" | "already_exists" | "already_member" | "conflict" | "poll_closed";

export class ApiError extends Error {
  constructor(
    readonly status: ErrorStatus,
    readonly code: ErrorCode,
    message: string,
    readonly details: Record<string, unknown> = {},
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export const err = {
  invalid(fields: Record<string, FieldError>): ApiError {
    const message = Object.entries(fields)
      .map(([k, v]) => `${k}: ${v}`)
      .join(", ");
    return new ApiError(400, "invalid_request", message || "invalid request", { fields });
  },
  unauthorized(): ApiError {
    return new ApiError(401, "unauthorized", "authentication required");
  },
  forbidden(code: ForbiddenCode = "forbidden", message?: string, details: Record<string, unknown> = {}): ApiError {
    return new ApiError(403, code, message ?? code.replace(/_/g, " "), details);
  },
  suspended(until: number | null, reason: string): ApiError {
    return new ApiError(403, "account_suspended", "account suspended", { until: isoOrNull(until), reason });
  },
  notFound(what?: string): ApiError {
    return new ApiError(404, "not_found", what ? `${what} not found` : "not found");
  },
  conflict(code: ConflictCode, reason?: string): ApiError {
    const message = reason ? `${code.replace(/_/g, " ")}: ${reason}` : code.replace(/_/g, " ");
    return new ApiError(409, code, message, reason !== undefined ? { reason } : {});
  },
  inviteInvalid(reason: "expired" | "exhausted" | "revoked"): ApiError {
    return new ApiError(410, "invite_invalid", `invite ${reason}`, { reason });
  },
  limit(what: string, limit: number): ApiError {
    return new ApiError(422, "limit_exceeded", `${what} limit (${limit}) exceeded`, { what, limit });
  },
  rateLimited(limiter: string, retryAfterSec: number): ApiError {
    return new ApiError(429, "rate_limited", "rate limited", { limiter, retry_after_sec: retryAfterSec });
  },
};

function envelope(c: AppContext, e: ApiError): Response {
  if (e.code === "rate_limited" && typeof e.details.retry_after_sec === "number") {
    c.header("Retry-After", String(e.details.retry_after_sec));
  }
  return c.json({ error: { code: e.code, message: e.message, details: e.details } }, e.status);
}

/** ApiError -> envelope; Hono's own 400/413 (malformed JSON, body limit) -> matching codes; anything else -> 500 (logged). */
export function onError(e: unknown, c: AppContext): Response {
  if (e instanceof ApiError) return envelope(c, e);
  if (e instanceof HTTPException) {
    if (e.status === 400) return envelope(c, new ApiError(400, "invalid_json", "request body is not valid JSON"));
    if (e.status === 413) return envelope(c, new ApiError(413, "payload_too_large", "request body too large"));
  }
  console.error("unhandled error", { method: c.req.method, path: c.req.path, err: errFields(e) });
  return envelope(c, new ApiError(500, "internal_error", "internal error"));
}

export function notFoundHandler(c: AppContext): Response {
  return envelope(c, err.notFound());
}
