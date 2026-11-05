import { env, runInDurableObject, runDurableObjectAlarm } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import type { Runtime } from "../../src/env";
import { newId } from "../../src/lib/ids";
import { deliverToUsers } from "../../src/lib/realtime";
import type { Alert, AlertKind } from "../../src/types/api";
import { api, connectWs, signup } from "../helpers";

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
  });
});
