// arch.* card data schemas (P3 Task 26, docs/contract/arch.md 3): user-sent cards of the architecture design tool.
// The server checks shape and size only — it never draws them and never runs the engine. A card copy of a design
// (`design`) is checked for its schema_version only: clients load it leniently (arch.md 2.3) and the 16 KB data cap
// (index.ts) bounds it. Strings the client draws (title, mini picture, labels) refuse control and bidi characters.
import * as z from "zod";
import { isId } from "../ids";
import type { CardSpec, CardType } from "./index";

const MAX_INT = Number.MAX_SAFE_INTEGER;
const MINI_LINES = 8;
const MINI_CELLS = 60;

// arch.md 1.3: C0 / DEL / C1 controls, bidi marks, embeddings, overrides and isolates, line and paragraph separators
const FORBIDDEN = /[\u0000-\u001f\u007f-\u009f\u061c\u200e\u200f\u202a-\u202e\u2066-\u2069\u2028\u2029]/;
const LONE_SURROGATE = /[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/;

const codePoints = (s: string): number => {
  let n = 0;
  for (const _ of s) n++;
  return n;
};

/** Text the client draws: min..max code points, no forbidden character. */
const zDrawn = (min: number, max: number) =>
  z.string().refine((s) => !FORBIDDEN.test(s) && !LONE_SURROGATE.test(s) && codePoints(s) >= min && codePoints(s) <= max);

// A lower bound of the client's cells (x/ansi `textwidth.Width`), never above it: the server must never refuse a
// line the client cut to 60 cells, and the client re-cuts on draw anyway. One grapheme cluster is 2 cells only for a
// Hangul syllable (always wide), 0 when it is only marks, format characters or Hangul fillers (zero-width to the
// client or narrower), else 1 — emoji are left at 1 since text-presentation ones (🖥 🌡) are 1 cell to the client and
// the two sides' emoji tables may differ.
const HANGUL_SYLLABLE = /^[\uac00-\ud7a3]/;
const ZERO_WIDTH = /^[\p{M}\p{Cf}\u115f\u1160\u3164\uffa0]+$/u;
const graphemes = new Intl.Segmenter(undefined, { granularity: "grapheme" });

/** Terminal cells of s, at most what the client measures. */
export function cells(s: string): number {
  let n = 0;
  for (const { segment } of graphemes.segment(s)) {
    if (ZERO_WIDTH.test(segment)) continue;
    n += HANGUL_SYLLABLE.test(segment) ? 2 : 1;
  }
  return n;
}

const zMiniLine = z.string().refine((s) => !FORBIDDEN.test(s) && !LONE_SURROGATE.test(s) && cells(s) <= MINI_CELLS);
const zInt = (min = 0, max = MAX_INT) => z.number().int().min(min).max(max);
const zNullableId = z.string().refine(isId).nullable();
const zPriceAsOf = z.string().regex(/^\d{4}-(0[1-9]|1[0-2])$/);
const zSeed = z.string().regex(/^(0|[1-9]\d{0,19})$/).refine((s) => BigInt(s) <= 18_446_744_073_709_551_615n);
const zDesign = z.looseObject({ schema_version: z.literal(1) }).nullable();

/** doc_id only with its space_id; a design copy only when the card names no room document. */
const refRules = (d: { space_id: string | null; doc_id: string | null; design: unknown }) =>
  (d.doc_id === null || d.space_id !== null) && (d.design === null || d.doc_id === null);

const LIMITS = ["cpu", "iops", "nic", "pool", "ext_limit", "queue", "link"] as const;

const ref = {
  title: zDrawn(1, 80),
  space_id: zNullableId,
  doc_id: zNullableId,
  version_no: zInt(1).nullable(),
  engine_version: zInt(1),
  cost_usd_month: zInt(),
  price_asof: zPriceAsOf,
  design: zDesign,
};

export const designV1 = z
  .strictObject({
    ...ref,
    node_count: zInt(0, 300),
    mini: z.array(zMiniLine).max(MINI_LINES),
  })
  .refine(refRules);

export const simV1 = z
  .strictObject({
    ...ref,
    seed: zSeed,
    max_rps: zInt(),
    read_permille: zInt(0, 1000),
    p50_ms: zInt(),
    p99_ms: zInt(),
    error_permille: zInt(0, 1000),
    bottleneck: z
      .strictObject({ target_label: zDrawn(1, 80), limit: z.enum(LIMITS), util_permille: zInt() })
      .nullable(),
    challenge: z
      .strictObject({
        date: z.string().regex(/^\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])$/),
        scenario: z.string().regex(/^[a-z0-9_-]{1,64}$/),
        score: zInt(),
      })
      .nullable(),
  })
  .refine(refRules);

export const cards: Partial<Record<CardType, CardSpec>> = {
  "arch.design": { serverOnly: false, versions: { 1: designV1 } },
  "arch.sim": { serverOnly: false, versions: { 1: simV1 } },
};
