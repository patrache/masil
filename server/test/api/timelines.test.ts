// test/api/timelines.test.ts
import { expect, test } from "vitest";
import { api, signup, createPost, follow, connectWs } from "../helpers";
const tl = async (t: any, q = "") => (await api(t.token).get<any>(`/v1/timeline/following${q}`)).body;

test("following: me + active followees, top-level and quotes, no replies, repost deduped to latest", async () => {
  const me = await signup(); const a = await signup(); const b = await signup(); const z = await signup();
  await follow(me, a); await follow(me, b);
  const mine = await createPost(me, "mine");
  const pa = await createPost(a, "a");
  await createPost(a, "reply", { reply_to_id: pa.id });
  const pz = await createPost(z, "z");
  await api(a.token).put(`/v1/posts/${pz.id}/repost`);
  await api(b.token).put(`/v1/posts/${pz.id}/repost`);
  const items = (await tl(me)).items;
  expect(items.map((i: any) => [i.type, i.post.id, i.reposted_by?.id ?? null])).toEqual([
    ["repost", pz.id, b.user.id], ["post", pa.id, null], ["post", mine.id, null]]);
});
test("cursor and since/gap", async () => {
  const me = await signup();
  const p = [];
  for (let i = 0; i < 4; i++) p.push(await createPost(me, `p${i}`));
  const page1 = await tl(me, "?limit=3");
  expect(page1.items.map((i: any) => i.id)).toEqual([p[3].id, p[2].id, p[1].id]);
  expect((await tl(me, `?limit=3&cursor=${page1.next_cursor}`)).items.map((i: any) => i.id)).toEqual([p[0].id]);
  expect(await tl(me, `?since=${p[1].id}&limit=5`)).toMatchObject({ gap: false, next_cursor: null });
  expect((await tl(me, `?since=${p[0].id}&limit=2`)).gap).toBe(true);
  expect((await tl(me)).gap).toBe(false);
});
test("repost of blocked author hidden from timeline and event", async () => {
  const me = await signup(); const c = await signup(); const blocked = await signup();
  await follow(me, c);
  await api(me.token).put(`/v1/blocks/@${blocked.handle}`);
  const bp = await createPost(blocked, "blocked post");
  const w = await connectWs(me.token);
  await api(c.token).put(`/v1/posts/${bp.id}/repost`);
  await expect(w.next("timeline.item_created", undefined, 500)).rejects.toThrow();
  expect((await tl(me)).items.find((i: any) => i.post.id === bp.id)).toBeUndefined();
});
test("discover: public non-suspended top-level only, excludes private and blocked, no reposts", async () => {
  const v = await signup(); const pub = await signup(); const priv = await signup(); const blk = await signup();
  const a = await createPost(pub, "pub");
  await api(priv.token).patch("/v1/me/settings", { is_private: true });
  const b = await createPost(priv, "priv");
  const c = await createPost(blk, "blk");
  await api(blk.token).put(`/v1/blocks/@${v.handle}`);
  await api(pub.token).put(`/v1/posts/${a.id}/repost`);
  const items = (await api(v.token).get<any>(`/v1/timeline/discover?since=${a.id.slice(0, 10)}${"0".repeat(16)}&limit=100`)).body.items;
  const ids = items.map((i: any) => i.id);
  expect(ids).toContain(a.id); expect(ids).not.toContain(b.id); expect(ids).not.toContain(c.id);
  expect(items.every((i: any) => i.type === "post")).toBe(true);
});
test("users/{user}/posts filters; private -> 403 private_account", async () => {
  const a = await signup(); const v = await signup(); const o = await signup();
  const top = await createPost(a, "top");
  const rep = await createPost(a, "rep", { reply_to_id: top.id });
  const op = await createPost(o, "o"); await api(a.token).put(`/v1/posts/${op.id}/repost`);
  const tab = async (f: string) => (await api(v.token).get<any>(`/v1/users/@${a.handle}/posts?filter=${f}`)).body.items.map((i: any) => i.post.id);
  expect(await tab("posts")).toEqual([op.id, top.id]);
  expect(await tab("replies")).toEqual([rep.id]);
  expect(await tab("media")).toEqual([]);
  await api(a.token).patch("/v1/me/settings", { is_private: true });
  expect((await api(v.token).get<any>(`/v1/users/@${a.handle}/posts`)).body.error.code).toBe("private_account");
});
test("profile posts tab hides reposts the viewer can't see (blocked author, quoted blocked author, later-private author)", async () => {
  const a = await signup(); const b = await signup(); const c = await signup(); const p = await signup(); const v = await signup();
  await follow(a, p);
  const bp = await createPost(b, "b post");
  const pp = await createPost(p, "p post");
  const cq = await createPost(c, "quoting b", { quote_of_id: bp.id });
  for (const id of [bp.id, pp.id, cq.id]) await api(a.token).put(`/v1/posts/${id}/repost`);
  await api(b.token).put(`/v1/blocks/@${v.handle}`);
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  const tab = async (t: any) => (await api(t.token).get<any>(`/v1/users/@${a.handle}/posts?filter=posts`)).body.items.map((i: any) => i.post.id);
  expect(await tab(v)).toEqual([]);
  expect(await tab(a)).toEqual([cq.id, pp.id, bp.id]);
});
