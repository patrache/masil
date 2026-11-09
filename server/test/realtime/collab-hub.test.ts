// docs/contract/collab.md 2-4: the document socket — server sequencing with field LWW, ack / op / reject, idempotent
// resends, hello catch-up (ops or snapshot), presence, frame and rate limits, and who may connect.
import { env, runInDurableObject } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { api, connectDoc, makeDoc, makeSpace, signup, type DocClient } from "../helpers";

const hub = (docId: string) => env.COLLAB_HUB.getByName(`doc:${docId}`);

/** Replaces the hub's clock: each call returns the next value of `clock()`. */
async function setClock(docId: string, clock: () => number): Promise<void> {
  await runInDurableObject(hub(docId), (inst) => {
    (inst as any).now = clock;
  });
}

let opNo = 0;
/** Sends one op frame and returns its client_op_id. */
function sendOp(c: DocClient, ops: unknown[], clientOpId = `c${++opNo}`, baseSeq = 0): string {
  c.send({ t: "op", client_op_id: clientOpId, base_seq: baseSeq, ops });
  return clientOpId;
}

const ackOf = (c: DocClient, id: string) => c.next("ack", (f) => f.client_op_id === id);
const rejectOf = (c: DocClient, id: string) => c.next("reject", (f) => f.client_op_id === id);

async function room() {
  const owner = await signup();
  const member = await signup();
  const { space } = await makeSpace(owner, [member]);
  return { owner, member, space };
}

const N1 = { seq: 0, objects: [{ id: "n1", type: "node", fields: { label: "api" } }] };

