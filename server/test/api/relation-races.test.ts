// Pending item 10 (TOCTOU): a block or suspension check that is only a plain read before the write lets a block (or
// suspension) committing in between leave behind exactly what the block cleanup removes (api.md 3.2: friendship,
// pending friend requests both ways, follows both ways, pending direct invites both ways). The write statements
// themselves must re-check, so these tests commit the block/suspension between the pre-check and the write (or run
// the write statement directly with the block row in place) and assert nothing is written.
import { env, createExecutionContext, waitOnExecutionContext } from "cloudflare:test";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp } from "../../src/app";
import type { Runtime } from "../../src/env";
import { newId } from "../../src/lib/ids";
import { insertDirectInvite } from "../../src/services/membership";
import { applyBlock, follow, insertFriendRequest, makeFriends, removeBlock, unfriend } from "../../src/services/relations";
import type { FriendRequestRow } from "../../src/types/rows";
import { api, makeFriends as befriend, makeSpace, signup, uniqueIp, type TestUser } from "../helpers";

// ---- local fixtures (this file only) ----

const plainRt: Runtime = { env, waitUntil: () => {} };

/** A Runtime whose D1 handle runs `before(realDb)` once, right before the first batch (a concurrent commit). */
function commitsBeforeFirstBatch(before: (db: D1Database) => Promise<unknown>) {
  let fired = false;
  const db = new Proxy(env.DB, {
    get(target, key) {
      if (key === "batch") {
        return async (stmts: D1PreparedStatement[]) => {
          if (!fired) {
            fired = true;
            await before(target);
          }
          return target.batch(stmts);
        };
      }
      const v = Reflect.get(target, key);
      return typeof v === "function" ? v.bind(target) : v;
    },
  });
  const rt: Runtime = { env: { ...env, DB: db }, waitUntil: () => {} };
  return { rt, fired: () => fired };
}

/**
 * One request through a fresh app whose env.DB is `db` (e.g. commitsBeforeFirstBatch's): the route's pre-checks are
 * plain reads, so the hook lands between them and the route's write batch. Background work is awaited.
 */
