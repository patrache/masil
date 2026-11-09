// v0.1.3 A10.3 (P1 Task 3): PUT /v1/leet/daily/{date} — facts reported by clients (first report wins, a different
// report only counts a mismatch) and one #daily root per channel per day, made by the reporter, even when two
// members report at once (leet_daily_roots claim).
import { describe, expect, it } from "vitest";
import { utcDate } from "../../src/lib/leet";
import { api, makeSpace, signup, type TestUser } from "../helpers";

const DAY = 86_400_000;
const today = () => utcDate(Date.now());
const facts = {
  frontend_id: "1606",
  slug: "find-servers-that-handled-most-number-of-requests",
  title: "Find Servers That Handled Most Number of Requests",
  difficulty: "Hard",
  tags: ["Array", "Heap (Priority Queue)", "Ordered Set"],
  is_paid_only: false,
};
const report = (u: TestUser, body: unknown = facts, date = today()) => api(u.token).put<any>(`/v1/leet/daily/${date}`, body);

async function dailySpace(owner: TestUser, members: TestUser[] = []): Promise<{ spaceId: string; channelId: string }> {
  const { space } = await makeSpace(owner, members);
  const r = await api(owner.token).put<any>(`/v1/spaces/${space.id}/leet`, { enabled: true });
  if (r.status !== 200) throw new Error(`leet enable failed: ${r.status} ${JSON.stringify(r.body)}`);
  return { spaceId: space.id, channelId: r.body.settings.channel_id };
}

async function leetCards(token: string, channelId: string, type: string): Promise<any[]> {
  const r = await api(token).get<any>(`/v1/channels/${channelId}/messages?limit=100`);
  return (r.body.items as any[]).filter((m) => m.kind === "card" && m.card?.type === type);
}

