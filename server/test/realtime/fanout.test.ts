import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import type { Runtime } from "../../src/env";
import { deliverToUsers, publishEvent, publishEvents, resolveOnlineAudience } from "../../src/lib/realtime";
import { newId } from "../../src/lib/ids";
import { notify, notifyMany, sendAlerts } from "../../src/lib/notify";
import type { Alert } from "../../src/types/api";
import { connectWs, makeSpace, signup, uniqueHandle } from "../helpers";

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

/** blocker blocks blocked (raw row). */
const blockSql = (blocker: U, blocked: U) => block(blocker, blocked);

/** 1:1 dm channel between a and b (raw rows); returns channelId. */
async function dmSql(a: U, b: U): Promise<string> {
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

/** Alert target with every optional field null. */
const target = (type: Alert["target"]["type"], id: string): Alert["target"] => ({
  type,
  id,
  channel_id: null,
  message_id: null,
  thread_root_id: null,
  post_id: null,
  space_id: null,
});

const sorted = (ids: string[]) => [...ids].sort();

const rtFor = (e: typeof env): Runtime => ({ env: e, waitUntil: () => {} });

/** Short wait so anything still in flight would have arrived. */
const settle = (ms = 150) => new Promise<void>((r) => setTimeout(r, ms));

/** Shape only matters to the client; the hub frames whatever data it gets. */
const fakeMessage = (channelId: string) => ({ id: newId(), channel_id: channelId, body: "hi" });

function fakeAlert(): Alert {
  return {
    id: newId(),
    kind: "channel_message",
    title: "hi",
    body: "hello",
    actor: null,
    target: { type: "channel", id: newId(), channel_id: null, message_id: null, thread_root_id: null, post_id: null, space_id: null },
    created_at: new Date().toISOString(),
    suppressed: false,
    suppressed_reason: null,
  };
}

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

  it("summaryWatchers = presenceWatchers + co-members of live spaces (each once; a deleted space drops out)", async () => {
    const [a, b, c, d, e] = await Promise.all([signup(), signup(), signup(), signup(), signup()]);
    await friend(a, b);
    await groupDm(a, [c]);
    await space(d, [a, b]);
    const gone = await space(e, [a]);
    await env.DB.prepare("UPDATE spaces SET deleted_at = ?1 WHERE id = ?2").bind(Date.now(), gone).run();
    for (const u of [a, b, c, d, e]) await online(u, { full: true });
    const got = await resolveOnlineAudience(env, { summaryWatchers: a.user.id });
    expect(sorted(got)).toEqual(sorted([b.user.id, c.user.id, d.user.id]));
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

describe("publish", () => {
  it("two members in one space both receive message.created (self included)", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const s = await space(a, [b]);
    const ch = await publicText(s);
    const [ca, cb] = await Promise.all([connectWs(a.token), connectWs(b.token)]);
    await publishEvent(rtFor(env), { channel: ch }, "message.created", { message: fakeMessage(ch) } as any);
    const [fa, fb] = await Promise.all([ca.next("message.created"), cb.next("message.created")]);
    expect(fa.seq).toBeGreaterThan(0);
    expect(fb.seq).toBeGreaterThan(0);
  });

  it("exclude skips; ephemeral has seq null", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const s = await space(a, [b]);
    const ch = await publicText(s);
    const [ca, cb] = await Promise.all([connectWs(a.token), connectWs(b.token)]);
    await publishEvent(rtFor(env), { channel: ch }, "typing", { channel_id: ch, thread_root_id: null, user_id: a.user.id, expires_in_ms: 6000 }, { ephemeral: true, exclude: [a.user.id] });
    expect((await cb.next("typing")).seq).toBeNull();
    await settle();
    expect(ca.frames.some((f) => f.type === "typing")).toBe(false);
    expect((await env.USER_HUB.getByName(b.user.id).inspect()).seq).toBe(0); // ephemeral: not logged
  });

  it("mixed batch is split: daemon-only user gets only the notify-allowed part, TUI user gets both in one seq run", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const s = await space(a, [b]);
    const ch = await publicText(s);
    const ca = await connectWs(a.token);
    const db = await connectWs(b.token, { mode: "notify" });
    await publishEvents(rtFor(env), { space: s }, [
      { type: "message.created", data: { message: fakeMessage(ch) } },
      { type: "alert", data: { alert: fakeAlert() } },
    ]);
    await Promise.all([ca.next("message.created"), ca.next("alert"), db.next("alert")]);
    expect((await env.USER_HUB.getByName(a.user.id).inspect()).seq).toBe(2);
    expect((await env.USER_HUB.getByName(b.user.id).inspect()).seq).toBe(1); // message.created never woke b's hub
    expect(db.frames.some((f) => f.type === "message.created")).toBe(false);
  });

  it("includeSelf adds the user only if online by the same rule", async () => {
    const [author, f, offline] = await Promise.all([signup(), signup(), signup()]);
    await followActive(f, author);
    const [cAuthor, cF] = await Promise.all([connectWs(author.token), connectWs(f.token)]);
    const data = { item: { id: newId() } } as any;
    await publishEvent(rtFor(env), { followers: author.user.id }, "timeline.item_created", data, { includeSelf: author.user.id });
    await Promise.all([cAuthor.next("timeline.item_created"), cF.next("timeline.item_created")]);
    // includeSelf of an offline user reaches no hub
    await publishEvent(rtFor(env), { followers: author.user.id }, "timeline.item_created", data, { includeSelf: offline.user.id });
    expect((await env.USER_HUB.getByName(offline.user.id).inspect()).seq).toBe(0);
    // includeSelf already in the audience is delivered once
    await publishEvent(rtFor(env), { users: [author.user.id] }, "timeline.item_created", data, { includeSelf: author.user.id });
    await cAuthor.next("timeline.item_created");
    await settle();
    expect(cAuthor.frames.filter((x) => x.type === "timeline.item_created")).toHaveLength(2);
  });

  it("deliverToUsers online-filters explicit users by the notify rule", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    const ca = await connectWs(a.token);
    const db = await connectWs(b.token, { mode: "notify" });
    const ts = Date.now();
    await deliverToUsers(rtFor(env), [a.user.id, b.user.id, c.user.id], [{ type: "settings.updated", data: { n: 1 } as any, ts, logged: true }]);
    await ca.next("settings.updated");
    expect((await env.USER_HUB.getByName(b.user.id).inspect()).seq).toBe(0);
    await deliverToUsers(rtFor(env), [a.user.id, b.user.id, c.user.id], [{ type: "alert", data: { alert: fakeAlert() }, ts, logged: true }]);
    await Promise.all([ca.next("alert"), db.next("alert")]);
    expect((await env.USER_HUB.getByName(c.user.id).inspect()).seq).toBe(0);
  });

  it("fans out to more than 50 online users (RPC chunks)", async () => {
    const owner = await signup();
    const members = await seedUsers(60);
    const s = await space(owner, members);
    const ch = await publicText(s);
    for (const u of [owner, ...members]) await online(u, { full: true });
    await publishEvent(rtFor(env), { channel: ch }, "message.created", { message: fakeMessage(ch) } as any);
    const seqs = await Promise.all([owner, ...members].map((u) => env.USER_HUB.getByName(u.user.id).inspect().then((x) => x.seq)));
    expect(seqs.every((n) => n === 1)).toBe(true);
  });
});