async function fetchWith(db: D1Database, token: string, method: string, path: string, json?: unknown): Promise<{ status: number; body: any }> {
  const ctx = createExecutionContext();
  const res = await createApp().fetch(
    new Request(`http://masil.test${path}`, {
      method,
      headers: {
        "User-Agent": "masil/0.1.0 (test; test)",
        "CF-Connecting-IP": uniqueIp(),
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: json === undefined ? undefined : JSON.stringify(json),
    }),
    { ...env, DB: db } as typeof env,
    ctx,
  );
  await waitOnExecutionContext(ctx);
  const text = await res.text();
  return { status: res.status, body: text === "" ? null : JSON.parse(text) };
}

function insertBlock(db: D1Database, blocker: TestUser, blocked: TestUser): Promise<unknown> {
  return db.prepare("INSERT INTO blocks (blocker_id, blocked_id, created_at) VALUES (?1, ?2, ?3)").bind(blocker.user.id, blocked.user.id, Date.now()).run();
}

/** Suspends `u` straight in D1 (until = null: indefinitely). */
function suspend(db: D1Database, u: TestUser, until: number | null): Promise<unknown> {
  const t = Date.now();
  return db.prepare("UPDATE users SET suspended_at = ?2, suspended_until = ?3, suspend_reason = 'x' WHERE id = ?1").bind(u.user.id, t - 1000, until).run();
}

async function counts(u: TestUser): Promise<{ follower_count: number; following_count: number }> {
  return (await env.DB.prepare("SELECT follower_count, following_count FROM users WHERE id = ?1").bind(u.user.id).first<{
    follower_count: number;
    following_count: number;
  }>())!;
}

async function followRow(a: TestUser, b: TestUser): Promise<{ status: string } | null> {
  return env.DB.prepare("SELECT status FROM follows WHERE follower_id = ?1 AND followee_id = ?2").bind(a.user.id, b.user.id).first<{ status: string }>();
}

async function friendshipExists(a: TestUser, b: TestUser): Promise<boolean> {
  const [lo, hi] = a.user.id < b.user.id ? [a.user.id, b.user.id] : [b.user.id, a.user.id];
  return (await env.DB.prepare("SELECT 1 AS x FROM friendships WHERE user_a = ?1 AND user_b = ?2").bind(lo, hi).first()) !== null;
}

const forbidden = { status: 403, code: "action_not_allowed" };

// ---- follow ----

describe("follow: the block / suspension check is part of the write", () => {
  for (const dir of ["followee blocks follower", "follower blocks followee"] as const) {
    it(`a block committed after the pre-check (${dir}) -> 403 action_not_allowed, no row, counters unchanged`, async () => {
      const [a, b] = await Promise.all([signup(), signup()]);
      const { rt, fired } = commitsBeforeFirstBatch((db) => (dir === "followee blocks follower" ? insertBlock(db, b, a) : insertBlock(db, a, b)));
      await expect(follow(rt, a.user.id, b.user.id)).rejects.toMatchObject(forbidden);
      expect(fired()).toBe(true);
      expect(await followRow(a, b)).toBeNull();
      expect(await counts(a)).toEqual({ follower_count: 0, following_count: 0 });
      expect(await counts(b)).toEqual({ follower_count: 0, following_count: 0 });
    });
  }

  it("a private followee: a block committed after the pre-check -> 403, no pending row", async () => {
    const [p, a] = await Promise.all([signup(), signup()]);
    await api(p.token).patch("/v1/me/settings", { is_private: true });
    const { rt, fired } = commitsBeforeFirstBatch((db) => insertBlock(db, p, a));
    await expect(follow(rt, a.user.id, p.user.id)).rejects.toMatchObject(forbidden);
    expect(fired()).toBe(true);
    expect(await followRow(a, p)).toBeNull();
    expect((await api(p.token).get<any>("/v1/me/follow-requests")).body.items).toEqual([]);
  });

  it("the followee suspended after the pre-check -> 403, no row, counters unchanged", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const { rt, fired } = commitsBeforeFirstBatch((db) => suspend(db, b, null));
    await expect(follow(rt, a.user.id, b.user.id)).rejects.toMatchObject(forbidden);
    expect(fired()).toBe(true);
    expect(await followRow(a, b)).toBeNull();
    expect(await counts(a)).toEqual({ follower_count: 0, following_count: 0 });
    expect(await counts(b)).toEqual({ follower_count: 0, following_count: 0 });
  });

  it("a timed suspension still running (until in the future) also refuses; one already over (until <= now) does not", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    const running = commitsBeforeFirstBatch((db) => suspend(db, b, Date.now() + 3_600_000));
    await expect(follow(running.rt, a.user.id, b.user.id)).rejects.toMatchObject(forbidden);
    expect(await followRow(a, b)).toBeNull();
    // same boundary as postVisibilitySql: suspended_until <= now counts as active again
    await suspend(env.DB, c, Date.now() - 1);
    expect(await follow(plainRt, a.user.id, c.user.id)).toBe("active");
    expect(await counts(c)).toMatchObject({ follower_count: 1 });
  });

  it("idempotent: a concurrent follow of the same pair committed first -> its state, counted once", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const { rt, fired } = commitsBeforeFirstBatch((db) =>
      db.batch([
        db.prepare("INSERT INTO follows (follower_id, followee_id, status, created_at, accepted_at) VALUES (?1, ?2, 'active', ?3, ?3)").bind(a.user.id, b.user.id, Date.now()),
        db.prepare("UPDATE users SET following_count = following_count + 1 WHERE id = ?1").bind(a.user.id),
        db.prepare("UPDATE users SET follower_count = follower_count + 1 WHERE id = ?1").bind(b.user.id),
      ]),
    );
    expect(await follow(rt, a.user.id, b.user.id)).toBe("active");
    expect(fired()).toBe(true);
    expect(await counts(a)).toMatchObject({ following_count: 1 });
    expect(await counts(b)).toMatchObject({ follower_count: 1 });
    // and a plain refollow afterwards is still a no-op
    expect(await follow(plainRt, a.user.id, b.user.id)).toBe("active");
    expect(await counts(b)).toMatchObject({ follower_count: 1 });
  });

  it("no block, no suspension: active (public) / pending (private), counters only for active", async () => {
    const [a, b, p] = await Promise.all([signup(), signup(), signup()]);
    await api(p.token).patch("/v1/me/settings", { is_private: true });
    expect(await follow(plainRt, a.user.id, b.user.id)).toBe("active");
    expect(await follow(plainRt, a.user.id, p.user.id)).toBe("pending");
    expect(await counts(a)).toEqual({ follower_count: 0, following_count: 1 });
    expect(await counts(b)).toEqual({ follower_count: 1, following_count: 0 });
    expect(await counts(p)).toEqual({ follower_count: 0, following_count: 0 });
  });

  it("a block committed just before the follow's batch, via the real block path (cleanup first) -> still nothing left", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const { rt, fired } = commitsBeforeFirstBatch(() => applyBlock(plainRt, b.user.id, a.user.id));
    await expect(follow(rt, a.user.id, b.user.id)).rejects.toMatchObject(forbidden);
    expect(fired()).toBe(true);
    expect(await followRow(a, b)).toBeNull();
    expect(await counts(b)).toEqual({ follower_count: 0, following_count: 0 });
  });
});

