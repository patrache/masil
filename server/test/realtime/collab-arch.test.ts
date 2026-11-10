// docs/contract/arch.md 2 / collab.md 6: an `arch` document over GET /v1/docs/{id}/ws (P0 route) runs the arch
// validator — a bad op is refused to its sender only, a good one reaches the other members with its seq.
import { describe, it, expect } from "vitest";
import { api, connectDoc, makeDoc, makeSpace, signup, type DocClient } from "../helpers";

let opNo = 0;
function sendOp(c: DocClient, ops: unknown[]): string {
  const id = `arch${++opNo}`;
  c.send({ t: "op", client_op_id: id, base_seq: 0, ops });
  return id;
}

const API = { id: "n:api", type: "node", fields: { kind: "api", label: "api", pos: [16, 2], parent: "", instances: 2 } };

async function room() {
  const owner = await signup();
  const member = await signup();
  const { space } = await makeSpace(owner, [member]);
  const doc = await makeDoc(owner, space.id, "arch", { seq: 0, objects: [{ id: "meta", type: "meta", fields: { title: "pay", seed: 42, duration_s: 300 } }, API] });
  const a = await connectDoc(owner.token, doc.id);
  const b = await connectDoc(member.token, doc.id);
  return { owner, member, space, doc, a, b };
}

describe("CollabHub arch documents", () => {
  it("A's bad op gets reject{code: invalid, detail: arch_bad_field}; B never sees it; a good op reaches B with its seq", async () => {
    const { owner, doc, a, b } = await room();
    const bad = sendOp(a, [{ op: "set", id: "n:api", field: "cpu", value: 8 }]);
    expect(await a.next("reject", (f) => f.client_op_id === bad)).toEqual({ t: "reject", client_op_id: bad, code: "invalid", detail: "arch_bad_field" });

    const good = sendOp(a, [{ op: "set", id: "n:api", field: "instances", value: 4 }]);
    const ack = await a.next("ack", (f) => f.client_op_id === good);
    const op = await b.next("op", (f) => f.client_op_id === good);
    expect(op).toEqual({ t: "op", seq: ack.seq, author: owner.user.id, client_op_id: good, ops: [{ op: "set", id: "n:api", field: "instances", value: 4 }] });
    expect(b.frames.some((f) => f.client_op_id === bad)).toBe(false);

    const c = await connectDoc(owner.token, doc.id);
    expect(c.hello.snapshot.objects.find((o: any) => o.id === "n:api").fields.instances).toBe(4);
  });

  it("range, cycle and limit refusals carry their detail; nothing of the frame is applied", async () => {
    const { a, b } = await room();
    const cases: Array<[unknown[], string, string]> = [
      [[{ op: "set", id: "n:api", field: "instances", value: 0 }], "invalid", "arch_bad_value"],
      [[{ op: "put", id: "n:x", type: "box", fields: {} }], "invalid", "arch_bad_object"],
      [
        [
          { op: "put", id: "b:p", type: "box", fields: { kind: "region", parent: "" } },
          { op: "put", id: "b:q", type: "box", fields: { kind: "vpc", parent: "p" } },
          { op: "set", id: "b:p", field: "parent", value: "q" },
        ],
        "invalid",
        "arch_cycle",
      ],
    ];
    for (const [ops, code, detail] of cases) {
      const id = sendOp(a, ops);
      expect(await a.next("reject", (f) => f.client_op_id === id)).toEqual({ t: "reject", client_op_id: id, code, detail });
    }
    // fill the nodes up to the limit in frames of 200 ops, then the 301st put is refused
    const puts = Array.from({ length: 299 }, (_, i) => ({ op: "put", id: `n:x${i}`, type: "node", fields: { kind: "api" } }));
    for (const chunk of [puts.slice(0, 200), puts.slice(200)]) {
      const id = sendOp(a, chunk);
      await a.next("ack", (f) => f.client_op_id === id);
    }
    const over = sendOp(a, [{ op: "put", id: "n:last", type: "node", fields: { kind: "api" } }]);
    expect(await a.next("reject", (f) => f.client_op_id === over)).toEqual({ t: "reject", client_op_id: over, code: "limit_exceeded", detail: "arch_limit" });
    expect(b.frames.some((f) => f.t === "op" && f.ops.some((o: any) => o.id === "b:p" || o.id === "n:last"))).toBe(false);
  });

  it("a new document's snapshot is validated too", async () => {
    const owner = await signup();
    const { space } = await makeSpace(owner, []);
    const r = await api(owner.token).post(`/v1/spaces/${space.id}/docs`, { type: "arch", title: "bad", snapshot: { seq: 0, objects: [{ id: "n1", type: "node", fields: {} }] } });
    expect(r.status).toBe(400);
  });
});
