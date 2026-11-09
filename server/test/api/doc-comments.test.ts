// api.md 5.24 / events.md 4.7·4.10 (P0 Task 29): document comments — create / list / resolve / edit / delete, the
// doc.comment_created space event, and a mention alert per @handle (template doc.comment_mention, target type "doc")
// without an inbox row.
import { describe, it, expect } from "vitest";
import { api, connectWs, inOneRateWindow, makeDoc, makeSpace, signup } from "../helpers";

const fieldsOf = (r: { body: any }) => r.body.error.details.fields;
const settle = () => new Promise((r) => setTimeout(r, 150));

async function titledDoc(owner: Awaited<ReturnType<typeof signup>>, spaceId: string, type: "arch" | "apicol" = "arch") {
  const r = await api(owner.token).post<any>(`/v1/spaces/${spaceId}/docs`, { type, title: "결제 시스템 v3" });
  expect(r.status).toBe(201);
  return r.body.doc;
}

describe("doc comments (api.md 5.24)", () => {
  it("create -> 201 + doc.comment_created to the space; list oldest first; replies carry root_id", async () => {
    const [o, m] = await Promise.all([signup({ displayName: "하나" }), signup()]);
    const { space } = await makeSpace(o, [m]);
    const d = await makeDoc(o, space.id);
    const wm = await connectWs(m.token);
    const r = await api(o.token).post<any>(`/v1/docs/${d.id}/comments`, { anchor: "node:n1", body: "  여기 확인 " });
    expect(r.status).toBe(201);
    expect(r.body.comment).toMatchObject({
      doc_id: d.id,
      anchor: "node:n1",
      author: { id: o.user.id, display_name: "하나" },
      body: "여기 확인",
      root_id: null,
      resolved_at: null,
      deleted: false,
    });
    const ev = await wm.next("doc.comment_created", (x: any) => x.comment.id === r.body.comment.id);
    expect(ev.data).toEqual({ doc_id: d.id, comment: r.body.comment });

    const reply = await api(m.token).post<any>(`/v1/docs/${d.id}/comments`, { anchor: "node:n1", body: "넵", root_id: r.body.comment.id });
    expect(reply.status).toBe(201);
    expect(reply.body.comment.root_id).toBe(r.body.comment.id);

    const list = await api(m.token).get<any>(`/v1/docs/${d.id}/comments`);
    expect(list.status).toBe(200);
    expect(list.body.items.map((c: any) => c.id)).toEqual([r.body.comment.id, reply.body.comment.id]);
    expect(list.body.next_cursor).toBeNull();
    const p1 = await api(m.token).get<any>(`/v1/docs/${d.id}/comments?limit=1`);
    expect(p1.body.items.map((c: any) => c.id)).toEqual([r.body.comment.id]);
    const p2 = await api(m.token).get<any>(`/v1/docs/${d.id}/comments?limit=1&cursor=${p1.body.next_cursor}`);
    expect(p2.body.items.map((c: any) => c.id)).toEqual([reply.body.comment.id]);
    expect(p2.body.next_cursor).toBeNull();
  });

  it("validation: body 1..2000, anchor 1..128, root_id must be a root comment of the same doc", async () => {
    const o = await signup();
    const { space } = await makeSpace(o);
    const d = await makeDoc(o, space.id);
    const other = await makeDoc(o, space.id);
    const post = (docId: string, j: object) => api(o.token).post<any>(`/v1/docs/${docId}/comments`, j);
    const big = await post(d.id, { anchor: "a", body: "가".repeat(2001) });
    expect(big.status).toBe(400);
    expect(fieldsOf(big)).toEqual({ body: "too_long" });
    expect((await post(d.id, { anchor: "a", body: "가".repeat(2000) })).status).toBe(201);
    expect(fieldsOf(await post(d.id, { anchor: "a" }))).toEqual({ body: "required" });
    expect(fieldsOf(await post(d.id, { anchor: "a".repeat(129), body: "x" }))).toEqual({ anchor: "too_long" });
    expect(fieldsOf(await post(d.id, { body: "x" }))).toEqual({ anchor: "required" });
    const root = (await post(other.id, { anchor: "a", body: "x" })).body.comment;
    expect(fieldsOf(await post(d.id, { anchor: "a", body: "y", root_id: root.id }))).toEqual({ root_id: "invalid" });
    expect(fieldsOf(await post(d.id, { anchor: "a", body: "y", root_id: "01JC9ZA0000000000000000000" }))).toEqual({ root_id: "invalid" });
    const reply = (await post(other.id, { anchor: "a", body: "r", root_id: root.id })).body.comment;
    expect(fieldsOf(await post(other.id, { anchor: "a", body: "y", root_id: reply.id }))).toEqual({ root_id: "invalid" }); // replies are one level
  });

  it("@handle of a room member -> mention alert (doc.comment_mention, target doc); ko and en fallback; no inbox row", async () => {
    const [a, b] = await Promise.all([signup({ displayName: "하나" }), signup()]);
    const { space } = await makeSpace(a, [b]);
    const d = await titledDoc(a, space.id);
    const ko = await connectWs(b.token);
    const en = await connectWs(b.token, { headers: { "Accept-Language": "en" } });
    const r = await api(a.token).post<any>(`/v1/docs/${d.id}/comments`, { anchor: "node:n1", body: `@${b.handle} 여기 봐 주세요` });
    expect(r.status).toBe(201);
    const fk = (await ko.next("alert", (x: any) => x.alert.kind === "mention")).data.alert;
    const fe = (await en.next("alert", (x: any) => x.alert.kind === "mention")).data.alert;
    expect(fk.template).toEqual({ id: "doc.comment_mention", params: { actor: "하나", doc: "결제 시스템 v3", space: space.name } });
    expect(fk.title).toBe("하나님이 결제 시스템 v3 댓글에서 멘션했어요");
    expect(fk.body).toBe(`@${b.handle} 여기 봐 주세요`);
    expect(fe.title).toBe(`하나 mentioned you in a comment on 결제 시스템 v3 · ${space.name}`);
    expect(fk.target).toEqual({
      type: "doc",
      id: d.id,
      space_id: space.id,
      doc_type: "arch",
      channel_id: null,
      message_id: null,
      thread_root_id: null,
      post_id: null,
    });
    expect(fk.actor.id).toBe(a.user.id);
    await settle();
    expect(ko.frames.some((f) => f.type === "notification.created")).toBe(false);
    expect((await api(b.token).get<any>("/v1/notifications")).body.items).toEqual([]);
  });

  it("apicol docs carry doc_type apicol; the author mentioning themself gets nothing", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const { space } = await makeSpace(a, [b]);
    const d = await titledDoc(a, space.id, "apicol");
    const wa = await connectWs(a.token);
    const wb = await connectWs(b.token);
    expect((await api(a.token).post(`/v1/docs/${d.id}/comments`, { anchor: "r1", body: `@${a.handle} @${b.handle}` })).status).toBe(201);
    expect((await wb.next("alert", (x: any) => x.alert.kind === "mention")).data.alert.target.doc_type).toBe("apicol");
    await settle();
    expect(wa.frames.some((f) => f.type === "alert")).toBe(false);
  });

  it("no alert for a blocked person or someone outside the room", async () => {
    const [a, b, blocked, outsider] = await Promise.all([signup(), signup(), signup(), signup()]);
    const { space } = await makeSpace(a, [b, blocked]);
    expect((await api(blocked.token).put(`/v1/blocks/@${a.handle}`)).status).toBe(204);
    const d = await makeDoc(a, space.id);
    const wb = await connectWs(b.token);
    const wx = await connectWs(blocked.token);
    const wo = await connectWs(outsider.token);
    const body = `@${blocked.handle} @${outsider.handle} @${b.handle}`;
    expect((await api(a.token).post(`/v1/docs/${d.id}/comments`, { anchor: "x", body })).status).toBe(201);
    await wb.next("alert", (x: any) => x.alert.kind === "mention");
    await settle();
    expect(wx.frames.some((f) => f.type === "alert")).toBe(false);
    expect(wo.frames.some((f) => f.type === "alert")).toBe(false);
  });

  it("PATCH: body by the author only (403 otherwise); resolved by any member; DELETE by author / owner / admin", async () => {
    const [o, c, m] = await Promise.all([signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [c, m]);
    const d = await makeDoc(c, space.id); // the doc's creator is not a comment manager
    const cm = (await api(m.token).post<any>(`/v1/docs/${d.id}/comments`, { anchor: "x", body: "원래" })).body.comment;

    const other = await api(c.token).patch<any>(`/v1/docs/${d.id}/comments/${cm.id}`, { body: "남이 고침" });
    expect(other.status).toBe(403);
    expect(other.body.error.code).toBe("forbidden");
    const mine = await api(m.token).patch<any>(`/v1/docs/${d.id}/comments/${cm.id}`, { body: "고침" });
    expect(mine.status).toBe(200);
    expect(mine.body.comment.body).toBe("고침");

    const res = await api(c.token).patch<any>(`/v1/docs/${d.id}/comments/${cm.id}`, { resolved: true });
    expect(res.status).toBe(200);
    expect(res.body.comment.resolved_at).not.toBeNull();
    const un = await api(o.token).patch<any>(`/v1/docs/${d.id}/comments/${cm.id}`, { resolved: false });
    expect(un.body.comment.resolved_at).toBeNull();
    expect(fieldsOf(await api(m.token).patch<any>(`/v1/docs/${d.id}/comments/${cm.id}`, { body: "가".repeat(2001) }))).toEqual({ body: "too_long" });

    expect((await api(c.token).del(`/v1/docs/${d.id}/comments/${cm.id}`)).status).toBe(403);
    expect((await api(m.token).del(`/v1/docs/${d.id}/comments/${cm.id}`)).status).toBe(204);
    const cm2 = (await api(m.token).post<any>(`/v1/docs/${d.id}/comments`, { anchor: "x", body: "둘" })).body.comment;
    expect((await api(o.token).del(`/v1/docs/${d.id}/comments/${cm2.id}`)).status).toBe(204); // owner
    const list = (await api(m.token).get<any>(`/v1/docs/${d.id}/comments`)).body.items;
    expect(list.map((x: any) => [x.id, x.deleted, x.body])).toEqual([
      [cm.id, true, ""],
      [cm2.id, true, ""],
    ]);
    expect((await api(m.token).patch(`/v1/docs/${d.id}/comments/${cm.id}`, { resolved: true })).status).toBe(404);
    expect((await api(m.token).del(`/v1/docs/${d.id}/comments/${cm.id}`)).status).toBe(404);
    // a comment id of another doc is 404 on this doc's path
    const d2 = await makeDoc(c, space.id);
    const c3 = (await api(m.token).post<any>(`/v1/docs/${d2.id}/comments`, { anchor: "x", body: "셋" })).body.comment;
    expect((await api(m.token).patch(`/v1/docs/${d.id}/comments/${c3.id}`, { resolved: true })).status).toBe(404);
  });

  it("outsiders get 404 on every comment path; a deleted doc's comments are 404", async () => {
    const [o, x] = await Promise.all([signup(), signup()]);
    const { space } = await makeSpace(o);
    const d = await makeDoc(o, space.id);
    const cm = (await api(o.token).post<any>(`/v1/docs/${d.id}/comments`, { anchor: "a", body: "b" })).body.comment;
    expect((await api(x.token).get(`/v1/docs/${d.id}/comments`)).status).toBe(404);
    expect((await api(x.token).post(`/v1/docs/${d.id}/comments`, { anchor: "a", body: "b" })).status).toBe(404);
    expect((await api(x.token).patch(`/v1/docs/${d.id}/comments/${cm.id}`, { resolved: true })).status).toBe(404);
    expect((await api(x.token).del(`/v1/docs/${d.id}/comments/${cm.id}`)).status).toBe(404);
    expect((await api(o.token).del(`/v1/docs/${d.id}`)).status).toBe(204);
    expect((await api(o.token).get(`/v1/docs/${d.id}/comments`)).status).toBe(404);
  });

  it("RL_DOC counts comment writes; reads are free", async () => {
    await inOneRateWindow(60, async () => {
      const o = await signup();
      const { space } = await makeSpace(o);
      const d = await makeDoc(o, space.id); // write 1
      for (let i = 2; i <= 30; i++) expect((await api(o.token).post(`/v1/docs/${d.id}/comments`, { anchor: "a", body: `c${i}` })).status).toBe(201);
      const r = await api(o.token).post<any>(`/v1/docs/${d.id}/comments`, { anchor: "a", body: "c31" });
      expect(r.status).toBe(429);
      expect(r.body.error.details.limiter).toBe("RL_DOC");
      expect((await api(o.token).get(`/v1/docs/${d.id}/comments`)).status).toBe(200);
    });
  });
});
