import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { resolveOnlineAudience } from "../../src/lib/realtime";
import { newId } from "../../src/lib/ids";
import { signup, uniqueHandle } from "../helpers";

// ---- local fixtures (this file only): seed relations / presence with raw SQL ----

/** Anything with a user id (TestUser, or a user row seeded straight into D1). */
type U = { user: { id: string } };

/** Sets user_presence liveness: full -> online_until = now+60s, notify -> notify_online_until = now+60s. */
async function online(u: U, opts: { full?: boolean; notify?: boolean }): Promise<void> {
  const t = Date.now();
  const full = opts.full ? t + 60_000 : 0;
  const notify = opts.notify ? t + 60_000 : 0;
  await env.DB.prepare(
    `INSERT INTO user_presence (user_id, online_until, notify_online_until, updated_at) VALUES (?1, ?2, ?3, ?4)
     ON CONFLICT (user_id) DO UPDATE SET online_until = excluded.online_until, notify_online_until = excluded.notify_online_until, updated_at = excluded.updated_at`,
  ).bind(u.user.id, full, notify, t).run();
}

/** Space with owner + members (role member); returns spaceId. */
async function space(owner: U, members: U[] = []): Promise<string> {
  const id = newId();
  const t = Date.now();
  await env.DB.batch([
    env.DB.prepare("INSERT INTO spaces (id, name, owner_id, member_count, created_at, updated_at) VALUES (?1, 'test', ?2, ?3, ?4, ?4)").bind(id, owner.user.id, members.length + 1, t),
    env.DB.prepare("INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?1, ?2, 'owner', ?3)").bind(id, owner.user.id, t),
    ...members.map((m) => env.DB.prepare("INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?1, ?2, 'member', ?3)").bind(id, m.user.id, t)),
  ]);
  return id;
}

async function textChannel(spaceId: string, opts: { private?: boolean; members?: U[] } = {}): Promise<string> {
  const id = newId();
  const t = Date.now();
  const members = opts.members ?? [];
  const owner = await env.DB.prepare("SELECT owner_id FROM spaces WHERE id = ?1").bind(spaceId).first<{ owner_id: string }>();
  await env.DB.batch([
    env.DB.prepare("INSERT INTO channels (id, kind, space_id, name, is_private, created_by, member_count, created_at, updated_at) VALUES (?1, 'text', ?2, ?3, ?4, ?5, ?6, ?7, ?7)")
      .bind(id, spaceId, "c" + id.slice(-10).toLowerCase(), opts.private ? 1 : 0, owner!.owner_id, members.length, t),
    ...members.map((m) => env.DB.prepare("INSERT INTO channel_members (channel_id, user_id, joined_at) VALUES (?1, ?2, ?3)").bind(id, m.user.id, t)),
  ]);
  return id;
}

const publicText = (spaceId: string) => textChannel(spaceId);
const privateText = (spaceId: string, members: U[]) => textChannel(spaceId, { private: true, members });

async function friend(a: U, b: U): Promise<void> {
  const [lo, hi] = a.user.id < b.user.id ? [a.user.id, b.user.id] : [b.user.id, a.user.id];
  await env.DB.prepare("INSERT INTO friendships (user_a, user_b, created_at) VALUES (?1, ?2, ?3)").bind(lo, hi, Date.now()).run();
}

async function block(blocker: U, blocked: U): Promise<void> {
  await env.DB.prepare("INSERT INTO blocks (blocker_id, blocked_id, created_at) VALUES (?1, ?2, ?3)").bind(blocker.user.id, blocked.user.id, Date.now()).run();
}

/** follower -> followee as an active follow (with the counters the follow service maintains). */
async function followActive(follower: U, followee: U): Promise<void> {
  const t = Date.now();
  await env.DB.batch([
    env.DB.prepare("INSERT INTO follows (follower_id, followee_id, status, created_at, accepted_at) VALUES (?1, ?2, 'active', ?3, ?3)").bind(follower.user.id, followee.user.id, t),
    env.DB.prepare("UPDATE users SET follower_count = follower_count + 1 WHERE id = ?1").bind(followee.user.id),
    env.DB.prepare("UPDATE users SET following_count = following_count + 1 WHERE id = ?1").bind(follower.user.id),
  ]);
}

async function groupDm(owner: U, members: U[]): Promise<string> {
  const id = newId();
  const t = Date.now();
  const all = [owner, ...members];
  await env.DB.batch([
    env.DB.prepare("INSERT INTO channels (id, kind, owner_id, created_by, member_count, created_at, updated_at) VALUES (?1, 'group_dm', ?2, ?2, ?3, ?4, ?4)").bind(id, owner.user.id, all.length, t),
    ...all.map((m) => env.DB.prepare("INSERT INTO channel_members (channel_id, user_id, joined_at) VALUES (?1, ?2, ?3)").bind(id, m.user.id, t)),
  ]);
  return id;
}

