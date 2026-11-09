// v0.1.3 C3.11 / api.md 2 (카드 type 표): api.result and api.run data schemas — what the API test tool posts.
// The server checks shape and size only; secrets are masked by the client before the card leaves (C3.10).
import { describe, it, expect } from "vitest";
import { api, makeSpace, signup } from "../helpers";

const nonce = () => crypto.randomUUID();
const fieldsOf = (r: { body: any }) => r.body.error.details.fields;

const RESULT = {
  method: "POST",
  url: "{{base_url}}/v1/payments",
  status: 201,
  time_ms: 142,
  size: 98,
  asserts: { passed: 4, total: 4 },
  env: "staging",
  ref: { collection: "Payments", request_id: "r-pay", doc_id: "01JD00000000000000000APIC1" },
  req_headers: [
    ["Authorization", "•••"],
    ["Content-Type", "application/json"],
  ],
  body_head: ["{", '  "id": "pay_1"', "}"],
};

const RUN = {
  collection: "Payments",
  requests: 6,
  passed: 2,
  failed: 4,
  time_ms: 1500,
  failures: [
    { name: "a", method: "GET", status: 500, reason: "status" },
    { name: "b", method: "POST", status: 0, reason: "network" },
  ],
  ref: { doc_id: "01JD00000000000000000APIC1" },
};

describe("api cards", () => {
  it("api.result / api.run with valid data -> 201, stored as sent", async () => {
    const o = await signup();
    const { generalChannelId: ch } = await makeSpace(o);
    const send = (card: object, body = "🧪 POST /v1/payments · 201 · 142ms · ✓4/4") =>
      api(o.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: nonce(), kind: "card", body, card });
    const r = await send({ type: "api.result", v: 1, data: RESULT });
    expect(r.status).toBe(201);
    expect(r.body.message.card).toEqual({ type: "api.result", v: 1, data: RESULT });
    // the summary alone (options off), a personal collection (no doc_id)
    const { req_headers: _h, body_head: _b, env: _e, ...bare } = RESULT;
    expect((await send({ type: "api.result", v: 1, data: { ...bare, ref: { collection: "시험", request_id: "pay" } } })).status).toBe(201);
    expect((await send({ type: "api.run", v: 1, data: RUN }, "🧪 Payments 실행 · 6개 · ✓2 ✕4 · 1500ms")).status).toBe(201);
    expect((await send({ type: "api.run", v: 1, data: { ...RUN, failures: [], ref: {} } })).status).toBe(201);
  });

  it("a missing field, a wrong type, an unknown key, over the list limits -> 400 card.data invalid", async () => {
    const o = await signup();
    const { generalChannelId: ch } = await makeSpace(o);
    const send = (card: object) => api(o.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: nonce(), kind: "card", body: "x", card });
    const { status: _s, ...noStatus } = RESULT;
    const bad: object[] = [
      { type: "api.result", v: 1, data: noStatus },
      { type: "api.result", v: 1, data: { ...RESULT, status: "201" } },
      { type: "api.result", v: 1, data: { ...RESULT, ref: { collection: "c" } } },
      { type: "api.result", v: 1, data: { ...RESULT, extra: 1 } },
      { type: "api.result", v: 1, data: { ...RESULT, req_headers: Array.from({ length: 21 }, () => ["a", "b"]) } },
      { type: "api.result", v: 1, data: { ...RESULT, body_head: Array.from({ length: 21 }, () => "x") } },
      { type: "api.result", v: 1, data: { ...RESULT, body_head: ["x".repeat(201)] } },
      { type: "api.result", v: 1, data: { ...RESULT, req_headers: [["only-name"]] } },
      { type: "api.run", v: 1, data: { ...RUN, failures: [...RUN.failures, ...RUN.failures] } },
      { type: "api.run", v: 1, data: { ...RUN, passed: -1 } },
      { type: "api.run", v: 1, data: { ...RUN, ref: undefined } },
    ];
    for (const card of bad) {
      const r = await send(card);
      expect(r.status, JSON.stringify(card)).toBe(400);
      expect(fieldsOf(r)).toEqual({ "card.data": "invalid" });
    }
  });

  it("no body -> 400 body required (common rule)", async () => {
    const o = await signup();
    const { generalChannelId: ch } = await makeSpace(o);
    const r = await api(o.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: nonce(), kind: "card", card: { type: "api.result", v: 1, data: RESULT } });
    expect(r.status).toBe(400);
    expect(fieldsOf(r)).toEqual({ body: "required" });
  });
});
