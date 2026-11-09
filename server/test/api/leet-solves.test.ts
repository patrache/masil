// v0.1.3 A10.3 (P1 Task 4): PUT / DELETE /v1/leet/solves/{date} — my solve record (accepted only) and, per shared
// space, my leet.solve card under that day's #daily root (made on first record, rewritten on a re-PUT, deleted with
// the record). A solve recorded after the day's UTC midnight does not fill that day (JA26).
import { env } from "cloudflare:test";
import { describe, expect, it } from "vitest";
import { utcDate } from "../../src/lib/leet";
import { api, connectWs, inOneRateWindow, makeSpace, signup, type TestUser } from "../helpers";

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
const link = `https://leetcode.com/problems/${facts.slug}/`;
const solveBody = (extra: Record<string, unknown> = {}) => ({ status: "accepted", slug: facts.slug, lang: "golang", frontend_id: "1606", ...extra });
const putSolve = (u: TestUser, body: unknown, date = today()) => api(u.token).put<any>(`/v1/leet/solves/${date}`, body);

async function dailySpace(owner: TestUser, members: TestUser[] = []): Promise<{ spaceId: string; channelId: string }> {
  const { space } = await makeSpace(owner, members);
  const r = await api(owner.token).put<any>(`/v1/spaces/${space.id}/leet`, { enabled: true });
  if (r.status !== 200) throw new Error(`leet enable failed: ${r.status}`);
  return { spaceId: space.id, channelId: r.body.settings.channel_id };
}

async function reportBoth(u: TestUser): Promise<void> {
  for (const d of [today(), yesterday()]) {
    const r = await api(u.token).put<any>(`/v1/leet/daily/${d}`, facts);
    if (r.status !== 200) throw new Error(`report failed: ${r.status} ${JSON.stringify(r.body)}`);
  }
}

async function threadCards(token: string, rootId: string): Promise<any[]> {
  const r = await api(token).get<any>(`/v1/messages/${rootId}/thread?limit=100`);
  return (r.body.items as any[]).filter((m) => m.kind === "card" && m.card?.type === "leet.solve");
}

async function rootOf(token: string, channelId: string, date: string): Promise<string> {
  const r = await api(token).get<any>(`/v1/channels/${channelId}/messages?limit=100`);
  const root = (r.body.items as any[]).find((m) => m.card?.type === "leet.daily" && m.card.data.date === date);
  if (!root) throw new Error("no root");
  return root.id;
}

