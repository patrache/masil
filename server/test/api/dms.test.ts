import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { newId } from "../../src/lib/ids";
import { loadUnreadStates } from "../../src/lib/serialize";
import { addChannelMembers } from "../../src/services/membership";
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

/**
 * D2 (owner decision 2026-11-06): a group DM refused because of a block between two would-be members must not say who
 * blocked whom — 403 dm_not_allowed, details {}, and none of the block-linked ids anywhere in the response.
 */
function expectHiddenBlockDenial(res: { status: number; body: any }, hidden: string[]): void {
  expect(res.status).toBe(403);
  expect(res.body.error.code).toBe("dm_not_allowed");
  expect(res.body.error.details).toEqual({});
  const raw = JSON.stringify(res.body);
  for (const id of hidden) expect(raw).not.toContain(id);
}

/** The same check on an ApiError thrown by a service call. */
async function expectHiddenBlockRejection(p: Promise<unknown>, hidden: string[]): Promise<void> {
  const e: any = await p.then(
    () => null,
    (x: unknown) => x,
  );
  expect(e).toMatchObject({ status: 403, code: "dm_not_allowed" });
  expect(e.details).toEqual({});
  const raw = JSON.stringify({ message: e.message, details: e.details });
  for (const id of hidden) expect(raw).not.toContain(id);
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
    // seed the 9 friendships in one D1 batch (rows as the friends service writes them, user_a < user_b):
    // 9 sequential request+accept round-trips pushed this test past the 5s default under a busy pool.
    const t = Date.now();
    await env.DB.batch(
      fs.map((f) => {
        const [lo, hi] = me.user.id < f.user.id ? [me.user.id, f.user.id] : [f.user.id, me.user.id];
        return env.DB.prepare("INSERT INTO friendships (user_a, user_b, created_at) VALUES (?1, ?2, ?3)").bind(lo, hi, t);
      }),
    );
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
    expectHiddenBlockDenial(blk, [w.user.id, y.user.id]);
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

  // ---- pending-fixes item 8: a block between two participants (not just creator <-> participant) refuses the group ----

  /** group_dm channels created by `userId` (to prove a refused create left nothing behind). */
  async function groupDmsBy(userId: string): Promise<number> {
    const r = await env.DB.prepare("SELECT COUNT(*) AS n FROM channels WHERE kind = 'group_dm' AND created_by = ?1").bind(userId).first<{ n: number }>();
    return r?.n ?? 0;
  }

  it("group dm create: a block between two participants (either direction) -> 403 dm_not_allowed without ids, nothing created", async () => {
    const [c, a, b, d] = await Promise.all([signup(), signup(), signup(), signup()]);
    for (const f of [a, b, d]) await makeFriends(c, f);
    // a blocks b: c may DM both, but must not be able to put them in one room (b could then message a)
    expect((await api(a.token).put(`/v1/blocks/@${b.handle}`)).status).toBeLessThan(300);
    const r1 = await api(c.token).post<any>("/v1/dms", { users: [`@${a.handle}`, `@${b.handle}`, `@${d.handle}`] });
    expectHiddenBlockDenial(r1, [a.user.id, b.user.id]);
    // the order of the request does not matter, and the response is the same whichever way the block points
    const r2 = await api(c.token).post<any>("/v1/dms", { users: [`@${d.handle}`, `@${b.handle}`, `@${a.handle}`] });
    expectHiddenBlockDenial(r2, [a.user.id, b.user.id]);
    expect(r2.body).toEqual(r1.body);
    expect(await groupDmsBy(c.user.id)).toBe(0);
    // participants with no block between them still pass
    const ok = await api(c.token).post<any>("/v1/dms", { users: [`@${a.handle}`, `@${d.handle}`] });
    expect(ok.status).toBe(201);
    expect(ok.body.channel.members.map((m: any) => m.id).sort()).toEqual([c.user.id, a.user.id, d.user.id].sort());
    expect(await groupDmsBy(c.user.id)).toBe(1);
  });

  it("group dm create: a block from the second participant toward the first is refused too", async () => {
    const [c, a, b] = await Promise.all([signup(), signup(), signup()]);
    await makeFriends(c, a);
    await makeFriends(c, b);
    await api(b.token).put(`/v1/blocks/@${a.handle}`);
    const r = await api(c.token).post<any>("/v1/dms", { users: [`@${a.handle}`, `@${b.handle}`] });
    expectHiddenBlockDenial(r, [a.user.id, b.user.id]);
    expect(await groupDmsBy(c.user.id)).toBe(0);
  });

  // D2: can_dm(me, X) failures keep listing X (api.md 3.1, the rule 1:1 DMs already follow: a failed can_dm does not
  // say whether it was a block); a block pair between two other people is never named, even next to such a failure.
  it("group dm create: can_dm failures are still listed, a block pair beside them is not", async () => {
    const [c, a, b, s] = await Promise.all([signup(), signup(), signup(), signup()]);
    await makeFriends(c, a);
    await makeFriends(c, b);
    await api(a.token).put(`/v1/blocks/@${b.handle}`);
    const r = await api(c.token).post<any>("/v1/dms", { users: [`@${a.handle}`, `@${s.handle}`, `@${b.handle}`] });
    expect(r.status).toBe(403);
    expect(r.body.error).toEqual({ code: "dm_not_allowed", message: expect.any(String), details: { user_ids: [s.user.id] } });
    const raw = JSON.stringify(r.body);
    expect(raw).not.toContain(a.user.id);
    expect(raw).not.toContain(b.user.id);
    expect(await groupDmsBy(c.user.id)).toBe(0);
  });

  it("group dm add: can_dm failures are still listed, a block with a member beside them is not", async () => {
    const [o, x, y, w, s] = await Promise.all([signup(), signup(), signup(), signup(), signup()]);
    for (const f of [x, y, w]) await makeFriends(o, f);
    const g = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`, `@${y.handle}`] })).body.channel;
    await api(w.token).put(`/v1/blocks/@${y.handle}`);
    const r = await api(o.token).post<any>(`/v1/channels/${g.id}/members`, { users: [`@${w.handle}`, `@${s.handle}`] });
    expect(r.status).toBe(403);
    expect(r.body.error.details).toEqual({ user_ids: [s.user.id] });
    const raw = JSON.stringify(r.body);
    expect(raw).not.toContain(w.user.id);
    expect(raw).not.toContain(y.user.id);
    // without the stranger: refused without naming anyone
    expectHiddenBlockDenial(await api(o.token).post<any>(`/v1/channels/${g.id}/members`, { users: [`@${w.handle}`] }), [w.user.id, y.user.id]);
  });

  it("group dm add: a block between two of the users being added -> 403 without ids; each alone passes", async () => {
    const [o, x, y, p, q] = await Promise.all([signup(), signup(), signup(), signup(), signup()]);
    for (const f of [x, y, p, q]) await makeFriends(o, f);
    const g = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`, `@${y.handle}`] })).body.channel;
    await api(p.token).put(`/v1/blocks/@${q.handle}`);
    const both = await api(o.token).post<any>(`/v1/channels/${g.id}/members`, { users: [`@${q.handle}`, `@${p.handle}`] });
    expectHiddenBlockDenial(both, [q.user.id, p.user.id]);
    const cnt = await env.DB.prepare("SELECT COUNT(*) AS n FROM channel_members WHERE channel_id = ?1").bind(g.id).first<{ n: number }>();
    expect(cnt?.n).toBe(3);
    // p alone has no block with any current member -> added; then q is refused against p, now a member
    expect((await api(o.token).post<any>(`/v1/channels/${g.id}/members`, { users: [`@${p.handle}`] })).status).toBe(200);
    const qAfter = await api(o.token).post<any>(`/v1/channels/${g.id}/members`, { users: [`@${q.handle}`] });
    expectHiddenBlockDenial(qAfter, [q.user.id, p.user.id]);
  });

  // Pending item 8 (TOCTOU): the pairwise block pre-check is a plain read, so two concurrent adds of p and q (who block
  // each other) both pass it against the same members. The insert itself must re-check blocks against the members it
  // sees inside the batch, and the add that loses reports 403 dm_not_allowed.
  async function memberIds(channelId: string): Promise<string[]> {
    const rows = await env.DB.prepare("SELECT user_id FROM channel_members WHERE channel_id = ?1").bind(channelId).all<{ user_id: string }>();
    return rows.results.map((r) => r.user_id);
  }

  /** A Runtime whose D1 handle commits `userId` into `channelId` right before the first batch (a concurrent add). */
  function commitsMemberBeforeBatch(channelId: string, userId: string, addedBy: string) {
    let fired = false;
    const db = new Proxy(env.DB, {
      get(target, key) {
        if (key === "batch") {
          return async (stmts: D1PreparedStatement[]) => {
            if (!fired) {
              fired = true;
              await target
                .prepare("INSERT INTO channel_members (channel_id, user_id, added_by, joined_at) VALUES (?1, ?2, ?3, ?4)")
                .bind(channelId, userId, addedBy, Date.now())
                .run();
            }
            return target.batch(stmts);
          };
        }
        const v = Reflect.get(target, key);
        return typeof v === "function" ? v.bind(target) : v;
      },
    });
    return { rt: { env: { ...env, DB: db }, waitUntil: () => {} }, fired: () => fired };
  }

  it("group dm add: a block-linked member committed after the pre-check -> the insert refuses (403 dm_not_allowed), nothing written", async () => {
    const [o, x, y, p, q] = await Promise.all([signup(), signup(), signup(), signup(), signup()]);
    for (const f of [x, y, p, q]) await makeFriends(o, f);
    const g = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`, `@${y.handle}`] })).body.channel;
    await api(q.token).put(`/v1/blocks/@${p.handle}`);
    // The pre-check saw [o, x, y] only; p's add commits right before this add's batch.
    const { rt, fired } = commitsMemberBeforeBatch(g.id, p.user.id, o.user.id);
    await expectHiddenBlockRejection(addChannelMembers(rt, g.id, o.user.id, [q.user.id]), [q.user.id, p.user.id]);
    expect(fired()).toBe(true);
    expect((await memberIds(g.id)).sort()).toEqual([o.user.id, x.user.id, y.user.id, p.user.id].sort());
    const ch = await env.DB.prepare("SELECT member_count FROM channels WHERE id = ?1").bind(g.id).first<{ member_count: number }>();
    expect(ch?.member_count).toBe(4); // the recount in the same batch counts p (the concurrent add), not q
  });

  // The mirror of the test above: the block points from the concurrently committed member p toward q (the user being
  // added), so only the INSERT's "blocked IN new AND blocker IN room" clause can refuse it.
  it("group dm add: a member committed after the pre-check who blocked the user being added -> the insert refuses (403 dm_not_allowed)", async () => {
    const [o, x, y, p, q] = await Promise.all([signup(), signup(), signup(), signup(), signup()]);
    for (const f of [x, y, p, q]) await makeFriends(o, f);
    const g = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`, `@${y.handle}`] })).body.channel;
    await api(p.token).put(`/v1/blocks/@${q.handle}`);
    // The pre-check saw [o, x, y] only; p's add commits right before this add's batch.
    const { rt, fired } = commitsMemberBeforeBatch(g.id, p.user.id, o.user.id);
    await expectHiddenBlockRejection(addChannelMembers(rt, g.id, o.user.id, [q.user.id]), [q.user.id, p.user.id]);
    expect(fired()).toBe(true);
    expect((await memberIds(g.id)).sort()).toEqual([o.user.id, x.user.id, y.user.id, p.user.id].sort());
    const ch = await env.DB.prepare("SELECT member_count FROM channels WHERE id = ?1").bind(g.id).first<{ member_count: number }>();
    expect(ch?.member_count).toBe(4);
  });

  it("group dm add: concurrent adds of two users who block each other -> at most one gets in, the other 403 dm_not_allowed", async () => {
    const [x, y, p, q] = await Promise.all([signup(), signup(), signup(), signup()]);
    await api(p.token).put(`/v1/blocks/@${q.handle}`);
    for (let i = 0; i < 8; i++) {
      // A fresh adder per round: friend requests, POST /v1/dms and member adds all draw on the adder's RL_SOCIAL
      // (30 / 60s). One adder across rounds spends 4 + 3 per round and hits 429 at round 9; per round it is 7.
      const o = await signup();
      for (const f of [x, y, p, q]) await makeFriends(o, f);
      const g = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`, `@${y.handle}`] })).body.channel;
      const [rp, rq] = await Promise.all([
        api(o.token).post<any>(`/v1/channels/${g.id}/members`, { users: [`@${p.handle}`] }),
        api(o.token).post<any>(`/v1/channels/${g.id}/members`, { users: [`@${q.handle}`] }),
      ]);
      const ids = await memberIds(g.id);
      expect(ids.includes(p.user.id) && ids.includes(q.user.id)).toBe(false);
      expect([rp.status, rq.status].sort()).toEqual([200, 403]);
      const loser = rp.status === 403 ? rp : rq;
      expectHiddenBlockDenial(loser, [p.user.id, q.user.id]);
      expect(ids).toHaveLength(4);
    }
  }, 60_000);

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