// ---- friend requests ----

function requestRow(from: TestUser, to: TestUser): FriendRequestRow {
  const t = Date.now();
  return { id: newId(t), from_user_id: from.user.id, to_user_id: to.user.id, status: "pending", created_at: t, responded_at: null };
}

async function pendingRequests(a: TestUser, b: TestUser): Promise<number> {
  const r = await env.DB.prepare(
    `SELECT COUNT(*) AS n FROM friend_requests
      WHERE status = 'pending' AND ((from_user_id = ?1 AND to_user_id = ?2) OR (from_user_id = ?2 AND to_user_id = ?1))`,
  )
    .bind(a.user.id, b.user.id)
    .first<{ n: number }>();
  return r!.n;
}

describe("friend request insert: the block / suspension check is part of the INSERT", () => {
  for (const dir of ["target blocks sender", "sender blocks target"] as const) {
    it(`with a block row in place (${dir}) the INSERT writes nothing -> 403 action_not_allowed`, async () => {
      const [a, b] = await Promise.all([signup(), signup()]);
      await (dir === "target blocks sender" ? insertBlock(env.DB, b, a) : insertBlock(env.DB, a, b));
      await expect(insertFriendRequest(env.DB, requestRow(a, b), 100)).rejects.toMatchObject(forbidden);
      expect(await pendingRequests(a, b)).toBe(0);
    });
  }

  it("the target suspended now -> 403, nothing written; a suspension already over -> inserted", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    await suspend(env.DB, b, null);
    await expect(insertFriendRequest(env.DB, requestRow(a, b), 100)).rejects.toMatchObject(forbidden);
    expect(await pendingRequests(a, b)).toBe(0);
    await suspend(env.DB, c, Date.now() - 1);
    await insertFriendRequest(env.DB, requestRow(a, c), 100);
    expect(await pendingRequests(a, c)).toBe(1);
  });

  it("no block: inserted; a second one -> 409 already_exists pending; the cap -> 422", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    await insertFriendRequest(env.DB, requestRow(a, b), 100);
    expect(await pendingRequests(a, b)).toBe(1);
    await expect(insertFriendRequest(env.DB, requestRow(a, b), 100)).rejects.toMatchObject({ status: 409, code: "already_exists", details: { reason: "pending" } });
    await expect(insertFriendRequest(env.DB, requestRow(a, c), 1)).rejects.toMatchObject({ status: 422, code: "limit_exceeded" });
  });

  it("through the API: a block in place -> 403 and no request (pre-check and INSERT agree)", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await api(b.token).put(`/v1/blocks/@${a.handle}`);
    const r = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
    expect(r.status).toBe(403);
    expect(r.body.error.code).toBe("action_not_allowed");
    expect(await pendingRequests(a, b)).toBe(0);
  });
});

// ---- accepting a friend request ----

