// test/cron/cleanup.test.ts
import { env, createScheduledController, createExecutionContext } from "cloudflare:test";
import { expect, test } from "vitest";
import { handleScheduled } from "../../src/cron";
import { api, makeSpace, signup, uploadPng } from "../helpers";
const run = () => handleScheduled(createScheduledController({ cron: "17 * * * *", scheduledTime: Date.now() }), env, createExecutionContext());
const H = 3_600_000, D = 86_400_000;

test("expired story (>1h past) deleted with views and R2 media; recently expired kept", async () => {
  const a = await signup(); const v = await signup();
  const m = await uploadPng(a, "story");
  const old = (await api(a.token).post<any>("/v1/stories", { media_upload_id: m.id })).body.story;
  const recent = (await api(a.token).post<any>("/v1/stories", { body: "r" })).body.story;
  await api(v.token).post(`/v1/stories/${old.id}/view`);
  await env.DB.batch([
    env.DB.prepare("UPDATE stories SET expires_at=? WHERE id=?").bind(Date.now() - H - 1000, old.id),
    env.DB.prepare("UPDATE stories SET expires_at=? WHERE id=?").bind(Date.now() - 1000, recent.id)]);
  await run();
  expect(await env.DB.prepare("SELECT 1 FROM stories WHERE id=?").bind(old.id).first()).toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM story_views WHERE story_id=?").bind(old.id).first()).toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM stories WHERE id=?").bind(recent.id).first()).not.toBeNull();
  expect((await env.DB.prepare("SELECT status FROM uploads WHERE id=?").bind(m.id).first<any>()).status).toBe("removed");
  expect(await env.MEDIA.head(`stories/${a.user.id}/${m.id}.png`)).toBeNull();
});
test("orphan pending upload >24h removed; status message and suspension expiry cleared; stale presence offline", async () => {
  const a = await signup();
  const up = await uploadPng(a, "post");
  const now = Date.now();
  await env.DB.batch([
    env.DB.prepare("UPDATE uploads SET created_at=? WHERE id=?").bind(now - D - 1000, up.id),
    env.DB.prepare("UPDATE users SET status_text='x', status_expires_at=?, suspended_at=?, suspended_until=?, suspend_reason='r' WHERE id=?").bind(now - 1, now - D, now - 1, a.user.id),
    env.DB.prepare("UPDATE user_presence SET status='online', online_until=? WHERE user_id=?").bind(now - 1, a.user.id)]);
  await run();
  expect((await env.DB.prepare("SELECT status FROM uploads WHERE id=?").bind(up.id).first<any>()).status).toBe("removed");
  expect(await env.DB.prepare("SELECT status_text, status_expires_at, suspended_at, suspended_until, suspend_reason FROM users WHERE id=?").bind(a.user.id).first())
    .toEqual({ status_text: null, status_expires_at: null, suspended_at: null, suspended_until: null, suspend_reason: null });
  expect((await env.DB.prepare("SELECT status FROM user_presence WHERE user_id=?").bind(a.user.id).first<any>()).status).toBe("offline");
});
test("old sessions, notifications, responded requests, dead invites deleted; pending requests kept", async () => {
  const a = await signup(); const b = await signup(); const c = await signup();
  await api(a.token).post("/v1/friends/requests", { user: `@${b.handle}` });
  const kept = await api(c.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
  const now = Date.now(), M = 30 * D;
  await env.DB.batch([
    env.DB.prepare("UPDATE friend_requests SET status='declined', responded_at=? WHERE from_user_id=?").bind(now - M - 1000, a.user.id),
    env.DB.prepare("UPDATE friend_requests SET created_at=? WHERE id=?").bind(now - 2 * M, kept.body.request.id),
    env.DB.prepare("UPDATE notifications SET created_at=? WHERE user_id=?").bind(now - 90 * D - 1000, b.user.id),
    env.DB.prepare("UPDATE sessions SET revoked_at=?, revoke_reason='logout' WHERE user_id=?").bind(now - M - 1000, c.user.id)]);
  await run();
  expect(await env.DB.prepare("SELECT 1 FROM friend_requests WHERE from_user_id=?").bind(a.user.id).first()).toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM friend_requests WHERE id=?").bind(kept.body.request.id).first()).not.toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM notifications WHERE user_id=?").bind(b.user.id).first()).toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM sessions WHERE user_id=?").bind(c.user.id).first()).toBeNull();
});
test("dead invite links, responded direct invites and expired sessions deleted; live and pending kept", async () => {
  const o = await signup(); const u = await signup(); const s = await signup();
  const { space } = await makeSpace(o);
  const mk = async () => (await api(o.token).post<any>(`/v1/spaces/${space.id}/invites`, { max_uses: null })).body.invite;
  const [revoked, expired, live] = [await mk(), await mk(), await mk()];
  const now = Date.now(), M = 30 * D;
  const di = (status: string, responded: number | null) => {
    const id = crypto.randomUUID();
    return { id, st: env.DB.prepare("INSERT INTO space_direct_invites (id, space_id, inviter_id, invitee_id, status, created_at, responded_at) VALUES (?,?,?,?,?,?,?)")
      .bind(id, space.id, o.user.id, u.user.id, status, now - 2 * M, responded) };
  };
  const doneOld = di("declined", now - M - 1000), doneRecent = di("accepted", now - 1000), pendingOld = di("pending", null);
  await env.DB.batch([
    env.DB.prepare("UPDATE space_invites SET revoked_at=? WHERE code=?").bind(now - M - 1000, revoked.code),
    env.DB.prepare("UPDATE space_invites SET expires_at=? WHERE code=?").bind(now - M - 1000, expired.code),
    env.DB.prepare("UPDATE space_invites SET expires_at=? WHERE code=?").bind(now - 1000, live.code),
    doneOld.st, doneRecent.st, pendingOld.st,
    env.DB.prepare("UPDATE sessions SET expires_at=? WHERE user_id=?").bind(now - M - 1000, s.user.id)]);
  await run();
  const inv = (code: string) => env.DB.prepare("SELECT 1 FROM space_invites WHERE code=?").bind(code).first();
  expect(await inv(revoked.code)).toBeNull();
  expect(await inv(expired.code)).toBeNull();
  expect(await inv(live.code)).not.toBeNull();
  const dinv = (id: string) => env.DB.prepare("SELECT 1 FROM space_direct_invites WHERE id=?").bind(id).first();
  expect(await dinv(doneOld.id)).toBeNull();
  expect(await dinv(doneRecent.id)).not.toBeNull();
  expect(await dinv(pendingOld.id)).not.toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM sessions WHERE user_id=?").bind(s.user.id).first()).toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM sessions WHERE user_id=?").bind(o.user.id).first()).not.toBeNull();
});
