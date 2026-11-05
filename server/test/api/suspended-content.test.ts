// Suspended owners' SNS content (api.md 3.4 rule 2 / 3.9): profile still 200 (suspended=true, can_view_posts=false),
// but stories, guestbook, follow lists, posts tab, story media and reports of that content are hidden (404) from
// everyone but the owner while the suspension lasts, and come back once suspended_until has passed.
import { env } from "cloudflare:test";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as P from "../../src/lib/permissions";
import { newId } from "../../src/lib/ids";
import { api, connectWs, createPost, follow, signup, uploadPng, type TestUser } from "../helpers";

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

describe("notification inbox: a suspended actor's rows are hidden (api.md 3.9 / 5.18)", () => {
  const inbox = async (t: TestUser, q = ""): Promise<any> => (await api(t.token).get<any>(`/v1/notifications${q}`)).body;
  const unread = async (t: TestUser): Promise<number> => (await api(t.token).get<any>("/v1/notifications/unread-count")).body.unread_count;

  it("guestbook_entry / post_mention previews of a suspended author leave the list, unread_only and the badge; back after expiry", async () => {
    const [s, v, d] = await Promise.all([signup(), signup(), signup()]);
    expect((await api(s.token).post(`/v1/users/@${v.handle}/guestbook`, { body: "정지될 방명록 본문" })).status).toBe(201);
    await createPost(s, `@${v.handle} 정지될 글 본문`);
    await api(d.token).post("/v1/friends/requests", { user: `@${v.handle}` });
    const before = await inbox(v);
    expect(before.items.map((n: any) => [n.type, n.actor.id])).toEqual([
      ["friend_request", d.user.id],
      ["post_mention", s.user.id],
      ["guestbook_entry", s.user.id],
    ]);
    expect(before.unread_count).toBe(3);

    await suspend(s, null);
    const during = await inbox(v);
    expect(during.items.map((n: any) => [n.type, n.actor.id])).toEqual([["friend_request", d.user.id]]);
    expect(JSON.stringify(during)).not.toContain("정지될");
    expect(during.unread_count).toBe(1);
    expect(await unread(v)).toBe(1);
    expect((await inbox(v, "?unread_only=true")).items.map((n: any) => n.actor.id)).toEqual([d.user.id]);
    // marking everything read and the count it returns use the same rule
    expect((await api(v.token).post<any>("/v1/notifications/read", { ids: [during.items[0].id] })).body.unread_count).toBe(0);

    await suspend(s, Date.now() + 60_000); // timed, still running
    expect((await inbox(v)).items.map((n: any) => n.actor.id)).toEqual([d.user.id]);

    await suspend(s, Date.now() - 1000); // expired: rows come back
    const after = await inbox(v);
    expect(after.items.map((n: any) => n.type)).toEqual(["friend_request", "post_mention", "guestbook_entry"]);
    expect(after.unread_count).toBe(2);
  });

  it("boundary matches postVisibilitySql: suspended_until = now is shown again, now + 1 is still hidden", async () => {
    const [s, v] = await Promise.all([signup(), signup()]);
    expect((await api(s.token).post(`/v1/users/@${v.handle}/guestbook`, { body: "글" })).status).toBe(201);
    const t = Date.now() + 10 * 60_000;
    vi.spyOn(Date, "now").mockReturnValue(t);
    await env.DB.prepare("UPDATE users SET suspended_at = ?1, suspended_until = ?2 WHERE id = ?3").bind(t - 1000, t, s.user.id).run();
    expect((await inbox(v)).items.map((n: any) => n.actor.id)).toEqual([s.user.id]);
    expect(await unread(v)).toBe(1);
    await env.DB.prepare("UPDATE users SET suspended_until = ?1 WHERE id = ?2").bind(t + 1, s.user.id).run();
    expect((await inbox(v)).items).toEqual([]);
    expect(await unread(v)).toBe(0);
  });

  it("notification.created badge (single and multi-recipient batch) leaves out a suspended actor's unread rows", async () => {
    const [s, v, w, x] = await Promise.all([signup(), signup(), signup(), signup()]);
    for (const r of [v, w]) expect((await api(s.token).post(`/v1/users/@${r.handle}/guestbook`, { body: "글" })).status).toBe(201);
    expect(await unread(v)).toBe(1);
    await suspend(s, null);
    const [wv, ww] = await Promise.all([connectWs(v.token), connectWs(w.token)]);
    await createPost(x, `@${v.handle} @${w.handle} 둘 다`); // one notifyMany call, two recipients -> batched counts
    expect((await wv.next("notification.created")).data.unread_count).toBe(1);
    expect((await ww.next("notification.created")).data.unread_count).toBe(1);
    expect((await api(x.token).post(`/v1/users/@${v.handle}/guestbook`, { body: "하나" })).status).toBe(201); // single recipient
    expect((await wv.next("notification.created", (d) => d.notification.type === "guestbook_entry")).data.unread_count).toBe(2);
    expect(await unread(v)).toBe(2);
  });
});

