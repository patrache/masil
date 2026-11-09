// docs/contract/workspace.md 4 (P2 Task 13): the WorkspaceHub relays ciphertext by the outer header only — level
// filter both ways, owner-only broadcast, ctl_* consumed (never forwarded), keyframe cache replay, kick / end close
// codes, the owner's 2-minute grace, per-connection frame limits, attention alerts, and who may connect.
import { env, runDurableObjectAlarm, runInDurableObject } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { FLAG_GROUP_START, HEADER_LEN, MAX_FRAME } from "../../src/do/workspace-frame";
import { api, connectWs, makeFriends, signup, waitFor, type TestUser } from "../helpers";
import { connectWorkspaceWs, frame, json, shareWsRaw, type WsBin } from "../workspace-helpers";

const hub = (shareId: string) => env.WORKSPACE_HUB.getByName(shareId);

/** Replaces the hub's clock: each call returns `clock()`. */
async function setClock(shareId: string, clock: () => number): Promise<void> {
  await runInDurableObject(hub(shareId), (inst) => {
    (inst as any).now = clock;
  });
}

async function shareOf(o: TestUser, members: TestUser[]): Promise<string> {
  for (const m of members) await makeFriends(o, m);
  const r = await api(o.token).post<any>("/v1/workspace-shares", { title: "ws", users: members.map((m) => `@${m.handle}`) });
  if (r.status !== 201) throw new Error(`share create: ${r.status} ${JSON.stringify(r.body)}`);
  return r.body.share.id;
}

/** owner + n members, all connected (owner first), with the owner's srv_peer joins consumed. */
async function room(n: number) {
  const owner = await signup();
  const members: TestUser[] = [];
  for (let i = 0; i < Math.max(n, 1); i++) members.push(await signup()); // a share needs one invitee; only n connect
  const shareId = await shareOf(owner, members);
  const o = await connectWorkspaceWs(owner.token, shareId);
  const ps: WsBin[] = [];
  for (let i = 0; i < n; i++) {
    ps.push(await connectWorkspaceWs(members[i]!.token, shareId));
    await o.next("srv_peer", (f) => json(f).seat === i + 1 && json(f).state === "joined");
  }
  return { owner, members, shareId, o, ps };
}

const levels = (o: WsBin, lv: Record<string, string>, verified: Record<string, boolean> = {}) =>
  o.send("ctl_levels", { from: 0, to: 254, payload: { levels: lv, verified } });

/** A marker the owner sends to `to` (To=n reaches a seat whatever its level) and waits for: everything sent before it
 *  to that socket has arrived. */
let markNo = 0;
async function sync(o: WsBin, p: WsBin, seat: number): Promise<void> {
  const tag = `m${++markNo}`;
  o.send("hs_decline", { to: seat, payload: tag });
  await p.next("hs_decline", (f) => new TextDecoder().decode(f.payload) === tag);
}

/** Same, the other way: participant -> owner with a kind every connected seat may send (hs_commit, MinLevel None). */
async function syncOwner(o: WsBin, p: WsBin, seat: number): Promise<void> {
  const tag = `o${++markNo}`;
  p.send("hs_commit", { from: seat, to: 0, payload: tag });
  await o.next("hs_commit", (f) => new TextDecoder().decode(f.payload) === tag);
}