describe("makeFriends: no friendship across a block committed after the pre-check", () => {
  async function pendingRequestId(from: TestUser, to: TestUser): Promise<string> {
    const r = await api(from.token).post<any>("/v1/friends/requests", { user: `@${to.handle}` });
    expect(r.status).toBe(201);
    return r.body.request.id;
  }

  it("a block row committed right before the accept batch -> 403 action_not_allowed, no friendship, request untouched", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const id = await pendingRequestId(a, b);
    const { rt, fired } = commitsBeforeFirstBatch((db) => insertBlock(db, a, b));
    await expect(makeFriends(rt, b.user.id, a.user.id, id)).rejects.toMatchObject(forbidden);
    expect(fired()).toBe(true);
    expect(await friendshipExists(a, b)).toBe(false);
    const st = await env.DB.prepare("SELECT status FROM friend_requests WHERE id = ?1").bind(id).first<{ status: string }>();
    expect(st?.status).toBe("pending");
  });

  it("the real block path committed right before the accept batch -> refused, no friendship, request canceled", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const id = await pendingRequestId(a, b);
    const { rt, fired } = commitsBeforeFirstBatch(() => applyBlock(plainRt, a.user.id, b.user.id));
    await expect(makeFriends(rt, b.user.id, a.user.id, id)).rejects.toMatchObject({ status: 403 });
    expect(fired()).toBe(true);
    expect(await friendshipExists(a, b)).toBe(false);
    const st = await env.DB.prepare("SELECT status FROM friend_requests WHERE id = ?1").bind(id).first<{ status: string }>();
    expect(st?.status).toBe("canceled");
  });

  it("no block: accepted and friends", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const id = await pendingRequestId(a, b);
    await makeFriends(plainRt, b.user.id, a.user.id, id);
    expect(await friendshipExists(a, b)).toBe(true);
  });

  it("the sender suspended right before the accept batch -> 403 action_not_allowed, no friendship, request untouched", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const id = await pendingRequestId(a, b);
    const { rt, fired } = commitsBeforeFirstBatch((db) => suspend(db, a, null));
    await expect(makeFriends(rt, b.user.id, a.user.id, id)).rejects.toMatchObject(forbidden);
    expect(fired()).toBe(true);
    expect(await friendshipExists(a, b)).toBe(false);
    expect(await requestStatus(id)).toBe("pending");
  });

  it("a timed suspension still running refuses; one already over (until <= now) does not", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    const id = await pendingRequestId(a, b);
    const running = commitsBeforeFirstBatch((db) => suspend(db, a, Date.now() + 3_600_000));
    await expect(makeFriends(running.rt, b.user.id, a.user.id, id)).rejects.toMatchObject(forbidden);
    expect(await friendshipExists(a, b)).toBe(false);
    const id2 = await pendingRequestId(c, b);
    await suspend(env.DB, c, Date.now() - 1);
    await makeFriends(plainRt, b.user.id, c.user.id, id2);
    expect(await friendshipExists(b, c)).toBe(true);
  });
});

async function requestStatus(id: string): Promise<string | undefined> {
  return (await env.DB.prepare("SELECT status FROM friend_requests WHERE id = ?1").bind(id).first<{ status: string }>())?.status;
}

async function requestCount(a: TestUser, b: TestUser): Promise<number> {
  const r = await env.DB.prepare(
    "SELECT COUNT(*) AS n FROM friend_requests WHERE (from_user_id = ?1 AND to_user_id = ?2) OR (from_user_id = ?2 AND to_user_id = ?1)",
  )
    .bind(a.user.id, b.user.id)
    .first<{ n: number }>();
  return r!.n;
}

describe("POST /v1/friends/requests crossing auto-accept: block / suspension landing after the route's pre-check", () => {
  /** a -> b pending; returns its id. */
  async function aToB(a: TestUser, b: TestUser): Promise<string> {
    const r = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
    expect(r.status).toBe(201);
    return r.body.request.id;
  }

  const races: Array<[string, (db: D1Database, a: TestUser, b: TestUser) => Promise<unknown>, string]> = [
    ["the other side (the request's sender) suspended", (db, a) => suspend(db, a, null), "pending"],
    ["a block row (sender blocks me)", (db, a, b) => insertBlock(db, a, b), "pending"],
    ["a block row (I block the sender)", (db, a, b) => insertBlock(db, b, a), "pending"],
    ["the real block path (cleanup cancels the request)", (_db, a, b) => applyBlock(plainRt, a.user.id, b.user.id), "canceled"],
  ];
  for (const [what, land, status] of races) {
    it(`${what} -> 403 action_not_allowed, no friendship, no second request`, async () => {
      const [a, b] = await Promise.all([signup(), signup()]);
      const id = await aToB(a, b);
      const { rt, fired } = commitsBeforeFirstBatch((db) => land(db, a, b));
      const r = await fetchWith(rt.env.DB, b.token, "POST", "/v1/friends/requests", { user: `@${a.handle}` });
      expect(fired()).toBe(true);
      expect(r).toMatchObject({ status: 403, body: { error: { code: "action_not_allowed" } } });
      expect(await friendshipExists(a, b)).toBe(false);
      expect(await requestStatus(id)).toBe(status);
      expect(await requestCount(a, b)).toBe(1);
    });
  }

  it("nothing lands in between -> 200 auto-accept, friends", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const id = await aToB(a, b);
    const { rt, fired } = commitsBeforeFirstBatch(async () => {});
    const r = await fetchWith(rt.env.DB, b.token, "POST", "/v1/friends/requests", { user: `@${a.handle}` });
    expect(fired()).toBe(true);
    expect(r.status).toBe(200);
    expect(r.body.request).toMatchObject({ id, status: "accepted" });
    expect(await friendshipExists(a, b)).toBe(true);
  });
});

