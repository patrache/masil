// v0.1.3 A10.3 (P1 Task 5): GET /v1/leet/me (my streaks, last 30 days, my #daily spaces), the space board (only current,
// unsuspended members who left a solve card in that space's #daily — acting is consenting) and the friends board
// (streaks only, of friends whose leet_share_with_friends is on, and me).
import { env } from "cloudflare:test";
import { describe, expect, it } from "vitest";
import { utcDate } from "../../src/lib/leet";
import { api, makeFriends, makeSpace, signup, type TestUser } from "../helpers";

const DAY = 86_400_000;
const today = () => utcDate(Date.now());
const yesterday = () => utcDate(Date.now() - DAY);
const facts = {
  frontend_id: "1606",
  slug: "find-servers-that-handled-most-number-of-requests",
  title: "Find Servers That Handled Most Number of Requests",
  difficulty: "Hard",
  tags: ["Array"],
  is_paid_only: false,
};

async function dailySpace(owner: TestUser, members: TestUser[] = []): Promise<{ spaceId: string; channelId: string }> {
  const { space } = await makeSpace(owner, members);
  const r = await api(owner.token).put<any>(`/v1/spaces/${space.id}/leet`, { enabled: true });
  if (r.status !== 200) throw new Error(`leet enable failed: ${r.status}`);
  return { spaceId: space.id, channelId: r.body.settings.channel_id };
}

async function report(u: TestUser): Promise<void> {
  for (const d of [today(), yesterday()]) {
    const r = await api(u.token).put<any>(`/v1/leet/daily/${d}`, facts);
    if (r.status !== 200) throw new Error(`report failed: ${r.status}`);
  }
}

async function solve(u: TestUser, date: string, share: string[] = []): Promise<void> {
  const r = await api(u.token).put<any>(`/v1/leet/solves/${date}`, { status: "accepted", slug: facts.slug, lang: "golang", frontend_id: "1606", share_space_ids: share });
  if (r.status !== 200) throw new Error(`solve failed: ${r.status} ${JSON.stringify(r.body)}`);
}

/** Puts a counted solve on `date` straight into D1 (the API only takes today / yesterday). */
async function seedSolve(u: TestUser, date: string): Promise<void> {
  const at = Date.parse(`${date}T12:00:00.000Z`);
  await env.DB.prepare("INSERT INTO leet_solves (user_id, date, slug, lang, passed_at) VALUES (?1, ?2, 'two-sum', 'golang', ?3)").bind(u.user.id, date, at).run();
}

const board = (u: TestUser, spaceId: string, period = "all") => api(u.token).get<any>(`/v1/spaces/${spaceId}/leet/board?period=${period}`);