// the facts table is shared by every test in this file: each test uses its own day only where it must
describe("PUT /v1/leet/daily/{date}", () => {
  it("first report is stored; the same again is kept; a different one only counts a mismatch", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    const date = utcDate(Date.now() - DAY); // yesterday: this test owns that day's facts
    const r1 = await report(a, facts, date);
    expect(r1.status).toBe(200);
    expect(r1.body.daily).toMatchObject({ ...facts, date, mismatch_count: 0, first_reporter: { id: a.user.id, handle: a.handle } });
    expect(typeof r1.body.daily.first_reported_at).toBe("string");
    expect(r1.body.roots).toEqual([]);
    const r2 = await report(b, facts, date);
    expect(r2.body.daily).toMatchObject({ slug: facts.slug, mismatch_count: 0, first_reporter: { id: a.user.id } });
    expect(r2.body.daily.first_reported_at).toBe(r1.body.daily.first_reported_at);
    const r3 = await report(c, { ...facts, slug: "two-sum", frontend_id: "1" }, date);
    expect(r3.status).toBe(200);
    expect(r3.body.daily).toMatchObject({ slug: facts.slug, frontend_id: "1606", mismatch_count: 1, first_reporter: { id: a.user.id } });
    const r4 = await report(c, { ...facts, tags: ["Array"] }, date);
    expect(r4.body.daily.mismatch_count).toBe(2);
  });

  it("date must be today or yesterday (UTC) and a real day; body is strict", async () => {
    const a = await signup();
    for (const d of [utcDate(Date.now() - 2 * DAY), utcDate(Date.now() + DAY), "2026-13-01", "20261109", "x"]) {
      const r = await report(a, facts, d);
      expect(r.status).toBe(400);
      expect(r.body.error.code).toBe("invalid_request");
      expect(r.body.error.details.fields).toEqual({ date: "invalid" });
    }
    const bad = await report(a, { ...facts, slug: "../../etc" });
    expect(bad.status).toBe(400);
    expect(bad.body.error.details.fields).toEqual({ slug: "invalid" });
    expect((await report(a, { ...facts, difficulty: "Insane" })).status).toBe(400);
    expect((await report(a, { ...facts, url: "https://evil.example/" })).status).toBe(400);
    expect((await api().put<any>(`/v1/leet/daily/${today()}`, facts)).status).toBe(401);
  });

  it("roots only in the reporter's spaces with #daily on: kind card, leet.daily, body = the link", async () => {
    const [a, o] = await Promise.all([signup(), signup()]);
    const on = await dailySpace(o, [a]);
    const { space: off } = await makeSpace(o, [a]);
    const r = await report(a);
    expect(r.status).toBe(200);
    expect(r.body.roots).toHaveLength(1);
    expect(r.body.roots[0]).toMatchObject({ space_id: on.spaceId, channel_id: on.channelId });
    const msg = await api(a.token).get<any>(`/v1/messages/${r.body.roots[0].message_id}`);
    expect(msg.body.message).toMatchObject({
      kind: "card",
      author: { id: a.user.id },
      body: `https://leetcode.com/problems/${r.body.daily.slug}/`,
      card: { type: "leet.daily", v: 1, data: { date: today(), slug: r.body.daily.slug, frontend_id: r.body.daily.frontend_id } },
      thread_root_id: null,
    });
    expect(Object.keys(msg.body.message.card.data).sort()).toEqual(["date", "difficulty", "frontend_id", "is_paid_only", "slug", "tags", "title"]);
    const offChannels = (await api(o.token).get<any>(`/v1/spaces/${off.id}/channels`)).body.items as any[];
    for (const ch of offChannels) expect(await leetCards(o.token, ch.id, "leet.daily")).toHaveLength(0);
    // a second report the same day: the same root, no new message
    const again = await report(o);
    expect(again.body.roots).toEqual(r.body.roots);
    expect(await leetCards(o.token, on.channelId, "leet.daily")).toHaveLength(1);
  });

  it("two members reporting at once make exactly one root", async () => {
    const [o, a, b] = await Promise.all([signup(), signup(), signup()]);
    const { channelId } = await dailySpace(o, [a, b]);
    const [ra, rb] = await Promise.all([report(a), report(b)]);
    expect(ra.status).toBe(200);
    expect(rb.status).toBe(200);
    const cards = await leetCards(o.token, channelId, "leet.daily");
    expect(cards).toHaveLength(1);
    const ids = [...ra.body.roots, ...rb.body.roots].map((x: any) => x.message_id);
    for (const id of ids) expect(id).toBe(cards[0].id);
  });

  it("the root cannot be edited, takes reactions and thread replies", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    await dailySpace(o, [a]);
    const r = await report(a);
    const id = r.body.roots[0].message_id;
    const p = await api(o.token).patch<any>(`/v1/messages/${id}`, { body: "changed" });
    expect(p.status).toBeGreaterThanOrEqual(400);
    expect(p.status).toBeLessThan(500);
    const pa = await api(a.token).patch<any>(`/v1/messages/${id}`, { body: "changed" });
    expect(pa.status).toBe(400);
    expect((await api(o.token).put(`/v1/messages/${id}/reactions/${encodeURIComponent("🔥")}`)).status).toBe(204);
    expect((await api(o.token).get<any>(`/v1/messages/${id}`)).body.message.reactions).toEqual([{ emoji: "🔥", count: 1, me: true }]);
    const reply = await api(o.token).post<any>(`/v1/messages/${id}/thread`, { client_nonce: crypto.randomUUID(), body: "good luck" });
    expect(reply.status).toBe(201);
  });

  it("a space whose #daily channel was deleted is skipped", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const live = await dailySpace(o, [a]);
    const gone = await dailySpace(o, [a]);
    expect((await api(o.token).del(`/v1/channels/${gone.channelId}`)).status).toBe(204);
    const r = await report(a);
    expect(r.body.roots.map((x: any) => x.space_id)).toEqual([live.spaceId]);
  });

  it("the stored facts make the card, not a differing reporter's", async () => {
    const [o, a, b] = await Promise.all([signup(), signup(), signup()]);
    const first = await report(a); // today's facts are fixed by whoever reported first in this file
    const stored = first.body.daily;
    const { channelId } = await dailySpace(o, [b]);
    const r = await report(b, { ...facts, slug: "two-sum", frontend_id: "1", title: "Two Sum", difficulty: "Easy" });
    expect(r.body.roots).toHaveLength(1);
    const [card] = await leetCards(o.token, channelId, "leet.daily");
    expect(card.card.data).toMatchObject({ slug: stored.slug, frontend_id: stored.frontend_id, title: stored.title });
    expect(card.body).toBe(`https://leetcode.com/problems/${stored.slug}/`);
  });

  it("a root whose message was deleted is made again", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const { channelId } = await dailySpace(o, [a]);
    const r = await report(a);
    expect((await api(o.token).del(`/v1/messages/${r.body.roots[0].message_id}`)).status).toBe(204);
    const again = await report(a);
    expect(again.body.roots).toHaveLength(1);
    expect(again.body.roots[0].message_id).not.toBe(r.body.roots[0].message_id);
    expect(await leetCards(o.token, channelId, "leet.daily")).toHaveLength(1);
  });
});