describe("CollabHub: sequencing and field LWW", () => {
  it("two members edit different fields at once: both get the same two ops in order, both fields survive", async () => {
    const { owner, member, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    expect(doc.head_seq).toBe(1);
    const a = await connectDoc(owner.token, doc.id);
    const b = await connectDoc(member.token, doc.id);
    expect(a.hello.seq).toBe(1);
    expect(a.hello.snapshot).toEqual({ seq: 1, objects: [{ id: "n1", type: "node", fields: { label: "api" } }] });

    const ia = sendOp(a, [{ op: "set", id: "n1", field: "cpu", value: 8 }]);
    const ib = sendOp(b, [{ op: "set", id: "n1", field: "mem", value: 16 }]);
    const ackA = await ackOf(a, ia);
    const ackB = await ackOf(b, ib);
    expect([ackA.seq, ackB.seq].sort()).toEqual([2, 3]);
    const opAtB = await b.next("op", (f) => f.client_op_id === ia);
    const opAtA = await a.next("op", (f) => f.client_op_id === ib);
    expect(opAtB).toEqual({ t: "op", seq: ackA.seq, author: owner.user.id, client_op_id: ia, ops: [{ op: "set", id: "n1", field: "cpu", value: 8 }] });
    expect(opAtA).toMatchObject({ seq: ackB.seq, author: member.user.id, ops: [{ op: "set", id: "n1", field: "mem", value: 16 }] });
    // the sender never gets its own op back
    expect(a.frames.some((f) => f.t === "op" && f.client_op_id === ia)).toBe(false);

    const c = await connectDoc(member.token, doc.id);
    expect(c.hello.seq).toBe(3);
    expect(c.hello.snapshot.objects).toEqual([{ id: "n1", type: "node", fields: { cpu: 8, label: "api", mem: 16 } }]);
  });

  it("the same field at once: the later seq's value wins everywhere", async () => {
    const { owner, member, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    const a = await connectDoc(owner.token, doc.id);
    const b = await connectDoc(member.token, doc.id);
    const ia = sendOp(a, [{ op: "set", id: "n1", field: "cpu", value: 1 }]);
    const ib = sendOp(b, [{ op: "set", id: "n1", field: "cpu", value: 2 }]);
    const sa = (await ackOf(a, ia)).seq as number;
    const sb = (await ackOf(b, ib)).seq as number;
    const c = await connectDoc(owner.token, doc.id);
    expect(c.hello.snapshot.objects[0].fields.cpu).toBe(sa > sb ? 1 : 2);
  });

  it("a resent client_op_id is acked with its first seq and changes nothing", async () => {
    const { owner, member, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    const a = await connectDoc(owner.token, doc.id);
    const b = await connectDoc(member.token, doc.id);
    const id = sendOp(a, [{ op: "set", id: "n1", field: "cpu", value: 4 }]);
    const first = await ackOf(a, id);
    // reconnect, resend the same frame (even with other content)
    a.close();
    const a2 = await connectDoc(owner.token, doc.id, { since: first.seq });
    sendOp(a2, [{ op: "set", id: "n1", field: "cpu", value: 99 }], id);
    const again = await ackOf(a2, id);
    expect(again.seq).toBe(first.seq);
    expect(await hub(doc.id).head()).toBe(first.seq);
    const probe = sendOp(b, [{ op: "set", id: "n1", field: "x", value: 1 }]);
    await ackOf(b, probe);
    expect(b.frames.filter((f) => f.t === "op" && f.client_op_id === id)).toHaveLength(1);
    const c = await connectDoc(owner.token, doc.id);
    expect(c.hello.snapshot.objects[0].fields.cpu).toBe(4);
  });
});

describe("CollabHub: hello catch-up", () => {
  it("hello{since:3} -> the ops after 3; a range compaction removed -> snapshot", async () => {
    const { owner, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch");
    const a = await connectDoc(owner.token, doc.id);
    expect(a.hello).toEqual({ t: "hello", seq: 0, snapshot: { seq: 0, objects: [] } });
    for (let i = 1; i <= 5; i++) await ackOf(a, sendOp(a, [{ op: "put", id: `n${i}`, type: "node", fields: { i } }]));

    const b = await connectDoc(owner.token, doc.id, { since: 3 });
    expect(b.hello.seq).toBe(5);
    expect(b.hello.snapshot).toBeUndefined();
    expect(b.hello.ops).toEqual([
      { seq: 4, author: owner.user.id, ops: [{ op: "put", id: "n4", type: "node", fields: { i: 4 } }] },
      { seq: 5, author: owner.user.id, ops: [{ op: "put", id: "n5", type: "node", fields: { i: 5 } }] },
    ]);
    const upToDate = await connectDoc(owner.token, doc.id, { since: 5 });
    expect(upToDate.hello).toEqual({ t: "hello", seq: 5, ops: [] });

    await runInDurableObject(hub(doc.id), (inst) => (inst as any).compact(true));
    const c = await connectDoc(owner.token, doc.id, { since: 3 });
    expect(c.hello.ops).toBeUndefined();
    expect(c.hello.snapshot.seq).toBe(5);
    expect(c.hello.snapshot.objects.map((o: any) => o.id)).toEqual(["n1", "n2", "n3", "n4", "n5"]);
    // a since the hub never had -> snapshot too
    const d = await connectDoc(owner.token, doc.id, { since: 9 });
    expect(d.hello.snapshot.seq).toBe(5);
    // after compaction the state still survives a reload from storage
    const e = await connectDoc(owner.token, doc.id, { since: 5 });
    expect(e.hello).toEqual({ t: "hello", seq: 5, ops: [] });
  });

  it("a ~1MB apicol document arrives as one snapshot frame (no splitting)", async () => {
    const { owner, space } = await room();
    const doc = await makeDoc(owner, space.id, "apicol");
    const a = await connectDoc(owner.token, doc.id);
    const big = "x".repeat(60_000);
    for (let i = 0; i < 17; i++) await ackOf(a, sendOp(a, [{ op: "put", id: `req:r${i}`, type: "request", fields: { body: big } }]));
    const b = await connectDoc(owner.token, doc.id);
    expect(b.frames.filter((f) => f.t === "hello")).toHaveLength(1);
    expect(b.hello.snapshot.objects).toHaveLength(17);
    expect(JSON.stringify(b.hello).length).toBeGreaterThan(1_000_000);
  });

  it("only the first hello of a connection is answered: repeats get nothing (no full snapshot per 30-byte frame)", async () => {
    const { owner, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    const a = await connectDoc(owner.token, doc.id);
    for (let i = 0; i < 20; i++) a.send({ t: "hello", since: null });
    a.send({ t: "hello", since: 0 });
    await ackOf(a, sendOp(a, [{ op: "set", id: "n1", field: "cpu", value: 1 }])); // frames are handled in order
    expect(a.frames.filter((f) => f.t === "hello")).toHaveLength(1);
  });

  it("a long-gone client whose ops bundle would exceed maxDocBytes gets the snapshot instead", async () => {
    const { owner, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    const a = await connectDoc(owner.token, doc.id);
    let t = Date.now();
    await setClock(doc.id, () => (t += 100)); // <= 10 frames per simulated second, under the op rate
    const v = "y".repeat(15_000);
    for (let i = 0; i < 72; i++) await ackOf(a, sendOp(a, [{ op: "set", id: "n1", field: "blob", value: `${i}${v}` }])); // 72 x 15KB > 1,048,576
    const head = 73;
    const old = await connectDoc(owner.token, doc.id, { since: 1 });
    expect(old.hello.ops).toBeUndefined();
    expect(old.hello.snapshot.seq).toBe(head);
    const recent = await connectDoc(owner.token, doc.id, { since: head - 2 });
    expect(recent.hello.ops.map((o: any) => o.seq)).toEqual([head - 1, head]);
  });
});

describe("CollabHub: refusals and limits", () => {
  it("__proto__ / constructor / non-ASCII names -> reject invalid; nothing sequenced", async () => {
    const { owner, member, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    const b = await connectDoc(member.token, doc.id);
    const bad = [
      [{ op: "set", id: "n1", field: "__proto__", value: { polluted: true } }],
      [{ op: "set", id: "n1", field: "constructor", value: 1 }],
      [{ op: "put", id: "노드", type: "node", fields: {} }],
      [{ op: "put", id: "n2", type: "node", fields: JSON.parse('{"__proto__": {"x": 1}}') }],
      [{ op: "reset", snapshot: { seq: 0, objects: [] } }],
      [{ op: "move", id: "n1" }],
      [],
    ];
    for (const ops of bad) {
      const id = sendOp(b, ops);
      expect(await rejectOf(b, id)).toEqual({ t: "reject", client_op_id: id, code: "invalid" });
    }
    // raw text so the __proto__ key reaches the server as an own key
    b.send('{"t":"op","client_op_id":"rawproto","base_seq":1,"ops":[{"op":"put","id":"n3","type":"node","fields":{"__proto__":{"x":1}}}]}');
    expect((await rejectOf(b, "rawproto")).code).toBe("invalid");
    expect(await hub(doc.id).head()).toBe(1);
    // a plain member edits an unlocked doc
    const ok = sendOp(b, [{ op: "set", id: "n1", field: "cpu", value: 2 }]);
    expect((await ackOf(b, ok)).seq).toBe(2);
  });

  it("a client frame over 65,536 bytes -> close 1009", async () => {
    const { owner, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    const a = await connectDoc(owner.token, doc.id);
    sendOp(a, [{ op: "set", id: "n1", field: "f", value: "z".repeat(70_000) }]);
    expect((await a.closed).code).toBe(1009);
  });

  it("limits: the 301st node -> limit_exceeded limit_node; a field over 16,384 -> too_large", async () => {
    const { owner, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch");
    const a = await connectDoc(owner.token, doc.id);
    const puts = (from: number, n: number) => Array.from({ length: n }, (_, i) => ({ op: "put", id: `n${from + i}`, type: "node", fields: {} }));
    await ackOf(a, sendOp(a, puts(0, 200)));
    await ackOf(a, sendOp(a, puts(200, 100)));
    const over = sendOp(a, puts(300, 1));
    expect(await rejectOf(a, over)).toEqual({ t: "reject", client_op_id: over, code: "limit_exceeded", detail: "limit_node" });
    const big = sendOp(a, [{ op: "set", id: "n0", field: "f", value: "q".repeat(16_383) }]);
    expect(await rejectOf(a, big)).toEqual({ t: "reject", client_op_id: big, code: "too_large" });
    expect(await hub(doc.id).head()).toBe(2);
  });

  it("the 21st op frame in a second -> reject rate_limited; three such seconds in a row -> close 4008", async () => {
    const { owner, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    const a = await connectDoc(owner.token, doc.id);
    let second = 1_000_000_000_000;
    await setClock(doc.id, () => second + 500);
    const burst = async () => {
      const ids = Array.from({ length: 21 }, () => sendOp(a, [{ op: "set", id: "n1", field: "cpu", value: 1 }]));
      for (const id of ids.slice(0, 20)) await ackOf(a, id);
      return ids[20]!;
    };
    const last1 = await burst();
    expect(await rejectOf(a, last1)).toEqual({ t: "reject", client_op_id: last1, code: "rate_limited" });
    second += 1000;
    const last2 = await burst();
    expect((await rejectOf(a, last2)).code).toBe("rate_limited");
    second += 1000;
    await burst();
    expect((await a.closed).code).toBe(4008);
  });
});

describe("CollabHub: presence", () => {
  it("goes to the others only, is not kept (a new connection sees none)", async () => {
    const { owner, member, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    const a = await connectDoc(owner.token, doc.id);
    const b = await connectDoc(member.token, doc.id);
    a.send({ t: "presence", cursor: { x: 3, y: 4 }, selection: ["n1"] });
    expect(await b.next("presence")).toEqual({ t: "presence", user: owner.user.id, cursor: { x: 3, y: 4 }, selection: ["n1"], dragging: null });
    a.send({ t: "presence", dragging: { id: "n1" } }); // only what changed; the rest is kept for this connection
    expect(await b.next("presence")).toEqual({ t: "presence", user: owner.user.id, cursor: { x: 3, y: 4 }, selection: ["n1"], dragging: { id: "n1" } });
    // a sync point: the probe's ack comes after the presence frames were handled
    await ackOf(a, sendOp(a, [{ op: "set", id: "n1", field: "k", value: 1 }]));
    expect(a.frames.some((f) => f.t === "presence")).toBe(false);

    const c = await connectDoc(member.token, doc.id);
    await ackOf(a, sendOp(a, [{ op: "set", id: "n1", field: "k", value: 2 }]));
    await c.next("op");
    expect(c.frames.some((f) => f.t === "presence")).toBe(false);
  });

  it("over 10 presence frames a second are dropped silently", async () => {
    const { owner, member, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    const a = await connectDoc(owner.token, doc.id);
    const b = await connectDoc(member.token, doc.id);
    await setClock(doc.id, () => 2_000_000_000_500);
    for (let i = 0; i < 15; i++) a.send({ t: "presence", cursor: i });
    await ackOf(a, sendOp(a, [{ op: "set", id: "n1", field: "k", value: 1 }]));
    await b.next("op");
    expect(b.frames.filter((f) => f.t === "presence").map((f) => f.cursor)).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 8, 9]);
    expect(a.ws.readyState).toBe(WebSocket.OPEN);
  });
});

describe("CollabHub: who may connect", () => {
  it("an outsider -> 404 before the upgrade, even with a forged X-Masil-Doc-Auth", async () => {
    const { owner, space } = await room();
    const outsider = await signup();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    const plain = await api(outsider.token).raw(`/v1/docs/${doc.id}/ws`, { headers: { Upgrade: "websocket" } });
    expect(plain.status).toBe(404);
    const forged = JSON.stringify({ userId: owner.user.id, sessionId: "s", role: "creator", docId: doc.id, docType: "arch", locked: false });
    const res = await api(outsider.token).raw(`/v1/docs/${doc.id}/ws`, { headers: { Upgrade: "websocket", "X-Masil-Doc-Auth": forged } });
    expect(res.status).toBe(404);
  });

  it("a member's forged X-Masil-Doc-Auth is ignored: ops carry their own id", async () => {
    const { owner, member, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    const a = await connectDoc(owner.token, doc.id);
    const forged = JSON.stringify({ userId: owner.user.id, sessionId: "s", role: "creator", docId: doc.id, docType: "arch", locked: false });
    const b = await connectDoc(member.token, doc.id, { headers: { "X-Masil-Doc-Auth": forged } });
    const id = sendOp(b, [{ op: "set", id: "n1", field: "cpu", value: 3 }]);
    expect((await a.next("op", (f) => f.client_op_id === id)).author).toBe(member.user.id);
  });

  it("the hub refuses a request without the Worker's header", async () => {
    const res = await hub("no-such-doc").fetch("http://do/ws", { headers: { Upgrade: "websocket" } });
    expect(res.status).toBe(401);
  });
});

describe("CollabHub.init: POST /v1/spaces/{id}/docs snapshot", () => {
  it("a broken snapshot -> 400 fields.snapshot invalid; over the type's limits -> too_long; no doc either way", async () => {
    const { owner, space } = await room();
    const post = (snapshot: unknown) => api(owner.token).post<any>(`/v1/spaces/${space.id}/docs`, { type: "arch", title: "t", snapshot });
    for (const bad of [
      { objects: "x" },
      { seq: 0, objects: [{ id: "a", type: "node", fields: {} }, { id: "a", type: "node", fields: {} }] },
      { seq: 0, objects: [{ id: "노드", type: "node", fields: {} }] },
      JSON.parse('{"seq":0,"objects":[{"id":"a","type":"node","fields":{"__proto__":1}}]}'),
    ]) {
      const r = await post(bad);
      expect(r.status).toBe(400);
      expect(r.body.error.details.fields).toEqual({ snapshot: "invalid" });
    }
    const many = { seq: 0, objects: Array.from({ length: 301 }, (_, i) => ({ id: `n${i}`, type: "node", fields: {} })) };
    const r = await post(many);
    expect(r.status).toBe(400);
    expect(r.body.error.details.fields).toEqual({ snapshot: "too_long" });
    const list = await api(owner.token).get<any>(`/v1/spaces/${space.id}/docs`);
    expect(list.body.items).toEqual([]);
  });

  it("an empty snapshot starts at seq 0; snapshot.seq is ignored", async () => {
    const { owner, space } = await room();
    expect((await makeDoc(owner, space.id, "arch", { seq: 0, objects: [] })).head_seq).toBe(0);
    const doc = await makeDoc(owner, space.id, "arch", { seq: 42, objects: [{ id: "a", type: "node", fields: {} }] });
    expect(doc.head_seq).toBe(1);
    const a = await connectDoc(owner.token, doc.id);
    expect(a.hello.snapshot.seq).toBe(1);
  });
});
