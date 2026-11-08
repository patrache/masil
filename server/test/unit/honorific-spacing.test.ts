// v0.1.1 D8 (owner decision 2026-11-08): the honorific '님' is written attached to the name — "하나님이" (O),
// "하나 님이" (X). Every user-visible server string (alert titles and bodies, notification text) follows it; this scan
// keeps a spaced '님' from coming back anywhere in server/src.
import { it, expect } from "vitest";

const sources = import.meta.glob("../../src/**/*.ts", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

it("no server source puts whitespace before 님", () => {
  expect(Object.keys(sources).length).toBeGreaterThan(10);
  const hits: string[] = [];
  for (const [file, src] of Object.entries(sources)) {
    src.split("\n").forEach((line, i) => {
      if (/\s님/u.test(line)) hits.push(`${file}:${i + 1}: ${line.trim()}`);
    });
  }
  expect(hits).toEqual([]);
});
