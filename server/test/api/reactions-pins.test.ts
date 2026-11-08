import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { newId } from "../../src/lib/ids";
import { api, connectWs, makeFriends, makeSpace, signup, type TestUser } from "../helpers";

// ---- local fixtures (this file only) ----

/** Two friends and their 1:1 dm channel id. */
async function dmPair(): Promise<{ a: TestUser; b: TestUser; ch: string }> {
  const [a, b] = await Promise.all([signup(), signup()]);
  await makeFriends(a, b);
  const r = await api(a.token).post<any>("/v1/dms", { users: [`@${b.handle}`] });
  if (r.status !== 201) throw new Error(`dm create failed: ${r.status} ${JSON.stringify(r.body)}`);
  return { a, b, ch: r.body.channel.id };
}

/** Sends a text message as `u` in `ch` and returns it (throws unless 201). */
async function post(u: TestUser, ch: string, body: string): Promise<any> {
  const r = await api(u.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: crypto.randomUUID().slice(0, 12), body });
  if (r.status !== 201) throw new Error(`post failed: ${r.status} ${JSON.stringify(r.body)}`);
  return r.body.message;
}

/** n top-level messages by `u` in `ch`, each pinned by `u` (one batch, INSERT … SELECT FROM json_each). */
async function seedPinnedMessagesSql(ch: string, u: TestUser, n: number): Promise<void> {
  const t = Date.now();
  const ids = JSON.stringify(Array.from({ length: n }, () => newId(t)));
  await env.DB.batch([
    env.DB.prepare(
      "INSERT INTO messages (id, channel_id, author_id, kind, body, created_at) SELECT j.value, ?2, ?3, 'text', 'seed', ?4 FROM json_each(?1) j",
    ).bind(ids, ch, u.user.id, t),
    env.DB.prepare("INSERT INTO message_pins (channel_id, message_id, pinned_by, pinned_at) SELECT ?2, j.value, ?3, ?4 FROM json_each(?1) j").bind(
      ids,
      ch,
      u.user.id,
      t,
    ),
  ]);
}

const react = (msgId: string, e: string) => `/v1/messages/${msgId}/reactions/${encodeURIComponent(e)}`;

describe("reactions", () => {
  it("Review Focus 4: multi-codepoint single graphemes ok, two graphemes / text 400", async () => {
    const { a, b, ch } = await dmPair();
    const msg = (await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: "x", body: "hi" })).body.message;
    const cb = await connectWs(b.token);
    for (const e of ["👍🏽", "👨‍👩‍👧", "🇰🇷"]) expect((await api(a.token).put(react(msg.id, e))).status).toBe(204);
    for (const e of ["👍👍", "ab", "가"]) {
      const r = await api(a.token).put<any>(react(msg.id, e));
      expect(r.status).toBe(400);
      expect(r.body.error.details.fields.emoji).toBe("invalid");
    }
    expect((await cb.next("reaction.added")).data).toMatchObject({ emoji: "👍🏽", user_id: a.user.id, count: 1, thread_root_id: null });
    await api(b.token).put(react(msg.id, "👍🏽"));
    const m = (await api(b.token).get<any>(`/v1/messages/${msg.id}`)).body.message;
    expect(m.reactions[0]).toEqual({ emoji: "👍🏽", count: 2, me: true });
    expect(m.reactions.map((r: any) => r.emoji)).toEqual(["👍🏽", "👨‍👩‍👧", "🇰🇷"]);
    await api(a.token).del(react(msg.id, "👍🏽"));
    expect((await cb.next("reaction.removed")).data.count).toBe(1);
    expect((await api(a.token).get<any>(react(msg.id, "👍🏽"))).body.items.map((u: any) => u.id)).toEqual([b.user.id]);
  });

  it("21st distinct emoji -> 422", async () => {
    const { a, ch } = await dmPair();
    const msg = (await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: "x", body: "hi" })).body.message;
    const emojis = [..."😀😁😂🤣😃😄😅😆😉😊😋😎😍😘🥰😗😙😚🙂🤗🤩"];
    for (const e of emojis.slice(0, 20)) await api(a.token).put(react(msg.id, e));
    expect((await api(a.token).put<any>(react(msg.id, emojis[20]!))).body.error.details).toEqual({ what: "reactions_per_message", limit: 20 });
  });
});

describe("pins", () => {
  it("pins: text needs admin, dm any member; system message; list newest first", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { generalChannelId: g } = await makeSpace(o, [m]);
    const [m1, m2] = [await post(o, g, "a"), await post(o, g, "b")];
    expect((await api(m.token).put(`/v1/messages/${m1.id}/pin`)).status).toBe(403);
    const cm = await connectWs(m.token);
    expect((await api(o.token).put(`/v1/messages/${m1.id}/pin`)).status).toBe(204);
    await api(o.token).put(`/v1/messages/${m2.id}/pin`);
    expect((await cm.next("message.pinned")).data).toEqual({ channel_id: g, message_id: m1.id, pinned_by: o.user.id });
    expect((await cm.next("message.created", (d) => d.message.kind === "system")).data.message.system).toMatchObject({
      event: "message_pinned",
      actor_id: o.user.id,
      data: { message_id: m1.id },
    });
    expect((await api(m.token).get<any>(`/v1/channels/${g}/pins`)).body.items.map((x: any) => x.id)).toEqual([m2.id, m1.id]);
    expect((await api(m.token).del(`/v1/messages/${m1.id}/pin`)).status).toBe(403);
    expect((await api(o.token).del(`/v1/messages/${m1.id}/pin`)).status).toBe(204);
    expect((await cm.next("message.unpinned")).data).toEqual({ channel_id: g, message_id: m1.id, unpinned_by: o.user.id });
    expect((await api(m.token).get<any>(`/v1/channels/${g}/pins`)).body.items.map((x: any) => x.id)).toEqual([m2.id]);
    const { a, ch } = await dmPair();
    const d = await post(a, ch, "dm");
    expect((await api(a.token).put(`/v1/messages/${d.id}/pin`)).status).toBe(204);
    const reply = (await api(a.token).post<any>(`/v1/messages/${d.id}/thread`, { client_nonce: "t", body: "reply" })).body.message;
    expect((await api(a.token).put(`/v1/messages/${reply.id}/pin`)).status).toBe(400); // top-level only
  });

  it("51st pin in a channel -> 422 pins_per_channel", async () => {
    const { a, ch } = await dmPair();
    await seedPinnedMessagesSql(ch, a, 50); // local fixture: 50 messages + message_pins rows (INSERT … SELECT FROM json_each)
    const extra = await post(a, ch, "one more");
    expect((await api(a.token).put<any>(`/v1/messages/${extra.id}/pin`)).body.error.details).toEqual({ what: "pins_per_channel", limit: 50 });
  });
});
