// Pending item 10-a: when a conditional write changes nothing, the error code it answers with must come from the facts
// the write itself saw — read in the same batch (one transaction) — not from a re-read after the batch, where a commit
// landing in between (a block lifted, a membership restored, a row canceled, a message deleted) turns the code into a
// different one. insertFriendRequest / makeFriends already do this (relation-races.test.ts pins their 403s); this file
// pins their remaining reason (409 pending) and every sibling 0-row path that used to re-read.
import { env, createExecutionContext, waitOnExecutionContext } from "cloudflare:test";
import { describe, expect, it } from "vitest";
import { createApp } from "../../src/app";
import type { Runtime } from "../../src/env";
import { newId } from "../../src/lib/ids";
import { insertDirectInvite, joinSpace } from "../../src/services/membership";
import { applyBlock, follow, insertFriendRequest } from "../../src/services/relations";
import { api, makeFriends, makeSpace, signup, uniqueIp, uploadPng, type TestUser } from "../helpers";

// ---- local fixtures (this file only) ----

/**
 * A D1 handle that runs `before(realDb)` right before, and `after(realDb)` right after, the first query whose SQL
 * matches `pattern` — a lone statement (first/all/run/raw) or a batch holding one — once. Commits landing just before
 * the write's transaction and just after it, whatever the call shape.
 */
function aroundSql(pattern: RegExp, before: (db: D1Database) => Promise<unknown>, after: (db: D1Database) => Promise<unknown>) {
  let fired = false;
  const sqlOf = new WeakMap<object, string>();
  const real = new WeakMap<object, D1PreparedStatement>();
  const around = async <T>(hit: boolean, run: () => Promise<T>): Promise<T> => {
    if (!hit || fired) return run();
    fired = true;
    await before(env.DB);
    const res = await run();
    await after(env.DB);
    return res;
  };
  const wrapStmt = (s: D1PreparedStatement, sql: string): D1PreparedStatement => {
    const p = new Proxy(s, {
      get(target, key) {
        if (key === "bind") return (...args: unknown[]) => wrapStmt(target.bind(...args), sql);
        if (key === "first" || key === "all" || key === "run" || key === "raw") {
          return (...args: unknown[]) =>
            around(pattern.test(sql), () => (Reflect.get(target, key) as (...a: unknown[]) => Promise<unknown>).apply(target, args));
        }
        const v = Reflect.get(target, key);
        return typeof v === "function" ? v.bind(target) : v;
      },
    });
    sqlOf.set(p, sql);
    real.set(p, s);
    return p;
  };
  const db = new Proxy(env.DB, {
    get(target, key) {
      if (key === "prepare") return (sql: string) => wrapStmt(target.prepare(sql), sql);
      if (key === "batch") {
        return (stmts: D1PreparedStatement[]) =>
          around(
            stmts.some((x) => pattern.test(sqlOf.get(x) ?? "")),
            () => target.batch(stmts.map((x) => real.get(x) ?? x)),
          );
      }
      const v = Reflect.get(target, key);
      return typeof v === "function" ? v.bind(target) : v;
    },
  });
  const rt: Runtime = { env: { ...env, DB: db }, waitUntil: () => {} };
  return { db, rt, fired: () => fired };
}

/** One request through a fresh app whose env.DB is `db`; background work is awaited. */
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

const sql = (q: string, ...args: unknown[]) => env.DB.prepare(q).bind(...args).run();

// ---- relations ----

describe("insertFriendRequest: the 409 pending reason is the INSERT's own", () => {
  it("a concurrent pending a -> b committed before the INSERT and canceled right after -> 409 already_exists pending (not 422)", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const other = newId(Date.now());
    const { db, fired } = aroundSql(
      /INSERT INTO friend_requests/,
      () => sql("INSERT INTO friend_requests (id, from_user_id, to_user_id, status, created_at) VALUES (?1, ?2, ?3, 'pending', ?4)", other, a.user.id, b.user.id, Date.now()),
      () => sql("UPDATE friend_requests SET status = 'canceled', responded_at = ?2 WHERE id = ?1", other, Date.now()),
    );
    const t = Date.now();
    const row = { id: newId(t), from_user_id: a.user.id, to_user_id: b.user.id, status: "pending" as const, created_at: t, responded_at: null };
    await expect(insertFriendRequest(db, row, 100)).rejects.toMatchObject({ status: 409, code: "already_exists", details: { reason: "pending" } });
    expect(fired()).toBe(true);
  });
});

