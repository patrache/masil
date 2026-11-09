// Pending item 4-a (api.md 3.9 / 5.18, events.md 3.3): the UserHub log keeps frames for up to 24h / 2,000 events, so a
// `resume` used to replay `notification.created` / `alert` frames whose actor was suspended (or blocked by the
// recipient) after they were logged — the very rows the inbox hides. The replay now applies the inbox rule
// (inboxRowVisibleSql: recipient blocked the actor, or the actor is suspended right now) to every frame that names a
// notification actor: such `notification.created` / `alert` frames are skipped, and `focus.ended` summaries lose those
// items. The focus summary built at focus end applies the same rule to the alerts it collected. Notify mode (the daemon)
// replays through the same filter. A replayed friend.request_received whose sender is suspended right now carries the
// masked sender summary (D1): the sender is usually a stranger, outside the summaryWatchers that get user.updated.
import { env, runInDurableObject } from "cloudflare:test";
import { describe, expect, it } from "vitest";
import { newId } from "../../src/lib/ids";
import type { Alert, FocusSummary, UserSummary } from "../../src/types/api";
import { api, connectWs, makeAdmin, signup, uploadPng, type TestUser, type WsClient } from "../helpers";

const hub = (id: string) => env.USER_HUB.getByName(id);
const settle = (ms = 150) => new Promise<void>((r) => setTimeout(r, ms));

function summaryOf(t: TestUser): UserSummary {
  return { id: t.user.id, handle: t.handle, display_name: t.handle, avatar_url: null, status: null, is_private: false, suspended: false };
}

function alertFrom(actor: UserSummary | null, kind: Alert["kind"] = "mention"): Alert {
  return {
    id: newId(),
    kind,
    title: "t",
    body: "b",
    actor,
    target: { type: "user", id: newId(), channel_id: null, message_id: null, thread_root_id: null, post_id: null, space_id: null },
    created_at: new Date().toISOString(),
    suppressed: false,
    suppressed_reason: null,
    template: null,
  };
}

/** Frames carrying a notification actor `actorId` (inbox row or desktop alert). */
function actorFrames(c: WsClient, actorId: string) {
  return c.frames.filter(
    (f) =>
      (f.type === "notification.created" && f.data.notification.actor?.id === actorId) ||
      (f.type === "alert" && f.data.alert.actor?.id === actorId),
  );
}

/** r is online; s sends r a friend request (notification.created + alert{friend_request}, both logged); r goes away. */
async function requestThenLeave(): Promise<{ r: TestUser; s: TestUser; adm: TestUser; epoch: string; seq: number }> {
  const [r, s, adm] = await Promise.all([signup(), signup(), signup()]);
  await makeAdmin(adm);
  const c = await connectWs(r.token);
  const ready = c.frames.find((f) => f.type === "ready")!;
  expect((await api(s.token).post("/v1/friends/requests", { user: `@${r.handle}` })).status).toBe(201);
  await c.next("notification.created", (d) => d.notification.actor?.id === s.user.id);
  await c.next("alert", (d) => d.alert.actor?.id === s.user.id);
  c.close();
  await settle();
  return { r, s, adm, epoch: ready.data.epoch, seq: ready.data.seq };
}

