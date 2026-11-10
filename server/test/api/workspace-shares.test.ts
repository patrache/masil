// api.md 5.26 (v0.1.3 P2 Task 12): workspace share metadata — create / list / read / invite / remove / end, who may be
// invited, the live-share and member caps, the D1-counted 60-minute create window, workspace_share.* events and the
// workspace.invite alert template. The share content itself never reaches the server (docs/contract/workspace.md).
import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { api, connectWs, makeFriends, makeSpace, signup, type TestUser } from "../helpers";

const create = (o: TestUser, users: string[], title = "sam의 작업공간") =>
  api(o.token).post<any>("/v1/workspace-shares", { title, users });
const at = (u: TestUser) => `@${u.handle}`;

async function shareWith(o: TestUser, members: TestUser[]): Promise<any> {
  const r = await create(o, members.map(at));
  if (r.status !== 201) throw new Error(`share create failed: ${r.status} ${JSON.stringify(r.body)}`);
  return r.body.share;
}

describe("workspace shares: create", () => {
  it("creates a share for a friend and alerts with a template", async () => {
    const [o, f] = [await signup(), await signup()];
    await makeFriends(o, f);
    const ws = await connectWs(f.token);
    const r = await api(o.token).post<{ share: any }>("/v1/workspace-shares", { title: "sam의 작업공간", users: [`@${f.handle}`] });
    expect(r.status).toBe(201);
    expect(r.body.share).toMatchObject({ title: "sam의 작업공간", status: "live", ended_at: null, end_reason: null, owner: { id: o.user.id } });
    expect(r.body.share.members).toHaveLength(1);
    expect(r.body.share.members[0]).toMatchObject({ seat: 1, user: { id: f.user.id }, joined_at: null, left_at: null, max_level: null, verified: false });
    const inv = await ws.next("workspace_share.invited");
    expect(inv.data.share.id).toBe(r.body.share.id);
    const al = await ws.next("alert", (d) => d.alert.kind === "workspace_invite");
    expect(al.data.alert.template).toEqual({ id: "workspace.invite", params: { actor: o.user.display_name, share: "sam의 작업공간" } });
    expect(al.data.alert.target).toMatchObject({ type: "workspace_share", id: r.body.share.id });
    expect(al.data.alert.target).toEqual({
      type: "workspace_share",
      id: r.body.share.id,
      channel_id: null,
      message_id: null,
      thread_root_id: null,
      post_id: null,
      space_id: null,
    });
    expect(al.data.alert.title).toBe(`${o.user.display_name}님이 작업공간을 공유했어요`);
    expect(al.data.alert.body).toBe("sam의 작업공간");
  });

  it("seats follow the request order; a room co-member may be invited; a duplicate counts once", async () => {
    const o = await signup();
    const [a, b] = [await signup(), await signup()];
    await makeSpace(o, [a, b]);
    const r = await create(o, [at(b), at(a), at(b)]);
    expect(r.status).toBe(201);
    expect(r.body.share.members.map((m: any) => [m.seat, m.user.id])).toEqual([
      [1, b.user.id],
      [2, a.user.id],
    ]);
  });

  it("rejects a stranger and a blocked friend with 403 action_not_allowed", async () => {
    const [o, f, s, bl] = [await signup(), await signup(), await signup(), await signup()];
    await makeFriends(o, f);
    await makeFriends(o, bl);
    expect((await api(bl.token).put(`/v1/blocks/@${o.handle}`)).status).toBeLessThan(300);
    const r = await create(o, [at(f), at(s), at(bl)]);
    expect(r.status).toBe(403);
    expect(r.body.error.code).toBe("action_not_allowed");
    expect([...r.body.error.details.user_ids].sort()).toEqual([s.user.id, bl.user.id].sort());
    // nothing was written
    expect((await api(o.token).get<any>("/v1/workspace-shares")).body.items).toEqual([]);
  });

  it("rejects myself, a suspended friend and an unknown user", async () => {
    const [o, f] = [await signup(), await signup()];
    await makeFriends(o, f);
    const me = await create(o, ["@me"]);
    expect(me.status).toBe(403);
    expect(me.body.error.details.user_ids).toEqual([o.user.id]);
    await env.DB.prepare("UPDATE users SET suspended_at = ?1, suspended_until = NULL WHERE id = ?2").bind(Date.now(), f.user.id).run();
    const sus = await create(o, [at(f)]);
    expect(sus.status).toBe(403);
    expect(sus.body.error.details.user_ids).toEqual([f.user.id]);
    expect((await create(o, ["@nobody_here_x"])).status).toBe(404);
  });

  it("validates title and users", async () => {
    const [o, f] = [await signup(), await signup()];
    await makeFriends(o, f);
    const bad = async (body: unknown) => {
      const r = await api(o.token).post<any>("/v1/workspace-shares", body);
      expect(r.status).toBe(400);
      return r.body.error.details.fields;
    };
    expect(await bad({ title: "", users: [at(f)] })).toMatchObject({ title: expect.any(String) });
    expect(await bad({ title: "가".repeat(41), users: [at(f)] })).toMatchObject({ title: "too_long" });
    expect(await bad({ title: "x", users: [] })).toMatchObject({ users: "invalid" });
    expect(await bad({ title: "x", users: Array.from({ length: 9 }, () => at(f)) })).toMatchObject({ users: "invalid" });
    expect((await create(o, [at(f)], "가".repeat(40))).status).toBe(201);
  });

  it("caps live shares at 3 (422) and members at 8 (422)", async () => {
    const o = await signup();
    const ms: TestUser[] = [];
    for (let i = 0; i < 9; i++) ms.push(await signup());
    await makeSpace(o, ms);
    const first = await shareWith(o, ms.slice(0, 8));
    expect(first.members.map((m: any) => m.seat)).toEqual([1, 2, 3, 4, 5, 6, 7, 8]);
    const over = await api(o.token).post<any>(`/v1/workspace-shares/${first.id}/invites`, { users: [at(ms[8]!)] });
    expect(over.status).toBe(422);
    expect(over.body.error.details).toEqual({ what: "workspace_share_members", limit: 8 });
    await shareWith(o, [ms[0]!]);
    await shareWith(o, [ms[1]!]);
    const fourth = await create(o, [at(ms[2]!)]);
    expect(fourth.status).toBe(422);
    expect(fourth.body.error.details).toEqual({ what: "live_workspace_shares", limit: 3 });
    // ending one frees a live slot
    expect((await api(o.token).del(`/v1/workspace-shares/${first.id}`)).status).toBe(204);
    expect((await create(o, [at(ms[2]!)])).status).toBe(201);
  });

  it("counts the 60-minute window in D1: the 11th share in an hour is 429 RL_SHARE", async () => {
    const [o, f] = [await signup(), await signup()];
    await makeFriends(o, f);
    const t = Date.now() - 30 * 60_000;
    const stmts = Array.from({ length: 10 }, (_, i) =>
      env.DB.prepare(
        "INSERT INTO workspace_shares (id, owner_id, title, status, created_at, ended_at, end_reason) VALUES (?1, ?2, 'x', 'ended', ?3, ?3, 'ended')",
      ).bind(`old-${i}-${o.user.id}`, o.user.id, t + i),
    );
    await env.DB.batch(stmts);
    const r = await create(o, [at(f)]);
    expect(r.status).toBe(429);
    expect(r.body.error.details).toEqual({ limiter: "RL_SHARE", retry_after_sec: 60 });
    expect(r.headers.get("Retry-After")).toBe("60");
    // older than an hour no longer counts
    await env.DB.prepare("UPDATE workspace_shares SET created_at = created_at - 3600000 WHERE owner_id = ?1").bind(o.user.id).run();
    expect((await create(o, [at(f)])).status).toBe(201);
  });
});

