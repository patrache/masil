import { it, expect } from "vitest";
import { env } from "cloudflare:test";
import { encodeCursor, decodeCursor, parseLimit, jsonList, all, first, run } from "../../src/lib/db";

it("cursor roundtrip and opaque base64url", () => {
  const s = encodeCursor({ k: 1730000000000, i: "01JC8Z3K4M5N6P7Q8R9S0TVWXY" });
  expect(s).toMatch(/^[A-Za-z0-9_-]+$/);
  expect(decodeCursor(s)).toEqual({ k: 1730000000000, i: "01JC8Z3K4M5N6P7Q8R9S0TVWXY" });
  expect(decodeCursor(undefined)).toBeNull();
  expect(() => decodeCursor("%%%")).toThrow();
  expect(() => decodeCursor(btoa('{"k":1}'))).toThrow();
});

it("parseLimit", () => {
  expect(parseLimit(undefined)).toBe(30);
  expect(parseLimit("500")).toBe(100);
  expect(parseLimit("7", 50, 100)).toBe(7);
  expect(() => parseLimit("0")).toThrow();
  expect(() => parseLimit("abc")).toThrow();
});

it("jsonList passes >100 ids as one parameter; undefined binds as null", async () => {
  const ids = Array.from({ length: 150 }, (_, i) => `x${i}`);
  const rows = await all<{ value: string }>(env.DB, "SELECT value FROM json_each(?1)", jsonList(ids));
  expect(rows).toHaveLength(150);
  expect(await first<{ v: null }>(env.DB, "SELECT ?1 AS v", undefined)).toEqual({ v: null });
  const res = await run(env.DB, "SELECT 1");
  expect(res.success).toBe(true);
});
