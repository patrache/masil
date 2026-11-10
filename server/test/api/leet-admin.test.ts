// v0.1.3 A10.3 (P1 Task 7): PUT /v1/admin/leet/daily/{date} — an operator fixes a day's stored facts. The facts are
// overwritten, mismatch_count goes back to 0, and every live #daily root of that day is rewritten to the new facts
// (card.data and the body link) with message.updated.
import { env } from "cloudflare:test";
import { describe, expect, it } from "vitest";
import { utcDate } from "../../src/lib/leet";
import { api, connectWs, makeAdmin, makeSpace, signup, type TestUser } from "../helpers";

const DAY = 86_400_000;
const today = () => utcDate(Date.now());
const wrong = {
  frontend_id: "1",
  slug: "two-sum",
  title: "Two Sum",
  difficulty: "Easy",
  tags: ["Array"],
  is_paid_only: false,
};
const right = {
  frontend_id: "1606",
  slug: "find-servers-that-handled-most-number-of-requests",
  title: "Find Servers That Handled Most Number of Requests",
  difficulty: "Hard",
  tags: ["Array", "Ordered Set"],
  is_paid_only: false,
};
const fix = (u: TestUser, date: string, body: unknown = right) => api(u.token).put<any>(`/v1/admin/leet/daily/${date}`, body);

async function dailySpace(owner: TestUser, members: TestUser[] = []): Promise<{ spaceId: string; channelId: string }> {
  const { space } = await makeSpace(owner, members);
  const r = await api(owner.token).put<any>(`/v1/spaces/${space.id}/leet`, { enabled: true });
  if (r.status !== 200) throw new Error(`leet enable failed: ${r.status} ${JSON.stringify(r.body)}`);
  return { spaceId: space.id, channelId: r.body.settings.channel_id };
}

describe("PUT /v1/admin/leet/daily/{date}", () => {
  it("a normal user gets 403 not_admin and nothing changes", async () => {
    const u = await signup();
    const date = utcDate(Date.now() - 3 * DAY);
    const r = await fix(u, date);
    expect(r.status).toBe(403);
    expect(r.body.error.code).toBe("not_admin");
    expect(await env.DB.prepare("SELECT 1 FROM leet_daily WHERE date = ?1").bind(date).first()).toBeNull();
  });

  it("overwrites the facts, resets mismatch_count and rewrites every live root (data + body) with message.updated", async () => {
    const [adm, o, a, b] = await Promise.all([signup(), signup(), signup(), signup()]);
    await makeAdmin(adm);
    const s1 = await dailySpace(o, [a, b]);
    const s2 = await dailySpace(a);
    const date = today();
    // wrong facts reported first (stored), a later different report only counts a mismatch
    const first = await api(a.token).put<any>(`/v1/leet/daily/${date}`, wrong);
    expect(first.status).toBe(200);
    expect(first.body.roots).toHaveLength(2);
    await api(b.token).put<any>(`/v1/leet/daily/${date}`, right);
    const wo = await connectWs(o.token);
    const r = await fix(adm, date);
    expect(r.status).toBe(200);
    expect(r.body.daily).toMatchObject({ ...right, date, mismatch_count: 0, first_reporter: { id: a.user.id } });
    for (const root of first.body.roots as any[]) {
      const msg = (await api(a.token).get<any>(`/v1/messages/${root.message_id}`)).body.message;
      expect(msg.body).toBe(`https://leetcode.com/problems/${right.slug}/`);
      expect(msg.card).toMatchObject({ type: "leet.daily", v: 1, data: { ...right, date } });
    }
    const ev = await wo.next("message.updated", (d: any) => d.message.channel_id === s1.channelId);
    expect(ev.data.message.card.data.slug).toBe(right.slug);
    expect(ev.data.message.body).toBe(`https://leetcode.com/problems/${right.slug}/`);
    expect(s2.channelId).not.toBe(s1.channelId);
    // the fixed facts are now what a client report is compared with
    const again = await api(b.token).put<any>(`/v1/leet/daily/${date}`, right);
    expect(again.body.daily.mismatch_count).toBe(0);
  });

  it("a day nobody reported is stored with no first reporter; a deleted root is left alone", async () => {
    const [adm, o] = await Promise.all([signup(), signup()]);
    await makeAdmin(adm);
    const date = utcDate(Date.now() - 10 * DAY);
    const r = await fix(adm, date);
    expect(r.status).toBe(200);
    expect(r.body.daily).toMatchObject({ ...right, date, mismatch_count: 0, first_reporter: null });
    expect(typeof r.body.daily.first_reported_at).toBe("string");
    // yesterday: a root that was deleted is not rewritten (and does not fail the request)
    const { channelId } = await dailySpace(o);
    const y = utcDate(Date.now() - DAY);
    const rep = await api(o.token).put<any>(`/v1/leet/daily/${y}`, wrong);
    const rootId = rep.body.roots[0].message_id;
    expect((await api(o.token).raw(`/v1/messages/${rootId}`, { method: "DELETE" })).status).toBeLessThan(300);
    const r2 = await fix(adm, y);
    expect(r2.status).toBe(200);
    // a delete blanks the body; the card stays what it was (not rewritten)
    const row = await env.DB.prepare("SELECT deleted_at, card_json FROM messages WHERE id = ?1").bind(rootId).first<any>();
    expect(row.deleted_at).not.toBeNull();
    expect(row.card_json === null || !String(row.card_json).includes(right.slug)).toBe(true);
    expect(channelId).toBeTruthy();
  });

  it("date must be a real calendar day; the body is strict and checked like a report", async () => {
    const adm = await signup();
    await makeAdmin(adm);
    for (const d of ["2026-13-01", "20261109", "x", "2026-02-30"]) {
      const r = await fix(adm, d);
      expect(r.status).toBe(400);
      expect(r.body.error.details.fields).toEqual({ date: "invalid" });
    }
    const bad = await fix(adm, today(), { ...right, slug: "../x" });
    expect(bad.status).toBe(400);
    expect(bad.body.error.details.fields).toEqual({ slug: "invalid" });
    expect((await fix(adm, today(), { ...right, content: "<p>body</p>" })).status).toBe(400);
    expect((await fix(adm, today(), { ...right, frontend_id: "1234567" })).status).toBe(400);
  });
});
