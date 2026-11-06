// Owner decision D1 (api.md 3.9): while an account is suspended, anyone but its owner sees it only as handle, display
// name and suspended=true. The profile already hides presence/last_seen_at; the presence paths must too — GET
// /v1/presence, presence.snapshot and presence.updated give `{status: "offline", last_seen_at: null}` for it (the
// last_seen_at would otherwise be, in effect, the suspension time). Suspend and unsuspend also send user.updated to its
// presence watchers, so an open friends list drops (and later restores) the status message and avatar.
import { env } from "cloudflare:test";
import { describe, expect, it } from "vitest";
import { api, connectWs, makeAdmin, makeFriends, signup, type TestUser } from "../helpers";

const settle = (ms = 150) => new Promise<void>((r) => setTimeout(r, ms));

async function presenceOf(viewer: TestUser, target: TestUser): Promise<any> {
  const r = await api(viewer.token).get<any>(`/v1/presence?user_ids=${target.user.id}`);
  expect(r.status).toBe(200);
  return r.body.items;
}

/** o (friend of f) has been online once, so its last_seen_at is set; adm is an operator. */
async function seenOnce(): Promise<{ o: TestUser; f: TestUser; adm: TestUser }> {
  const [o, f, adm] = await Promise.all([signup(), signup(), signup()]);
  await makeAdmin(adm);
  await makeFriends(o, f);
  const c = await connectWs(o.token);
  c.close();
  for (let i = 0; i < 40; i++) {
    const [it] = await presenceOf(f, o);
    if (it?.last_seen_at) break;
    await settle(50);
  }
  const [it] = await presenceOf(f, o);
  expect(it).toEqual({ user_id: o.user.id, status: "offline", last_seen_at: expect.any(String) });
  return { o, f, adm };
}

describe("presence of a suspended account (D1)", () => {
  it("GET /v1/presence: offline with last_seen_at null while suspended; the real value is back after unsuspend", async () => {
    const { o, f, adm } = await seenOnce();
    const [before] = await presenceOf(f, o);
    expect((await api(adm.token).post(`/v1/admin/users/@${o.handle}/suspend`, { reason: "spam" })).status).toBe(200);
    expect(await presenceOf(f, o)).toEqual([{ user_id: o.user.id, status: "offline", last_seen_at: null }]);
    expect((await api(adm.token).post(`/v1/admin/users/@${o.handle}/unsuspend`)).status).toBe(200);
    expect(await presenceOf(f, o)).toEqual([before]);
  });

  it("a timed suspension that has passed shows presence again (same boundary as 3.4 rule 2)", async () => {
    const { o, f } = await seenOnce();
    const [before] = await presenceOf(f, o);
    const t = Date.now();
    await env.DB.prepare("UPDATE users SET suspended_at = ?1, suspended_until = ?2 WHERE id = ?3").bind(t - 10_000, t + 60_000, o.user.id).run();
    expect(await presenceOf(f, o)).toEqual([{ user_id: o.user.id, status: "offline", last_seen_at: null }]);
    await env.DB.prepare("UPDATE users SET suspended_until = ?1 WHERE id = ?2").bind(t - 1, o.user.id).run();
    expect(await presenceOf(f, o)).toEqual([before]);
  });

  it("presence.snapshot after ready: the suspended friend is offline with last_seen_at null", async () => {
    const { o, f, adm } = await seenOnce();
    expect((await api(adm.token).post(`/v1/admin/users/@${o.handle}/suspend`, { reason: "spam" })).status).toBe(200);
    const cf = await connectWs(f.token);
    expect((await cf.next("presence.snapshot")).data.items).toEqual([{ user_id: o.user.id, status: "offline", last_seen_at: null }]);
  });

  it("the disconnect on suspension sends presence.updated offline with last_seen_at null, not the suspension time", async () => {
    const { o, f, adm } = await seenOnce();
    const cf = await connectWs(f.token);
    await connectWs(o.token);
    await cf.next("presence.updated", (d) => d.user_id === o.user.id && d.status === "online");
    expect((await api(adm.token).post(`/v1/admin/users/@${o.handle}/suspend`, { reason: "spam" })).status).toBe(200);
    const off = await cf.next("presence.updated", (d) => d.user_id === o.user.id && d.status === "offline");
    expect(off.data).toEqual({ user_id: o.user.id, status: "offline", last_seen_at: null });
  });

  it("suspend and unsuspend send user.updated to the account's friends (status and avatar masked, then back)", async () => {
    const [o, f, adm] = await Promise.all([signup(), signup(), signup()]);
    await makeAdmin(adm);
    await makeFriends(o, f);
    expect((await api(o.token).put("/v1/me/status", { emoji: "🎧", text: "듣는 중" })).status).toBe(200);
    const cf = await connectWs(f.token);
    expect((await api(adm.token).post(`/v1/admin/users/@${o.handle}/suspend`, { reason: "spam" })).status).toBe(200);
    const sus = await cf.next("user.updated", (d) => d.user.id === o.user.id && d.user.suspended === true);
    expect(sus.data.user).toEqual(expect.objectContaining({ handle: o.handle, suspended: true, status: null, avatar_url: null }));
    expect((await api(adm.token).post(`/v1/admin/users/@${o.handle}/unsuspend`)).status).toBe(200);
    const back = await cf.next("user.updated", (d) => d.user.id === o.user.id && d.user.suspended === false);
    expect(back.data.user.status).toEqual(expect.objectContaining({ text: "듣는 중" }));
  });
});