describe("WorkspaceHub: connect", () => {
  it("srv_hello to the new socket, srv_peer joined / left to the owner, joined_at / left_at in D1", async () => {
    const { members, shareId, o, ps } = await room(1);
    const hello = json(ps[0]!.all("srv_hello")[0]!);
    expect(hello).toMatchObject({ share_id: shareId, seat: 1, max_frame: MAX_FRAME });
    expect(hello.seats).toEqual(expect.arrayContaining([{ seat: 0, user_id: expect.any(String) }, { seat: 1, user_id: members[0]!.user.id }]));
    expect(json(o.all("srv_hello")[0]!)).toMatchObject({ share_id: shareId, seat: 0 });
    const row = await env.DB.prepare("SELECT joined_at, left_at FROM workspace_share_members WHERE share_id = ?1").bind(shareId).first<any>();
    expect(row.joined_at).toBeGreaterThan(0);
    ps[0]!.close();
    expect(json(await o.next("srv_peer", (f) => json(f).state === "left"))).toEqual({ seat: 1, user_id: members[0]!.user.id, state: "left" });
    expect(await waitFor(async () => (await env.DB.prepare("SELECT left_at FROM workspace_share_members WHERE share_id = ?1").bind(shareId).first<any>()).left_at, (v) => v !== null)).toBeGreaterThan(0);
  });

  it("a second socket of the same user replaces the first (4102)", async () => {
    const { members, shareId, o, ps } = await room(1);
    const again = await connectWorkspaceWs(members[0]!.token, shareId);
    expect((await ps[0]!.closed).code).toBe(4102);
    // the owner sees the seat join again, never a "left" for the replaced socket
    await o.next("srv_peer", (f) => json(f).seat === 1 && json(f).state === "joined");
    await syncOwner(o, again, 1);
    expect(o.all("srv_peer").filter((f) => json(f).state === "left")).toHaveLength(0);
  });

  it("returns 404 for non-members, kicked members and blocked pairs", async () => {
    const owner = await signup();
    const [a, b, s] = [await signup(), await signup(), await signup()];
    const shareId = await shareOf(owner, [a, b]);
    expect((await shareWsRaw(s.token, shareId)).status).toBe(404);
    expect((await api(owner.token).del(`/v1/workspace-shares/${shareId}/participants/@${a.handle}`)).status).toBe(204);
    expect((await shareWsRaw(a.token, shareId)).status).toBe(404);
    expect((await api(b.token).put(`/v1/blocks/@${owner.handle}`)).status).toBeLessThan(300);
    expect((await shareWsRaw(b.token, shareId)).status).toBe(404);
    const ok = await connectWorkspaceWs(owner.token, shareId);
    ok.close();
    expect((await api(owner.token).del(`/v1/workspace-shares/${shareId}`)).status).toBe(204);
    expect((await shareWsRaw(owner.token, shareId)).status).toBe(404);
  });
});

