// P3 Task 26 (docs/contract/arch.md 3): the arch.design and arch.sim card data schemas (server/src/lib/cards/arch.ts).
// The server checks shape and size only; the client re-checks everything before drawing.
import { describe, expect, it } from "vitest";
import { CARD_DATA_MAX, validateCard } from "../../src/lib/cards/index";
import { ApiError } from "../../src/lib/errors";
import { api, makeSpace, signup } from "../helpers";

const nonce = () => crypto.randomUUID();
const fieldsOf = (r: { body: any }) => r.body.error?.details?.fields;

/** validateCard's field errors for a user send ({} = accepted) — the long lists skip the message rate limiter. */
function check(type: string, data: unknown, v = 1): Record<string, string> {
  try {
    validateCard({ type, v, data }, false);
    return {};
  } catch (e) {
    if (e instanceof ApiError) return (e.details as { fields: Record<string, string> }).fields;
    throw e;
  }
}
const ULID = "01JA0000000000000000000000";

const mini = ["┌결제 API┐   ┌주 DB─┐", "│▊   28%◆━━▶│   15%│", "└────────┘   └──────┘"];
const design = { schema_version: 1, title: "결제 v3", seed: "42", duration_s: 300, nodes: [], edges: [], boxes: [], faults: [] };

function designData(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    title: "결제 시스템 v3",
    space_id: null,
    doc_id: null,
    version_no: null,
    node_count: 12,
    cost_usd_month: 6359,
    price_asof: "2026-11",
    engine_version: 1,
    mini,
    design,
    ...over,
  };
}

function simData(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    title: "결제 시스템 v3",
    space_id: ULID,
    doc_id: ULID,
    version_no: 3,
    seed: "18446744073709551615",
    engine_version: 1,
    max_rps: 1200,
    read_permille: 800,
    p50_ms: 20,
    p99_ms: 156,
    error_permille: 0,
    cost_usd_month: 6359,
    price_asof: "2026-11",
    bottleneck: { target_label: "결제 API", limit: "cpu", util_permille: 280 },
    challenge: null,
    design: null,
    ...over,
  };
}

async function sender() {
  const o = await signup();
  const { generalChannelId: ch } = await makeSpace(o);
  return (type: string, data: unknown, v = 1) =>
    api(o.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: nonce(), kind: "card", body: "📐 결제 시스템 v3", card: { type, v, data } });
}

/** designData with design.pad grown until JSON.stringify(data) is exactly n UTF-8 bytes. */
function sized(n: number): Record<string, unknown> {
  const d = designData({ design: { ...design, pad: "" } });
  const len = new TextEncoder().encode(JSON.stringify(d)).byteLength;
  (d.design as Record<string, unknown>).pad = "x".repeat(n - len);
  return d;
}

describe("arch cards", () => {
  it("arch.design and arch.sim accepted with valid data; 16KB+1 → 400 fields.card.data too_long", async () => {
    const send = await sender();
    const a = await send("arch.design", designData());
    expect(a.status).toBe(201);
    expect(a.body.message.card).toEqual({ type: "arch.design", v: 1, data: designData() });
    expect((await send("arch.design", designData({ space_id: ULID, doc_id: ULID, version_no: 4, design: null }))).status).toBe(201);
    expect((await send("arch.design", designData({ mini: [] }))).status).toBe(201);
    const s = await send("arch.sim", simData());
    expect(s.status).toBe(201);
    expect((await send("arch.sim", simData({ space_id: null, doc_id: null, version_no: null, bottleneck: null, design }))).status).toBe(201);
    expect((await send("arch.sim", simData({ challenge: { date: "2026-11-09", scenario: "ticket_rush", score: 9100 } }))).status).toBe(201);
    // the size check sees the whole data
    expect(new TextEncoder().encode(JSON.stringify(sized(CARD_DATA_MAX))).byteLength).toBe(CARD_DATA_MAX);
    expect((await send("arch.design", sized(CARD_DATA_MAX))).status).toBe(201);
    expect(fieldsOf(await send("arch.design", sized(CARD_DATA_MAX + 1)))).toEqual({ "card.data": "too_long" });
  });

  it("mini line wider than 60 cells or with ESC → 400; design present together with doc_id → 400", async () => {
    const send = await sender();
    // through the API once each way
    expect(fieldsOf(await send("arch.design", designData({ mini: ["x".repeat(61)] })))).toEqual({ "card.data": "invalid" });
    expect(fieldsOf(await send("arch.design", designData({ mini: ["ok \u001b[31mred"] })))).toEqual({ "card.data": "invalid" });
    expect(fieldsOf(await send("arch.design", designData({ space_id: ULID, doc_id: ULID, design })))).toEqual({ "card.data": "invalid" });
    const bad = (type: string, data: unknown) => expect(check(type, data)).toEqual({ "card.data": "invalid" });
    // 60 cells pass: 60 ASCII or 30 Hangul; 61 / 31 do not
    expect(check("arch.design", designData({ mini: ["x".repeat(60), "가".repeat(30), "┌결제 API×3┐ ▊ 28% ⚠"] }))).toEqual({});
    bad("arch.design", designData({ mini: ["가".repeat(31)] }));
    bad("arch.design", designData({ mini: ["ok \u202Eevil"] }));
    bad("arch.design", designData({ mini: Array(9).fill("x") }));
    bad("arch.design", designData({ mini: [7] }));
    bad("arch.design", designData({ doc_id: ULID })); // doc_id needs its space_id
    bad("arch.design", designData({ space_id: "nope" }));
    bad("arch.design", designData({ title: "" }));
    bad("arch.design", designData({ title: "가".repeat(81) }));
    bad("arch.design", designData({ title: "a\u0007b" }));
    bad("arch.design", designData({ node_count: 301 }));
    bad("arch.design", designData({ cost_usd_month: -1 }));
    bad("arch.design", designData({ cost_usd_month: 1.5 }));
    bad("arch.design", designData({ price_asof: "2026-13" }));
    bad("arch.design", designData({ engine_version: 0 }));
    bad("arch.design", designData({ design: { schema_version: 2 } }));
    bad("arch.design", designData({ design: [] }));
    bad("arch.design", designData({ extra: 1 }));
    const { title: _t, ...noTitle } = designData();
    bad("arch.design", noTitle);
    expect(check("arch.sim", simData())).toEqual({});
    bad("arch.sim", simData({ seed: "18446744073709551616" })); // 2^64
    bad("arch.sim", simData({ seed: "-1" }));
    bad("arch.sim", simData({ seed: "007" }));
    bad("arch.sim", simData({ read_permille: 1001 }));
    bad("arch.sim", simData({ bottleneck: { target_label: "x", limit: "gpu", util_permille: 1 } }));
    bad("arch.sim", simData({ bottleneck: { target_label: "\u202Ex", limit: "cpu", util_permille: 1 } }));
    bad("arch.sim", simData({ challenge: { date: "2026-11-9", scenario: "x", score: 1 } }));
    bad("arch.sim", simData({ design }));
    bad("arch.sim", simData({ mini }));
  });

  it("unknown future v (2) → 400", async () => {
    const send = await sender();
    expect(fieldsOf(await send("arch.design", designData(), 2))).toEqual({ "card.v": "invalid" });
    expect(check("arch.sim", simData(), 2)).toEqual({ "card.v": "invalid" });
  });
});
