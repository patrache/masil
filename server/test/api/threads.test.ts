import { describe, it, expect } from "vitest";
import { api, connectWs, makeSpace, signup } from "../helpers";

describe("threads", () => {
  it("reply updates root summary and notifies root author + participants", async () => {
    const [o, a, b] = await Promise.all([signup(), signup(), signup()]);
    const { generalChannelId: g } = await makeSpace(o, [a, b]);
    const root = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "r", body: "배포 몇 시?" })).body.message;
    const [co, ca] = await Promise.all([connectWs(o.token), connectWs(a.token)]);
    const r1 = (await api(a.token).post<any>(`/v1/messages/${root.id}/thread`, { client_nonce: "t1", body: "3시" })).body.message;
    expect(r1.thread_root_id).toBe(root.id);
    expect((await co.next("thread.updated")).data.thread).toMatchObject({ reply_count: 1, participant_ids: [a.user.id, o.user.id] });
    expect((await co.next("alert", (d) => d.alert.kind === "thread_reply")).data.alert.target.thread_root_id).toBe(root.id);
    await api(b.token).post(`/v1/messages/${r1.id}/thread`, { client_nonce: "t2", body: "ok" }); // reply-to-reply -> same root
    expect((await ca.next("notification.created", (d) => d.notification.type === "thread_reply")).data.notification).toMatchObject({
      subject: { type: "message" },
      context: { channel_id: g, thread_root_id: root.id },
    });
    const t = (await api(o.token).get<any>(`/v1/messages/${root.id}/thread`)).body;
    expect(t.root.thread.reply_count).toBe(2);
    expect(t.items).toHaveLength(2);
    expect((await api(o.token).get<any>(`/v1/channels/${g}/messages`)).body.items.every((m: any) => m.thread_root_id === null)).toBe(true);
  });

  it("thread mention counts only in inbox, not channel mention_count; poll reply 400", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const { generalChannelId: g } = await makeSpace(o, [a]);
    const root = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "r", body: "q" })).body.message;
    await api(o.token).post(`/v1/messages/${root.id}/thread`, { client_nonce: "t", body: `@${a.handle} 봐줘` });
    expect((await api(a.token).get<any>(`/v1/channels/${g}`)).body.channel.my.mention_count).toBe(0);
    expect(
      (await api(o.token).post(`/v1/messages/${root.id}/thread`, { client_nonce: "p", kind: "poll", body: "", poll: { question: "q", options: ["a", "b"] } }))
        .status,
    ).toBe(400);
  });

  it("deleted root with replies stays as tombstone; deleting a reply decrements count", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const { generalChannelId: g } = await makeSpace(o, [a]);
    const root = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "r", body: "q" })).body.message;
    const rep = (await api(a.token).post<any>(`/v1/messages/${root.id}/thread`, { client_nonce: "t", body: "x" })).body.message;
    await api(o.token).del(`/v1/messages/${root.id}`);
    expect((await api(a.token).get<any>(`/v1/channels/${g}/messages`)).body.items.find((m: any) => m.id === root.id)).toMatchObject({ deleted: true, body: "" });
    const ca = await connectWs(a.token);
    await api(a.token).del(`/v1/messages/${rep.id}`);
    expect((await ca.next("thread.updated")).data.thread.reply_count).toBe(0);
  });

  // ---- additional coverage of api.md 5.11 beyond the plan's three cases ----

  it("reply: duplicate nonce 200; channel last_message and read pointer untouched; GET via reply id resolves to root", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const { generalChannelId: g } = await makeSpace(o, [a]);
    const root = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "r", body: "q" })).body.message;
    const before = (await api(o.token).get<any>(`/v1/channels/${g}`)).body.channel;
    const req = { client_nonce: "dup", body: "x" };
    const r1 = await api(a.token).post<any>(`/v1/messages/${root.id}/thread`, req);
    expect(r1.status).toBe(201);
    const r2 = await api(a.token).post<any>(`/v1/messages/${root.id}/thread`, req);
    expect(r2.status).toBe(200);
    expect(r2.body.message.id).toBe(r1.body.message.id);
    const after = (await api(o.token).get<any>(`/v1/channels/${g}`)).body.channel;
    expect(after.last_message_at).toBe(before.last_message_at);
    expect(after.my.unread_count).toBe(0);
    const t = (await api(o.token).get<any>(`/v1/messages/${r1.body.message.id}/thread`)).body;
    expect(t.root.id).toBe(root.id);
    expect(t.root.thread).toMatchObject({ reply_count: 1 });
    expect(t.items.map((m: any) => m.id)).toEqual([r1.body.message.id]);
    expect(t).toMatchObject({ has_more_before: false, has_more_after: false });
  });

  it("thread list pages: latest by default, before / after, ascending; outsiders 404", async () => {
    const [o, a, out] = await Promise.all([signup(), signup(), signup()]);
    const { generalChannelId: g } = await makeSpace(o, [a]);
    const root = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "r", body: "q" })).body.message;
    const ids: string[] = [];
    for (let i = 0; i < 5; i++) {
      ids.push((await api(i % 2 === 0 ? a.token : o.token).post<any>(`/v1/messages/${root.id}/thread`, { client_nonce: `p${i}`, body: `r${i}` })).body.message.id);
    }
    const latest = (await api(o.token).get<any>(`/v1/messages/${root.id}/thread?limit=2`)).body;
    expect(latest.items.map((m: any) => m.id)).toEqual(ids.slice(3));
    expect(latest).toMatchObject({ has_more_before: true, has_more_after: false });
    const older = (await api(o.token).get<any>(`/v1/messages/${root.id}/thread?before=${ids[3]}&limit=2`)).body;
    expect(older.items.map((m: any) => m.id)).toEqual(ids.slice(1, 3));
    expect(older).toMatchObject({ has_more_before: true, has_more_after: true });
    const newer = (await api(o.token).get<any>(`/v1/messages/${root.id}/thread?after=${ids[0]}&limit=3`)).body;
    expect(newer.items.map((m: any) => m.id)).toEqual(ids.slice(1, 4));
    expect(newer).toMatchObject({ has_more_before: true, has_more_after: true });
    expect((await api(o.token).get(`/v1/messages/${root.id}/thread?before=${ids[1]}&after=${ids[0]}`)).status).toBe(400);
    expect((await api(out.token).get(`/v1/messages/${root.id}/thread`)).status).toBe(404);
    expect((await api(out.token).post(`/v1/messages/${root.id}/thread`, { client_nonce: "o", body: "x" })).status).toBe(404);
  });

  it("a deleted root without replies cannot be replied to or opened", async () => {
    const o = await signup();
    const { generalChannelId: g } = await makeSpace(o, []);
    const root = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "r", body: "q" })).body.message;
    await api(o.token).del(`/v1/messages/${root.id}`);
    expect((await api(o.token).post(`/v1/messages/${root.id}/thread`, { client_nonce: "t", body: "x" })).status).toBe(404);
    expect((await api(o.token).get(`/v1/messages/${root.id}/thread`)).status).toBe(404);
  });

  it("deleted root with replies opens as a tombstone root; deleted replies are left out", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const { generalChannelId: g } = await makeSpace(o, [a]);
    const root = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "r", body: "q" })).body.message;
    const r1 = (await api(a.token).post<any>(`/v1/messages/${root.id}/thread`, { client_nonce: "t1", body: "x" })).body.message;
    const r2 = (await api(a.token).post<any>(`/v1/messages/${root.id}/thread`, { client_nonce: "t2", body: "y" })).body.message;
    await api(a.token).del(`/v1/messages/${r1.id}`);
    await api(o.token).del(`/v1/messages/${root.id}`);
    const t = (await api(a.token).get<any>(`/v1/messages/${root.id}/thread`)).body;
    expect(t.root).toMatchObject({ id: root.id, deleted: true, body: "" });
    expect(t.items.map((m: any) => m.id)).toEqual([r2.id]);
  });
});