describe("WorkspaceHub: routing", () => {
  it("relays owner broadcast only to seats with a level", async () => {
    const { o, ps } = await room(2);
    levels(o, { "1": "viewer" });
    o.send("term_out", { to: 255, payload: new Uint8Array([1, 2, 3]) });
    o.send("hs_owner_pub", { to: 2, payload: "pub-for-2" }); // To=n reaches a seat in the handshake (no level)
    const got = await ps[0]!.next("term_out");
    expect([...got.payload]).toEqual([1, 2, 3]);
    expect(got.header).toMatchObject({ from: 0, to: 255 });
    await ps[1]!.next("hs_owner_pub");
    await sync(o, ps[1]!, 2);
    expect(ps[1]!.all("term_out")).toHaveLength(0);
    // To=1 never reaches seat 2
    o.send("doc_state", { to: 1, payload: "only-1" });
    await ps[0]!.next("doc_state");
    await sync(o, ps[1]!, 2);
    expect(ps[1]!.all("doc_state")).toHaveLength(0);
  });

  it("forwards participant frames to the owner only when the level allows", async () => {
    const { o, ps } = await room(1);
    const p = ps[0]!;
    levels(o, { "1": "viewer" });
    await sync(o, p, 1);
    p.send("term_input", { from: 1, to: 0, payload: "x" });
    p.send("doc_open", { from: 1, to: 0, payload: "open" });
    p.send("doc_open", { from: 1, to: 255, payload: "not-to-owner" }); // participants may only send To=0
    await o.next("doc_open");
    await syncOwner(o, p, 1);
    expect(o.all("term_input")).toHaveLength(0);
    expect(o.all("doc_open")).toHaveLength(1);
    levels(o, { "1": "keyboard" });
    await sync(o, p, 1);
    p.send("term_input", { from: 1, to: 0, payload: "y" });
    expect(new TextDecoder().decode((await o.next("term_input")).payload)).toBe("y");
  });

  it("closes 4000 on a text frame and on a spoofed From", async () => {
    const { o, ps } = await room(2);
    ps[0]!.sendRaw("hello");
    expect((await ps[0]!.closed).code).toBe(4000);
    ps[1]!.send("hs_commit", { from: 0, to: 0, payload: "spoof" });
    expect((await ps[1]!.closed).code).toBe(4000);
    expect(o.all("hs_commit")).toHaveLength(0);
  });

  it("rejects frames larger than 256 KiB with 4000", async () => {
    const { o } = await room(0);
    o.sendRaw(frame("term_out", 0, 255, new Uint8Array(MAX_FRAME - HEADER_LEN + 1)));
    expect((await o.closed).code).toBe(4000);
  });

  it("never forwards ctl_* frames and stores levels in D1 metadata", async () => {
    const { members, shareId, o, ps } = await room(2);
    const watch = await connectWs(members[0]!.token);
    levels(o, { "1": "editor", "2": "viewer" }, { "1": true });
    ps[0]!.send("ctl_levels", { from: 1, to: 254, payload: { levels: { "1": "keyboard" } } }); // a participant: dropped
    await sync(o, ps[0]!, 1);
    await sync(o, ps[1]!, 2);
    for (const p of ps) expect(p.frames.filter((f) => f.kind.startsWith("ctl_"))).toHaveLength(0);
    const upd = await watch.next("workspace_share.updated", (d) => d.share.members.some((m: any) => m.max_level === "editor"));
    expect(upd.data.share.members.map((m: any) => [m.seat, m.max_level, m.verified])).toEqual([
      [1, "editor", true],
      [2, "viewer", false],
    ]);
    const rows = await env.DB.prepare("SELECT seat, max_level, verified FROM workspace_share_members WHERE share_id = ?1 ORDER BY seat").bind(shareId).all<any>();
    expect(rows.results).toEqual([
      { seat: 1, max_level: "editor", verified: 1 },
      { seat: 2, max_level: "viewer", verified: 0 },
    ]);
    // max_level keeps the highest granted
    levels(o, { "1": "viewer" }, { "1": true });
    await sync(o, ps[0]!, 1);
    expect((await env.DB.prepare("SELECT max_level FROM workspace_share_members WHERE share_id = ?1 AND seat = 1").bind(shareId).first<any>()).max_level).toBe("editor");
    // the participant's own keyboard level never took: term_input stays blocked
    ps[0]!.send("term_input", { from: 1, to: 0, payload: "x" });
    await syncOwner(o, ps[0]!, 1);
    expect(o.all("term_input")).toHaveLength(0);
  });

  it("replays the latest keyframe group to a newly levelled seat", async () => {
    const { o, ps } = await room(1);
    const p = ps[0]!;
    o.send("term_key", { to: 255, flags: FLAG_GROUP_START, payload: "old-key" });
    o.send("term_key", { to: 255, flags: FLAG_GROUP_START, payload: "key" });
    o.send("term_out", { to: 255, payload: "out1" });
    o.send("term_status", { to: 255, payload: "status" }); // not part of the cache
    levels(o, { "1": "viewer" });
    const k = await p.next("term_key");
    expect(new TextDecoder().decode(k.payload)).toBe("key");
    expect(k.header.flags & FLAG_GROUP_START).toBe(FLAG_GROUP_START);
    expect(new TextDecoder().decode((await p.next("term_out")).payload)).toBe("out1");
    await sync(o, p, 1);
    expect(p.all("term_key")).toHaveLength(1);
    expect(p.all("term_status")).toHaveLength(0);
    // levelled already: a second ctl_levels does not replay again
    levels(o, { "1": "editor" });
    await sync(o, p, 1);
    expect(p.all("term_key")).toHaveLength(1);
  });
});

