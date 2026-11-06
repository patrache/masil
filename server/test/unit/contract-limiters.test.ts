// Pending item 8-a: the "추가 리미터" column of api.md 6 (endpoint summary table) drifted from the code once —
// POST /v1/channels/{channel_id}/members gained limit("RL_SOCIAL") while its row stayed blank. Clients and reviewers
// read the table as the contract, so this test re-derives the column from the code and compares both ways: every
// route the app serves has a row whose limiters for that method are exactly the ones the code checks, and every
// (method, path) in the table is a route the app serves. RL_API (applied by requireAuth to every authenticated
// request) is the base limiter, not an "additional" one, so neither side lists it.
//
// Where the code's limiters come from:
// - limit("RL_X") middleware: read at runtime. limit() tags the middleware it returns (LIMITER_OF), and
//   createApp().routes lists every handler of every route, so a limiter counts however the middleware was built.
// - checkLimit(c.env, "RL_X", key) inside a handler: read from the source, inside the parentheses of the route's
//   registration call (`xRoutes.post("/path", ..., async (c) => { ... })`).
// Both depend on limiters being applied only in those forms, so a lint over all of src rejects every other way of
// reaching a rate limiter (pending item 8-a follow-up: hoisted constants, wrappers in lib/ratelimit.ts, `.js`
// import specifiers and re-exports all used to pass unseen).
import { describe, it, expect } from "vitest";
import { COMPOSED_HANDLER } from "hono/utils/constants";
import { createApp } from "../../src/app";
import * as ratelimit from "../../src/lib/ratelimit";
import { LIMITER_OF, limit } from "../../src/lib/ratelimit";

