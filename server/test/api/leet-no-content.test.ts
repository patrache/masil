// v0.1.3 A10.8 "D1 본문 0건" (P1 Task 3): the server stores no problem body. A report cannot carry one (strict body),
// and after a normal report no TEXT column of any table holds a sentence of the fixture problem's statement.
import { env } from "cloudflare:test";
import { describe, expect, it } from "vitest";
import { utcDate } from "../../src/lib/leet";
import { api, makeSpace, signup } from "../helpers";

const BODY_SENTENCE = "You have k servers numbered from 0 to k-1";
const MARKER = "ZXQ-BODY-MARKER";
const facts = {
  frontend_id: "1606",
  slug: "find-servers-that-handled-most-number-of-requests",
  title: "Find Servers That Handled Most Number of Requests",
  difficulty: "Hard",
  tags: ["Array"],
  is_paid_only: false,
};

async function textColumnHits(needle: string): Promise<string[]> {
  const tables = await env.DB.prepare(
    "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '_cf_%' AND name NOT LIKE '%_fts_%'",
  ).all<{ name: string }>();
  const hits: string[] = [];
  for (const { name } of tables.results) {
    const cols = await env.DB.prepare(`SELECT name, type FROM pragma_table_info(?1)`).bind(name).all<{ name: string; type: string }>();
    for (const col of cols.results) {
      if (col.type !== "" && !/TEXT/i.test(col.type)) continue;
      const q = `SELECT COUNT(*) AS n FROM "${name}" WHERE instr(CAST("${col.name}" AS TEXT), ?1) > 0`;
      const row = await env.DB.prepare(q).bind(needle).first<{ n: number }>();
      if ((row?.n ?? 0) > 0) hits.push(`${name}.${col.name}`);
    }
  }
  return hits;
}

describe("no problem body in D1", () => {
  it("a body field is refused; after a normal report no table holds the statement", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const { space } = await makeSpace(o, [a]);
    expect((await api(o.token).put(`/v1/spaces/${space.id}/leet`, { enabled: true })).status).toBe(200);
    const date = utcDate(Date.now());
    for (const extra of [
      { content: `<p>${BODY_SENTENCE}</p> ${MARKER}` },
      { body: MARKER },
      { html: MARKER },
      { code_snippets: [{ lang: "golang", code: MARKER }] },
      { example_testcases: MARKER },
    ]) {
      const r = await api(a.token).put<any>(`/v1/leet/daily/${date}`, { ...facts, ...extra });
      expect(r.status).toBe(400);
    }
    expect(await textColumnHits(MARKER)).toEqual([]);
    const ok = await api(a.token).put<any>(`/v1/leet/daily/${date}`, facts);
    expect(ok.status).toBe(200);
    expect(ok.body.roots).toHaveLength(1);
    // the scan really looks: the stored title is found
    expect(await textColumnHits(facts.title)).toContain("leet_daily.title");
    expect(await textColumnHits(BODY_SENTENCE)).toEqual([]);
    expect(await textColumnHits(MARKER)).toEqual([]);
  });
});
