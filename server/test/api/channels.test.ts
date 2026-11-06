import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { newId } from "../../src/lib/ids";
import { api, connectWs, makeFriends, makeSpace, signup, type TestUser } from "../helpers";

// ---- local fixtures (this file only) ----

/** `n` public text channels named `seed-<i>` straight in D1 (positions after the existing ones). */
async function seedChannelsSql(spaceId: string, creator: TestUser, n: number): Promise<void> {
  const t = Date.now();
  await env.DB.batch(
    Array.from({ length: n }, (_, i) =>
      env.DB.prepare(
        `INSERT INTO channels (id, kind, space_id, name, topic, is_private, position, created_by, created_at, updated_at)
         VALUES (?1, 'text', ?2, ?3, '', 0, ?4, ?5, ?6, ?6)`,
      ).bind(newId(t), spaceId, `seed-${i}`, i + 1, creator.user.id, t),
    ),
  );
}

/** Two friends and their 1:1 dm channel id. */
async function dmPair(): Promise<{ a: TestUser; b: TestUser; ch: string }> {
  const [a, b] = await Promise.all([signup(), signup()]);
  await makeFriends(a, b);
  const r = await api(a.token).post<any>("/v1/dms", { users: [`@${b.handle}`] });
  if (r.status !== 201) throw new Error(`dm create failed: ${r.status} ${JSON.stringify(r.body)}`);
  return { a, b, ch: r.body.channel.id };
}

/** Sends a text message as `u` in `ch` and returns it (throws unless 201). */
async function post(u: TestUser, ch: string, body: string): Promise<any> {
  const r = await api(u.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: crypto.randomUUID().slice(0, 12), body });
  if (r.status !== 201) throw new Error(`post failed: ${r.status} ${JSON.stringify(r.body)}`);
  return r.body.message;
}

/** `owner` makes an invite link for `spaceId` and `joiner` accepts it (throws unless both succeed). */
async function joinViaInvite(spaceId: string, owner: TestUser, joiner: TestUser): Promise<void> {
  const inv = await api(owner.token).post<any>(`/v1/spaces/${spaceId}/invites`, { max_uses: null });
  if (inv.status !== 201) throw new Error(`invite create failed: ${inv.status} ${JSON.stringify(inv.body)}`);
  const r = await api(joiner.token).post(`/v1/invites/${inv.body.invite.code}/accept`);
  if (r.status !== 200) throw new Error(`invite accept failed: ${r.status} ${JSON.stringify(r.body)}`);
}

