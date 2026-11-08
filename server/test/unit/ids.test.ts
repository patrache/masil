import { it, expect } from "vitest";
import { newId, isId, idFloor, newInviteCode } from "../../src/lib/ids";
import { iso, isoOrNull } from "../../src/lib/time";

it("ULID shape and monotonic within the same ms", () => {
  const ids = Array.from({ length: 1000 }, () => newId(1_700_000_000_000));
  expect(ids.every((s) => /^[0-9A-HJKMNP-TV-Z]{26}$/.test(s) && isId(s))).toBe(true);
  expect([...ids].sort()).toEqual(ids);
  expect(new Set(ids).size).toBe(1000);
});
it("idFloor sorts below every id of that ms and above earlier ms", () => {
  // idFloor is pure; never compare against an id made after a larger ms (newId is monotonic and would keep that larger ms)
  expect(idFloor(1_699_999_999_999) < idFloor(1_700_000_000_000)).toBe(true);
  expect(idFloor(1_700_000_000_000).endsWith("0000000000000000")).toBe(true);
  const ms = Date.now();                 // >= every ms this module has seen (tests never pass future ms to newId)
  const id = newId(ms);
  expect(id.slice(0, 10)).toBe(idFloor(ms).slice(0, 10));
  expect(idFloor(ms) < id && id < idFloor(ms + 1)).toBe(true);
});
it("clock going backwards keeps the last timestamp and increments the random part", () => {
  const a = newId(Date.now());
  const b = newId(Date.now() - 5_000);
  expect(b > a).toBe(true);
  expect(b.slice(0, 10)).toBe(a.slice(0, 10));
});
it("invite code: 8 chars without 0 O o 1 l I", () => {
  for (let i = 0; i < 200; i++) expect(newInviteCode()).toMatch(/^[A-HJ-NP-Za-km-np-z2-9]{8}$/);
});
it("iso is RFC3339 ms Z", () => {
  expect(iso(Date.UTC(2026, 10, 5, 2, 3, 4, 567))).toBe("2026-11-05T02:03:04.567Z");
  expect(isoOrNull(null)).toBeNull();
});
