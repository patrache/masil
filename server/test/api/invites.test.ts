import { env, createExecutionContext, waitOnExecutionContext } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { createApp } from "../../src/app";
import { joinSpace } from "../../src/services/membership";
import { api, connectWs, makeFriends, makeSpace, signup, uniqueIp, type TestUser } from "../helpers";

// ---- local fixtures (this file only) ----

/** POST /v1/spaces by `owner`; throws unless 201. Returns the SpaceDetail. */
async function spaceViaApi(owner: TestUser, name = "초대방"): Promise<any> {
  const r = await api(owner.token).post<any>("/v1/spaces", { name });
  if (r.status !== 201) throw new Error(`space create failed: ${r.status} ${JSON.stringify(r.body)}`);
  return r.body.space;
}

describe("invites", () => {
  it("link: create (default 7d), preview, accept, system message, use_count", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const s = await spaceViaApi(o);
    const created = await api(o.token).post<any>(`/v1/spaces/${s.id}/invites`, { max_uses: 1 });
    expect(created.status).toBe(201);
    const inv = created.body.invite;
    expect(inv.code).toMatch(/^[A-HJ-NP-Za-km-np-z2-9]{8}$/);
    expect(inv).toMatchObject({ space: { id: s.id, name: "초대방" }, created_by: { id: o.user.id }, max_uses: 1, use_count: 0, revoked: false });
    expect(Date.parse(inv.expires_at) - Date.now()).toBeGreaterThan(604_790_000);
    expect((await api(m.token).get<any>(`/v1/invites/${inv.code}`)).body.invite).toMatchObject({
      code: inv.code,
      already_member: false,
      created_by: { id: o.user.id },
      space: { id: s.id, member_count: 1 },
    });
    expect((await api(o.token).get<any>(`/v1/invites/${inv.code}`)).body.invite.already_member).toBe(true);
    expect((await api(m.token).get(`/v1/invites/ABCDEFGH`)).status).toBe(404);
    const co = await connectWs(o.token);
    const cm = await connectWs(m.token);
    const j = await api(m.token).post<any>(`/v1/invites/${inv.code}/accept`);
    expect(j.status).toBe(200);
    expect(j.body.space).toMatchObject({ id: s.id, my_role: "member", member_count: 2 });
    expect((await co.next("space.member_joined")).data.member.user.id).toBe(m.user.id);
    expect((await cm.next("space.joined")).data.space).toMatchObject({ id: s.id, my_role: "member" });
    const general = (await api(o.token).get<any>(`/v1/channels/${s.default_channel_id}/messages`)).body.items.at(-1);
    expect(general.system).toMatchObject({ event: "member_joined", actor_id: m.user.id });
    const third = await signup();
    const ex = await api(third.token).post<any>(`/v1/invites/${inv.code}/accept`);
    expect(ex.status).toBe(410);
    expect(ex.body.error).toMatchObject({ code: "invite_invalid", details: { reason: "exhausted" } });
    expect((await api(m.token).post<any>(`/v1/invites/${inv.code}/accept`)).status).toBe(410);
    const row = await env.DB.prepare("SELECT use_count FROM space_invites WHERE code=?").bind(inv.code).first<any>();
    expect(row.use_count).toBe(1);
  });

  it("link options: max_uses / expires_in_sec validation and null = unlimited", async () => {
    const o = await signup();
    const s = await spaceViaApi(o);
    const forever = (await api(o.token).post<any>(`/v1/spaces/${s.id}/invites`, { max_uses: null, expires_in_sec: null })).body.invite;
    expect(forever).toMatchObject({ max_uses: null, expires_at: null });
    const short = (await api(o.token).post<any>(`/v1/spaces/${s.id}/invites`, { expires_in_sec: 300 })).body.invite;
    expect(Math.abs(Date.parse(short.expires_at) - Date.now() - 300_000)).toBeLessThan(10_000);
    for (const bad of [{ max_uses: 0 }, { max_uses: 1001 }, { max_uses: 1.5 }, { expires_in_sec: 299 }, { expires_in_sec: 2_592_001 }]) {
      expect((await api(o.token).post<any>(`/v1/spaces/${s.id}/invites`, bad)).status).toBe(400);
    }
  });

  it("revoked / expired / banned / already member", async () => {
    const [o, m, b] = await Promise.all([signup(), signup(), signup()]);
    const s = await spaceViaApi(o);
    const inv = (await api(o.token).post<any>(`/v1/spaces/${s.id}/invites`, {})).body.invite;
    expect(inv.max_uses).toBeNull();
    expect((await api(m.token).post(`/v1/invites/${inv.code}/accept`)).status).toBe(200);
    const again = await api(m.token).post<any>(`/v1/invites/${inv.code}/accept`);
    expect(again.status).toBe(409);
    expect(again.body.error.code).toBe("already_member");
    expect((await api(m.token).del(`/v1/invites/${inv.code}`)).status).toBe(403); // plain member cannot revoke
    await api(b.token).post(`/v1/invites/${inv.code}/accept`);
    await api(o.token).del(`/v1/spaces/${s.id}/members/@${b.handle}?ban=true`);
    const banned = await api(b.token).post<any>(`/v1/invites/${inv.code}/accept`);
    expect(banned.status).toBe(403);
    expect(banned.body.error.code).toBe("banned_from_space");
    expect((await api(o.token).del(`/v1/invites/${inv.code}`)).status).toBe(204);
    const revoked = await api((await signup()).token).get<any>(`/v1/invites/${inv.code}`);
    expect(revoked.status).toBe(410);
    expect(revoked.body.error.details.reason).toBe("revoked");
    expect((await api(o.token).get<any>(`/v1/spaces/${s.id}/invites`)).body.items.map((i: any) => i.code)).not.toContain(inv.code);
    await env.DB.prepare("UPDATE space_invites SET revoked_at=NULL, expires_at=1 WHERE code=?").bind(inv.code).run();
    expect((await api((await signup()).token).get<any>(`/v1/invites/${inv.code}`)).body.error.details.reason).toBe("expired");
    expect((await api((await signup()).token).post<any>(`/v1/invites/${inv.code}/accept`)).body.error.details.reason).toBe("expired");
  });

  it("direct invite: friend only, any role; accept emits invite_removed to both", async () => {
    const [o, m, f] = await Promise.all([signup(), signup(), signup()]);
    const { space, generalChannelId } = await makeSpace(o, [m]);
    expect(space).toMatchObject({ member_count: 2, my_role: "owner", default_channel_id: generalChannelId });
    expect((await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` })).body.error.code).toBe("action_not_allowed");
    await makeFriends(m, f);
    const cf = await connectWs(f.token);
    const cm = await connectWs(m.token);
    const di = await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` });
    expect(di.status).toBe(201);
    expect(di.body.invite).toMatchObject({ space: { id: space.id }, inviter: { id: m.user.id }, invitee: { id: f.user.id }, status: "pending" });
    expect((await cf.next("space.invite_received")).data.invite.id).toBe(di.body.invite.id);
    const alert = (await cf.next("alert")).data.alert;
    expect(alert).toMatchObject({ kind: "invite", target: { type: "space", id: space.id, space_id: space.id } });
    expect(alert.title).toContain(space.name);
    const n = (await cf.next("notification.created")).data.notification;
    expect(n).toMatchObject({ type: "space_invite", subject: { type: "space_direct_invite", id: di.body.invite.id }, context: { space_id: space.id, space_name: space.name } });
    const dup = await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` });
    expect(dup.status).toBe(409);
    expect(dup.body.error.code).toBe("already_exists");
    expect((await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${o.handle}` })).status).toBe(403); // not m's friend
    const mine = (await api(f.token).get<any>("/v1/me/direct-invites")).body.items;
    expect(mine).toHaveLength(1);
    expect(mine[0].id).toBe(di.body.invite.id);
    expect((await api(m.token).post(`/v1/direct-invites/${di.body.invite.id}/accept`)).status).toBe(404); // not the invitee
    const acc = await api(f.token).post<any>(`/v1/direct-invites/${di.body.invite.id}/accept`);
    expect(acc.status).toBe(200);
    expect(acc.body.space).toMatchObject({ id: space.id, my_role: "member", member_count: 3 });
    expect((await cm.next("space.invite_removed")).data).toEqual({ invite_id: di.body.invite.id, reason: "accepted" });
    expect((await cf.next("space.invite_removed")).data).toEqual({ invite_id: di.body.invite.id, reason: "accepted" });
    expect((await cm.next("space.member_joined")).data.member.user.id).toBe(f.user.id);
    expect((await api(f.token).post(`/v1/direct-invites/${di.body.invite.id}/accept`)).status).toBe(404); // no longer pending
    const member = await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` });
    expect(member.status).toBe(409);
    expect(member.body.error.code).toBe("already_member");
  });

  it("direct invite to a banned friend: 403 banned_from_space", async () => {
    const [o, f] = await Promise.all([signup(), signup()]);
    const { space } = await makeSpace(o, [f]);
    await api(o.token).del(`/v1/spaces/${space.id}/members/@${f.handle}?ban=true`);
    await makeFriends(o, f);
    const r = await api(o.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` });
    expect(r.status).toBe(403);
    expect(r.body.error.code).toBe("banned_from_space");
  });

  it("direct invite to a suspended friend: 403 action_not_allowed, no invite or notification (api.md 3.9)", async () => {
    const [o, f] = await Promise.all([signup(), signup()]);
    const { space } = await makeSpace(o, []);
    await makeFriends(o, f);
    await env.DB.prepare("UPDATE users SET suspended_at=?1, suspend_reason='spam' WHERE id=?2").bind(Date.now(), f.user.id).run();
    const r = await api(o.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` });
    expect(r.status).toBe(403);
    expect(r.body.error.code).toBe("action_not_allowed");
    const inv = await env.DB.prepare("SELECT COUNT(*) AS n FROM space_direct_invites WHERE invitee_id=?").bind(f.user.id).first<any>();
    expect(inv.n).toBe(0);
    const notif = await env.DB.prepare("SELECT COUNT(*) AS n FROM notifications WHERE user_id=? AND type='space_invite'").bind(f.user.id).first<any>();
    expect(notif.n).toBe(0);
    // a suspension whose suspended_until already passed counts as active again
    await env.DB.prepare("UPDATE users SET suspended_until=?1 WHERE id=?2").bind(Date.now() - 1000, f.user.id).run();
    expect((await api(o.token).post(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` })).status).toBe(201);
  });

  it("direct invite decline and cancel; invite list for admins", async () => {
    const [o, m, f, g] = await Promise.all([signup(), signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [m]);
    await makeFriends(m, f);
    await makeFriends(m, g);
    const cm = await connectWs(m.token);
    const d1 = (await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` })).body.invite;
    expect((await api(g.token).post(`/v1/direct-invites/${d1.id}/decline`)).status).toBe(404); // not the invitee
    expect((await api(f.token).post(`/v1/direct-invites/${d1.id}/decline`)).status).toBe(204);
    expect((await cm.next("space.invite_removed", (d) => d.invite_id === d1.id)).data.reason).toBe("declined");
    expect((await api(f.token).post(`/v1/direct-invites/${d1.id}/decline`)).status).toBe(404); // already declined
    const d2 = (await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${g.handle}` })).body.invite;
    const cg = await connectWs(g.token);
    expect((await api(f.token).del(`/v1/direct-invites/${d2.id}`)).status).toBe(404); // stranger to this invite
    const plain = await signup();
    await api(plain.token).post(`/v1/invites/${(await api(o.token).post<any>(`/v1/spaces/${space.id}/invites`, {})).body.invite.code}/accept`);
    expect((await api(plain.token).del(`/v1/direct-invites/${d2.id}`)).status).toBe(403); // plain member, not the inviter
    expect((await api(o.token).del(`/v1/direct-invites/${d2.id}`)).status).toBe(204); // owner may cancel
    expect((await cg.next("space.invite_removed")).data).toEqual({ invite_id: d2.id, reason: "canceled" });
    expect((await cm.next("space.invite_removed", (d) => d.invite_id === d2.id)).data.reason).toBe("canceled");
    expect((await api(g.token).get<any>("/v1/me/direct-invites")).body.items).toEqual([]);
    expect((await api(o.token).del(`/v1/direct-invites/${d2.id}`)).status).toBe(404); // already canceled
    const l = (await api(o.token).post<any>(`/v1/spaces/${space.id}/invites`, {})).body.invite;
    expect((await api(m.token).get(`/v1/spaces/${space.id}/invites`)).status).toBe(403);
    expect((await api(m.token).post(`/v1/spaces/${space.id}/invites`, {})).status).toBe(403);
    expect((await api(f.token).get(`/v1/spaces/${space.id}/invites`)).status).toBe(404);
    expect((await api(o.token).get<any>(`/v1/spaces/${space.id}/invites`)).body.items.map((i: any) => i.code)).toContain(l.code);
  });

  it("two concurrent accepts of a max_uses=1 link: exactly one joins", async () => {
    const [o, u1, u2] = await Promise.all([signup(), signup(), signup()]);
    const s = await spaceViaApi(o);
    const inv = (await api(o.token).post<any>(`/v1/spaces/${s.id}/invites`, { max_uses: 1 })).body.invite;
    const rs = await Promise.all([u1, u2].map((u) => api(u.token).post<any>(`/v1/invites/${inv.code}/accept`)));
    expect(rs.map((r) => r.status).sort()).toEqual([200, 410]);
    expect(rs.find((r) => r.status === 410)!.body.error.details.reason).toBe("exhausted");
    const row = await env.DB.prepare(
      "SELECT s.member_count, i.use_count, (SELECT COUNT(*) FROM space_members WHERE space_id=s.id) n FROM spaces s JOIN space_invites i ON i.space_id=s.id WHERE i.code=?",
    )
      .bind(inv.code)
      .first<any>();
    expect(row).toEqual({ member_count: 2, use_count: 1, n: 2 });
  });

  it("joinSpace race inside the batch: the losers write nothing (use_count, members, member_count stay consistent)", async () => {
    const o = await signup();
    const users = await Promise.all([signup(), signup(), signup()]);
    const s = await spaceViaApi(o);
    const inv = (await api(o.token).post<any>(`/v1/spaces/${s.id}/invites`, { max_uses: 1 })).body.invite;
    const rt = { env, waitUntil: () => {} };
    // In one isolate the pre-check reads of all three interleave before any batch commits, so the losers fail in the batch.
    const rs = await Promise.allSettled(users.map((u) => joinSpace(rt, s.id, u.user.id, { inviteCode: inv.code })));
    expect(rs.filter((r) => r.status === "fulfilled")).toHaveLength(1);
    for (const r of rs.filter((r) => r.status === "rejected")) {
      expect((r as PromiseRejectedResult).reason).toMatchObject({ status: 410, code: "invite_invalid", details: { reason: "exhausted" } });
    }
    const row = await env.DB.prepare(
      "SELECT s.member_count, i.use_count, (SELECT COUNT(*) FROM space_members WHERE space_id=s.id) n FROM spaces s JOIN space_invites i ON i.space_id=s.id WHERE i.code=?",
    )
      .bind(inv.code)
      .first<any>();
    expect(row).toEqual({ member_count: 2, use_count: 1, n: 2 });
  });

  it("joinSpace race on one direct invite: one join, the other 404", async () => {
    const [o, f] = await Promise.all([signup(), signup()]);
    const { space } = await makeSpace(o);
    await makeFriends(o, f);
    const di = (await api(o.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` })).body.invite;
    const rt = { env, waitUntil: () => {} };
    const rs = await Promise.allSettled([0, 1].map(() => joinSpace(rt, space.id, f.user.id, { directInviteId: di.id })));
    expect(rs.filter((r) => r.status === "fulfilled")).toHaveLength(1);
    const lost = rs.find((r) => r.status === "rejected") as PromiseRejectedResult;
    expect([404, 409]).toContain(lost.reason.status); // 404 = lost on the invite row; 409 = lost on the member PK
    const row = await env.DB.prepare("SELECT member_count, (SELECT COUNT(*) FROM space_members WHERE space_id=?1) n FROM spaces WHERE id=?1").bind(space.id).first<any>();
    expect(row).toEqual({ member_count: 2, n: 2 });
    expect((await env.DB.prepare("SELECT status FROM space_direct_invites WHERE id=?").bind(di.id).first<any>()).status).toBe("accepted");
  });
});

// pending item 9: an invite only counts while the person behind it could still issue it.
describe("invites whose inviter lost the right to invite", () => {
  /** A link's state as seen by a stranger: GET status + reason, accept status + reason. */
  async function linkState(code: string): Promise<{ get: number; reason?: string; accept: number; acceptReason?: string }> {
    const u = await signup();
    const g = await api(u.token).get<any>(`/v1/invites/${code}`);
    const a = await api(u.token).post<any>(`/v1/invites/${code}/accept`);
    return { get: g.status, reason: g.body?.error?.details?.reason, accept: a.status, acceptReason: a.body?.error?.details?.reason };
  }

  for (const how of ["kicked", "banned", "left"] as const) {
    it(`direct invite from a member who was ${how}: canceled with the removal, accept 404, nobody joins`, async () => {
      const [o, m, f] = await Promise.all([signup(), signup(), signup()]);
      const { space } = await makeSpace(o, [m]);
      await makeFriends(m, f);
      const di = (await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` })).body.invite;
      const cf = await connectWs(f.token);
      const path = how === "left" ? `/v1/spaces/${space.id}/members/@me` : `/v1/spaces/${space.id}/members/@${m.handle}${how === "banned" ? "?ban=true" : ""}`;
      expect((await api(how === "left" ? m.token : o.token).del(path)).status).toBe(204);
      expect((await cf.next("space.invite_removed")).data).toEqual({ invite_id: di.id, reason: "canceled" });
      expect((await env.DB.prepare("SELECT status FROM space_direct_invites WHERE id=?").bind(di.id).first<any>()).status).toBe("canceled");
      expect((await api(f.token).get<any>("/v1/me/direct-invites")).body.items).toEqual([]);
      expect((await api(f.token).post(`/v1/direct-invites/${di.id}/accept`)).status).toBe(404);
      const row = await env.DB.prepare("SELECT member_count, (SELECT COUNT(*) FROM space_members WHERE space_id=?1) n FROM spaces WHERE id=?1").bind(space.id).first<any>();
      expect(row).toEqual({ member_count: 1, n: 1 });
    });
  }

  // pending item 9-a (sibling of the link test below): the use-time "inviter is a member" check alone also refuses the
  // invite while the inviter is out, so it cannot tell whether removal really canceled it. Once the inviter is back in
  // the space (and still the invitee's friend) only the cancel written at removal keeps the old invite dead.
  for (const how of ["kicked", "banned", "left"] as const) {
    it(`direct invite from a member who was ${how}: stays canceled after the inviter rejoins`, async () => {
      const [o, m, f] = await Promise.all([signup(), signup(), signup()]);
      const { space } = await makeSpace(o, [m]);
      await makeFriends(m, f);
      const di = (await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` })).body.invite;
      const ownerLink = (await api(o.token).post<any>(`/v1/spaces/${space.id}/invites`, { max_uses: null, expires_in_sec: null })).body.invite;
      const path = how === "left" ? `/v1/spaces/${space.id}/members/@me` : `/v1/spaces/${space.id}/members/@${m.handle}${how === "banned" ? "?ban=true" : ""}`;
      expect((await api(how === "left" ? m.token : o.token).del(path)).status).toBe(204);
      if (how === "banned") expect((await api(o.token).del(`/v1/spaces/${space.id}/bans/@${m.handle}`)).status).toBe(204);
      expect((await api(m.token).post(`/v1/invites/${ownerLink.code}/accept`)).status).toBe(200);
      expect((await api(f.token).get<any>("/v1/me/direct-invites")).body.items).toEqual([]);
      expect((await api(f.token).post(`/v1/direct-invites/${di.id}/accept`)).status).toBe(404);
      expect((await env.DB.prepare("SELECT status FROM space_direct_invites WHERE id=?").bind(di.id).first<any>()).status).toBe("canceled");
      expect((await env.DB.prepare("SELECT COUNT(*) AS n FROM space_members WHERE space_id=?1 AND user_id=?2").bind(space.id, f.user.id).first<any>()).n).toBe(0);
      // a fresh invite from the rejoined member still works: only the old one is dead
      const fresh = await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` });
      expect(fresh.status).toBe(201);
      expect((await api(f.token).post(`/v1/direct-invites/${fresh.body.invite.id}/accept`)).status).toBe(200);
    });
  }

  it("direct invite still pending although its inviter is gone (no cancel ran): accept 404, invite untouched", async () => {
    const [o, m, f] = await Promise.all([signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [m]);
    await makeFriends(m, f);
    const di = (await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` })).body.invite;
    // the inviter's membership disappears without going through removeFromSpace (rows from before the fix, a future path)
    await env.DB.prepare("DELETE FROM space_members WHERE space_id=?1 AND user_id=?2").bind(space.id, m.user.id).run();
    expect((await api(f.token).post(`/v1/direct-invites/${di.id}/accept`)).status).toBe(404);
    expect((await env.DB.prepare("SELECT status FROM space_direct_invites WHERE id=?").bind(di.id).first<any>()).status).toBe("pending");
    expect((await env.DB.prepare("SELECT COUNT(*) AS n FROM space_members WHERE space_id=?1 AND user_id=?2").bind(space.id, f.user.id).first<any>()).n).toBe(0);
  });

  it("direct invite: the inviter leaves between the pre-check and the batch -> the conditional UPDATE refuses it", async () => {
    const [o, m, f] = await Promise.all([signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [m]);
    await makeFriends(m, f);
    const di = (await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` })).body.invite;
    // A DB whose batch() first drops the inviter's membership (no cancel): only the in-batch EXISTS can stop the join.
    const db = env.DB;
    const racing = new Proxy(db, {
      get(target, prop) {
        if (prop === "batch") {
          return async (stmts: D1PreparedStatement[]) => {
            await target.prepare("DELETE FROM space_members WHERE space_id=?1 AND user_id=?2").bind(space.id, m.user.id).run();
            return target.batch(stmts);
          };
        }
        const v = Reflect.get(target, prop, target);
        return typeof v === "function" ? v.bind(target) : v;
      },
    });
    const rt = { env: { ...env, DB: racing } as typeof env, waitUntil: () => {} };
    await expect(joinSpace(rt, space.id, f.user.id, { directInviteId: di.id })).rejects.toMatchObject({ status: 404 });
    expect((await env.DB.prepare("SELECT status FROM space_direct_invites WHERE id=?").bind(di.id).first<any>()).status).toBe("pending");
    expect((await env.DB.prepare("SELECT COUNT(*) AS n FROM space_members WHERE space_id=?1 AND user_id=?2").bind(space.id, f.user.id).first<any>()).n).toBe(0);
  });

  it("removing one member cancels only their own pending invites in that space", async () => {
    const [o, m, k, f, g, h] = await Promise.all([signup(), signup(), signup(), signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [m, k]);
    const { space: other } = await makeSpace(o, [k]);
    await Promise.all([makeFriends(m, f), makeFriends(k, g), makeFriends(k, h)]);
    const fromM = (await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` })).body.invite;
    const fromK = (await api(k.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${g.handle}` })).body.invite;
    const fromKElsewhere = (await api(k.token).post<any>(`/v1/spaces/${other.id}/direct-invites`, { user: `@${h.handle}` })).body.invite;
    expect((await api(o.token).del(`/v1/spaces/${space.id}/members/@${k.handle}`)).status).toBe(204);
    expect((await api(g.token).post(`/v1/direct-invites/${fromK.id}/accept`)).status).toBe(404);
    expect((await api(f.token).post(`/v1/direct-invites/${fromM.id}/accept`)).status).toBe(200); // m is still a member
    expect((await api(h.token).post(`/v1/direct-invites/${fromKElsewhere.id}/accept`)).status).toBe(200); // k is still in the other space
  });

  for (const how of ["kicked", "banned", "left"] as const) {
    it(`invite link of an admin who was ${how}: revoked with the removal`, async () => {
      const [o, a] = await Promise.all([signup(), signup()]);
      const { space } = await makeSpace(o, [a]);
      expect((await api(o.token).patch(`/v1/spaces/${space.id}/members/@${a.handle}`, { role: "admin" })).status).toBe(200);
      const link = (await api(a.token).post<any>(`/v1/spaces/${space.id}/invites`, {})).body.invite;
      const ownerLink = (await api(o.token).post<any>(`/v1/spaces/${space.id}/invites`, {})).body.invite;
      const path = how === "left" ? `/v1/spaces/${space.id}/members/@me` : `/v1/spaces/${space.id}/members/@${a.handle}${how === "banned" ? "?ban=true" : ""}`;
      expect((await api(how === "left" ? a.token : o.token).del(path)).status).toBe(204);
      expect(await linkState(link.code)).toEqual({ get: 410, reason: "revoked", accept: 410, acceptReason: "revoked" });
      const listed = (await api(o.token).get<any>(`/v1/spaces/${space.id}/invites`)).body.items.map((i: any) => i.code);
      expect(listed).toContain(ownerLink.code); // makeSpace's own owner link is listed too
      expect(listed).not.toContain(link.code);
      expect((await linkState(ownerLink.code)).accept).toBe(200); // the owner's link is not touched
    });

    // The use-time issuer check alone also yields 410 'revoked' while the issuer is out, so the test above cannot tell
    // whether removal really revoked the row. This one pins the revoke itself: the row is marked at removal, and the
    // link stays dead after the issuer comes back and is made admin again (api.md 3.5: 다시 admin 이 돼도 되살아나지 않음).
    it(`invite link of an admin who was ${how}: revoked_at is set at removal and the link stays dead after rejoin + re-promotion`, async () => {
      const [o, a] = await Promise.all([signup(), signup()]);
      const { space } = await makeSpace(o, [a]);
      expect((await api(o.token).patch(`/v1/spaces/${space.id}/members/@${a.handle}`, { role: "admin" })).status).toBe(200);
      const link = (await api(a.token).post<any>(`/v1/spaces/${space.id}/invites`, { max_uses: null, expires_in_sec: null })).body.invite;
      expect(link).toMatchObject({ max_uses: null, expires_at: null });
      const ownerLink = (await api(o.token).post<any>(`/v1/spaces/${space.id}/invites`, { max_uses: null, expires_in_sec: null })).body.invite;
      const revokedAt = async (code: string) =>
        (await env.DB.prepare("SELECT revoked_at FROM space_invites WHERE code=?").bind(code).first<any>()).revoked_at;
      expect(await revokedAt(link.code)).toBeNull();
      const path = how === "left" ? `/v1/spaces/${space.id}/members/@me` : `/v1/spaces/${space.id}/members/@${a.handle}${how === "banned" ? "?ban=true" : ""}`;
      expect((await api(how === "left" ? a.token : o.token).del(path)).status).toBe(204);
      expect(await revokedAt(link.code)).not.toBeNull();
      expect(await revokedAt(ownerLink.code)).toBeNull();
      if (how === "banned") expect((await api(o.token).del(`/v1/spaces/${space.id}/bans/@${a.handle}`)).status).toBe(204);
      expect((await api(a.token).post(`/v1/invites/${ownerLink.code}/accept`)).status).toBe(200);
      expect((await api(o.token).patch(`/v1/spaces/${space.id}/members/@${a.handle}`, { role: "admin" })).status).toBe(200);
      expect(await linkState(link.code)).toEqual({ get: 410, reason: "revoked", accept: 410, acceptReason: "revoked" });
      expect(await revokedAt(link.code)).not.toBeNull();
    });
  }

  it("invite link of an admin demoted to member: revoked; links of admins who keep the role survive", async () => {
    const [o, a, b] = await Promise.all([signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [a, b]);
    for (const u of [a, b]) expect((await api(o.token).patch(`/v1/spaces/${space.id}/members/@${u.handle}`, { role: "admin" })).status).toBe(200);
    const la = (await api(a.token).post<any>(`/v1/spaces/${space.id}/invites`, {})).body.invite;
    const lb = (await api(b.token).post<any>(`/v1/spaces/${space.id}/invites`, {})).body.invite;
    // re-setting the same role keeps the link
    expect((await api(o.token).patch(`/v1/spaces/${space.id}/members/@${a.handle}`, { role: "admin" })).status).toBe(200);
    expect((await linkState(la.code)).get).toBe(200);
    expect((await api(o.token).patch(`/v1/spaces/${space.id}/members/@${a.handle}`, { role: "member" })).status).toBe(200);
    expect(await linkState(la.code)).toEqual({ get: 410, reason: "revoked", accept: 410, acceptReason: "revoked" });
    expect((await linkState(lb.code)).accept).toBe(200);
    // promoted back: the old link stays revoked
    expect((await api(o.token).patch(`/v1/spaces/${space.id}/members/@${a.handle}`, { role: "admin" })).status).toBe(200);
    expect((await linkState(la.code)).get).toBe(410);
  });

  it("ownership transfer: the previous owner (now admin) keeps their links", async () => {
    const [o, n] = await Promise.all([signup(), signup()]);
    const { space } = await makeSpace(o, [n]);
    const lo = (await api(o.token).post<any>(`/v1/spaces/${space.id}/invites`, {})).body.invite;
    expect((await api(o.token).post(`/v1/spaces/${space.id}/transfer`, { user: `@${n.handle}` })).status).toBe(200);
    expect((await linkState(lo.code)).accept).toBe(200);
  });
});