describe("leet boards", () => {
  it("GET /v1/leet/me: streaks, recent newest first, my #daily spaces", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const s1 = await dailySpace(o, [a]);
    await makeSpace(o, [a]); // #daily off: not listed
    await report(o);
    for (let i = 2; i <= 4; i++) await seedSolve(a, utcDate(Date.now() - i * DAY));
    await seedSolve(a, utcDate(Date.now() - 40 * DAY)); // outside the 30 days
    await solve(a, today());
    const r = await api(a.token).get<any>("/v1/leet/me");
    expect(r.status).toBe(200);
    // today + 2,3,4 days ago (yesterday missing): current 1, best 3
    expect(r.body).toMatchObject({ current: 1, best: 3 });
    expect(r.body.recent.map((x: any) => x.date)).toEqual([0, 2, 3, 4].map((i) => utcDate(Date.now() - i * DAY)));
    expect(r.body.recent[0]).toMatchObject({ slug: facts.slug, lang: "golang", counts_for_streak: true });
    expect(r.body.daily_spaces).toEqual([{ space_id: s1.spaceId, channel_id: s1.channelId }]);
    const empty = await api(o.token).get<any>("/v1/leet/me");
    expect(empty.body).toMatchObject({ current: 0, best: 0, recent: [] });
  });

  it("space board: only card-leavers who are current, unsuspended members; order; periods", async () => {
    const [o, a, b, c, d, gone, sus] = await Promise.all([signup(), signup(), signup(), signup(), signup(), signup(), signup()]);
    const s = await dailySpace(o, [a, b, c, d, gone, sus]);
    await report(o);
    await solve(a, yesterday(), [s.spaceId]);
    await solve(a, today(), [s.spaceId]);
    await solve(b, today(), [s.spaceId]);
    await seedSolve(b, utcDate(Date.now() - 2 * DAY)); // b: streak via records, but only 1 card here
    await solve(c, today()); // (a) a record without a card: not on the board
    await solve(gone, today(), [s.spaceId]);
    await solve(sus, today(), [s.spaceId]);
    expect((await api(gone.token).del(`/v1/spaces/${s.spaceId}/members/@me`)).status).toBe(204); // (b)
    await env.DB.prepare("UPDATE users SET suspended_at = ?1, suspended_until = NULL WHERE id = ?2").bind(Date.now(), sus.user.id).run(); // (c)
    const r = await board(d, s.spaceId);
    expect(r.status).toBe(200);
    expect(r.body.period).toBe("all");
    // a: 2 card days, but yesterday's record came in today (no grace day) -> streak 1;
    // b: 1 card day (records today + 2 days ago, yesterday missing -> streak 1)
    expect(r.body.items.map((x: any) => [x.user.id, x.solved, x.streak])).toEqual([
      [a.user.id, 2, 1],
      [b.user.id, 1, 1],
    ]);
    expect(r.body.items[0].user).toMatchObject({ handle: a.handle });
    for (const p of ["week", "month"]) {
      const w = await board(d, s.spaceId, p);
      expect(w.status).toBe(200);
      expect(w.body.period).toBe(p);
    }
    // (d) bad period, (f) not a member
    const bad = await board(d, s.spaceId, "bad");
    expect(bad.status).toBe(400);
    expect(bad.body.error.details.fields).toEqual({ period: "invalid" });
    expect((await board(gone, s.spaceId)).status).toBe(404);
    // default period = week
    expect((await api(d.token).get<any>(`/v1/spaces/${s.spaceId}/leet/board`)).body.period).toBe("week");
  });

  it("space board: ties by streak then handle; a space without #daily has an empty board", async () => {
    const [o, a, b] = await Promise.all([signup({ handle: undefined }), signup(), signup()]);
    const s = await dailySpace(o, [a, b]);
    await report(o);
    await solve(a, today(), [s.spaceId]);
    await solve(b, today(), [s.spaceId]);
    const r = await board(o, s.spaceId);
    const expected = [a, b].sort((x, y) => (x.handle < y.handle ? -1 : 1)).map((u) => u.user.id);
    expect(r.body.items.map((x: any) => x.user.id)).toEqual(expected);
    const { space: plain } = await makeSpace(o);
    expect((await board(o, plain.id)).body).toEqual({ period: "all", items: [] });
  });

  it("friends board: friends sharing (default on) + me; the setting turns a friend off", async () => {
    const [me, f1, f2, stranger] = await Promise.all([signup(), signup(), signup(), signup()]);
    await makeFriends(me, f1);
    await makeFriends(me, f2);
    await seedSolve(f1, today());
    await seedSolve(f1, yesterday());
    await seedSolve(stranger, today());
    const s = await api(f1.token).get<any>("/v1/me/settings");
    expect(s.body.settings.leet_share_with_friends).toBe(true); // default
    const r = await api(me.token).get<any>("/v1/leet/friends/board");
    expect(r.status).toBe(200);
    expect(r.body.items.map((x: any) => x.user.id).sort()).toEqual([me.user.id, f1.user.id, f2.user.id].sort());
    expect(r.body.items[0]).toMatchObject({ user: { id: f1.user.id }, current: 2, best: 2 });
    expect(Object.keys(r.body.items[0]).sort()).toEqual(["best", "current", "user"]);
    // (e) f1 stops sharing
    const p = await api(f1.token).patch<any>("/v1/me/settings", { leet_share_with_friends: false });
    expect(p.status).toBe(200);
    expect(p.body.settings.leet_share_with_friends).toBe(false);
    const r2 = await api(me.token).get<any>("/v1/leet/friends/board");
    expect(r2.body.items.map((x: any) => x.user.id).sort()).toEqual([me.user.id, f2.user.id].sort());
    // my own board still lists me even when I do not share
    expect((await api(f1.token).get<any>("/v1/leet/friends/board")).body.items.map((x: any) => x.user.id)).toContain(f1.user.id);
    expect((await api(f1.token).patch<any>("/v1/me/settings", { leet_share_with_friends: "no" })).status).toBe(400);
  });
});
