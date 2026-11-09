// LeetCode daily problem — pure helpers (v0.1.3 A10.3, P1 Task 1). The server never calls LeetCode: it keeps only
// facts (date, number, slug, title, difficulty, tags, paid flag) and solve records. Links are always built from the
// slug on a fixed host, so a false report can only show the wrong problem, never a phishing link.
import * as z from "zod";
import { charCount, normalizeText } from "./text";

export const SLUG_RE = /^[a-z0-9-]{1,100}$/;
export const FRONTEND_ID_RE = /^[0-9]{1,6}$/;
export const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

const DAY_MS = 86_400_000;

/** The UTC calendar day of `ms` as YYYY-MM-DD (the leet day boundary is UTC midnight, JA10). */
export function utcDate(ms: number): string {
  return new Date(ms).toISOString().slice(0, 10);
}

/** YYYY-MM-DD that names a real calendar day. */
export function isValidDate(date: string): boolean {
  if (!DATE_RE.test(date)) return false;
  const ms = Date.parse(`${date}T00:00:00.000Z`);
  return Number.isFinite(ms) && utcDate(ms) === date;
}

/** Facts may be reported for today (UTC) or yesterday only (A10.3). */
export function isReportableDate(date: string, nowMs: number): boolean {
  return isValidDate(date) && (date === utcDate(nowMs) || date === utcDate(nowMs - DAY_MS));
}

/** The one link a leet card carries (its whole body). */
export function leetLink(slug: string): string {
  return `https://leetcode.com/problems/${slug}/`;
}

/** NFC + trim, then min..max code points (like validate.ts zText; repeated here so lib/leet stays leaf-level). */
function text(min: number, max: number): z.ZodType<string, string> {
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

/** Code as the user wrote it (not trimmed): 1..4000 code points like a message body. */
export const CODE_MAX = 4000;
export const zCode: z.ZodType<string, string> = z.string().superRefine((s, ctx) => {
  const n = charCount(s);
  if (n < 1) ctx.addIssue({ code: "too_small", origin: "string", minimum: 1, inclusive: true, input: s });
  else if (n > CODE_MAX) ctx.addIssue({ code: "too_big", origin: "string", maximum: CODE_MAX, inclusive: true, input: s });
});

export const zSlug = z.string().regex(SLUG_RE);
export const zFrontendId = z.string().regex(FRONTEND_ID_RE);
export const zLeetDate = z.string().refine(isValidDate);
export const zLang = z.string().min(1).max(32);

export const DIFFICULTIES = ["Easy", "Medium", "Hard"] as const;
export type Difficulty = (typeof DIFFICULTIES)[number];

/** What a client reports for a day (strict: a body / HTML field cannot ride along). */
export const LeetDailyFacts = z.strictObject({
  frontend_id: zFrontendId,
  slug: zSlug,
  title: text(1, 200),
  difficulty: z.enum(DIFFICULTIES),
  tags: z.array(text(1, 50)).max(20),
  is_paid_only: z.boolean(),
});
export type LeetDailyFactsInput = z.output<typeof LeetDailyFacts>;

/** leet.daily card data v1 = the stored facts + the day. */
export const LeetDailyCardData = LeetDailyFacts.extend({ date: zLeetDate });
export type LeetDailyCard = z.output<typeof LeetDailyCardData>;

/** leet.solve card data v1. The code (optional) is always a spoiler. */
export const LeetSolveCardData = z.strictObject({
  date: zLeetDate,
  frontend_id: zFrontendId,
  slug: zSlug,
  lang: zLang,
  runtime_percentile: z.number().min(0).max(100).nullable(),
  code: z.strictObject({ lang: zLang, text: zCode }).nullable(),
  spoiler: z.literal(true),
});
export type LeetSolveCard = z.output<typeof LeetSolveCardData>;

function dayIndex(date: string): number {
  return Math.round(Date.parse(`${date}T00:00:00.000Z`) / DAY_MS);
}

/**
 * Streaks over the filled days (each `utcDate(passed_at) === date`, JA26 — no grace day). `best` is the longest run
 * of consecutive days; `current` is the run that ends today, or — while today is still empty — the run that ends
 * yesterday; otherwise 0. Order and duplicates do not matter; invalid dates and days after `today` are ignored.
 */
export function computeStreak(filledDates: string[], today: string): { current: number; best: number } {
  const t = dayIndex(today);
  const days = [...new Set(filledDates.filter(isValidDate).map(dayIndex))].filter((d) => d <= t).sort((a, b) => a - b);
  let best = 0;
  let run = 0;
  let prev = Number.NaN;
  const runEndingAt = new Map<number, number>();
  for (const d of days) {
    run = d === prev + 1 ? run + 1 : 1;
    prev = d;
    runEndingAt.set(d, run);
    if (run > best) best = run;
  }
  const current = runEndingAt.get(t) ?? runEndingAt.get(t - 1) ?? 0;
  return { current, best };
}