// pending item 9 (follow-up): the issuer's right is checked where the invite is created and every time it is used, not
// only when the issuer is removed or demoted. The races below interpose between the route's role check and its write.
describe("invites are gated on the issuer's right at creation and at use", () => {
  /**
   * env.DB, except that the first bound statement whose SQL matches `match` runs `cb` before (or after) it executes
   * through first/run/all. Everything else passes through untouched.
   */
  function hookDb(match: RegExp, when: "before" | "after", cb: () => Promise<void>): D1Database {
    const db = env.DB;
    let fired = false;
    const fire = async () => {
      if (fired) return;
      fired = true;
      await cb();
    };
    const pass = <T extends object>(o: T, p: string | symbol) => {
      const v = Reflect.get(o, p, o);
      return typeof v === "function" ? v.bind(o) : v;
    };
    const wrapBound = (bound: D1PreparedStatement): D1PreparedStatement =>
      new Proxy(bound, {
        get(b, q) {
          if (q === "first" || q === "run" || q === "all") {
            return async (...a: unknown[]) => {
              if (when === "before") await fire();
              const out = await (b[q] as (...x: unknown[]) => Promise<unknown>)(...a);
              if (when === "after") await fire();
              return out;
            };
          }
          return pass(b, q);
        },
      });
    return new Proxy(db, {
      get(target, prop) {
        if (prop === "prepare") {
          return (sql: string) => {
            const s = target.prepare(sql);
            if (fired || !match.test(sql)) return s;
            return new Proxy(s, {
              get(st, p) {
                if (p === "bind") return (...args: unknown[]) => wrapBound(st.bind(...args));
                return pass(st, p);
              },
            });
          };
        }
        return pass(target, prop);
      },
    });
  }

  /** One request through a fresh app whose env.DB is `db`; the background work is awaited. */
  async function fetchWith(db: D1Database, token: string, method: string, path: string, json: unknown): Promise<{ status: number; body: any }> {
    const ctx = createExecutionContext();
    const res = await createApp().fetch(
      new Request(`http://masil.test${path}`, {
        method,
        headers: {
          "User-Agent": "masil/0.1.0 (test; test)",
          "CF-Connecting-IP": uniqueIp(),
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify(json),
      }),
      { ...env, DB: db } as typeof env,
      ctx,
    );
    await waitOnExecutionContext(ctx);
    const text = await res.text();
    return { status: res.status, body: text === "" ? null : JSON.parse(text) };
  }

  async function linkState(code: string): Promise<{ get: number; reason?: string; accept: number; acceptReason?: string }> {
    const u = await signup();
    const g = await api(u.token).get<any>(`/v1/invites/${code}`);
    const a = await api(u.token).post<any>(`/v1/invites/${code}/accept`);
    return { get: g.status, reason: g.body?.error?.details?.reason, accept: a.status, acceptReason: a.body?.error?.details?.reason };
  }

  const linkCount = async (spaceId: string, userId: string) =>
    (await env.DB.prepare("SELECT COUNT(*) AS n FROM space_invites WHERE space_id=?1 AND created_by=?2").bind(spaceId, userId).first<any>()).n;

  for (const how of ["demoted", "kicked"] as const) {
    it(`link create: the admin is ${how} between the role check and the INSERT -> ${how === "demoted" ? 403 : 404}, no link`, async () => {
      const [o, a] = await Promise.all([signup(), signup()]);
      const { space } = await makeSpace(o, [a]);
      expect((await api(o.token).patch(`/v1/spaces/${space.id}/members/@${a.handle}`, { role: "admin" })).status).toBe(200);
      const db = hookDb(/INSERT INTO space_invites/, "before", async () => {
        const r =
          how === "demoted"
            ? await api(o.token).patch(`/v1/spaces/${space.id}/members/@${a.handle}`, { role: "member" })
            : await api(o.token).del(`/v1/spaces/${space.id}/members/@${a.handle}`);
        expect(r.status).toBe(how === "demoted" ? 200 : 204);
      });
      const r = await fetchWith(db, a.token, "POST", `/v1/spaces/${space.id}/invites`, { expires_in_sec: null });
      expect(r.status).toBe(how === "demoted" ? 403 : 404);
      expect(await linkCount(space.id, a.user.id)).toBe(0);
    });
  }

  it("link create: an admin who keeps the role still gets 201 through the gated INSERT", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const { space } = await makeSpace(o, [a]);
    expect((await api(o.token).patch(`/v1/spaces/${space.id}/members/@${a.handle}`, { role: "admin" })).status).toBe(200);
    const r = await api(a.token).post<any>(`/v1/spaces/${space.id}/invites`, { max_uses: 3, expires_in_sec: null });
    expect(r.status).toBe(201);
    expect(r.body.invite).toMatchObject({ max_uses: 3, use_count: 0, expires_at: null });
    expect((await linkState(r.body.invite.code)).accept).toBe(200);
  });

  for (const how of ["gone", "demoted"] as const) {
    it(`link whose creator is ${how} without the revoke running: preview and accept 410 revoked, not listed`, async () => {
      const [o, a] = await Promise.all([signup(), signup()]);
      const { space } = await makeSpace(o, [a]);
      expect((await api(o.token).patch(`/v1/spaces/${space.id}/members/@${a.handle}`, { role: "admin" })).status).toBe(200);
      const link = (await api(a.token).post<any>(`/v1/spaces/${space.id}/invites`, {})).body.invite;
      // the creator's right disappears without going through removeFromSpace / PATCH (rows from before the fix, a future path)
      const sql =
        how === "gone"
          ? "DELETE FROM space_members WHERE space_id=?1 AND user_id=?2"
          : "UPDATE space_members SET role='member' WHERE space_id=?1 AND user_id=?2";
      await env.DB.prepare(sql).bind(space.id, a.user.id).run();
      expect(await linkState(link.code)).toEqual({ get: 410, reason: "revoked", accept: 410, acceptReason: "revoked" });
      const row = await env.DB.prepare("SELECT use_count, revoked_at FROM space_invites WHERE code=?").bind(link.code).first<any>();
      expect(row).toEqual({ use_count: 0, revoked_at: null });
      const listed = (await api(o.token).get<any>(`/v1/spaces/${space.id}/invites`)).body.items.map((i: any) => i.code);
      expect(listed).not.toContain(link.code);
      expect(listed.length).toBeGreaterThan(0); // makeSpace's owner link is still there
    });
  }

  it("link accept: the creator is demoted between the pre-check and the batch -> the conditional UPDATE refuses it", async () => {
    const [o, a, s] = await Promise.all([signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [a]);
    expect((await api(o.token).patch(`/v1/spaces/${space.id}/members/@${a.handle}`, { role: "admin" })).status).toBe(200);
    const link = (await api(a.token).post<any>(`/v1/spaces/${space.id}/invites`, {})).body.invite;
    const db = env.DB;
    const racing = new Proxy(db, {
      get(target, prop) {
        if (prop === "batch") {
          return async (stmts: D1PreparedStatement[]) => {
            await target.prepare("UPDATE space_members SET role='member' WHERE space_id=?1 AND user_id=?2").bind(space.id, a.user.id).run();
            return target.batch(stmts);
          };
        }
        const v = Reflect.get(target, prop, target);
        return typeof v === "function" ? v.bind(target) : v;
      },
    });
    const rt = { env: { ...env, DB: racing } as typeof env, waitUntil: () => {} };
    await expect(joinSpace(rt, space.id, s.user.id, { inviteCode: link.code })).rejects.toMatchObject({
      status: 410,
      code: "invite_invalid",
      details: { reason: "revoked" },
    });
    const row = await env.DB.prepare(
      "SELECT i.use_count, s.member_count, (SELECT COUNT(*) FROM space_members WHERE space_id=s.id) n FROM space_invites i JOIN spaces s ON s.id=i.space_id WHERE i.code=?",
    )
      .bind(link.code)
      .first<any>();
    expect(row).toEqual({ use_count: 0, member_count: 2, n: 2 });
  });

  it("direct invite create: the inviter is kicked between the role check and the INSERT -> 404, nothing reaches the invitee", async () => {
    const [o, m, f] = await Promise.all([signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [m]);
    await Promise.all([makeFriends(m, f), makeFriends(o, f)]);
    const cf = await connectWs(f.token);
    // after the route's own pre-check reads (ban / member / pending), before its write
    const db = hookDb(/AS pending/, "after", async () => {
      expect((await api(o.token).del(`/v1/spaces/${space.id}/members/@${m.handle}`)).status).toBe(204);
    });
    const r = await fetchWith(db, m.token, "POST", `/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` });
    expect(r.status).toBe(404);
    const counts = await env.DB.prepare(
      `SELECT (SELECT COUNT(*) FROM space_direct_invites WHERE space_id=?1 AND invitee_id=?2) AS invites,
              (SELECT COUNT(*) FROM notifications WHERE user_id=?2 AND type='space_invite') AS notes`,
    )
      .bind(space.id, f.user.id)
      .first<any>();
    expect(counts).toEqual({ invites: 0, notes: 0 });
    expect((await api(f.token).get<any>("/v1/me/direct-invites")).body.items).toEqual([]);
    // a current member can still invite the same person
    const fresh = await api(o.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` });
    expect(fresh.status).toBe(201);
    expect((await cf.next("space.invite_received")).data.invite.id).toBe(fresh.body.invite.id);
  });

  it("stale pending direct invite (inviter gone, no cancel ran): not listed; a member's fresh invite replaces it", async () => {
    const [o, m, f] = await Promise.all([signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [m]);
    await Promise.all([makeFriends(m, f), makeFriends(o, f)]);
    const stale = (await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` })).body.invite;
    await env.DB.prepare("DELETE FROM space_members WHERE space_id=?1 AND user_id=?2").bind(space.id, m.user.id).run();
    expect((await api(f.token).get<any>("/v1/me/direct-invites")).body.items).toEqual([]);
    const cf = await connectWs(f.token);
    const fresh = await api(o.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` });
    expect(fresh.status).toBe(201);
    expect((await cf.next("space.invite_removed")).data).toEqual({ invite_id: stale.id, reason: "canceled" });
    expect((await env.DB.prepare("SELECT status FROM space_direct_invites WHERE id=?").bind(stale.id).first<any>()).status).toBe("canceled");
    expect((await api(f.token).get<any>("/v1/me/direct-invites")).body.items.map((i: any) => i.id)).toEqual([fresh.body.invite.id]);
    expect((await api(f.token).post(`/v1/direct-invites/${fresh.body.invite.id}/accept`)).status).toBe(200);
  });

  it("a pending invite from a current member still blocks a second one (409 already_exists)", async () => {
    const [o, m, f] = await Promise.all([signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [m]);
    await Promise.all([makeFriends(m, f), makeFriends(o, f)]);
    expect((await api(m.token).post(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` })).status).toBe(201);
    const dup = await api(o.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` });
    expect(dup.status).toBe(409);
    expect(dup.body.error.code).toBe("already_exists");
  });
});

// D3 (owner decision 2026-11-06): ending a friendship cancels the pending direct invites between the two (both ways),
// and accepting a direct invite re-checks the friendship (and no block either way) inside the conditional UPDATE.
describe("direct invites end with the friendship (D3)", () => {
  async function inviteStatus(id: string): Promise<string> {
    return (await env.DB.prepare("SELECT status FROM space_direct_invites WHERE id=?").bind(id).first<any>()).status;
  }
  async function isMember(spaceId: string, u: TestUser): Promise<boolean> {
    return (await env.DB.prepare("SELECT COUNT(*) AS n FROM space_members WHERE space_id=?1 AND user_id=?2").bind(spaceId, u.user.id).first<any>()).n === 1;
  }
  async function invite(from: TestUser, spaceId: string, to: TestUser): Promise<any> {
    const r = await api(from.token).post<any>(`/v1/spaces/${spaceId}/direct-invites`, { user: `@${to.handle}` });
    if (r.status !== 201) throw new Error(`direct invite failed: ${r.status} ${JSON.stringify(r.body)}`);
    return r.body.invite;
  }
  /** Raw DELETE of the friendship row — no unfriend cleanup ran (rows from before D3, a future path that skips it). */
  async function dropFriendshipRaw(a: TestUser, b: TestUser): Promise<void> {
    await env.DB.prepare("DELETE FROM friendships WHERE user_a = MIN(?1, ?2) AND user_b = MAX(?1, ?2)").bind(a.user.id, b.user.id).run();
  }

  it("DELETE /v1/friends/{user} cancels pending direct invites both ways (and only those), with space.invite_removed to both", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    await makeFriends(a, b);
    await makeFriends(a, c);
    const [{ space: sa }, { space: sb }] = await Promise.all([makeSpace(a), makeSpace(b)]);
    const ab = await invite(a, sa.id, b); // a -> b
    const ba = await invite(b, sb.id, a); // b -> a
    const ac = await invite(a, sa.id, c); // a -> c: untouched
    const [ca, cb] = await Promise.all([connectWs(a.token), connectWs(b.token)]);

    expect((await api(a.token).del(`/v1/friends/@${b.handle}`)).status).toBe(204);

    expect(await inviteStatus(ab.id)).toBe("canceled");
    expect(await inviteStatus(ba.id)).toBe("canceled");
    expect(await inviteStatus(ac.id)).toBe("pending");
    for (const ws of [ca, cb]) {
      for (const id of [ab.id, ba.id]) {
        expect((await ws.next("space.invite_removed", (d) => d.invite_id === id)).data).toEqual({ invite_id: id, reason: "canceled" });
      }
      await ws.next("friend.removed");
    }
    expect(ca.frames.some((f) => f.type === "space.invite_removed" && f.data.invite_id === ac.id)).toBe(false);
    expect((await api(b.token).get<any>("/v1/me/direct-invites")).body.items).toEqual([]);
    expect((await api(a.token).get<any>("/v1/me/direct-invites")).body.items).toEqual([]);
    expect((await api(b.token).post(`/v1/direct-invites/${ab.id}/accept`)).status).toBe(404);
    expect((await api(a.token).post(`/v1/direct-invites/${ba.id}/accept`)).status).toBe(404);
    expect(await isMember(sa.id, b)).toBe(false);
    expect(await isMember(sb.id, a)).toBe(false);
    expect((await api(c.token).get<any>("/v1/me/direct-invites")).body.items.map((i: any) => i.id)).toEqual([ac.id]);
  });

  it("the friendship gone without the cleanup (invite still pending): not listed, accept 404, nothing written", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await makeFriends(a, b);
    const { space } = await makeSpace(a);
    const di = await invite(a, space.id, b);
    await dropFriendshipRaw(a, b);
    expect((await api(b.token).get<any>("/v1/me/direct-invites")).body.items).toEqual([]);
    expect((await api(b.token).post(`/v1/direct-invites/${di.id}/accept`)).status).toBe(404);
    expect(await inviteStatus(di.id)).toBe("pending");
    expect(await isMember(space.id, b)).toBe(false);
  });

  it("a block row in place (friendship row still there, no cleanup): not listed, accept 404, nothing written", async () => {
    for (const dir of ["invitee blocks inviter", "inviter blocks invitee"] as const) {
      const [a, b] = await Promise.all([signup(), signup()]);
      await makeFriends(a, b);
      const { space } = await makeSpace(a);
      const di = await invite(a, space.id, b);
      const [blocker, blocked] = dir === "invitee blocks inviter" ? [b, a] : [a, b];
      await env.DB.prepare("INSERT INTO blocks (blocker_id, blocked_id, created_at) VALUES (?1, ?2, ?3)").bind(blocker.user.id, blocked.user.id, Date.now()).run();
      expect((await api(b.token).get<any>("/v1/me/direct-invites")).body.items).toEqual([]);
      expect((await api(b.token).post(`/v1/direct-invites/${di.id}/accept`)).status).toBe(404);
      expect(await inviteStatus(di.id)).toBe("pending");
      expect(await isMember(space.id, b)).toBe(false);
    }
  });

  for (const how of ["friendship dropped", "block row inserted"] as const) {
    it(`accept: ${how} between the pre-check and the batch -> the conditional UPDATE refuses it`, async () => {
      const [a, b] = await Promise.all([signup(), signup()]);
      await makeFriends(a, b);
      const { space } = await makeSpace(a);
      const di = await invite(a, space.id, b);
      let fired = false; // once, before joinSpace's batch (later batches — system message — run untouched)
      const racing = new Proxy(env.DB, {
        get(target, prop) {
          if (prop === "batch") {
            return async (stmts: D1PreparedStatement[]) => {
              if (fired) return target.batch(stmts);
              fired = true;
              if (how === "friendship dropped") await dropFriendshipRaw(a, b);
              else await target.prepare("INSERT INTO blocks (blocker_id, blocked_id, created_at) VALUES (?1, ?2, ?3)").bind(b.user.id, a.user.id, Date.now()).run();
              return target.batch(stmts);
            };
          }
          const v = Reflect.get(target, prop, target);
          return typeof v === "function" ? v.bind(target) : v;
        },
      });
      const rt = { env: { ...env, DB: racing } as typeof env, waitUntil: () => {} };
      await expect(joinSpace(rt, space.id, b.user.id, { directInviteId: di.id })).rejects.toMatchObject({ status: 404 });
      expect(await inviteStatus(di.id)).toBe("pending");
      expect(await isMember(space.id, b)).toBe(false);
    });
  }

  it("a stale invite from an ex-friend (no cleanup ran) does not block a current friend's fresh invite, which replaces it", async () => {
    const [a, b, m] = await Promise.all([signup(), signup(), signup()]);
    await makeFriends(a, b);
    await makeFriends(m, b);
    const { space } = await makeSpace(a, [m]);
    const old = await invite(a, space.id, b);
    await dropFriendshipRaw(a, b);
    const cb = await connectWs(b.token);
    const fresh = await invite(m, space.id, b);
    expect(await inviteStatus(old.id)).toBe("canceled");
    expect((await cb.next("space.invite_removed", (d) => d.invite_id === old.id)).data).toEqual({ invite_id: old.id, reason: "canceled" });
    expect((await api(b.token).get<any>("/v1/me/direct-invites")).body.items.map((i: any) => i.id)).toEqual([fresh.id]);
    expect((await api(b.token).post(`/v1/direct-invites/${fresh.id}/accept`)).status).toBe(200);
  });

  it("friends still and no block: accept still works (200, member)", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await makeFriends(a, b);
    const { space } = await makeSpace(a);
    const di = await invite(a, space.id, b);
    expect((await api(b.token).post(`/v1/direct-invites/${di.id}/accept`)).status).toBe(200);
    expect(await isMember(space.id, b)).toBe(true);
  });
});
