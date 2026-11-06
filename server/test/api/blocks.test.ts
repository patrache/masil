import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { newId } from "../../src/lib/ids";
import { api, connectWs, makeFriends, signup } from "../helpers";

// ---- local fixtures (this file only) ----

type U = { user: { id: string } };

const settle = (ms = 150) => new Promise<void>((r) => setTimeout(r, ms));

/** A follows row as the follow service writes it; an active one also bumps follower_count/following_count. */
async function followSql(follower: U, followee: U, status: "active" | "pending"): Promise<void> {
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

/** A space owned by `owner` with a pending direct invite from `inviter` to `invitee`; returns the invite id. */
async function directInviteSql(owner: U, inviter: U, invitee: U): Promise<string> {
  const t = Date.now();
  const spaceId = newId();
  const inviteId = newId();
  await env.DB.batch([
    env.DB.prepare("INSERT INTO spaces (id, name, owner_id, member_count, created_at, updated_at) VALUES (?1, 'blk', ?2, 1, ?3, ?3)").bind(spaceId, owner.user.id, t),
    env.DB.prepare("INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?1, ?2, 'owner', ?3)").bind(spaceId, owner.user.id, t),
    env.DB.prepare("INSERT INTO space_direct_invites (id, space_id, inviter_id, invitee_id, status, created_at) VALUES (?1, ?2, ?3, ?4, 'pending', ?5)").bind(
      inviteId,
      spaceId,
      inviter.user.id,
      invitee.user.id,
      t,
    ),
  ]);
  return inviteId;
}

describe("blocks", () => {
  it("block cleans relations in one go and only the blocker hears about it", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await makeFriends(a, b);
    await followSql(a, b, "active");
    await followSql(b, a, "pending");
    const [ca, cb] = await Promise.all([connectWs(a.token), connectWs(b.token)]);
    expect((await api(a.token).put(`/v1/blocks/@${b.handle}`)).status).toBe(204);
    expect((await ca.next("block.updated")).data).toEqual({ user_id: b.user.id, blocked: true });
    await ca.next("friend.removed");
    await settle();
    expect(cb.frames.some((f) => ["friend.removed", "block.updated"].includes(f.type))).toBe(false);
    const q = (sql: string) => env.DB.prepare(sql).bind(a.user.id, b.user.id).first<any>();
    expect((await q("SELECT COUNT(*) n FROM friendships WHERE user_a IN (?1,?2) AND user_b IN (?1,?2)")).n).toBe(0);
    expect((await q("SELECT COUNT(*) n FROM follows WHERE follower_id IN (?1,?2) AND followee_id IN (?1,?2)")).n).toBe(0);
    expect((await env.DB.prepare("SELECT follower_count FROM users WHERE id=?").bind(b.user.id).first<any>()).follower_count).toBe(0);
    expect((await api(a.token).get<any>("/v1/blocks")).body.items[0].user.id).toBe(b.user.id);
    expect((await api(a.token).del(`/v1/blocks/@${b.handle}`)).status).toBe(204);
    expect((await api(a.token).get<any>(`/v1/users/@${b.handle}`)).body.user.relationship.is_friend).toBe(false); // not restored
  });

  it("pending friend requests both ways become canceled", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const r = await api(b.token).post<any>("/v1/friends/requests", { user: `@${a.handle}` });
    await api(a.token).put(`/v1/blocks/@${b.handle}`);
    expect((await env.DB.prepare("SELECT status FROM friend_requests WHERE id=?").bind(r.body.request.id).first<any>()).status).toBe("canceled");
  });

  it("counters: only active follows are decremented; pending ones leave counters alone", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await followSql(a, b, "active");
    await followSql(b, a, "pending");
    await api(a.token).put(`/v1/blocks/@${b.handle}`);
    const counts = async (u: U) => env.DB.prepare("SELECT follower_count, following_count FROM users WHERE id=?").bind(u.user.id).first<any>();
    expect(await counts(a)).toEqual({ follower_count: 0, following_count: 0 });
    expect(await counts(b)).toEqual({ follower_count: 0, following_count: 0 });
  });

  it("pending direct invites both ways become canceled; others untouched", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    const ab = await directInviteSql(a, a, b);
    const ba = await directInviteSql(b, b, a);
    const ac = await directInviteSql(a, a, c);
    expect((await api(a.token).put(`/v1/blocks/@${b.handle}`)).status).toBe(204);
    const status = async (id: string) => (await env.DB.prepare("SELECT status FROM space_direct_invites WHERE id=?").bind(id).first<any>()).status;
    expect(await status(ab)).toBe("canceled");
    expect(await status(ba)).toBe("canceled");
    expect(await status(ac)).toBe("pending");
  });

  it("self -> 409 conflict self; PUT is idempotent; list newest first; unblock event", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    expect((await api(a.token).put<any>("/v1/blocks/@me")).body.error).toMatchObject({ code: "conflict", details: { reason: "self" } });
    expect((await api(a.token).put(`/v1/blocks/@${b.handle}`)).status).toBe(204);
    await settle(5);
    expect((await api(a.token).put(`/v1/blocks/@${c.handle}`)).status).toBe(204);
    expect((await api(a.token).put(`/v1/blocks/@${c.handle}`)).status).toBe(204);
    const list = (await api(a.token).get<any>("/v1/blocks")).body.items;
    expect(list.map((x: any) => x.user.id)).toEqual([c.user.id, b.user.id]);
    expect(typeof list[0].created_at).toBe("string");
    const ca = await connectWs(a.token);
    expect((await api(a.token).del(`/v1/blocks/@${c.handle}`)).status).toBe(204);
    expect((await ca.next("block.updated")).data).toEqual({ user_id: c.user.id, blocked: false });
    expect((await api(a.token).get<any>("/v1/blocks")).body.items.map((x: any) => x.user.id)).toEqual([b.user.id]);
    expect((await api(b.token).get<any>("/v1/blocks")).body.items).toEqual([]);
  });
});
