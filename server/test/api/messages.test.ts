import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { newId } from "../../src/lib/ids";
import { api, connectWs, makeFriends, makeSpace, signup, uniqueHandle, type TestUser } from "../helpers";

// ---- local fixtures (this file only) ----

/** Two friends and their 1:1 dm channel id. */
async function dmPair(): Promise<{ a: TestUser; b: TestUser; ch: string }> {
  const [a, b] = await Promise.all([signup(), signup()]);
  await makeFriends(a, b);
  const r = await api(a.token).post<any>("/v1/dms", { users: [`@${b.handle}`] });
  if (r.status !== 201) throw new Error(`dm create failed: ${r.status} ${JSON.stringify(r.body)}`);
  return { a, b, ch: r.body.channel.id };
}

/** Lets fire-and-forget fan-out land before asserting that something did NOT arrive. */
const settle = (ms = 150) => new Promise<void>((r) => setTimeout(r, ms));

/** n users (with settings) joined to the space, user_presence.status='online' (liveness left stale: no sockets to wake). */
async function seedOnlineMembersSql(spaceId: string, n: number): Promise<void> {
  const t = Date.now();
  const list = JSON.stringify(Array.from({ length: n }, () => ({ id: newId(), handle: uniqueHandle("om") })));
  await env.DB.batch([
    env.DB.prepare(
      `INSERT INTO users (id, handle, display_name, password_hash, created_at, updated_at)
       SELECT json_extract(j.value, '$.id'), json_extract(j.value, '$.handle'), 'seed', 'x', ?2, ?2 FROM json_each(?1) j`,
    ).bind(list, t),
    env.DB.prepare("INSERT INTO user_settings (user_id, updated_at) SELECT json_extract(j.value, '$.id'), ?2 FROM json_each(?1) j").bind(list, t),
    env.DB.prepare(
      "INSERT INTO user_presence (user_id, status, updated_at) SELECT json_extract(j.value, '$.id'), 'online', ?2 FROM json_each(?1) j",
    ).bind(list, t),
    env.DB.prepare(
      "INSERT INTO space_members (space_id, user_id, role, joined_at) SELECT ?2, json_extract(j.value, '$.id'), 'member', ?3 FROM json_each(?1) j",
    ).bind(list, spaceId, t),
    env.DB.prepare("UPDATE spaces SET member_count = member_count + ?2 WHERE id = ?1").bind(spaceId, n),
  ]);
}

