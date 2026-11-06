import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import * as P from "../../src/lib/permissions";
import { newId } from "../../src/lib/ids";
import type { PostRow } from "../../src/types/rows";
import { api, makeFriends, signup, type TestUser } from "../helpers";

// ---- local fixtures (this file only): seed relations with raw SQL ----

async function friend(a: TestUser, b: TestUser): Promise<void> {
  const [lo, hi] = a.user.id < b.user.id ? [a.user.id, b.user.id] : [b.user.id, a.user.id];
  await env.DB.prepare("INSERT INTO friendships (user_a, user_b, created_at) VALUES (?1, ?2, ?3)").bind(lo, hi, Date.now()).run();
}

async function block(blocker: TestUser, blocked: TestUser): Promise<void> {
  await env.DB.prepare("INSERT INTO blocks (blocker_id, blocked_id, created_at) VALUES (?1, ?2, ?3)").bind(blocker.user.id, blocked.user.id, Date.now()).run();
}

async function policy(u: TestUser, p: "friends_only" | "friends_and_spaces"): Promise<void> {
  await env.DB.prepare("UPDATE user_settings SET dm_policy = ?1 WHERE user_id = ?2").bind(p, u.user.id).run();
}

/** Space with owner + members (role member); returns spaceId. */
async function space(owner: TestUser, members: TestUser[] = []): Promise<string> {
  const id = newId();
  const t = Date.now();
  await env.DB.batch([
    env.DB.prepare("INSERT INTO spaces (id, name, owner_id, member_count, created_at, updated_at) VALUES (?1, 'test', ?2, ?3, ?4, ?4)").bind(id, owner.user.id, members.length + 1, t),
    env.DB.prepare("INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?1, ?2, 'owner', ?3)").bind(id, owner.user.id, t),
    ...members.map((m) => env.DB.prepare("INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?1, ?2, 'member', ?3)").bind(id, m.user.id, t)),
  ]);
  return id;
}

async function textChannel(spaceId: string, creator: TestUser, opts: { private?: boolean; members?: TestUser[] } = {}): Promise<string> {
  const id = newId();
  const t = Date.now();
  const members = opts.members ?? [];
  await env.DB.batch([
    env.DB.prepare("INSERT INTO channels (id, kind, space_id, name, is_private, created_by, member_count, created_at, updated_at) VALUES (?1, 'text', ?2, ?3, ?4, ?5, ?6, ?7, ?7)")
      .bind(id, spaceId, "c" + id.slice(-10).toLowerCase(), opts.private ? 1 : 0, creator.user.id, members.length, t),
    ...members.map((m) => env.DB.prepare("INSERT INTO channel_members (channel_id, user_id, joined_at) VALUES (?1, ?2, ?3)").bind(id, m.user.id, t)),
  ]);
  return id;
}

async function privateText(spaceId: string, members: TestUser[]): Promise<string> {
  return textChannel(spaceId, members[0], { private: true, members });
}

async function dm(a: TestUser, b: TestUser): Promise<string> {
  const id = newId();
  const t = Date.now();
  const key = a.user.id < b.user.id ? `${a.user.id}:${b.user.id}` : `${b.user.id}:${a.user.id}`;
  await env.DB.batch([
    env.DB.prepare("INSERT INTO channels (id, kind, created_by, dm_key, member_count, created_at, updated_at) VALUES (?1, 'dm', ?2, ?3, 2, ?4, ?4)").bind(id, a.user.id, key, t),
    env.DB.prepare("INSERT INTO channel_members (channel_id, user_id, joined_at) VALUES (?1, ?2, ?3)").bind(id, a.user.id, t),
    env.DB.prepare("INSERT INTO channel_members (channel_id, user_id, joined_at) VALUES (?1, ?2, ?3)").bind(id, b.user.id, t),
  ]);
  return id;
}

async function groupDm(owner: TestUser, members: TestUser[]): Promise<string> {
  const id = newId();
  const t = Date.now();
  const all = [owner, ...members];
  await env.DB.batch([
    env.DB.prepare("INSERT INTO channels (id, kind, owner_id, created_by, member_count, created_at, updated_at) VALUES (?1, 'group_dm', ?2, ?2, ?3, ?4, ?4)").bind(id, owner.user.id, all.length, t),
    ...all.map((m) => env.DB.prepare("INSERT INTO channel_members (channel_id, user_id, joined_at) VALUES (?1, ?2, ?3)").bind(id, m.user.id, t)),
  ]);
  return id;
}

