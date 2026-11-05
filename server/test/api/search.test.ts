import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { newId } from "../../src/lib/ids";
import { serializePosts } from "../../src/lib/serialize";
import type { PostRow } from "../../src/types/rows";
import { api, makeFriends, makeSpace, signup, uniqueHandle, type TestUser } from "../helpers";

/** 1:1 dm between two fresh friends. */
async function dmPair(): Promise<{ a: TestUser; b: TestUser; ch: string }> {
  const [a, b] = await Promise.all([signup(), signup()]);
  await makeFriends(a, b);
  const r = await api(a.token).post<any>("/v1/dms", { users: [`@${b.handle}`] });
  if (r.status !== 201) throw new Error(`dm create failed: ${r.status} ${JSON.stringify(r.body)}`);
  return { a, b, ch: r.body.channel.id };
}

/** Sends a text message as `u` in `ch` and returns it (throws unless 201). */
async function post(u: TestUser, ch: string, body: string): Promise<any> {
  const r = await api(u.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: crypto.randomUUID().slice(0, 12), body });
  if (r.status !== 201) throw new Error(`post failed: ${r.status} ${JSON.stringify(r.body)}`);
  return r.body.message;
}

/**
 * Seeds a post (and its post_hashtags rows) with raw SQL; posts_fts is filled by the trigger. `deleted` sets deleted_at but
 * keeps the body (so FTS still matches and only the deleted_at filter can drop it); tags are inserted even for deleted posts.
 */
async function insertPost(
  author: TestUser,
  body: string,
  opts: { createdAt?: number; tags?: string[]; deleted?: boolean; quoteOf?: string; replyTo?: string } = {},
): Promise<PostRow> {
  const id = newId();
  const t = opts.createdAt ?? Date.now();
  const stmts = [
    env.DB.prepare(
      `INSERT INTO posts (id, author_id, body, reply_to_id, root_id, quote_of_id, created_at, deleted_at, deleted_by)
       VALUES (?1, ?2, ?3, ?4, ?4, ?5, ?6, ?7, ?8)`,
    ).bind(id, author.user.id, body, opts.replyTo ?? null, opts.quoteOf ?? null, t, opts.deleted ? Date.now() : null, opts.deleted ? author.user.id : null),
    ...(opts.tags ?? []).map((tag) =>
      env.DB.prepare("INSERT INTO post_hashtags (tag, post_id, author_id, created_at) VALUES (?1, ?2, ?3, ?4)").bind(tag, id, author.user.id, t),
    ),
  ];
  await env.DB.batch(stmts);
  return (await env.DB.prepare("SELECT * FROM posts WHERE id = ?1").bind(id).first<PostRow>())!;
}

/** follower -> followee follow row with the counters the follow service keeps. */
async function followSql(follower: TestUser, followee: TestUser, status: "active" | "pending"): Promise<void> {
  const t = Date.now();
  const stmts = [
    env.DB.prepare("INSERT INTO follows (follower_id, followee_id, status, created_at, accepted_at) VALUES (?1, ?2, ?3, ?4, ?5)").bind(
      follower.user.id,
      followee.user.id,
      status,
      t,
      status === "active" ? t : null,
    ),
  ];
  if (status === "active") {
    stmts.push(env.DB.prepare("UPDATE users SET follower_count = follower_count + 1 WHERE id = ?1").bind(followee.user.id));
    stmts.push(env.DB.prepare("UPDATE users SET following_count = following_count + 1 WHERE id = ?1").bind(follower.user.id));
  }
  await env.DB.batch(stmts);
}