describe("send / list / get", () => {
  it("send 201, duplicate nonce 200, event to both", async () => {
    const { a, b, ch } = await dmPair();
    const cb = await connectWs(b.token);
    const body = { client_nonce: "n-1", body: "  안녕 **세상**  " };
    const r1 = await api(a.token).post<any>(`/v1/channels/${ch}/messages`, body);
    expect(r1.status).toBe(201);
    expect(r1.body.message).toMatchObject({
      kind: "text",
      body: "안녕 **세상**",
      client_nonce: "n-1",
      thread: null,
      reactions: [],
      pinned: false,
      deleted: false,
      edited_at: null,
    });
    const r2 = await api(a.token).post<any>(`/v1/channels/${ch}/messages`, body);
    expect(r2.status).toBe(200);
    expect(r2.body.message.id).toBe(r1.body.message.id);
    expect((await cb.next("message.created")).data.message.id).toBe(r1.body.message.id);
    expect((await api(a.token).get<any>(`/v1/channels/${ch}`)).body.channel.last_message_id).toBe(r1.body.message.id);
  });

  it("validation: empty body, 4001 chars, emoji counted per code point, bad dice", async () => {
    const { a, ch } = await dmPair();
    const p = (b: any) => api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: crypto.randomUUID().slice(0, 8), ...b });
    expect((await p({ body: "   " })).body.error.details.fields.body).toBe("too_short");
    expect((await p({ body: "가".repeat(4001) })).body.error.details.fields.body).toBe("too_long");
    expect((await p({ body: "😀".repeat(4000) })).status).toBe(201);
    expect((await p({ kind: "roll", body: "2d1" })).body.error.details.fields.body).toBe("invalid");
    const roll = (await p({ kind: "roll", body: "2d6+1" })).body.message;
    expect(roll.roll).toMatchObject({ expr: "2d6+1", modifier: 1 });
    expect(roll.roll.rolls).toHaveLength(2);
    const poll = (await p({ kind: "poll", poll: { question: "점심?", options: ["국밥", "라멘"] } })).body.message;
    expect(poll.poll).toMatchObject({
      question: "점심?",
      multi: false,
      closed: false,
      my_votes: [],
      options: [
        { idx: 0, text: "국밥", vote_count: 0 },
        { idx: 1, text: "라멘", vote_count: 0 },
      ],
    });
  });

  it("pagination: latest, before, after, around; ascending; has_more flags", async () => {
    const { a, ch } = await dmPair();
    const ids: string[] = [];
    for (let i = 0; i < 12; i++) ids.push((await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: `p${i}`, body: `m${i}` })).body.message.id);
    const latest = (await api(a.token).get<any>(`/v1/channels/${ch}/messages?limit=5`)).body;
    expect(latest.items.map((m: any) => m.id)).toEqual(ids.slice(7));
    expect(latest).toMatchObject({ has_more_before: true, has_more_after: false });
    expect((await api(a.token).get<any>(`/v1/channels/${ch}/messages?before=${ids[7]}&limit=5`)).body.items.map((m: any) => m.id)).toEqual(ids.slice(2, 7));
    expect((await api(a.token).get<any>(`/v1/channels/${ch}/messages?after=${ids[9]}`)).body.items.map((m: any) => m.id)).toEqual(ids.slice(10));
    const ar = (await api(a.token).get<any>(`/v1/channels/${ch}/messages?around=${ids[5]}&limit=4`)).body.items.map((m: any) => m.id);
    expect(ar).toContain(ids[5]);
    expect(ar).toEqual([...ar].sort());
    expect((await api(a.token).get(`/v1/channels/${ch}/messages?before=${ids[1]}&after=${ids[0]}`)).status).toBe(400);
  });

  it("non-member gets 404; read-only dm send -> 403 read_only", async () => {
    const { a, b, ch } = await dmPair();
    const x = await signup();
    expect((await api(x.token).get(`/v1/channels/${ch}/messages`)).status).toBe(404);
    await api(a.token).del(`/v1/friends/@${b.handle}`);
    expect((await api(b.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: "z", body: "hi" })).body.error.code).toBe("read_only");
  });

  it("before/after flags, around split, deleted messages without replies are hidden", async () => {
    const { a, ch } = await dmPair();
    const ids: string[] = [];
    for (let i = 0; i < 6; i++) ids.push((await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: `q${i}`, body: `m${i}` })).body.message.id);
    const before = (await api(a.token).get<any>(`/v1/channels/${ch}/messages?before=${ids[3]}&limit=2`)).body;
    expect(before).toMatchObject({ has_more_before: true, has_more_after: true });
    expect(before.items.map((m: any) => m.id)).toEqual(ids.slice(1, 3));
    const after = (await api(a.token).get<any>(`/v1/channels/${ch}/messages?after=${ids[1]}&limit=2`)).body;
    expect(after).toMatchObject({ has_more_before: true, has_more_after: true });
    expect(after.items.map((m: any) => m.id)).toEqual(ids.slice(2, 4));
    const around = (await api(a.token).get<any>(`/v1/channels/${ch}/messages?around=${ids[3]}&limit=3`)).body;
    expect(around.items.map((m: any) => m.id)).toEqual(ids.slice(2, 5)); // ceil(3/2)=2 at/before + floor(3/2)=1 after
    expect(around).toMatchObject({ has_more_before: true, has_more_after: true });
    await env.DB.prepare("UPDATE messages SET deleted_at = ?2, body = '' WHERE id = ?1").bind(ids[4], Date.now()).run();
    await env.DB.prepare("UPDATE messages SET deleted_at = ?2, body = '', reply_count = 1 WHERE id = ?1").bind(ids[5], Date.now()).run();
    const items = (await api(a.token).get<any>(`/v1/channels/${ch}/messages`)).body.items;
    expect(items.map((m: any) => m.id)).not.toContain(ids[4]);
    expect(items.find((m: any) => m.id === ids[5])).toMatchObject({ deleted: true, body: "" });
    expect((await api(a.token).get(`/v1/channels/${ch}/messages?around=nope`)).status).toBe(400);
  });

  it("GET /messages/:id needs read access; sender's read pointer advances", async () => {
    const { a, b, ch } = await dmPair();
    const m = (await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: "g1", kind: "me", body: "waves" })).body.message;
    expect((await api(b.token).get<any>(`/v1/messages/${m.id}`)).body.message).toMatchObject({ id: m.id, kind: "me", body: "waves", channel_id: ch });
    expect((await api((await signup()).token).get(`/v1/messages/${m.id}`)).status).toBe(404);
    const mine = (await api(a.token).get<any>(`/v1/channels/${ch}`)).body.channel.my;
    expect(mine).toMatchObject({ last_read_message_id: m.id, unread_count: 0 });
    expect((await api(b.token).get<any>(`/v1/channels/${ch}`)).body.channel.my.unread_count).toBe(1);
  });

  it("poll needs poll input; body ignored", async () => {
    const { a, ch } = await dmPair();
    const r = await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: "pl0", kind: "poll", body: "" });
    expect(r.body.error.details.fields.poll).toBe("required");
    const bad = await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: "pl1", kind: "poll", poll: { question: "q", options: ["only"] } });
    expect(bad.status).toBe(400);
    const ok = await api(a.token).post<any>(`/v1/channels/${ch}/messages`, {
      client_nonce: "pl2",
      kind: "poll",
      body: "ignored",
      poll: { question: "q", options: ["x", "y", "z"], multi: true, duration_sec: 60 },
    });
    expect(ok.status).toBe(201);
    expect(ok.body.message).toMatchObject({ kind: "poll", body: "", poll: { context_type: "message", context_id: ok.body.message.id, multi: true, voter_count: 0 } });
    expect(ok.body.message.poll.closes_at).not.toBeNull();
  });
});