async function insertPost(author: TestUser): Promise<PostRow> {
  const id = newId();
  await env.DB.prepare("INSERT INTO posts (id, author_id, body, created_at) VALUES (?1, ?2, 'hello', ?3)").bind(id, author.user.id, Date.now()).run();
  return (await env.DB.prepare("SELECT * FROM posts WHERE id = ?1").bind(id).first<PostRow>())!;
}

/** Runs postVisibilitySql over the given post ids and returns the visible ones (id order). */
async function visibleViaSql(viewerId: string, postIds: string[]): Promise<string[]> {
  const vis = P.postVisibilitySql(viewerId, "p");
  const { results } = await env.DB
    .prepare(`SELECT p.id FROM posts p WHERE p.id IN (SELECT value FROM json_each(?)) AND ${vis.sql} ORDER BY p.id`)
    .bind(JSON.stringify(postIds), ...vis.params)
    .all<{ id: string }>();
  return results.map((r) => r.id);
}

describe("rules", () => {
  it("canDm truth table", async () => {
    const [a, b, c, d] = await Promise.all([signup(), signup(), signup(), signup()]);
    expect(await P.canDm(env.DB, a.user.id, b.user.id)).toBe(false); // strangers
    await friend(a, b);
    expect(await P.canDm(env.DB, a.user.id, b.user.id)).toBe(true);
    await space(a, [c]);
    expect(await P.canDm(env.DB, a.user.id, c.user.id)).toBe(true);
    await policy(c, "friends_only");
    expect(await P.canDm(env.DB, a.user.id, c.user.id)).toBe(false);
    expect(await P.canDm(env.DB, c.user.id, a.user.id)).toBe(false); // either side's friends_only applies
    await block(b, a);
    expect(await P.canDm(env.DB, a.user.id, b.user.id)).toBe(false); // either direction
    expect(await P.canDm(env.DB, b.user.id, a.user.id)).toBe(false);
    expect(await P.canDm(env.DB, a.user.id, a.user.id)).toBe(false);
    await env.DB.prepare("UPDATE users SET suspended_at=? WHERE id=?").bind(Date.now(), d.user.id).run();
    await friend(a, d);
    expect(await P.canDm(env.DB, a.user.id, d.user.id)).toBe(false);
    // suspension that already expired counts as active again
    await env.DB.prepare("UPDATE users SET suspended_until=? WHERE id=?").bind(Date.now() - 1000, d.user.id).run();
    expect(await P.canDm(env.DB, a.user.id, d.user.id)).toBe(true);
  });

  it("canDmMany splits ok/denied in input order", async () => {
    const [a, b, c, d] = await Promise.all([signup(), signup(), signup(), signup()]);
    await friend(a, b);
    await friend(a, d);
    await block(d, a);
    const r = await P.canDmMany(env.DB, a.user.id, [c.user.id, b.user.id, d.user.id, b.user.id, a.user.id, "01UNKNOWNUSER0000000000000"]);
    expect(r.ok).toEqual([b.user.id]);
    expect(r.denied).toEqual([c.user.id, d.user.id, a.user.id, "01UNKNOWNUSER0000000000000"]);
    expect(await P.canDmMany(env.DB, a.user.id, [])).toEqual({ ok: [], denied: [] });
  });

  it("deleted space no longer counts as shared", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const s = await space(a, [b]);
    expect(await P.shareSpace(env.DB, a.user.id, b.user.id)).toBe(true);
    await env.DB.prepare("UPDATE spaces SET deleted_at=? WHERE id=?").bind(Date.now(), s).run();
    expect(await P.shareSpace(env.DB, a.user.id, b.user.id)).toBe(false);
    expect(await P.canDm(env.DB, a.user.id, b.user.id)).toBe(false);
    expect(await P.getSpaceRole(env.DB, s, a.user.id)).toBeNull();
  });

  it("private text channel requires current space membership", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    const s = await space(a, [b, c]);
    const ch = await privateText(s, [a, b]);
    expect((await P.getChannelAccess(env.DB, ch, b.user.id))?.canRead).toBe(true);
    expect(await P.getChannelAccess(env.DB, ch, c.user.id)).toBeNull(); // space member, not channel member
    await env.DB.prepare("DELETE FROM space_members WHERE space_id=? AND user_id=?").bind(s, b.user.id).run();
    expect(await P.getChannelAccess(env.DB, ch, b.user.id)).toBeNull();
    await expect(P.requireChannelRead(env.DB, ch, b.user.id)).rejects.toMatchObject({ status: 404 });
    // owner: moderator of a text channel; deleted channel -> null
    const access = await P.requireChannelWrite(env.DB, ch, a.user.id);
    expect(access).toMatchObject({ canRead: true, canWrite: true, readOnlyReason: null, spaceRole: "owner", otherUserId: null });
    expect(P.canModerateChannel(access)).toBe(true);
    await env.DB.prepare("UPDATE channels SET deleted_at=? WHERE id=?").bind(Date.now(), ch).run();
    expect(await P.getChannelAccess(env.DB, ch, a.user.id)).toBeNull();
  });

  it("1:1 dm becomes read-only; reason 'blocked' only for the blocker", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await friend(a, b);
    const ch = await dm(a, b);
    expect(await P.getChannelAccess(env.DB, ch, a.user.id)).toMatchObject({ canRead: true, canWrite: true, readOnlyReason: null, otherUserId: b.user.id, spaceRole: null });
    await block(a, b);
    await env.DB.prepare("DELETE FROM friendships WHERE user_a IN (?1,?2)").bind(a.user.id, b.user.id).run();
    expect(await P.getChannelAccess(env.DB, ch, a.user.id)).toMatchObject({ canRead: true, canWrite: false, readOnlyReason: "blocked" });
    expect(await P.getChannelAccess(env.DB, ch, b.user.id)).toMatchObject({ canWrite: false, readOnlyReason: "not_eligible" });
    await expect(P.requireChannelWrite(env.DB, ch, b.user.id)).rejects.toMatchObject({ status: 403, code: "read_only" });
    expect(P.canModerateChannel((await P.getChannelAccess(env.DB, ch, a.user.id))!)).toBe(false);
  });

  it("group dm: members write regardless of pair relations; outsiders get null", async () => {
    const [a, b, c, d] = await Promise.all([signup(), signup(), signup(), signup()]);
    const ch = await groupDm(a, [b, c]);
    await block(b, a);
    expect(await P.getChannelAccess(env.DB, ch, b.user.id)).toMatchObject({ canRead: true, canWrite: true, readOnlyReason: null, otherUserId: null });
    expect(await P.getChannelAccess(env.DB, ch, d.user.id)).toBeNull();
  });

  it("requireSpaceRole: 404 non-member, 403 below min", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    const s = await space(a, [b]);
    await expect(P.requireSpaceRole(env.DB, s, c.user.id, "member")).rejects.toMatchObject({ status: 404 });
    await expect(P.requireSpaceRole(env.DB, s, b.user.id, "admin")).rejects.toMatchObject({ status: 403, code: "forbidden" });
    expect(await P.requireSpaceRole(env.DB, s, a.user.id, "admin")).toBe("owner");
    expect(await P.requireSpaceRole(env.DB, s, b.user.id, "member")).toBe("member");
  });

  it("accessibleChannelIds: public text of live spaces, own private/dm/group_dm", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const s1 = await space(a, [b]);
    const s2 = await space(b);
    const pub = await textChannel(s1, a);
    const privMine = await privateText(s1, [a, b]);
    const privOther = await privateText(s1, [a]);
    const onlyB = await textChannel(s2, b); // a is not in s2
    const d = await dm(a, b);
    const g = await groupDm(a, [b, (await signup())]);
    expect((await P.accessibleChannelIds(env.DB, b.user.id)).sort()).toEqual([pub, privMine, onlyB, d, g].sort());
    expect((await P.accessibleChannelIds(env.DB, b.user.id, { spaceId: s1 })).sort()).toEqual([pub, privMine].sort());
    expect((await P.accessibleChannelIds(env.DB, b.user.id, { spaceId: s2 })).sort()).toEqual([onlyB]);
    expect(await P.accessibleChannelIds(env.DB, a.user.id, { spaceId: s2 })).toEqual([]);
    expect(await P.accessibleChannelIds(env.DB, a.user.id, { spaceId: s1 })).toContain(privOther); // private: a only
    await env.DB.prepare("UPDATE spaces SET deleted_at=? WHERE id=?").bind(Date.now(), s1).run();
    expect((await P.accessibleChannelIds(env.DB, b.user.id)).sort()).toEqual([onlyB, d, g].sort());
  });

  it("post visibility: private author needs active follow; block hides", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    await env.DB.prepare("UPDATE users SET is_private=1 WHERE id=?").bind(a.user.id).run();
    const post = await insertPost(a);
    expect(await P.canViewPost(env.DB, a.user.id, post)).toBe(true); // author
    expect(await P.canViewPost(env.DB, b.user.id, post)).toBe(false);
    await env.DB.prepare("INSERT INTO follows VALUES (?1, ?2, 'active', 0, 0)").bind(b.user.id, a.user.id).run();
    expect(await P.canViewPost(env.DB, b.user.id, post)).toBe(true);
    expect(await P.canViewUserContent(env.DB, b.user.id, a.user.id)).toBe("ok");
    await block(a, b);
    expect(await P.canViewPost(env.DB, b.user.id, post)).toBe(false);
    expect(await P.canViewUserContent(env.DB, c.user.id, a.user.id)).toBe("private");
    expect(await P.canViewUserContent(env.DB, b.user.id, a.user.id)).toBe("hidden");
    expect(await P.canViewUserContent(env.DB, a.user.id, b.user.id)).toBe("hidden");
    expect(await P.canViewUserContent(env.DB, a.user.id, a.user.id)).toBe("ok");
  });

  it("postVisibilitySql agrees with canViewPost (deleted, suspended, private, blocked)", async () => {
    const [pub, priv, susp, blk, viewer] = await Promise.all([signup(), signup(), signup(), signup(), signup()]);
    await env.DB.prepare("UPDATE users SET is_private=1 WHERE id=?").bind(priv.user.id).run();
    await env.DB.prepare("UPDATE users SET suspended_at=? WHERE id=?").bind(Date.now(), susp.user.id).run();
    await block(viewer, blk);
    const posts = await Promise.all([insertPost(pub), insertPost(pub), insertPost(priv), insertPost(susp), insertPost(blk)]);
    await env.DB.prepare("UPDATE posts SET deleted_at=? WHERE id=?").bind(Date.now(), posts[1].id).run();
    const ids = posts.map((p) => p.id);
    expect(await visibleViaSql(viewer.user.id, ids)).toEqual([posts[0].id]);
    for (const p of posts) {
      const fresh = (await env.DB.prepare("SELECT * FROM posts WHERE id=?").bind(p.id).first<PostRow>())!;
      expect(await P.canViewPost(env.DB, viewer.user.id, fresh)).toBe(fresh.id === posts[0].id);
    }
    expect(await visibleViaSql(priv.user.id, ids)).toEqual([posts[0].id, posts[2].id, posts[4].id].sort()); // own private post; no block with blk
    expect(() => P.postVisibilitySql(viewer.user.id, "p; DROP TABLE posts")).toThrow();
  });

  it("presenceVisible: friends, shared space, shared dm only", async () => {
    const [a, b, c, d] = await Promise.all([signup(), signup(), signup(), signup()]);
    await friend(a, b);
    await space(a, [c]);
    expect([...(await P.presenceVisible(env.DB, a.user.id, [a.user.id, b.user.id, c.user.id, d.user.id]))].sort()).toEqual([a.user.id, b.user.id, c.user.id].sort());
    await dm(a, d);
    expect((await P.presenceVisible(env.DB, a.user.id, [d.user.id])).has(d.user.id)).toBe(true);
    expect((await P.presenceVisible(env.DB, a.user.id, [])).size).toBe(0);
  });

  it("presenceVisible: group dm counts, deleted space does not", async () => {
    const [a, b, c, e] = await Promise.all([signup(), signup(), signup(), signup()]);
    await groupDm(a, [b, c]);
    const s = await space(a, [e]);
    await env.DB.prepare("UPDATE spaces SET deleted_at=? WHERE id=?").bind(Date.now(), s).run();
    expect([...(await P.presenceVisible(env.DB, b.user.id, [a.user.id, c.user.id, e.user.id]))].sort()).toEqual([a.user.id, c.user.id].sort());
    expect((await P.presenceVisible(env.DB, a.user.id, [e.user.id])).size).toBe(0);
  });
});

describe("dm api", () => {
  it("unfriend turns 1:1 dm read-only (send -> 403 read_only)", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await makeFriends(a, b);
    const d = (await api(a.token).post<any>("/v1/dms", { users: [`@${b.handle}`] })).body.channel;
    await api(a.token).del(`/v1/friends/@${b.handle}`);
    expect((await api(a.token).get<any>(`/v1/channels/${d.id}`)).body.channel).toMatchObject({ read_only: true, read_only_reason: "not_eligible" });
  });
});
