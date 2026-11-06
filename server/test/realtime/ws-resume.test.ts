import { describe, it, expect } from "vitest";
import { env, SELF, runInDurableObject, runDurableObjectAlarm } from "cloudflare:test";
import { newId } from "../../src/lib/ids";
import type { Alert } from "../../src/types/api";
import { BASE, connectWs, signup, uniqueIp } from "../helpers";

// ---- local fixtures (this file only) ----

const hub = (id: string) => env.USER_HUB.getByName(id);
const ev = (n: number) => ({ type: "settings.updated" as const, data: { n }, ts: Date.now(), logged: true });

/** Short wait so frames sent by the client are processed by the hub. */
const settle = (ms = 100) => new Promise<void>((r) => setTimeout(r, ms));

function fakeAlert(): Alert {
  return {
    id: newId(),
    kind: "dm",
    title: "hi",
    body: "hello",
    actor: null,
    target: { type: "channel", id: newId(), channel_id: null, message_id: null, thread_root_id: null, post_id: null, space_id: null },
    created_at: new Date().toISOString(),
    suppressed: false,
    suppressed_reason: null,
  };
}

/** Resolves with the close code/reason the socket sees. */
function waitClose(ws: WebSocket, timeoutMs = 3000): Promise<{ code: number; reason: string }> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("socket did not close")), timeoutMs);
    ws.addEventListener("close", (e) => {
      clearTimeout(timer);
      resolve({ code: e.code, reason: e.reason });
    }, { once: true });
  });
}

/** A connection that receives hello but never sends resume. */
async function rawConnect(token: string): Promise<WebSocket> {
  const res = await SELF.fetch(BASE + "/v1/ws?device_id=" + newId(), {
    headers: { Upgrade: "websocket", Authorization: "Bearer " + token, "User-Agent": "masil/0.1.0 (test; test)", "CF-Connecting-IP": uniqueIp() },
  });
  const ws = res.webSocket!;
  const hello = new Promise<any>((resolve) => ws.addEventListener("message", (e) => resolve(JSON.parse(e.data as string)), { once: true }));
  ws.accept();
  expect((await hello).type).toBe("hello");
  return ws;
}

describe("resume", () => {
  it("replays logged events after last_seq in order; ephemeral has seq null", async () => {
    const u = await signup(); const c1 = await connectWs(u.token); const epoch = c1.frames[0].data.epoch;
    await hub(u.user.id).deliver({ userId: u.user.id, events: [ev(1), ev(2), { type: "typing", data: {}, ts: Date.now(), logged: false }] });
    const t = await c1.next("typing"); expect(t.seq).toBeNull();
    c1.close(1000);
    await hub(u.user.id).deliver({ userId: u.user.id, events: [ev(3)] }); // offline but within grace: logged
    const c2 = await connectWs(u.token, { resume: { epoch, last_seq: 1 } });
    expect(c2.frames.filter((f) => f.type === "settings.updated").map((f) => f.seq)).toEqual([2, 3]);
    expect(c2.frames.find((f) => f.type === "ready")!.data).toMatchObject({ resumed: true, resync_required: false, replayed: 2, seq: 3 });
  });

  it("watermark passed without close handler -> new epoch -> resync_required", async () => {
    const u = await signup(); const c1 = await connectWs(u.token); const epoch = c1.frames[0].data.epoch;
    await env.DB.prepare("UPDATE user_presence SET online_until=1 WHERE user_id=?").bind(u.user.id).run();
    await runInDurableObject(hub(u.user.id), (_i, st) => { st.storage.sql.exec("UPDATE meta SET v='1' WHERE k='full_until'"); });
    const c2 = await connectWs(u.token, { resume: { epoch, last_seq: 0 } });
    expect(c2.frames[0].data.epoch).not.toBe(epoch);
    expect(c2.frames.find((f) => f.type === "ready")!.data).toMatchObject({ resumed: false, resync_required: true });
  });

  it("use_device_ack is per (device, mode)", async () => {
    const u = await signup(); const dev = newId();
    const full = await connectWs(u.token, { deviceId: dev }); const notify = await connectWs(u.token, { deviceId: dev, mode: "notify" });
    await hub(u.user.id).deliver({ userId: u.user.id, events: [{ type: "alert", data: { alert: fakeAlert() }, ts: Date.now(), logged: true }, ev(2)] });
    full.send({ type: "ack", data: { seq: 2 } }); notify.send({ type: "ack", data: { seq: 0 } });
    const acks = () => runInDurableObject(hub(u.user.id), (_i, st): Record<string, SqlStorageValue>[] => st.storage.sql.exec("SELECT mode, seq FROM device_acks ORDER BY mode").toArray());
    // Wait until the hub has stored both acks (a fixed sleep was flaky under full-suite load: the notify ack could
    // still be unprocessed when n2 resumed, so use_device_ack found no row and replayed nothing).
    for (let i = 0; i < 100 && (await acks()).length < 2; i++) await settle(30);
    notify.close(1000);
    const n2 = await connectWs(u.token, { deviceId: dev, mode: "notify", resume: { use_device_ack: true } as any });
    expect(n2.frames.filter((f) => f.type === "alert").map((f) => f.seq)).toEqual([1]); // settings.updated not allowed in notify
    expect(await acks()).toEqual([{ mode: "full", seq: 2 }, { mode: "notify", seq: 0 }]);
  });

  it("alarm: heartbeat timeout closes 4002; no resume in 10s closes 4000", async () => {
    const u = await signup(); const c = await connectWs(u.token); const cl = waitClose(c.ws);
    await runInDurableObject(hub(u.user.id), (inst) => { (inst as any).now = () => Date.now() + 121_000; });
    await runDurableObjectAlarm(hub(u.user.id)); expect((await cl).code).toBe(4002);
    const v = await signup(); const raw = await rawConnect(v.token); const cl2 = waitClose(raw);
    await runInDurableObject(hub(v.user.id), (inst) => { (inst as any).now = () => Date.now() + 11_000; });
    await runDurableObjectAlarm(hub(v.user.id)); expect((await cl2).code).toBe(4000);
  });

  it("alarm extends online_until when <5min left and trims log to 2000", async () => {
    const u = await signup(); await connectWs(u.token);
    await runInDurableObject(hub(u.user.id), (_i, st) => { st.storage.sql.exec("UPDATE meta SET v=? WHERE k='full_until'", String(Date.now() + 60_000)); });
    await hub(u.user.id).deliver({ userId: u.user.id, events: Array.from({ length: 2005 }, (_, i) => ev(i)) });
    await runDurableObjectAlarm(hub(u.user.id));
    const s = await hub(u.user.id).inspect();
    expect(s.fullUntil).toBeGreaterThan(Date.now() + 9 * 60_000); expect(s.seq - s.oldestSeq + 1).toBe(2000);
  });

  it("close of last full socket shortens online_until to ~now+120s", async () => {
    const u = await signup(); const c = await connectWs(u.token); c.close(1000); await settle();
    const p = await env.DB.prepare("SELECT online_until, last_seen_at FROM user_presence WHERE user_id=?").bind(u.user.id).first<any>();
    expect(p.online_until).toBeLessThanOrEqual(Date.now() + 120_000); expect(p.last_seen_at).not.toBeNull();
  });
});
