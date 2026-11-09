// test/api/notifications.test.ts
import { env } from "cloudflare:test";
import { expect, test } from "vitest";
import { api, signup, connectWs, makeFriends, makeSpace } from "../helpers";
const inbox = async (t: any, q = "") => (await api(t.token).get<any>(`/v1/notifications${q}`)).body;

test("friend_request creates inbox row, notification.created and alert on daemon socket", async () => {
  const a = await signup(); const b = await signup();
  const d = await connectWs(b.token, { mode: "notify" });
  const r = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
  const n = (await inbox(b)).items[0];
  expect(n).toMatchObject({ type: "friend_request", actor: { id: a.user.id }, subject: { type: "friend_request", id: r.body.request.id }, read: false });
  expect((await d.next("notification.created")).data.unread_count).toBe(1);
  expect((await d.next("alert")).data.alert).toMatchObject({ kind: "friend_request", suppressed: false });
});
test("friend_accepted, space_invite, message_mention, thread_reply hooks", async () => {
  const a = await signup(); const b = await signup(); const c = await signup();
  const req = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
  await api(b.token).post(`/v1/friends/requests/${req.body.request.id}/accept`);
  expect((await inbox(a)).items[0]).toMatchObject({ type: "friend_accepted", subject: { type: "user", id: b.user.id } });
  const { space, generalChannelId } = await makeSpace(a, [c]);
  const inv = await api(a.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${b.handle}` });
  expect((await inbox(b)).items[0]).toMatchObject({ type: "space_invite", subject: { type: "space_direct_invite", id: inv.body.invite.id }, context: { space_id: space.id, space_name: space.name } });
  const m = await api(a.token).post<any>(`/v1/channels/${generalChannelId}/messages`, { client_nonce: "n1", body: `@${c.handle} 점심?` });
  expect((await inbox(c)).items[0]).toMatchObject({ type: "message_mention", context: { channel_id: generalChannelId, message_id: m.body.message.id, thread_root_id: null, space_id: space.id } });
  const rep = await api(c.token).post<any>(`/v1/messages/${m.body.message.id}/thread`, { client_nonce: "n2", body: "ㅇㅋ" });
  expect((await inbox(a)).items[0]).toMatchObject({ type: "thread_reply", subject: { type: "message", id: rep.body.message.id }, context: { thread_root_id: m.body.message.id } });
});
// Thread replies in a 1:1 dm and a group dm reach the root author's inbox: channelReadersAmong once bound
// space_id / is_private for every kind, D1 rejected the extra bindings and the whole fan-out was lost — the
// inbox rows and every member's `dm` alert alike. One alert per person: dm > mention > thread_reply.
test("thread_reply in a 1:1 dm and a group dm reaches the root author", async () => {
  const a = await signup(); const b = await signup(); const c = await signup();
  await makeFriends(a, b); await makeFriends(a, c);
  const wa = await connectWs(a.token);
  // the alerts a got for message id, once a later sentinel alert from the same hub has arrived
  const alertsFor = async (messageId: string, channelId: string, nonce: string) => {
    const s = (await api(b.token).post<any>(`/v1/channels/${channelId}/messages`, { client_nonce: nonce, body: "sentinel" })).body.message;
    await wa.next("alert", (d) => d.alert.target.message_id === s.id);
    return wa.frames.filter((f) => f.type === "alert" && f.data.alert.target.message_id === messageId).map((f) => f.data.alert.kind);
  };
  const dm = (await api(a.token).post<any>("/v1/dms", { users: [`@${b.handle}`] })).body.channel;
  const root = (await api(a.token).post<any>(`/v1/channels/${dm.id}/messages`, { client_nonce: "d1", body: "주말에 뭐 해?" })).body.message;
  const rep = (await api(b.token).post<any>(`/v1/messages/${root.id}/thread`, { client_nonce: "d2", body: "등산!" })).body.message;
  expect((await inbox(a)).items[0]).toMatchObject({ type: "thread_reply", actor: { id: b.user.id }, subject: { type: "message", id: rep.id },
    context: { channel_id: dm.id, message_id: rep.id, thread_root_id: root.id } });
  // the thread_reply row carries no alert of its own: a gets the dm alert, exactly once
  expect(await alertsFor(rep.id, dm.id, "ds1")).toEqual(["dm"]);
  // a reply that @mentions the root author in a 1:1 dm still leaves a thread_reply row (a 1:1 dm has no
  // message_mention notifications, though message_mentions records the explicit mention)
  const rep2 = (await api(b.token).post<any>(`/v1/messages/${root.id}/thread`, { client_nonce: "d3", body: `@${a.handle} 같이 갈래?` })).body.message;
  expect((await inbox(a)).items[0]).toMatchObject({ type: "thread_reply", subject: { type: "message", id: rep2.id } });
  expect(await alertsFor(rep2.id, dm.id, "ds2")).toEqual(["dm"]);

  const g = (await api(a.token).post<any>("/v1/dms", { users: [`@${b.handle}`, `@${c.handle}`] })).body.channel;
  const groot = (await api(a.token).post<any>(`/v1/channels/${g.id}/messages`, { client_nonce: "g1", body: "점심?" })).body.message;
  const grep = (await api(b.token).post<any>(`/v1/messages/${groot.id}/thread`, { client_nonce: "g2", body: "좋아" })).body.message;
  expect((await inbox(a)).items[0]).toMatchObject({ type: "thread_reply", subject: { type: "message", id: grep.id }, context: { channel_id: g.id, thread_root_id: groot.id } });
  expect(await alertsFor(grep.id, g.id, "gs1")).toEqual(["dm"]);
  // c neither wrote the root nor replied: no thread_reply
  expect((await inbox(c)).items.filter((n: any) => n.type === "thread_reply")).toEqual([]);
  // a group dm reply that @mentions the root author: the message_mention row alone, and still the one dm alert
  const grep2 = (await api(b.token).post<any>(`/v1/messages/${groot.id}/thread`, { client_nonce: "g3", body: `@${a.handle} 몇 시?` })).body.message;
  const rows = (await inbox(a)).items.filter((n: any) => n.subject.id === grep2.id);
  expect(rows.map((n: any) => n.type)).toEqual(["message_mention"]);
  expect(rows[0]).toMatchObject({ context: { channel_id: g.id, message_id: grep2.id, thread_root_id: groot.id } });
  expect(await alertsFor(grep2.id, g.id, "gs2")).toEqual(["dm"]);
});
test("notify helper: self skipped, blocked actor skipped, dedupe_key collapses", async () => {
  const a = await signup(); const b = await signup();
  const { notify } = await import("../../src/lib/notify");
  const rt = { env, waitUntil: () => {} };
  const base = { userId: b.user.id, type: "follow" as const, actorId: a.user.id, subject: { type: "user" as const, id: a.user.id }, dedupeKey: `follow:${b.user.id}:${a.user.id}`, alert: null };
  await notify(rt, base); await notify(rt, base);
  await notify(rt, { ...base, userId: a.user.id, dedupeKey: undefined });
  expect((await inbox(b)).items.length).toBe(1);
  expect((await inbox(a)).items.length).toBe(0);
  await api(b.token).put(`/v1/blocks/@${a.handle}`);
  await notify(rt, { ...base, type: "follow_request", dedupeKey: undefined });
  const n = await env.DB.prepare("SELECT COUNT(*) AS n FROM notifications WHERE user_id = ?").bind(b.user.id).first<{ n: number }>();
  expect(n!.n).toBe(1);
});
test("list hides notifications whose actor I blocked; unread_only; read by ids/before/all", async () => {
  const a = await signup(); const b = await signup(); const c = await signup();
  for (const x of [a, c]) await api(x.token).post("/v1/friends/requests", { user: `@${b.handle}` });
  let list = await inbox(b);
  expect(list.unread_count).toBe(2);
  await api(b.token).put(`/v1/blocks/@${c.handle}`);
  list = await inbox(b);
  expect(list.items.map((n: any) => n.actor.id)).toEqual([a.user.id]);
  expect((await api(b.token).get<any>("/v1/notifications/unread-count")).body.unread_count).toBe(1);
  const w = await connectWs(b.token);
  const d = await signup();
  await api(d.token).post("/v1/friends/requests", { user: `@${b.handle}` });
  // event badge uses the same block-filtered count as the REST route: a + d, c's unread row excluded
  expect((await w.next("notification.created")).data.unread_count).toBe(2);
  expect((await api(b.token).get<any>("/v1/notifications/unread-count")).body.unread_count).toBe(2);
  list = await inbox(b);
  expect(list.items[0].actor.id).toBe(d.user.id);
  const r = await api(b.token).post<any>("/v1/notifications/read", { ids: [list.items[0].id] });
  expect(r.body.unread_count).toBe(1);
  expect((await w.next("notifications.read")).data).toEqual({ ids: [list.items[0].id], all: false, before: null, unread_count: 1 });
  expect((await inbox(b, "?unread_only=true")).items.map((n: any) => n.actor.id)).toEqual([a.user.id]);
  expect((await api(b.token).post("/v1/notifications/read", {})).status).toBe(400);
  expect((await api(b.token).post("/v1/notifications/read", { ids: [] })).status).toBe(400);
  expect((await api(b.token).post<any>("/v1/notifications/read", { all: true })).body.unread_count).toBe(0);
});
test("cursor pagination newest first", async () => {
  const b = await signup();
  for (let i = 0; i < 3; i++) { const x = await signup(); await api(x.token).post("/v1/friends/requests", { user: `@${b.handle}` }); }
  const p1 = await inbox(b, "?limit=2");
  expect(p1.items.length).toBe(2); expect(p1.next_cursor).not.toBeNull();
  const p2 = await inbox(b, `?limit=2&cursor=${p1.next_cursor}`);
  expect(p2.items.length).toBe(1); expect(p2.next_cursor).toBeNull();
  expect(p1.items[0].id > p1.items[1].id && p1.items[1].id > p2.items[0].id).toBe(true);
});
