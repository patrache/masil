import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { newId } from "../../src/lib/ids";
import { loadUnreadStates } from "../../src/lib/serialize";
import { api, connectWs, makeFriends, signup } from "../helpers";

// ---- local fixtures (this file only) ----

/** Inserts a message row (and bumps channels.last_message_id) straight into D1. */
async function seedMessage(channelId: string, authorId: string, opts: { kind?: string; root?: string; deleted?: boolean } = {}): Promise<string> {
  const id = newId();
  const t = Date.now();
  await env.DB.batch([
    env.DB.prepare("INSERT INTO messages (id, channel_id, author_id, kind, body, thread_root_id, created_at, deleted_at) VALUES (?1, ?2, ?3, ?4, 'x', ?5, ?6, ?7)").bind(
      id,
      channelId,
      authorId,
      opts.kind ?? "text",
      opts.root ?? null,
      t,
      opts.deleted ? t : null,
    ),
    env.DB.prepare("UPDATE channels SET last_message_id = ?2, last_message_at = ?3 WHERE id = ?1 AND (last_message_id IS NULL OR last_message_id < ?2)").bind(channelId, id, t),
  ]);
  return id;
}

async function seedMention(messageId: string, channelId: string, userId: string, inThread: boolean): Promise<void> {
  await env.DB.prepare("INSERT INTO message_mentions (message_id, user_id, channel_id, in_thread) VALUES (?1, ?2, ?3, ?4)").bind(messageId, userId, channelId, inThread ? 1 : 0).run();
}

