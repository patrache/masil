// v0.1.3 A10.3 (P1 Task 1): pure LeetCode helpers — UTC day, reportable window, slug / id rules, the fixed link,
// strict fact / card schemas (no body field can ride along) and the streak rule (no grace day, JA26).
import { describe, expect, test } from "vitest";
import { CARD_TYPES } from "../../src/lib/cards/index";
import {
  DATE_RE,
  FRONTEND_ID_RE,
  LeetDailyCardData,
  LeetDailyFacts,
  LeetSolveCardData,
  SLUG_RE,
  computeStreak,
  isReportableDate,
  leetLink,
  utcDate,
} from "../../src/lib/leet";

const facts = {
  frontend_id: "1606",
  slug: "find-servers-that-handled-most-number-of-requests",
  title: "Find Servers That Handled Most Number of Requests",
  difficulty: "Hard",
  tags: ["Array", "Heap (Priority Queue)", "Ordered Set"],
  is_paid_only: false,
};

const solve = {
  date: "2026-11-09",
  frontend_id: "1606",
  slug: "find-servers-that-handled-most-number-of-requests",
  lang: "golang",
  runtime_percentile: 12,
  code: { lang: "golang", text: "func busiestServers() {}" },
  spoiler: true,
};

describe("leet lib", () => {
  test("utcDate and reportable window", () => {
    const t = Date.UTC(2026, 10, 9, 23, 59, 0);
    expect(utcDate(t)).toBe("2026-11-09");
    expect(utcDate(Date.UTC(2026, 10, 10, 0, 0, 0))).toBe("2026-11-10");
    expect(isReportableDate("2026-11-09", t)).toBe(true);
    expect(isReportableDate("2026-11-08", t)).toBe(true);
    expect(isReportableDate("2026-11-07", t)).toBe(false);
    expect(isReportableDate("2026-11-10", t)).toBe(false);
    expect(isReportableDate("2026-1-09", t)).toBe(false);
    expect(isReportableDate("2026-02-30", Date.UTC(2026, 2, 1))).toBe(false); // not a calendar day
  });

  test("computeStreak: gap breaks, today empty keeps yesterday's run", () => {
    expect(computeStreak(["2026-11-07", "2026-11-08"], "2026-11-09")).toEqual({ current: 2, best: 2 });
    expect(computeStreak(["2026-11-05", "2026-11-07", "2026-11-08", "2026-11-09"], "2026-11-09")).toEqual({ current: 3, best: 3 });
    expect(computeStreak(["2026-11-01", "2026-11-02", "2026-11-03"], "2026-11-09")).toEqual({ current: 0, best: 3 });
    expect(computeStreak([], "2026-11-09")).toEqual({ current: 0, best: 0 });
    // order and duplicates do not matter; month / year boundaries are consecutive days
    expect(computeStreak(["2027-01-01", "2026-12-31", "2026-12-31", "2026-12-30"], "2027-01-01")).toEqual({ current: 3, best: 3 });
    expect(computeStreak(["2026-11-01", "2026-11-02", "2026-11-03", "2026-11-09"], "2026-11-09")).toEqual({ current: 1, best: 3 });
  });

  test("slug and id regexes, link", () => {
    for (const bad of ["../x", "A", "a b", "a".repeat(101), "", "two_sum", "x/y"]) expect(SLUG_RE.test(bad)).toBe(false);
    for (const ok of ["two-sum", "a", "a".repeat(100), "3sum"]) expect(SLUG_RE.test(ok)).toBe(true);
    for (const bad of ["", "1234567", "12a", "-1", " 1"]) expect(FRONTEND_ID_RE.test(bad)).toBe(false);
    expect(FRONTEND_ID_RE.test("1606")).toBe(true);
    expect(DATE_RE.test("2026-11-09")).toBe(true);
    expect(leetLink("two-sum")).toBe("https://leetcode.com/problems/two-sum/");
    expect(LeetDailyFacts.safeParse(facts).success).toBe(true);
    expect(LeetDailyFacts.safeParse({ ...facts, content: "<p>x</p>" }).success).toBe(false); // strict: no body field
    expect(LeetDailyFacts.safeParse({ ...facts, slug: "../../x" }).success).toBe(false);
    expect(LeetDailyFacts.safeParse({ ...facts, difficulty: "hard" }).success).toBe(false);
    expect(LeetDailyFacts.safeParse({ ...facts, title: "" }).success).toBe(false);
    expect(LeetDailyFacts.safeParse({ ...facts, title: "x".repeat(201) }).success).toBe(false);
    expect(LeetDailyFacts.safeParse({ ...facts, tags: Array.from({ length: 21 }, (_, i) => `t${i}`) }).success).toBe(false);
    expect(LeetDailyFacts.safeParse({ ...facts, tags: ["x".repeat(51)] }).success).toBe(false);
    expect(LeetDailyFacts.safeParse({ ...facts, is_paid_only: "no" }).success).toBe(false);
  });

  test("card data schemas", () => {
    expect(LeetDailyCardData.safeParse({ ...facts, date: "2026-11-09" }).success).toBe(true);
    expect(LeetDailyCardData.safeParse(facts).success).toBe(false); // date required
    expect(LeetDailyCardData.safeParse({ ...facts, date: "2026-11-09", content: "x" }).success).toBe(false);
    expect(LeetSolveCardData.safeParse(solve).success).toBe(true);
    expect(LeetSolveCardData.safeParse({ ...solve, code: null, runtime_percentile: null }).success).toBe(true);
    expect(LeetSolveCardData.safeParse({ ...solve, spoiler: false }).success).toBe(false);
    expect(LeetSolveCardData.safeParse({ ...solve, runtime_percentile: 101 }).success).toBe(false);
    expect(LeetSolveCardData.safeParse({ ...solve, lang: "" }).success).toBe(false);
    expect(LeetSolveCardData.safeParse({ ...solve, lang: "x".repeat(33) }).success).toBe(false);
    expect(LeetSolveCardData.safeParse({ ...solve, code: { lang: "golang", text: "" } }).success).toBe(false);
    expect(LeetSolveCardData.safeParse({ ...solve, code: { lang: "golang", text: "x".repeat(4001) } }).success).toBe(false);
    expect(LeetSolveCardData.safeParse({ ...solve, code: { lang: "golang", text: "가".repeat(4000) } }).success).toBe(true); // code points
    expect(LeetSolveCardData.safeParse({ ...solve, slug: "A" }).success).toBe(false);
    // the registry uses these schemas, server-only
    expect(CARD_TYPES["leet.daily"].serverOnly).toBe(true);
    expect(CARD_TYPES["leet.solve"].serverOnly).toBe(true);
    expect(CARD_TYPES["leet.daily"].versions[1]!.safeParse({}).success).toBe(false);
    expect(CARD_TYPES["leet.solve"].versions[1]!.safeParse(solve).success).toBe(true);
  });
});
