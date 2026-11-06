// Owner decision D1 (api.md 3.9 / 5.2): while an account is suspended, anyone but its owner sees only its handle,
// display name and suspended=true. The profile hides bio (""), status, avatar, counts (0) and presence; every other
// UserSummary of it (lists, lookups) hides status and avatar too, and its avatar media is 404 — except for operators
// (/v1/admin/*, and the media an operator opens). All of it comes back once suspended_until has passed.
import { env } from "cloudflare:test";
import { describe, expect, it } from "vitest";
import { serializeProfile } from "../../src/lib/serialize";
import type { UserRow } from "../../src/types/rows";
import { api, createPost, follow, makeAdmin, makeFriends, signup, uploadPng, type TestUser } from "../helpers";

async function suspend(u: TestUser, until: number | null): Promise<void> {
  await env.DB.prepare("UPDATE users SET suspended_at = ?1, suspended_until = ?2 WHERE id = ?3").bind(Date.now(), until, u.user.id).run();
}

/** o with a bio, a status, an avatar, one follower (f, also a friend so presence is visible to f) and one post. */
async function dressedUp(): Promise<{ o: TestUser; f: TestUser; avatarId: string }> {
  const [o, f] = await Promise.all([signup(), signup()]);
  const up = await uploadPng(o, "avatar");
  expect((await api(o.token).patch("/v1/me/profile", { bio: "소개 글", avatar_upload_id: up.id })).status).toBe(200);
  expect((await api(o.token).put("/v1/me/status", { emoji: "🎧", text: "듣는 중" })).status).toBe(200);
  await follow(f, o);
  await makeFriends(o, f);
  await createPost(o, "글");
  return { o, f, avatarId: up.id };
}

async function mediaStatus(t: TestUser, id: string): Promise<number> {
  const r = await api(t.token).raw(`/v1/media/${id}`, { method: "GET" });
  await r.body?.cancel();
  return r.status;
}

describe("suspended account profile (D1)", () => {
  it("a viewer sees only handle, display name and suspended=true; everything is back after suspended_until", async () => {
    const { o, f } = await dressedUp();
    const F = api(f.token);
    const before = (await F.get<any>(`/v1/users/@${o.handle}`)).body.user;
    expect(before).toEqual(
      expect.objectContaining({
        bio: "소개 글",
        avatar_url: expect.stringMatching(/^\/v1\/media\//),
        status: expect.objectContaining({ text: "듣는 중" }),
        follower_count: 1,
        post_count: 1,
        suspended: false,
      }),
    );
    expect(before.presence).not.toBeNull();
    expect(before.last_seen_at === null || typeof before.last_seen_at === "string").toBe(true);

    await suspend(o, null);
    const r = await F.get<any>(`/v1/users/@${o.handle}`);
    expect(r.status).toBe(200);
    const during = r.body.user;
    expect(during).toEqual(
      expect.objectContaining({
        id: o.user.id,
        handle: o.handle,
        display_name: before.display_name,
        suspended: true,
        bio: "",
        avatar_url: null,
        status: null,
        follower_count: 0,
        following_count: 0,
        post_count: 0,
        presence: null,
        last_seen_at: null,
      }),
    );
    expect(JSON.stringify(during)).not.toContain("소개 글");
    expect(JSON.stringify(during)).not.toContain("듣는 중");
    expect(during.relationship.can_view_posts).toBe(false);
    expect(during.relationship.can_dm).toBe(false);

    await suspend(o, Date.now() + 60_000); // timed, still running
    expect((await F.get<any>(`/v1/users/@${o.handle}`)).body.user.bio).toBe("");

    await suspend(o, Date.now() - 1000); // expired: active again
    const after = (await F.get<any>(`/v1/users/@${o.handle}`)).body.user;
    expect(after).toEqual(expect.objectContaining({ bio: "소개 글", avatar_url: before.avatar_url, follower_count: 1, post_count: 1, suspended: false }));
    expect(after.status).toEqual(expect.objectContaining({ text: "듣는 중" }));
  });

  it("the owner's own profile is not masked (serializeProfile with viewer = owner)", async () => {
    const { o } = await dressedUp();
    await suspend(o, null);
    const row = (await env.DB.prepare("SELECT * FROM users WHERE id = ?").bind(o.user.id).first<UserRow>())!;
    const self = await serializeProfile(env.DB, o.user.id, row);
    expect(self).toEqual(
      expect.objectContaining({ bio: "소개 글", avatar_url: expect.stringMatching(/^\/v1\/media\//), follower_count: 1, post_count: 1, suspended: true }),
    );
    expect(self.status).toEqual(expect.objectContaining({ text: "듣는 중" }));
  });

  it("sibling UserSummary paths hide status and avatar too: ids lookup, someone else's follower list", async () => {
    const { o, f } = await dressedUp();
    const v = await signup();
    await follow(o, v); // o shows up in v's follower list
    const V = api(v.token);
    const lookup = async (): Promise<any> => (await V.get<any>(`/v1/users?ids=${o.user.id}`)).body.items[0];
    const inFollowers = async (): Promise<any> =>
      (await V.get<any>(`/v1/users/@${v.handle}/followers`)).body.items.find((u: any) => u.id === o.user.id);
    expect((await lookup()).avatar_url).not.toBeNull();
    expect((await inFollowers()).status).not.toBeNull();

    await suspend(o, null);
    for (const u of [await lookup(), await inFollowers()]) {
      expect(u).toEqual(expect.objectContaining({ handle: o.handle, suspended: true, avatar_url: null, status: null }));
    }
    // f (friend) sees the same in the friend list
    const friend = (await api(f.token).get<any>("/v1/friends")).body.items.find((x: any) => x.user.id === o.user.id);
    expect(friend.user).toEqual(expect.objectContaining({ suspended: true, avatar_url: null, status: null }));

    await suspend(o, Date.now() - 1000);
    expect((await lookup()).avatar_url).not.toBeNull();
  });

  it("the avatar media of a suspended account is 404 for others, still served to an operator; back after expiry", async () => {
    const { o, f, avatarId } = await dressedUp();
    const admin = await signup();
    await makeAdmin(admin);
    expect(await mediaStatus(f, avatarId)).toBe(200);
    await suspend(o, null);
    expect(await mediaStatus(f, avatarId)).toBe(404);
    expect(await mediaStatus(admin, avatarId)).toBe(200);
    await suspend(o, Date.now() - 1000);
    expect(await mediaStatus(f, avatarId)).toBe(200);
  });

  it("operator views keep the suspended account's avatar and status (/v1/admin/users)", async () => {
    const { o } = await dressedUp();
    const admin = await signup();
    await makeAdmin(admin);
    await suspend(o, null);
    const items = (await api(admin.token).get<any>(`/v1/admin/users?q=@${o.handle}`)).body.items;
    const row = items.find((u: any) => u.id === o.user.id);
    expect(row).toEqual(
      expect.objectContaining({ suspended: true, avatar_url: expect.stringMatching(/^\/v1\/media\//), status: expect.objectContaining({ text: "듣는 중" }) }),
    );
  });
});
