// test/api/posts.test.ts
import { env } from "cloudflare:test";
import { expect, test } from "vitest";
import { api, signup, uploadPng, createPost, follow, connectWs } from "../helpers";

test("create with media, hashtags, mentions; counters; uploads attached", async () => {
  const a = await signup(); const b = await signup();
  const m1 = await uploadPng(a, "post"); const m2 = await uploadPng(a, "post");
  const p = await createPost(a, `오늘 첫 눈 ❄️ #첫눈 #Go #첫눈 @${b.handle}`, { media_upload_ids: [m2.id, m1.id] });
  expect(p.media.map((m) => m.id)).toEqual([m2.id, m1.id]);
  expect(p.hashtags).toEqual(["첫눈", "go"]);
  expect(p.mentions).toEqual([{ user_id: b.user.id, handle: b.handle }]);
  expect(p).toMatchObject({ reply_to_id: null, root_id: null, quote: null, like_count: 0, deleted: false, viewer: { liked: false, reposted: false } });
  expect((await api(a.token).get<any>("/v1/me")).body.user.post_count).toBe(1);
  const st = await env.DB.prepare("SELECT status, attached_to FROM uploads WHERE id=?").bind(m1.id).first<any>();
  expect(st).toEqual({ status: "attached", attached_to: `post:${p.id}` });
  expect((await api(b.token).get<any>("/v1/notifications")).body.items[0]).toMatchObject({ type: "post_mention", context: { post_id: p.id } });
});
test("same client_nonce -> 200 same post", async () => {
  const a = await signup();
  const body = { client_nonce: "dup-1", body: "hi" };
  const r1 = await api(a.token).post<any>("/v1/posts", body);
  const r2 = await api(a.token).post<any>("/v1/posts", body);
  expect([r1.status, r2.status]).toEqual([201, 200]);
  expect(r2.body.post.id).toBe(r1.body.post.id);
});
test("validation: empty body w/o media, 501 chars, 5 media, someone else's upload, reply+quote, poll on reply", async () => {
  const a = await signup(); const b = await signup();
  const P = (j: any) => api(a.token).post<any>("/v1/posts", { client_nonce: crypto.randomUUID(), ...j });
  expect((await P({ body: "  " })).body.error.details.fields.body).toBe("too_short");
  expect((await P({ body: "가".repeat(501) })).body.error.details.fields.body).toBe("too_long");
  const ups = await Promise.all([0, 1, 2, 3, 4].map(() => uploadPng(a, "post")));
  const five = await P({ body: "", media_upload_ids: ups.map((u) => u.id) });
  expect(five.status).toBe(422); expect(five.body.error.details).toEqual({ what: "post_media", limit: 4 });
  const theirs = await uploadPng(b, "post");
  expect((await P({ body: "x", media_upload_ids: [theirs.id] })).body.error.details.fields.media_upload_ids).toBe("invalid");
  const story = await uploadPng(a, "story");
  expect((await P({ body: "x", media_upload_ids: [story.id] })).status).toBe(400);
  const t = await createPost(a, "t");
  expect((await P({ body: "x", reply_to_id: t.id, quote_of_id: t.id })).status).toBe(400);
  expect((await P({ body: "x", reply_to_id: t.id, poll: { question: "q", options: ["a", "b"] } })).status).toBe(400);
  expect((await P({ body: "", poll: { question: "점심?", options: ["국밥", "라멘"] } })).status).toBe(201);
});
test("mention of a user who cannot view (blocked / private author non-follower) is not resolved", async () => {
  const a = await signup(); const b = await signup(); const c = await signup();
  await api(b.token).put(`/v1/blocks/@${a.handle}`);
  await api(a.token).patch("/v1/me/settings", { is_private: true });
  const p = await createPost(a, `@${b.handle} @${c.handle}`);
  expect(p.mentions).toEqual([]);
  expect((await api(c.token).get<any>("/v1/notifications")).body.items).toEqual([]);
});
test("GET visibility and timeline.item_created to follower + author", async () => {
  const a = await signup(); const f = await signup(); const x = await signup();
  await follow(f, a);
  const wf = await connectWs(f.token); const wa = await connectWs(a.token);
  const p = await createPost(a, "hello");
  expect((await wf.next("timeline.item_created")).data.item).toMatchObject({ id: p.id, type: "post", reposted_by: null });
  expect((await wa.next("timeline.item_created")).seq).toBeNull();
  expect((await api(x.token).get<any>(`/v1/posts/${p.id}`)).body.post.id).toBe(p.id);
  await api(a.token).put(`/v1/blocks/@${x.handle}`);
  expect((await api(x.token).get(`/v1/posts/${p.id}`)).status).toBe(404);
});
test("delete: author only; soft delete, R2 removed, post.deleted to followers, post_count decremented", async () => {
  const a = await signup(); const f = await signup();
  await follow(f, a);
  const m = await uploadPng(a, "post");
  const p = await createPost(a, "bye", { media_upload_ids: [m.id] });
  const wf = await connectWs(f.token);
  expect((await api(f.token).del(`/v1/posts/${p.id}`)).status).toBe(403);
  expect((await api(a.token).del(`/v1/posts/${p.id}`)).status).toBe(204);
  expect((await wf.next("post.deleted")).data).toEqual({ post_id: p.id });
  expect((await api(a.token).get(`/v1/posts/${p.id}`)).status).toBe(404);
  expect(await env.MEDIA.head(`posts/${a.user.id}/${m.id}.png`)).toBeNull();
  const row = await env.DB.prepare("SELECT body, deleted_by FROM posts WHERE id=?").bind(p.id).first<any>();
  expect(row).toEqual({ body: "", deleted_by: a.user.id });
  expect((await api(a.token).get<any>("/v1/me")).body.user.post_count).toBe(0);
});
