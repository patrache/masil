import { describe, it, expect } from "vitest";
import { SELF } from "cloudflare:test";
import { api, BASE, signup, uniqueHandle, uniqueIp } from "../helpers";

describe("rate limits", () => {
  it("RL_SIGNUP: 6th signup from one IP in 60s -> 429 with Retry-After", async () => {
    const ip = uniqueIp(); let last: Response | undefined;
    for (let i = 0; i < 6; i++) last = await SELF.fetch(BASE + "/v1/auth/signup", { method: "POST", headers: { "Content-Type": "application/json", "CF-Connecting-IP": ip, "User-Agent": "masil/0.1.0 (test; test)" }, body: JSON.stringify({ handle: uniqueHandle(), password: "password123" }) });
    expect(last!.status).toBe(429);
    expect(last!.headers.get("Retry-After")).toBe("60");
    expect((await last!.json<any>()).error.details).toEqual({ limiter: "RL_SIGNUP", retry_after_sec: 60 });
  });
  it("RL_LOGIN keys by handle across IPs", async () => {
    const u = await signup(); let status = 0;
    for (let i = 0; i < 11; i++) status = (await api().post("/v1/auth/login", { handle: u.handle, password: "wrong pass" })).status;
    expect(status).toBe(429);
  });
});
