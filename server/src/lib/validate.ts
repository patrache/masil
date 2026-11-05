// zod request validation (server-architecture.md 4.3). Failures become
// 400 invalid_request with details.fields.<dotted.path> = "required" | "too_long" | "too_short" | "invalid".
import { zValidator } from "@hono/zod-validator";
import type { MiddlewareHandler } from "hono";
import * as z from "zod";
import type { AppEnv } from "../env";
import { err, type FieldError } from "./errors";
import { isId } from "./ids";
import { charCount, normalizeText } from "./text";

type Issue = z.core.$ZodIssue;

const ROOT_KEY = "request"; // issue on the whole body/query (e.g. a JSON array where an object was expected)

function valueAt(data: unknown, path: readonly PropertyKey[]): unknown {
  let cur: unknown = data;
  for (const key of path) {
    if (cur === null || typeof cur !== "object") return undefined;
    cur = (cur as Record<PropertyKey, unknown>)[key];
  }
  return cur;
}

function fieldError(issue: Issue, data: unknown): FieldError {
  if (valueAt(data, issue.path) === undefined) return "required";
  if (issue.code === "too_big" && issue.origin === "string") return "too_long";
  if (issue.code === "too_small" && issue.origin === "string") return "too_short";
  return "invalid";
}

/** Exported for tests and for routes that validate by hand (e.g. path params). */
export function issuesToFields(issues: readonly Issue[], data: unknown): Record<string, FieldError> {
  const fields: Record<string, FieldError> = {};
  for (const issue of issues) {
    const key = issue.path.length > 0 ? issue.path.map(String).join(".") : ROOT_KEY;
    if (!(key in fields)) fields[key] = fieldError(issue, data);
  }
  return fields;
}

type Validated<Target extends "json" | "query", T extends z.ZodType> = MiddlewareHandler<
  AppEnv,
  string,
  { in: { [K in Target]: z.input<T> }; out: { [K in Target]: z.output<T> } }
>;

function make<Target extends "json" | "query", T extends z.ZodType>(target: Target, schema: T): Validated<Target, T> {
  const mw = zValidator(target, schema, (result) => {
    if (!result.success) throw err.invalid(issuesToFields(result.error.issues as readonly Issue[], result.data));
  });
  return mw as unknown as Validated<Target, T>;
}

/** Validates the JSON body; read it with `c.req.valid("json")`. */
export function vJson<T extends z.ZodType>(schema: T): Validated<"json", T> {
  return make("json", schema);
}

/** Validates the query string; read it with `c.req.valid("query")`. */
export function vQuery<T extends z.ZodType>(schema: T): Validated<"query", T> {
  return make("query", schema);
}

// ---- shared zod pieces ----

/** ULID (26 Crockford base32 chars). */
export const zId: z.ZodType<string, string> = z.string().refine(isId, { message: "invalid id" });

const HANDLE_REF_RE = /^@[A-Za-z0-9_]{3,20}$/;

/** "@handle" (lowercased) | ULID | "@me" (api.md 0.2). */
export const zUserRef: z.ZodType<string, string> = z
  .string()
  .refine((s) => s === "@me" || isId(s) || HANDLE_REF_RE.test(s), { message: "invalid user reference" })
  .transform((s) => (s.startsWith("@") ? s.toLowerCase() : s));

/** client_nonce: 1..64 of [A-Za-z0-9_-]. */
export const zNonce: z.ZodType<string, string> = z
  .string()
  .min(1)
  .max(64)
  .regex(/^[A-Za-z0-9_-]+$/);

/** NFC + trim, then min..max in code points (api.md 0.3). */
export function zText(min: number, max: number): z.ZodType<string, string> {
  return z.string().transform((raw, ctx) => {
    const s = normalizeText(raw);
    const n = charCount(s);
    if (n < min) {
      ctx.addIssue({ code: "too_small", origin: "string", minimum: min, inclusive: true, input: s });
      return z.NEVER;
    }
    if (n > max) {
      ctx.addIssue({ code: "too_big", origin: "string", maximum: max, inclusive: true, input: s });
      return z.NEVER;
    }
    return s;
  });
}

/** PollInput (api.md 2, 0.3): question 1..200, 2..10 options of 1..80, multi default false, duration_sec 60..604800 | null. */
export const zPollInput = z.object({
  question: zText(1, 200),
  options: z.array(zText(1, 80)).min(2).max(10),
  multi: z.boolean().optional(),
  duration_sec: z.number().int().min(60).max(604_800).nullable().optional(),
});

/** Text channel name (api.md 0.3): NFC + trim, then `^[a-z0-9가-힣_-]{1,32}$` (no case folding). */
export const CHANNEL_NAME_RE = /^[a-z0-9가-힣_-]{1,32}$/;

export const zChannelName: z.ZodType<string, string> = z
  .string()
  .transform((s) => normalizeText(s))
  .refine((s) => CHANNEL_NAME_RE.test(s), { message: "invalid channel name" });
