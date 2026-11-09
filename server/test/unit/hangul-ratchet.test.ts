// i18n 7.6: the server builds no user-facing sentences — alert text lives in src/lib/i18n.ts only. This ratchet counts,
// per file, the string / template literals that contain Hangul (comments and regex literals do not count) and fails
// when a file has more or fewer than test/fixtures/hangul-allow.json says. src/lib/i18n.ts (the catalog) and
// src/lib/validate.ts (the channel-name rule) are not counted. Lowering a count means editing the allow file too.
import { describe, it, expect } from "vitest";

const sources = import.meta.glob("../../src/**/*.ts", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const allowRaw = import.meta.glob("../fixtures/hangul-allow.json", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const allow = JSON.parse(Object.values(allowRaw)[0]!) as Record<string, number>;

const EXEMPT = new Set(["src/lib/i18n.ts", "src/lib/validate.ts"]);
const HANGUL = /[ᄀ-ᇿ㄰-㆏ꥠ-꥿가-힯ힰ-퟿]/;

/** The text of every string and template literal in `src`, skipping comments and regex literals (small tokenizer). */
function stringLiterals(src: string): string[] {
  const out: string[] = [];
  let i = 0;
  let prevSignificant = ""; // last non-space char outside literals/comments: decides "/" = regex or division
  const n = src.length;
  const regexAllowedAfter = (c: string) => c === "" || "(,=:[!&|?{};+-*%<>~^".includes(c);
  const readQuoted = (q: string): string => {
    let s = "";
    i++;
    while (i < n && src[i] !== q) {
      if (src[i] === "\\") {
        s += src[i]! + (src[i + 1] ?? "");
        i += 2;
        continue;
      }
      s += src[i];
      i++;
    }
    i++;
    return s;
  };
  // template literal: returns its text; nested `${ ... }` expressions are scanned recursively for their own literals
  const readTemplate = (): string => {
    let s = "";
    i++;
    while (i < n && src[i] !== "`") {
      if (src[i] === "\\") {
        s += src[i]! + (src[i + 1] ?? "");
        i += 2;
        continue;
      }
      if (src[i] === "$" && src[i + 1] === "{") {
        i += 2;
        let depth = 1;
        const start = i;
        while (i < n && depth > 0) {
          const c = src[i]!;
          if (c === "'" || c === '"') {
            readQuoted(c);
            continue;
          }
          if (c === "`") {
            out.push(readTemplate());
            continue;
          }
          if (c === "{") depth++;
          else if (c === "}") depth--;
          i++;
        }
        void start;
        s += "${}";
        continue;
      }
      s += src[i];
      i++;
    }
    i++;
    return s;
  };
  while (i < n) {
    const c = src[i]!;
    const d = src[i + 1];
    if (c === "/" && d === "/") {
      while (i < n && src[i] !== "\n") i++;
      continue;
    }
    if (c === "/" && d === "*") {
      const end = src.indexOf("*/", i + 2);
      i = end < 0 ? n : end + 2;
      continue;
    }
    if (c === "'" || c === '"') {
      out.push(readQuoted(c));
      prevSignificant = "a";
      continue;
    }
    if (c === "`") {
      out.push(readTemplate());
      prevSignificant = "a";
      continue;
    }
    if (c === "/" && regexAllowedAfter(prevSignificant)) {
      // regex literal: skip to the closing unescaped "/" outside a class, then its flags
      i++;
      let inClass = false;
      while (i < n) {
        const r = src[i]!;
        if (r === "\\") {
          i += 2;
          continue;
        }
        if (r === "[") inClass = true;
        else if (r === "]") inClass = false;
        else if (r === "/" && !inClass) break;
        else if (r === "\n") break;
        i++;
      }
      i++;
      while (i < n && /[a-z]/i.test(src[i]!)) i++;
      prevSignificant = "a";
      continue;
    }
    if (!/\s/.test(c)) {
      // identifiers / keywords that a regex may follow (return /x/, typeof /x/ ...)
      if (/[A-Za-z0-9_$]/.test(c)) {
        let w = "";
        while (i < n && /[A-Za-z0-9_$]/.test(src[i]!)) w += src[i++];
        prevSignificant = ["return", "typeof", "case", "in", "of", "delete", "void", "throw", "new", "else", "do", "yield", "await"].includes(w) ? "" : "a";
        continue;
      }
      prevSignificant = c === ")" || c === "]" || c === "}" ? "a" : c;
    }
    i++;
  }
  return out;
}

function countsBySource(): Record<string, number> {
  const out: Record<string, number> = {};
  for (const [path, src] of Object.entries(sources)) {
    const rel = path.replace(/^(\.\.\/)+/, "");
    if (EXEMPT.has(rel)) continue;
    const n = stringLiterals(src).filter((s) => HANGUL.test(s)).length;
    if (n > 0) out[rel] = n;
  }
  return out;
}

describe("hangul ratchet (i18n 7.6: no Korean sentences outside src/lib/i18n.ts)", () => {
  it("the tokenizer skips comments and regex literals", () => {
    const src = [
      "// 주석 '따옴표'",
      "/* 블록 `주석` */",
      "const re = /[가-힣]+/u;",
      "const a = '하나';",
      'const b = "둘" + x / 2 / y;',
      "const c = `셋 ${n} 넷 ${`다섯`}`;",
      "const d = 'plain';",
    ].join("\n");
    expect(stringLiterals(src).filter((s) => HANGUL.test(s))).toEqual(["하나", "둘", "다섯", "셋 ${} 넷 ${}"]);
  });

  it("per-file counts match test/fixtures/hangul-allow.json", () => {
    expect(countsBySource()).toEqual(allow);
  });
});
