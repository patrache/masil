// Stories (api.md 5.17, plan 02 Task 9).
import { env } from "cloudflare:test";
import { expect, test } from "vitest";
import { api, signup, follow, uploadPng, connectWs } from "../helpers";

test("create text+media story, 24h expiry, story.created to followers and self", async () => {
  const a = await signup(); const f = await signup();
  await follow(f, a);
  const wf = await connectWs(f.token);
  const m = await uploadPng(a, "story");
  const r = await api(a.token).post<any>("/v1/stories", { body: "점심 🍜", media_upload_id: m.id, bg: "accent3" });
  expect(r.status).toBe(201);
  const s = r.body.story;
  expect(Date.parse(s.expires_at) - Date.parse(s.created_at)).toBe(86_400_000);
  expect(s).toMatchObject({ bg: "accent3", seen: true, view_count: 0, media: { id: m.id }, author: { id: a.user.id } }); // author's own story is always seen
  expect((await wf.next("story.created")).data).toEqual({ user_id: a.user.id, story_id: s.id, expires_at: s.expires_at });
  expect((await api(f.token).raw(`/v1/media/${m.id}`, { method: "GET" })).status).toBe(200);
});
test("validation: empty story 400, post-purpose media 400, body 201 chars too_long", async () => {
  const a = await signup();
  expect((await api(a.token).post("/v1/stories", {})).status).toBe(400);
  const pm = await uploadPng(a, "post");
  expect((await api(a.token).post("/v1/stories", { media_upload_id: pm.id })).status).toBe(400);
  expect((await api(a.token).post<any>("/v1/stories", { body: "가".repeat(201) })).body.error.details.fields.body).toBe("too_long");
});
test("31st active story -> 422 active_stories", async () => {
  const a = await signup();
  await env.DB.prepare(`INSERT INTO stories (id, author_id, body, created_at, expires_at) SELECT 'S' || value || ?1, ?1, 'x', ?2, ?3 FROM json_each(?4)`)
    .bind(a.user.id, Date.now(), Date.now() + 86_400_000, JSON.stringify([...Array(30).keys()].map(String))).run();
  const r = await api(a.token).post<any>("/v1/stories", { body: "one more" });
  expect(r.status).toBe(422); expect(r.body.error.details).toEqual({ what: "active_stories", limit: 30 });
});
test("feed ordering: me first, then unseen groups by latest_at desc, then seen; stories oldest first", async () => {
  const me = await signup(); const a = await signup(); const b = await signup(); const c = await signup();
  for (const x of [a, b, c]) await follow(me, x);
  const sa = (await api(a.token).post<any>("/v1/stories", { body: "a1" })).body.story;
  await api(a.token).post("/v1/stories", { body: "a2" });
  await api(b.token).post("/v1/stories", { body: "b1" });
  const sc = (await api(c.token).post<any>("/v1/stories", { body: "c1" })).body.story;
  await api(me.token).post("/v1/stories", { body: "me" });
  await api(me.token).post(`/v1/stories/${sc.id}/view`); await api(me.token).post(`/v1/stories/${sc.id}/view`);
  const g = (await api(me.token).get<any>("/v1/stories/feed")).body.items;
  expect(g.map((x: any) => [x.user.id, x.has_unseen])).toEqual([[me.user.id, false], [b.user.id, true], [a.user.id, true], [c.user.id, false]]);
  expect(g[2].stories.map((s: any) => s.body)).toEqual(["a1", "a2"]);
  expect((await api(c.token).get<any>(`/v1/stories/${sc.id}/viewers`)).body.items.map((v: any) => v.user.id)).toEqual([me.user.id]);
  expect((await api(me.token).get(`/v1/stories/${sc.id}/viewers`)).status).toBe(403);
  expect(sa.view_count).toBe(0);
  expect(g.find((x: any) => x.user.id === c.user.id).stories[0]).toMatchObject({ id: sc.id, seen: true, view_count: null });
  expect(g[2].stories[0]).toMatchObject({ seen: false, view_count: null });
});
test("expired story hidden before cleanup (feed, user stories, media, view)", async () => {
  const a = await signup(); const v = await signup();
  await follow(v, a);
  const m = await uploadPng(a, "story");
  const s = (await api(a.token).post<any>("/v1/stories", { media_upload_id: m.id })).body.story;
  await env.DB.prepare("UPDATE stories SET expires_at = ? WHERE id = ?").bind(Date.now() - 1000, s.id).run();
  expect((await api(v.token).get<any>("/v1/stories/feed")).body.items).toEqual([]);
  expect((await api(v.token).get<any>(`/v1/users/@${a.handle}/stories`)).body.items).toEqual([]);
  expect((await api(v.token).raw(`/v1/media/${m.id}`, { method: "GET" })).status).toBe(404);
  expect((await api(v.token).post(`/v1/stories/${s.id}/view`)).status).toBe(404);
});
test("private author's stories need active follow; delete by author emits story.deleted and removes media", async () => {
  const p = await signup(); const x = await signup(); const f = await signup();
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  await follow(f, p); await api(p.token).post(`/v1/me/follow-requests/@${f.handle}/accept`);
  const m = await uploadPng(p, "story");
  const s = (await api(p.token).post<any>("/v1/stories", { media_upload_id: m.id })).body.story;
  expect((await api(x.token).get<any>(`/v1/users/@${p.handle}/stories`)).body.error.code).toBe("private_account");
  const wf = await connectWs(f.token);
  expect((await api(x.token).del(`/v1/stories/${s.id}`)).status).toBe(404);
  expect((await api(p.token).del(`/v1/stories/${s.id}`)).status).toBe(204);
  expect((await wf.next("story.deleted")).data).toEqual({ user_id: p.user.id, story_id: s.id });
  expect(await env.MEDIA.head(`stories/${p.user.id}/${m.id}.png`)).toBeNull();
});
test("a suspended followee's stories leave the feed while the suspension lasts and come back after it (api.md 3.9)", async () => {
  const v = await signup(); const s = await signup(); const a = await signup();
  await follow(v, s); await follow(v, a);
  await api(s.token).post("/v1/stories", { body: "정지될 스토리" });
  await api(a.token).post("/v1/stories", { body: "a" });
  const groups = async () => (await api(v.token).get<any>("/v1/stories/feed")).body.items.map((g: any) => g.user.id);
  expect((await groups()).sort()).toEqual([s.user.id, a.user.id].sort());
  const suspend = (until: number | null) =>
    env.DB.prepare("UPDATE users SET suspended_at = ?1, suspended_until = ?2 WHERE id = ?3").bind(Date.now(), until, s.user.id).run();
  await suspend(null);
  expect(await groups()).toEqual([a.user.id]);
  await suspend(Date.now() + 60_000);
  expect(await groups()).toEqual([a.user.id]);
  await suspend(Date.now() - 1000);
  expect((await groups()).sort()).toEqual([s.user.id, a.user.id].sort());
});
