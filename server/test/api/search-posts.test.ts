// test/api/search-posts.test.ts
import { env } from "cloudflare:test";
import { expect, test } from "vitest";
import { api, signup, createPost, follow, uniqueHandle } from "../helpers";
const sp = async (t: any, q: string) => (await api(t.token).get<any>(`/v1/search/posts?q=${encodeURIComponent(q)}`)).body;

test("3+ char Korean term via FTS with highlights; 2-char term via LIKE fallback", async () => {
  const a = await signup(); const v = await signup();
  const tag = uniqueHandle("s").slice(-6);
  const p = await createPost(a, `오늘 배포는 ${tag} 완료`);
  await createPost(a, `다른 글 ${tag}`);
  const r = await sp(v, `배포는 ${tag}`);
  expect(r.items.map((i: any) => i.post.id)).toEqual([p.id]);
  expect(r.items[0].highlights).toEqual([{ start: 3, length: 3 }, { start: 7, length: tag.length }]);
  expect(r.next_cursor).toBeNull();
  expect((await sp(v, `배포 ${tag}`)).items.map((i: any) => i.post.id)).toEqual([p.id]);   // "배포" < 3 chars -> LIKE
  expect((await api(v.token).get("/v1/search/posts?q=")).status).toBe(400);
});
test("LIKE fallback only looks at the last 90 days", async () => {
  const a = await signup(); const v = await signup();
  const tag = uniqueHandle("o").slice(-6);
  const old = await createPost(a, `옛 ${tag}`);
  const oldId = "0".repeat(10) + old.id.slice(10);   // same random part, timestamp far in the past
  await env.DB.prepare("UPDATE posts SET id=?, created_at=? WHERE id=?").bind(oldId, Date.now() - 100 * 86_400_000, old.id).run();
  expect((await sp(v, `옛 ${tag}`)).items).toEqual([]);
  expect((await sp(v, `${tag}`)).items.map((i: any) => i.post.id)).toEqual([oldId]);   // FTS path has no age bound
});
test("visibility: blocked, private non-follower, suspended and deleted authors excluded; private visible to active follower", async () => {
  const v = await signup(); const blk = await signup(); const priv = await signup(); const sus = await signup(); const pub = await signup();
  const tag = uniqueHandle("q").slice(-6);
  await api(priv.token).patch("/v1/me/settings", { is_private: true });
  await createPost(blk, `검색어 ${tag}`);
  const pp = await createPost(priv, `검색어 ${tag}`);
  await createPost(sus, `검색어 ${tag}`);
  const pd = await createPost(pub, `검색어 ${tag} 삭제`);
  const ok = await createPost(pub, `검색어 ${tag}`);
  await api(blk.token).put(`/v1/blocks/@${v.handle}`);
  await env.DB.prepare("UPDATE users SET suspended_at=? WHERE id=?").bind(Date.now(), sus.user.id).run();
  await api(pub.token).del(`/v1/posts/${pd.id}`);
  expect((await sp(v, `검색어 ${tag}`)).items.map((i: any) => i.post.id)).toEqual([ok.id]);
  await follow(v, priv); await api(priv.token).post(`/v1/me/follow-requests/@${v.handle}/accept`);
  expect((await sp(v, `검색어 ${tag}`)).items.map((i: any) => i.post.id)).toEqual([ok.id, pp.id]);
});
test("hashtag search: prefix over the last 7 days with 24h counts; private and >7d excluded", async () => {
  const a = await signup(); const b = await signup(); const priv = await signup(); const v = await signup();
  const pre = `해${uniqueHandle("h").slice(-5)}`;
  await createPost(a, `#${pre}가 #${pre}나`); await createPost(b, `#${pre}가`);
  const twoDays = await createPost(a, `#${pre}다`);
  await env.DB.prepare("UPDATE post_hashtags SET created_at=? WHERE post_id=?").bind(Date.now() - 2 * 86_400_000, twoDays.id).run();
  const old = await createPost(a, `#${pre}라`);
  await env.DB.prepare("UPDATE post_hashtags SET created_at=? WHERE post_id=?").bind(Date.now() - 8 * 86_400_000, old.id).run();
  await api(priv.token).patch("/v1/me/settings", { is_private: true });
  await createPost(priv, `#${pre}마`);
  const r = (await api(v.token).get<any>(`/v1/hashtags/search?q=${encodeURIComponent("#" + pre.toUpperCase())}`)).body;
  expect(r.items).toEqual([{ tag: `${pre}가`, post_count_24h: 2 }, { tag: `${pre}나`, post_count_24h: 1 }, { tag: `${pre}다`, post_count_24h: 0 }]);
  expect((await api(v.token).get(`/v1/hashtags/search?q=${encodeURIComponent("#")}`)).status).toBe(400);   // empty after stripping "#"
});