describe("POST /spaces/:space_id/channels", () => {
  it("admin creates public channel; member cannot; duplicate name incl. NFD -> 409", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { space } = await makeSpace(o, [m]);
    const cm = await connectWs(m.token);
    expect((await api(m.token).post(`/v1/spaces/${space.id}/channels`, { name: "잡담" })).status).toBe(403);
    const r = await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "개발", topic: "dev" });
    expect(r.status).toBe(201);
    expect((await cm.next("channel.created")).data.channel.name).toBe("개발");
    expect((await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "개발".normalize("NFD") })).body.error.code).toBe("already_exists");
    expect((await api(o.token).post(`/v1/spaces/${space.id}/channels`, { name: "Bad Name" })).status).toBe(400);
  });

  it("new public channel: shape, position after existing, channel_created system message, listed in order", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { space, generalChannelId } = await makeSpace(o, [m]);
    const r = await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "배포알림", topic: "  deploys  " });
    expect(r.status).toBe(201);
    expect(r.body.channel).toMatchObject({ kind: "text", space_id: space.id, name: "배포알림", topic: "deploys", is_private: false, members: null, member_count: 2 });
    expect(r.body.channel.position).toBeGreaterThan(0);
    const items = (await api(m.token).get<any>(`/v1/spaces/${space.id}/channels`)).body.items.map((c: any) => c.id);
    expect(items).toEqual([generalChannelId, r.body.channel.id]);
    const sys = (await api(m.token).get<any>(`/v1/channels/${r.body.channel.id}/messages`)).body.items.at(-1).system;
    expect(sys).toMatchObject({ event: "channel_created", actor_id: o.user.id, data: { name: "배포알림" } });
    expect((await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "a".repeat(33) })).body.error.details.fields).toEqual({ name: "invalid" });
    expect((await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "ok", topic: "x".repeat(251) })).body.error.details.fields).toEqual({ topic: "too_long" });
  });

  it("100 channels per space -> 422 channels_per_space", async () => {
    const o = await signup();
    const { space } = await makeSpace(o);
    await seedChannelsSql(space.id, o, 99); // + 일반 = 100
    const r = await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "넘침" });
    expect(r.status).toBe(422);
    expect(r.body.error.details).toEqual({ what: "channels_per_space", limit: 100 });
  });

  it("private channel visible only to its members (even owner must be member)", async () => {
    const [o, ad, m] = await Promise.all([signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [ad, m]);
    await api(o.token).patch(`/v1/spaces/${space.id}/members/@${ad.handle}`, { role: "admin" });
    const p = (await api(ad.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "비밀", is_private: true, members: [`@${m.handle}`] })).body.channel;
    const names = async (u: TestUser) => (await api(u.token).get<any>(`/v1/spaces/${space.id}/channels`)).body.items.map((c: any) => c.name);
    expect(await names(m)).toContain("비밀");
    expect(await names(o)).not.toContain("비밀");
    expect((await api(o.token).get(`/v1/channels/${p.id}`)).status).toBe(404);
    expect((await api(m.token).post<any>(`/v1/channels/${p.id}/members`, { users: [`@${o.handle}`] })).status).toBe(200); // member may add
    expect((await api(m.token).del(`/v1/channels/${p.id}/members/@${o.handle}`)).status).toBe(403); // but not remove
  });

  it("private channel: channel.created to its members only, members must be space members, member list and counts", async () => {
    const [o, m, n, outsider] = await Promise.all([signup(), signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [m, n]);
    expect((await api(o.token).post(`/v1/spaces/${space.id}/channels`, { name: "x1", is_private: true, members: [`@${outsider.handle}`] })).status).toBe(404);
    expect((await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "x2", members: [`@${m.handle}`] })).body.error.details.fields).toEqual({ members: "not_allowed" });
    const [cm, cn] = await Promise.all([connectWs(m.token), connectWs(n.token)]);
    const r = await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "x3", is_private: true, members: [`@${m.handle}`, `@${m.handle}`] });
    expect(r.status).toBe(201);
    expect(r.body.channel).toMatchObject({ is_private: true, member_count: 2, members: null });
    expect((await cm.next("channel.created")).data.channel.id).toBe(r.body.channel.id);
    // a later public channel reaches n; the private one never did
    const pub = (await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "x4" })).body.channel;
    expect((await cn.next("channel.created")).data.channel.id).toBe(pub.id);
    expect(cn.frames.some((f) => f.type === "channel.created" && f.data.channel.id === r.body.channel.id)).toBe(false);
    const members = (await api(m.token).get<any>(`/v1/channels/${r.body.channel.id}/members`)).body;
    expect(members.items.map((x: any) => x.user.id).sort()).toEqual([o.user.id, m.user.id].sort());
    expect(members.next_cursor).toBeNull();
    expect(typeof members.items[0].joined_at).toBe("string");
  });
});

