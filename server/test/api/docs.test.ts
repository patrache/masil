// v0.1.3 C1.4 / api.md 5.24: room documents — D1 rows, REST, doc.* events (events.md 4.10), permissions, RL_DOC.
import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { api, connectWs, inOneRateWindow, makeDoc, makeSpace, signup } from "../helpers";

const fieldsOf = (r: { body: any }) => r.body.error.details.fields;

describe("room documents (api.md 5.24)", () => {
  it("create -> 201 + doc.created to the space; GET; list with type filter", async () => {
    const [o, m] = await Promise.all([signup({ displayName: "하나" }), signup()]);
    const { space } = await makeSpace(o, [m]);
    const wm = await connectWs(m.token);
    const r = await api(o.token).post<any>(`/v1/spaces/${space.id}/docs`, { type: "arch", title: "  결제 시스템 v3 " });
    expect(r.status).toBe(201);
    expect(r.body.doc).toMatchObject({
      space_id: space.id,
      type: "arch",
      title: "결제 시스템 v3",
      created_by: { id: o.user.id, display_name: "하나" },
      head_seq: 0,
      locked: false,
    });
    expect(r.body.doc.created_at).toBe(r.body.doc.updated_at);
    const ev = await wm.next("doc.created", (d: any) => d.doc.id === r.body.doc.id);
    expect(ev.data.doc).toEqual(r.body.doc);

    expect((await api(m.token).get<any>(`/v1/docs/${r.body.doc.id}`)).body.doc).toEqual(r.body.doc);
    const col = await makeDoc(m, space.id, "apicol");
    const all = await api(m.token).get<any>(`/v1/spaces/${space.id}/docs`);
    expect(all.status).toBe(200);
    expect(all.body.items.map((d: any) => d.id)).toEqual([col.id, r.body.doc.id]); // updated_at newest first
    expect(all.body.next_cursor).toBeNull();
    const arch = await api(m.token).get<any>(`/v1/spaces/${space.id}/docs?type=arch`);
    expect(arch.body.items.map((d: any) => d.id)).toEqual([r.body.doc.id]);
    expect(fieldsOf(await api(m.token).get<any>(`/v1/spaces/${space.id}/docs?type=nope`))).toEqual({ type: "invalid" });
  });

  it("list pages with a cursor", async () => {
    const o = await signup();
    const { space } = await makeSpace(o);
    const ids: string[] = [];
    for (let i = 0; i < 5; i++) ids.push((await makeDoc(o, space.id)).id);
    const p1 = await api(o.token).get<any>(`/v1/spaces/${space.id}/docs?limit=2`);
    expect(p1.body.items.map((d: any) => d.id)).toEqual([ids[4], ids[3]]);
    const p2 = await api(o.token).get<any>(`/v1/spaces/${space.id}/docs?limit=2&cursor=${p1.body.next_cursor}`);
    expect(p2.body.items.map((d: any) => d.id)).toEqual([ids[2], ids[1]]);
    const p3 = await api(o.token).get<any>(`/v1/spaces/${space.id}/docs?limit=2&cursor=${p2.body.next_cursor}`);
    expect(p3.body.items.map((d: any) => d.id)).toEqual([ids[0]]);
    expect(p3.body.next_cursor).toBeNull();
  });

  it("validation: title 1..80, type", async () => {
    const o = await signup();
    const { space } = await makeSpace(o);
    const post = (j: object) => api(o.token).post<any>(`/v1/spaces/${space.id}/docs`, j);
    expect(fieldsOf(await post({ type: "arch", title: "가".repeat(81) }))).toEqual({ title: "too_long" });
    expect((await post({ type: "arch", title: "가".repeat(80) })).status).toBe(201);
    expect(fieldsOf(await post({ type: "arch" }))).toEqual({ title: "required" });
    expect(fieldsOf(await post({ type: "arch", title: "   " }))).toEqual({ title: "too_short" });
    expect(fieldsOf(await post({ type: "sheet", title: "x" }))).toEqual({ type: "invalid" });
    const d = await makeDoc(o, space.id);
    expect(fieldsOf(await api(o.token).patch<any>(`/v1/docs/${d.id}`, { title: "가".repeat(81) }))).toEqual({ title: "too_long" });
  });

  it("outsiders get 404 on every path (GET / PATCH / DELETE / list / create / ws)", async () => {
    const [o, x] = await Promise.all([signup(), signup()]);
    const { space } = await makeSpace(o);
    const d = await makeDoc(o, space.id);
    expect((await api(x.token).get(`/v1/docs/${d.id}`)).status).toBe(404);
    expect((await api(x.token).patch(`/v1/docs/${d.id}`, { title: "y" })).status).toBe(404);
    expect((await api(x.token).del(`/v1/docs/${d.id}`)).status).toBe(404);
    expect((await api(x.token).get(`/v1/spaces/${space.id}/docs`)).status).toBe(404);
    expect((await api(x.token).post(`/v1/spaces/${space.id}/docs`, { type: "arch", title: "y" })).status).toBe(404);
    const ws = await api(x.token).raw(`/v1/docs/${d.id}/ws`, { headers: { Upgrade: "websocket" } });
    expect(ws.status).toBe(404);
    expect((await api(o.token).get(`/v1/docs/not-a-ulid`)).status).toBe(404);
    expect((await api(o.token).get(`/v1/docs/01JC9ZA0000000000000000000`)).status).toBe(404);
  });

  it("title by any member; locked and delete by creator / owner / admin only (403 forbidden otherwise)", async () => {
    const [o, c, m] = await Promise.all([signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [c, m]);
    const d = await makeDoc(c, space.id);
    const wo = await connectWs(o.token);
    const t = await api(m.token).patch<any>(`/v1/docs/${d.id}`, { title: "새 제목" });
    expect(t.status).toBe(200);
    expect(t.body.doc.title).toBe("새 제목");
    expect((await wo.next("doc.updated", (x: any) => x.doc.id === d.id)).data).toEqual({ doc: t.body.doc, deleted: false });

    const lm = await api(m.token).patch<any>(`/v1/docs/${d.id}`, { locked: true });
    expect(lm.status).toBe(403);
    expect(lm.body.error.code).toBe("forbidden");
    expect((await api(m.token).get<any>(`/v1/docs/${d.id}`)).body.doc.locked).toBe(false);

    const lc = await api(c.token).patch<any>(`/v1/docs/${d.id}`, { locked: true });
    expect(lc.status).toBe(200);
    expect(lc.body.doc.locked).toBe(true);
    expect((await wo.next("doc.updated", (x: any) => x.doc.id === d.id && x.doc.locked)).data.deleted).toBe(false);
    expect((await api(o.token).patch<any>(`/v1/docs/${d.id}`, { locked: false })).body.doc.locked).toBe(false); // owner

    expect((await api(m.token).del(`/v1/docs/${d.id}`)).status).toBe(403);
    expect((await api(o.token).del(`/v1/docs/${d.id}`)).status).toBe(204);
    const del = await wo.next("doc.updated", (x: any) => x.doc.id === d.id && x.deleted === true);
    expect(del.data.doc.id).toBe(d.id);
    expect((await api(c.token).get(`/v1/docs/${d.id}`)).status).toBe(404);
    expect((await api(c.token).patch(`/v1/docs/${d.id}`, { title: "z" })).status).toBe(404);
    expect((await api(c.token).get<any>(`/v1/spaces/${space.id}/docs`)).body.items).toEqual([]);
  });

  it("a locked doc's title is renamed only by creator / owner / admin (api.md 5.24: locked = no edits by others)", async () => {
    const [o, c, m] = await Promise.all([signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [c, m]);
    const d = await makeDoc(c, space.id);
    expect((await api(c.token).patch(`/v1/docs/${d.id}`, { locked: true })).status).toBe(200);
    const tm = await api(m.token).patch<any>(`/v1/docs/${d.id}`, { title: "멤버 제목" });
    expect(tm.status).toBe(403);
    expect(tm.body.error.code).toBe("forbidden");
    expect((await api(m.token).get<any>(`/v1/docs/${d.id}`)).body.doc.title).not.toBe("멤버 제목");
    expect((await api(c.token).patch<any>(`/v1/docs/${d.id}`, { title: "만든 이 제목" })).body.doc.title).toBe("만든 이 제목");
    expect((await api(o.token).patch<any>(`/v1/docs/${d.id}`, { title: "방장 제목" })).body.doc.title).toBe("방장 제목");
    // unlocking and renaming in one request is still a manager-only change
    expect((await api(m.token).patch(`/v1/docs/${d.id}`, { title: "x", locked: false })).status).toBe(403);
  });

  it("a space admin may lock; a member promoted to admin may too", async () => {
    const [o, a, m] = await Promise.all([signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [a, m]);
    expect((await api(o.token).patch(`/v1/spaces/${space.id}/members/${a.user.id}`, { role: "admin" })).status).toBe(200);
    const d = await makeDoc(m, space.id);
    expect((await api(a.token).patch<any>(`/v1/docs/${d.id}`, { locked: true })).status).toBe(200);
  });

  it("after the space is deleted (soft) members get 404 on GET / PATCH / ws and the list", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { space } = await makeSpace(o, [m]);
    const d = await makeDoc(m, space.id);
    expect((await api(o.token).del(`/v1/spaces/${space.id}`)).status).toBe(204);
    const row = await env.DB.prepare("SELECT deleted_at FROM collab_docs WHERE id = ?").bind(d.id).first<any>();
    expect(row.deleted_at).toBeNull(); // the doc row stays; the space's deleted_at hides it
    expect((await api(m.token).get(`/v1/docs/${d.id}`)).status).toBe(404);
    expect((await api(m.token).patch(`/v1/docs/${d.id}`, { title: "z" })).status).toBe(404);
    expect((await api(m.token).raw(`/v1/docs/${d.id}/ws`, { headers: { Upgrade: "websocket" } })).status).toBe(404);
  });

  it("RL_DOC: the 31st write in one window is 429", async () => {
    await inOneRateWindow(60, async () => {
      const o = await signup();
      const { space } = await makeSpace(o);
      const d = await makeDoc(o, space.id); // write 1
      for (let i = 2; i <= 30; i++) expect((await api(o.token).patch(`/v1/docs/${d.id}`, { title: `t${i}` })).status).toBe(200);
      const r = await api(o.token).patch<any>(`/v1/docs/${d.id}`, { title: "t31" });
      expect(r.status).toBe(429);
      expect(r.body.error.details.limiter).toBe("RL_DOC");
      expect((await api(o.token).get(`/v1/docs/${d.id}`)).status).toBe(200); // reads are not RL_DOC
    });
  });
});