describe("WorkspaceHub: kick, end, owner grace", () => {
  it("kick closes the kicked socket with 4101 and ending closes all with 4100", async () => {
    const { owner, members, shareId, o, ps } = await room(3);
    expect((await api(owner.token).del(`/v1/workspace-shares/${shareId}/participants/@${members[0]!.handle}`)).status).toBe(204);
    expect((await ps[0]!.closed).code).toBe(4101);
    // ctl_kick from the owner: same close, and the member is removed in D1
    o.send("ctl_kick", { to: 254, payload: { seat: 2 } });
    expect((await ps[1]!.closed).code).toBe(4101);
    expect(await waitFor(async () => (await env.DB.prepare("SELECT removed_at FROM workspace_share_members WHERE share_id = ?1 AND seat = 2").bind(shareId).first<any>()).removed_at, (v) => v !== null)).toBeGreaterThan(0);
    expect((await api(owner.token).del(`/v1/workspace-shares/${shareId}`)).status).toBe(204);
    expect((await ps[2]!.closed).code).toBe(4100);
    expect((await o.closed).code).toBe(4100);
  });

  it("ctl_end ends the share in D1 and closes everyone 4100", async () => {
    const { members, shareId, o, ps } = await room(1);
    const watch = await connectWs(members[0]!.token);
    o.send("ctl_end", { to: 254, payload: {} });
    expect((await ps[0]!.closed).code).toBe(4100);
    expect((await o.closed).code).toBe(4100);
    expect((await watch.next("workspace_share.ended")).data).toEqual({ share_id: shareId, reason: "ended" });
    expect(await env.DB.prepare("SELECT status, end_reason FROM workspace_shares WHERE id = ?1").bind(shareId).first()).toEqual({ status: "ended", end_reason: "ended" });
  });

  it("owner gone 2 minutes ends the share", async () => {
    const { members, shareId, o, ps } = await room(1);
    const watch = await connectWs(members[0]!.token);
    const t0 = Date.now();
    o.close();
    const at = await waitFor(() => runInDurableObject(hub(shareId), (_i, state) => state.storage.getAlarm()), (v) => v !== null);
    expect(at!).toBeGreaterThanOrEqual(t0 + 119_000);
    expect(at!).toBeLessThanOrEqual(Date.now() + 121_000);
    expect(await runDurableObjectAlarm(hub(shareId))).toBe(true);
    expect((await ps[0]!.closed).code).toBe(4100);
    expect(await env.DB.prepare("SELECT status, end_reason FROM workspace_shares WHERE id = ?1").bind(shareId).first()).toEqual({ status: "ended", end_reason: "timeout" });
    expect((await watch.next("workspace_share.ended")).data).toEqual({ share_id: shareId, reason: "timeout" });
  });

  it("owner reconnect within the grace cancels the end", async () => {
    const { owner, shareId, o } = await room(1);
    o.close();
    expect(await waitFor(() => runInDurableObject(hub(shareId), (_i, state) => state.storage.getAlarm()), (v) => v !== null)).not.toBeNull();
    const back = await connectWorkspaceWs(owner.token, shareId);
    expect(await runInDurableObject(hub(shareId), (_i, state) => state.storage.getAlarm())).toBeNull();
    expect(await runDurableObjectAlarm(hub(shareId))).toBe(false);
    expect((await env.DB.prepare("SELECT status FROM workspace_shares WHERE id = ?1").bind(shareId).first<any>()).status).toBe("live");
    back.close();
  });
});

describe("WorkspaceHub: sockets the hub closes itself", () => {
  const leftAt = (shareId: string, seat: number) =>
    env.DB.prepare("SELECT left_at FROM workspace_share_members WHERE share_id = ?1 AND seat = ?2").bind(shareId, seat).first<any>().then((r) => r.left_at);

  it("a participant closed 4000 leaves: the owner hears srv_peer left and left_at is set", async () => {
    const { members, shareId, o, ps } = await room(1);
    ps[0]!.sendRaw("hello");
    expect((await ps[0]!.closed).code).toBe(4000);
    expect(json(await o.next("srv_peer", (f) => json(f).state === "left"))).toEqual({ seat: 1, user_id: members[0]!.user.id, state: "left" });
    expect(await waitFor(() => leftAt(shareId, 1), (v) => v !== null)).toBeGreaterThan(0);
  });

  it("a participant closed 4008 leaves, and a reconnect is a fresh join", async () => {
    const { members, shareId, o, ps } = await room(1);
    const p = ps[0]!;
    levels(o, { "1": "keyboard" });
    await sync(o, p, 1);
    let t = 1_000_000_000_000;
    await setClock(shareId, () => t);
    for (const round of [0, 1, 2]) {
      for (let i = 0; i < 51; i++) p.send("term_input", { from: 1, to: 0, payload: "x" });
      if (round === 2) break;
      await syncOwner(o, p, 1); // the hub reads the clock per frame: the burst is handled before the clock moves
      t += 1000;
    }
    expect((await p.closed).code).toBe(4008);
    await o.next("srv_peer", (f) => json(f).seat === 1 && json(f).state === "left");
    expect(await waitFor(() => leftAt(shareId, 1), (v) => v !== null)).toBeGreaterThan(0);
    await connectWorkspaceWs(members[0]!.token, shareId);
    await o.next("srv_peer", (f) => json(f).seat === 1 && json(f).state === "joined"); // the first joined was taken by room()
    expect(o.all("srv_peer").filter((f) => json(f).seat === 1).map((f) => json(f).state)).toEqual(["joined", "left", "joined"]);
  });

  it("the owner closed 4000 still gets the 2-minute grace, then the share times out", async () => {
    const { shareId, o, ps } = await room(1);
    o.sendRaw(frame("term_out", 0, 255, new Uint8Array(MAX_FRAME - HEADER_LEN + 1)));
    expect((await o.closed).code).toBe(4000);
    expect(await waitFor(() => runInDurableObject(hub(shareId), (_i, state) => state.storage.getAlarm()), (v) => v !== null)).not.toBeNull();
    expect(await runDurableObjectAlarm(hub(shareId))).toBe(true);
    expect((await ps[0]!.closed).code).toBe(4100);
    expect(await env.DB.prepare("SELECT status, end_reason FROM workspace_shares WHERE id = ?1").bind(shareId).first()).toEqual({ status: "ended", end_reason: "timeout" });
  });

  it("ending the share keeps only the ended flag in DO storage (no title, owner or levels)", async () => {
    const { owner, shareId, o } = await room(1);
    levels(o, { "1": "viewer" }, { "1": true });
    o.send("ctl_attention", { to: 254, payload: {} }); // a frame after ctl_levels: ctl_levels has been handled once the hub reads it
    await waitFor(() => runInDurableObject(hub(shareId), (_i, state) => state.storage.get("levels")), (v) => v !== undefined);
    expect((await api(owner.token).del(`/v1/workspace-shares/${shareId}`)).status).toBe(204);
    expect((await o.closed).code).toBe(4100);
    const keys = await runInDurableObject(hub(shareId), async (_i, state) => [...(await state.storage.list()).keys()].sort());
    expect(keys).toEqual(["ended"]);
  });
});

