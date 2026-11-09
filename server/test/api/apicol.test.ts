// docs/contract/collab.md 7: the apicol doc type — object kinds and fields, the request / body limits, secret values never
// stored with a secret flag, refusals leave nothing in the record. The server checks shape and size only (C3.12).
import { env, runInDurableObject } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { api, connectDoc, makeDoc, makeSpace, signup, type DocClient } from "../helpers";
import { DOC_TYPES } from "../../src/lib/collab-doctypes";
import { DocState, applyFrame } from "../../src/lib/collab-reducer";

const hub = (docId: string) => env.COLLAB_HUB.getByName(`doc:${docId}`);

let opNo = 0;
function sendOp(c: DocClient, ops: unknown[]): string {
  const id = `a${++opNo}`;
  c.send({ t: "op", client_op_id: id, base_seq: 0, ops });
  return id;
}
const ackOf = (c: DocClient, id: string) => c.next("ack", (f) => f.client_op_id === id);
const rejectOf = (c: DocClient, id: string) => c.next("reject", (f) => f.client_op_id === id);

async function apicolDoc(snapshot?: unknown) {
  const owner = await signup();
  const { space } = await makeSpace(owner);
  const doc = await makeDoc(owner, space.id, "apicol", snapshot);
  const a = await connectDoc(owner.token, doc.id);
  return { owner, space, doc, a };
}

const VALID = [
  { op: "put", id: "col", type: "col", fields: { name: "Payments", vars: [{ id: "v1", name: "token", secret: true }, { name: "base", value: "https://x" }] } },
  { op: "put", id: "folder:f1", type: "folder", fields: { name: "pay", parent: "", order: 0 } },
  {
    op: "put",
    id: "req:r1",
    type: "request",
    fields: {
      name: "Create",
      folder: "f1",
      order: 0,
      method: "POST",
      url: "{{base}}/v1/payments",
      body_kind: "json",
      body: '{"amount":1}',
      content_type: "",
      auth: { kind: "bearer", token: "{{token}}" },
      settings: { timeout_ms: 5000, no_follow: true },
    },
  },
  { op: "put", id: "row:h1", type: "row", fields: { req: "r1", list: "header", order: 0, key: "X-Trace", value: "a", off: false, extra: {} } },
  { op: "put", id: "row:a1", type: "row", fields: { req: "r1", list: "assert", order: 0, key: "status", value: "201", off: false, extra: { op: "eq" } } },
  { op: "put", id: "env:e1", type: "env", fields: { name: "staging", order: 0 } },
  { op: "put", id: "envvar:v2", type: "envvar", fields: { env: "e1", name: "token", secret: true, order: 0 } },
  { op: "put", id: "envvar:v3", type: "envvar", fields: { env: "e1", name: "base_url", secret: false, order: 1, value: "https://api.example.com" } },
];