describe("workspace shares: read", () => {
  it("hides shares from non-members with 404 and lets members read", async () => {
    const [o, f, s] = [await signup(), await signup(), await signup()];
    await makeFriends(o, f);
    const share = await shareWith(o, [f]);
    expect((await api(o.token).get<any>(`/v1/workspace-shares/${share.id}`)).body.share.id).toBe(share.id);
    expect((await api(f.token).get<any>(`/v1/workspace-shares/${share.id}`)).body.share.id).toBe(share.id);
    expect((await api(s.token).get<any>(`/v1/workspace-shares/${share.id}`)).status).toBe(404);
    expect((await api(s.token).get<any>(`/v1/workspace-shares/01JZZZZZZZZZZZZZZZZZZZZZZZ`)).status).toBe(404);
    expect((await api(s.token).post<any>(`/v1/workspace-shares/${share.id}/invites`, { users: [at(f)] })).status).toBe(404);
    expect((await api(s.token).del(`/v1/workspace-shares/${share.id}`)).status).toBe(404);
  });

  it("lists by role and status, newest first", async () => {
    const [o, f] = [await signup(), await signup()];
    await makeFriends(o, f);
    const a = await shareWith(o, [f]);
    const b = await shareWith(o, [f]);
    expect((await api(o.token).get<any>("/v1/workspace-shares")).body.items.map((x: any) => x.id)).toEqual([b.id, a.id]);
    expect((await api(o.token).get<any>("/v1/workspace-shares?role=invited")).body.items).toEqual([]);
    expect((await api(f.token).get<any>("/v1/workspace-shares?role=invited")).body.items.map((x: any) => x.id)).toEqual([b.id, a.id]);
    expect((await api(f.token).get<any>("/v1/workspace-shares")).body.items).toEqual([]);
    await api(o.token).del(`/v1/workspace-shares/${a.id}`);
    expect((await api(f.token).get<any>("/v1/workspace-shares?role=invited&status=live")).body.items.map((x: any) => x.id)).toEqual([b.id]);
    const ended = (await api(o.token).get<any>("/v1/workspace-shares?status=ended")).body.items;
    expect(ended).toHaveLength(1);
    expect(ended[0]).toMatchObject({ id: a.id, status: "ended", end_reason: "ended" });
    expect(ended[0].ended_at).not.toBeNull();
    const bad = await api(o.token).get<any>("/v1/workspace-shares?role=boss");
    expect(bad.status).toBe(400);
    expect(bad.body.error.details.fields).toMatchObject({ role: "invalid" });
  });
});

