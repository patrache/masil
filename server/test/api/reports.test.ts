// test/api/reports.test.ts
import { env } from "cloudflare:test";
import { expect, test } from "vitest";
import { api, signup, createPost, uploadPng, makeSpace } from "../helpers";
const report = (t: any, j: any) => api(t.token).post<any>("/v1/reports", { reason: "spam", ...j });

test("report post copies snapshot; duplicate open report -> 409 already_exists", async () => {
  const a = await signup(); const r = await signup();
  const m = await uploadPng(a, "post");
  const p = await createPost(a, "광고 #스팸", { media_upload_ids: [m.id] });
  const res = await report(r, { target_type: "post", target_id: p.id, note: "도배" });
  expect(res.status).toBe(201);
  const row = await env.DB.prepare("SELECT target_user_id, snapshot, status FROM reports WHERE id=?").bind(res.body.report_id).first<any>();
  expect(row.target_user_id).toBe(a.user.id); expect(row.status).toBe("open");
  expect(JSON.parse(row.snapshot)).toMatchObject({ body: "광고 #스팸", author_id: a.user.id, media_urls: [`/v1/media/${m.id}`] });
  expect((await report(r, { target_type: "post", target_id: p.id })).body.error.code).toBe("already_exists");
});
test("reporter must be able to see the target", async () => {
  const a = await signup(); const r = await signup();
  await api(a.token).patch("/v1/me/settings", { is_private: true });
  const p = await createPost(a, "secret");
  expect((await report(r, { target_type: "post", target_id: p.id })).status).toBe(404);
  const { space, generalChannelId } = await makeSpace(a);
  const msg = (await api(a.token).post<any>(`/v1/channels/${generalChannelId}/messages`, { client_nonce: "m", body: "x" })).body.message;
  expect((await report(r, { target_type: "message", target_id: msg.id })).status).toBe(404);
  expect((await report(r, { target_type: "space", target_id: space.id })).status).toBe(404);
  expect((await report(r, { target_type: "user", target_id: a.user.id, reason: "harassment" })).status).toBe(201);
});
test("validation: bad reason, note too long", async () => {
  const a = await signup(); const r = await signup();
  expect((await report(r, { target_type: "user", target_id: a.user.id, reason: "meh" })).status).toBe(400);
  expect((await report(r, { target_type: "user", target_id: a.user.id, note: "x".repeat(1001) })).body.error.details.fields.note).toBe("too_long");
});
test("story and secret guestbook snapshots; secret entry hidden from third parties; concurrent duplicate -> one open report", async () => {
  const a = await signup(); const o = await signup(); const r = await signup();
  const s = (await api(a.token).post<any>("/v1/stories", { body: "스토리" })).body.story;
  const rs = await report(r, { target_type: "story", target_id: s.id });
  expect(rs.status).toBe(201);
  const srow = await env.DB.prepare("SELECT target_user_id, snapshot FROM reports WHERE id=?").bind(rs.body.report_id).first<any>();
  expect(srow.target_user_id).toBe(a.user.id);
  expect(JSON.parse(srow.snapshot)).toEqual({ body: "스토리", author_id: a.user.id, media_urls: [] });
  const e = (await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "비밀", is_secret: true })).body.entry;
  expect((await report(r, { target_type: "guestbook_entry", target_id: e.id })).status).toBe(404);
  const ro = await report(o, { target_type: "guestbook_entry", target_id: e.id });
  expect(ro.status).toBe(201);
  const grow = await env.DB.prepare("SELECT target_user_id, snapshot FROM reports WHERE id=?").bind(ro.body.report_id).first<any>();
  expect(grow.target_user_id).toBe(a.user.id);
  expect(JSON.parse(grow.snapshot)).toEqual({ body: "비밀", author_id: a.user.id, owner_id: o.user.id });
  const both = await Promise.all([report(r, { target_type: "user", target_id: o.user.id }), report(r, { target_type: "user", target_id: o.user.id })]);
  expect(both.map((x) => x.status).sort()).toEqual([201, 409]);
  const n = await env.DB.prepare("SELECT COUNT(*) AS n FROM reports WHERE reporter_id=? AND target_id=? AND status='open'").bind(r.user.id, o.user.id).first<any>();
  expect(n.n).toBe(1);
});
