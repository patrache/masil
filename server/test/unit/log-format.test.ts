// Pending item 7: every server log call is a constant message plus at most one structured-fields object. A value
// spliced into the message (template literal, concatenation, extra positional arg) lets whatever it holds — a request
// path, a topic, an error text — forge or split log lines, and a `%` in the first argument is a console format
// directive that eats the next argument.
import { it, expect, vi, afterEach } from "vitest";
import { errFields } from "../../src/lib/log";

const sources = import.meta.glob("../../src/**/*.ts", { query: "?raw", import: "default", eager: true });

type Call = { file: string; line: number; method: string; args: string[] };

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

/** Top-level comma split of the argument list between `open` (the `(`) and its match. */
function splitArgs(src: string, open: number): string[] {
  const end = skipBalanced(src, open) - 1;
  const args: string[] = [];
  let start = open + 1;
  for (let j = open + 1; j < end; j++) {
    const ch = src[j];
    if (ch === '"' || ch === "'" || ch === "`") { j = skipString(src, j) - 1; continue; }
    if (ch === "(" || ch === "[" || ch === "{") { j = skipBalanced(src, j) - 1; continue; }
    if (ch === ",") { args.push(src.slice(start, j).trim()); start = j + 1; }
  }
  const last = src.slice(start, end).trim();
  if (last !== "") args.push(last);
  return args;
}

function consoleCalls(): Call[] {
  const calls: Call[] = [];
  for (const [file, src] of Object.entries(sources)) {
    for (const m of src.matchAll(/\bconsole\.(\w+)\s*\(/g)) {
      const open = m.index + m[0].length - 1;
      calls.push({ file: file.replace("../../", ""), line: src.slice(0, m.index).split("\n").length, method: m[1], args: splitArgs(src, open) });
    }
  }
  return calls;
}

/** Why a call breaks the rule, or null when it is a constant message with an optional `{ ... }` fields object. */
function violation(c: Call): string | null {
  const [msg, ...rest] = c.args;
  if (msg === undefined) return "no message";
  if (!/^"[^"\\%]*"$/.test(msg)) return `message is not a plain constant string: ${msg}`;
  if (rest.length > 1) return `positional values after the message: ${rest.join(", ")}`;
  if (rest.length === 1 && !rest[0].startsWith("{")) return `second argument is not a fields object: ${rest[0]}`;
  return null;
}

afterEach(() => vi.restoreAllMocks());

it("the scanner sees the server's log calls (sanity: an empty scan would pass vacuously)", () => {
  expect(Object.keys(sources).length).toBeGreaterThan(20);
  const calls = consoleCalls();
  expect(calls.length).toBeGreaterThan(20);
  expect(calls.some((c) => c.file === "src/do/topic-hub.ts")).toBe(true);
  expect(calls.some((c) => c.file === "src/services/posts.ts")).toBe(true);
  // the rule itself rejects the shapes the finding names
  const bad = (args: string[]) => violation({ file: "x", line: 1, method: "error", args });
  expect(bad(["`TopicHub ${topic}: deliver to ${chunk[j]} failed`", "r.reason"])).not.toBeNull();
  expect(bad(["`posts: ${label} failed`", "e"])).not.toBeNull();
  expect(bad(['"unhandled error"', "c.req.method", "c.req.path", "e"])).not.toBeNull();
  expect(bad(['"cleanup task failed"', "name"])).not.toBeNull();
  expect(bad(['"100% done"'])).not.toBeNull();
  expect(bad(['"posts background task failed"', "{ label, err: errFields(e) }"])).toBeNull();
});

it("every console.* call in server/src is a constant message plus at most one structured-fields object", () => {
  const offenders = consoleCalls()
    .map((c) => ({ c, why: violation(c) }))
    .filter((x) => x.why !== null)
    .map(({ c, why }) => `${c.file}:${c.line} console.${c.method}: ${why}`);
  expect(offenders).toEqual([]);
});

it("errFields turns an Error into plain JSON fields (Workers Logs indexes object keys; a nested Error would serialise as {})", () => {
  const e = new TypeError("boom\nFAKE LOG LINE");
  const f = errFields(e) as { name: string; message: string; stack?: string };
  expect(f.name).toBe("TypeError");
  expect(f.message).toBe("boom\nFAKE LOG LINE");
  expect(typeof f.stack).toBe("string");
  expect(JSON.parse(JSON.stringify(f))).toEqual(f);
  expect(errFields("plain")).toEqual({ message: "plain" });
  expect(errFields({ code: 7 })).toEqual({ message: "[object Object]" });
  expect(errFields(undefined)).toEqual({ message: "undefined" });
});