describe("dms", () => {
  it("1:1 dm create is idempotent and requires can_dm", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    await makeFriends(a, b);
    const r1 = await api(a.token).post<any>("/v1/dms", { users: [`@${b.handle}`] });
    expect(r1.status).toBe(201);
    expect(r1.body.channel).toMatchObject({ kind: "dm", space_id: null, name: null, read_only: false, my: { notify: "all", unread_count: 0 } });
    expect(r1.body.channel.members.map((m: any) => m.id).sort()).toEqual([a.user.id, b.user.id].sort());
    const r2 = await api(b.token).post<any>("/v1/dms", { users: [`@${a.handle}`] });
    expect(r2.status).toBe(200);
    expect(r2.body.channel.id).toBe(r1.body.channel.id);
    expect((await api(a.token).post<any>("/v1/dms", { users: [`@${c.handle}`] })).body.error.code).toBe("dm_not_allowed");
  });

  it("group dm: 3..10, failing ids reported, channel.created to all", async () => {
    const me = await signup();
    const fs = await Promise.all(Array.from({ length: 9 }, () => signup()));
    for (const f of fs) await makeFriends(me, f);
    const c1 = await connectWs(fs[0].token);
    const stranger = await signup();
    const bad = await api(me.token).post<any>("/v1/dms", { users: [`@${fs[0].handle}`, `@${stranger.handle}`] });
    expect(bad.body.error).toMatchObject({ code: "dm_not_allowed", details: { user_ids: [stranger.user.id] } });
    const g = await api(me.token).post<any>("/v1/dms", { users: fs.map((f) => `@${f.handle}`), name: "점심팟" });
    expect(g.status).toBe(201);
    expect(g.body.channel).toMatchObject({ kind: "group_dm", name: "점심팟", owner_id: me.user.id, member_count: 10 });
    await c1.next("channel.created");
    const extra = await signup();
    await makeFriends(me, extra);
    expect((await api(me.token).post<any>(`/v1/channels/${g.body.channel.id}/members`, { users: [`@${extra.handle}`] })).body.error.details).toEqual({
      what: "group_dm_members",
      limit: 10,
    });
    c1.close();
  });

  it("group dm member removal: owner or self only; dm list hides blocked 1:1", async () => {
    const [o, x, y] = await Promise.all([signup(), signup(), signup()]);
    await makeFriends(o, x);
    await makeFriends(o, y);
    const g = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`, `@${y.handle}`] })).body.channel;
    expect((await api(x.token).del(`/v1/channels/${g.id}/members/@${y.handle}`)).status).toBe(403);
    const cy = await connectWs(y.token);
    expect((await api(y.token).del(`/v1/channels/${g.id}/members/@me`)).status).toBe(204);
    expect((await cy.next("channel.deleted")).data).toEqual({ channel_id: g.id, space_id: null, reason: "removed" });
    const d = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`] })).body.channel;
    await api(o.token).put(`/v1/blocks/@${x.handle}`);
    expect((await api(o.token).get<any>("/v1/dms")).body.items.map((c: any) => c.id)).not.toContain(d.id);
    expect((await api(x.token).get<any>(`/v1/channels/${d.id}`)).body.channel).toMatchObject({ read_only: true, read_only_reason: "not_eligible" });
    cy.close();
  });

  // ---- extra coverage beyond the plan's three cases ----

  it("group dm add: member_added to existing members, channel.created to the new one; already_member 409; dm/self 409/400", async () => {
    const [o, x, y, z] = await Promise.all([signup(), signup(), signup(), signup()]);
    await makeFriends(o, x);
    await makeFriends(o, y);
    await makeFriends(x, z);
    const g = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`, `@${y.handle}`] })).body.channel;
    expect(g.members.map((m: any) => m.id).sort()).toEqual([o.user.id, x.user.id, y.user.id].sort());
    // o is not z's friend: o cannot add z
    const denied = await api(o.token).post<any>(`/v1/channels/${g.id}/members`, { users: [`@${z.handle}`] });
    expect(denied.status).toBe(403);
    expect(denied.body.error).toMatchObject({ code: "dm_not_allowed", details: { user_ids: [z.user.id] } });
    const [co, cz] = await Promise.all([connectWs(o.token), connectWs(z.token)]);
    const added = await api(x.token).post<any>(`/v1/channels/${g.id}/members`, { users: [`@${z.handle}`] });
    expect(added.status).toBe(200);
    expect(added.body.channel).toMatchObject({ id: g.id, member_count: 4 });
    expect((await co.next("channel.member_added")).data).toMatchObject({ channel_id: g.id, user: { id: z.user.id } });
    expect((await cz.next("channel.created")).data.channel).toMatchObject({ id: g.id, kind: "group_dm", member_count: 4 });
    expect((await api(x.token).post<any>(`/v1/channels/${g.id}/members`, { users: [`@${y.handle}`] })).body.error.code).toBe("already_member");
    // a blocked relation with any existing member also refuses the add
    const w = await signup();
    await makeFriends(x, w);
    await api(y.token).put(`/v1/blocks/@${w.handle}`);
    const blk = await api(x.token).post<any>(`/v1/channels/${g.id}/members`, { users: [`@${w.handle}`] });
    expect(blk.body.error).toMatchObject({ code: "dm_not_allowed", details: { user_ids: [w.user.id] } });
    // 1:1 dm: adding/removing members is not supported
    const d = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`] })).body.channel;
    expect((await api(o.token).post<any>(`/v1/channels/${d.id}/members`, { users: [`@${y.handle}`] })).body.error).toMatchObject({
      code: "conflict",
      details: { reason: "not_supported" },
    });
    expect((await api(o.token).del<any>(`/v1/channels/${d.id}/members/@me`)).body.error.details).toEqual({ reason: "not_supported" });
    // owner removes someone else; a non-member sees 404
    const cy = await connectWs(y.token);
    expect((await api(o.token).del(`/v1/channels/${g.id}/members/@${z.handle}`)).status).toBe(204);
    expect((await cy.next("channel.member_removed")).data).toEqual({ channel_id: g.id, user_id: z.user.id });
    expect((await api(z.token).get(`/v1/channels/${g.id}`)).status).toBe(404);
    expect((await api(o.token).post<any>("/v1/dms", { users: ["@me"] })).status).toBe(400);
    for (const c of [co, cz, cy]) c.close();
  });

  it("GET /dms lists newest activity first and pages with a cursor", async () => {
    const [me, p, q, r] = await Promise.all([signup(), signup(), signup(), signup()]);
    for (const f of [p, q, r]) await makeFriends(me, f);
    const ids: string[] = [];
    for (const f of [p, q, r]) ids.push((await api(me.token).post<any>("/v1/dms", { users: [`@${f.handle}`] })).body.channel.id);
    const page1 = await api(me.token).get<any>("/v1/dms?limit=2");
    expect(page1.status).toBe(200);
    expect(page1.body.items.map((c: any) => c.id)).toEqual([ids[2], ids[1]]);
    expect(page1.body.next_cursor).not.toBeNull();
    const page2 = await api(me.token).get<any>(`/v1/dms?limit=2&cursor=${page1.body.next_cursor}`);
    expect(page2.body.items.map((c: any) => c.id)).toEqual([ids[0]]);
    expect(page2.body.next_cursor).toBeNull();
    expect(page1.body.items[0].members).toHaveLength(2);
  });

  it("unread state: top-level non-system by others after the pointer; mentions top-level only; capped at 999", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await makeFriends(a, b);
    const d = (await api(a.token).post<any>("/v1/dms", { users: [`@${b.handle}`] })).body.channel;
    const m1 = await seedMessage(d.id, b.user.id);
    const m2 = await seedMessage(d.id, b.user.id);
    await seedMessage(d.id, b.user.id, { kind: "system" });
    await seedMessage(d.id, a.user.id); // mine
    await seedMessage(d.id, b.user.id, { deleted: true });
    const reply = await seedMessage(d.id, b.user.id, { root: m1 });
    const m3 = await seedMessage(d.id, b.user.id);
    await seedMention(m2, d.id, a.user.id, false);
    await seedMention(reply, d.id, a.user.id, true);
    const before = (await api(a.token).get<any>(`/v1/channels/${d.id}`)).body.channel;
    expect(before.my).toEqual({ notify: "all", muted: false, last_read_message_id: null, unread_count: 3, mention_count: 1 });
    expect(before.last_message_id).not.toBeNull();
    await env.DB.prepare("INSERT INTO channel_reads (user_id, channel_id, last_read_message_id, updated_at) VALUES (?1, ?2, ?3, ?4)").bind(a.user.id, d.id, m2, Date.now()).run();
    const st = (await loadUnreadStates(env.DB, a.user.id, [d.id])).get(d.id);
    expect(st).toEqual({ channel_id: d.id, unread_count: 1, mention_count: 0, last_message_id: m3, last_read_message_id: m2 });

    // 1001 unread messages -> 999
    const prefix = newId().slice(0, 10);
    await env.DB.prepare(
      `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 1001)
       INSERT INTO messages (id, channel_id, author_id, kind, body, created_at)
       SELECT ?1 || printf('%016d', i), ?2, ?3, 'text', 'x', ?4 FROM n`,
    ).bind(prefix, d.id, b.user.id, Date.now()).run();
    await env.DB.prepare("UPDATE channels SET last_message_id = ?2 WHERE id = ?1").bind(d.id, `${prefix}${String(1001).padStart(16, "0")}`).run();
    await env.DB.prepare("DELETE FROM channel_reads WHERE user_id = ?1 AND channel_id = ?2").bind(a.user.id, d.id).run();
    expect((await loadUnreadStates(env.DB, a.user.id, [d.id])).get(d.id)?.unread_count).toBe(999);
    // a channel I am not in yields zero counts
    const c = await signup();
    expect((await loadUnreadStates(env.DB, c.user.id, [d.id])).get(d.id)).toMatchObject({ unread_count: 0, mention_count: 0 });
  });
});