describe("POST /v1/friends/requests/{id}/accept: the sender's suspension (api.md 3.2 / 3.9 / 5.3)", () => {
  async function aToB(a: TestUser, b: TestUser): Promise<string> {
    const r = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
    expect(r.status).toBe(201);
    return r.body.request.id;
  }

  it("the sender already suspended -> 403 action_not_allowed (pre-check), no friendship, request still pending", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const id = await aToB(a, b);
    await suspend(env.DB, a, null);
    const r = await api(b.token).post<any>(`/v1/friends/requests/${id}/accept`);
    expect(r.status).toBe(403);
    expect(r.body.error.code).toBe("action_not_allowed");
    expect(await friendshipExists(a, b)).toBe(false);
    expect(await requestStatus(id)).toBe("pending");
  });

  it("the sender suspended after the pre-check, before the accept batch -> 403, no friendship", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const id = await aToB(a, b);
    const { rt, fired } = commitsBeforeFirstBatch((db) => suspend(db, a, null));
    const r = await fetchWith(rt.env.DB, b.token, "POST", `/v1/friends/requests/${id}/accept`);
    expect(fired()).toBe(true);
    expect(r).toMatchObject({ status: 403, body: { error: { code: "action_not_allowed" } } });
    expect(await friendshipExists(a, b)).toBe(false);
    expect(await requestStatus(id)).toBe("pending");
  });

  it("a suspension already over (until <= now) -> 200, friends", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const id = await aToB(a, b);
    await suspend(env.DB, a, Date.now() - 1);
    const r = await api(b.token).post<any>(`/v1/friends/requests/${id}/accept`);
    expect(r.status).toBe(200);
    expect(await friendshipExists(a, b)).toBe(true);
  });
});

// ---- direct space invites ----

