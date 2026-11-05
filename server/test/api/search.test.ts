import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { newId } from "../../src/lib/ids";
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
