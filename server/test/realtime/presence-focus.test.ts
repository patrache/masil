import { env, runInDurableObject, runDurableObjectAlarm, createScheduledController, createExecutionContext } from "cloudflare:test";
import { handleScheduled } from "../../src/cron";
import { describe, it, expect } from "vitest";
import type { Runtime } from "../../src/env";
import { newId } from "../../src/lib/ids";
import { deliverToUsers } from "../../src/lib/realtime";
import type { Alert, AlertKind } from "../../src/types/api";
import { serializeMe } from "../../src/lib/serialize";
import { api, connectWs, signup, waitFor } from "../helpers";

// ---- local fixtures (this file only) ----

type U = { user: { id: string } };

const rtFor = (e: typeof env): Runtime => ({ env: e, waitUntil: () => {} });

const settle = (ms = 150) => new Promise<void>((r) => setTimeout(r, ms));

/** Friendship row as the friends service writes it (user_a < user_b). */
async function makeFriendsSql(a: U, b: U): Promise<void> {
  const [lo, hi] = a.user.id < b.user.id ? [a.user.id, b.user.id] : [b.user.id, a.user.id];
  await env.DB.prepare("INSERT INTO friendships (user_a, user_b, created_at) VALUES (?1, ?2, ?3)").bind(lo, hi, Date.now()).run();
}

function fakeAlert(kind: AlertKind = "dm"): Alert {
  return {
    id: newId(),
    kind,
    title: "hi",
    body: "hello",
    actor: null,
    target: { type: "channel", id: newId(), channel_id: null, message_id: null, thread_root_id: null, post_id: null, space_id: null },
    created_at: new Date().toISOString(),
    suppressed: false,
    suppressed_reason: null,
    template: null,
  };
}

describe("presence", () => {
  it("status: offline -> online on full connect; notify-only stays offline; idle -> away", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await makeFriendsSql(a, b);
    const cb = await connectWs(b.token);
    await connectWs(a.token, { mode: "notify" });
    await settle();
    expect((await api(a.token).get<any>(`/v1/presence?user_ids=${a.user.id}`)).body.items[0].status).toBe("offline");
    const ca = await connectWs(a.token);
    expect((await cb.next("presence.updated", (d) => d.user_id === a.user.id && d.status === "online")).seq).toBeNull();
    ca.send({ type: "idle", data: { idle: true } });
    await cb.next("presence.updated", (d) => d.user_id === a.user.id && d.status === "away");
  });

  it("presence.snapshot lists friends after ready", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await makeFriendsSql(a, b);
    const ca = await connectWs(a.token);
    expect((await ca.next("presence.snapshot")).data.items).toEqual([{ user_id: b.user.id, status: "offline", last_seen_at: null }]);
  });

  it("manual dnd suppresses alerts with reason dnd; GET /presence hides strangers", async () => {
    const [a, s] = await Promise.all([signup(), signup()]);
    const c = await connectWs(a.token);
    expect((await api(a.token).put<any>("/v1/me/presence", { mode: "dnd" })).body.presence).toMatchObject({ status: "dnd", mode: "dnd" });
    await deliverToUsers(rtFor(env), [a.user.id], [{ type: "alert", data: { alert: fakeAlert() }, ts: Date.now(), logged: true }]);
    expect((await c.next("alert")).data.alert).toMatchObject({ suppressed: true, suppressed_reason: "dnd" });
    expect((await api(a.token).get<any>(`/v1/presence?user_ids=${s.user.id}`)).body.items).toEqual([]);
  });
});

