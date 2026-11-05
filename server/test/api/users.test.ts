import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { newId } from "../../src/lib/ids";
import { api, signup, uniqueHandle } from "../helpers";

// ---- local fixtures (this file only) ----

type U = { user: { id: string } };

/** Friendship row as the friends service writes it (user_a < user_b). */
async function makeFriendsSql(a: U, b: U): Promise<void> {
  const [lo, hi] = a.user.id < b.user.id ? [a.user.id, b.user.id] : [b.user.id, a.user.id];
  await env.DB.prepare("INSERT INTO friendships (user_a, user_b, created_at) VALUES (?1, ?2, ?3)").bind(lo, hi, Date.now()).run();
}

async function blockSql(blocker: U, blocked: U): Promise<void> {
  await env.DB.prepare("INSERT INTO blocks (blocker_id, blocked_id, created_at) VALUES (?1, ?2, ?3)").bind(blocker.user.id, blocked.user.id, Date.now()).run();
}

/** A live space with the given members (first = owner); no channels needed for these tests. */
async function spaceSql(members: U[]): Promise<string> {
  const id = newId();
  const t = Date.now();
  await env.DB.batch([
    env.DB.prepare(
      `INSERT INTO spaces (id, name, description, owner_id, default_channel_id, member_count, created_at, updated_at)
       VALUES (?1, 's', '', ?2, ?3, ?4, ?5, ?5)`,
    ).bind(id, members[0].user.id, newId(), members.length, t),
    ...members.map((m, i) =>
      env.DB.prepare("INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?1, ?2, ?3, ?4)").bind(id, m.user.id, i === 0 ? "owner" : "member", t),
    ),
  ]);
  return id;
}

