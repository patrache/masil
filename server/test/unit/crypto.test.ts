import { it, expect } from "vitest";
import { hashPassword, verifyPassword, newSessionToken, sha256Hex, dummyVerify } from "../../src/lib/crypto";

it("pbkdf2 format and verify", async () => {
  const h = await hashPassword("correct horse battery");
  expect(h).toMatch(/^pbkdf2_sha256\$100000\$[A-Za-z0-9_-]{22}\$[A-Za-z0-9_-]{43}$/);
  expect(await verifyPassword("correct horse battery", h)).toBe(true);
  expect(await verifyPassword("wrong", h)).toBe(false);
  expect(await verifyPassword("x", "garbage")).toBe(false);
  await expect(dummyVerify()).resolves.toBeUndefined();
});
it("token and sha256", async () => {
  expect(newSessionToken()).toMatch(/^msl_[A-Za-z0-9_-]{43}$/);
  expect(await sha256Hex("abc")).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
});