describe("schema", () => {
  it("applies 0001 and 0002", async () => {
    const { results } = await env.DB.prepare("SELECT name FROM sqlite_master WHERE type IN ('table') ORDER BY name").all<{ name: string }>();
    const names = results.map((r) => r.name);
    for (const t of ["users", "sessions", "channels", "messages", "message_mentions", "user_presence", "messages_fts", "posts_fts", "admin_actions"]) expect(names).toContain(t);
  });
  it("trigram FTS matches a 3-char Korean substring", async () => {
    const id = "01JTESTFTS" + crypto.randomUUID().replace(/-/g, "").slice(0, 16).toUpperCase();
    await env.DB.batch([
      env.DB.prepare("INSERT INTO users (id, handle, display_name, password_hash, created_at, updated_at) VALUES (?1, ?2, 'x', 'x', 0, 0)").bind(id, "fts_" + id.slice(-8).toLowerCase()),
      env.DB.prepare("INSERT INTO channels (id, kind, created_by, created_at, updated_at) VALUES (?1, 'group_dm', ?1, 0, 0)").bind(id),
      env.DB.prepare("INSERT INTO messages (id, channel_id, author_id, body, created_at) VALUES (?1, ?1, ?1, '오늘 배포는 몇 시?', 0)").bind(id),
    ]);
    const row = await env.DB.prepare("SELECT m.id FROM messages_fts f JOIN messages m ON m.rowid = f.rowid WHERE messages_fts MATCH ?1 AND m.id = ?2").bind('"배포는"', id).first();
    expect(row).not.toBeNull();
  });
});

describe("message search", () => {
  it("3+ chars via FTS, 2 chars via LIKE, scoped to accessible channels, excludes blocked authors", async () => {
    const [o, m, out, blk] = await Promise.all([signup(), signup(), signup(), signup()]); const { generalChannelId: g } = await makeSpace(o, [m, blk]);
    const tag = uniqueHandle("k").slice(-6);
    const hit = await post(o, g, `오늘 배포는 ${tag} 세 시`); await post(blk, g, `배포는 ${tag} 차단`);
    const { a, ch } = await dmPair(); await post(a, ch, `배포는 ${tag} 남의 DM`);
    await api(m.token).put(`/v1/blocks/@${blk.handle}`);
    const r = (await api(m.token).get<any>(`/v1/search/messages?q=${encodeURIComponent("배포는 " + tag)}`)).body;
    expect(r.items.map((i: any) => i.message.id)).toEqual([hit.id]);
    expect(r.items[0]).toMatchObject({ channel: { id: g, kind: "text", name: "일반" }, highlights: [{ start: 3, length: 3 }, expect.any(Object)] });
    expect((await api(m.token).get<any>(`/v1/search/messages?q=${encodeURIComponent("배포 " + tag)}`)).body.items.map((i: any) => i.message.id)).toEqual([hit.id]);
    expect((await api(out.token).get<any>(`/v1/search/messages?q=${tag}`)).body.items).toEqual([]);
    expect((await api(out.token).get(`/v1/search/messages?q=${tag}&channel_id=${g}`)).status).toBe(404);
  });
  it("Review Focus 1: NFD query finds NFC text; deleted messages disappear; empty q 400", async () => {
    const [o] = await Promise.all([signup()]); const { generalChannelId: g } = await makeSpace(o, []); const tag = uniqueHandle("n").slice(-6);
    const msg = await post(o, g, `한글검색 ${tag}`);
    expect((await api(o.token).get<any>(`/v1/search/messages?q=${encodeURIComponent("한글검색".normalize("NFD") + " " + tag)}`)).body.items).toHaveLength(1);
    await api(o.token).del(`/v1/messages/${msg.id}`);
    expect((await api(o.token).get<any>(`/v1/search/messages?q=${tag}`)).body.items).toEqual([]);
    expect((await api(o.token).get(`/v1/search/messages?q=`)).status).toBe(400);
  });
  it("author and space_id narrow the scope; newest-first cursor pages; LIKE wildcards are literal", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const [{ space: s1, generalChannelId: g1 }, { generalChannelId: g2 }] = await Promise.all([makeSpace(o, [m]), makeSpace(o, [m])]);
    const tag = uniqueHandle("p").slice(-6);
    const a1 = await post(o, g1, `페이지 ${tag} 하나`);
    const a2 = await post(m, g1, `페이지 ${tag} 둘`);
    const a3 = await post(o, g2, `페이지 ${tag} 셋 100%`);
    const q = (extra: string) => api(m.token).get<any>(`/v1/search/messages?q=${encodeURIComponent("페이지 " + tag)}${extra}`);
    const ids = (r: any) => r.body.items.map((i: any) => i.message.id);
    expect(ids(await q(""))).toEqual([a3.id, a2.id, a1.id]);
    expect(ids(await q(`&space_id=${s1.id}`))).toEqual([a2.id, a1.id]);
    expect(ids(await q(`&author=@${o.handle}`))).toEqual([a3.id, a1.id]);
    const p1 = await q("&limit=2");
    expect(ids(p1)).toEqual([a3.id, a2.id]);
    expect(p1.body.next_cursor).toEqual(expect.any(String));
    const p2 = await q(`&limit=2&cursor=${p1.body.next_cursor}`);
    expect(ids(p2)).toEqual([a1.id]);
    expect(p2.body.next_cursor).toBeNull();
    expect((await q("&cursor=garbage")).status).toBe(400);
    expect(ids(await api(m.token).get<any>(`/v1/search/messages?q=${encodeURIComponent(tag + " 0%")}`))).toEqual([a3.id]);
    expect(ids(await api(m.token).get<any>(`/v1/search/messages?q=${encodeURIComponent(tag + " _0")}`))).toEqual([]);
    const out = await signup();
    expect((await api(out.token).get(`/v1/search/messages?q=${tag}&space_id=${s1.id}`)).status).toBe(404);
  });
  it("more than 500 accessible channels -> 422 search_scope, narrowing with channel_id still works", async () => {
    const u = await signup();
    const t = Date.now();
    const chans = JSON.stringify(Array.from({ length: 501 }, () => newId(t)));
    await env.DB.batch([
      env.DB.prepare("INSERT INTO channels (id, kind, created_by, member_count, created_at, updated_at) SELECT value, 'group_dm', ?2, 1, ?3, ?3 FROM json_each(?1)").bind(chans, u.user.id, t),
      env.DB.prepare("INSERT INTO channel_members (channel_id, user_id, joined_at) SELECT value, ?2, ?3 FROM json_each(?1)").bind(chans, u.user.id, t),
    ]);
    const r = await api(u.token).get<any>(`/v1/search/messages?q=anything`);
    expect(r.status).toBe(422);
    expect(r.body.error).toMatchObject({ code: "limit_exceeded", details: { what: "search_scope", limit: 500 } });
    expect((await api(u.token).get<any>(`/v1/search/messages?q=anything&channel_id=${JSON.parse(chans)[0]}`)).body.items).toEqual([]);
  });
});

