// v0.1.3 A10.3 (P1 Task 1): leet.daily / leet.solve are server-only card types — an ordinary send of a schema-valid
// card is refused (400 fields.card.type "not_allowed"), as a channel message and as a thread reply.
import { describe, expect, it } from "vitest";
import { api, makeSpace, signup } from "../helpers";

const nonce = () => crypto.randomUUID();
const fieldsOf = (r: { body: any }) => r.body.error.details.fields;

const daily = {
  date: "2026-11-09",
  frontend_id: "1606",
  slug: "find-servers-that-handled-most-number-of-requests",
  title: "Find Servers That Handled Most Number of Requests",
  difficulty: "Hard",
  tags: ["Array"],
  is_paid_only: false,
};
const solve = { date: "2026-11-09", frontend_id: "1606", slug: daily.slug, lang: "golang", runtime_percentile: null, code: null, spoiler: true };

describe("leet cards are server-only", () => {
  it("a member cannot send leet.daily or leet.solve (message or thread reply)", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { generalChannelId: ch } = await makeSpace(o, [m]);
    const body = `https://leetcode.com/problems/${daily.slug}/`;
    const d = await api(m.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: nonce(), kind: "card", body, card: { type: "leet.daily", v: 1, data: daily } });
    expect(d.status).toBe(400);
    expect(fieldsOf(d)).toEqual({ "card.type": "not_allowed" });
    const s = await api(m.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: nonce(), kind: "card", body, card: { type: "leet.solve", v: 1, data: solve } });
    expect(s.status).toBe(400);
    expect(fieldsOf(s)).toEqual({ "card.type": "not_allowed" });

    const root = await api(o.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: nonce(), body: "root" });
    expect(root.status).toBe(201);
    const r = await api(m.token).post<any>(`/v1/messages/${root.body.message.id}/thread`, { client_nonce: nonce(), kind: "card", body, card: { type: "leet.solve", v: 1, data: solve } });
    expect(r.status).toBe(400);
    expect(fieldsOf(r)).toEqual({ "card.type": "not_allowed" });
    // nothing was written
    const th = await api(o.token).get<any>(`/v1/messages/${root.body.message.id}/thread`);
    expect(th.body.items ?? []).toHaveLength(0);
  });
});