describe("direct invite insert: friendship / block / suspension are part of the INSERT", () => {
  async function setup() {
    const [a, b] = await Promise.all([signup(), signup()]);
    await befriend(a, b);
    const { space } = await makeSpace(a);
    return { a, b, spaceId: space.id };
  }

  async function invites(spaceId: string, invitee: TestUser): Promise<number> {
    const r = await env.DB.prepare("SELECT COUNT(*) AS n FROM space_direct_invites WHERE space_id = ?1 AND invitee_id = ?2")
      .bind(spaceId, invitee.user.id)
      .first<{ n: number }>();
    return r!.n;
  }

  for (const dir of ["invitee blocks inviter", "inviter blocks invitee"] as const) {
    it(`with a block row in place (${dir}, friendship row still there) -> 403 action_not_allowed, nothing written`, async () => {
      const { a, b, spaceId } = await setup();
      await (dir === "invitee blocks inviter" ? insertBlock(env.DB, b, a) : insertBlock(env.DB, a, b));
      const t = Date.now();
      await expect(insertDirectInvite(env.DB, spaceId, a.user.id, b.user.id, t, newId(t))).rejects.toMatchObject(forbidden);
      expect(await invites(spaceId, b)).toBe(0);
    });
  }

  it("the friendship gone (unfriended / the real block path) -> 403, nothing written", async () => {
    const { a, b, spaceId } = await setup();
    await applyBlock(plainRt, b.user.id, a.user.id);
    const t = Date.now();
    await expect(insertDirectInvite(env.DB, spaceId, a.user.id, b.user.id, t, newId(t))).rejects.toMatchObject(forbidden);
    expect(await invites(spaceId, b)).toBe(0);
  });

  it("only the friendship gone (unfriended, no block row) -> 403, nothing written", async () => {
    const { a, b, spaceId } = await setup();
    await unfriend(plainRt, a.user.id, b.user.id);
    const t = Date.now();
    await expect(insertDirectInvite(env.DB, spaceId, a.user.id, b.user.id, t, newId(t))).rejects.toMatchObject(forbidden);
    expect(await invites(spaceId, b)).toBe(0);
  });

  it("through the API: an unfriend (no block) committed after the route's pre-check -> 403, nothing written", async () => {
    const { a, b, spaceId } = await setup();
    const { rt, fired } = commitsBeforeFirstBatch(() => unfriend(plainRt, b.user.id, a.user.id));
    const r = await fetchWith(rt.env.DB, a.token, "POST", `/v1/spaces/${spaceId}/direct-invites`, { user: `@${b.handle}` });
    expect(fired()).toBe(true);
    expect(r).toMatchObject({ status: 403, body: { error: { code: "action_not_allowed" } } });
    expect(await invites(spaceId, b)).toBe(0);
    expect(await env.DB.prepare("SELECT COUNT(*) AS n FROM blocks WHERE blocker_id IN (?1, ?2)").bind(a.user.id, b.user.id).first("n")).toBe(0);
    expect((await api(b.token).get<any>("/v1/me/direct-invites")).body.items).toEqual([]);
  });

  it("the invitee suspended now -> 403, nothing written", async () => {
    const { a, b, spaceId } = await setup();
    await suspend(env.DB, b, null);
    const t = Date.now();
    await expect(insertDirectInvite(env.DB, spaceId, a.user.id, b.user.id, t, newId(t))).rejects.toMatchObject(forbidden);
    expect(await invites(spaceId, b)).toBe(0);
  });

  it("inviter no longer a member -> 404 (unchanged); all fine -> the pending row", async () => {
    const { a, b, spaceId } = await setup();
    const [c] = await Promise.all([signup()]);
    await befriend(c, b);
    let t = Date.now();
    await expect(insertDirectInvite(env.DB, spaceId, c.user.id, b.user.id, t, newId(t))).rejects.toMatchObject({ status: 404 });
    t = Date.now();
    const { row } = await insertDirectInvite(env.DB, spaceId, a.user.id, b.user.id, t, newId(t));
    expect(row).toMatchObject({ space_id: spaceId, inviter_id: a.user.id, invitee_id: b.user.id, status: "pending" });
  });
});

// ---- the accept's later statements carry the rule themselves, not only statement 1 ----
// Two accepts of the same request can share one now() (Date.now() in Workers only advances on I/O): both pre-reads see
// it pending, X commits (accepted at t + friendship), a block commits (friendship deleted; the request is 'accepted',
// so the cleanup leaves it), then Z runs with the same t. Z's statement 1 changes nothing, but the "accepted at t"
// token its later statements gated on is still true — so those statements must check the block / suspension too.

afterEach(() => {
  vi.restoreAllMocks();
});

/**
 * A Runtime whose D1 handle runs `before(realDb)` right before the first batch and `after(realDb)` right after it
 * resolves (each once): a commit landing just before / just after the batch's transaction.
 */
function aroundFirstBatch(before: (db: D1Database) => Promise<unknown>, after: (db: D1Database) => Promise<unknown>) {
  let fired = false;
  const db = new Proxy(env.DB, {
    get(target, key) {
      if (key === "batch") {
        return async (stmts: D1PreparedStatement[]) => {
          if (fired) return target.batch(stmts);
          fired = true;
          await before(target);
          const res = await target.batch(stmts);
          await after(target);
          return res;
        };
      }
      const v = Reflect.get(target, key);
      return typeof v === "function" ? v.bind(target) : v;
    },
  });
  const rt: Runtime = { env: { ...env, DB: db }, waitUntil: () => {} };
  return { rt, fired: () => fired };
}

/** A D1 handle that runs `after(realDb)` once, right after its first query of any kind (first/all/run/raw/batch) resolves. */
function afterFirstQuery(after: (db: D1Database) => Promise<unknown>) {
  let fired = false;
  const once = async <T>(p: Promise<T>): Promise<T> => {
    const res = await p;
    if (!fired) {
      fired = true;
      await after(env.DB);
    }
    return res;
  };
  const real = new WeakMap<object, D1PreparedStatement>();
  const wrapStmt = (s: D1PreparedStatement): D1PreparedStatement => {
    const p = new Proxy(s, {
      get(target, key) {
        if (key === "bind") return (...args: unknown[]) => wrapStmt(target.bind(...args));
        if (key === "first" || key === "all" || key === "run" || key === "raw") {
          return (...args: unknown[]) => once((Reflect.get(target, key) as (...a: unknown[]) => Promise<unknown>).apply(target, args));
        }
        const v = Reflect.get(target, key);
        return typeof v === "function" ? v.bind(target) : v;
      },
    });
    real.set(p, s);
    return p;
  };
  const db = new Proxy(env.DB, {
    get(target, key) {
      if (key === "prepare") return (sql: string) => wrapStmt(target.prepare(sql));
      // the real batch gets the real statements back (unwrapped)
      if (key === "batch") return (stmts: D1PreparedStatement[]) => once(target.batch(stmts.map((x) => real.get(x) ?? x)));
      const v = Reflect.get(target, key);
      return typeof v === "function" ? v.bind(target) : v;
    },
  });
  return { db, fired: () => fired };
}