describe("follow: a 0-row INSERT reports what the INSERT saw", () => {
  it("a concurrent follow won the race, then a block landed right after the batch -> that follow's state (not 403)", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const { rt, fired } = aroundSql(
      /INSERT INTO follows/,
      (db) =>
        db.batch([
          db.prepare("INSERT INTO follows (follower_id, followee_id, status, created_at, accepted_at) VALUES (?1, ?2, 'active', ?3, ?3)").bind(a.user.id, b.user.id, Date.now()),
          db.prepare("UPDATE users SET following_count = following_count + 1 WHERE id = ?1").bind(a.user.id),
          db.prepare("UPDATE users SET follower_count = follower_count + 1 WHERE id = ?1").bind(b.user.id),
        ]),
      () => applyBlock({ env, waitUntil: () => {} }, b.user.id, a.user.id),
    );
    expect(await follow(rt, a.user.id, b.user.id)).toBe("active");
    expect(fired()).toBe(true);
  });

  it("blocked at the INSERT, lifted right after -> still 403 action_not_allowed, nothing written", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const { rt, fired } = aroundSql(
      /INSERT INTO follows/,
      () => sql("INSERT INTO blocks (blocker_id, blocked_id, created_at) VALUES (?1, ?2, ?3)", b.user.id, a.user.id, Date.now()),
      () => sql("DELETE FROM blocks WHERE blocker_id = ?1 AND blocked_id = ?2", b.user.id, a.user.id),
    );
    await expect(follow(rt, a.user.id, b.user.id)).rejects.toMatchObject({ status: 403, code: "action_not_allowed" });
    expect(fired()).toBe(true);
    expect(await env.DB.prepare("SELECT 1 AS x FROM follows WHERE follower_id = ?1 AND followee_id = ?2").bind(a.user.id, b.user.id).first()).toBeNull();
  });
});

// ---- direct invites ----

describe("insertDirectInvite: a refused INSERT reports what the INSERT saw", () => {
  async function setup() {
    const [a, b] = await Promise.all([signup(), signup()]);
    await makeFriends(a, b);
    const { space } = await makeSpace(a);
    return { a, b, spaceId: space.id };
  }

  it("the inviter out of the space at the INSERT, back right after -> 404 (not 403)", async () => {
    const { a, b, spaceId } = await setup();
    const { db, fired } = aroundSql(
      /INSERT INTO space_direct_invites/,
      () => sql("DELETE FROM space_members WHERE space_id = ?1 AND user_id = ?2", spaceId, a.user.id),
      () => sql("INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?1, ?2, 'owner', ?3)", spaceId, a.user.id, Date.now()),
    );
    const t = Date.now();
    await expect(insertDirectInvite(db, spaceId, a.user.id, b.user.id, t, newId(t))).rejects.toMatchObject({ status: 404 });
    expect(fired()).toBe(true);
  });

  it("the invitee inside the space at the INSERT, gone right after -> 409 already_member (not 403)", async () => {
    const { a, b, spaceId } = await setup();
    const { db, fired } = aroundSql(
      /INSERT INTO space_direct_invites/,
      () => sql("INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?1, ?2, 'member', ?3)", spaceId, b.user.id, Date.now()),
      () => sql("DELETE FROM space_members WHERE space_id = ?1 AND user_id = ?2", spaceId, b.user.id),
    );
    const t = Date.now();
    await expect(insertDirectInvite(db, spaceId, a.user.id, b.user.id, t, newId(t))).rejects.toMatchObject({ status: 409, code: "already_member" });
    expect(fired()).toBe(true);
  });

  it("blocked at the INSERT, lifted right after -> 403 action_not_allowed", async () => {
    const { a, b, spaceId } = await setup();
    const { db, fired } = aroundSql(
      /INSERT INTO space_direct_invites/,
      () => sql("INSERT INTO blocks (blocker_id, blocked_id, created_at) VALUES (?1, ?2, ?3)", b.user.id, a.user.id, Date.now()),
      () => sql("DELETE FROM blocks WHERE blocker_id = ?1 AND blocked_id = ?2", b.user.id, a.user.id),
    );
    const t = Date.now();
    await expect(insertDirectInvite(db, spaceId, a.user.id, b.user.id, t, newId(t))).rejects.toMatchObject({ status: 403, code: "action_not_allowed" });
    expect(fired()).toBe(true);
  });
});

// ---- joining by link ----

describe("joinSpace by link: the 410 reason is the use UPDATE's own", () => {
  it("the link's creator demoted at the join, re-promoted right after -> 410 revoked (not exhausted)", async () => {
    const [o, c] = await Promise.all([signup(), signup()]);
    const { space } = await makeSpace(o);
    const inv = await api(o.token).post<any>(`/v1/spaces/${space.id}/invites`, { max_uses: null });
    expect(inv.status).toBe(201);
    const { rt, fired } = aroundSql(
      /UPDATE space_invites SET use_count/,
      () => sql("UPDATE space_members SET role = 'member' WHERE space_id = ?1 AND user_id = ?2", space.id, o.user.id),
      () => sql("UPDATE space_members SET role = 'owner' WHERE space_id = ?1 AND user_id = ?2", space.id, o.user.id),
    );
    await expect(joinSpace(rt, space.id, c.user.id, { inviteCode: inv.body.invite.code })).rejects.toMatchObject({
      status: 410,
      code: "invite_invalid",
      details: { reason: "revoked" },
    });
    expect(fired()).toBe(true);
    expect(await env.DB.prepare("SELECT 1 AS x FROM space_members WHERE space_id = ?1 AND user_id = ?2").bind(space.id, c.user.id).first()).toBeNull();
  });
});

