import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { newId } from "../../src/lib/ids";
import { joinSpace } from "../../src/services/membership";
import { api, connectWs, signup, type TestUser } from "../helpers";

// ---- local fixtures (this file only) ----

/** POST /v1/spaces by `owner`; throws unless 201. Returns the SpaceDetail. */
async function spaceViaApi(owner: TestUser, name = "테스트방"): Promise<any> {
  const r = await api(owner.token).post<any>("/v1/spaces", { name });
  if (r.status !== 201) throw new Error(`space create failed: ${r.status} ${JSON.stringify(r.body)}`);
  return r.body.space;
}

/** Adds `user` as a plain member straight in D1 (space_members row + spaces.member_count+1, as joinSpace does). */
async function joinSql(spaceId: string, user: TestUser): Promise<void> {
  const t = Date.now();
  await env.DB.batch([
    env.DB.prepare("INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?1, ?2, 'member', ?3)").bind(spaceId, user.user.id, t),
    env.DB.prepare("UPDATE spaces SET member_count = member_count + 1 WHERE id = ?1").bind(spaceId),
  ]);
}

/** A private text channel in `spaceId` whose channel_members are `members` (member_count kept in step). */
async function privateTextSql(spaceId: string, members: TestUser[]): Promise<string> {
  const id = newId();
  const t = Date.now();
  await env.DB.batch([
    env.DB.prepare(
      "INSERT INTO channels (id, kind, space_id, name, is_private, position, created_by, member_count, created_at, updated_at) VALUES (?1, 'text', ?2, ?3, 1, 1, ?4, ?5, ?6, ?6)",
    ).bind(id, spaceId, `p-${id.slice(-6).toLowerCase()}`, members[0]!.user.id, members.length, t),
    ...members.map((m) => env.DB.prepare("INSERT INTO channel_members (channel_id, user_id, added_by, joined_at) VALUES (?1, ?2, ?3, ?4)").bind(id, m.user.id, members[0]!.user.id, t)),
  ]);
  return id;
}

/** Short wait so anything still in flight would have arrived. */
const settle = (ms = 150) => new Promise<void>((r) => setTimeout(r, ms));

