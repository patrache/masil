import { describe, it, expect } from "vitest";
import { env, SELF } from "cloudflare:test";
import { newId } from "../../src/lib/ids";
import { BASE, connectWs, signup, uniqueIp } from "../helpers";

// ---- local fixtures (this file only) ----

type Frame = { type: string; seq: number | null; data: any };

/** Resolves with the first text frame the socket receives (parsed). */
function firstFrame(ws: WebSocket, timeoutMs = 3000): Promise<Frame> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("no frame received")), timeoutMs);
    ws.addEventListener("message", (e) => {
      clearTimeout(timer);
      resolve(JSON.parse(e.data as string));
    }, { once: true });
  });
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
  const hello = firstFrame(ws);
  ws.accept();
  expect((await hello).type).toBe("hello");
  return ws;
}

describe("ws handshake", () => {
  it("hello -> resume(null) -> ready; ping auto-pong", async () => {
    const u = await signup(); const dev = newId();
    const c = await connectWs(u.token, { deviceId: dev });
    const hello = c.frames.find((f) => f.type === "hello")!;
    expect(hello.seq).toBeNull();
    expect(hello.data).toMatchObject({ user_id: u.user.id, mode: "full", device_id: dev, seq: 0, oldest_seq: 1, heartbeat_interval_ms: 25000, heartbeat_timeout_ms: 120000, local_tui_connected: false, limits: { max_frame_bytes: 8192, watch_topics_max: 50, typing_min_interval_ms: 3000 } });
    expect((await c.next("ready")).data).toMatchObject({ resumed: false, resync_required: false, replayed: 0, epoch: hello.data.epoch });
    c.send('{"type":"ping"}'); await c.next("pong");
    const p = await env.DB.prepare("SELECT online_until FROM user_presence WHERE user_id=?").bind(u.user.id).first<any>();
    expect(p.online_until).toBeGreaterThan(Date.now() + 9 * 60_000);
  });

  it("rejects before upgrade: no token 401, bad device_id 400, not websocket 400", async () => {
    const u = await signup();
    expect((await SELF.fetch(BASE + "/v1/ws?device_id=" + newId(), { headers: { Upgrade: "websocket" } })).status).toBe(401);
    expect((await SELF.fetch(BASE + "/v1/ws?device_id=bad", { headers: { Upgrade: "websocket", Authorization: "Bearer " + u.token } })).status).toBe(400);
    expect((await SELF.fetch(BASE + "/v1/ws?device_id=" + newId(), { headers: { Authorization: "Bearer " + u.token } })).status).toBe(400);
  });

  it("client cannot forge X-Masil-Auth", async () => {
    const [u, v] = await Promise.all([signup(), signup()]);
    const res = await SELF.fetch(BASE + "/v1/ws?device_id=" + newId(), { headers: { Upgrade: "websocket", Authorization: "Bearer " + u.token, "X-Masil-Auth": JSON.stringify({ userId: v.user.id }) } });
    const ws = res.webSocket!; const first = firstFrame(ws); ws.accept(); const hello = await first;
    expect(hello.data.user_id).toBe(u.user.id);
  });

  it("11th connection replaces the oldest with 4009", async () => {
    const u = await signup(); const cs = []; for (let i = 0; i < 10; i++) cs.push(await connectWs(u.token));
    const closed = waitClose(cs[0].ws); await connectWs(u.token);
    expect((await closed).code).toBe(4009);
  });

  it("Review Focus 3: binary -> 4000, >8KB -> 1009, unknown type -> reply, frame before resume -> 4000", async () => {
    const u = await signup();
    let c = await connectWs(u.token); let cl = waitClose(c.ws); c.ws.send(new Uint8Array([1, 2])); expect((await cl).code).toBe(4000);
    c = await connectWs(u.token); cl = waitClose(c.ws); c.send({ type: "idle", data: { idle: false, pad: "x".repeat(9000) } }); expect((await cl).code).toBe(1009);
    c = await connectWs(u.token); c.send({ type: "dance", ref: "r1", data: {} });
    expect((await c.next("reply", (d) => d.ref === "r1")).data).toMatchObject({ ok: false, error: { code: "unknown_type" } });
    c.send("not json"); c.send({ type: "ack", data: { seq: 0 } }); // still alive
    c.send({ type: "ack", ref: "r2", data: { seq: -1 } });
    expect((await c.next("reply", (d) => d.ref === "r2")).data).toMatchObject({ ok: false, error: { code: "invalid_frame" } });
    const raw = await rawConnect(u.token); cl = waitClose(raw); raw.send(JSON.stringify({ type: "typing", data: { channel_id: newId(), thread_root_id: null } }));
    expect((await cl).code).toBe(4000);
  });

  it("frame flood: >60 frames/10s -> 4008", async () => {
    const u = await signup(); const c = await connectWs(u.token); const cl = waitClose(c.ws);
    for (let i = 0; i < 61; i++) c.send({ type: "ack", data: { seq: 0 } });
    expect((await cl).code).toBe(4008);
  });
});