describe("focus", () => {
  it("focus: start -> suppressed(focus) -> DELETE returns summary + focus.ended + focus_summary alert exactly once; second DELETE 409", async () => {
    const a = await signup();
    const c = await connectWs(a.token, { mode: "notify" });
    const f = await api(a.token).post<any>("/v1/me/focus", { duration_sec: 60 });
    expect(f.status).toBe(200);
    await c.next("focus.started");
    expect((await api(a.token).get<any>("/v1/me/focus")).body.focus).toEqual(f.body.focus);
    await deliverToUsers(rtFor(env), [a.user.id], [{ type: "alert", data: { alert: fakeAlert("mention") }, ts: Date.now(), logged: true }]);
    expect((await c.next("alert")).data.alert).toMatchObject({ suppressed: true, suppressed_reason: "focus" });
    const end = await api(a.token).del<any>("/v1/me/focus");
    expect(end.body.summary.counts).toEqual({ dm: 0, mention: 1, thread_reply: 0, invite: 0, friend_request: 0, other: 0 });
    expect((await c.next("focus.ended")).data.summary.counts.mention).toBe(1);
    const sum = await c.next("alert", (d) => d.alert.kind === "focus_summary"); // early end also sends the summary alert
    expect(sum.seq).not.toBeNull();
    expect(sum.data.alert).toMatchObject({ title: "집중 모드 끝 — 멘션 1, DM 0", suppressed: false, suppressed_reason: null });
    expect((await api(a.token).del<any>("/v1/me/focus")).body.error).toMatchObject({ code: "conflict", details: { reason: "not_focusing" } });
    // the original end time passing later must not produce a second summary
    await runInDurableObject(env.USER_HUB.getByName(a.user.id), (inst) => {
      (inst as any).now = () => Date.now() + 61_000;
    }); // < 120s heartbeat timeout, socket stays open
    await runDurableObjectAlarm(env.USER_HUB.getByName(a.user.id));
    await settle();
    expect(c.frames.filter((f) => f.type === "alert" && f.data.alert.kind === "focus_summary")).toHaveLength(1);
    expect(c.frames.filter((f) => f.type === "focus.ended")).toHaveLength(1);
    expect((await api(a.token).get<any>("/v1/me/focus")).body.focus).toBeNull();
    expect((await api(a.token).post<any>("/v1/me/focus", { duration_sec: 30 })).status).toBe(400);
  });

  it("focus natural end via alarm sends focus.ended + focus_summary alert", async () => {
    const a = await signup();
    const c = await connectWs(a.token);
    await api(a.token).post("/v1/me/focus", { duration_sec: 60 });
    await runInDurableObject(env.USER_HUB.getByName(a.user.id), (inst) => {
      (inst as any).now = () => Date.now() + 61_000;
    });
    await runDurableObjectAlarm(env.USER_HUB.getByName(a.user.id));
    await c.next("focus.ended");
    expect((await c.next("alert", (d) => d.alert.kind === "focus_summary")).data.alert.suppressed).toBe(false);
    expect((await api(a.token).del<any>("/v1/me/focus")).status).toBe(409); // already ended: no second summary path
    await settle();
    expect(c.frames.filter((f) => f.type === "alert" && f.data.alert.kind === "focus_summary")).toHaveLength(1);
    // the D1 mirror drops the focus pair on a natural end too
    expect(
      await env.DB.prepare("SELECT focus_started_at, focus_ends_at FROM user_presence WHERE user_id = ?1").bind(a.user.id).first(),
    ).toEqual({ focus_started_at: null, focus_ends_at: null });
  });
});