describe("WorkspaceHub: limits and alerts", () => {
  it("rate limits participant input and closes 4008 after 3 seconds over", async () => {
    const { shareId, o, ps } = await room(1);
    const p = ps[0]!;
    levels(o, { "1": "keyboard" });
    await sync(o, p, 1);
    let t = 1_000_000_000_000;
    await setClock(shareId, () => t);
    for (let i = 0; i < 51; i++) p.send("term_input", { from: 1, to: 0, payload: `a${i}` });
    await syncOwner(o, p, 1);
    expect(o.all("term_input")).toHaveLength(50);
    t += 1000;
    for (let i = 0; i < 51; i++) p.send("term_input", { from: 1, to: 0, payload: `b${i}` });
    await syncOwner(o, p, 1);
    expect(o.all("term_input")).toHaveLength(100);
    t += 1000;
    for (let i = 0; i < 51; i++) p.send("term_input", { from: 1, to: 0, payload: `c${i}` });
    expect((await p.closed).code).toBe(4008);
  });

  it("an over second followed by a calm one starts the count again", async () => {
    const { shareId, o, ps } = await room(1);
    const p = ps[0]!;
    levels(o, { "1": "editor" });
    await sync(o, p, 1);
    let t = 1_000_000_000_000;
    await setClock(shareId, () => t);
    // the hub reads the clock when it handles a frame: wait for each burst before moving the clock
    const burst = async (n: number) => {
      for (let i = 0; i < n; i++) p.send("doc_op", { from: 1, to: 0, payload: "op" });
      await syncOwner(o, p, 1);
    };
    await burst(31);
    t += 1000;
    await burst(31);
    t += 1000;
    await burst(1); // calm
    t += 1000;
    await burst(31);
    expect(o.all("doc_op")).toHaveLength(30 + 30 + 1 + 30);
  });

  it("attention alert to the owner at most once per 30 seconds", async () => {
    const { owner, shareId, o } = await room(0);
    const watch = await connectWs(owner.token);
    let t = Date.now();
    await setClock(shareId, () => t);
    o.send("ctl_attention", { to: 254, payload: {} });
    o.send("ctl_attention", { to: 254, payload: {} });
    const al = await watch.next("alert", (d) => d.alert.kind === "workspace_attention");
    expect(al.data.alert.template).toEqual({ id: "workspace.attention", params: { share: "ws" } });
    expect(al.data.alert.target).toMatchObject({ type: "workspace_share", id: shareId });
    expect(al.data.alert.body).toBe("공유 중인 작업공간에서 확인이 필요해요");
    t += 31_000;
    o.send("ctl_attention", { to: 254, payload: {} });
    await watch.next("alert", (d) => d.alert.kind === "workspace_attention");
    expect(watch.frames.filter((f) => f.type === "alert" && f.data.alert.kind === "workspace_attention")).toHaveLength(2);
  });
});
