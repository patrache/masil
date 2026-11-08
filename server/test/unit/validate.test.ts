import { describe, it, expect } from "vitest";
import { Hono } from "hono";
import * as z from "zod";
import type { AppEnv } from "../../src/env";
import { onError } from "../../src/lib/errors";
import { decodeCursor, parseLimit } from "../../src/lib/db";
import { vJson, vQuery, zId, zNonce, zText, zUserRef } from "../../src/lib/validate";

function app() {
  const a = new Hono<AppEnv>();
  a.onError(onError);
  a.post(
    "/j",
    vJson(z.object({ name: zText(1, 3), nonce: zNonce, user: zUserRef, nested: z.object({ id: zId }).optional() })),
    (c) => c.json(c.req.valid("json")),
  );
  a.get("/q", vQuery(z.object({ q: zText(1, 5) })), (c) => c.json(c.req.valid("query")));
  return a;
}

const post = (body: unknown) =>
  app().request("/j", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });

describe("validate", () => {
  it("maps zod issues to field codes", async () => {
    const r = await post({ name: "abcd", nonce: "", user: "nope", nested: { id: "x" } });
    expect(r.status).toBe(400);
    const e = (await r.json<any>()).error;
    expect(e.code).toBe("invalid_request");
    expect(e.details.fields).toEqual({ name: "too_long", nonce: "too_short", user: "invalid", "nested.id": "invalid" });
    const missing = await post({});
    expect((await missing.json<any>()).error.details.fields).toEqual({ name: "required", nonce: "required", user: "required" });
  });
  it("NFC + trim, code-point length, user refs lowercased", async () => {
    const nfd = "가나"; // 가나 in NFD (4 UTF-16 units, 2 code points after NFC)
    const r = await post({ name: `  ${nfd}다 `, nonce: "n-1_A", user: "@MinJi" });
    expect(r.status).toBe(200);
    expect(await r.json()).toEqual({ name: "가나다", nonce: "n-1_A", user: "@minji" });
    expect((await (await post({ name: "   ", nonce: "a", user: "@me" })).json<any>()).error.details.fields).toEqual({ name: "too_short" });
    const q = await app().request("/q?q=%20hi%20");
    expect(await q.json()).toEqual({ q: "hi" });
  });
  it("cursor/limit errors are invalid_request", () => {
    expect(() => decodeCursor("%%%")).toThrow(expect.objectContaining({ code: "invalid_request", details: { fields: { cursor: "invalid" } } }));
    expect(() => parseLimit("0")).toThrow(expect.objectContaining({ status: 400, details: { fields: { limit: "invalid" } } }));
  });
});
