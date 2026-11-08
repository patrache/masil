// test/api/posts-replies.test.ts
import { expect, test } from "vitest";
import { api, signup, createPost, follow, connectWs, watch, uploadPng } from "../helpers";

test("reply: root_id, parent reply_count, post_reply notification, post.reply_created + post.stats on parent topic", async () => {
  const a = await signup(); const b = await signup();
  const root = await createPost(a, "root");
  const w = await connectWs(a.token); await watch(w, [`post:${root.id}`]);
  const r1 = await createPost(b, "r1", { reply_to_id: root.id });
  const r2 = await createPost(a, "r2", { reply_to_id: r1.id });
  expect(r1).toMatchObject({ reply_to_id: root.id, root_id: root.id, reply_to_author: { id: a.user.id } });
  expect(r2.root_id).toBe(root.id);
  expect((await w.next("post.reply_created")).data).toMatchObject({ parent_id: root.id, post: { id: r1.id } });
  expect((await w.next("post.stats")).data).toMatchObject({ post_id: root.id, reply_count: 1 });
  expect((await api(a.token).get<any>("/v1/notifications")).body.items[0]).toMatchObject({ type: "post_reply", subject: { type: "post", id: r1.id }, context: { post_id: r1.id } });
});
test("post.reply_created not delivered to watcher blocked by reply author or who can't see a private replier", async () => {
  const a = await signup(); const b = await signup(); const v = await signup(); const p = await signup();
  const root = await createPost(a, "root");
  const wv = await connectWs(v.token); await watch(wv, [`post:${root.id}`]);
  await api(b.token).put(`/v1/blocks/@${v.handle}`);
  await createPost(b, "hidden reply", { reply_to_id: root.id });
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  await createPost(p, "private reply", { reply_to_id: root.id });
  await expect(wv.next("post.reply_created", undefined, 500)).rejects.toThrow();
});
test("replies list: ascending, hides blocked author's reply, keeps deleted reply only if it has children", async () => {
  const a = await signup(); const b = await signup(); const c = await signup(); const v = await signup();
  const root = await createPost(a, "root");
  const x = await createPost(b, "x", { reply_to_id: root.id });
  const y = await createPost(c, "y", { reply_to_id: root.id });
  const z = await createPost(a, "z", { reply_to_id: root.id });
  await createPost(a, "child", { reply_to_id: y.id });
  await api(c.token).del(`/v1/posts/${y.id}`); await api(a.token).del(`/v1/posts/${z.id}`);
  await api(v.token).put(`/v1/blocks/@${b.handle}`);
  const r = await api(v.token).get<any>(`/v1/posts/${root.id}/replies`);
  expect(r.body.items.map((p: any) => [p.id, p.deleted])).toEqual([[y.id, true]]);
  expect((await api(a.token).get<any>(`/v1/posts/${root.id}/replies`)).body.items.map((p: any) => p.id)).toEqual([x.id, y.id]);
});
test("context returns ancestors root-first; invisible ancestor is a deleted placeholder", async () => {
  const a = await signup(); const b = await signup(); const v = await signup();
  const root = await createPost(a, "root");
  const m = await uploadPng(b, "post");
  const mid = await createPost(b, `mid #비밀태그 @${a.handle}`, { reply_to_id: root.id, media_upload_ids: [m.id] });
  await api(a.token).put(`/v1/posts/${mid.id}/like`);
  const leaf = await createPost(a, "leaf", { reply_to_id: mid.id });
  await api(v.token).put(`/v1/blocks/@${b.handle}`);
  const r = await api(v.token).get<any>(`/v1/posts/${leaf.id}/context`);
  expect(r.body.ancestors.map((p: any) => [p.id, p.deleted, p.body])).toEqual([[root.id, false, "root"], [mid.id, true, ""]]);
  // placeholder carries structure only: no author identity, media, tags, mentions or counts of the hidden post
  expect(r.body.ancestors[1]).toMatchObject({
    author: { id: "00000000000000000000000000", handle: "", display_name: "", avatar_url: null },
    reply_to_id: root.id, root_id: root.id, reply_to_author: null,
    media: [], poll: null, quote: null, hashtags: [], mentions: [],
    like_count: 0, repost_count: 0, quote_count: 0, reply_count: 1, viewer: { liked: false, reposted: false } });
  expect(r.body.post).toMatchObject({ id: leaf.id, reply_to_author: null }); // parent hidden from v -> its author is not named either
  expect((await api(a.token).get<any>(`/v1/posts/${leaf.id}`)).body.post.reply_to_author).toMatchObject({ id: b.user.id });
});
test("quote: quote_count, post_quote notification, quotes list; quoting private or invisible post rejected", async () => {
  const a = await signup(); const b = await signup(); const p = await signup();
  const orig = await createPost(a, "orig");
  const q = await createPost(b, "lol", { quote_of_id: orig.id });
  expect(q.quote).toMatchObject({ available: true, post: { id: orig.id, quote: null } });
  expect((await api(a.token).get<any>(`/v1/posts/${orig.id}`)).body.post.quote_count).toBe(1);
  expect((await api(a.token).get<any>("/v1/notifications")).body.items[0]).toMatchObject({ type: "post_quote", subject: { id: q.id } });
  expect((await api(a.token).get<any>(`/v1/posts/${orig.id}/quotes`)).body.items.map((x: any) => x.id)).toEqual([q.id]);
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  const pp = await createPost(p, "secret");
  await follow(b, p); await api(p.token).post(`/v1/me/follow-requests/@${b.handle}/accept`);
  const r = await api(b.token).post<any>("/v1/posts", { client_nonce: "q2", body: "x", quote_of_id: pp.id });
  expect(r.body.error.code).toBe("action_not_allowed");
  expect((await api(a.token).post("/v1/posts", { client_nonce: "q3", body: "x", reply_to_id: pp.id })).status).toBe(404);
  await api(a.token).del(`/v1/posts/${orig.id}`);
  expect((await api(b.token).get<any>(`/v1/posts/${q.id}`)).body.post.quote).toEqual({ available: false, id: orig.id, reason: "deleted" });
});