describe("resume replay hides notification frames of hidden actors (4-a)", () => {
  it("control: without suspension or block the friend-request frames are replayed", async () => {
    const { r, s, epoch, seq } = await requestThenLeave();
    const again = await connectWs(r.token, { resume: { epoch, last_seq: seq } });
    expect(again.frames.find((f) => f.type === "ready")!.data).toEqual(expect.objectContaining({ resumed: true }));
    expect(actorFrames(again, s.user.id).map((f) => f.type)).toEqual(["notification.created", "alert"]);
  });

  it("actor suspended after the frames were logged: they are not replayed (full), the rest is", async () => {
    const { r, s, adm, epoch, seq } = await requestThenLeave();
    expect((await api(adm.token).post(`/v1/admin/users/@${s.handle}/suspend`, { reason: "spam" })).status).toBe(200);
    const again = await connectWs(r.token, { resume: { epoch, last_seq: seq } });
    const ready = again.frames.find((f) => f.type === "ready")!;
    expect(ready.data).toEqual(expect.objectContaining({ resumed: true, resync_required: false }));
    expect(actorFrames(again, s.user.id)).toEqual([]);
    // the friend.request_received frame is not a notification-actor frame — it still replays (with `from` masked, see
    // the friend.request_received tests below), so the log itself is intact
    expect(again.frames.some((f) => f.type === "friend.request_received")).toBe(true);
    expect(ready.data.replayed).toBe(again.frames.filter((f) => f.seq !== null).length);
    // the inbox agrees
    const inbox = await api(r.token).get<any>("/v1/notifications");
    expect(inbox.body.items.filter((n: any) => n.actor?.id === s.user.id)).toEqual([]);
    // notify mode (the daemon) has its own tests below: it needs the same device_id and the notify epoch
  });

  it("a timed suspension that has already ended does not hide them (same boundary as 3.4 rule 2)", async () => {
    const { r, s, epoch, seq } = await requestThenLeave();
    const t = Date.now();
    await env.DB.prepare("UPDATE users SET suspended_at = ?1, suspended_until = ?2 WHERE id = ?3").bind(t - 10_000, t - 1, s.user.id).run();
    const again = await connectWs(r.token, { resume: { epoch, last_seq: seq } });
    expect(actorFrames(again, s.user.id).map((f) => f.type)).toEqual(["notification.created", "alert"]);
  });

  it("recipient blocked the actor after the frames were logged: not replayed; block.updated is", async () => {
    const [r, s] = await Promise.all([signup(), signup()]);
    const c = await connectWs(r.token);
    const ready = c.frames.find((f) => f.type === "ready")!;
    expect((await api(s.token).post("/v1/friends/requests", { user: `@${r.handle}` })).status).toBe(201);
    await c.next("alert", (d) => d.alert.actor?.id === s.user.id);
    expect((await api(r.token).put(`/v1/blocks/@${s.handle}`)).status).toBeLessThan(300);
    await c.next("block.updated");
    c.close();
    await settle();
    const again = await connectWs(r.token, { resume: { epoch: ready.data.epoch, last_seq: ready.data.seq } });
    expect(actorFrames(again, s.user.id)).toEqual([]);
    expect(again.frames.some((f) => f.type === "block.updated" && f.data.user_id === s.user.id)).toBe(true);
  });

  it("frames without an actor, and of visible actors, still replay next to a hidden one", async () => {
    const [r, s, v] = await Promise.all([signup(), signup(), signup()]);
    const c = await connectWs(r.token);
    const ready = c.frames.find((f) => f.type === "ready")!;
    const hidden = alertFrom(summaryOf(s));
    const visible = alertFrom(summaryOf(v));
    const none = alertFrom(null);
    await hub(r.user.id).deliver({
      userId: r.user.id,
      events: [hidden, visible, none].map((alert) => ({ type: "alert" as const, data: { alert }, ts: Date.now(), logged: true })),
    });
    await c.next("alert", (d) => d.alert.id === none.id);
    c.close();
    await settle();
    const t = Date.now();
    await env.DB.prepare("UPDATE users SET suspended_at = ?1 WHERE id = ?2").bind(t, s.user.id).run();
    const again = await connectWs(r.token, { resume: { epoch: ready.data.epoch, last_seq: ready.data.seq } });
    expect(again.frames.filter((f) => f.type === "alert").map((f) => f.data.alert.id)).toEqual([visible.id, none.id]);
  });

  it("replayed focus.ended drops summary items of a hidden actor", async () => {
    const [r, s, v] = await Promise.all([signup(), signup(), signup()]);
    const c = await connectWs(r.token);
    const ready = c.frames.find((f) => f.type === "ready")!;
    const hs = alertFrom(summaryOf(s));
    const vs = alertFrom(summaryOf(v));
    const summary: FocusSummary = {
      started_at: new Date(Date.now() - 60_000).toISOString(),
      ended_at: new Date().toISOString(),
      counts: { dm: 0, mention: 2, thread_reply: 0, invite: 0, friend_request: 0, other: 0 },
      items: [hs, vs],
    };
    await hub(r.user.id).deliver({ userId: r.user.id, events: [{ type: "focus.ended", data: { summary }, ts: Date.now(), logged: true }] });
    await c.next("focus.ended");
    c.close();
    await settle();
    await env.DB.prepare("UPDATE users SET suspended_at = ?1 WHERE id = ?2").bind(Date.now(), s.user.id).run();
    const again = await connectWs(r.token, { resume: { epoch: ready.data.epoch, last_seq: ready.data.seq } });
    const fe = again.frames.find((f) => f.type === "focus.ended")!;
    expect(fe.data.summary.items.map((a: Alert) => a.id)).toEqual([vs.id]);
  });

  it("D1 failing during resume: the socket closes 1011 and nothing is replayed unfiltered", async () => {
    const [r, s] = await Promise.all([signup(), signup()]);
    const c = await connectWs(r.token);
    const ready = c.frames.find((f) => f.type === "ready")!;
    const hs = alertFrom(summaryOf(s));
    await hub(r.user.id).deliver({ userId: r.user.id, events: [{ type: "alert", data: { alert: hs }, ts: Date.now(), logged: true }] });
    await c.next("alert");
    c.close();
    await settle();
    // Break D1 only for the hub's resume lookup: the connect gate has already run when hello arrives.
    const res = await api(r.token).raw(`/v1/ws?mode=full&device_id=${newId()}`, { headers: { Upgrade: "websocket" } });
    const ws = res.webSocket!;
    const got: any[] = [];
    const closed = new Promise<number>((resolve) => ws.addEventListener("close", (e) => resolve(e.code), { once: true }));
    const hello = new Promise<void>((resolve) =>
      ws.addEventListener("message", (e) => {
        got.push(JSON.parse(e.data as string));
        resolve();
      }),
    );
    ws.accept();
    await hello;
    await runInDurableObject(hub(r.user.id), (inst) => {
      const real = (inst as any).env;
      (inst as any).env = { ...real, DB: { prepare: () => { throw new Error("D1 down"); } } };
    });
    ws.send(JSON.stringify({ type: "resume", data: { epoch: ready.data.epoch, last_seq: ready.data.seq } }));
    expect(await closed).toBe(1011);
    expect(got.map((f) => f.type)).toEqual(["hello"]);
  });
});