describe("post search", () => {
  it("FTS + LIKE, visibility rules, author filter, highlights", async () => {
    const [v, pub, priv, blk, sus] = await Promise.all([signup(), signup(), signup(), signup(), signup()]);
    const tag = uniqueHandle("p").slice(-6);
    await env.DB.prepare("UPDATE users SET is_private=1 WHERE id=?").bind(priv.user.id).run();
    const hit = await insertPost(pub, `오늘 배포는 ${tag} 완료`);
    const hidden = await insertPost(priv, `배포는 ${tag} 비공개`);
    await insertPost(blk, `배포는 ${tag} 차단`); await insertPost(sus, `배포는 ${tag} 정지`); await insertPost(pub, `배포는 ${tag} 삭제`, { deleted: true });
    await api(v.token).put(`/v1/blocks/@${blk.handle}`);
    await env.DB.prepare("UPDATE users SET suspended_at=? WHERE id=?").bind(Date.now(), sus.user.id).run();
    const q = (s: string, u = v) => api(u.token).get<any>(`/v1/search/posts?q=${encodeURIComponent(s)}`);
    const r = (await q(`배포는 ${tag}`)).body;
    expect(r.items.map((i: any) => i.post.id)).toEqual([hit.id]);
    expect(r.items[0]).toMatchObject({ post: { author: { id: pub.user.id }, body: `오늘 배포는 ${tag} 완료`, deleted: false, quote: null, viewer: { liked: false, reposted: false } }, highlights: [{ start: 3, length: 3 }, expect.any(Object)] });
    expect((await q(`배포 ${tag}`)).body.items.map((i: any) => i.post.id)).toEqual([hit.id]);            // 2-char term -> LIKE mode
    await followSql(v, priv, "active");
    expect((await q(`배포는 ${tag}`)).body.items.map((i: any) => i.post.id)).toEqual([hidden.id, hit.id]); // active follower now sees private author
    expect((await api(v.token).get<any>(`/v1/search/posts?q=${tag}&author=@${pub.handle}`)).body.items.map((i: any) => i.post.id)).toEqual([hit.id]);
    expect((await api(v.token).get(`/v1/search/posts?q=`)).status).toBe(400);
  });
  it("NFD query; cursor pages newest first; quote of a deleted post is a placeholder", async () => {
    const a = await signup(); const tag = uniqueHandle("c").slice(-6);
    const old = await insertPost(a, `한글검색 ${tag} 1`, { deleted: false }); const gone = await insertPost(a, "원글", { deleted: true });
    const q2 = await insertPost(a, `한글검색 ${tag} 2`, { quoteOf: gone.id });
    const p1 = (await api(a.token).get<any>(`/v1/search/posts?q=${encodeURIComponent("한글검색".normalize("NFD") + " " + tag)}&limit=1`)).body;
    expect(p1.items.map((i: any) => i.post.id)).toEqual([q2.id]); expect(p1.items[0].post.quote).toEqual({ available: false, id: gone.id, reason: "deleted" });
    const p2 = (await api(a.token).get<any>(`/v1/search/posts?q=${encodeURIComponent("한글검색 " + tag)}&limit=1&cursor=${p1.next_cursor}`)).body;
    expect(p2.items.map((i: any) => i.post.id)).toEqual([old.id]); expect(p2.next_cursor).toBeNull();
  });
  it("author that does not exist -> 404; bad cursor -> 400; LIKE wildcards are literal", async () => {
    const a = await signup(); const tag = uniqueHandle("w").slice(-6);
    const hit = await insertPost(a, `${tag} 100%`);
    await insertPost(a, `${tag} 1000`);
    expect((await api(a.token).get(`/v1/search/posts?q=${tag}&author=@nobody_${tag}`)).status).toBe(404);
    expect((await api(a.token).get(`/v1/search/posts?q=${tag}&cursor=garbage`)).status).toBe(400);
    expect((await api(a.token).get<any>(`/v1/search/posts?q=${encodeURIComponent(tag + " 0%")}`)).body.items.map((i: any) => i.post.id)).toEqual([hit.id]);
  });
});