describe("private text channel members", () => {
  it("add: 404 for non-space-member, 409 already_member, events; remove: admin anyone, member only @me", async () => {
    const [o, ad, m, n, outsider] = await Promise.all([signup(), signup(), signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [ad, m, n]);
    await api(o.token).patch(`/v1/spaces/${space.id}/members/@${ad.handle}`, { role: "admin" });
    const p = (await api(ad.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "운영", is_private: true, members: [`@${m.handle}`] })).body.channel;
    expect((await api(m.token).post(`/v1/channels/${p.id}/members`, { users: [`@${outsider.handle}`] })).status).toBe(404);
    expect((await api(m.token).post<any>(`/v1/channels/${p.id}/members`, { users: [`@${ad.handle}`] })).body.error.code).toBe("already_member");

    const [cad, cn] = await Promise.all([connectWs(ad.token), connectWs(n.token)]);
    const added = await api(m.token).post<any>(`/v1/channels/${p.id}/members`, { users: [`@${n.handle}`] });
    expect(added.status).toBe(200);
    expect(added.body.channel.member_count).toBe(3);
    expect((await cad.next("channel.member_added")).data).toMatchObject({ channel_id: p.id, user: { id: n.user.id } });
    expect((await cn.next("channel.created")).data.channel.id).toBe(p.id);
    expect((await api(n.token).get(`/v1/channels/${p.id}`)).status).toBe(200);

    expect((await api(n.token).del(`/v1/channels/${p.id}/members/@${m.handle}`)).status).toBe(403);
    expect((await api(ad.token).del(`/v1/channels/${p.id}/members/@${n.handle}`)).status).toBe(204);
    expect((await cn.next("channel.deleted")).data).toEqual({ channel_id: p.id, space_id: space.id, reason: "removed" });
    expect((await cad.next("channel.member_removed")).data).toEqual({ channel_id: p.id, user_id: n.user.id });
    expect((await api(n.token).get(`/v1/channels/${p.id}`)).status).toBe(404);
    expect((await api(m.token).del(`/v1/channels/${p.id}/members/@me`)).status).toBe(204);
    expect((await api(m.token).get(`/v1/channels/${p.id}`)).status).toBe(404);
    expect((await api(ad.token).get<any>(`/v1/channels/${p.id}`)).body.channel.member_count).toBe(1);
  });

  it("public text channel members cannot be added or removed (409 not_supported)", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { generalChannelId } = await makeSpace(o, [m]);
    expect((await api(o.token).post<any>(`/v1/channels/${generalChannelId}/members`, { users: [`@${m.handle}`] })).body.error.details).toEqual({ reason: "not_supported" });
    expect((await api(o.token).del<any>(`/v1/channels/${generalChannelId}/members/@${m.handle}`)).body.error.details).toEqual({ reason: "not_supported" });
  });
});

