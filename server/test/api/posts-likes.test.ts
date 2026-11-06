// test/api/posts-likes.test.ts
import { expect, test } from "vitest";
import { api, signup, createPost, follow, connectWs, watch } from "../helpers";

test("like toggles, post.stats on topic, post_like notification survives unlike and is deduped", async () => {
  const a = await signup(); const b = await signup();
  const p = await createPost(a, "p");
  const w = await connectWs(a.token); await watch(w, [`post:${p.id}`]);
  expect((await api(b.token).put<any>(`/v1/posts/${p.id}/like`)).body).toEqual({ post_id: p.id, liked: true, like_count: 1 });
  expect((await w.next("post.stats")).data.like_count).toBe(1);
  expect((await api(b.token).del<any>(`/v1/posts/${p.id}/like`)).body).toEqual({ post_id: p.id, liked: false, like_count: 0 });
  await api(b.token).put(`/v1/posts/${p.id}/like`);
  const n = (await api(a.token).get<any>("/v1/notifications")).body.items.filter((x: any) => x.type === "post_like");
  expect(n.length).toBe(1);
  expect((await api(b.token).get<any>(`/v1/posts/${p.id}`)).body.post.viewer).toEqual({ liked: true, reposted: false });
  expect((await api(a.token).get<any>(`/v1/posts/${p.id}/likes`)).body.items.map((u: any) => u.id)).toEqual([b.user.id]);
});
test("concurrent double like counts once", async () => {
  const a = await signup(); const b = await signup();
  const p = await createPost(a, "p");
  await Promise.all([api(b.token).put(`/v1/posts/${p.id}/like`), api(b.token).put(`/v1/posts/${p.id}/like`)]);
  expect((await api(a.token).get<any>(`/v1/posts/${p.id}`)).body.post.like_count).toBe(1);
  await Promise.all([api(b.token).del(`/v1/posts/${p.id}/like`), api(b.token).del(`/v1/posts/${p.id}/like`)]);
  expect((await api(a.token).get<any>(`/v1/posts/${p.id}`)).body.post.like_count).toBe(0);
});
test("repost: own post allowed, timeline.item_created(repost) to reposter's followers, notification, list", async () => {
  const a = await signup(); const b = await signup(); const f = await signup();
  await follow(f, b);
  const p = await createPost(a, "p");
  const wf = await connectWs(f.token);
  const r = await api(b.token).put<any>(`/v1/posts/${p.id}/repost`);
  expect(r.body).toEqual({ post_id: p.id, reposted: true, repost_count: 1 });
  const ev = (await wf.next("timeline.item_created", (d) => d.item.type === "repost")).data.item;
  expect(ev).toMatchObject({ type: "repost", post: { id: p.id }, reposted_by: { id: b.user.id } });
  expect(ev.id).not.toBe(p.id);
  expect((await api(a.token).get<any>("/v1/notifications")).body.items[0].type).toBe("post_repost");
  expect((await api(a.token).get<any>(`/v1/posts/${p.id}/reposts`)).body.items.map((u: any) => u.id)).toEqual([b.user.id]);
  expect((await api(a.token).put(`/v1/posts/${p.id}/repost`)).status).toBe(200);
});
test("private author's post: repost forbidden even for approved follower and author; blocked liker gets 404", async () => {
  const p = await signup(); const f = await signup(); const x = await signup();
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  await follow(f, p); await api(p.token).post(`/v1/me/follow-requests/@${f.handle}/accept`);
  const post = await createPost(p, "secret");
  expect((await api(f.token).put<any>(`/v1/posts/${post.id}/repost`)).body.error.code).toBe("action_not_allowed");
  expect((await api(p.token).put<any>(`/v1/posts/${post.id}/repost`)).body.error.code).toBe("action_not_allowed");
  expect((await api(f.token).put(`/v1/posts/${post.id}/like`)).status).toBe(200);
  await api(p.token).put(`/v1/blocks/@${x.handle}`);
  expect((await api(x.token).put(`/v1/posts/${post.id}/like`)).status).toBe(404);
});
