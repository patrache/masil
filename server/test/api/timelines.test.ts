// test/api/timelines.test.ts
import { env } from "cloudflare:test";
import { expect, test } from "vitest";
import { api, signup, createPost, follow, connectWs, watch } from "../helpers";
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
test("timeline.item_created never names a reply parent's author the recipient cannot see (private parent, block either way)", async () => {
  const p = await signup(); const r = await signup(); const q = await signup(); const f = await signup(); const g = await signup();
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  await follow(r, p); await api(p.token).post(`/v1/me/follow-requests/@${r.handle}/accept`);
  await follow(f, q); await follow(g, q);
  const root = await createPost(p, "private root");
  const wr = await connectWs(r.token); await watch(wr, [`post:${root.id}`]);
  const reply = await createPost(r, "public reply", { reply_to_id: root.id });
  expect(reply.reply_to_author).toMatchObject({ id: p.user.id }); // r follows p
  // post.reply_created already restricts recipients to p's followers, so the private parent author stays named there
  expect((await wr.next("post.reply_created")).data.post.reply_to_author).toMatchObject({ id: p.user.id });
  const wf = await connectWs(f.token);
  // repost of a reply to a private account: f (not p's follower) sees reply_to_author null over REST, so in the event too
  await api(q.token).put(`/v1/posts/${reply.id}/repost`);
  const rep = (await wf.next("timeline.item_created", (d) => d.item.type === "repost")).data.item;
  expect(rep.post.id).toBe(reply.id);
  expect((await api(f.token).get<any>(`/v1/posts/${reply.id}`)).body.post.reply_to_author).toBeNull();
  expect(rep.post.reply_to_author).toBeNull();
  // quote of that reply: the quoted post's reply_to_author is hidden the same way
  const quote = await createPost(q, "quoting", { quote_of_id: reply.id });
  const qi = (await wf.next("timeline.item_created", (d) => d.item.post.id === quote.id)).data.item;
  expect(qi.post.quote).toMatchObject({ available: true, post: { id: reply.id } });
  expect(qi.post.quote.post.reply_to_author).toBeNull();

  // public parent: named in the event, and recipients blocked either way with that author are filtered out
  const pub = await signup();
  const root2 = await createPost(pub, "public root");
  const reply2 = await createPost(r, "reply 2", { reply_to_id: root2.id });
  await api(f.token).put(`/v1/blocks/@${pub.handle}`); // f blocked the parent author
  await api(pub.token).put(`/v1/blocks/@${g.handle}`); // the parent author blocked g
  const h = await signup(); await follow(h, q);
  const [wf2, wg, wh] = [wf, await connectWs(g.token), await connectWs(h.token)];
  await api(q.token).put(`/v1/posts/${reply2.id}/repost`);
  const hi = (await wh.next("timeline.item_created", (d) => d.item.post.id === reply2.id)).data.item;
  expect(hi.post.reply_to_author).toMatchObject({ id: pub.user.id });
  const quote2 = await createPost(q, "quoting 2", { quote_of_id: reply2.id });
  const hq = (await wh.next("timeline.item_created", (d) => d.item.post.id === quote2.id)).data.item;
  expect(hq.post.quote.post.reply_to_author).toMatchObject({ id: pub.user.id });
  for (const w of [wf2, wg]) {
    await expect(w.next("timeline.item_created", (d) => d.item.post.id === reply2.id || d.item.post.id === quote2.id, 500)).rejects.toThrow();
  }
  expect((await api(f.token).get<any>(`/v1/posts/${reply2.id}`)).body.post.reply_to_author).toBeNull();
  expect((await api(g.token).get<any>(`/v1/posts/${reply2.id}`)).body.post.reply_to_author).toBeNull();
});
test("suspended accounts (api.md 3.9): following timeline drops the followee's posts and its reposts, falling back to an older visible repost; discover drops the author", async () => {
  const me = await signup(); const s = await signup(); const a = await signup(); const z = await signup();
  await follow(me, s); await follow(me, a);
  const ps = await createPost(s, "정지될 글");
  const pz = await createPost(z, "z");
  await api(a.token).put(`/v1/posts/${pz.id}/repost`);
  await api(s.token).put(`/v1/posts/${pz.id}/repost`); // newest repost of pz, by the soon-suspended followee
  const pa = await createPost(a, "a");
  const items = async () => (await tl(me)).items.map((i: any) => [i.type, i.post.id, i.reposted_by?.id ?? null]);
  const discover = async () =>
    (await api(me.token).get<any>(`/v1/timeline/discover?since=${ps.id.slice(0, 10)}${"0".repeat(16)}&limit=100`)).body.items.map((i: any) => i.id);
  const visible = [["post", pa.id, null], ["repost", pz.id, s.user.id], ["post", ps.id, null]];
  expect(await items()).toEqual(visible);
  expect(await discover()).toContain(ps.id);
  const suspend = (until: number | null) =>
    env.DB.prepare("UPDATE users SET suspended_at = ?1, suspended_until = ?2 WHERE id = ?3").bind(Date.now(), until, s.user.id).run();
  for (const until of [null, Date.now() + 60_000]) {
    await suspend(until);
    expect(await items()).toEqual([["post", pa.id, null], ["repost", pz.id, a.user.id]]);
    expect(await discover()).not.toContain(ps.id);
  }
  await suspend(Date.now() - 1000);
  expect(await items()).toEqual(visible);
  expect(await discover()).toContain(ps.id);
});