describe("makeFriends: a same-ms second accept writes nothing across a block (the friendship INSERT / crossing UPDATE check it)", () => {
  async function pendingRequestId(from: TestUser, to: TestUser): Promise<string> {
    const r = await api(from.token).post<any>("/v1/friends/requests", { user: `@${to.handle}` });
    expect(r.status).toBe(201);
    return r.body.request.id;
  }

  /** Freezes Date.now() for the rest of the test (afterEach restores it). */
  function freezeNow(): number {
    const t = Date.now() + 60_000;
    vi.spyOn(Date, "now").mockReturnValue(t);
    return t;
  }

  async function blockCount(a: TestUser, b: TestUser): Promise<number> {
    return (await env.DB.prepare("SELECT COUNT(*) AS n FROM blocks WHERE (blocker_id = ?1 AND blocked_id = ?2) OR (blocker_id = ?2 AND blocked_id = ?1)")
      .bind(a.user.id, b.user.id)
      .first<number>("n"))!;
  }

  it("X accepts at t, then the real block path, then Z (same t) -> Z 403, no friendship next to the block", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const id = await pendingRequestId(a, b);
    freezeNow();
    // Z's pre-read (before its batch) saw the request pending; X and the block commit before Z's batch
    const { rt, fired } = commitsBeforeFirstBatch(async () => {
      await makeFriends(plainRt, b.user.id, a.user.id, id);
      expect(await friendshipExists(a, b)).toBe(true);
      await applyBlock(plainRt, a.user.id, b.user.id);
      expect(await friendshipExists(a, b)).toBe(false);
    });
    await expect(makeFriends(rt, b.user.id, a.user.id, id)).rejects.toMatchObject(forbidden);
    expect(fired()).toBe(true);
    expect(await blockCount(a, b)).toBe(1);
    expect(await friendshipExists(a, b)).toBe(false);
    expect(await requestStatus(id)).toBe("accepted");
  });

  it("the friendships INSERT on its own: X at t, then a block row and the friendship gone -> Z inserts nothing", async () => {
    for (const dir of ["sender blocks accepter", "accepter blocks sender"] as const) {
      const [a, b] = await Promise.all([signup(), signup()]);
      const id = await pendingRequestId(a, b);
      freezeNow();
      const { rt } = commitsBeforeFirstBatch(async (db) => {
        await makeFriends(plainRt, b.user.id, a.user.id, id);
        await (dir === "sender blocks accepter" ? insertBlock(db, a, b) : insertBlock(db, b, a));
        await unfriend(plainRt, a.user.id, b.user.id);
      });
      await expect(makeFriends(rt, b.user.id, a.user.id, id)).rejects.toMatchObject(forbidden);
      expect(await friendshipExists(a, b)).toBe(false);
      vi.restoreAllMocks();
    }
  });

  it("the friendships INSERT on its own: X at t, then the sender suspended and the friendship gone -> Z inserts nothing", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const id = await pendingRequestId(a, b);
    freezeNow();
    const { rt } = commitsBeforeFirstBatch(async (db) => {
      await makeFriends(plainRt, b.user.id, a.user.id, id);
      await suspend(db, a, null);
      await unfriend(plainRt, a.user.id, b.user.id);
    });
    await expect(makeFriends(rt, b.user.id, a.user.id, id)).rejects.toMatchObject(forbidden);
    expect(await friendshipExists(a, b)).toBe(false);
  });

  it("the crossing-request UPDATE on its own: X at t, then a block row and a pending b -> a request -> Z leaves it pending", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const id = await pendingRequestId(a, b);
    const t = freezeNow();
    const crossing = newId(t);
    const { rt } = commitsBeforeFirstBatch(async (db) => {
      await makeFriends(plainRt, b.user.id, a.user.id, id);
      await insertBlock(db, a, b);
      // a pending row between the pair under the block (raw; the block cleanup / insert gate would not leave one)
      await db
        .prepare("INSERT INTO friend_requests (id, from_user_id, to_user_id, status, created_at) VALUES (?1, ?2, ?3, 'pending', ?4)")
        .bind(crossing, b.user.id, a.user.id, t)
        .run();
    });
    await expect(makeFriends(rt, b.user.id, a.user.id, id)).rejects.toMatchObject(forbidden);
    expect(await requestStatus(crossing)).toBe("pending");
  });

  it("the crossing-request UPDATE on its own: the sender suspended after X -> Z leaves the crossing request pending", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const id = await pendingRequestId(a, b);
    const t = freezeNow();
    const crossing = newId(t);
    const { rt } = commitsBeforeFirstBatch(async (db) => {
      await makeFriends(plainRt, b.user.id, a.user.id, id);
      await suspend(db, a, null);
      await db
        .prepare("INSERT INTO friend_requests (id, from_user_id, to_user_id, status, created_at) VALUES (?1, ?2, ?3, 'pending', ?4)")
        .bind(crossing, b.user.id, a.user.id, t)
        .run();
    });
    await expect(makeFriends(rt, b.user.id, a.user.id, id)).rejects.toMatchObject(forbidden);
    expect(await requestStatus(crossing)).toBe("pending");
  });

  it("no block, same-ms double accept -> the second is a 404 and the friendship stays (one row)", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const id = await pendingRequestId(a, b);
    freezeNow();
    const { rt } = commitsBeforeFirstBatch(() => makeFriends(plainRt, b.user.id, a.user.id, id));
    await expect(makeFriends(rt, b.user.id, a.user.id, id)).rejects.toMatchObject({ status: 404 });
    expect(await friendshipExists(a, b)).toBe(true);
  });
});