describe("PATCH / DELETE /channels/:channel_id, prefs", () => {
  it("rename -> channel.updated + system message; default channel undeletable; prefs", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { space, generalChannelId } = await makeSpace(o, [m]);
    const cm = await connectWs(m.token);
    const ch = (await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "a1" })).body.channel;
    await api(o.token).patch(`/v1/channels/${ch.id}`, { name: "a2" });
    expect((await cm.next("channel.updated")).data.channel.name).toBe("a2");
    expect((await api(o.token).get<any>(`/v1/channels/${ch.id}/messages`)).body.items.at(-1).system).toMatchObject({ event: "channel_renamed", data: { name: "a2" } });
    expect((await api(o.token).del<any>(`/v1/channels/${generalChannelId}`)).body.error.details.reason).toBe("default_channel");
    const pr = await api(m.token).put<any>(`/v1/channels/${ch.id}/prefs`, { notify: "all", muted: true });
    expect(pr.body.my).toMatchObject({ notify: "all", muted: true });
    await cm.next("channel.prefs_updated");
  });

  it("topic and position: member 403, topic system message, position reorders, name clash 409", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { space, generalChannelId } = await makeSpace(o, [m]);
    const ch = (await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "b1" })).body.channel;
    expect((await api(m.token).patch(`/v1/channels/${ch.id}`, { topic: "x" })).status).toBe(403);
    const r = await api(o.token).patch<any>(`/v1/channels/${ch.id}`, { topic: "새 주제" });
    expect(r.status).toBe(200);
    expect(r.body.channel.topic).toBe("새 주제");
    expect((await api(m.token).get<any>(`/v1/channels/${ch.id}/messages`)).body.items.at(-1).system).toMatchObject({ event: "channel_topic_changed", actor_id: o.user.id, data: { topic: "새 주제" } });
    expect((await api(o.token).patch<any>(`/v1/channels/${generalChannelId}`, { position: 5 })).body.channel.position).toBe(5);
    expect((await api(m.token).get<any>(`/v1/spaces/${space.id}/channels`)).body.items.map((c: any) => c.id)).toEqual([ch.id, generalChannelId]);
    expect((await api(o.token).patch<any>(`/v1/channels/${ch.id}`, { name: "일반".normalize("NFD") })).body.error.code).toBe("already_exists");
    expect((await api(o.token).patch<any>(`/v1/channels/${ch.id}`, { name: "Nope!" })).body.error.details.fields).toEqual({ name: "invalid" });
    expect((await api(o.token).patch<any>(`/v1/channels/${ch.id}`, { position: -1 })).status).toBe(400);
  });

  it("delete channel: members get channel.deleted{reason:'deleted'}; member list", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { space } = await makeSpace(o, [m]);
    const cm = await connectWs(m.token);
    const ch = (await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "tmp" })).body.channel;
    expect((await api(o.token).get<any>(`/v1/channels/${ch.id}/members`)).body.items.map((x: any) => x.user.id).sort()).toEqual([o.user.id, m.user.id].sort());
    expect((await api(m.token).del(`/v1/channels/${ch.id}`)).status).toBe(403);
    expect((await api(o.token).del(`/v1/channels/${ch.id}`)).status).toBe(204);
    expect((await cm.next("channel.deleted")).data).toEqual({ channel_id: ch.id, space_id: space.id, reason: "deleted" });
    expect((await api(m.token).get(`/v1/channels/${ch.id}`)).status).toBe(404);
    // the name is free again
    expect((await api(o.token).post(`/v1/spaces/${space.id}/channels`, { name: "tmp" })).status).toBe(201);
  });

  it("delete private channel: only its members get channel.deleted", async () => {
    const [o, m, n] = await Promise.all([signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [m, n]);
    const p = (await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "숨김", is_private: true, members: [`@${m.handle}`] })).body.channel;
    const [cm, cn] = await Promise.all([connectWs(m.token), connectWs(n.token)]);
    expect((await api(o.token).del(`/v1/channels/${p.id}`)).status).toBe(204);
    expect((await cm.next("channel.deleted")).data).toEqual({ channel_id: p.id, space_id: space.id, reason: "deleted" });
    const pub = (await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "다음" })).body.channel;
    await cn.next("channel.created", (d) => d.channel.id === pub.id);
    expect(cn.frames.some((f) => f.type === "channel.deleted")).toBe(false);
  });

  it("member list paginates with a cursor", async () => {
    const users = await Promise.all([signup(), signup(), signup(), signup()]);
    const [o, ...rest] = users;
    const { generalChannelId } = await makeSpace(o!, rest);
    const p1 = (await api(o!.token).get<any>(`/v1/channels/${generalChannelId}/members?limit=3`)).body;
    expect(p1.items).toHaveLength(3);
    expect(p1.next_cursor).not.toBeNull();
    const p2 = (await api(o!.token).get<any>(`/v1/channels/${generalChannelId}/members?limit=3&cursor=${p1.next_cursor}`)).body;
    expect(p2.items).toHaveLength(1);
    expect(p2.next_cursor).toBeNull();
    expect([...p1.items, ...p2.items].map((x: any) => x.user.id).sort()).toEqual(users.map((u) => u.user.id).sort());
    expect((await api(o!.token).get(`/v1/channels/${generalChannelId}/members?cursor=garbage`)).status).toBe(400);
  });

  it("prefs: defaults, partial update keeps the other field, invalid value 400, unreadable channel 404", async () => {
    const [o, m, x] = await Promise.all([signup(), signup(), signup()]);
    const { generalChannelId } = await makeSpace(o, [m]);
    expect((await api(m.token).get<any>(`/v1/channels/${generalChannelId}`)).body.channel.my).toMatchObject({ notify: "mentions", muted: false });
    expect((await api(m.token).put<any>(`/v1/channels/${generalChannelId}/prefs`, { muted: true })).body.my).toMatchObject({ notify: "mentions", muted: true });
    expect((await api(m.token).put<any>(`/v1/channels/${generalChannelId}/prefs`, { notify: "none" })).body.my).toMatchObject({ notify: "none", muted: true });
    expect((await api(m.token).get<any>(`/v1/channels/${generalChannelId}`)).body.channel.my).toMatchObject({ notify: "none", muted: true });
    expect((await api(o.token).get<any>(`/v1/channels/${generalChannelId}`)).body.channel.my).toMatchObject({ notify: "mentions", muted: false });
    expect((await api(m.token).put(`/v1/channels/${generalChannelId}/prefs`, { notify: "loud" })).status).toBe(400);
    expect((await api(x.token).put(`/v1/channels/${generalChannelId}/prefs`, { muted: true })).status).toBe(404);
  });

  it("group dm rename by any member -> channel.updated + group_renamed; 1:1 dm patch 409", async () => {
    const [o, x, y] = await Promise.all([signup(), signup(), signup()]);
    await makeFriends(o, x);
    await makeFriends(o, y);
    const g = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`, `@${y.handle}`] })).body.channel;
    const cy = await connectWs(y.token);
    expect((await api(x.token).patch<any>(`/v1/channels/${g.id}`, { name: "점심팟" })).body.channel.name).toBe("점심팟");
    expect((await cy.next("channel.updated")).data.channel.name).toBe("점심팟");
    expect((await api(y.token).get<any>(`/v1/channels/${g.id}/messages`)).body.items.at(-1).system).toMatchObject({ event: "group_renamed", actor_id: x.user.id, data: { name: "점심팟" } });
    expect((await api(x.token).get<any>(`/v1/channels/${g.id}/members`)).body.items).toHaveLength(3);
    expect((await api(x.token).patch(`/v1/channels/${g.id}`, { position: 2 })).status).toBe(400);
    expect((await api(x.token).del<any>(`/v1/channels/${g.id}`)).body.error.details).toEqual({ reason: "not_supported" });
    const d = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`] })).body.channel;
    expect((await api(o.token).patch(`/v1/channels/${d.id}`, { name: "x" })).status).toBe(409);
  });
});

