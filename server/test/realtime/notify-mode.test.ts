import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import type { Runtime } from "../../src/env";
import { newId } from "../../src/lib/ids";
import { publishEvent } from "../../src/lib/realtime";
import type { Alert } from "../../src/types/api";
import { connectWs, signup } from "../helpers";

// ---- local fixtures (this file only) ----

type U = { user: { id: string } };

const rtFor = (e: typeof env): Runtime => ({ env: e, waitUntil: () => {} });

/** Space with owner + members (role member); returns spaceId. */
async function space(owner: U, members: U[] = []): Promise<string> {
  const id = newId();
  const t = Date.now();
  await env.DB.batch([
    env.DB.prepare("INSERT INTO spaces (id, name, owner_id, member_count, created_at, updated_at) VALUES (?1, 'test', ?2, ?3, ?4, ?4)").bind(id, owner.user.id, members.length + 1, t),
    env.DB.prepare("INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?1, ?2, 'owner', ?3)").bind(id, owner.user.id, t),
    ...members.map((m) => env.DB.prepare("INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?1, ?2, 'member', ?3)").bind(id, m.user.id, t)),
  ]);
  return id;
}

/** Public text channel in a space; returns channelId. */
async function publicText(spaceId: string): Promise<string> {
  const id = newId();
  const t = Date.now();
  const owner = await env.DB.prepare("SELECT owner_id FROM spaces WHERE id = ?1").bind(spaceId).first<{ owner_id: string }>();
  await env.DB.prepare("INSERT INTO channels (id, kind, space_id, name, is_private, created_by, member_count, created_at, updated_at) VALUES (?1, 'text', ?2, ?3, 0, ?4, 0, ?5, ?5)")
    .bind(id, spaceId, "c" + id.slice(-10).toLowerCase(), owner!.owner_id, t)
    .run();
  return id;
}

/** Shape only matters to the client; the hub frames whatever data it gets. */
const fakeMessage = (channelId: string) => ({ id: newId(), channel_id: channelId, body: "hi" });

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
    template: null,
  };
}

describe("notify mode", () => {
  it("daemon-only user gets alert but not message.created", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const s = await space(a, [b]);
    const ch = await publicText(s);
    const d = await connectWs(b.token, { mode: "notify" });
    const r = rtFor(env);
    await publishEvent(r, { channel: ch }, "message.created", { message: fakeMessage(ch) } as any);
    await publishEvent(r, { users: [b.user.id] }, "alert", { alert: fakeAlert() });
    await d.next("alert");
    expect(d.frames.some((f) => f.type === "message.created")).toBe(false);
    expect((await env.USER_HUB.getByName(b.user.id).inspect()).seq).toBe(1); // message.created never reached the hub
  });

  it("device.tui_state toggles and hello.local_tui_connected", async () => {
    const u = await signup();
    const dev = newId();
    const d = await connectWs(u.token, { mode: "notify", deviceId: dev });
    expect(d.frames[0].data.local_tui_connected).toBe(false);
    const t = await connectWs(u.token, { deviceId: dev });
    const on = await d.next("device.tui_state");
    expect(on.data).toEqual({ device_id: dev, tui_connected: true });
    expect(on.seq).toBeNull(); // volatile
    const d2 = await connectWs(u.token, { mode: "notify", deviceId: dev });
    expect(d2.frames[0].data.local_tui_connected).toBe(true);
    expect(t.frames.some((f) => f.type === "device.tui_state")).toBe(false); // full sockets never get it
    t.close(1000);
    expect((await d.next("device.tui_state", (x) => !x.tui_connected)).data.tui_connected).toBe(false);
  });

  it("device.tui_state goes only to the same device, and only on 0<->1 full transitions", async () => {
    const u = await signup();
    const dev = newId();
    const d = await connectWs(u.token, { mode: "notify", deviceId: dev });
    const other = await connectWs(u.token, { mode: "notify" }); // a different device
    const t1 = await connectWs(u.token, { deviceId: dev });
    await d.next("device.tui_state", (x) => x.tui_connected);
    const t2 = await connectWs(u.token, { deviceId: dev }); // 1 -> 2: no signal
    t2.close(1000); // 2 -> 1: no signal
    await new Promise((r) => setTimeout(r, 150));
    t1.close(1000); // 1 -> 0
    await d.next("device.tui_state", (x) => !x.tui_connected);
    await new Promise((r) => setTimeout(r, 100));
    expect(d.frames.filter((f) => f.type === "device.tui_state").map((f) => f.data.tui_connected)).toEqual([true, false]);
    expect(other.frames.some((f) => f.type === "device.tui_state")).toBe(false);
  });

  it("notify mode rejects typing/idle/watch", async () => {
    const u = await signup();
    const d = await connectWs(u.token, { mode: "notify" });
    d.send({ type: "idle", ref: "i1", data: { idle: true } });
    expect((await d.next("reply", (x) => x.ref === "i1")).data.error.code).toBe("not_allowed_in_mode");
  });
});