/** n users inserted straight into D1 (signup's password hashing is the bottleneck for big audiences). */
async function seedUsers(n: number): Promise<U[]> {
  const t = Date.now();
  const users = Array.from({ length: n }, () => ({ user: { id: newId() }, handle: uniqueHandle("bulk") }));
  await env.DB.batch(
    users.flatMap((u) => [
      env.DB.prepare("INSERT INTO users (id, handle, display_name, password_hash, created_at, updated_at) VALUES (?1, ?2, ?2, 'x', ?3, ?3)").bind(u.user.id, u.handle, t),
      env.DB.prepare("INSERT INTO user_presence (user_id, updated_at) VALUES (?1, ?2)").bind(u.user.id, t),
    ]),
  );
  return users;
}

const sorted = (ids: string[]) => [...ids].sort();

describe("audience", () => {
  it("channel audience: only full-online space members; notifyOk adds daemon-only", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    const s = await space(a, [b, c]);
    const ch = await publicText(s);
    await online(a, { full: true });
    await online(b, { notify: true });
    expect(await resolveOnlineAudience(env, { channel: ch })).toEqual([a.user.id]);
    expect(sorted(await resolveOnlineAudience(env, { channel: ch }, { notifyOk: true }))).toEqual(sorted([a.user.id, b.user.id]));
    expect(await resolveOnlineAudience(env, { channel: ch }, { exclude: [a.user.id] })).toEqual([]);
    // users / space audiences use the same online rule
    expect(sorted(await resolveOnlineAudience(env, { users: [a.user.id, b.user.id, c.user.id, a.user.id] }, { notifyOk: true }))).toEqual(sorted([a.user.id, b.user.id]));
    expect(await resolveOnlineAudience(env, { space: s })).toEqual([a.user.id]);
    // expired liveness is offline
    await env.DB.prepare("UPDATE user_presence SET online_until = ?1 WHERE user_id = ?2").bind(Date.now() - 1, a.user.id).run();
    expect(await resolveOnlineAudience(env, { channel: ch })).toEqual([]);
  });

  it("left member never in private channel audience; deleted channel / deleted space -> empty", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const s = await space(a, [b]);
    const ch = await privateText(s, [a, b]);
    const pub = await publicText(s);
    await online(a, { full: true });
    await online(b, { full: true });
    expect(sorted(await resolveOnlineAudience(env, { channel: ch }))).toEqual(sorted([a.user.id, b.user.id]));
    await env.DB.prepare("DELETE FROM space_members WHERE space_id=? AND user_id=?").bind(s, b.user.id).run();
    expect(await resolveOnlineAudience(env, { channel: ch })).toEqual([a.user.id]);
    await env.DB.prepare("UPDATE channels SET deleted_at=1 WHERE id=?").bind(pub).run();
    expect(await resolveOnlineAudience(env, { channel: pub })).toEqual([]);
    await env.DB.prepare("UPDATE spaces SET deleted_at=1 WHERE id=?").bind(s).run();
    expect(await resolveOnlineAudience(env, { channel: ch })).toEqual([]);
    expect(await resolveOnlineAudience(env, { space: s })).toEqual([]);
  });

  it("followers + visibleTo: blocked or non-follower of private author filtered", async () => {
    const [author, f1, f2, other] = await Promise.all([signup(), signup(), signup(), signup()]);
    for (const f of [f1, f2]) await followActive(f, author);
    await env.DB.prepare("UPDATE users SET is_private=1 WHERE id=?").bind(other.user.id).run();
    for (const u of [f1, f2]) await online(u, { full: true });
    expect(sorted(await resolveOnlineAudience(env, { followers: author.user.id }))).toEqual(sorted([f1.user.id, f2.user.id]));
    await block(other, f2);
    const r = await resolveOnlineAudience(env, { followers: author.user.id }, { visibleTo: { authors: [author.user.id, other.user.id], privateAuthors: [other.user.id] } });
    expect(r).toEqual([]); // f1 doesn't follow private 'other'; f2 is blocked by 'other'
    // a block between follower and the followed user removes the follower from the audience itself
    await block(author, f1);
    expect(await resolveOnlineAudience(env, { followers: author.user.id })).toEqual([f2.user.id]);
  });

  it("presenceWatchers = friends + dm co-members", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    await friend(a, b);
    await groupDm(a, [c]);
    for (const u of [a, b, c]) await online(u, { full: true });
    expect(sorted(await resolveOnlineAudience(env, { presenceWatchers: a.user.id }))).toEqual(sorted([b.user.id, c.user.id]));
  });

  it("Review Focus 2: 120 online space members resolve without hitting the bind limit", async () => {
    const owner = await signup();
    const members = await seedUsers(119);
    const s = await space(owner, members);
    const ch = await publicText(s);
    for (const u of [owner, ...members]) await online(u, { full: true });
    expect(await resolveOnlineAudience(env, { channel: ch })).toHaveLength(120);
    expect(await resolveOnlineAudience(env, { space: s })).toHaveLength(120);
    const all = [owner, ...members].map((u) => u.user.id);
    expect(await resolveOnlineAudience(env, { users: all }, { exclude: all.slice(0, 110) })).toHaveLength(10);
  });
});