// ---- reactions / pins ----

async function spaceMessage(): Promise<{ o: TestUser; msgId: string; ch: string }> {
  const o = await signup();
  const { generalChannelId: ch } = await makeSpace(o);
  const r = await api(o.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: crypto.randomUUID().slice(0, 12), body: "hi" });
  expect(r.status).toBe(201);
  return { o, msgId: r.body.message.id, ch };
}

describe("PUT reaction: the 422 / 404 choice is the INSERT's own", () => {
  it("20 distinct emoji at the INSERT, the message deleted right after -> 422 reactions_per_message (not 404)", async () => {
    const { o, msgId } = await spaceMessage();
    const emoji = JSON.stringify(Array.from({ length: 20 }, (_, i) => String.fromCodePoint(0x1f600 + i)));
    await sql(
      "INSERT INTO message_reactions (message_id, emoji, user_id, created_at) SELECT ?1, j.value, ?2, ?3 FROM json_each(?4) j",
      msgId,
      o.user.id,
      Date.now(),
      emoji,
    );
    const { db, fired } = aroundSql(
      /INSERT INTO message_reactions/,
      async () => {},
      () => sql("UPDATE messages SET deleted_at = ?2 WHERE id = ?1", msgId, Date.now()),
    );
    const r = await fetchWith(db, o.token, "PUT", `/v1/messages/${msgId}/reactions/${encodeURIComponent("🙏")}`);
    expect(fired()).toBe(true);
    expect(r).toMatchObject({ status: 422, body: { error: { code: "limit_exceeded" } } });
  });
});

describe("PUT pin: the 204 / 422 / 404 choice is the INSERT's own", () => {
  it("pinned by someone else at the INSERT, unpinned right after -> 204 already pinned (not 422)", async () => {
    const { o, msgId, ch } = await spaceMessage();
    const { db, fired } = aroundSql(
      /INSERT INTO message_pins/,
      () => sql("INSERT INTO message_pins (channel_id, message_id, pinned_by, pinned_at) VALUES (?1, ?2, ?3, ?4)", ch, msgId, o.user.id, Date.now()),
      () => sql("DELETE FROM message_pins WHERE message_id = ?1", msgId),
    );
    const r = await fetchWith(db, o.token, "PUT", `/v1/messages/${msgId}/pin`);
    expect(fired()).toBe(true);
    expect(r.status).toBe(204);
  });

  it("the message deleted at the INSERT -> 404 message", async () => {
    const { o, msgId } = await spaceMessage();
    const { db, fired } = aroundSql(
      /INSERT INTO message_pins/,
      () => sql("UPDATE messages SET deleted_at = ?2 WHERE id = ?1", msgId, Date.now()),
      async () => {},
    );
    const r = await fetchWith(db, o.token, "PUT", `/v1/messages/${msgId}/pin`);
    expect(fired()).toBe(true);
    expect(r).toMatchObject({ status: 404 });
  });
});

// ---- stories ----

describe("POST /v1/stories: a refused INSERT reports what the INSERT saw", () => {
  async function seedActiveStories(u: TestUser, n: number): Promise<unknown> {
    const t = Date.now();
    const ids = JSON.stringify(Array.from({ length: n }, () => newId(t)));
    return sql(
      "INSERT INTO stories (id, author_id, body, bg, created_at, expires_at) SELECT j.value, ?2, 'seed', 'accent', ?3, ?4 FROM json_each(?1) j",
      ids,
      u.user.id,
      t,
      t + 86_400_000,
    );
  }

  it("30 active at the INSERT, the media attached elsewhere right after -> 422 active_stories (not 400)", async () => {
    const a = await signup();
    const media = await uploadPng(a, "story");
    const { db, fired } = aroundSql(
      /INSERT INTO stories/,
      () => seedActiveStories(a, 30),
      () => sql("UPDATE uploads SET status = 'attached', attached_to = 'story:x' WHERE id = ?1", media.id),
    );
    const r = await fetchWith(db, a.token, "POST", "/v1/stories", { body: "hi", media_upload_id: media.id });
    expect(fired()).toBe(true);
    expect(r).toMatchObject({ status: 422, body: { error: { code: "limit_exceeded" } } });
  });

  it("the media taken at the INSERT -> 400 media_upload_id invalid", async () => {
    const a = await signup();
    const media = await uploadPng(a, "story");
    const { db, fired } = aroundSql(
      /INSERT INTO stories/,
      () => sql("UPDATE uploads SET status = 'attached', attached_to = 'story:x' WHERE id = ?1", media.id),
      async () => {},
    );
    const r = await fetchWith(db, a.token, "POST", "/v1/stories", { body: "hi", media_upload_id: media.id });
    expect(fired()).toBe(true);
    expect(r).toMatchObject({ status: 400 });
    expect(r.body.error.details.fields.media_upload_id).toBe("invalid");
  });
});