describe("focus summary leaves out alerts of actors hidden by focus end (4-a sibling)", () => {
  it("an actor suspended during the focus: not in summary items nor counts; DELETE /me/focus agrees", async () => {
    const [r, s, v] = await Promise.all([signup(), signup(), signup()]);
    const c = await connectWs(r.token);
    expect((await api(r.token).post("/v1/me/focus", { duration_sec: 600 })).status).toBeLessThan(300);
    await c.next("focus.started");
    const hs = alertFrom(summaryOf(s), "dm");
    const vs = alertFrom(summaryOf(v), "mention");
    await hub(r.user.id).deliver({
      userId: r.user.id,
      events: [hs, vs].map((alert) => ({ type: "alert" as const, data: { alert }, ts: Date.now(), logged: true })),
    });
    await c.next("alert", (d) => d.alert.id === vs.id);
    await env.DB.prepare("UPDATE users SET suspended_at = ?1 WHERE id = ?2").bind(Date.now(), s.user.id).run();
    const end = await api(r.token).del<any>("/v1/me/focus");
    expect(end.status).toBe(200);
    const fe = await c.next("focus.ended");
    for (const sum of [end.body.summary, fe.data.summary]) {
      expect(sum.items.map((a: Alert) => a.id)).toEqual([vs.id]);
      expect(sum.counts).toEqual(expect.objectContaining({ dm: 0, mention: 1 }));
    }
    const sa = await c.next("alert", (d) => d.alert.kind === "focus_summary");
    expect(sa.data.alert.title).toContain("DM 0");
  });

  it("D1 failing at focus end: counts keep every alert, items keep only actor-less ones", async () => {
    const [r, v] = await Promise.all([signup(), signup()]);
    const c = await connectWs(r.token);
    expect((await api(r.token).post("/v1/me/focus", { duration_sec: 600 })).status).toBeLessThan(300);
    await c.next("focus.started");
    const va = alertFrom(summaryOf(v), "mention");
    const na = alertFrom(null, "invite");
    await hub(r.user.id).deliver({
      userId: r.user.id,
      events: [va, na].map((alert) => ({ type: "alert" as const, data: { alert }, ts: Date.now(), logged: true })),
    });
    await c.next("alert", (d) => d.alert.id === na.id);
    const sum = await runInDurableObject(hub(r.user.id), async (inst) => {
      const real = (inst as any).env;
      (inst as any).env = { ...real, DB: { prepare: () => { throw new Error("D1 down"); } } };
      try {
        return (await (inst as any).finishFocus(Date.now())) as FocusSummary;
      } finally {
        (inst as any).env = real;
      }
    });
    expect(sum.items.map((a) => a.id)).toEqual([na.id]);
    expect(sum.counts).toEqual(expect.objectContaining({ mention: 1, invite: 1 }));
  });
});

