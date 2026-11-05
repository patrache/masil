// Text utilities (server-architecture.md 4.10, api.md 2.1): input normalization, code-point length,
// @mention / @here / #hashtag extraction (code spans and fenced blocks excluded), dice, plain snippets, emoji checks.

// Local until Task 5 defines it in types/api.ts (api.md RollResult).
export type RollResult = { expr: string; rolls: number[]; modifier: number; total: number };

const MAX_MENTIONS = 50;
const MAX_HASHTAGS = 10;

const MENTION_RE = /(?<![A-Za-z0-9_])@([a-z0-9_]{3,20})(?![a-z0-9_])/gi;
const HERE_RE = /(?<![A-Za-z0-9_])@here(?![a-z0-9_])/i;
const HASHTAG_RE = /(?<![\p{L}\p{N}_])#([\p{L}\p{N}_]{1,50})/gu;
const DICE_RE = /^(\d{1,2})d(\d{1,4})([+-]\d{1,4})?$/;

// Fenced block (``` … ``` or ``` … to end of body) first, then a single-line inline code span (`…`, ``…``).
const CODE_RE = /```[\s\S]*?(?:```|$)|(`+)(?!`)[^\n]*?(?<!`)\1(?!`)/g;

export function normalizeText(s: string): string {
  return s.normalize("NFC").trim();
}

export function charCount(s: string): number {
  let n = 0;
  for (const _ of s) n++;
  return n;
}

type Segment = { code: boolean; text: string };

/** Splits a body into plain-text and code segments (code = fenced block or inline code span, markers included). */
function splitCode(body: string): Segment[] {
  const out: Segment[] = [];
  let last = 0;
  for (const m of body.matchAll(CODE_RE)) {
    if (m.index > last) out.push({ code: false, text: body.slice(last, m.index) });
    out.push({ code: true, text: m[0] });
    last = m.index + m[0].length;
  }
  if (last < body.length) out.push({ code: false, text: body.slice(last) });
  return out;
}

/** Replaces code spans/blocks with same-length whitespace so mention/hashtag matching skips them. */
function maskCode(body: string): string {
  return splitCode(body)
    .map((s) => (s.code ? s.text.replace(/[^\n]/g, " ") : s.text))
    .join("");
}

export function extractMentions(body: string): string[] {
  const seen = new Set<string>();
  for (const m of maskCode(body.normalize("NFC")).matchAll(MENTION_RE)) {
    seen.add(m[1].toLowerCase());
    if (seen.size >= MAX_MENTIONS) break;
  }
  return [...seen];
}

export function hasHereMention(body: string): boolean {
  return HERE_RE.test(maskCode(body.normalize("NFC")));
}

export function extractHashtags(body: string): string[] {
  const seen = new Set<string>();
  for (const m of maskCode(body.normalize("NFC")).matchAll(HASHTAG_RE)) {
    seen.add(m[1].toLowerCase().normalize("NFC"));
    if (seen.size >= MAX_HASHTAGS) break;
  }
  return [...seen];
}

export function parseDice(expr: string): { count: number; sides: number; modifier: number } | null {
  const m = DICE_RE.exec(expr);
  if (!m) return null;
  const count = Number(m[1]);
  const sides = Number(m[2]);
  const modifier = m[3] ? Number(m[3]) || 0 : 0; // "-0" -> 0
  if (count < 1 || count > 20) return null;
  if (sides < 2 || sides > 1000) return null;
  if (modifier < -1000 || modifier > 1000) return null;
  return { count, sides, modifier };
}

/** Uniform integer in [1, sides] by rejection sampling over 32-bit values (no modulo bias). */
function rollDie(sides: number): number {
  const limit = Math.floor(0x1_0000_0000 / sides) * sides;
  const buf = new Uint32Array(1);
  for (;;) {
    crypto.getRandomValues(buf);
    if (buf[0] < limit) return (buf[0] % sides) + 1;
  }
}

export function rollDice(spec: { count: number; sides: number; modifier: number }): RollResult {
  const rolls = Array.from({ length: spec.count }, () => rollDie(spec.sides));
  const mod = spec.modifier || 0;
  const expr = `${spec.count}d${spec.sides}${mod > 0 ? `+${mod}` : mod < 0 ? String(mod) : ""}`;
  return { expr, rolls, modifier: mod, total: rolls.reduce((a, b) => a + b, 0) + mod };
}

function stripCodeMarkers(code: string): string {
  if (code.startsWith("```")) {
    let inner = code.slice(3);
    if (inner.endsWith("```")) inner = inner.slice(0, -3);
    const nl = inner.indexOf("\n");
    return nl >= 0 ? inner.slice(nl + 1) : inner; // drop the ```lang line
  }
  const ticks = /^`+/.exec(code)![0].length;
  return code.slice(ticks, code.length - ticks);
}

function stripInlineMarkdown(text: string): string {
  return text
    .replace(/!?\[([^\]\n]*)\]\(([^()\s]*)\)/g, "$1")
    .replace(/\*\*(?=\S)([\s\S]*?\S)\*\*/g, "$1")
    .replace(/\*(?=[^\s*])([^*]*?[^\s*])\*/g, "$1")
    .replace(/(?<![\p{L}\p{N}_])_(?=[^\s_])([^_]*?[^\s_])_(?![\p{L}\p{N}_])/gu, "$1");
}

/** Markdown-stripped, whitespace-collapsed one-line text, cut to at most maxChars code points. */
export function plainSnippet(body: string, maxChars: number): string {
  const plain = splitCode(body.normalize("NFC"))
    .map((s) => (s.code ? ` ${stripCodeMarkers(s.text)} ` : stripInlineMarkdown(s.text)))
    .join("")
    .replace(/\s+/gu, " ")
    .trim();
  if (maxChars <= 0) return "";
  const cps = [...plain];
  return cps.length <= maxChars ? plain : cps.slice(0, maxChars).join("").trimEnd();
}

const PICTOGRAPHIC_RE = /\p{Extended_Pictographic}/u;
const FLAG_RE = /^\p{Regional_Indicator}{2}$/u;
const KEYCAP_RE = /^[0-9#*]️?⃣$/u;
const graphemes = new Intl.Segmenter(undefined, { granularity: "grapheme" });
const utf8 = new TextEncoder();

/** Exactly one grapheme that is an emoji (pictographic, flag or keycap) and fits in maxBytes of UTF-8. */
export function isSingleEmoji(s: string, maxBytes: number): boolean {
  if (s === "" || utf8.encode(s).length > maxBytes) return false;
  let n = 0;
  for (const _ of graphemes.segment(s)) if (++n > 1) return false;
  return PICTOGRAPHIC_RE.test(s) || FLAG_RE.test(s) || KEYCAP_RE.test(s);
}