describe("the reason for a 0-row write is read in the write's own transaction (a lift right after it does not change the code)", () => {
  it("insertFriendRequest: blocked at the INSERT, block lifted right after -> still 403 action_not_allowed, nothing written", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await insertBlock(env.DB, b, a);
    const { db, fired } = afterFirstQuery(() => removeBlock(plainRt, b.user.id, a.user.id));
    await expect(insertFriendRequest(db, requestRow(a, b), 100)).rejects.toMatchObject(forbidden);
    expect(fired()).toBe(true);
    expect(await pendingRequests(a, b)).toBe(0);
  });

  it("insertFriendRequest: target suspended at the INSERT, lifted right after -> still 403, nothing written", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await suspend(env.DB, b, null);
    const { db, fired } = afterFirstQuery((real) => real.prepare("UPDATE users SET suspended_at = NULL, suspended_until = NULL WHERE id = ?1").bind(b.user.id).run());
    await expect(insertFriendRequest(db, requestRow(a, b), 100)).rejects.toMatchObject(forbidden);
    expect(fired()).toBe(true);
    expect(await pendingRequests(a, b)).toBe(0);
  });

  it("makeFriends: a block landing before the batch and lifted right after it -> 403 (not 404), no friendship", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const r = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
    const id: string = r.body.request.id;
    const { rt, fired } = aroundFirstBatch(
      (db) => insertBlock(db, a, b),
      () => removeBlock(plainRt, a.user.id, b.user.id),
    );
    await expect(makeFriends(rt, b.user.id, a.user.id, id)).rejects.toMatchObject(forbidden);
    expect(fired()).toBe(true);
    expect(await friendshipExists(a, b)).toBe(false);
    expect(await requestStatus(id)).toBe("pending");
  });

  it("makeFriends: the sender suspended before the batch and lifted right after it -> 403 (not 404), no friendship", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const r = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
    const id: string = r.body.request.id;
    const { rt, fired } = aroundFirstBatch(
      (db) => suspend(db, a, null),
      (db) => db.prepare("UPDATE users SET suspended_at = NULL, suspended_until = NULL WHERE id = ?1").bind(a.user.id).run(),
    );
    await expect(makeFriends(rt, b.user.id, a.user.id, id)).rejects.toMatchObject(forbidden);
    expect(fired()).toBe(true);
    expect(await friendshipExists(a, b)).toBe(false);
    expect(await requestStatus(id)).toBe("pending");
  });
});
