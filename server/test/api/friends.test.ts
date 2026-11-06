import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { newId } from "../../src/lib/ids";
import { api, connectWs, makeFriends, signup, uniqueHandle } from "../helpers";

// ---- local fixtures (this file only) ----

type U = { user: { id: string } };

async function blockSql(blocker: U, blocked: U): Promise<void> {
  await env.DB.prepare("INSERT INTO blocks (blocker_id, blocked_id, created_at) VALUES (?1, ?2, ?3)").bind(blocker.user.id, blocked.user.id, Date.now()).run();
}

/** `n` throwaway users plus a pending friend request from `from` to each (one INSERT … SELECT FROM json_each each). */
async function seedPendingRequestsSql(from: U, n: number): Promise<void> {
  const t = Date.now();
  const users = Array.from({ length: n }, () => ({ id: newId(), handle: uniqueHandle("pq"), rid: newId() }));
  const list = JSON.stringify(users);
  await env.DB.batch([
    env.DB.prepare(
      `INSERT INTO users (id, handle, display_name, password_hash, created_at, updated_at)
       SELECT json_extract(j.value, '$.id'), json_extract(j.value, '$.handle'), 'seed', 'x', ?2, ?2 FROM json_each(?1) j`,
    ).bind(list, t),
    env.DB.prepare(
      `INSERT INTO friend_requests (id, from_user_id, to_user_id, status, created_at)
       SELECT json_extract(j.value, '$.rid'), ?2, json_extract(j.value, '$.id'), 'pending', ?3 FROM json_each(?1) j`,
    ).bind(list, from.user.id, t),
  ]);
}

describe("friends", () => {
  it("request -> accept -> friends both sides with events", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const [ca, cb] = await Promise.all([connectWs(a.token), connectWs(b.token)]);
    const r = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
    expect(r.status).toBe(201);
    expect(r.body).toMatchObject({ request: { status: "pending", from: { id: a.user.id }, to: { id: b.user.id } }, friend: null });
    await cb.next("friend.request_received");
    expect((await cb.next("alert")).data.alert.kind).toBe("friend_request");
    const acc = await api(b.token).post<any>(`/v1/friends/requests/${r.body.request.id}/accept`);
    expect(acc.body.friend.user.id).toBe(a.user.id);
    await ca.next("friend.request_accepted");
    expect((await ca.next("friend.added")).data.friend.user.id).toBe(b.user.id);
    expect((await api(a.token).get<any>("/v1/friends")).body.items.map((f: any) => f.user.id)).toEqual([b.user.id]);
  });

  it("crossing requests auto-accept with 200", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await api(a.token).post("/v1/friends/requests", { user: `@${b.handle}` });
    const r = await api(b.token).post<any>("/v1/friends/requests", { user: `@${a.handle}` });
    expect(r.status).toBe(200);
    expect(r.body.request.status).toBe("accepted");
    expect(r.body.friend.user.id).toBe(a.user.id);
  });

  it("errors: self, duplicate, already friends, blocked, wrong accepter", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    expect((await api(a.token).post<any>("/v1/friends/requests", { user: "@me" })).body.error).toMatchObject({ code: "conflict", details: { reason: "self" } });
    const r = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
    expect((await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` })).body.error.details.reason).toBe("pending");
    expect((await api(c.token).post(`/v1/friends/requests/${r.body.request.id}/accept`)).status).toBe(404);
    await blockSql(c, a);
    expect((await api(a.token).post<any>("/v1/friends/requests", { user: `@${c.handle}` })).body.error.code).toBe("action_not_allowed");
  });

  it("decline and cancel emit request_removed; unfriend emits friend.removed both sides", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    const ca = await connectWs(a.token);
    const r = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
    expect((await api(b.token).post(`/v1/friends/requests/${r.body.request.id}/decline`)).status).toBe(204);
    expect((await ca.next("friend.request_removed")).data).toEqual({ request_id: r.body.request.id, reason: "declined" });
    const r2 = await api(a.token).post<any>("/v1/friends/requests", { user: `@${c.handle}` });
    const cc = await connectWs(c.token);
    expect((await api(c.token).del(`/v1/friends/requests/${r2.body.request.id}`)).status).toBe(404); // only the sender cancels
    expect((await api(a.token).del(`/v1/friends/requests/${r2.body.request.id}`)).status).toBe(204);
    expect((await cc.next("friend.request_removed")).data).toEqual({ request_id: r2.body.request.id, reason: "canceled" });
    expect((await api(c.token).get<any>("/v1/friends/requests?direction=incoming")).body.items).toEqual([]);
    await makeFriends(a, b);
    const cb = await connectWs(b.token);
    expect((await api(a.token).del(`/v1/friends/@${b.handle}`)).status).toBe(204);
    expect((await cb.next("friend.removed")).data.user_id).toBe(a.user.id);
  });

  it("request lists by direction; 101st outgoing pending -> 422", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    const out = (await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` })).body.request;
    const inc = (await api(c.token).post<any>("/v1/friends/requests", { user: `@${a.handle}` })).body.request;
    expect((await api(a.token).get<any>("/v1/friends/requests?direction=outgoing")).body.items.map((x: any) => x.id)).toEqual([out.id]);
    expect((await api(a.token).get<any>("/v1/friends/requests?direction=incoming")).body.items.map((x: any) => x.id)).toEqual([inc.id]);
    await seedPendingRequestsSql(a, 99); // 99 users rows + pending friend_requests from a (one INSERT … SELECT FROM json_each)
    const d = await signup();
    expect((await api(a.token).post<any>("/v1/friends/requests", { user: `@${d.handle}` })).body.error).toMatchObject({
      code: "limit_exceeded",
      details: { what: "pending_friend_requests", limit: 100 },
    });
  });
});