// ---- notify mode (the daemon): same device_id, notify epoch ----

const deviceAcks = (userId: string) =>
  runInDurableObject(hub(userId), (_i, st) =>
    st.storage.sql.exec<{ mode: string; seq: number }>("SELECT mode, seq FROM device_acks").toArray(),
  );

/**
 * r's daemon (notify, device `dev`) is online and acks the seq it had before s's friend request; the request's
 * notification.created + alert{friend_request} reach the daemon (both notify types, both logged); the daemon goes away.
 */
async function notifyRequestThenLeave(): Promise<{ r: TestUser; s: TestUser; adm: TestUser; dev: string; epoch: string; seq: number }> {
  const [r, s, adm] = await Promise.all([signup(), signup(), signup()]);
  await makeAdmin(adm);
  const dev = newId();
  const n = await connectWs(r.token, { mode: "notify", deviceId: dev });
  const ready = n.frames.find((f) => f.type === "ready")!;
  expect((await api(s.token).post("/v1/friends/requests", { user: `@${r.handle}` })).status).toBe(201);
  await n.next("notification.created", (d) => d.notification.actor?.id === s.user.id);
  await n.next("alert", (d) => d.alert.actor?.id === s.user.id);
  n.send({ type: "ack", data: { seq: ready.data.seq } });
  for (let i = 0; i < 100 && !(await deviceAcks(r.user.id)).some((a) => a.mode === "notify"); i++) await settle(30);
  expect(await deviceAcks(r.user.id)).toEqual([{ mode: "notify", seq: ready.data.seq }]);
  n.close();
  await settle();
  return { r, s, adm, dev, epoch: ready.data.epoch, seq: ready.data.seq };
}

type NotifyResume = (x: { dev: string; epoch: string; seq: number }) => { deviceId: string; resume: any };
const notifyResumes: Array<[string, NotifyResume]> = [
  ["use_device_ack", ({ dev }) => ({ deviceId: dev, resume: { use_device_ack: true } })],
  ["notify epoch + last_seq", ({ epoch, seq }) => ({ deviceId: newId(), resume: { epoch, last_seq: seq } })],
];

