import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { api, connectWs, makeFriends, signup, type TestUser } from "../helpers";

// ---- local fixtures (this file only) ----

/** Two friends and their 1:1 dm channel id. */
async function dmPair(): Promise<{ a: TestUser; b: TestUser; ch: string }> {
  const [a, b] = await Promise.all([signup(), signup()]);
  await makeFriends(a, b);
  const r = await api(a.token).post<any>("/v1/dms", { users: [`@${b.handle}`] });
  if (r.status !== 201) throw new Error(`dm create failed: ${r.status} ${JSON.stringify(r.body)}`);
  return { a, b, ch: r.body.channel.id };
}

describe("send / list / get", () => {
  it("send 201, duplicate nonce 200, event to both", async () => {
    const { a, b, ch } = await dmPair();
    const cb = await connectWs(b.token);
    const body = { client_nonce: "n-1", body: "  안녕 **세상**  " };
    const r1 = await api(a.token).post<any>(`/v1/channels/${ch}/messages`, body);
    expect(r1.status).toBe(201);
    expect(r1.body.message).toMatchObject({
      kind: "text",
      body: "안녕 **세상**",
      client_nonce: "n-1",
      thread: null,
      reactions: [],
      pinned: false,
      deleted: false,
      edited_at: null,
    });
    const r2 = await api(a.token).post<any>(`/v1/channels/${ch}/messages`, body);
    expect(r2.status).toBe(200);
    expect(r2.body.message.id).toBe(r1.body.message.id);
    expect((await cb.next("message.created")).data.message.id).toBe(r1.body.message.id);
    expect((await api(a.token).get<any>(`/v1/channels/${ch}`)).body.channel.last_message_id).toBe(r1.body.message.id);
  });

  it("validation: empty body, 4001 chars, emoji counted per code point, bad dice", async () => {
    const { a, ch } = await dmPair();
    const p = (b: any) => api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: crypto.randomUUID().slice(0, 8), ...b });
    expect((await p({ body: "   " })).body.error.details.fields.body).toBe("too_short");
    expect((await p({ body: "가".repeat(4001) })).body.error.details.fields.body).toBe("too_long");
    expect((await p({ body: "😀".repeat(4000) })).status).toBe(201);
    expect((await p({ kind: "roll", body: "2d1" })).body.error.details.fields.body).toBe("invalid");
    const roll = (await p({ kind: "roll", body: "2d6+1" })).body.message;
    expect(roll.roll).toMatchObject({ expr: "2d6+1", modifier: 1 });
    expect(roll.roll.rolls).toHaveLength(2);
    const poll = (await p({ kind: "poll", poll: { question: "점심?", options: ["국밥", "라멘"] } })).body.message;
    expect(poll.poll).toMatchObject({
      question: "점심?",
      multi: false,
      closed: false,
      my_votes: [],
      options: [
        { idx: 0, text: "국밥", vote_count: 0 },
        { idx: 1, text: "라멘", vote_count: 0 },
      ],
    });
  });

  it("pagination: latest, before, after, around; ascending; has_more flags", async () => {
    const { a, ch } = await dmPair();
    const ids: string[] = [];
    for (let i = 0; i < 12; i++) ids.push((await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: `p${i}`, body: `m${i}` })).body.message.id);
    const latest = (await api(a.token).get<any>(`/v1/channels/${ch}/messages?limit=5`)).body;
    expect(latest.items.map((m: any) => m.id)).toEqual(ids.slice(7));
    expect(latest).toMatchObject({ has_more_before: true, has_more_after: false });
    expect((await api(a.token).get<any>(`/v1/channels/${ch}/messages?before=${ids[7]}&limit=5`)).body.items.map((m: any) => m.id)).toEqual(ids.slice(2, 7));
    expect((await api(a.token).get<any>(`/v1/channels/${ch}/messages?after=${ids[9]}`)).body.items.map((m: any) => m.id)).toEqual(ids.slice(10));
    const ar = (await api(a.token).get<any>(`/v1/channels/${ch}/messages?around=${ids[5]}&limit=4`)).body.items.map((m: any) => m.id);
    expect(ar).toContain(ids[5]);
    expect(ar).toEqual([...ar].sort());
    expect((await api(a.token).get(`/v1/channels/${ch}/messages?before=${ids[1]}&after=${ids[0]}`)).status).toBe(400);
  });

  it("non-member gets 404; read-only dm send -> 403 read_only", async () => {
    const { a, b, ch } = await dmPair();
    const x = await signup();
    expect((await api(x.token).get(`/v1/channels/${ch}/messages`)).status).toBe(404);
    await api(a.token).del(`/v1/friends/@${b.handle}`);
    expect((await api(b.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: "z", body: "hi" })).body.error.code).toBe("read_only");
  });

  it("before/after flags, around split, deleted messages without replies are hidden", async () => {
    const { a, ch } = await dmPair();
    const ids: string[] = [];
    for (let i = 0; i < 6; i++) ids.push((await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: `q${i}`, body: `m${i}` })).body.message.id);
    const before = (await api(a.token).get<any>(`/v1/channels/${ch}/messages?before=${ids[3]}&limit=2`)).body;
    expect(before).toMatchObject({ has_more_before: true, has_more_after: true });
    expect(before.items.map((m: any) => m.id)).toEqual(ids.slice(1, 3));
    const after = (await api(a.token).get<any>(`/v1/channels/${ch}/messages?after=${ids[1]}&limit=2`)).body;
    expect(after).toMatchObject({ has_more_before: true, has_more_after: true });
    expect(after.items.map((m: any) => m.id)).toEqual(ids.slice(2, 4));
    const around = (await api(a.token).get<any>(`/v1/channels/${ch}/messages?around=${ids[3]}&limit=3`)).body;
    expect(around.items.map((m: any) => m.id)).toEqual(ids.slice(2, 5)); // ceil(3/2)=2 at/before + floor(3/2)=1 after
    expect(around).toMatchObject({ has_more_before: true, has_more_after: true });
    await env.DB.prepare("UPDATE messages SET deleted_at = ?2, body = '' WHERE id = ?1").bind(ids[4], Date.now()).run();
    await env.DB.prepare("UPDATE messages SET deleted_at = ?2, body = '', reply_count = 1 WHERE id = ?1").bind(ids[5], Date.now()).run();
    const items = (await api(a.token).get<any>(`/v1/channels/${ch}/messages`)).body.items;
    expect(items.map((m: any) => m.id)).not.toContain(ids[4]);
    expect(items.find((m: any) => m.id === ids[5])).toMatchObject({ deleted: true, body: "" });
    expect((await api(a.token).get(`/v1/channels/${ch}/messages?around=nope`)).status).toBe(400);
  });

  it("GET /messages/:id needs read access; sender's read pointer advances", async () => {
    const { a, b, ch } = await dmPair();
    const m = (await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: "g1", kind: "me", body: "waves" })).body.message;
    expect((await api(b.token).get<any>(`/v1/messages/${m.id}`)).body.message).toMatchObject({ id: m.id, kind: "me", body: "waves", channel_id: ch });
    expect((await api((await signup()).token).get(`/v1/messages/${m.id}`)).status).toBe(404);
    const mine = (await api(a.token).get<any>(`/v1/channels/${ch}`)).body.channel.my;
    expect(mine).toMatchObject({ last_read_message_id: m.id, unread_count: 0 });
    expect((await api(b.token).get<any>(`/v1/channels/${ch}`)).body.channel.my.unread_count).toBe(1);
  });

  it("poll needs poll input; body ignored", async () => {
    const { a, ch } = await dmPair();
    const r = await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: "pl0", kind: "poll", body: "" });
    expect(r.body.error.details.fields.poll).toBe("required");
    const bad = await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: "pl1", kind: "poll", poll: { question: "q", options: ["only"] } });
    expect(bad.status).toBe(400);
    const ok = await api(a.token).post<any>(`/v1/channels/${ch}/messages`, {
      client_nonce: "pl2",
      kind: "poll",
      body: "ignored",
      poll: { question: "q", options: ["x", "y", "z"], multi: true, duration_sec: 60 },
    });
    expect(ok.status).toBe(201);
    expect(ok.body.message).toMatchObject({ kind: "poll", body: "", poll: { context_type: "message", context_id: ok.body.message.id, multi: true, voter_count: 0 } });
    expect(ok.body.message.poll.closes_at).not.toBeNull();
  });
});

