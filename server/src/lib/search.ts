// Search query building and highlights (server-architecture.md 4.13, data-model.md 4).
// Rule 1: NFC + trim, split on whitespace, every term wrapped in "…" (inner " doubled) and joined with AND, so no
// FTS5 syntax ever reaches MATCH. Rules 2/3: trigram FTS only matches 3+ code-point substrings, so a single shorter
// term switches the whole query to the LIKE fallback. Rule 6: highlights are case-insensitive, in code points.
import type { SearchHighlight } from "../types/api";
import { charCount, normalizeText } from "./text";

/** Trigram tokenizer: shortest term (code points) FTS can match. */
export const FTS_MIN_TERM = 3;

export function buildMessageSearch(q: string): { mode: "fts"; match: string; terms: string[] } | { mode: "like"; terms: string[] } {
  const terms = normalizeText(q)
    .split(/\s+/u)
    .filter((t) => t !== "");
  if (terms.some((t) => charCount(t) < FTS_MIN_TERM)) return { mode: "like", terms };
  const match = terms.map((t) => `"${t.replace(/"/g, '""')}"`).join(" AND ");
  return { mode: "fts", match, terms };
}

function foldedCodePoints(s: string): string[] {
  return Array.from(s, (ch) => ch.toLowerCase()); // per code point, so positions stay code-point indices of `s`
}

/** Every occurrence of every term in `body` (case-insensitive), sorted by start, overlaps dropped (earlier/longer wins). */
export function highlights(body: string, terms: string[]): SearchHighlight[] {
  const b = foldedCodePoints(body);
  const found: SearchHighlight[] = [];
  for (const term of terms) {
    const t = foldedCodePoints(term);
    if (t.length === 0) continue;
    for (let i = 0; i + t.length <= b.length; ) {
      let hit = true;
      for (let j = 0; j < t.length; j++) {
        if (b[i + j] !== t[j]) {
          hit = false;
          break;
        }
      }
      if (hit) {
        found.push({ start: i, length: t.length });
        i += t.length;
      } else i++;
    }
  }
  found.sort((x, y) => x.start - y.start || y.length - x.length);
  const out: SearchHighlight[] = [];
  let end = 0;
  for (const h of found) {
    if (h.start < end) continue;
    out.push(h);
    end = h.start + h.length;
  }
  return out;
}
