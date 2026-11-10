import { it, expect } from "vitest";
import { compareSemver, parseClientVersion, parseSemver } from "../../src/lib/version";

// 0.1.3 patch release v0.1.3-1: the gate reads the x.y.z prefix of the
// User-Agent, so a "-N" patch revision counts as its x.y.z.
it("masil/0.1.3-1 parses as 0.1.3 and passes a 0.1.3 gate", () => {
  const v = parseClientVersion("masil/0.1.3-1 (darwin; arm64)");
  expect(v).toEqual([0, 1, 3]);
  expect(compareSemver(v!, parseSemver("0.1.3")!)).toBe(0);
  expect(compareSemver(v!, parseSemver("0.1.2")!)).toBe(1);
  expect(compareSemver(v!, parseSemver("0.1.4")!)).toBe(-1);
  expect(parseClientVersion("masil/0.1.3 (linux; amd64)")).toEqual([0, 1, 3]);
  expect(parseClientVersion("curl/8.0")).toBeNull();
});
