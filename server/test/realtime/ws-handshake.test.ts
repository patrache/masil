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

  it("upgrade racing a revocation: the hub re-checks the session, sends session.revoked and closes 4001 / 4003", async () => {
    /** Hands an upgrade straight to the hub, as routes/ws.ts does after requireAuth read a still-live session. */
    const hubUpgrade = async (u: Awaited<ReturnType<typeof signup>>, sessionId: string) => {
      const auth = { userId: u.user.id, sessionId, handle: u.handle, role: "user" };
      const res = await env.USER_HUB.getByName(u.user.id).fetch(
        new Request(`${BASE}/v1/ws?mode=full&device_id=${newId()}`, { headers: { Upgrade: "websocket", "X-Masil-Auth": JSON.stringify(auth) } }),
      );
      expect(res.status).toBe(101);
      const ws = res.webSocket!;
      const first = firstFrame(ws); const closed = waitClose(ws); ws.accept();
      return { first: await first, closed: await closed };
    };
    const sessionOf = async (userId: string) =>
      (await env.DB.prepare("SELECT id FROM sessions WHERE user_id=? ORDER BY created_at DESC LIMIT 1").bind(userId).first<any>()).id as string;

    // logged out between requireAuth and the hub: no hello, session.revoked{logout} then 4001
    const u = await signup(); const sid = await sessionOf(u.user.id);
    const live = await connectWs(u.token);
    const liveClosed = waitClose(live.ws, 1500).then(() => true, () => false);
    await env.DB.prepare("UPDATE sessions SET revoked_at=?1, revoke_reason='logout' WHERE id=?2").bind(Date.now(), sid).run();
    const r1 = await hubUpgrade(u, sid);
    expect(r1.first).toMatchObject({ type: "session.revoked", data: { session_id: sid, reason: "logout" } });
    expect(r1.closed.code).toBe(4001);
    expect(await liveClosed).toBe(false); // a dead upgrade never replaces or touches an open socket
    expect((await env.USER_HUB.getByName(u.user.id).inspect()).connections).toBe(1);

    // expired session: reason revoked, 4001
    const e = await signup(); const esid = await sessionOf(e.user.id);
    await env.DB.prepare("UPDATE sessions SET expires_at=?1 WHERE id=?2").bind(Date.now() - 1, esid).run();
    const r2 = await hubUpgrade(e, esid);
    expect(r2.first).toMatchObject({ type: "session.revoked", data: { session_id: esid, reason: "revoked" } });
    expect(r2.closed.code).toBe(4001);

    // suspended in that window (session row not revoked yet): reason suspended, 4003
    const s = await signup(); const ssid = await sessionOf(s.user.id);
    await env.DB.prepare("UPDATE users SET suspended_at=?1, suspend_reason='spam' WHERE id=?2").bind(Date.now(), s.user.id).run();
    const r3 = await hubUpgrade(s, ssid);
    expect(r3.first).toMatchObject({ type: "session.revoked", data: { session_id: ssid, reason: "suspended" } });
    expect(r3.closed.code).toBe(4003);
    expect((await env.USER_HUB.getByName(s.user.id).inspect()).connections).toBe(0);
  });

  it("frame flood: >60 frames/10s -> 4008", async () => {
    const u = await signup(); const c = await connectWs(u.token); const cl = waitClose(c.ws);
    for (let i = 0; i < 61; i++) c.send({ type: "ack", data: { seq: 0 } });
    expect((await cl).code).toBe(4008);
  });
});