describe("system messages", () => {
  it("group dm member add/remove posts group_member_added/removed", async () => {
    const [o, x, y, z] = await Promise.all([signup(), signup(), signup(), signup()]);
    await Promise.all([makeFriends(o, x), makeFriends(o, y), makeFriends(o, z)]);
    const g = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`, `@${y.handle}`] })).body.channel;
    const cx = await connectWs(x.token);
    expect((await api(o.token).post(`/v1/channels/${g.id}/members`, { users: [`@${z.handle}`] })).status).toBe(200);
    const added = (await cx.next("message.created")).data.message;
    expect(added).toMatchObject({ kind: "system", body: "", client_nonce: null, system: { event: "group_member_added", actor_id: o.user.id, target_user_id: z.user.id } });
    expect((await api(o.token).del(`/v1/channels/${g.id}/members/@${y.handle}`)).status).toBe(204);
    expect((await cx.next("message.created")).data.message.system).toMatchObject({ event: "group_member_removed", actor_id: o.user.id, target_user_id: y.user.id });
    const items = (await api(x.token).get<any>(`/v1/channels/${g.id}/messages`)).body.items;
    expect(items.map((m: any) => m.system?.event)).toEqual(["group_member_added", "group_member_removed"]);
    expect((await api(x.token).get<any>(`/v1/channels/${g.id}`)).body.channel.my.unread_count).toBe(0);
  });
});

describe("mentions", () => {
  it("@handle resolves only for readers without blocks; author in mentions[] but not notified", async () => {
    const [o, m, out, blk] = await Promise.all([signup(), signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [m, blk]);
    const priv = (await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "p", is_private: true, members: [`@${blk.handle}`] })).body.channel;
    await api(blk.token).put(`/v1/blocks/@${o.handle}`);
    const cm = await connectWs(m.token);
    const msg = (
      await api(o.token).post<any>(`/v1/channels/${priv.id}/messages`, { client_nonce: "m1", body: `@${m.handle} @${out.handle} @${blk.handle} @${o.handle} hi` })
    ).body.message;
    expect(msg.mentions.map((x: any) => x.handle)).toEqual([o.handle]); // m: not a member of private channel; out: not in space; blk: blocked
    await settle();
    expect(cm.frames.some((f) => f.type === "notification.created")).toBe(false);
  });

  it("mention in channel -> message_mention inbox + mention alert; @here expands to online non-dnd only", async () => {
    const [o, on, dnd, off] = await Promise.all([signup(), signup(), signup(), signup()]);
    const { generalChannelId: g } = await makeSpace(o, [on, dnd, off]);
    const [con, cdnd] = await Promise.all([connectWs(on.token), connectWs(dnd.token)]);
    await api(dnd.token).put("/v1/me/presence", { mode: "dnd" });
    const msg = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "h1", body: "@here 회의" })).body.message;
    expect(msg).toMatchObject({ mention_here: true, mentions: [] });
    expect((await con.next("notification.created")).data.notification).toMatchObject({
      type: "message_mention",
      context: { message_id: msg.id, thread_root_id: null },
    });
    expect((await con.next("alert")).data.alert.kind).toBe("mention");
    await settle();
    expect(cdnd.frames.some((f) => f.type === "notification.created")).toBe(false);
    expect((await env.DB.prepare("SELECT COUNT(*) n FROM message_mentions WHERE message_id=?").bind(msg.id).first<any>()).n).toBe(1);
  });

  it("@here in 1:1 dm does not expand; dm alert to partner", async () => {
    const { a, b, ch } = await dmPair();
    const cb = await connectWs(b.token);
    await api(a.token).post(`/v1/channels/${ch}/messages`, { client_nonce: "d1", body: "@here yo" });
    expect((await cb.next("alert")).data.alert).toMatchObject({ kind: "dm", title: a.user.display_name, body: "@here yo" });
    expect(
      (await env.DB.prepare("SELECT COUNT(*) n FROM message_mentions m JOIN messages x ON x.id=m.message_id WHERE x.channel_id=?").bind(ch).first<any>()).n,
    ).toBe(0);
  });

  it("channel notify:all gives channel_message alert (top-level only); default mentions gives none", async () => {
    const [o, m, q] = await Promise.all([signup(), signup(), signup()]);
    const { space, generalChannelId: g } = await makeSpace(o, [m, q]);
    await api(m.token).put(`/v1/channels/${g}/prefs`, { notify: "all" });
    const [cm, cq] = await Promise.all([connectWs(m.token), connectWs(q.token)]);
    const msg = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "c1", body: "**공지** 입니다" })).body.message;
    expect((await cm.next("alert")).data.alert).toMatchObject({
      kind: "channel_message",
      body: "공지 입니다",
      target: { type: "message", channel_id: g, message_id: msg.id, space_id: space.id },
    });
    await settle();
    expect(cq.frames.some((f) => f.type === "alert")).toBe(false);
    await api(o.token).post(`/v1/messages/${msg.id}/thread`, { client_nonce: "c2", body: "답글" });
    await settle();
    expect(cm.frames.filter((f) => f.type === "alert")).toHaveLength(1); // thread replies never give channel_message
  });

  it("Review Focus 2: @here with 120 online members writes 119 is_here rows and 119 inbox rows", async () => {
    const o = await signup();
    const { space, generalChannelId: g } = await makeSpace(o, []);
    await seedOnlineMembersSql(space.id, 119);
    const r = await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "h120", body: "@here 공지" });
    expect(r.status).toBe(201);
    const count = async (sql: string) => (await env.DB.prepare(sql).bind(r.body.message.id).first<any>()).n;
    expect(await count("SELECT COUNT(*) n FROM message_mentions WHERE message_id=? AND is_here=1")).toBe(119);
    expect(await count("SELECT COUNT(*) n FROM notifications WHERE type='message_mention' AND subject_id=?")).toBe(119);
  });
});

describe("edit/delete", () => {
  it("only author edits text; new mention notifies once", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { generalChannelId: g } = await makeSpace(o, [m]);
    const cm = await connectWs(m.token);
    const msg = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "e1", body: "hello" })).body.message;
    expect((await api(m.token).patch(`/v1/messages/${msg.id}`, { body: "hack" })).status).toBe(403);
    const ed = (await api(o.token).patch<any>(`/v1/messages/${msg.id}`, { body: `hello @${m.handle}` })).body.message;
    expect(ed.edited_at).not.toBeNull();
    await cm.next("message.updated");
    await cm.next("notification.created");
    await api(o.token).patch(`/v1/messages/${msg.id}`, { body: `hello again @${m.handle}` });
    await settle();
    expect(cm.frames.filter((f) => f.type === "notification.created")).toHaveLength(1);
  });

  it("delete: author / space admin ok, other member 403; event tombstone flag", async () => {
    const [o, m, x] = await Promise.all([signup(), signup(), signup()]);
    const { generalChannelId: g } = await makeSpace(o, [m, x]);
    const cx = await connectWs(x.token);
    const msg = (await api(m.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "d1", body: "bye" })).body.message;
    expect((await api(x.token).del(`/v1/messages/${msg.id}`)).status).toBe(403);
    expect((await api(o.token).del(`/v1/messages/${msg.id}`)).status).toBe(204);
    expect((await cx.next("message.deleted")).data).toEqual({ channel_id: g, message_id: msg.id, thread_root_id: null, tombstone: false });
    expect((await api(o.token).get<any>(`/v1/channels/${g}/messages`)).body.items.map((i: any) => i.id)).not.toContain(msg.id);
  });
});