describe("spaces", () => {
  it("create space: owner, default channel 일반, space.joined", async () => {
    const a = await signup();
    const c = await connectWs(a.token);
    const r = await api(a.token).post<any>("/v1/spaces", { name: " 노바 개발방 ", icon_emoji: "🚀" });
    expect(r.status).toBe(201);
    expect(r.body.space).toMatchObject({
      name: "노바 개발방",
      icon_emoji: "🚀",
      description: "",
      owner_id: a.user.id,
      my_role: "owner",
      member_count: 1,
      channels: [{ kind: "text", name: "일반", is_private: false }],
    });
    expect(r.body.space.default_channel_id).toBe(r.body.space.channels[0].id);
    expect((await c.next("space.joined")).data.space).toMatchObject({ id: r.body.space.id, my_role: "owner" });
    expect((await api(a.token).post<any>("/v1/spaces", { name: "" })).body.error.details.fields.name).toBe("too_short");
    expect((await api(a.token).post<any>("/v1/spaces", { name: "x", icon_emoji: "ab" })).body.error.details.fields.icon_emoji).toBe("invalid");
  });

  it("roles: admin can kick members but not admins; owner cannot leave; transfer", async () => {
    const [o, ad, m1, m2] = await Promise.all([signup(), signup(), signup(), signup()]);
    const s = await spaceViaApi(o);
    for (const u of [ad, m1, m2]) await joinSql(s.id, u);
    expect((await api(o.token).patch<any>(`/v1/spaces/${s.id}/members/@${ad.handle}`, { role: "admin" })).body.member).toMatchObject({ user: { id: ad.user.id }, role: "admin" });
    expect((await api(m1.token).del(`/v1/spaces/${s.id}/members/@${m2.handle}`)).status).toBe(403);
    expect((await api(ad.token).del(`/v1/spaces/${s.id}/members/@${o.handle}`)).status).toBe(403);
    const cm2 = await connectWs(m2.token);
    expect((await api(ad.token).del(`/v1/spaces/${s.id}/members/@${m2.handle}?ban=true&reason=spam`)).status).toBe(204);
    expect((await cm2.next("space.removed")).data).toEqual({ space_id: s.id, reason: "banned" });
    expect((await cm2.next("space.member_left")).data).toEqual({ space_id: s.id, user_id: m2.user.id, reason: "banned" });
    expect((await api(ad.token).get<any>(`/v1/spaces/${s.id}/bans`)).body.items[0]).toMatchObject({ user: { id: m2.user.id }, banned_by: { id: ad.user.id }, reason: "spam" });
    expect((await api(m2.token).get(`/v1/spaces/${s.id}`)).status).toBe(404);
    expect((await api(o.token).del<any>(`/v1/spaces/${s.id}/members/@me`)).body.error.details.reason).toBe("owner_cannot_leave");
    const t = await api(o.token).post<any>(`/v1/spaces/${s.id}/transfer`, { user: `@${m1.handle}` });
    expect(t.status).toBe(200);
    expect(t.body.space).toMatchObject({ owner_id: m1.user.id, my_role: "admin" });
    expect((await api(o.token).get<any>(`/v1/spaces/${s.id}`)).body.space.my_role).toBe("admin");
    expect((await api(m1.token).get<any>(`/v1/spaces/${s.id}`)).body.space.my_role).toBe("owner");
    expect((await api(o.token).post(`/v1/spaces/${s.id}/transfer`, { user: `@${ad.handle}` })).status).toBe(403);
    const sp = await env.DB.prepare("SELECT member_count FROM spaces WHERE id = ?1").bind(s.id).first<any>();
    expect(sp.member_count).toBe(3);
  });

  it("leaving removes private channel memberships and their events stop", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const s = await spaceViaApi(o);
    await joinSql(s.id, m);
    const priv = await privateTextSql(s.id, [o, m]);
    const co = await connectWs(o.token);
    expect((await api(m.token).del(`/v1/spaces/${s.id}/members/@me`)).status).toBe(204);
    expect((await co.next("channel.member_removed")).data).toEqual({ channel_id: priv, user_id: m.user.id });
    expect((await co.next("space.member_left")).data).toEqual({ space_id: s.id, user_id: m.user.id, reason: "left" });
    expect((await env.DB.prepare("SELECT COUNT(*) n FROM channel_members WHERE channel_id=? AND user_id=?").bind(priv, m.user.id).first<any>()).n).toBe(0);
    expect((await env.DB.prepare("SELECT member_count FROM channels WHERE id=?").bind(priv).first<any>()).member_count).toBe(1);
    expect((await api(m.token).get(`/v1/channels/${priv}`)).status).toBe(404);
    const cm = await connectWs(m.token);
    await api(o.token).post(`/v1/channels/${priv}/messages`, { client_nonce: "x", body: "secret" });
    await settle();
    expect(cm.frames.some((f) => f.type === "message.created")).toBe(false);
    const general = (await api(o.token).get<any>(`/v1/channels/${s.default_channel_id}/messages`)).body.items.at(-1);
    expect(general.system).toMatchObject({ event: "member_left", actor_id: m.user.id });
    expect((await api(m.token).get<any>("/v1/spaces")).body.items.map((sp: any) => sp.id)).not.toContain(s.id);
  });

  it("delete space: members get space.deleted; channels 404 afterwards", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const s = await spaceViaApi(o);
    await joinSql(s.id, m);
    const cm = await connectWs(m.token);
    expect((await api(m.token).del(`/v1/spaces/${s.id}`)).status).toBe(403);
    expect((await api(o.token).del(`/v1/spaces/${s.id}`)).status).toBe(204);
    expect((await cm.next("space.deleted")).data).toEqual({ space_id: s.id });
    expect((await api(m.token).get(`/v1/channels/${s.default_channel_id}`)).status).toBe(404);
    expect((await api(o.token).get(`/v1/spaces/${s.id}`)).status).toBe(404);
  });

  it("list, patch + space.updated, member list order and q, unban", async () => {
    const [o, ad, m, x] = await Promise.all([signup({ displayName: "오너" }), signup({ displayName: "가관리" }), signup({ displayName: "나멤버" }), signup()]);
    const s = await spaceViaApi(o);
    for (const u of [ad, m, x]) await joinSql(s.id, u);
    await api(o.token).patch(`/v1/spaces/${s.id}/members/@${ad.handle}`, { role: "admin" });
    expect((await api(m.token).get<any>("/v1/spaces")).body.items.map((sp: any) => sp.id)).toContain(s.id);
    const cm = await connectWs(m.token);
    expect((await api(m.token).patch(`/v1/spaces/${s.id}`, { name: "x" })).status).toBe(403);
    expect((await api(ad.token).patch<any>(`/v1/spaces/${s.id}`, { name: "새 이름", icon_emoji: null })).body.space).toMatchObject({ name: "새 이름", my_role: "admin" });
    expect((await cm.next("space.updated")).data.space.name).toBe("새 이름");
    const members = (await api(m.token).get<any>(`/v1/spaces/${s.id}/members`)).body.items;
    expect(members.slice(0, 2).map((mm: any) => mm.role)).toEqual(["owner", "admin"]);
    expect(members).toHaveLength(4);
    expect((await api(m.token).get<any>(`/v1/spaces/${s.id}/members?q=${encodeURIComponent("나멤")}`)).body.items.map((mm: any) => mm.user.id)).toEqual([m.user.id]);
    await api(o.token).del(`/v1/spaces/${s.id}/members/@${x.handle}?ban=true`);
    expect((await api(m.token).del(`/v1/spaces/${s.id}/bans/@${x.handle}`)).status).toBe(403);
    expect((await api(ad.token).del(`/v1/spaces/${s.id}/bans/@${x.handle}`)).status).toBe(204);
    expect((await api(o.token).get<any>(`/v1/spaces/${s.id}/bans`)).body.items).toEqual([]);
  });

  it("member list: role filter and cursor paging; channels list", async () => {
    const owner = await signup();
    const s = await spaceViaApi(owner);
    const us = await Promise.all(Array.from({ length: 4 }, () => signup()));
    for (const u of us) await joinSql(s.id, u);
    const p1 = (await api(owner.token).get<any>(`/v1/spaces/${s.id}/members?role=member&limit=3`)).body;
    expect(p1.items).toHaveLength(3);
    expect(p1.next_cursor).not.toBeNull();
    const p2 = (await api(owner.token).get<any>(`/v1/spaces/${s.id}/members?role=member&limit=3&cursor=${p1.next_cursor}`)).body;
    expect(p2.items).toHaveLength(1);
    expect(p2.next_cursor).toBeNull();
    expect([...p1.items, ...p2.items].map((mm: any) => mm.user.id).sort()).toEqual(us.map((u) => u.user.id).sort());
    expect((await api(owner.token).get(`/v1/spaces/${s.id}/members?cursor=bad`)).status).toBe(400);
    const chs = (await api(us[0]!.token).get<any>(`/v1/spaces/${s.id}/channels`)).body.items;
    expect(chs.map((c: any) => c.id)).toEqual([s.default_channel_id]);
    expect((await api((await signup()).token).get(`/v1/spaces/${s.id}/channels`)).status).toBe(404);
  });

  it("joinSpace (no invite): space.joined, member_joined, system message; ban 403, member 409", async () => {
    const [o, u, b] = await Promise.all([signup(), signup(), signup()]);
    const s = await spaceViaApi(o);
    const rt = { env, waitUntil: () => {} };
    const [co, cu] = await Promise.all([connectWs(o.token), connectWs(u.token)]);
    const detail = await joinSpace(rt, s.id, u.user.id, {});
    expect(detail).toMatchObject({ id: s.id, my_role: "member", member_count: 2, channels: [{ id: s.default_channel_id }] });
    expect((await cu.next("space.joined")).data.space).toMatchObject({ id: s.id, my_role: "member" });
    expect((await co.next("space.member_joined")).data).toMatchObject({ space_id: s.id, member: { user: { id: u.user.id }, role: "member" } });
    await settle();
    expect(cu.frames.some((f) => f.type === "space.member_joined")).toBe(false);
    const last = (await api(o.token).get<any>(`/v1/channels/${s.default_channel_id}/messages`)).body.items.at(-1);
    expect(last.system).toMatchObject({ event: "member_joined", actor_id: u.user.id });
    await expect(joinSpace(rt, s.id, u.user.id, {})).rejects.toMatchObject({ status: 409, code: "already_member" });
    await joinSql(s.id, b);
    await api(o.token).del(`/v1/spaces/${s.id}/members/@${b.handle}?ban=true`);
    await expect(joinSpace(rt, s.id, b.user.id, {})).rejects.toMatchObject({ status: 403, code: "banned_from_space" });
    await env.DB.prepare("UPDATE spaces SET member_count = 5000 WHERE id = ?1").bind(s.id).run();
    await expect(joinSpace(rt, s.id, (await signup()).user.id, {})).rejects.toMatchObject({ status: 422, code: "limit_exceeded" });
  });
});
