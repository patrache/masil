import { describe, it, expect } from "vitest";
import { SELF } from "cloudflare:test";
import { api, BASE } from "../helpers";

describe("app shell", () => {
  it("meta and health", async () => {
    const r = await api().get<any>("/v1/meta");
    expect(r.status).toBe(200);
    expect(r.body).toMatchObject({ api_version: 1, min_client_version: "0.1.0", limits: { message_max_chars: 4000, group_dm_max_members: 10, reactions_per_message: 20, watch_topics_max: 50 } });
    expect(r.headers.get("X-Request-Id")).toMatch(/^[0-9A-Z]{26}$/);
    expect((await api().get("/v1/health")).body).toEqual({ ok: true });
  });
  it("unknown route -> 404 envelope", async () => {
    const r = await api().get<any>("/v1/nope");
    expect(r.status).toBe(404);
    expect(r.body).toEqual({ error: { code: "not_found", message: expect.any(String), details: {} } });
  });
  it("old client -> 426, non-masil UA passes", async () => {
    const old = await SELF.fetch(BASE + "/v1/meta", { headers: { "User-Agent": "masil/0.0.9 (x; y)" } });
    expect(old.status).toBe(426);
    expect((await old.json<any>()).error.details).toEqual({ min_client_version: "0.1.0" });
    expect((await SELF.fetch(BASE + "/v1/meta", { headers: { "User-Agent": "curl/8" } })).status).toBe(200);
  });
  it("bad JSON -> 400 invalid_json; >64KB -> 413", async () => {
    const bad = await api().raw("/v1/auth/signup", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{" });
    expect(bad.status).toBe(400); expect((await bad.json<any>()).error.code).toBe("invalid_json");
    const big = await api().raw("/v1/auth/signup", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ x: "a".repeat(70_000) }) });
    expect(big.status).toBe(413); expect((await big.json<any>()).error.code).toBe("payload_too_large");
  });
});