describe("system messages", () => {
  it("group dm member add/remove posts group_member_added/removed", async () => {
    const [o, x, y, z] = await Promise.all([signup(), signup(), signup(), signup()]);
    await Promise.all([makeFriends(o, x), makeFriends(o, y), makeFriends(o, z)]);
    const g = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`, `@${y.handle}`] })).body.channel;
    const cx = await connectWs(x.token);
    expect((await api(o.token).post(`/v1/channels/${g.id}/members`, { users: [`@${z.handle}`] })).status).toBe(200);
    const added = (await cx.next("message.created")).data.message;
    expect(added).toMatchObject({ kind: "system", body: "", client_nonce: null, system: { event: "group_member_added", actor_id: o.user.id, target_user_id: z.user.id } });
    expect((await api(o.token).del(`/v1/channels/${g.id}/members/@${y.handle}`)).status).toBe(204);
    expect((await cx.next("message.created")).data.message.system).toMatchObject({ event: "group_member_removed", actor_id: o.user.id, target_user_id: y.user.id });
    const items = (await api(x.token).get<any>(`/v1/channels/${g.id}/messages`)).body.items;
    expect(items.map((m: any) => m.system?.event)).toEqual(["group_member_added", "group_member_removed"]);
    expect((await api(x.token).get<any>(`/v1/channels/${g.id}`)).body.channel.my.unread_count).toBe(0);
  });
});