describe("serializePosts", () => {
  it("media (position order, removed dropped), hashtags, mentions, viewer flags, quote levels, reply_to_author, placeholders", async () => {
    const [a, b, v, p] = await Promise.all([signup(), signup(), signup(), signup()]);
    await env.DB.prepare("UPDATE users SET is_private=1 WHERE id=?").bind(p.user.id).run();
    const t = Date.now();
    const [u0, u1, u2] = [newId(), newId(), newId()];
    const up = (id: string, status: string) =>
      env.DB.prepare(
        "INSERT INTO uploads (id, owner_id, purpose, r2_key, content_type, size, width, height, status, created_at) VALUES (?1, ?2, 'post', ?3, 'image/png', 10, 4, NULL, ?4, ?5)",
      ).bind(id, a.user.id, `posts/${a.user.id}/${id}.png`, status, t);
    const root = await insertPost(b, "root");
    const quoted = await insertPost(b, "quoted", { quoteOf: root.id });
    const privQ = await insertPost(p, "private");
    const main = await insertPost(a, `main #가 #b @${b.handle}`, { replyTo: root.id, quoteOf: quoted.id });
    const q2 = await insertPost(a, "quotes private", { quoteOf: privQ.id });
    const del = await insertPost(a, "secret #x", { deleted: true, tags: ["x"] });
    await env.DB.batch([
      up(u0, "attached"), up(u1, "attached"), up(u2, "removed"),
      env.DB.prepare("INSERT INTO post_media (post_id, position, upload_id) VALUES (?1, 1, ?2), (?1, 0, ?3), (?1, 2, ?4)").bind(main.id, u0, u1, u2),
      env.DB.prepare("INSERT INTO post_hashtags (tag, post_id, author_id, created_at) VALUES ('가', ?1, ?2, ?3), ('b', ?1, ?2, ?3)").bind(main.id, a.user.id, t),
      env.DB.prepare("INSERT INTO post_mentions (post_id, user_id) VALUES (?1, ?2)").bind(main.id, b.user.id),
      env.DB.prepare("INSERT INTO post_likes (post_id, user_id, created_at) VALUES (?1, ?2, ?3)").bind(main.id, v.user.id, t),
      env.DB.prepare("UPDATE posts SET like_count = 1 WHERE id = ?1").bind(main.id),
    ]);
    const mainNow = (await env.DB.prepare("SELECT * FROM posts WHERE id = ?1").bind(main.id).first<PostRow>())!;
    const [m, qp, d] = await serializePosts(env.DB, v.user.id, [mainNow, q2, del]);
    expect(m).toMatchObject({
      id: main.id, author: { id: a.user.id }, body: main.body, reply_to_id: root.id, root_id: root.id, reply_to_author: { id: b.user.id },
      hashtags: ["가", "b"], mentions: [{ user_id: b.user.id, handle: b.handle }], like_count: 1, viewer: { liked: true, reposted: false }, poll: null, deleted: false,
    });
    expect(m.media).toEqual([
      { id: u1, url: `/v1/media/${u1}`, content_type: "image/png", size: 10, width: 4, height: null },
      { id: u0, url: `/v1/media/${u0}`, content_type: "image/png", size: 10, width: 4, height: null },
    ]);
    expect(m.quote).toMatchObject({ available: true, post: { id: quoted.id, author: { id: b.user.id }, body: "quoted", quote: null } });
    expect(qp.quote).toEqual({ available: false, id: privQ.id, reason: "not_visible" });
    expect(d).toMatchObject({ id: del.id, body: "", media: [], poll: null, hashtags: [], mentions: [], quote: null, reply_to_author: null, deleted: true, author: { handle: "" } });
    const [anon] = await serializePosts(env.DB, null, [mainNow]);
    expect(anon.viewer).toEqual({ liked: false, reposted: false });
    expect(await serializePosts(env.DB, v.user.id, [])).toEqual([]);
    await env.DB.prepare("INSERT INTO blocks (blocker_id, blocked_id, created_at) VALUES (?1, ?2, ?3)").bind(b.user.id, v.user.id, t).run();
    const [mb] = await serializePosts(env.DB, v.user.id, [mainNow]);
    expect(mb).toMatchObject({ reply_to_author: null, quote: { available: false, id: quoted.id, reason: "not_visible" } }); // blocked by b
    await followSql(v, p, "active");
    expect((await serializePosts(env.DB, v.user.id, [q2]))[0].quote).toMatchObject({ available: true, post: { id: privQ.id } });
  });
});