describe("presence regressions", () => {
  it("Me.presence.focus (GET /me, login) matches GET /me/focus even after a connect rewrites user_presence.updated_at", async () => {
    const a = await signup();
    const started = (await api(a.token).post<any>("/v1/me/focus", { duration_sec: 600 })).body.focus;
    await settle(30); // the connect below must land on a later millisecond than the focus start
    const c = await connectWs(a.token);
    const focus = (await api(a.token).get<any>("/v1/me/focus")).body.focus;
    expect(focus).toEqual(started);
    const me = (await api(a.token).get<any>("/v1/me")).body.user;
    expect(me.presence).toEqual({ status: "dnd", mode: "auto", focus });
    const relogin = await api().post<any>("/v1/auth/login", { handle: a.handle, password: a.password });
    expect(relogin.body.user.presence.focus).toEqual(focus);
    const patched = await api(a.token).patch<any>("/v1/me/profile", { bio: "x" });
    expect(patched.body.user.presence.focus).toEqual(focus);
    c.close();
  });

  it("D1 mirror keeps the exact focus start (user_presence.focus_started_at): the D1 fallback serializeMe reports it through connects and extensions; cleared on end", async () => {
    const a = await signup();
    const mirror = () =>
      env.DB.prepare("SELECT focus_started_at, focus_ends_at FROM user_presence WHERE user_id = ?1")
        .bind(a.user.id)
        .first<{ focus_started_at: number | null; focus_ends_at: number | null }>();
    const started = (await api(a.token).post<any>("/v1/me/focus", { duration_sec: 600 })).body.focus;
    expect(await mirror()).toEqual({ focus_started_at: Date.parse(started.started_at), focus_ends_at: Date.parse(started.ends_at) });
    await settle(30); // the connect below rewrites user_presence.updated_at on a later millisecond than the focus start
    const c = await connectWs(a.token);
    await settle(30);
    // serializeMe is the D1-only path (signup, and serializeMeLive's fallback when the hub is unreachable)
    expect((await serializeMe(env.DB, a.user.id)).presence.focus).toEqual(started);
    // extending replaces the end, not the start — in D1 too
    const extended = (await api(a.token).post<any>("/v1/me/focus", { duration_sec: 1200 })).body.focus;
    expect(extended.started_at).toBe(started.started_at);
    expect(await mirror()).toEqual({ focus_started_at: Date.parse(started.started_at), focus_ends_at: Date.parse(extended.ends_at) });
    expect((await serializeMe(env.DB, a.user.id)).presence.focus).toEqual(extended);
    expect((await api(a.token).del<any>("/v1/me/focus")).status).toBe(200);
    expect(await mirror()).toEqual({ focus_started_at: null, focus_ends_at: null });
    expect((await serializeMe(env.DB, a.user.id)).presence.focus).toBeNull();
    c.close();
  });

  it("a row written before 0003 (focus_ends_at live, focus_started_at NULL) reports its last write as the start, never after the end", async () => {
    const a = await signup();
    const t = Date.now();
    const row = (updatedAt: number, endsAt: number) =>
      env.DB.prepare(
        `INSERT INTO user_presence (user_id, status, mode, focus_ends_at, focus_started_at, updated_at)
         VALUES (?1, 'dnd', 'auto', ?3, NULL, ?2)
         ON CONFLICT (user_id) DO UPDATE SET status = 'dnd', mode = 'auto', focus_ends_at = ?3, focus_started_at = NULL, updated_at = ?2`,
      )
        .bind(a.user.id, updatedAt, endsAt)
        .run();
    await row(t - 60_000, t + 600_000);
    expect((await serializeMe(env.DB, a.user.id)).presence.focus).toEqual({
      started_at: new Date(t - 60_000).toISOString(),
      ends_at: new Date(t + 600_000).toISOString(),
    });
    // an updated_at past the end (a later write of the row) is clamped to the end
    await row(t + 900_000, t + 600_000);
    expect((await serializeMe(env.DB, a.user.id)).presence.focus).toEqual({
      started_at: new Date(t + 600_000).toISOString(),
      ends_at: new Date(t + 600_000).toISOString(),
    });
  });

  it("manual dnd: cleanup marks D1 offline while disconnected; the reconnect restores D1 dnd and re-announces it", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await makeFriendsSql(a, b);
    const c1 = await connectWs(a.token);
    expect((await api(a.token).put<any>("/v1/me/presence", { mode: "dnd" })).body.presence.status).toBe("dnd");
    c1.close();
    // the hub's close handling (online_until -> now+120s, last_seen_at) lands before the cron below runs
    const closed = await waitFor(
      () => env.DB.prepare("SELECT online_until, last_seen_at FROM user_presence WHERE user_id = ?1").bind(a.user.id).first<any>(),
      (r) => r.last_seen_at !== null && r.online_until <= Date.now() + 120_000,
    );
    expect(closed.last_seen_at).not.toBeNull();
    // grace passed + cron task 9 (data-model.md 6) ran while no socket was connected
    await env.DB.prepare("UPDATE user_presence SET online_until = ?1 WHERE user_id = ?2").bind(Date.now() - 1000, a.user.id).run();
    await handleScheduled(createScheduledController({ cron: "17 * * * *", scheduledTime: Date.now() }), env, createExecutionContext());
    expect((await env.DB.prepare("SELECT status FROM user_presence WHERE user_id = ?1").bind(a.user.id).first<any>()).status).toBe("offline");

    const cb = await connectWs(b.token);
    await connectWs(a.token);
    await cb.next("presence.updated", (d) => d.user_id === a.user.id && d.status === "dnd");
    expect(await env.DB.prepare("SELECT status, mode FROM user_presence WHERE user_id = ?1").bind(a.user.id).first()).toEqual({ status: "dnd", mode: "dnd" });
    expect((await api(b.token).get<any>(`/v1/presence?user_ids=${a.user.id}`)).body.items[0].status).toBe("dnd");
  });
});
