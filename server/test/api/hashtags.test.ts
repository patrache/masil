// test/api/hashtags.test.ts
import { expect, test } from "vitest";
import { api, signup, createPost } from "../helpers";

test("NFD and uppercase tags normalize to one feed", async () => {
  const a = await signup(); const v = await signup();
  const sfx = Math.random().toString(36).slice(2, 7);
  const p1 = await createPost(a, `#첫눈${sfx}`);
  const p2 = await createPost(a, `#${`첫눈${sfx}`.normalize("NFD")}`);
  const p3 = await createPost(a, `#GoLang${sfx} 그리고 #첫눈${sfx}이`);
  const feed = async (t: string) => (await api(v.token).get<any>(`/v1/hashtags/${encodeURIComponent(t)}/posts`)).body.items.map((p: any) => p.id);
  expect(await feed(`첫눈${sfx}`)).toEqual([p2.id, p1.id]);
  expect(await feed(`#첫눈${sfx}`.normalize("NFD"))).toEqual([p2.id, p1.id]);
  expect(await feed(`golang${sfx.toUpperCase()}`)).toEqual([p3.id]);
  expect((await api(v.token).get(`/v1/hashtags/${encodeURIComponent("a b")}/posts`)).status).toBe(400);
});
test("hashtag feed excludes private accounts' posts even for followers", async () => {
  const p = await signup(); const tag = `t${Math.random().toString(36).slice(2, 8)}`;
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  await createPost(p, `#${tag}`);
  expect((await api(p.token).get<any>(`/v1/hashtags/${tag}/posts`)).body.items).toEqual([]);
});