describe("users", () => {
  it("profile relationship, presence visibility, block keeps 200", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    await makeFriendsSql(a, b);
    const pb = (await api(a.token).get<any>(`/v1/users/@${b.handle}`)).body.user;
    expect(pb.relationship).toMatchObject({ is_friend: true, can_dm: true, blocked: false, following: "none", can_view_posts: true });
    expect(pb.presence).toBe("offline");
    expect((await api(a.token).get<any>(`/v1/users/${c.user.id}`)).body.user.presence).toBeNull();
    expect((await api(a.token).get<any>("/v1/users/@me")).body.user.relationship).toBeNull();
    await blockSql(c, a);
    const pc = await api(a.token).get<any>(`/v1/users/@${c.handle}`);
    expect(pc.status).toBe(200);
    expect(pc.body.user.relationship).toMatchObject({ can_dm: false, can_view_posts: false, blocked: false });
    expect((await api(a.token).get(`/v1/users/@nobody_${Date.now() % 1e6}`)).status).toBe(404);
  });

  it("profile shape: counts, friend request, follows, shared spaces, my block", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const t = Date.now();
    const reqId = newId();
    await env.DB.batch([
      env.DB.prepare("INSERT INTO friend_requests (id, from_user_id, to_user_id, created_at) VALUES (?1, ?2, ?3, ?4)").bind(reqId, b.user.id, a.user.id, t),
      env.DB.prepare("INSERT INTO follows (follower_id, followee_id, status, created_at, accepted_at) VALUES (?1, ?2, 'active', ?3, ?3)").bind(a.user.id, b.user.id, t),
      env.DB.prepare("INSERT INTO follows (follower_id, followee_id, status, created_at) VALUES (?1, ?2, 'pending', ?3)").bind(b.user.id, a.user.id, t),
    ]);
    const space = await spaceSql([a, b]);
    const pb = (await api(a.token).get<any>(`/v1/users/${b.user.id}`)).body.user;
    expect(pb).toMatchObject({ id: b.user.id, handle: b.handle, bio: "", follower_count: 0, post_count: 0, last_seen_at: null, presence: "offline" });
    expect(typeof pb.created_at).toBe("string");
    expect(pb.relationship).toEqual({
      is_friend: false,
      friend_request: { id: reqId, direction: "incoming" },
      following: "active",
      followed_by: "pending",
      blocked: false,
      can_dm: true,
      can_view_posts: true,
      shared_space_ids: [space],
    });
    expect((await api(b.token).get<any>(`/v1/users/${a.user.id}`)).body.user.relationship.friend_request).toEqual({ id: reqId, direction: "outgoing" });
    await blockSql(a, b);
    expect((await api(a.token).get<any>(`/v1/users/${b.user.id}`)).body.user.relationship).toMatchObject({ blocked: true, can_dm: false, can_view_posts: false });
  });

  it("private account: can_view_posts only for active followers", async () => {
    const [p, v] = await Promise.all([signup(), signup()]);
    await env.DB.prepare("UPDATE users SET is_private = 1 WHERE id = ?").bind(p.user.id).run();
    expect((await api(v.token).get<any>(`/v1/users/@${p.handle}`)).body.user.relationship.can_view_posts).toBe(false);
    await env.DB.prepare("INSERT INTO follows (follower_id, followee_id, status, created_at, accepted_at) VALUES (?1, ?2, 'active', ?3, ?3)").bind(v.user.id, p.user.id, Date.now()).run();
    expect((await api(v.token).get<any>(`/v1/users/@${p.handle}`)).body.user.relationship.can_view_posts).toBe(true);
  });

  it("search excludes blocked and suspended; ids lookup drops unknown", async () => {
    const pre = uniqueHandle("sr").slice(0, 8);
    const [me, x, y, z] = [await signup(), await signup({ handle: pre + "_x" }), await signup({ handle: pre + "_y" }), await signup({ handle: pre + "_z" })];
    await blockSql(y, me);
    await env.DB.prepare("UPDATE users SET suspended_at=? WHERE id=?").bind(Date.now(), z.user.id).run();
    expect((await api(me.token).get<any>(`/v1/users/search?q=${pre}`)).body.items.map((u: any) => u.id)).toEqual([x.user.id]);
    expect((await api(me.token).get<any>(`/v1/users?ids=${x.user.id},${newId()}`)).body.items).toHaveLength(1);
  });

  it("search: display_name substring (NFD query), LIKE wildcards literal, limit bounds", async () => {
    const tag = uniqueHandle("dn").slice(3, 11);
    const [me, k1, k2] = await Promise.all([signup(), signup({ displayName: `마실${tag}개발` }), signup({ displayName: `개발자${tag}` })]);
    const q = encodeURIComponent(`${tag}개발`.normalize("NFD"));
    expect((await api(me.token).get<any>(`/v1/users/search?q=${q}`)).body.items.map((u: any) => u.id)).toEqual([k1.user.id]);
    const both = (await api(me.token).get<any>(`/v1/users/search?q=${tag}&limit=20`)).body.items.map((u: any) => u.id);
    expect(both.sort()).toEqual([k1.user.id, k2.user.id].sort());
    expect((await api(me.token).get<any>(`/v1/users/search?q=${tag}&limit=1`)).body.items).toHaveLength(1);
    expect((await api(me.token).get<any>("/v1/users/search?q=%25")).body.items).toEqual([]); // "%" is not a wildcard
    expect((await api(me.token).get<any>("/v1/users/search?q=")).status).toBe(400);
    expect((await api(me.token).get<any>("/v1/users/search")).status).toBe(400);
    expect((await api(me.token).get<any>(`/v1/users/search?q=${tag}&limit=0`)).status).toBe(400);
  });

  it("ids lookup keeps input order; > 100 or malformed -> 400", async () => {
    const [me, a, b] = await Promise.all([signup(), signup(), signup()]);
    const items = (await api(me.token).get<any>(`/v1/users?ids=${b.user.id},${a.user.id},${b.user.id}`)).body.items;
    expect(items.map((u: any) => u.id)).toEqual([b.user.id, a.user.id]);
    expect(items[0]).toEqual(expect.objectContaining({ handle: b.handle, suspended: false, status: null }));
    const many = Array.from({ length: 101 }, () => newId()).join(",");
    expect((await api(me.token).get<any>(`/v1/users?ids=${many}`)).body.error.details.fields.ids).toBe("invalid");
    expect((await api(me.token).get<any>("/v1/users?ids=nope")).status).toBe(400);
  });
});