describe("workspace shares: invite, remove, end", () => {
  it("only the owner invites (member 403) and ending is idempotent", async () => {
    const [o, f, g] = [await signup(), await signup(), await signup()];
    await makeFriends(o, f);
    await makeFriends(o, g);
    await makeFriends(f, g);
    const share = await shareWith(o, [f]);
    const asMember = await api(f.token).post<any>(`/v1/workspace-shares/${share.id}/invites`, { users: [at(g)] });
    expect(asMember.status).toBe(403);
    expect(asMember.body.error.code).toBe("forbidden");
    const fw = await connectWs(f.token);
    const gw = await connectWs(g.token);
    const inv = await api(o.token).post<any>(`/v1/workspace-shares/${share.id}/invites`, { users: [at(f), at(g)] });
    expect(inv.status).toBe(200);
    expect(inv.body.share.members.map((m: any) => [m.seat, m.user.id])).toEqual([
      [1, f.user.id],
      [2, g.user.id],
    ]);
    expect((await gw.next("workspace_share.invited")).data.share.id).toBe(share.id);
    await gw.next("alert", (d) => d.alert.kind === "workspace_invite");
    expect((await fw.next("workspace_share.updated")).data.share.members).toHaveLength(2);
    expect((await api(f.token).del(`/v1/workspace-shares/${share.id}`)).status).toBe(403);
    expect((await api(o.token).del(`/v1/workspace-shares/${share.id}`)).status).toBe(204);
    const ended = await fw.next("workspace_share.ended");
    expect(ended.data).toEqual({ share_id: share.id, reason: "ended" });
    expect((await gw.next("workspace_share.ended")).data.share_id).toBe(share.id);
    expect((await api(o.token).del(`/v1/workspace-shares/${share.id}`)).status).toBe(204);
    const row = await env.DB.prepare("SELECT status, end_reason, ended_at FROM workspace_shares WHERE id = ?1").bind(share.id).first<any>();
    expect(row).toMatchObject({ status: "ended", end_reason: "ended" });
    const late = await api(o.token).post<any>(`/v1/workspace-shares/${share.id}/invites`, { users: [at(g)] });
    expect(late.status).toBe(409);
    expect(late.body.error.details).toEqual({ reason: "ended" });
  });

  it("kick sets removed_at and hides the share from the kicked user's invited list", async () => {
    const [o, f, g] = [await signup(), await signup(), await signup()];
    await makeFriends(o, f);
    await makeFriends(o, g);
    const share = await shareWith(o, [f, g]);
    const fw = await connectWs(f.token);
    expect((await api(g.token).del(`/v1/workspace-shares/${share.id}/participants/@${f.handle}`)).status).toBe(403);
    expect((await api(o.token).del(`/v1/workspace-shares/${share.id}/participants/@me`)).status).toBe(403);
    expect((await api(o.token).del(`/v1/workspace-shares/${share.id}/participants/@${f.handle}`)).status).toBe(204);
    const row = await env.DB.prepare("SELECT removed_at FROM workspace_share_members WHERE share_id = ?1 AND user_id = ?2").bind(share.id, f.user.id).first<any>();
    expect(row.removed_at).toBeGreaterThan(0);
    const upd = await fw.next("workspace_share.updated");
    expect(upd.data.share.members.map((m: any) => m.user.id)).toEqual([g.user.id]);
    expect((await api(f.token).get<any>("/v1/workspace-shares?role=invited")).body.items).toEqual([]);
    expect((await api(f.token).get<any>(`/v1/workspace-shares/${share.id}`)).status).toBe(404);
    expect((await api(o.token).get<any>(`/v1/workspace-shares/${share.id}`)).body.share.members.map((m: any) => m.seat)).toEqual([2]);
    // removing again: no longer a member
    expect((await api(o.token).del(`/v1/workspace-shares/${share.id}/participants/@${f.handle}`)).status).toBe(404);
    // a member leaves with @me
    expect((await api(g.token).del(`/v1/workspace-shares/${share.id}/participants/@me`)).status).toBe(204);
    expect((await api(o.token).get<any>(`/v1/workspace-shares/${share.id}`)).body.share.members).toEqual([]);
    // re-inviting a removed member brings them back on the same seat
    const back = await api(o.token).post<any>(`/v1/workspace-shares/${share.id}/invites`, { users: [at(f)] });
    expect(back.status).toBe(200);
    expect(back.body.share.members.map((m: any) => [m.seat, m.user.id])).toEqual([[1, f.user.id]]);
  });

  it("respects notify.workspace=false (no alert, event still sent)", async () => {
    const [o, f] = [await signup(), await signup()];
    await makeFriends(o, f);
    const p = await api(f.token).patch<any>("/v1/me/settings", { notify: { workspace: false } });
    expect(p.status).toBe(200);
    expect(p.body.settings.notify).toMatchObject({ workspace: false, dm: true });
    expect((await api(f.token).get<any>("/v1/me/settings")).body.settings.notify.workspace).toBe(false);
    const ws = await connectWs(f.token);
    const share = await shareWith(o, [f]);
    expect((await ws.next("workspace_share.invited")).data.share.id).toBe(share.id);
    // a later alert (a friend's DM) still arrives, so the missing workspace alert is not a timing accident
    const dm = await api(o.token).post<any>("/v1/dms", { users: [at(f)] });
    await api(o.token).post(`/v1/channels/${dm.body.channel.id}/messages`, { client_nonce: crypto.randomUUID(), body: "hi" });
    await ws.next("alert", (d) => d.alert.kind === "dm");
    expect(ws.frames.some((x) => x.type === "alert" && x.data.alert.kind === "workspace_invite")).toBe(false);
  });

  it("settings default to notify.workspace = true", async () => {
    const a = await signup();
    expect((await api(a.token).get<any>("/v1/me/settings")).body.settings.notify.workspace).toBe(true);
  });

  it("stores no path or file name: workspace tables have exactly the documented columns", async () => {
    const cols = async (t: string) => (await env.DB.prepare(`PRAGMA table_info(${t})`).all<{ name: string }>()).results.map((r) => r.name).sort();
    expect(await cols("workspace_shares")).toEqual(["created_at", "end_reason", "ended_at", "id", "owner_id", "status", "title"]);
    expect(await cols("workspace_share_members")).toEqual(
      ["invited_at", "joined_at", "left_at", "max_level", "removed_at", "seat", "share_id", "user_id", "verified"].sort(),
    );
  });
});
