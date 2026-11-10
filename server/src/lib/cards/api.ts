// api.* card schemas (v0.1.3 C3.11, api.md 2 카드 type 표) — owned by P4 (API testing). User-sent. The client masks
// secrets and fits the data under 16KB before sending; the server checks the shape only (C3.12).
import * as z from "zod";
import type { CardSpec, CardType } from "./index";

const count = z.number().int().min(0);
/** at most n code points (the client counts runes, not UTF-16 units) */
const chars = (n: number) => z.string().refine((s) => [...s].length <= n);

/** api.result v1: one request's result. */
const result = z.strictObject({
  method: z.string().min(1),
  url: z.string(),
  status: z.number().int().min(0).max(999),
  time_ms: count,
  size: count,
  asserts: z.strictObject({ passed: count, total: count }),
  env: z.string().optional(),
  ref: z.strictObject({ collection: z.string(), request_id: z.string(), doc_id: z.string().optional() }),
  req_headers: z.array(z.tuple([z.string(), z.string()])).max(20).optional(),
  body_head: z.array(chars(200)).max(20).optional(),
});

/** api.run v1: a runner summary. */
const run = z.strictObject({
  collection: z.string(),
  requests: count,
  passed: count,
  failed: count,
  time_ms: count,
  failures: z
    .array(z.strictObject({ name: z.string(), method: z.string(), status: z.number().int().min(0).max(999), reason: z.string() }))
    .max(3),
  ref: z.strictObject({ doc_id: z.string().optional() }),
});

export const cards: Partial<Record<CardType, CardSpec>> = {
  "api.result": { serverOnly: false, versions: { 1: result } },
  "api.run": { serverOnly: false, versions: { 1: run } },
};
