// Pending item 8-a: the "추가 리미터" column of api.md 6 (endpoint summary table) drifted from the code once —
// POST /v1/channels/{channel_id}/members gained limit("RL_SOCIAL") while its row stayed blank. Clients and reviewers
// read the table as the contract, so this test re-derives the column from the route sources and compares both ways:
// every route registered in src/routes has a row whose limiters for that method are exactly the ones the code checks,
// and every (method, path) in the table is a route the code registers. RL_API (applied by requireAuth to every
// authenticated request) is the base limiter, not an "additional" one, so neither side lists it.
import { describe, it, expect } from "vitest";
import { createApp } from "../../src/app";

const routeSources = import.meta.glob("../../src/routes/*.ts", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const allSources = import.meta.glob("../../src/**/*.ts", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const docs = import.meta.glob("../../../docs/contract/api.md", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"] as const;
const BASE_LIMITER = "RL_API";

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

/**
 * scanRouteLimiters only sees `limit("RL_X"` and `checkLimit(c.env, "RL_X"` written in src/routes. A limiter applied any
 * other way (in src/services or another module, through a constant/variable/single-quoted name, an import alias, or a
 * direct `env.RL_X.limit(...)` binding call) would be invisible to it, and the table could silently lack a limiter the
 * code checks. So every rate-limit call in src must be in a form the scanner reads; this returns the ones that are not.
 * src/lib/ratelimit.ts defines the helpers; requireAuth (src/lib/auth.ts) applies the base RL_API, which the table omits.
 */
function unscannableLimiterCalls(sources: Record<string, string>): string[] {
  const bad: string[] = [];
  for (const [file, src] of Object.entries(sources)) {
    const rel = file.replace(/^(\.\.\/)+/, "");
    if (rel === "src/lib/ratelimit.ts") continue;
    const inRoutes = /^src\/routes\/[^/]+\.ts$/.test(rel);
    // Only a module that imports limit/checkLimit can call them (aliases and namespace imports are rejected).
    const imported = new Set<string>();
    for (const m of src.matchAll(/import\s*\{([^}]*)\}\s*from\s*["'][^"']*\/ratelimit["']/g)) {
      if (/\b(limit|checkLimit)\s+as\b/.test(m[1])) bad.push(`${rel}: aliased limiter import: ${m[0]}`);
      for (const n of m[1].split(",").map((x) => x.trim())) imported.add(n);
    }
    for (const m of src.matchAll(/import\s*\*\s*as\s+\w+\s+from\s*["'][^"']*\/ratelimit["']/g)) bad.push(`${rel}: namespace limiter import: ${m[0]}`);
    for (const m of imported.has("limit") ? src.matchAll(/(?<![.\w])limit\([^\n]*/g) : []) {
      if (!inRoutes) bad.push(`${rel}: limit() outside src/routes: ${m[0]}`);
      else if (!/^limit\(\s*"RL_[A-Z]+"/.test(m[0])) bad.push(`${rel}: limit() without a "RL_X" literal: ${m[0]}`);
    }
    for (const m of imported.has("checkLimit") ? src.matchAll(/(?<![.\w])checkLimit\([^\n]*/g) : []) {
      if (rel === "src/lib/auth.ts" && /^checkLimit\(\s*c\.env\s*,\s*"RL_API"/.test(m[0])) continue;
      if (!inRoutes) bad.push(`${rel}: checkLimit() outside src/routes: ${m[0]}`);
      else if (!/^checkLimit\(\s*c\.env\s*,\s*"RL_[A-Z]+"/.test(m[0])) bad.push(`${rel}: checkLimit() not as checkLimit(c.env, "RL_X": ${m[0]}`);
    }
    for (const m of src.matchAll(/\bRL_[A-Z]+["'`]?\s*\]?\s*\.\s*limit\(/g)) bad.push(`${rel}: direct binding call: ${m[0]}`);
  }
  return bad;
}

/** "METHOD /path-pattern" -> sorted additional limiters, from every `xRoutes.<method>("/path", ...)` in src/routes. */
function scanRouteLimiters(sources: Record<string, string>): Map<string, string[]> {
  const out = new Map<string, string[]>();
  // The path may be a template literal (`/users/:user/${side}`); `${...}` stays in the key and matches one segment.
  const reg = /\b\w+Routes\.(get|post|put|patch|delete)\(\s*(["`])(.+?)\2/g;
  for (const [file, src] of Object.entries(sources)) {
    // A limiter mounted with .use() would apply to many routes at once; the table cannot express that, so forbid it.
    for (const m of src.matchAll(/\b\w+Routes\.use\([^\n]*/g)) {
      if (/limit\(|checkLimit\(/.test(m[0])) throw new Error(`${file}: limiter mounted via .use(): ${m[0]}`);
    }
    const hits = [...src.matchAll(reg)];
    hits.forEach((h, i) => {
      // The registration's arguments and handler body run until the next registration (or a .use()/end of file).
      const rest = src.slice(h.index!, i + 1 < hits.length ? hits[i + 1].index : undefined);
      const useAt = rest.search(/\b\w+Routes\.use\(/);
      const seg = useAt > 0 ? rest.slice(0, useAt) : rest;
      const names = new Set<string>();
      for (const m of seg.matchAll(/\blimit\(\s*"(RL_[A-Z]+)"/g)) names.add(m[1]);
      for (const m of seg.matchAll(/\bcheckLimit\(\s*c\.env\s*,\s*"(RL_[A-Z]+)"/g)) names.add(m[1]);
      names.delete(BASE_LIMITER);
      const key = `${h[1].toUpperCase()} ${normPath("/v1" + h[3])}`;
      if (out.has(key)) throw new Error(`${file}: ${key} registered twice`);
      out.set(key, [...names].sort());
    });
  }
  return out;
}

/** Every "METHOD /path" the assembled app serves (Hono's app.routes; .use() middleware shows up as method ALL). */
function appRoutes(): Set<string> {
  const keys = new Set<string>();
  for (const r of createApp().routes) if (r.method !== "ALL") keys.add(`${r.method} ${normPath(r.path)}`);
  return keys;
}

/** Joins the runtime route set with the scanned limiters; a route the scan cannot place is an error, not "none". */
function codeTable(): Map<string, string[]> {
  const scanned = scanRouteLimiters(routeSources);
  const patterns = [...scanned].map(([k, v]) => {
    const re = new RegExp("^" + k.split(/\$\{[^}]*\}/).map((s) => s.replace(/[.*+?^()|[\]\\]/g, "\\$&")).join("[^/ ]+") + "$");
    return { k, re, v };
  });
  const out = new Map<string, string[]>();
  for (const key of appRoutes()) {
    const exact = scanned.get(key);
    const hits = exact ? [{ k: key, v: exact }] : patterns.filter((p) => p.k.includes("${") && p.re.test(key));
    if (hits.length !== 1) throw new Error(`route ${key}: ${hits.length} matching registrations in src/routes`);
    out.set(key, hits[0].v);
  }
  return out;
}

describe("api.md 6 summary table: additional limiters match the code (pending item 8-a)", () => {
  const md = Object.values(docs)[0];
  const doc = parseDocTable(md);
  const code = codeTable();

  it("found the sources it compares", () => {
    expect(md).toBeTypeOf("string");
    expect(Object.keys(routeSources).length).toBeGreaterThan(20);
    expect(code.size).toBeGreaterThan(100);
    expect(doc.size).toBeGreaterThan(100);
  });

  it("POST /v1/channels/{channel_id}/members lists RL_SOCIAL", () => {
    expect(code.get("POST /v1/channels/*/members")).toEqual(["RL_SOCIAL"]);
    expect(doc.get("POST /v1/channels/*/members")).toEqual(["RL_SOCIAL"]);
    expect(doc.get("GET /v1/channels/*/members")).toEqual([]);
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

  it("every rate-limit call in src is in a form the route scanner reads", () => {
    expect(Object.keys(allSources).length).toBeGreaterThan(Object.keys(routeSources).length);
    expect(unscannableLimiterCalls(allSources)).toEqual([]);
  });

  it("the guard flags limiter calls the scanner would miss", () => {
    const flagged = unscannableLimiterCalls({
      "../../src/services/x.ts": 'import { checkLimit, limit } from "../lib/ratelimit";\nawait checkLimit(rt.env, "RL_SOCIAL", k);\nreturn limit("RL_POST");',
      "../../src/routes/a.ts": "import { limit } from '../lib/ratelimit';\naRoutes.post(\"/a\", limit('RL_POST'), h);\naRoutes.post(\"/b\", limit(NAME), h);",
      "../../src/routes/b.ts": 'import { checkLimit } from "../lib/ratelimit";\nawait checkLimit(env, "RL_LOGIN", k);\nawait c.env.RL_POST.limit({ key });\nawait c.env["RL_POST"].limit({ key });',
      "../../src/routes/c.ts": 'import { limit as rl } from "../lib/ratelimit";\nimport * as R from "../lib/ratelimit";',
      "../../src/lib/auth.ts": 'import { checkLimit } from "./ratelimit";\nawait checkLimit(c.env, "RL_API", k);\nawait checkLimit(c.env, "RL_SOCIAL", k);',
      // the forms the scanner reads, and things that only look alike
      "../../src/routes/ok.ts": 'import { checkLimit, clientIp, limit } from "../lib/ratelimit";\nokRoutes.post("/x", limit("RL_POST"), h);\nawait checkLimit(c.env, "RL_LOGIN", k);\nthrow err.limit("owned_spaces", 3);',
      "../../src/lib/errors.ts": "export const err = { limit(what: string, limit: number) { return x; } };",
      "../../src/lib/ratelimit.ts": "export function limit(name) { return checkLimit(env, name, k); }",
    });
    expect(flagged.map((f) => f.slice(0, f.indexOf(":")))).toEqual([
      "src/services/x.ts", "src/services/x.ts",
      "src/routes/a.ts", "src/routes/a.ts",
      "src/routes/b.ts", "src/routes/b.ts", "src/routes/b.ts",
      "src/routes/c.ts", "src/routes/c.ts",
      "src/lib/auth.ts",
    ]);
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