describe("PUT /v1/leet/solves/{date}", () => {
  it("first record: one row, counts for today's streak, my card in the shared space's root thread", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const { spaceId, channelId } = await dailySpace(o, [a]);
    await reportBoth(o);
    const r = await putSolve(a, solveBody({ runtime_percentile: 40, share_space_ids: [spaceId] }));
    expect(r.status).toBe(200);
    expect(r.body.solve).toMatchObject({ date: today(), slug: facts.slug, lang: "golang", runtime_percentile: 40, counts_for_streak: true });
    expect(typeof r.body.solve.passed_at).toBe("string");
    expect(r.body.cards).toHaveLength(1);
    expect(r.body.cards[0].space_id).toBe(spaceId);
    const rows = await env.DB.prepare("SELECT * FROM leet_solves WHERE user_id = ?1").bind(a.user.id).all<any>();
    expect(rows.results).toHaveLength(1);
    const root = await rootOf(o.token, channelId, today());
    const cards = await threadCards(o.token, root);
    expect(cards).toHaveLength(1);
    expect(cards[0]).toMatchObject({
      id: r.body.cards[0].message_id,
      author: { id: a.user.id },
      body: link,
      thread_root_id: root,
      card: { type: "leet.solve", v: 1, data: { date: today(), frontend_id: "1606", slug: facts.slug, lang: "golang", runtime_percentile: 40, code: null, spoiler: true } },
    });
  });

  it("re-PUT rewrites the same card (message.updated), keeps passed_at; code rides only in data", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const { spaceId, channelId } = await dailySpace(o, [a]);
    await reportBoth(o);
    const first = await putSolve(a, solveBody({ runtime_percentile: 40, share_space_ids: [spaceId] }));
    const wo = await connectWs(o.token);
    const again = await putSolve(a, solveBody({ runtime_percentile: 12, submission_id: "1234567890", share_space_ids: [spaceId], code: "func f() {}" }));
    expect(again.status).toBe(200);
    expect(again.body.cards).toEqual(first.body.cards);
    expect(again.body.solve.passed_at).toBe(first.body.solve.passed_at);
    expect(again.body.solve.runtime_percentile).toBe(12);
    const ev = await wo.next("message.updated", (d: any) => d.message.id === first.body.cards[0].message_id);
    expect(ev.data.message.card.data).toMatchObject({ runtime_percentile: 12, code: { lang: "golang", text: "func f() {}" }, spoiler: true });
    expect(ev.data.message.body).toBe(link);
    const root = await rootOf(o.token, channelId, today());
    expect(await threadCards(o.token, root)).toHaveLength(1);
    const row = await env.DB.prepare("SELECT submission_id FROM leet_solves WHERE user_id = ?1").bind(a.user.id).first<any>();
    expect(row.submission_id).toBe("1234567890");
  });

  it("validation: status, code length, card data size, submission id, share list, date", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const { spaceId } = await dailySpace(o, [a]);
    await reportBoth(o);
    const bad = async (body: unknown, date = today()) => {
      const r = await putSolve(a, body, date);
      expect(r.status).toBe(400);
      return r.body.error.details.fields;
    };
    expect(await bad(solveBody({ status: "wrong_answer" }))).toEqual({ status: "invalid" });
    expect(await bad(solveBody({ code: "x".repeat(4001) }))).toEqual({ code: "too_long" });
    expect(await bad(solveBody({ code: "\u0001".repeat(4000), share_space_ids: [spaceId] }))).toEqual({ "card.data": "too_long" });
    expect(await bad(solveBody({ submission_id: "12a" }))).toEqual({ submission_id: "invalid" });
    expect(await bad(solveBody({ slug: "Two Sum" }))).toEqual({ slug: "invalid" });
    expect(await bad(solveBody({ share_space_ids: Array.from({ length: 11 }, () => spaceId) }))).toEqual({ share_space_ids: "invalid" });
    expect(await bad(solveBody({ runtime_percentile: 101 }))).toEqual({ runtime_percentile: "invalid" });
    expect(await bad(solveBody({ extra: 1 }))).toEqual({ request: "invalid" });
    expect(await bad(solveBody(), utcDate(Date.now() - 2 * DAY))).toEqual({ date: "invalid" });
    // nothing was written by any of them
    const rows = await env.DB.prepare("SELECT COUNT(*) AS n FROM leet_solves WHERE user_id = ?1").bind(a.user.id).first<any>();
    expect(rows.n).toBe(0);
  });

  it("a shared space with #daily off or that I am not in -> 404 and nothing is written", async () => {
    const [o, a, x] = await Promise.all([signup(), signup(), signup()]);
    const on = await dailySpace(o, [a]);
    const { space: off } = await makeSpace(o, [a]);
    const notMine = await dailySpace(x);
    await reportBoth(o);
    for (const ids of [[on.spaceId, off.id], [notMine.spaceId]]) {
      const r = await putSolve(a, solveBody({ share_space_ids: ids }));
      expect(r.status).toBe(404);
    }
    const rows = await env.DB.prepare("SELECT COUNT(*) AS n FROM leet_solves WHERE user_id = ?1").bind(a.user.id).first<any>();
    expect(rows.n).toBe(0);
    const root = await rootOf(o.token, on.channelId, today());
    expect(await threadCards(o.token, root)).toHaveLength(0);
  });

  it("no root yet in the shared space: it is made (by me) and my card goes under it", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    await reportBoth(o); // o is in no #daily space yet: no root anywhere
    const { spaceId, channelId } = await dailySpace(o, [a]);
    const r = await putSolve(a, solveBody({ share_space_ids: [spaceId] }));
    expect(r.status).toBe(200);
    const root = await rootOf(o.token, channelId, today());
    const rootMsg = await api(o.token).get<any>(`/v1/messages/${root}`);
    expect(rootMsg.body.message.author.id).toBe(a.user.id);
    expect(await threadCards(o.token, root)).toHaveLength(1);
  });

  it("without a share list only the record is written", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const { channelId } = await dailySpace(o, [a]);
    await reportBoth(o);
    const r = await putSolve(a, solveBody());
    expect(r.status).toBe(200);
    expect(r.body.cards).toEqual([]);
    expect(await threadCards(o.token, await rootOf(o.token, channelId, today()))).toHaveLength(0);
  });

  it("solves after midnight do not fill yesterday", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const { spaceId, channelId } = await dailySpace(o, [a]);
    await reportBoth(o);
    const r = await putSolve(a, solveBody({ share_space_ids: [spaceId] }), yesterday());
    expect(r.status).toBe(200);
    expect(r.body.solve).toMatchObject({ date: yesterday(), counts_for_streak: false });
    // the card sits in yesterday's root thread
    const root = await rootOf(o.token, channelId, yesterday());
    expect((await threadCards(o.token, root))[0].card.data.date).toBe(yesterday());
  });

  it("DELETE removes the record and my cards (message.deleted); 204", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const s1 = await dailySpace(o, [a]);
    const s2 = await dailySpace(o, [a]);
    await reportBoth(o);
    const r = await putSolve(a, solveBody({ share_space_ids: [s1.spaceId, s2.spaceId] }));
    expect(r.body.cards).toHaveLength(2);
    const wo = await connectWs(o.token);
    const d = await api(a.token).del<any>(`/v1/leet/solves/${today()}`);
    expect(d.status).toBe(204);
    for (const card of r.body.cards) {
      const ev = await wo.next("message.deleted", (x: any) => x.message_id === card.message_id);
      expect(ev.data.thread_root_id).not.toBeNull();
      expect((await api(o.token).get<any>(`/v1/messages/${card.message_id}`)).status).toBe(404);
    }
    const rows = await env.DB.prepare("SELECT COUNT(*) AS n FROM leet_solves WHERE user_id = ?1").bind(a.user.id).first<any>();
    expect(rows.n).toBe(0);
    expect(await threadCards(o.token, await rootOf(o.token, s1.channelId, today()))).toHaveLength(0);
    // nothing left: still 204; a bad date 400
    expect((await api(a.token).del(`/v1/leet/solves/${today()}`)).status).toBe(204);
    expect((await api(a.token).del(`/v1/leet/solves/2026-02-30`)).status).toBe(400);
  });

  // review: the RL_MESSAGE check runs before anything is stored (api.md 5.25 "checks first, writes nothing")
  it("RL_MESSAGE used up: 429 and no record, no card", async () => {
    await inOneRateWindow(10, async () => {
      const [o, a] = await Promise.all([signup(), signup()]);
      const { spaceId, channelId } = await dailySpace(o, [a]);
      await reportBoth(o);
      for (let i = 0; i < 20; i++) await env.RL_MESSAGE.limit({ key: `u:${a.user.id}` });
      const r = await putSolve(a, solveBody({ share_space_ids: [spaceId] }));
      expect(r.status).toBe(429);
      expect(r.body.error.details.limiter).toBe("RL_MESSAGE");
      const rows = await env.DB.prepare("SELECT COUNT(*) AS n FROM leet_solves WHERE user_id = ?1").bind(a.user.id).first<any>();
      expect(rows.n).toBe(0);
      expect(await threadCards(o.token, await rootOf(o.token, channelId, today()))).toHaveLength(0);
    });
  });

  // review: a card in a space I can no longer read (kicked) is still mine to delete; one card never stops the others
  it("DELETE after I was kicked from one shared space: 204, record and every card gone", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const s1 = await dailySpace(o, [a]);
    const s2 = await dailySpace(o, [a]);
    await reportBoth(o);
    const r = await putSolve(a, solveBody({ share_space_ids: [s1.spaceId, s2.spaceId] }));
    expect(r.body.cards).toHaveLength(2);
    expect((await api(o.token).del(`/v1/spaces/${s1.spaceId}/members/${a.user.id}`)).status).toBe(204);
    const d = await api(a.token).del<any>(`/v1/leet/solves/${today()}`);
    expect(d.status).toBe(204);
    const rows = await env.DB.prepare("SELECT COUNT(*) AS n FROM leet_solves WHERE user_id = ?1").bind(a.user.id).first<any>();
    expect(rows.n).toBe(0);
    for (const card of r.body.cards) {
      const m = await env.DB.prepare("SELECT deleted_at FROM messages WHERE id = ?1").bind(card.message_id).first<any>();
      expect(m.deleted_at).not.toBeNull();
    }
  });
});
