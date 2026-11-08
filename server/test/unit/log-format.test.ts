// Pending item 7: every server log call is ONE structured object whose first key is a constant `msg`:
// `console.error({ msg: "TopicHub deliver failed", topic, target, err: errFields(e) })`. A value spliced into a message
// string (template literal, concatenation, extra positional arg) lets whatever it holds — a request path, a topic, an
// error text — forge or split log lines; the single-object form is the shape Workers Logs documents as indexed fields.
// This scanner is the only guard against the old shapes coming back, so it also rejects every way of reaching console
// other than a plain `console.<method>(` call (aliases, .call/.apply, `?.`, bracket access), and raw thrown values that
// would JSON-serialise as `{}` instead of going through errFields.
import { it, expect } from "vitest";
import { errFields } from "../../src/lib/log";

const sources = import.meta.glob("../../src/**/*.ts", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

const METHODS = new Set(["log", "info", "warn", "error", "debug"]);

/** Index just past the string/template literal that starts at `i` (`src[i]` is the quote). */
function skipString(src: string, i: number): number {
  const q = src[i];
  for (let j = i + 1; j < src.length; j++) {
    if (src[j] === "\\") { j++; continue; }
    if (q === "`" && src[j] === "$" && src[j + 1] === "{") { j = skipBalanced(src, j + 1) - 1; continue; }
    if (src[j] === q) return j + 1;
  }
  throw new Error("unterminated string");
}

/** Index just past the bracket group that opens at `i`. */
function skipBalanced(src: string, i: number): number {
  const close: Record<string, string> = { "(": ")", "[": "]", "{": "}" };
  const stack = [close[src[i]]];
  for (let j = i + 1; j < src.length; j++) {
    const ch = src[j];
    if (ch === '"' || ch === "'" || ch === "`") { j = skipString(src, j) - 1; continue; }
    if (ch === "/" && src[j + 1] === "/") { j = src.indexOf("\n", j) - 1; continue; }
    if (ch in close) stack.push(close[ch]);
    else if (ch === stack[stack.length - 1]) { stack.pop(); if (stack.length === 0) return j + 1; }
  }
  throw new Error("unbalanced brackets");
}

/** Top-level comma split of the bracket group that opens at `open` (`(` or `{`). */
function splitTop(src: string, open: number): string[] {
  const end = skipBalanced(src, open) - 1;
  const parts: string[] = [];
  let start = open + 1;
  for (let j = open + 1; j < end; j++) {
    const ch = src[j];
    if (ch === '"' || ch === "'" || ch === "`") { j = skipString(src, j) - 1; continue; }
    if (ch === "(" || ch === "[" || ch === "{") { j = skipBalanced(src, j) - 1; continue; }
    if (ch === ",") { parts.push(src.slice(start, j).trim()); start = j + 1; }
  }
  const last = src.slice(start, end).trim();
  if (last !== "") parts.push(last);
  return parts;
}

/** `src` with comments, string/template text and regex literals blanked to spaces (offsets and newlines kept;
 *  `${...}` code inside templates stays), so a `console` token found in it is real code. */
function codeOnly(src: string): string {
  const out = src.split("");
  const blank = (from: number, to: number) => { for (let k = from; k < to; k++) if (out[k] !== "\n") out[k] = " "; };
  let lastSig = ""; // last significant code character, to tell a regex literal from division
  const code = (i: number, closeBrace: boolean): number => {
    let depth = 0;
    for (; i < src.length; i++) {
      const ch = src[i];
      if (ch === "/" && src[i + 1] === "/") { const e = src.indexOf("\n", i); const end = e < 0 ? src.length : e; blank(i, end); i = end - 1; continue; }
      if (ch === "/" && src[i + 1] === "*") { const end = src.indexOf("*/", i + 2) + 2; if (end < 2) throw new Error("unterminated comment"); blank(i, end); i = end - 1; continue; }
      if (ch === '"' || ch === "'") { const end = skipString(src, i); blank(i, end); i = end - 1; lastSig = "a"; continue; }
      if (ch === "`") { i = template(i) - 1; lastSig = "a"; continue; }
      if (ch === "/" && (lastSig === "" || "(,=:[!&|?{};+-*%<>~^".includes(lastSig) || /\b(return|typeof|case)\s*$/.test(src.slice(Math.max(0, i - 8), i)))) {
        let j = i + 1, cls = false;
        for (; j < src.length; j++) {
          if (src[j] === "\\") { j++; continue; }
          if (src[j] === "[") cls = true; else if (src[j] === "]") cls = false;
          else if (src[j] === "/" && !cls) break;
          else if (src[j] === "\n") throw new Error("unterminated regex");
        }
        blank(i, j + 1); i = j; lastSig = "a"; continue;
      }
      if (closeBrace) { if (ch === "{") depth++; else if (ch === "}") { if (depth === 0) return i + 1; depth--; } }
      if (!/\s/.test(ch)) lastSig = ch;
    }
    if (closeBrace) throw new Error("unterminated template expression");
    return i;
  };
  const template = (i: number): number => {
    for (let j = i + 1; j < src.length; j++) {
      if (src[j] === "\\") { blank(j, j + 2); j++; continue; }
      if (src[j] === "$" && src[j + 1] === "{") { lastSig = "{"; j = code(j + 2, true) - 1; continue; }
      if (src[j] === "`") return j + 1;
      blank(j, j + 1);
    }
    throw new Error("unterminated template");
  };
  code(0, false);
  return out.join("");
}

/** Why one console call's argument list breaks the rule, or null when it is `({ msg: "<constant>", ...fields })`. */
function callViolation(args: string[]): string | null {
  if (args.length !== 1) return `expected exactly one fields object, got ${args.length} arguments`;
  const obj = args[0];
  if (!obj.startsWith("{") || skipBalanced(obj, 0) !== obj.length) return `argument is not an object literal: ${obj}`;
  const props = splitTop(obj, 0);
  if (props.length === 0) return "empty fields object";
  const keys: string[] = [];
  for (const [i, p] of props.entries()) {
    if (p.startsWith("...")) return `spread in fields object (could override msg): ${p}`;
    const m = /^([A-Za-z_$][\w$]*|"[A-Za-z_$][\w$]*")\s*(?::\s*([\s\S]+))?$/.exec(p);
    if (!m) return `property is not a plain key: ${p}`;
    const key = m[1].replace(/"/g, "");
    const value = (m[2] ?? key).trim();
    if (i === 0) {
      if (key !== "msg") return `first key is not msg: ${p}`;
      if (!/^"[^"\\]*"$/.test(value)) return `msg is not a plain constant string: ${value}`;
    } else if (key === "msg") return "msg given twice";
    if (key === "err" && !(value.startsWith("errFields(") && skipBalanced(value, "errFields".length) === value.length))
      return `err is not errFields(...): ${value}`;
    if (/^(e|err|error|reason|[\w$.]+\.reason)$/.test(value)) return `raw thrown value (an Error JSON-serialises as {}): ${p}`;
    keys.push(key);
  }
  return null;
}

type Offence = { file: string; line: number; why: string };

/** Every rule break in `files`: any `console` token that is not a plain `console.<method>(` call, and every call
 *  whose arguments break callViolation. */
function scan(files: Record<string, string>): { calls: number; offences: Offence[] } {
  let calls = 0;
  const offences: Offence[] = [];
  for (const [file, src] of Object.entries(files)) {
    const code = codeOnly(src);
    for (const m of code.matchAll(/\bconsole\b/g)) {
      const at = m.index;
      const line = src.slice(0, at).split("\n").length;
      const call = /^console\.(\w+)\s*\(/.exec(code.slice(at));
      if (!call) { offences.push({ file, line, why: `console used other than as console.<method>(: ${src.slice(at, at + 30).split("\n")[0]}` }); continue; }
      if (!METHODS.has(call[1])) { offences.push({ file, line, why: `console.${call[1]} is not an allowed log method` }); continue; }
      calls++;
      const why = callViolation(splitTop(src, at + call[0].length - 1));
      if (why) offences.push({ file, line, why });
    }
  }
  return { calls, offences };
}

const why = (src: string) => scan({ "x.ts": src }).offences.map((o) => o.why);

it("the scanner sees the server's log calls (sanity: an empty scan would pass vacuously)", () => {
  expect(Object.keys(sources).length).toBeGreaterThan(20);
  const { calls } = scan(sources);
  expect(calls).toBeGreaterThan(20);
  const files = (f: string) => scan({ [f]: sources[`../../${f}`] }).calls;
  expect(files("src/do/topic-hub.ts")).toBeGreaterThan(0);
  expect(files("src/services/posts.ts")).toBeGreaterThan(0);
  expect(files("src/do/user-hub.ts")).toBeGreaterThan(5);
});

it("the scanner rejects the shapes the finding names and every way around a plain console.<method>( call", () => {
  // the original finding's shapes and the earlier two-argument form
  for (const src of [
    "console.error(`TopicHub ${topic}: deliver to ${chunk[j]} failed`, r.reason);",
    "console.error(`posts: ${label} failed`, e);",
    'console.error("unhandled error", c.req.method, c.req.path, e);',
    'console.error("cleanup task failed", name);',
    'console.error("posts background task failed", { label, err: errFields(e) });',
    'console.error({ msg: `x ${t}` });',
    'console.error({ msg: "a" + t });',
    'console.error({ msg: "line\\nFAKE" });',
    'console.error({ topic, msg: "x" });',
    'console.error({ msg: "a", msg: "b" });',
    'console.error({ msg: "a", ...extra });',
    'console.error({ msg: "a", [k]: v });',
    'console.error({ msg: "a", err: e });',
    'console.error({ msg: "a", err: String(e) });',
    'console.error({ msg: "a", e });',
    'console.error({ msg: "a", reason: r.reason });',
    'console.error({ msg: "a", err: errFields(e).message + x });',
    "console.error(fields);",
    "console.error();",
  ]) expect(why(src), src).toHaveLength(1);
  // every way of reaching console that the old /console\.(\w+)\(/ regex missed
  for (const src of [
    'console.error.call(console, "x", e);',
    'console.error.apply(console, ["x", e]);',
    'console?.error({ msg: "x" });',
    "console['error']({ msg: \"x\" });",
    'console\n  .error({ msg: "x" });',
    "const log = console.error;",
    "const { error } = console;",
    "const c = console;",
    "console.table(rows);",
  ]) expect(why(src).length, src).toBeGreaterThan(0);
  // comments, strings, templates and regexes that mention console are not code
  expect(why('// console.error(`x ${y}`)\n/* console.log(a, b) */\nconst s = "console.log(x)" + \'console\';\nconst t = `console ${n} console`;\nconst r = /console\\.log\\(/;')).toEqual([]);
  // ... but code inside a template's ${} is
  expect(why("const t = `a ${console.error(x)} b`;")).toHaveLength(1);
  // the allowed shape
  expect(why('console.error({ msg: "posts background task failed", label, err: errFields(e) }); // trailing\n')).toEqual([]);
  expect(why('console.warn({ msg: "unknown cron", cron: controller.cron });')).toEqual([]);
  expect(why('console.error({\n  msg: "realtime: deliver failed",\n  target: chunk[j],\n  err: errFields(r.reason),\n});')).toEqual([]);
});

it("every console.* in server/src is one object with a constant msg first, errors only via errFields", () => {
  expect(scan(sources).offences.map((o) => `${o.file.replace("../../", "")}:${o.line}: ${o.why}`)).toEqual([]);
});

it("errFields turns an Error into plain JSON fields (a nested Error would serialise as {})", () => {
  const e = new TypeError("boom\nFAKE LOG LINE");
  const f = errFields(e) as { name: string; message: string; stack?: string };
  expect(f.name).toBe("TypeError");
  expect(f.message).toBe("boom\nFAKE LOG LINE");
  expect(typeof f.stack).toBe("string");
  expect(JSON.parse(JSON.stringify(f))).toEqual(f);
  expect(errFields("plain")).toEqual({ message: "plain" });
  expect(errFields({ code: 7 })).toEqual({ message: "[object Object]" });
  expect(errFields(undefined)).toEqual({ message: "undefined" });
  expect(errFields(Symbol("s"))).toEqual({ message: "Symbol(s)" });
});

it("errFields never throws, whatever was thrown (it runs in catch blocks inside blockConcurrencyWhile and .catch handlers)", () => {
  const throwingGetter = new Error("x");
  Object.defineProperty(throwingGetter, "stack", { get() { throw new Error("no stack"); } });
  const weirdName = new Error("y");
  Object.defineProperty(weirdName, "name", { value: Object.create(null) });
  const hostile = new Proxy({}, { getPrototypeOf() { throw new Error("trap"); } });
  for (const v of [
    Object.create(null),
    { toString() { throw 1; } },
    { [Symbol.toPrimitive]() { return {}; } },
    throwingGetter,
    weirdName,
    hostile,
  ]) {
    let f: ReturnType<typeof errFields> | undefined;
    expect(() => { f = errFields(v); }).not.toThrow();
    expect(typeof f!.message).toBe("string");
    expect(() => JSON.stringify(f)).not.toThrow();
    for (const val of Object.values(f!)) expect(typeof val).toBe("string");
  }
  expect(errFields(Object.create(null))).toEqual({ message: "[unprintable thrown value]" });
  expect(errFields(weirdName).message).toBe("y");
});
