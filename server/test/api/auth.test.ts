import { describe, it, expect } from "vitest";
import { env, SELF } from "cloudflare:test";
import { createApp } from "../../src/app";
import { api, BASE, login, signup, uniqueHandle } from "../helpers";

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

describe("signup/login", () => {
  it("signup 201 returns token, session, Me with defaults", async () => {
    const handle = uniqueHandle();
    const r = await api().post<any>("/v1/auth/signup", { handle: handle.toUpperCase(), password: "correct horse battery", display_name: "  민지 ", device_name: "mbp" });
    expect(r.status).toBe(201);
    expect(r.body.token).toMatch(/^msl_[A-Za-z0-9_-]{43}$/);
    expect(r.body.session).toMatchObject({ client: "tui", device_name: "mbp", current: true });
    expect(r.body.user).toMatchObject({ handle, display_name: "민지", role: "user", is_private: false, settings: { dm_policy: "friends_and_spaces", notify: { dm: true, social: false }, focus_default_min: 25 }, presence: { status: "offline", mode: "auto", focus: null } });
    const row = await env.DB.prepare("SELECT password_hash FROM users WHERE handle=?").bind(handle).first<any>();
    expect(row.password_hash.startsWith("pbkdf2_sha256$100000$")).toBe(true);
  });
  it("validation: reserved, short, password==handle, dup", async () => {
    expect((await api().post<any>("/v1/auth/signup", { handle: "admin", password: "12345678" })).body.error.details.fields.handle).toBe("invalid");
    expect((await api().post<any>("/v1/auth/signup", { handle: "abcdef", password: "short" })).body.error.details.fields.password).toBe("too_short");
    expect((await api().post<any>("/v1/auth/signup", { handle: "samesame", password: "samesame" })).body.error.details.fields.password).toBe("invalid");
    const u = await signup();
    expect((await api().post<any>("/v1/auth/signup", { handle: u.handle, password: "another pass" })).status).toBe(409);
  });
  it("login ok / wrong password / unknown handle look identical", async () => {
    const u = await signup();
    expect((await api().post<any>("/v1/auth/login", { handle: u.handle, password: u.password })).status).toBe(200);
    const a = await api().post<any>("/v1/auth/login", { handle: u.handle, password: "nope nope" });
    const b = await api().post<any>("/v1/auth/login", { handle: uniqueHandle(), password: "nope nope" });
    expect([a.status, a.body.error.code, b.status, b.body.error.code]).toEqual([401, "invalid_credentials", 401, "invalid_credentials"]);
  });
  it("auth middleware: missing/garbage token 401; suspended 403", async () => {
    expect((await api().get("/v1/auth/sessions")).status).toBe(401);
    expect((await api("msl_garbage").get("/v1/auth/sessions")).status).toBe(401);
    const u = await signup();
    await env.DB.prepare("UPDATE users SET suspended_at=?1, suspend_reason='spam' WHERE id=?2").bind(Date.now(), u.user.id).run();
    const r = await api().post<any>("/v1/auth/login", { handle: u.handle, password: u.password });
    expect(r.status).toBe(403); expect(r.body.error).toMatchObject({ code: "account_suspended", details: { until: null, reason: "spam" } });
  });
});
describe("sessions and auth middleware details", () => {
  it("GET /auth/sessions lists my active sessions newest first with current flag", async () => {
    const u = await signup();
    const second = await login(u.handle, u.password);
    const r = await api(second.token).get<any>("/v1/auth/sessions");
    expect(r.status).toBe(200);
    expect(r.body.items).toHaveLength(2);
    expect(r.body.items.map((s: any) => s.current)).toEqual([true, false]);
    expect(Object.keys(r.body.items[0]).sort()).toEqual(["client", "created_at", "current", "device_name", "expires_at", "id", "last_used_at"]);
  });
  it("touches last_used_at/expires_at only when older than 1h; expired or revoked sessions -> 401", async () => {
    const u = await signup();
    const old = Date.now() - 2 * 3_600_000;
    await env.DB.prepare("UPDATE sessions SET last_used_at=?1, expires_at=?2 WHERE user_id=?3").bind(old, old + 1_000_000_000, u.user.id).run();
    expect((await api(u.token).get("/v1/auth/sessions")).status).toBe(200);
    const row = await env.DB.prepare("SELECT last_used_at, expires_at FROM sessions WHERE user_id=?1").bind(u.user.id).first<any>();
    expect(row.last_used_at).toBeGreaterThan(old);
    expect(row.expires_at - row.last_used_at).toBe(90 * 86_400_000);
    await env.DB.prepare("UPDATE sessions SET expires_at=?1 WHERE user_id=?2").bind(Date.now() - 1, u.user.id).run();
    expect((await api(u.token).get<any>("/v1/auth/sessions")).body.error.code).toBe("unauthorized");
    const v = await signup();
    await env.DB.prepare("UPDATE sessions SET revoked_at=?1, revoke_reason='logout' WHERE user_id=?2").bind(Date.now(), v.user.id).run();
    expect((await api(v.token).get("/v1/auth/sessions")).status).toBe(401);
  });
  it("suspended user's token -> 403 on API; suspension that already ended counts as active", async () => {
    const u = await signup();
    await env.DB.prepare("UPDATE users SET suspended_at=?1, suspended_until=?2, suspend_reason='spam' WHERE id=?3").bind(Date.now(), Date.now() + 60_000, u.user.id).run();
    const r = await api(u.token).get<any>("/v1/auth/sessions");
    expect(r.status).toBe(403);
    expect(r.body.error.details).toEqual({ until: expect.any(String), reason: "spam" });
    await env.DB.prepare("UPDATE users SET suspended_until=?1 WHERE id=?2").bind(Date.now() - 1, u.user.id).run();
    expect((await api(u.token).get("/v1/auth/sessions")).status).toBe(200);
    expect((await api().post("/v1/auth/login", { handle: u.handle, password: u.password })).status).toBe(200);
  });
});
describe("app wiring", () => {
  // Hono app.route() copies a sub-app's .use(pattern) into the parent as an ALL route; on a shared prefix that runs
  // requireAuth (and the RL_API count) twice for other modules' routes. requireAuth must be a per-route argument.
  it("no wildcard ALL middleware on shared prefixes (requireAuth runs once per request)", () => {
    const shared = /^\/v1(\/(auth|me|users|channels|messages|spaces|hashtags)(\/:[^/]+)?)?\/\*$/;
    const offenders = createApp().routes.filter((r) => r.method === "ALL" && shared.test(r.path)).map((r) => r.path);
    expect(offenders).toEqual([]);
  });
  it("signup and login stay reachable without a token", async () => {
    const r = await api().post<any>("/v1/auth/login", { handle: uniqueHandle(), password: "nope nope" });
    expect(r.body.error.code).toBe("invalid_credentials"); // not "unauthorized": no auth middleware in front of login
  });
});
