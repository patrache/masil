// test/api/follows.test.ts
import { expect, test } from "vitest";
import { api, signup, follow } from "../helpers";
const me = async (t: any) => (await api(t.token).get<any>("/v1/me")).body.user;
const inbox = async (t: any) => (await api(t.token).get<any>("/v1/notifications")).body.items;

test("public follow is active, counters, follow notification deduped across refollow", async () => {
  const a = await signup(); const b = await signup();
  expect(await follow(a, b)).toBe("active");
  expect(await follow(a, b)).toBe("active");
  expect((await me(b)).follower_count).toBe(1); expect((await me(a)).following_count).toBe(1);
  expect((await api(a.token).del(`/v1/users/@${b.handle}/follow`)).status).toBe(204);
  expect((await me(b)).follower_count).toBe(0);
  await follow(a, b);
  expect((await inbox(b)).filter((n: any) => n.type === "follow").length).toBe(1);
});
test("self -> 409 conflict self; blocked either way -> 403 action_not_allowed", async () => {
  const a = await signup(); const b = await signup();
  const s = await api(a.token).put<any>(`/v1/users/@me/follow`);
  expect(s.status).toBe(409); expect(s.body.error.details.reason).toBe("self");
  await api(b.token).put(`/v1/blocks/@${a.handle}`);
  expect((await api(a.token).put<any>(`/v1/users/@${b.handle}/follow`)).body.error.code).toBe("action_not_allowed");
});
test("private account: pending -> follow_request; accept -> active + follow_accepted; decline silent", async () => {
  const p = await signup(); const a = await signup(); const c = await signup();
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  expect(await follow(a, p)).toBe("pending");
  expect((await me(p)).follower_count).toBe(0);
  expect((await inbox(p))[0]).toMatchObject({ type: "follow_request", actor: { id: a.user.id } });
  const reqs = await api(p.token).get<any>("/v1/me/follow-requests");
  expect(reqs.body.items.map((x: any) => x.user.id)).toEqual([a.user.id]);
  expect((await api(p.token).post(`/v1/me/follow-requests/@${a.handle}/accept`)).status).toBe(204);
  expect((await me(p)).follower_count).toBe(1);
  expect((await inbox(a))[0]).toMatchObject({ type: "follow_accepted", actor: { id: p.user.id } });
  await follow(c, p);
  expect((await api(p.token).post(`/v1/me/follow-requests/@${c.handle}/decline`)).status).toBe(204);
  expect((await inbox(c)).length).toBe(0);
  // pending rows were never counted: decline must not touch the counters
  expect((await me(p)).follower_count).toBe(1); expect((await me(c)).following_count).toBe(0);
  expect((await api(p.token).post(`/v1/me/follow-requests/@${c.handle}/accept`)).status).toBe(404);
});
test("cancelling a pending request or removing a pending follower leaves counters unchanged", async () => {
  const p = await signup(); const a = await signup(); const c = await signup();
  await follow(a, p);
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  expect(await follow(c, p)).toBe("pending");
  expect((await api(c.token).del(`/v1/users/@${p.handle}/follow`)).status).toBe(204);
  expect((await me(p)).follower_count).toBe(1); expect((await me(c)).following_count).toBe(0);
  expect(await follow(c, p)).toBe("pending");
  expect((await api(p.token).del(`/v1/users/@${c.handle}/follower`)).status).toBe(204);
  expect((await me(p)).follower_count).toBe(1); expect((await me(c)).following_count).toBe(0);
  expect((await api(p.token).get<any>("/v1/me/follow-requests")).body.items).toEqual([]);
});
test("declining an already-active follower -> 404 and counters unchanged (repeated)", async () => {
  const p = await signup(); const a = await signup();
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  expect(await follow(a, p)).toBe("pending");
  expect((await api(p.token).post(`/v1/me/follow-requests/@${a.handle}/accept`)).status).toBe(204);
  expect((await me(p)).follower_count).toBe(1); expect((await me(a)).following_count).toBe(1);
  for (let i = 0; i < 2; i++) {
    expect((await api(p.token).post(`/v1/me/follow-requests/@${a.handle}/decline`)).status).toBe(404);
    expect((await me(p)).follower_count).toBe(1); expect((await me(a)).following_count).toBe(1);
  }
  // the active follow row is still there and still removable normally
  expect((await api(p.token).del(`/v1/users/@${a.handle}/follower`)).status).toBe(204);
  expect((await me(p)).follower_count).toBe(0); expect((await me(a)).following_count).toBe(0);
});
test("going public approves all pending and notifies each requester", async () => {
  const p = await signup(); const a = await signup(); const b = await signup();
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  await follow(a, p); await follow(b, p);
  await api(p.token).patch("/v1/me/settings", { is_private: false });
  expect((await me(p)).follower_count).toBe(2);
  for (const x of [a, b]) expect((await inbox(x))[0].type).toBe("follow_accepted");
});
test("followers/following lists: active only, private -> 403 private_account, blocked -> 404, remove follower", async () => {
  const p = await signup(); const a = await signup(); const s = await signup(); const blk = await signup();
  await follow(a, p);
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  await follow(s, p);
  const own = await api(p.token).get<any>(`/v1/users/@${p.handle}/followers`);
  expect(own.body.items.map((u: any) => u.id)).toEqual([a.user.id]);
  expect((await api(s.token).get<any>(`/v1/users/@${p.handle}/followers`)).body.error.code).toBe("private_account");
  expect((await api(a.token).get<any>(`/v1/users/@${a.handle}/following`)).body.items[0].id).toBe(p.user.id);
  await api(blk.token).put(`/v1/blocks/@${a.handle}`);
  expect((await api(a.token).get(`/v1/users/@${blk.handle}/following`)).status).toBe(404);
  expect((await api(p.token).del(`/v1/users/@${a.handle}/follower`)).status).toBe(204);
  expect((await me(p)).follower_count).toBe(0);
});
