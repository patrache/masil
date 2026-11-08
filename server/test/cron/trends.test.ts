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
test("trends leave out suspended authors (indefinite and still-running timed) and count them again once expired", async () => {
  const sfx = Math.random().toString(36).slice(2, 7);
  const big = `정지트렌드${sfx}`; const small = `작은트렌드${sfx}`;
  const [viewer, ...authors] = await Promise.all([0, 1, 2, 3, 4, 5, 6, 7, 8].map(() => signup()));
  for (const a of authors) await createPost(a!, `#${big}`);
  for (const a of authors.slice(0, 3)) await createPost(a!, `#${small}`);
  const suspended = authors.slice(2); // 6 of the 8 big-tag authors; one of them also posted the small tag
  const run = async () => {
    await handleScheduled(createScheduledController({ cron: "*/5 * * * *", scheduledTime: Date.now() }), env, createExecutionContext());
    const items = (await api(viewer!.token).get<any>("/v1/trends")).body.items as any[];
    return Object.fromEntries(items.filter((i) => i.tag === big || i.tag === small).map((i) => [i.tag, i.author_count]));
  };
  expect(await run()).toEqual({ [big]: 8, [small]: 3 });
  const setSuspension = async (until: (i: number) => number | null) => {
    for (const [i, u] of suspended.entries())
      await env.DB.prepare("UPDATE users SET suspended_at = ?1, suspended_until = ?2 WHERE id = ?3").bind(Date.now(), until(i), u.user.id).run();
  };
  await setSuspension((i) => (i % 2 === 0 ? null : Date.now() + 60_000));
  expect(await run()).toEqual({ [big]: 2, [small]: 2 });
  await setSuspension(() => Date.now() - 1000);
  expect(await run()).toEqual({ [big]: 8, [small]: 3 });
});