describe("hashtag search", () => {
  it("prefix over last 7 days, 24h counts, excludes private/suspended/deleted, # and NFD ignored", async () => {
    const [a, b, p] = await Promise.all([signup(), signup(), signup()]); const base = "첫눈" + uniqueHandle("h").slice(-5);
    await env.DB.prepare("UPDATE users SET is_private=1 WHERE id=?").bind(p.user.id).run();
    await insertPost(a, "x", { tags: [base] }); await insertPost(b, "y", { tags: [base] });
    await insertPost(a, "z", { tags: [base + "_old"], createdAt: Date.now() - 3 * 86_400_000 });   // inside 7d, outside 24h
    await insertPost(a, "w", { tags: [base + "_gone"], createdAt: Date.now() - 8 * 86_400_000 });  // outside 7d
    await insertPost(p, "s", { tags: [base + "_priv"] }); await insertPost(a, "d", { tags: [base + "_del"], deleted: true });
    const r = await api(a.token).get<any>(`/v1/hashtags/search?q=${encodeURIComponent("#" + base.normalize("NFD"))}`);
    expect(r.body.items).toEqual([{ tag: base, post_count_24h: 2 }, { tag: base + "_old", post_count_24h: 0 }]);
    expect((await api(a.token).get(`/v1/hashtags/search?q=%23`)).status).toBe(400);
    expect((await api(a.token).get<any>(`/v1/hashtags/search?q=${encodeURIComponent(base)}&limit=1`)).body.items).toHaveLength(1);
  });
  it("suspended authors are excluded, q is case-folded, over-long q -> 400", async () => {
    const [a, s] = await Promise.all([signup(), signup()]); const base = "snow" + uniqueHandle("h").slice(-5);
    await insertPost(a, "x", { tags: [base] }); await insertPost(s, "y", { tags: [base + "_sus"] });
    await env.DB.prepare("UPDATE users SET suspended_at=? WHERE id=?").bind(Date.now(), s.user.id).run();
    expect((await api(a.token).get<any>(`/v1/hashtags/search?q=${encodeURIComponent(base.toUpperCase())}`)).body.items).toEqual([{ tag: base, post_count_24h: 1 }]);
    expect((await api(a.token).get(`/v1/hashtags/search?q=${"a".repeat(51)}`)).status).toBe(400);
  });
});