describe("notify-mode resume replay hides notification frames of hidden actors (4-a)", () => {
  it.each(notifyResumes)("control (%s): without suspension or block both frames are replayed", async (_name, how) => {
    const x = await notifyRequestThenLeave();
    const again = await connectWs(x.r.token, { mode: "notify", ...how(x) });
    expect(again.frames.find((f) => f.type === "ready")!.data).toEqual(expect.objectContaining({ resumed: true, resync_required: false, replayed: 2 }));
    expect(actorFrames(again, x.s.user.id).map((f) => f.type)).toEqual(["notification.created", "alert"]);
  });

  it.each(notifyResumes)("actor suspended after the frames were logged (%s): not replayed", async (_name, how) => {
    const x = await notifyRequestThenLeave();
    expect((await api(x.adm.token).post(`/v1/admin/users/@${x.s.handle}/suspend`, { reason: "spam" })).status).toBe(200);
    const again = await connectWs(x.r.token, { mode: "notify", ...how(x) });
    expect(again.frames.find((f) => f.type === "ready")!.data).toEqual(expect.objectContaining({ resumed: true, resync_required: false, replayed: 0 }));
    expect(actorFrames(again, x.s.user.id)).toEqual([]);
  });

  it.each(notifyResumes)("recipient blocked the actor after the frames were logged (%s): not replayed", async (_name, how) => {
    const x = await notifyRequestThenLeave();
    expect((await api(x.r.token).put(`/v1/blocks/@${x.s.handle}`)).status).toBeLessThan(300);
    const again = await connectWs(x.r.token, { mode: "notify", ...how(x) });
    expect(again.frames.find((f) => f.type === "ready")!.data).toEqual(expect.objectContaining({ resumed: true, resync_required: false }));
    expect(actorFrames(again, x.s.user.id)).toEqual([]);
  });
});

// ---- friend.request_received from a stranger: no user.updated follows, so the replay masks `from` itself (D1) ----

