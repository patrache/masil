// Suspended owners' SNS content (api.md 3.4 rule 2 / 3.9): profile still 200 (suspended=true, can_view_posts=false),
// but stories, guestbook, follow lists, posts tab, story media and reports of that content are hidden (404) from
// everyone but the owner while the suspension lasts, and come back once suspended_until has passed.
import { env } from "cloudflare:test";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as P from "../../src/lib/permissions";
import { newId } from "../../src/lib/ids";
import { api, createPost, follow, signup, uploadPng, type TestUser } from "../helpers";

async function suspend(u: TestUser, until: number | null): Promise<void> {
  await env.DB.prepare("UPDATE users SET suspended_at = ?1, suspended_until = ?2 WHERE id = ?3").bind(Date.now(), until, u.user.id).run();
}

async function postVisibleViaSql(viewerId: string, postId: string): Promise<boolean> {
  const vis = P.postVisibilitySql(viewerId, "p");
  const row = await env.DB.prepare(`SELECT 1 AS x FROM posts p WHERE p.id = ? AND ${vis.sql}`).bind(postId, ...vis.params).first();
  return row !== null;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("canViewUserContent: suspension", () => {
  it("suspended (indefinite or until the future) -> hidden for others, ok for the owner; expired -> ok", async () => {
    const [o, v] = await Promise.all([signup(), signup()]);
    expect(await P.canViewUserContent(env.DB, v.user.id, o.user.id)).toBe("ok");
    await suspend(o, null);
    expect(await P.canViewUserContent(env.DB, v.user.id, o.user.id)).toBe("hidden");
    expect(await P.canViewUserContent(env.DB, o.user.id, o.user.id)).toBe("ok");
    await suspend(o, Date.now() + 60_000);
    expect(await P.canViewUserContent(env.DB, v.user.id, o.user.id)).toBe("hidden");
    await suspend(o, Date.now() - 1000);
    expect(await P.canViewUserContent(env.DB, v.user.id, o.user.id)).toBe("ok");
  });

  it("suspension wins over private/following: an active follower of a suspended private account sees nothing", async () => {
    const [o, v] = await Promise.all([signup(), signup()]);
    await env.DB.prepare("UPDATE users SET is_private = 1 WHERE id = ?").bind(o.user.id).run();
    await env.DB.prepare("INSERT INTO follows VALUES (?1, ?2, 'active', 0, 0)").bind(v.user.id, o.user.id).run();
    expect(await P.canViewUserContent(env.DB, v.user.id, o.user.id)).toBe("ok");
    await suspend(o, null);
    expect(await P.canViewUserContent(env.DB, v.user.id, o.user.id)).toBe("hidden");
  });

  it("boundary matches postVisibilitySql: suspended_until = now is active again, now + 1 is still suspended", async () => {
    const [o, v] = await Promise.all([signup(), signup()]);
    const postId = newId();
    await env.DB.prepare("INSERT INTO posts (id, author_id, body, created_at) VALUES (?1, ?2, 'x', ?3)").bind(postId, o.user.id, Date.now()).run();
    const t = Date.now() + 10 * 60_000;
    vi.spyOn(Date, "now").mockReturnValue(t);
    await env.DB.prepare("UPDATE users SET suspended_at = ?1, suspended_until = ?2 WHERE id = ?3").bind(t - 1000, t, o.user.id).run();
    expect(await postVisibleViaSql(v.user.id, postId)).toBe(true);
    expect(await P.canViewUserContent(env.DB, v.user.id, o.user.id)).toBe("ok");
    await env.DB.prepare("UPDATE users SET suspended_until = ?1 WHERE id = ?2").bind(t + 1, o.user.id).run();
    expect(await postVisibleViaSql(v.user.id, postId)).toBe(false);
    expect(await P.canViewUserContent(env.DB, v.user.id, o.user.id)).toBe("hidden");
  });

  it("missing owner -> hidden", async () => {
    const v = await signup();
    expect(await P.canViewUserContent(env.DB, v.user.id, newId())).toBe("hidden");
  });
});

describe("suspended owner's content over the API", () => {
  it("hidden while suspended, visible again after suspended_until", async () => {
    const [o, v, f] = await Promise.all([signup(), signup(), signup()]);
    await follow(f, o);
    await follow(o, f);
    await follow(v, o);
    await createPost(o, "글");
    const up = await uploadPng(o, "story");
    const story = (await api(o.token).post<any>("/v1/stories", { body: "스토리", media_upload_id: up.id })).body.story;
    expect(story).toBeTruthy();
    const entry = (await api(f.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "방명록" })).body.entry;
    expect(entry).toBeTruthy();

    const V = api(v.token);
    const contentPaths = [
      `/v1/users/@${o.handle}/stories`,
      `/v1/users/@${o.handle}/guestbook`,
      `/v1/users/@${o.handle}/followers`,
      `/v1/users/@${o.handle}/following`,
      `/v1/users/@${o.handle}/posts`,
      `/v1/users/@${o.handle}/posts?filter=replies`,
      `/v1/users/@${o.handle}/posts?filter=media`,
    ];
    const expectVisible = async (): Promise<void> => {
      for (const p of contentPaths) expect([p, (await V.get(p)).status]).toEqual([p, 200]);
      const prof = (await V.get<any>(`/v1/users/@${o.handle}`)).body.user;
      expect(prof.relationship.can_view_posts).toBe(true);
      expect((await V.raw(`/v1/media/${up.id}`, { method: "GET" })).status).toBe(200);
    };
    await expectVisible();

    await suspend(o, null);
    for (const p of contentPaths) {
      const r = await V.get<any>(p);
      expect([p, r.status, r.body?.error?.code]).toEqual([p, 404, "not_found"]);
    }
    const prof = await V.get<any>(`/v1/users/@${o.handle}`);
    expect(prof.status).toBe(200); // the profile itself stays (api.md 3.9: UserSummary.suspended = true)
    expect(prof.body.user.suspended).toBe(true);
    expect(prof.body.user.relationship.can_view_posts).toBe(false);
    const media = await V.raw(`/v1/media/${up.id}`, { method: "GET" });
    await media.body?.cancel();
    expect(media.status).toBe(404);
    expect((await V.post("/v1/reports", { reason: "spam", target_type: "story", target_id: story.id })).status).toBe(404);
    expect((await V.post("/v1/reports", { reason: "spam", target_type: "guestbook_entry", target_id: entry.id })).status).toBe(404);
    expect((await V.del(`/v1/guestbook/${entry.id}`)).status).toBe(404); // a third party can't even tell it exists
    expect((await V.post("/v1/stories/" + story.id + "/view")).status).toBe(404);

    await suspend(o, Date.now() + 60_000); // timed, still running
    expect((await V.get(`/v1/users/@${o.handle}/stories`)).status).toBe(404);

    await suspend(o, Date.now() - 1000); // expired: active again
    await expectVisible();
  });

  it("a suspended author's entry in someone else's guestbook is neither listed nor reportable", async () => {
    const [o, a, v] = await Promise.all([signup(), signup(), signup()]);
    const entry = (await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "남긴 글" })).body.entry;
    const V = api(v.token);
    expect((await V.get<any>(`/v1/users/@${o.handle}/guestbook`)).body.items.map((e: any) => e.id)).toEqual([entry.id]);
    await suspend(a, null);
    expect((await V.get<any>(`/v1/users/@${o.handle}/guestbook`)).body.items).toEqual([]);
    expect((await V.post("/v1/reports", { reason: "spam", target_type: "guestbook_entry", target_id: entry.id })).status).toBe(404);
    await suspend(a, Date.now() - 1000);
    expect((await V.post("/v1/reports", { reason: "spam", target_type: "guestbook_entry", target_id: entry.id })).status).toBe(201);
  });

  it("an entry by an author in a block relation with the reporter is not reportable (hidden from them, api.md 3.2)", async () => {
    const [o, a, v] = await Promise.all([signup(), signup(), signup()]);
    const entry = (await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "남긴 글" })).body.entry;
    await env.DB.prepare("INSERT INTO blocks (blocker_id, blocked_id, created_at) VALUES (?1, ?2, ?3)").bind(a.user.id, v.user.id, Date.now()).run();
    expect((await api(v.token).get<any>(`/v1/users/@${o.handle}/guestbook`)).body.items).toEqual([]);
    expect((await api(v.token).post("/v1/reports", { reason: "spam", target_type: "guestbook_entry", target_id: entry.id })).status).toBe(404);
  });
});
