// test/cron/trends.test.ts
import { env, createScheduledController, createExecutionContext } from "cloudflare:test";
import { expect, test } from "vitest";
import { handleScheduled } from "../../src/cron";
import { api, signup, createPost } from "../helpers";

test("trends ranks by distinct authors, excludes private/deleted, Top 10 replaced every run", async () => {
  const tag = `트렌드${Math.random().toString(36).slice(2, 7)}`;
  const authors = await Promise.all([0, 1, 2, 3, 4, 5, 6, 7].map(() => signup()));
  for (const a of authors) await createPost(a, `#${tag}`);
  await createPost(authors[0], `#${tag} again`);
  await api(authors[7].token).patch("/v1/me/settings", { is_private: true });
  const del = await createPost(authors[6], `#${tag} del`);
  await api(authors[6].token).del(`/v1/posts/${del.id}`);
  await handleScheduled(createScheduledController({ cron: "*/5 * * * *", scheduledTime: Date.now() }), env, createExecutionContext());
  const r = (await api(authors[0].token).get<any>("/v1/trends")).body;
  expect(r.items.length).toBeLessThanOrEqual(10);
  expect(r.items[0]).toEqual({ rank: 1, tag, post_count: 8, author_count: 7 });
  expect(typeof r.computed_at).toBe("string");
  const n = await env.DB.prepare("SELECT COUNT(*) AS n FROM trends").first<{ n: number }>();
  expect(n!.n).toBe(r.items.length);
});