describe("replayed friend.request_received masks a sender suspended right now (4-a / D1)", () => {
  /** s (status + avatar, a stranger to r) requests r while r is online; r goes away. */
  async function strangerRequest(): Promise<{ r: TestUser; s: TestUser; adm: TestUser; epoch: string; seq: number }> {
    const [r, s, adm] = await Promise.all([signup(), signup(), signup()]);
    await makeAdmin(adm);
    const up = await uploadPng(s, "avatar");
    expect((await api(s.token).patch("/v1/me/profile", { avatar_upload_id: up.id })).status).toBe(200);
    expect((await api(s.token).put("/v1/me/status", { emoji: "🙂", text: "hello" })).status).toBe(200);
    const c = await connectWs(r.token);
    const ready = c.frames.find((f) => f.type === "ready")!;
    expect((await api(s.token).post("/v1/friends/requests", { user: `@${r.handle}` })).status).toBe(201);
    const live = await c.next("friend.request_received");
    expect(live.data.request.from).toEqual(expect.objectContaining({ id: s.user.id, suspended: false, status: expect.objectContaining({ text: "hello" }) }));
    expect(live.data.request.from.avatar_url).not.toBeNull();
    c.close();
    await settle();
    return { r, s, adm, epoch: ready.data.epoch, seq: ready.data.seq };
  }

  it("suspended after the frame was logged: replayed `from` is suspended=true, status/avatar null — as REST says", async () => {
    const { r, s, adm, epoch, seq } = await strangerRequest();
    expect((await api(adm.token).post(`/v1/admin/users/@${s.handle}/suspend`, { reason: "spam" })).status).toBe(200);
    const again = await connectWs(r.token, { resume: { epoch, last_seq: seq } });
    expect(again.frames.find((f) => f.type === "ready")!.data).toEqual(expect.objectContaining({ resumed: true, resync_required: false }));
    // no user.updated reaches a stranger, so nothing later in the stream would fix an unmasked summary
    expect(again.frames.some((f) => f.type === "user.updated" && f.data.user.id === s.user.id)).toBe(false);
    const fr = again.frames.filter((f) => f.type === "friend.request_received");
    expect(fr).toHaveLength(1);
    const from = fr[0]!.data.request.from;
    expect(from).toEqual({ id: s.user.id, handle: s.handle, display_name: s.handle, avatar_url: null, status: null, is_private: false, suspended: true });
    const rest = await api(r.token).get<any>("/v1/friends/requests?direction=incoming");
    expect(rest.body.items.map((q: any) => q.from)).toEqual([from]);
    // the rest of the request is untouched
    expect(fr[0]!.data.request).toEqual(expect.objectContaining({ id: rest.body.items[0].id, status: "pending", to: expect.objectContaining({ id: r.user.id }) }));
  });

  it("a suspension that has already ended masks nothing (same boundary as 3.4 rule 2)", async () => {
    const { r, s, epoch, seq } = await strangerRequest();
    const t = Date.now();
    await env.DB.prepare("UPDATE users SET suspended_at = ?1, suspended_until = ?2 WHERE id = ?3").bind(t - 10_000, t - 1, s.user.id).run();
    const again = await connectWs(r.token, { resume: { epoch, last_seq: seq } });
    const from = again.frames.find((f) => f.type === "friend.request_received")!.data.request.from;
    expect(from).toEqual(expect.objectContaining({ suspended: false, status: expect.objectContaining({ text: "hello" }) }));
    expect(from.avatar_url).not.toBeNull();
  });

  it("blocked (not suspended) sender: the frame replays as logged, then the block's friend.request_removed", async () => {
    const { r, s, epoch, seq } = await strangerRequest();
    expect((await api(r.token).put(`/v1/blocks/@${s.handle}`)).status).toBeLessThan(300);
    const again = await connectWs(r.token, { resume: { epoch, last_seq: seq } });
    const types = again.frames.filter((f) => f.seq !== null).map((f) => f.type);
    const fr = again.frames.find((f) => f.type === "friend.request_received")!;
    expect(fr.data.request.from).toEqual(expect.objectContaining({ suspended: false, status: expect.objectContaining({ text: "hello" }) }));
    expect(types.indexOf("friend.request_removed")).toBeGreaterThan(types.indexOf("friend.request_received"));
  });
});

describe("focus summary leaves out alerts of actors the recipient blocked during the focus (4-a sibling)", () => {
  it("blocked during the focus: not in summary items nor counts; DELETE /me/focus agrees", async () => {
    const [r, s, v] = await Promise.all([signup(), signup(), signup()]);
    const c = await connectWs(r.token);
    expect((await api(r.token).post("/v1/me/focus", { duration_sec: 600 })).status).toBeLessThan(300);
    await c.next("focus.started");
    const hs = alertFrom(summaryOf(s), "dm");
    const vs = alertFrom(summaryOf(v), "mention");
    await hub(r.user.id).deliver({
      userId: r.user.id,
      events: [hs, vs].map((alert) => ({ type: "alert" as const, data: { alert }, ts: Date.now(), logged: true })),
    });
    await c.next("alert", (d) => d.alert.id === vs.id);
    expect((await api(r.token).put(`/v1/blocks/@${s.handle}`)).status).toBeLessThan(300);
    await c.next("block.updated");
    const end = await api(r.token).del<any>("/v1/me/focus");
    expect(end.status).toBe(200);
    const fe = await c.next("focus.ended");
    for (const sum of [end.body.summary, fe.data.summary]) {
      expect(sum.items.map((a: Alert) => a.id)).toEqual([vs.id]);
      expect(sum.counts).toEqual(expect.objectContaining({ dm: 0, mention: 1 }));
    }
    const sa = await c.next("alert", (d) => d.alert.kind === "focus_summary");
    expect(sa.data.alert.title).toContain("DM 0");
  });
});
