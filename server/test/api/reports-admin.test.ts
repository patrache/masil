// test/api/reports-admin.test.ts
import { env } from "cloudflare:test";
import { expect, test } from "vitest";
import { api, signup, createPost, uploadPng, makeSpace, makeAdmin, connectWs } from "../helpers";
const remove = (adm: any, j: any) => api(adm.token).post<any>("/v1/admin/content/remove", { reason: "illegal", ...j });

test("non-admin -> 403 not_admin", async () => {
  const u = await signup();
  expect((await api(u.token).get<any>("/v1/admin/reports")).body.error.code).toBe("not_admin");
});
test("remove post with report: R2 deleted, post gone, all open reports on target resolved", async () => {
  const adm = await signup(); await makeAdmin(adm);
  const a = await signup(); const r1 = await signup(); const r2 = await signup();
  const m = await uploadPng(a, "post");
  const p = await createPost(a, "bad", { media_upload_ids: [m.id] });
  const rep1 = (await api(r1.token).post<any>("/v1/reports", { target_type: "post", target_id: p.id, reason: "illegal" })).body.report_id;
  const rep2 = (await api(r2.token).post<any>("/v1/reports", { target_type: "post", target_id: p.id, reason: "spam" })).body.report_id;
  const res = await remove(adm, { target_type: "post", target_id: p.id, report_id: rep1 });
  expect(res.body.action).toMatchObject({ action: "remove_content", target_type: "post", target_id: p.id, report_id: rep1, admin: { id: adm.user.id } });
  expect((await api(r1.token).get(`/v1/posts/${p.id}`)).status).toBe(404);
  expect(await env.MEDIA.head(`posts/${a.user.id}/${m.id}.png`)).toBeNull();
  const rows = (await env.DB.prepare("SELECT status, resolution FROM reports WHERE id IN (?, ?)").bind(rep1, rep2).all<any>()).results;
  expect(rows).toEqual([{ status: "resolved", resolution: "content_removed" }, { status: "resolved", resolution: "content_removed" }]);
});
test("remove message emits message.deleted; remove upload detaches from post; remove space emits space.deleted", async () => {
  const adm = await signup(); await makeAdmin(adm);
  const a = await signup(); const b = await signup();
  const { space, generalChannelId } = await makeSpace(a, [b]);
  const msg = (await api(a.token).post<any>(`/v1/channels/${generalChannelId}/messages`, { client_nonce: "x", body: "bad" })).body.message;
  const wb = await connectWs(b.token);
  await remove(adm, { target_type: "message", target_id: msg.id });
  expect((await wb.next("message.deleted")).data.message_id).toBe(msg.id);
  const m1 = await uploadPng(a, "post"); const m2 = await uploadPng(a, "post");
  const p = await createPost(a, "two pics", { media_upload_ids: [m1.id, m2.id] });
  await remove(adm, { target_type: "upload", target_id: m1.id });
  expect((await api(b.token).get<any>(`/v1/posts/${p.id}`)).body.post.media.map((x: any) => x.id)).toEqual([m2.id]);
  expect((await api(b.token).raw(`/v1/media/${m1.id}`, { method: "GET" })).status).toBe(404);
  await remove(adm, { target_type: "space", target_id: space.id });
  expect((await wb.next("space.deleted")).data).toEqual({ space_id: space.id });
  expect((await api(b.token).get(`/v1/spaces/${space.id}`)).status).toBe(404);
  wb.close();
});
test("remove story and guestbook entry; unknown target -> 404", async () => {
  const adm = await signup(); await makeAdmin(adm);
  const a = await signup(); const o = await signup();
  const s = (await api(a.token).post<any>("/v1/stories", { body: "x" })).body.story;
  await remove(adm, { target_type: "story", target_id: s.id });
  expect((await api(a.token).get<any>(`/v1/users/@${a.handle}/stories`)).body.items).toEqual([]);
  const e = (await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "bad" })).body.entry;
  await remove(adm, { target_type: "guestbook_entry", target_id: e.id });
  expect((await api(o.token).get<any>(`/v1/users/@${o.handle}/guestbook`)).body.items).toEqual([]);
  expect((await remove(adm, { target_type: "post", target_id: "0".repeat(26) })).status).toBe(404);
});
test("unknown report_id -> 404 and nothing removed; removing a body-less story's image removes the story", async () => {
  const adm = await signup(); await makeAdmin(adm);
  const a = await signup();
  const p = await createPost(a, "keep");
  expect((await remove(adm, { target_type: "post", target_id: p.id, report_id: "0".repeat(26) })).status).toBe(404);
  expect((await api(a.token).get(`/v1/posts/${p.id}`)).status).toBe(200);
  const m = await uploadPng(a, "story");
  const s = (await api(a.token).post<any>("/v1/stories", { media_upload_id: m.id })).body.story;
  const wa = await connectWs(a.token);
  const res = await remove(adm, { target_type: "upload", target_id: m.id });
  expect(res.body.action).toMatchObject({ action: "remove_content", target_type: "upload", target_id: m.id, report_id: null });
  expect((await wa.next("story.deleted")).data).toEqual({ user_id: a.user.id, story_id: s.id });
  expect((await api(a.token).get<any>(`/v1/users/@${a.handle}/stories`)).body.items).toEqual([]);
  expect(await env.MEDIA.head(`stories/${a.user.id}/${m.id}.png`)).toBeNull();
  wa.close();
});