describe("read state", () => {
  it("unread/mention counts top-level by others after pointer; baseline = join time; own send advances pointer", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { space, generalChannelId: g } = await makeSpace(o, []);
    await post(o, g, "before join");
    await joinViaInvite(space.id, o, m);
    const x1 = await post(o, g, "one");
    await post(o, g, `@${m.handle} two`);
    await api(o.token).post(`/v1/messages/${x1.id}/thread`, { client_nonce: "tr", body: "reply not counted" });
    const st = (await api(m.token).get<any>("/v1/me/unreads")).body.items.find((s: any) => s.channel_id === g);
    expect(st).toMatchObject({ unread_count: 2, mention_count: 1, last_read_message_id: null });
    const cm2 = await connectWs(m.token);
    const r = await api(m.token).put<any>(`/v1/channels/${g}/read`, { message_id: x1.id });
    expect(r.body.state).toMatchObject({ unread_count: 1, mention_count: 1, last_read_message_id: x1.id });
    await cm2.next("read_state.updated");
    // api.md 5.10: sending moves the author's own channel_reads to the new message -> nothing unread, channel drops out of /me/unreads
    const mine = await post(m, g, "mine");
    expect((await api(m.token).get<any>("/v1/me/unreads")).body.items.find((s: any) => s.channel_id === g)).toBeUndefined();
    expect((await api(m.token).get<any>(`/v1/channels/${g}`)).body.channel.my).toMatchObject({ unread_count: 0, mention_count: 0, last_read_message_id: mine.id });
  });

  it("Review Focus 5: pointer never moves back; foreign/thread ids rejected", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { generalChannelId: g } = await makeSpace(o, [m]);
    const [a1, a2] = [await post(o, g, "1"), await post(o, g, "2")];
    await api(m.token).put(`/v1/channels/${g}/read`, { message_id: a2.id });
    expect((await api(m.token).put<any>(`/v1/channels/${g}/read`, { message_id: a1.id })).body.state.last_read_message_id).toBe(a2.id);
    const reply = (await api(o.token).post<any>(`/v1/messages/${a1.id}/thread`, { client_nonce: "t", body: "r" })).body.message;
    expect((await api(m.token).put(`/v1/channels/${g}/read`, { message_id: reply.id })).status).toBe(400);
    const { a, ch } = await dmPair();
    const other = await post(a, ch, "elsewhere");
    expect((await api(m.token).put(`/v1/channels/${g}/read`, { message_id: other.id })).status).toBe(400);
  });
});