describe("apicol doc type", () => {
  it("accepts the collab.md 7.1 objects, sets (null clears a field), del and undel", async () => {
    const { a } = await apicolDoc();
    await ackOf(a, sendOp(a, VALID));
    await ackOf(a, sendOp(a, [{ op: "set", id: "req:r1", field: "url", value: "{{base}}/v2" }]));
    await ackOf(a, sendOp(a, [{ op: "set", id: "row:h1", field: "extra", value: null }]));
    await ackOf(a, sendOp(a, [{ op: "del", id: "row:h1" }, { op: "undel", id: "row:h1" }]));
    // making a variable secret: the value goes first, then the flag
    await ackOf(a, sendOp(a, [{ op: "set", id: "envvar:v3", field: "value", value: null }, { op: "set", id: "envvar:v3", field: "secret", value: true }]));
  });

  it("unknown kind / field, id prefix and type apart, wrong JSON shape, col on another id -> invalid invalid_op", async () => {
    const { doc, a } = await apicolDoc();
    await ackOf(a, sendOp(a, VALID));
    const bad: unknown[][] = [
      [{ op: "put", id: "foo:1", type: "foo", fields: {} }],
      [{ op: "set", id: "foo:1", field: "name", value: "x" }],
      [{ op: "set", id: "req:r1", field: "evil", value: "x" }],
      [{ op: "put", id: "req:r2", type: "row", fields: {} }],
      [{ op: "put", id: "req:r3", type: "col", fields: {} }],
      [{ op: "put", id: "col", type: "request", fields: {} }],
      [{ op: "put", id: "req:", type: "request", fields: {} }],
      [{ op: "set", id: "req:r1", field: "order", value: "first" }],
      [{ op: "set", id: "req:r1", field: "body_kind", value: "xml" }],
      [{ op: "set", id: "row:h1", field: "list", value: "cookie" }],
      [{ op: "set", id: "req:r1", field: "auth", value: { kind: "bearer", token: 1 } }],
      [{ op: "set", id: "req:r1", field: "settings", value: { proxy: "x" } }],
      [{ op: "set", id: "col", field: "vars", value: [{ value: "no name" }] }],
      [{ op: "del", id: "nope" }],
    ];
    for (const ops of bad) {
      const id = sendOp(a, ops);
      expect(await rejectOf(a, id), JSON.stringify(ops)).toEqual({ t: "reject", client_op_id: id, code: "invalid", detail: "invalid_op" });
    }
    expect(await hub(doc.id).head()).toBe(1);
  });

  it("the 1,001st live request (put or undel) -> limit_exceeded limit_requests", async () => {
    const { doc, a } = await apicolDoc();
    const puts = (from: number, n: number) => Array.from({ length: n }, (_, i) => ({ op: "put", id: `req:${from + i}`, type: "request", fields: {} }));
    for (let i = 0; i < 5; i++) await ackOf(a, sendOp(a, puts(i * 200, 200))); // 200 ops per frame (collab.md 3.1)
    const over = sendOp(a, puts(1000, 1));
    expect(await rejectOf(a, over)).toEqual({ t: "reject", client_op_id: over, code: "limit_exceeded", detail: "limit_requests" });
    // a put on an existing request is no new one
    await ackOf(a, sendOp(a, [{ op: "put", id: "req:0", type: "request", fields: { name: "again" } }]));
    await ackOf(a, sendOp(a, [{ op: "del", id: "req:0" }]));
    await ackOf(a, sendOp(a, puts(1000, 1)));
    const undel = sendOp(a, [{ op: "undel", id: "req:0" }]);
    expect(await rejectOf(a, undel)).toEqual({ t: "reject", client_op_id: undel, code: "limit_exceeded", detail: "limit_requests" });
    expect(await hub(doc.id).head()).toBe(8);
  });

  it("a body over 65,536 UTF-8 bytes -> too_large body_too_large (a snapshot too)", async () => {
    const st = new DocState();
    const body = "가".repeat(21_846); // 65,538 bytes
    expect(applyFrame(st, DOC_TYPES.apicol, 1, [{ op: "put", id: "req:r1", type: "request", fields: { body } }])).toEqual({
      ok: false,
      reason: { code: "too_large", detail: "body_too_large" },
    });
    expect(DOC_TYPES.apicol.validateOp({ op: "set", id: "req:r1", field: "body", value: "x".repeat(65_537) }, st)).toEqual({ code: "too_large", detail: "body_too_large" });
    expect(DOC_TYPES.apicol.validateOp({ op: "set", id: "req:r1", field: "body", value: "x".repeat(65_536) }, st)).toBeNull();
    // over the socket a body that fits the frame still meets the common field limit (61,440) first
    const { a } = await apicolDoc();
    const big = sendOp(a, [{ op: "put", id: "req:r1", type: "request", fields: { body: "x".repeat(62_000) } }]);
    expect(await rejectOf(a, big)).toEqual({ t: "reject", client_op_id: big, code: "too_large" });
  });

  it("a document over 2,097,152 bytes -> too_large (the common limit)", async () => {
    const { doc, a } = await apicolDoc();
    let t = Date.now();
    await runInDurableObject(hub(doc.id), (inst) => {
      (inst as any).now = () => (t += 100); // <= 10 frames per simulated second, under the op rate
    });
    const body = "x".repeat(60_000);
    for (let i = 0; i < 34; i++) await ackOf(a, sendOp(a, [{ op: "put", id: `req:${i}`, type: "request", fields: { body } }]));
    const over = sendOp(a, [{ op: "put", id: "req:34", type: "request", fields: { body } }]);
    expect(await rejectOf(a, over)).toEqual({ t: "reject", client_op_id: over, code: "too_large" });
    expect(await hub(doc.id).head()).toBe(34);
  });

  it("envvar secret:true with a non-empty value (same op or with the state) -> invalid secret_value; nothing recorded", async () => {
    const { owner, doc, a } = await apicolDoc();
    await ackOf(a, sendOp(a, VALID));
    const bad: unknown[][] = [
      [{ op: "put", id: "envvar:v9", type: "envvar", fields: { env: "e1", name: "k", secret: true, value: "canary-Q9x7Z2" } }],
      [{ op: "set", id: "envvar:v2", field: "value", value: "canary-Q9x7Z2" }],
      [{ op: "set", id: "envvar:v3", field: "secret", value: true }],
      [{ op: "put", id: "envvar:v3", type: "envvar", fields: { secret: true } }],
      [{ op: "set", id: "col", field: "vars", value: [{ name: "token", secret: true, value: "canary-Q9x7Z2" }] }],
    ];
    for (const ops of bad) {
      const id = sendOp(a, ops);
      expect(await rejectOf(a, id), JSON.stringify(ops)).toEqual({ t: "reject", client_op_id: id, code: "invalid", detail: "secret_value" });
    }
    expect(await hub(doc.id).head()).toBe(1);
    // neither the record nor the snapshot has a refused op
    const b = await connectDoc(owner.token, doc.id, { since: 0 });
    expect(JSON.stringify(b.hello)).not.toContain("canary");
    expect(JSON.stringify(b.hello)).not.toContain("envvar:v9");
    const fresh = await connectDoc(owner.token, doc.id);
    expect(JSON.stringify(fresh.hello.snapshot)).not.toContain("canary");
  });

  it("a create snapshot gets the same checks", async () => {
    const owner = await signup();
    const { space } = await makeSpace(owner);
    const objects = VALID.map((o: any) => ({ id: o.id, type: o.type, fields: o.fields }));
    const ok = await makeDoc(owner, space.id, "apicol", { seq: 0, objects });
    expect(ok.head_seq).toBe(1);
    const post = (objs: unknown[]) => api(owner.token).post<any>(`/v1/spaces/${space.id}/docs`, { type: "apicol", title: "x", snapshot: { seq: 0, objects: objs } });
    for (const objs of [
      [{ id: "foo:1", type: "foo", fields: {} }],
      [{ id: "envvar:v1", type: "envvar", fields: { secret: true, value: "canary" } }],
      Array.from({ length: 1001 }, (_, i) => ({ id: `req:${i}`, type: "request", fields: {} })),
    ]) {
      const r = await post(objs);
      expect(r.status).toBe(400);
      expect(r.body.error.details.fields).toEqual({ snapshot: "invalid" });
    }
  });
});