const routeSources = import.meta.glob("../../src/routes/*.ts", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const allSources = import.meta.glob("../../src/**/*.ts", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const docs = import.meta.glob("../../../docs/contract/api.md", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"] as const;
const BASE_LIMITER = "RL_API";
const RATELIMIT_TS = "src/lib/ratelimit.ts";
/** What src may import from lib/ratelimit. Anything else (a new helper) must be added here on purpose. */
const RATELIMIT_IMPORTS = new Set(["checkLimit", "clientIp", "limit", "LimiterName", "LIMITER_PERIOD_SEC"]);

const relPath = (file: string) => file.replace(/^(\.\.\/)+/, "");

/** `/v1/users/{user}` and `/v1/users/:user` both become `/v1/users/*` (parameter names differ between doc and code). */
function normPath(p: string): string {
  return p.replace(/(?<!\$)\{[^}]+\}/g, "*").replace(/:\w+/g, "*");
}

/** "METHOD /path" -> sorted additional limiters, from api.md section 6. */
function parseDocTable(md: string): Map<string, string[]> {
  const start = md.indexOf("\n## 6. ");
  if (start < 0) throw new Error("api.md: section 6 not found");
  const end = md.indexOf("\n## ", start + 1);
  const section = md.slice(start, end < 0 ? undefined : end);
  const out = new Map<string, string[]>();
  for (const line of section.split("\n")) {
    const cells = line.split("|").slice(1, -1).map((s) => s.trim());
    if (cells.length !== 4 || cells[0] === "메서드" || /^-+$/.test(cells[0])) continue;
    const [methodCell, pathCell, , limiterCell] = cells;
    const methods = methodCell.split("/");
    for (const m of methods) if (!(METHODS as readonly string[]).includes(m)) throw new Error(`api.md 6: bad method ${m} in: ${line}`);
    // "/v1/a/{x}/like, /repost" -> /v1/a/{x}/like, /v1/a/{x}/repost (a bare suffix replaces the last segment)
    const items = pathCell.split(",").map((s) => s.trim());
    const first = items[0];
    const paths = items.map((p) => (p.startsWith("/v1/") ? p : first.slice(0, first.lastIndexOf("/")) + p));
    // "RL_X" applies to every method of the row; "RL_X(PUT/DELETE)" only to the listed ones; "—" / blank: none
    const perMethod = new Map<string, Set<string>>(methods.map((m) => [m, new Set<string>()]));
    if (limiterCell !== "" && limiterCell !== "—") {
      for (const part of limiterCell.split(",").map((s) => s.trim())) {
        const mm = /^(RL_[A-Z]+)(?:\(([A-Z/]+)\))?$/.exec(part);
        if (!mm) throw new Error(`api.md 6: bad limiter cell in: ${line}`);
        const applies = mm[2] ? mm[2].split("/") : methods;
        for (const m of applies) {
          if (!perMethod.has(m)) throw new Error(`api.md 6: limiter ${part} names a method the row lacks: ${line}`);
          perMethod.get(m)!.add(mm[1]);
        }
      }
    }
    for (const p of paths) {
      for (const m of methods) {
        const key = `${m} ${normPath(p)}`;
        if (out.has(key)) throw new Error(`api.md 6: ${key} listed twice`);
        out.set(key, [...perMethod.get(m)!].sort());
      }
    }
  }
  return out;
}

interface Masked {
  /** Same length as the source: comments and the text inside string / template / regex literals are spaces. */
  code: string;
  /** Every string and template literal: [start, end) of the whole literal (quotes included) and its raw text. */
  strings: { start: number; end: number; text: string }[];
}

const REGEX_AFTER_WORD = new Set(["return", "typeof", "case", "do", "else", "in", "of", "new", "delete", "void", "throw", "await", "yield"]);

/** A small TS lexer: enough to tell code from comments and literals, so the lint below can match brackets. */
function maskSource(src: string): Masked {
  const out = src.split("");
  const strings: Masked["strings"] = [];
  const n = src.length;
  const blank = (a: number, b: number) => {
    for (let k = a; k < b && k < n; k++) if (out[k] !== "\n") out[k] = " ";
  };
  // `/` starts a regex literal unless it follows something that ends an expression (identifier, number, `)`, `]`, ...).
  const regexAllowed = (i: number): boolean => {
    let k = i - 1;
    while (k >= 0 && /\s/.test(out[k])) k--;
    if (k < 0) return true;
    if ("(,=:[!&|?{};+-*%<>~^".includes(out[k])) return true;
    if (!/[\w$]/.test(out[k])) return false;
    let w = k;
    while (w > 0 && /[\w$]/.test(out[w - 1])) w--;
    return REGEX_AFTER_WORD.has(out.slice(w, k + 1).join(""));
  };
  const template = (start: number): number => {
    let i = start + 1;
    while (i < n) {
      if (src[i] === "\\") { blank(i, i + 2); i += 2; continue; }
      if (src[i] === "`") break;
      if (src[i] === "$" && src[i + 1] === "{") { i = code(i + 2, true); continue; }
      blank(i, i + 1);
      i++;
    }
    strings.push({ start, end: i + 1, text: src.slice(start + 1, i) });
    return i + 1;
  };
  const code = (start: number, inTemplate: boolean): number => {
    let i = start;
    let depth = 0;
    while (i < n) {
      const ch = src[i];
      const nx = src[i + 1];
      if (ch === "/" && nx === "/") {
        const e = src.indexOf("\n", i);
        const end = e < 0 ? n : e;
        blank(i, end);
        i = end;
      } else if (ch === "/" && nx === "*") {
        const e = src.indexOf("*/", i + 2);
        const end = e < 0 ? n : e + 2;
        blank(i, end);
        i = end;
      } else if (ch === '"' || ch === "'") {
        let j = i + 1;
        while (j < n && src[j] !== ch && src[j] !== "\n") j += src[j] === "\\" ? 2 : 1;
        blank(i + 1, j);
        strings.push({ start: i, end: j + 1, text: src.slice(i + 1, j) });
        i = j + 1;
      } else if (ch === "`") {
        i = template(i);
      } else if (ch === "/" && regexAllowed(i)) {
        let j = i + 1;
        let cls = false;
        while (j < n && src[j] !== "\n") {
          if (src[j] === "\\") { j += 2; continue; }
          if (src[j] === "[") cls = true;
          else if (src[j] === "]") cls = false;
          else if (src[j] === "/" && !cls) break;
          j++;
        }
        blank(i + 1, j);
        i = j + 1;
      } else {
        if (inTemplate && ch === "{") depth++;
        if (inTemplate && ch === "}") {
          if (depth === 0) return i + 1;
          depth--;
        }
        i++;
      }
    }
    return i;
  };
  code(0, false);
  return { code: out.join(""), strings };
}

const OPEN = "([{";
const CLOSE = ")]}";

/** Index of the bracket that closes the one at `open` (in masked code). */
function closingBracket(code: string, open: number): number {
  let depth = 0;
  for (let k = open; k < code.length; k++) {
    if (OPEN.includes(code[k])) depth++;
    else if (CLOSE.includes(code[k]) && --depth === 0) return k;
  }
  throw new Error(`unbalanced bracket at ${open}`);
}

/** Bracket depth of `pos` counted from the bracket at `open` (1 = a direct argument of that call). */
function depthFrom(code: string, open: number, pos: number): number {
  let depth = 0;
  for (let k = open; k < pos; k++) {
    if (OPEN.includes(code[k])) depth++;
    else if (CLOSE.includes(code[k])) depth--;
  }
  return depth;
}

interface Registration {
  key: string;
  /** Index of the registration call's `(` and of its matching `)`. */
  open: number;
  close: number;
}

/** Every `xRoutes.<method>("/path", ...)` call in a route module, with the key "METHOD /v1/path-pattern". */
function registrations(src: string, m: Masked): Registration[] {
  const out: Registration[] = [];
  for (const h of m.code.matchAll(/\b\w+Routes\s*\.\s*(get|post|put|patch|delete)\s*\(/g)) {
    const open = h.index! + h[0].length - 1;
    // The path may be a template literal (`/users/:user/${side}`); `${...}` stays in the key and matches one segment.
    const p = /^\s*(["`])(.+?)\1/.exec(src.slice(open + 1));
    if (!p) throw new Error(`registration without a literal path: ${src.slice(h.index!, h.index! + 80)}`);
    out.push({ key: `${h[1].toUpperCase()} ${normPath("/v1" + p[2])}`, open, close: closingBracket(m.code, open) });
  }
  return out;
}

/** Previous non-space character of the masked code before `pos`. */
function prevChar(code: string, pos: number): string {
  let k = pos - 1;
  while (k >= 0 && /\s/.test(code[k])) k--;
  return k < 0 ? "" : code[k];
}

/**
 * The forms the table check can see are limit("RL_X"[, key]) written directly as an argument of a route registration
 * in src/routes, and checkLimit(c.env, "RL_X", key) written inside a registration's handler in src/routes (plus
 * requireAuth's base RL_API in src/lib/auth.ts). This returns every other way src reaches a rate limiter:
 * - an import of lib/ratelimit that is not `import { a, b } from "<dir>/ratelimit"` (namespace, default, alias,
 *   `.js`/`.ts` extension, dynamic import, `export ... from`), or a name the module does not offer to callers;
 * - `limit` / `checkLimit` used other than as such a call (hoisted into a constant, wrapped, passed on, re-exported,
 *   called outside src/routes, outside a registration, or inside a handler body);
 * - a binding touched directly (`env.RL_X`, `env[...]`, `x.limit(...)` other than err.limit, `x["limit"]`);
 * - in lib/ratelimit.ts itself, any use of limit / checkLimit / `.limit(` beyond the definitions, so it cannot grow a
 *   wrapper (`export const socialLimit = () => limit("RL_SOCIAL")`) that applies a fixed limiter.
 */
function unscannableLimiterCalls(sources: Record<string, string>): string[] {
  const bad: string[] = [];
  for (const [file, src] of Object.entries(sources)) {
    const rel = relPath(file);
    const m = maskSource(src);
    const at = (pos: number) => src.slice(pos, src.indexOf("\n", pos) < 0 ? undefined : src.indexOf("\n", pos));
    const flag = (pos: number, why: string) => bad.push(`${rel}: ${why}: ${at(pos).trim()}`);
    const ids = (name: string) => [...m.code.matchAll(new RegExp(`(?<![.\\w$])${name}(?![\\w$])`, "g"))].map((x) => x.index!);

    if (rel === RATELIMIT_TS) {
      // Exactly: `function checkLimit(` + the one call in limit(), `function limit(`, and `env[name].limit(` in checkLimit.
      for (const pos of ids("checkLimit")) {
        if (/function\s+$/.test(m.code.slice(0, pos))) continue;
        if (/^checkLimit\(c\.env, name, k\)/.test(src.slice(pos)) && ids("checkLimit").length === 2) continue;
        flag(pos, "checkLimit used inside lib/ratelimit.ts beyond limit()");
      }
      for (const pos of ids("limit")) if (!/function\s+$/.test(m.code.slice(0, pos))) flag(pos, "limit used inside lib/ratelimit.ts");
      const member = [...m.code.matchAll(/\.\s*limit\s*\(/g)];
      for (const x of member) {
        if (member.length === 1 && /env\[name\]$/.test(m.code.slice(0, x.index!))) continue;
        flag(x.index!, "binding .limit() call in lib/ratelimit.ts beyond checkLimit()");
      }
      continue;
    }

    const inRoutes = /^src\/routes\/[^/]+\.ts$/.test(rel);
    const regs = inRoutes ? registrations(src, m) : [];
    const regAt = (pos: number) => regs.find((r) => r.open < pos && pos < r.close);

    // Imports of lib/ratelimit: only the plain named form, so `imported` below is the whole truth.
    const imported = new Set<string>();
    const importSpans: [number, number][] = [];
    const canonical = new Set<number>();
    for (const x of src.matchAll(/^import\s*\{([^}]*)\}\s*from\s*(["'])((?:\.\.?\/)+(?:lib\/)?ratelimit)\2;?/gm)) {
      if (m.code.slice(x.index!, x.index! + 6) !== "import") continue; // inside a comment or string
      importSpans.push([x.index!, x.index! + x[0].length]);
      canonical.add(x.index! + x[0].indexOf(x[2] + x[3]));
      for (const part of x[1].split(",").map((s) => s.trim()).filter(Boolean)) {
        const name = part.replace(/^type\s+/, "");
        if (!RATELIMIT_IMPORTS.has(name)) flag(x.index!, `import of ${JSON.stringify(name)} from lib/ratelimit`);
        else imported.add(name);
      }
    }
    for (const s of m.strings) {
      if (/(^|\/)ratelimit(\.[cm]?[jt]sx?)?$/.test(s.text) && !canonical.has(s.start)) flag(s.start, "lib/ratelimit referenced other than by `import { ... } from \"<dir>/ratelimit\"`");
      if (/^(limit|checkLimit)$/.test(s.text) && prevChar(m.code, s.start) === "[") flag(s.start, "limiter reached by a computed key");
    }
    const inImport = (pos: number) => importSpans.some(([a, b]) => a <= pos && pos < b);

    for (const pos of ids("checkLimit")) {
      if (inImport(pos)) continue;
      if (rel === "src/lib/auth.ts" && /^checkLimit\(\s*c\.env\s*,\s*"RL_API"/.test(src.slice(pos))) continue;
      if (!inRoutes) flag(pos, "checkLimit outside src/routes");
      else if (!regAt(pos)) flag(pos, "checkLimit outside a route registration");
      else if (!/^checkLimit\(\s*c\.env\s*,\s*"RL_[A-Z]+"/.test(src.slice(pos))) flag(pos, 'checkLimit not as checkLimit(c.env, "RL_X", ...)');
    }
    if (imported.has("limit")) {
      for (const pos of ids("limit")) {
        if (inImport(pos)) continue;
        // `{ cursor, limit: z.string() }` — an object key, not the import
        if (/^limit\s*\??:/.test(m.code.slice(pos)) && "{,".includes(prevChar(m.code, pos))) continue;
        const r = regAt(pos);
        if (!inRoutes) flag(pos, "limit outside src/routes");
        else if (!r) flag(pos, "limit outside a route registration (hoisted, wrapped or mounted with .use())");
        else if (depthFrom(m.code, r.open, pos) !== 1) flag(pos, "limit inside a handler, not a registration argument");
        else if (!/^limit\(\s*"RL_[A-Z]+"\s*[,)]/.test(src.slice(pos))) flag(pos, 'limit not as limit("RL_X"[, key])');
      }
    }
    for (const x of m.code.matchAll(/\bRL_[A-Z]+\b/g)) flag(x.index!, "rate-limit binding referenced directly");
    for (const x of m.code.matchAll(/\benv\s*(\?\.)?\s*\[/g)) flag(x.index!, "env indexed by a computed key");
    for (const x of m.code.matchAll(/(\?\.|\.)\s*limit\s*\(/g)) {
      if (!/\berr\s*$/.test(m.code.slice(0, x.index!))) flag(x.index!, "binding .limit() call");
    }
  }
  return bad;
}

interface ScannedRoute { limit: string[]; checkLimit: string[] }

/** "METHOD /path-pattern" -> the limiters written in each registration of src/routes (RL_API dropped). */
function scanRouteLimiters(sources: Record<string, string>): Map<string, ScannedRoute> {
  const out = new Map<string, ScannedRoute>();
  for (const [file, src] of Object.entries(sources)) {
    const m = maskSource(src);
    for (const r of registrations(src, m)) {
      const seg = src.slice(r.open, r.close);
      const names = (re: RegExp) => [...new Set([...seg.matchAll(re)].map((x) => x[1]))].filter((x) => x !== BASE_LIMITER).sort();
      if (out.has(r.key)) throw new Error(`${relPath(file)}: ${r.key} registered twice`);
      out.set(r.key, { limit: names(/\blimit\(\s*"(RL_[A-Z]+)"/g), checkLimit: names(/\bcheckLimit\(\s*c\.env\s*,\s*"(RL_[A-Z]+)"/g) });
    }
  }
  return out;
}

interface AppRoute { method: string; path: string; handler: unknown }

/** "METHOD /path" -> the limiters limit() tagged on that route's handlers (Hono lists each handler separately). */
function runtimeLimiters(routes: readonly AppRoute[]): Map<string, string[]> {
  const out = new Map<string, Set<string>>();
  for (const r of routes) {
    let h = r.handler as Record<PropertyKey, unknown> | undefined;
    while (h && h[COMPOSED_HANDLER]) h = h[COMPOSED_HANDLER] as Record<PropertyKey, unknown>; // app.route() wrapper
    const name = h?.[LIMITER_OF] as string | undefined;
    // .use() middleware shows up as method ALL; a limiter there would cover many rows at once, which the table cannot say.
    if (r.method === "ALL") {
      if (name) throw new Error(`limiter ${name} mounted with .use() on ${r.path}`);
      continue;
    }
    const key = `${r.method} ${normPath(r.path)}`;
    if (!out.has(key)) out.set(key, new Set());
    if (name && name !== BASE_LIMITER) out.get(key)!.add(name);
  }
  return new Map([...out].map(([k, v]) => [k, [...v].sort()]));
}

/**
 * The code's side of the table: each served route's runtime limit() tags plus the checkLimit() calls in its
 * registration. A route the scan cannot place, or a route whose tags differ from the limit() calls written in its
 * registration (a limiter built somewhere the lint missed), is an error, not "none".
 */
function codeTable(routes: readonly AppRoute[], sources: Record<string, string>): Map<string, string[]> {
  const scanned = scanRouteLimiters(sources);
  const patterns = [...scanned].map(([k, v]) => {
    const re = new RegExp("^" + k.split(/\$\{[^}]*\}/).map((s) => s.replace(/[.*+?^()|[\]\\]/g, "\\$&")).join("[^/ ]+") + "$");
    return { k, re, v };
  });
  const out = new Map<string, string[]>();
  for (const [key, tagged] of runtimeLimiters(routes)) {
    const exact = scanned.get(key);
    const hits = exact ? [{ k: key, v: exact }] : patterns.filter((p) => p.k.includes("${") && p.re.test(key));
    if (hits.length !== 1) throw new Error(`route ${key}: ${hits.length} matching registrations in src/routes`);
    const { limit: written, checkLimit: checked } = hits[0].v;
    if (written.join(",") !== tagged.join(",")) throw new Error(`route ${key}: limit() middleware ${tagged.join(",") || "none"}, but its registration writes ${written.join(",") || "none"}`);
    out.set(key, [...new Set([...tagged, ...checked])].sort());
  }
  return out;
}

describe("api.md 6 summary table: additional limiters match the code (pending item 8-a)", () => {
  const md = Object.values(docs)[0];
  const doc = parseDocTable(md);
  // Built here but asserted in a test, so a limiter the table cannot account for fails one named test, not collection.
  let code = new Map<string, string[]>();
  let codeError: unknown;
  try {
    code = codeTable(createApp().routes, routeSources);
  } catch (e) {
    codeError = e;
  }

  it("every served route's limiters are accounted for (runtime tags = limit() calls written in its registration)", () => {
    expect(codeError).toBeUndefined();
  });

  it("found the sources it compares", () => {
    expect(md).toBeTypeOf("string");
    expect(Object.keys(routeSources).length).toBeGreaterThan(20);
    expect(code.size).toBeGreaterThan(100);
    expect(doc.size).toBeGreaterThan(100);
    expect([...code.values()].filter((v) => v.length > 0).length).toBeGreaterThan(20);
  });

  it("POST /v1/channels/{channel_id}/members lists RL_SOCIAL", () => {
    expect(code.get("POST /v1/channels/*/members")).toEqual(["RL_SOCIAL"]);
    expect(doc.get("POST /v1/channels/*/members")).toEqual(["RL_SOCIAL"]);
    expect(doc.get("GET /v1/channels/*/members")).toEqual([]);
    expect(code.get("POST /v1/auth/login")).toEqual(["RL_LOGIN"]); // checkLimit inside the handler
  });

  it("every registered route has a row, with exactly the limiters the code checks", () => {
    const wrong: string[] = [];
    for (const [key, limiters] of code) {
      const listed = doc.get(key);
      if (listed === undefined) wrong.push(`${key}: no row in api.md 6 (code: ${limiters.join(",") || "none"})`);
      else if (listed.join(",") !== limiters.join(",")) wrong.push(`${key}: api.md 6 says ${listed.join(",") || "none"}, code checks ${limiters.join(",") || "none"}`);
    }
    expect(wrong).toEqual([]);
  });

  it("every row of the table is a route the code registers", () => {
    expect([...doc.keys()].filter((k) => !code.has(k))).toEqual([]);
  });

  it("every rate-limit call in src is in a form the checks above read", () => {
    expect(Object.keys(allSources).length).toBeGreaterThan(Object.keys(routeSources).length);
    expect(Object.keys(allSources).map(relPath)).toContain(RATELIMIT_TS);
    expect(unscannableLimiterCalls(allSources)).toEqual([]);
  });

  it("limit() tags its middleware, and lib/ratelimit offers no other limiter-applying export", () => {
    expect((limit("RL_SOCIAL") as unknown as Record<symbol, unknown>)[LIMITER_OF]).toBe("RL_SOCIAL");
    expect(Object.keys(ratelimit).sort()).toEqual(["LIMITER_OF", "LIMITER_PERIOD_SEC", "checkLimit", "clientIp", "limit"]);
  });

  describe("the lint flags limiters the checks would miss", () => {
    const real = (rel: string) => {
      const hit = Object.entries(allSources).find(([f]) => relPath(f) === rel);
      if (!hit) throw new Error(`no source ${rel}`);
      return hit[1];
    };
    const channels = real("src/routes/channels.ts");
    const lintOne = (rel: string, src: string) => unscannableLimiterCalls({ ["../../" + rel]: src });

    // Each entry: a real file mutated, or a small module, that type-checks and applies a limiter the checks cannot see.
    const holes: Record<string, [string, string]> = {
      "(a) limiter hoisted above the first registration": ["src/routes/channels.ts", channels
        .replace('limit("RL_SOCIAL"), vJson(AddMembersBody)', "socialLimit, vJson(AddMembersBody)")
        .replace("export const channelRoutes", 'const socialLimit = limit("RL_SOCIAL");\nexport const channelRoutes')],
      "(a) limiter defined between two registrations": ["src/routes/x.ts",
        'import { limit } from "../lib/ratelimit";\nxRoutes.get("/a", requireAuth, h);\nconst prefsLimit = limit("RL_SOCIAL");\nxRoutes.put("/b", requireAuth, prefsLimit, h);'],
      "(a) checkLimit in a helper between registrations": ["src/routes/x.ts",
        'import { checkLimit } from "../lib/ratelimit";\nxRoutes.get("/a", h);\nconst guard = (c: C) => checkLimit(c.env, "RL_SOCIAL", "k");\nxRoutes.put("/b", async (c) => { await guard(c); });'],
      "(a) limit() mounted with .use()": ["src/routes/x.ts", 'import { limit } from "../lib/ratelimit";\nxRoutes.use("/x/*", limit("RL_POST"));'],
      "(a) limit() invoked inside a handler": ["src/routes/x.ts",
        'import { limit } from "../lib/ratelimit";\nxRoutes.post("/x", async (c, next) => { await limit("RL_POST")(c, next); });'],
      "(b) wrapper in lib/ratelimit.ts": [RATELIMIT_TS, real(RATELIMIT_TS) + '\nexport const socialLimit = () => limit("RL_SOCIAL");\n'],
      "(b) checkLimit wrapper in lib/ratelimit.ts": [RATELIMIT_TS,
        real(RATELIMIT_TS) + '\nexport const social: MiddlewareHandler<AppEnv> = async (c, next) => { await checkLimit(c.env, "RL_SOCIAL", "k"); await next(); };\n'],
      "(b) wrapper re-exported from a route module": ["src/routes/x.ts", 'import { limit } from "../lib/ratelimit";\nexport const socialLimit = () => limit("RL_SOCIAL");'],
      "(b) limit passed on as a value": ["src/routes/x.ts", 'import { limit } from "../lib/ratelimit";\nexport { limit as rl };'],
      "(c) .js specifier in a service": ["src/services/x.ts", 'import { checkLimit } from "../lib/ratelimit.js";\nawait checkLimit(rt.env, "RL_SOCIAL", k);'],
      "(c) .ts specifier in a route": ["src/routes/x.ts", 'import { limit } from "../lib/ratelimit.ts";\nxRoutes.post("/x", limit("RL_POST"), h);'],
      "(c) re-export": ["src/routes/x.ts", 'export { limit as socialLimit } from "../lib/ratelimit";'],
      "(c) export *": ["src/lib/x.ts", 'export * from "./ratelimit";'],
      "(c) dynamic import": ["src/services/x.ts", 'const { checkLimit: c2 } = await import("../lib/ratelimit");'],
      "(c) namespace import": ["src/routes/x.ts", 'import * as R from "../lib/ratelimit";'],
      "(c) aliased import": ["src/routes/x.ts", 'import { limit as rl } from "../lib/ratelimit";'],
      "checkLimit in a service": ["src/services/x.ts", 'import { checkLimit, limit } from "../lib/ratelimit";\nawait checkLimit(rt.env, "RL_SOCIAL", k);'],
      "limit in a service": ["src/services/x.ts", 'import { limit } from "../lib/ratelimit";\nexport const l = limit("RL_POST");'],
      "single-quoted limiter name": ["src/routes/x.ts", "import { limit } from '../lib/ratelimit';\nxRoutes.post(\"/a\", limit('RL_POST'), h);"],
      "limiter name from a constant": ["src/routes/x.ts", 'import { limit } from "../lib/ratelimit";\nxRoutes.post("/b", limit(NAME), h);'],
      "checkLimit without c.env": ["src/routes/x.ts", 'import { checkLimit } from "../lib/ratelimit";\nxRoutes.post("/a", async (c) => { await checkLimit(env, "RL_LOGIN", k); });'],
      "binding called directly": ["src/routes/x.ts", "xRoutes.post(\"/a\", async (c) => { await c.env.RL_POST.limit({ key }); });"],
      "binding by computed key": ["src/routes/x.ts", 'xRoutes.post("/a", async (c) => { await c.env["RL_POST"].limit({ key }); });'],
      "binding held in a variable": ["src/services/x.ts", "export async function f(env: Env, n: LimiterName) { const b = env[n]; await b.limit({ key: 'k' }); }"],
      "requireAuth checking a non-base limiter": ["src/lib/auth.ts", 'import { checkLimit } from "./ratelimit";\nawait checkLimit(c.env, "RL_SOCIAL", k);'],
    };
    for (const [name, [rel, src]] of Object.entries(holes)) {
      it(name, () => expect(lintOne(rel, src)).not.toEqual([]));
    }

    it("passes the forms the checks read, and things that only look alike", () => {
      expect(lintOne("src/routes/ok.ts", [
        'import { checkLimit, clientIp, limit } from "../lib/ratelimit";',
        "// limit(\"RL_SOCIAL\") in a comment, and checkLimit(env) too",
        'const Q = z.object({ cursor: z.string().optional(), limit: z.string().optional() });',
        'okRoutes.post("/x", requireAuth, limit("RL_POST"), vJson(B), async (c) => {',
        '  const s = "checkLimit(rt.env, \\"RL_SOCIAL\\") ) } ]"; const re = /["\'`(]/; const t = `${s} limit(`;',
        "  const n = parseLimit(c.req.valid(\"query\").limit);",
        "  throw err.limit(\"owned_spaces\", 3);",
        "});",
        'okRoutes.post("/y", limit("RL_SOCIAL", (c) => `ip:${clientIp(c)}`), async (c) => {',
        '  await checkLimit(c.env, "RL_LOGIN", `ip:${clientIp(c)}`);',
        "});",
      ].join("\n"))).toEqual([]);
      expect(lintOne("src/lib/errors.ts", "export const err = { limit(what: string, limit: number) { return x; } };")).toEqual([]);
      expect(lintOne("src/lib/auth.ts", 'import { checkLimit } from "./ratelimit";\nawait checkLimit(c.env, "RL_API", k);')).toEqual([]);
      expect(lintOne(RATELIMIT_TS, real(RATELIMIT_TS))).toEqual([]);
    });
  });

  describe("the code table refuses limiters it cannot account for", () => {
    const src = { "../../src/routes/x.ts": 'xRoutes.put("/x/:id", requireAuth, h);\nxRoutes.post("/x", limit("RL_POST"), async (c) => { await checkLimit(c.env, "RL_LOGIN", k); });' };
    const tagged = (name: string) => Object.defineProperty(() => {}, LIMITER_OF, { value: name });

    it("joins runtime tags with the checkLimit calls of the registration", () => {
      const t = codeTable([{ method: "PUT", path: "/v1/x/:id", handler: () => {} }, { method: "POST", path: "/v1/x", handler: tagged("RL_POST") }], src);
      expect(Object.fromEntries(t)).toEqual({ "PUT /v1/x/*": [], "POST /v1/x": ["RL_LOGIN", "RL_POST"] });
    });

    it("a tagged middleware its registration does not write (built elsewhere) is an error", () => {
      expect(() => codeTable([{ method: "PUT", path: "/v1/x/:id", handler: tagged("RL_SOCIAL") }], src)).toThrow(/limit\(\) middleware RL_SOCIAL/);
    });

    it("a tag behind app.route()'s error-handler wrapper is still read", () => {
      const wrapped = Object.assign(() => {}, { [COMPOSED_HANDLER]: tagged("RL_SOCIAL") });
      expect(() => codeTable([{ method: "PUT", path: "/v1/x/:id", handler: wrapped }], src)).toThrow(/RL_SOCIAL/);
    });

    it("a tagged .use() middleware is an error", () => {
      expect(() => codeTable([{ method: "ALL", path: "/v1/x/*", handler: tagged("RL_POST") }], src)).toThrow(/mounted with \.use\(\)/);
    });
  });

  it("the masker blanks comments and literals but keeps template expressions", () => {
    const m = maskSource('a("x)") // c(\nb(`t${f("y")}(`, /[)"]/g) /* ( */');
    expect(m.code).toBe('a("  ")      \nb(` ${f(" ")} `, /    /g)        ');
    expect(m.strings.map((s) => s.text)).toEqual(["x)", "y", 't${f("y")}(']);
  });

  it("the parser reads per-method limiter cells and suffix paths", () => {
    const t = parseDocTable(
      "x\n## 6. 엔드포인트 요약표\n\n| 메서드 | 경로 | 인증 | 추가 리미터 |\n|---|---|---|---|\n" +
        "| PUT/DELETE/GET | /v1/m/{id}/a, /b | ✓ | RL_REACT(PUT/DELETE) |\n| GET | /v1/x, /v1/y | 없음 | — |\n\n## 7. next\n| GET | /v1/z | ✓ | RL_WS |\n",
    );
    expect(Object.fromEntries(t)).toEqual({
      "PUT /v1/m/*/a": ["RL_REACT"], "DELETE /v1/m/*/a": ["RL_REACT"], "GET /v1/m/*/a": [],
      "PUT /v1/m/*/b": ["RL_REACT"], "DELETE /v1/m/*/b": ["RL_REACT"], "GET /v1/m/*/b": [],
      "GET /v1/x": [], "GET /v1/y": [],
    });
  });
});