describe("user lists keep a suspended member, flagged (decision: api.md 3.9 UserSummary.suspended=true)", () => {
  it("followers / following of a non-suspended owner, /likes and /reposts list S with suspended:true; user search drops S", async () => {
    const [o, s, v] = await Promise.all([signup(), signup(), signup()]);
    await follow(s, o);
    await follow(o, s);
    const post = await createPost(o, "글");
    expect((await api(s.token).put(`/v1/posts/${post.id}/like`)).status).toBe(200);
    expect((await api(s.token).put(`/v1/posts/${post.id}/repost`)).status).toBe(200);
    await suspend(s, null);
    const V = api(v.token);
    for (const p of [`/v1/users/@${o.handle}/followers`, `/v1/users/@${o.handle}/following`, `/v1/posts/${post.id}/likes`, `/v1/posts/${post.id}/reposts`]) {
      const r = await V.get<any>(p);
      expect([p, r.status, r.body.items.map((u: any) => [u.id, u.suspended])]).toEqual([p, 200, [[s.user.id, true]]]);
    }
    expect((await V.get<any>(`/v1/users/search?q=${s.handle}`)).body.items).toEqual([]);
  });
});

describe("embedded quote / reply parent of a suspended author (serializePosts canSee, api.md 3.4 rule 2 / 3.9)", () => {
  // serializePosts judges quoted and reply-parent authors itself, for REST (viewer) and event payloads (viewer null):
  // a suspended one -> quote {available:false, reason:"not_visible"} and reply_to_author null, on every surface.
  it("timeline, posts tab, post detail, search and timeline.item_created hide S's quoted post and S's name; back after expiry", async () => {
    const [s, c, r, v, q, f] = await Promise.all([signup(), signup(), signup(), signup(), signup(), signup()]);
    const tag = `sq${crypto.randomUUID().replace(/-/g, "").slice(0, 10)}`;
    const sp = await createPost(s, "정지될 원글 본문");
    const cq = await createPost(c, `quoting ${tag}`, { quote_of_id: sp.id });
    const rr = await createPost(r, `replying ${tag}`, { reply_to_id: sp.id });
    await follow(v, c);
    await follow(f, q);
    const V = api(v.token);

    const surfaces = async (): Promise<{ quote: any[]; reply: any[]; raw: string }> => {
      const tl = (await V.get<any>("/v1/timeline/following")).body;
      const tab = (await V.get<any>(`/v1/users/@${c.handle}/posts`)).body;
      const replies = (await V.get<any>(`/v1/users/@${r.handle}/posts?filter=replies`)).body;
      const dq = (await V.get<any>(`/v1/posts/${cq.id}`)).body;
      const dr = (await V.get<any>(`/v1/posts/${rr.id}`)).body;
      const search = (await V.get<any>(`/v1/search/posts?q=${tag}`)).body;
      const pick = (items: any[], id: string): any => items.find((i: any) => i.post.id === id)?.post;
      return {
        quote: [pick(tl.items, cq.id), pick(tab.items, cq.id), dq.post, pick(search.items, cq.id)].map((p) => p?.quote),
        reply: [pick(replies.items, rr.id), dr.post, pick(search.items, rr.id)].map((p) => (p === undefined ? "missing" : p.reply_to_author?.id ?? null)),
        raw: JSON.stringify([tl, tab, replies, dq, dr, search]),
      };
    };

    const before = await surfaces();
    for (const qt of before.quote) expect(qt).toMatchObject({ available: true, post: { id: sp.id, author: { id: s.user.id } } });
    expect(before.reply).toEqual([s.user.id, s.user.id, s.user.id]);

    await suspend(s, null);
    const during = await surfaces();
    for (const qt of during.quote) expect(qt).toEqual({ available: false, id: sp.id, reason: "not_visible" });
    expect(during.reply).toEqual([null, null, null]);
    expect(during.raw).not.toContain(s.user.id);
    expect(during.raw).not.toContain(s.handle);
    expect(during.raw).not.toContain("정지될");

    // event payloads (serializePosts with viewer null): repost of the quote, repost of the reply, a fresh quote of the reply
    const wf = await connectWs(f.token);
    expect((await api(q.token).put(`/v1/posts/${cq.id}/repost`)).status).toBe(200);
    const ev1 = (await wf.next("timeline.item_created", (d) => d.item.type === "repost" && d.item.post.id === cq.id)).data.item;
    expect(ev1.post.quote).toEqual({ available: false, id: sp.id, reason: "not_visible" });
    expect((await api(q.token).put(`/v1/posts/${rr.id}/repost`)).status).toBe(200);
    const ev2 = (await wf.next("timeline.item_created", (d) => d.item.type === "repost" && d.item.post.id === rr.id)).data.item;
    expect(ev2.post.reply_to_author).toBeNull();
    const qr = await createPost(q, "quoting the reply", { quote_of_id: rr.id });
    const ev3 = (await wf.next("timeline.item_created", (d) => d.item.post.id === qr.id)).data.item;
    expect(ev3.post.quote).toMatchObject({ available: true, post: { id: rr.id, reply_to_author: null } });
    const events = JSON.stringify([ev1, ev2, ev3]);
    expect(events).not.toContain(s.user.id);
    expect(events).not.toContain("정지될");
    wf.close();

    await suspend(s, Date.now() + 60_000); // timed, still running
    expect((await surfaces()).reply).toEqual([null, null, null]);

    await suspend(s, Date.now() - 1000); // expired: shown again
    const after = await surfaces();
    for (const qt of after.quote) expect(qt).toMatchObject({ available: true, post: { id: sp.id } });
    expect(after.reply).toEqual([s.user.id, s.user.id, s.user.id]);
  });
});
