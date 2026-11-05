import { it, expect } from "vitest";
import * as t from "../../src/lib/text";

it("normalize NFD->NFC and trim; count code points", () => {
  expect(t.normalizeText("  각 ")).toBe("각");
  expect(t.charCount("가😀a")).toBe(3);
});
it("mentions: regex, lowercase, unique, skips code", () => {
  expect(t.extractMentions("@Minji hi @minji a@bob `@code` ```\n@blk\n``` @jun_ho.")).toEqual(["minji", "jun_ho"]);
  expect(t.extractMentions(Array.from({ length: 60 }, (_, i) => `@user${i}`).join(" "))).toHaveLength(50);
  expect(t.extractMentions("@ab @here")).toEqual(["here"]); // 'ab' too short; "here" is filtered by caller (reserved)
});
it("@here detection ignores code", () => {
  expect(t.hasHereMention("ping @here now")).toBe(true);
  expect(t.hasHereMention("`@here`")).toBe(false);
  expect(t.hasHereMention("@heres")).toBe(false);
});
it("hashtags NFC lowercase unique max 10", () => {
  expect(t.extractHashtags("#첫눈 #Go #go a#b")).toEqual(["첫눈", "go"]);
  expect(t.extractHashtags(Array.from({ length: 12 }, (_, i) => `#t${i}`).join(" "))).toHaveLength(10);
});
it("dice", () => {
  expect(t.parseDice("2d6+3")).toEqual({ count: 2, sides: 6, modifier: 3 });
  for (const bad of ["0d6", "21d6", "1d1", "1d1001", "1d6+1001", "d6", "2d6 "]) expect(t.parseDice(bad)).toBeNull();
  const r = t.rollDice({ count: 3, sides: 6, modifier: -1 });
  expect(r.rolls).toHaveLength(3); expect(r.total).toBe(r.rolls.reduce((a, b) => a + b, 0) - 1);
  expect(r.expr).toBe("3d6-1");
});
it("snippet strips markdown and collapses whitespace", () => {
  expect(t.plainSnippet("**굵게**  `code`\n\n[링크](https://x.y)", 100)).toBe("굵게 code 링크");
  expect(t.charCount(t.plainSnippet("가".repeat(300), 200))).toBe(200);
});
it("single emoji", () => {
  for (const ok of ["👍", "👍🏽", "👨‍👩‍👧", "🇰🇷", "❤️"]) expect(t.isSingleEmoji(ok, 32)).toBe(true);
  for (const bad of ["👍👍", "ab", "", "가"]) expect(t.isSingleEmoji(bad, 32)).toBe(false);
});
