import { describe, it, expect } from "vitest";
import { env, SELF } from "cloudflare:test";
import { verifyPassword } from "../../src/lib/crypto";
import { api, BASE, inOneRateWindow, makeFriends, signup, uniqueHandle, uniqueIp } from "../helpers";

// Every test that spends a budget and expects the next hit to be 429 runs inside inOneRateWindow: the local limiter
// counts in wall-clock-aligned fixed windows, so a minute boundary in the middle would reset the count (a false failure).
describe("rate limits", () => {
  it("RL_SIGNUP: 6th signup from one IP in 60s -> 429 with Retry-After", async () => {
    await inOneRateWindow(60, async () => {
      const ip = uniqueIp(); let last: Response | undefined;
      for (let i = 0; i < 6; i++) last = await SELF.fetch(BASE + "/v1/auth/signup", { method: "POST", headers: { "Content-Type": "application/json", "CF-Connecting-IP": ip, "User-Agent": "masil/0.1.0 (test; test)" }, body: JSON.stringify({ handle: uniqueHandle(), password: "password123" }) });
      expect(last!.status).toBe(429);
      expect(last!.headers.get("Retry-After")).toBe("60");
      expect((await last!.json<any>()).error.details).toEqual({ limiter: "RL_SIGNUP", retry_after_sec: 60 });
    });
  });
  it("RL_LOGIN keys by handle across IPs", async () => {
    await inOneRateWindow(60, async () => {
      const u = await signup(); let status = 0;
      for (let i = 0; i < 11; i++) status = (await api().post("/v1/auth/login", { handle: u.handle, password: "wrong pass" })).status;
      expect(status).toBe(429);
    });
  });

  // POST /v1/auth/password re-verifies the current password, so it is a password oracle for anyone holding a stolen
  // token: it shares RL_LOGIN (same ip:/handle: keys) with login, checked before the PBKDF2 verify (api.md 0.7, 1.4).
  describe("RL_LOGIN on POST /v1/auth/password", () => {
    const wrong = { current_password: "wrong pass", new_password: "brand new pass" };

    it("keys by handle across IPs: 11th wrong current_password -> 429 in the contract shape", async () => {
      await inOneRateWindow(60, async () => {
        const u = await signup(); const statuses: number[] = []; let last: any;
        for (let i = 0; i < 11; i++) { last = await api(u.token).post<any>("/v1/auth/password", wrong); statuses.push(last.status); }
        expect(statuses.slice(0, 10)).toEqual(Array(10).fill(401));
        expect(last.status).toBe(429);
        expect(last.headers.get("Retry-After")).toBe("60");
        expect(last.body.error.code).toBe("rate_limited");
        expect(last.body.error.details).toEqual({ limiter: "RL_LOGIN", retry_after_sec: 60 });
      });
    });

    it("keys by IP across users", async () => {
      await inOneRateWindow(60, async () => {
        const ip = uniqueIp(); let status = 0;
        for (let i = 0; i < 11; i++) {
          const u = await signup();
          status = (await api(u.token).raw("/v1/auth/password", { method: "POST", headers: { "Content-Type": "application/json", "CF-Connecting-IP": ip }, body: JSON.stringify(wrong) })).status;
        }
        expect(status).toBe(429);
      });
    });

    it("shares the handle budget with login, and a limited request never changes the password", async () => {
      await inOneRateWindow(60, async () => {
        const u = await signup();
        for (let i = 0; i < 10; i++) expect((await api().post("/v1/auth/login", { handle: u.handle, password: "wrong pass" })).status).toBe(401);
        // Even the correct current password is refused once the budget is spent; the hash stays as it was.
        const r = await api(u.token).post<any>("/v1/auth/password", { current_password: u.password, new_password: "brand new pass" });
        expect(r.status).toBe(429);
        expect(r.body.error.details).toEqual({ limiter: "RL_LOGIN", retry_after_sec: 60 });
        const row = await env.DB.prepare("SELECT password_hash FROM users WHERE id = ?1").bind(u.user.id).first<{ password_hash: string }>();
        expect(await verifyPassword(u.password, row!.password_hash)).toBe(true);
        // The session survives a 429 (an outsider can spend the shared handle budget, so tripping it must not log the owner out).
        expect((await api(u.token).get("/v1/auth/sessions")).status).toBe(200);
      });
    });

    it("a normal change within the budget still succeeds", async () => {
      const u = await signup();
      expect((await api(u.token).post("/v1/auth/password", wrong)).status).toBe(401);
      expect((await api(u.token).post("/v1/auth/password", { current_password: u.password, new_password: "brand new pass" })).status).toBe(204);
      expect((await api().post("/v1/auth/login", { handle: u.handle, password: "brand new pass" })).status).toBe(200);
    });
  });

  // Member add is the other way into a group DM (besides POST /v1/dms), so it shares RL_SOCIAL: an unthrottled add
  // could be retried freely to win a race against the in-batch checks (pending item 8).
  it("RL_SOCIAL on POST /v1/channels/:id/members: 31st add in 60s -> 429", async () => {
    await inOneRateWindow(60, async () => {
      const [o, x, y] = await Promise.all([signup(), signup(), signup()]);
      await makeFriends(o, x);
      await makeFriends(o, y);
      const g = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`, `@${y.handle}`] })).body.channel;
      // x has spent nothing on RL_SOCIAL (it only accepted o's request); y is already a member -> 409 until the limit
      const statuses: number[] = []; let last: any;
      for (let i = 0; i < 31; i++) { last = await api(x.token).post<any>(`/v1/channels/${g.id}/members`, { users: [`@${y.handle}`] }); statuses.push(last.status); }
      expect(statuses.slice(0, 30)).toEqual(Array(30).fill(409));
      expect(last.status).toBe(429);
      expect(last.headers.get("Retry-After")).toBe("60");
      expect(last.body.error.details).toEqual({ limiter: "RL_SOCIAL", retry_after_sec: 60 });
    });
  });
});