describe("notify", () => {
  it("creates inbox row + notification.created + alert when switch on", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const cb = await connectWs(b.token);
    await notify(rtFor(env), {
      userId: b.user.id,
      type: "friend_request",
      actorId: a.user.id,
      subject: { type: "friend_request", id: newId() },
      alert: { kind: "friend_request", title: "t", body: "b", target: target("user", a.user.id) },
    });
    const n = await cb.next("notification.created");
    expect(n.data.unread_count).toBe(1);
    expect(n.data.notification).toMatchObject({ type: "friend_request", actor: { id: a.user.id }, read: false, context: { post_id: null } });
    expect((await cb.next("alert")).data.alert).toMatchObject({ kind: "friend_request", actor: { id: a.user.id } });
  });

  it("switch off -> no alert; self or blocked -> nothing; dedupe", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await env.DB.prepare("UPDATE user_settings SET notify_social=0 WHERE user_id=?").bind(b.user.id).run();
    const cb = await connectWs(b.token);
    const inp = {
      userId: b.user.id,
      type: "follow" as const,
      actorId: a.user.id,
      subject: { type: "user" as const, id: a.user.id },
      dedupeKey: `follow:${a.user.id}`,
      alert: { kind: "social" as const, title: "t", body: "", target: target("user", a.user.id) },
    };
    await notify(rtFor(env), inp);
    await notify(rtFor(env), inp);
    await cb.next("notification.created"); // the first one arrives …
    await settle(); // … and no second one follows (negative assertion: a sleep is the only way)
    expect(cb.frames.filter((f) => f.type === "notification.created")).toHaveLength(1);
    expect(cb.frames.some((f) => f.type === "alert")).toBe(false);
    await notify(rtFor(env), { ...inp, actorId: b.user.id, dedupeKey: undefined });
    await blockSql(b, a);
    await notify(rtFor(env), { ...inp, dedupeKey: "other" });
    expect((await env.DB.prepare("SELECT COUNT(*) n FROM notifications WHERE user_id=?").bind(b.user.id).first<any>()).n).toBe(1);
  });

  it("notifyMany to more than 50 online recipients in production mode: one scheduled fan-out, one presence query, at most 50 RPCs in flight", async () => {
    const actor = await signup();
    const members = await seedUsers(60);
    for (const u of members) await online(u, { full: true });

    // Production-like runtime: APP_ENV != 'test' hands work to waitUntil; count presence queries and in-flight RPCs.
    let presenceQueries = 0;
    const db = new Proxy(env.DB, {
      get(t, k) {
        if (k === "prepare")
          return (sql: string) => {
            if (sql.includes("user_presence")) presenceQueries++;
            return t.prepare(sql);
          };
        const v = (t as any)[k];
        return typeof v === "function" ? v.bind(t) : v;
      },
    });
    let inFlight = 0;
    let maxInFlight = 0;
    let delivers = 0;
    const hub = {
      getByName(name: string) {
        const stub = env.USER_HUB.getByName(name);
        return {
          async deliver(arg: any) {
            delivers++;
            inFlight++;
            maxInFlight = Math.max(maxInFlight, inFlight);
            try {
              await settle(20);
              return await stub.deliver(arg);
            } finally {
              inFlight--;
            }
          },
        };
      },
    };
    const pending: Promise<unknown>[] = [];
    const prodRt: Runtime = { env: { ...env, APP_ENV: "production", DB: db, USER_HUB: hub } as any, waitUntil: (p) => void pending.push(p) };

    await notifyMany(
      prodRt,
      members.map((m) => ({
        userId: m.user.id,
        type: "friend_request" as const,
        actorId: actor.user.id,
        subject: { type: "friend_request" as const, id: newId() },
        alert: { kind: "friend_request" as const, title: "t", body: "b", target: target("user", actor.user.id) },
      })),
    );
    expect(pending).toHaveLength(1); // a single schedule()
    await Promise.all(pending);
    expect(presenceQueries).toBe(1); // notifyMany + its fan-out: one online-audience query in total
    expect(delivers).toBe(60); // one RPC per recipient, carrying both events
    expect(maxInFlight).toBeLessThanOrEqual(50);
    const seqs = await Promise.all(members.map((u) => env.USER_HUB.getByName(u.user.id).inspect().then((x) => x.seq)));
    expect(seqs.every((n) => n === 2)).toBe(true); // notification.created + alert
  });

  it("sendAlerts respects muted / notify:none channel prefs", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const ch = await dmSql(a, b);
    const cb = await connectWs(b.token);
    await env.DB.prepare("INSERT INTO channel_prefs VALUES (?1, ?2, 'all', 1, 0)").bind(b.user.id, ch).run();
    await sendAlerts(rtFor(env), [{ userId: b.user.id, kind: "dm", actorId: a.user.id, title: "a", body: "hi", target: target("message", newId()), channelId: ch }]);
    await settle();
    expect(cb.frames.some((f) => f.type === "alert")).toBe(false);
    // notify:'none' (not muted) also suppresses
    await env.DB.prepare("UPDATE channel_prefs SET muted=0, notify='none' WHERE user_id=?1 AND channel_id=?2").bind(b.user.id, ch).run();
    await sendAlerts(rtFor(env), [{ userId: b.user.id, kind: "dm", actorId: a.user.id, title: "a", body: "hi", target: target("message", newId()), channelId: ch }]);
    await settle();
    expect(cb.frames.some((f) => f.type === "alert")).toBe(false);
    // no prefs row -> dm default 'all': delivered, clipped to 80/200 code points
    await env.DB.prepare("DELETE FROM channel_prefs WHERE user_id=?1 AND channel_id=?2").bind(b.user.id, ch).run();
    await sendAlerts(rtFor(env), [{ userId: b.user.id, kind: "dm", actorId: a.user.id, title: "가".repeat(90), body: "나".repeat(250), target: target("message", newId()), channelId: ch }]);
    const al = (await cb.next("alert")).data.alert;
    expect(al).toMatchObject({ kind: "dm", actor: { id: a.user.id }, suppressed: false, suppressed_reason: null });
    expect([...al.title]).toHaveLength(80);
    expect([...al.body]).toHaveLength(200);
  });
});

describe("typing", () => {
  it("typing reaches other members only, ephemeral, deduped within 2s; no write access -> reply not_found", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { generalChannelId: g } = await makeSpace(o, [m]);
    const [co, cm] = await Promise.all([connectWs(o.token), connectWs(m.token)]);
    co.send({ type: "typing", data: { channel_id: g, thread_root_id: null } });
    co.send({ type: "typing", data: { channel_id: g, thread_root_id: null } });
    const t = await cm.next("typing");
    expect(t).toMatchObject({ seq: null, data: { channel_id: g, user_id: o.user.id, expires_in_ms: 6000 } });
    await settle();
    expect(cm.frames.filter((f) => f.type === "typing")).toHaveLength(1);
    expect(co.frames.some((f) => f.type === "typing")).toBe(false);
    const x = await signup();
    const cx = await connectWs(x.token);
    cx.send({ type: "typing", ref: "t1", data: { channel_id: g, thread_root_id: null } });
    expect((await cx.next("reply", (d) => d.ref === "t1")).data.error.code).toBe("not_found");
  });
});
