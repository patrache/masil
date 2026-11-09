// api.md 5.24 (버전 규칙) / collab.md 1.1, 3.3: named versions, restore as a sequenced reset that keeps history, automatic
// versions and record-version retention from the CollabHub alarm, and the lock on edits and restores.
import { env, runDurableObjectAlarm, runInDurableObject } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { api, connectDoc, makeDoc, makeSpace, signup, type DocClient } from "../helpers";

const hub = (docId: string) => env.COLLAB_HUB.getByName(`doc:${docId}`);

async function setClock(docId: string, clock: () => number): Promise<void> {
  await runInDurableObject(hub(docId), (inst) => {
    (inst as any).now = clock;
  });
}

let opNo = 0;
async function edit(c: DocClient, ops: unknown[]): Promise<number> {
  const id = `v${++opNo}`;
  c.send({ t: "op", client_op_id: id, base_seq: 0, ops });
  const f = await c.next("ack", (x) => x.client_op_id === id);
  return f.seq as number;
}

async function room() {
  const owner = await signup();
  const member = await signup();
  const { space } = await makeSpace(owner, [member]);
  return { owner, member, space };
}

const N1 = { seq: 0, objects: [{ id: "n1", type: "node", fields: { cpu: 1 } }] };
const versionsOf = async (token: string, docId: string) => (await api(token).get<any>(`/v1/docs/${docId}/versions`)).body.items as any[];

describe("named versions and restore", () => {
  it("a version captures the head; restore resets every socket, keeps the history and adds a restore version", async () => {
    const { owner, member, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    const a = await connectDoc(owner.token, doc.id);
    const b = await connectDoc(member.token, doc.id);
    await edit(a, [{ op: "set", id: "n1", field: "cpu", value: 2 }]);

    const v1 = await api(member.token).post<any>(`/v1/docs/${doc.id}/versions`, { name: "출시 전" });
    expect(v1.status).toBe(201);
    expect(v1.body.version).toMatchObject({ no: 1, seq: 2, name: "출시 전", auto: false, restored_from: null });
    expect(v1.body.version.created_by.id).toBe(member.user.id);

    await edit(a, [{ op: "set", id: "n1", field: "cpu", value: 9 }, { op: "put", id: "n2", type: "node", fields: {} }]);
    const r = await api(member.token).post<any>(`/v1/docs/${doc.id}/versions/1/restore`);
    expect(r.status).toBe(200);
    expect(r.body.version).toMatchObject({ no: 2, seq: 4, name: null, auto: false, restored_from: 1 });
    expect(r.body.doc.head_seq).toBe(4);

    for (const c of [a, b]) {
      const f = await c.next("op", (x) => x.ops[0]?.op === "reset");
      expect(f.seq).toBe(4);
      expect(f.author).toBe(member.user.id);
      expect(f.client_op_id).toBeUndefined();
      expect(f.ops[0].snapshot.objects).toEqual([{ id: "n1", type: "node", fields: { cpu: 2 } }]);
    }
    const c = await connectDoc(owner.token, doc.id);
    expect(c.hello.snapshot).toEqual({ seq: 4, objects: [{ id: "n1", type: "node", fields: { cpu: 2 } }] });

    const items = await versionsOf(owner.token, doc.id);
    expect(items.map((v) => v.no)).toEqual([2, 1]);
    const one = await api(owner.token).get<any>(`/v1/docs/${doc.id}/versions/1`);
    expect(one.status).toBe(200);
    expect(one.body.version.no).toBe(1);
    expect(one.body.snapshot).toEqual({ seq: 2, objects: [{ id: "n1", type: "node", fields: { cpu: 2 } }] });
    expect((await api(owner.token).get(`/v1/docs/${doc.id}/versions/7`)).status).toBe(404);
    expect((await api(owner.token).get(`/v1/docs/${doc.id}/versions/x`)).status).toBe(404);
    expect((await api(owner.token).post(`/v1/docs/${doc.id}/versions/7/restore`)).status).toBe(404);
    // an outsider sees nothing
    const outsider = await signup();
    expect((await api(outsider.token).get(`/v1/docs/${doc.id}/versions`)).status).toBe(404);
  });

  it("name is 1..60; the 101st named version -> 422 limit_exceeded named_versions (restore versions do not count)", async () => {
    const { owner, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    expect((await api(owner.token).post<any>(`/v1/docs/${doc.id}/versions`, { name: "" })).status).toBe(400);
    expect((await api(owner.token).post<any>(`/v1/docs/${doc.id}/versions`, { name: "가".repeat(61) })).status).toBe(400);
    // 99 through the hub directly (RL_DOC allows 30 a minute), then the 100th and the restore through REST
    for (let i = 0; i < 99; i++) {
      const r = await hub(doc.id).createVersion(doc.id, "arch", `v${i}`, owner.user.id);
      expect("no" in r).toBe(true);
    }
    expect((await api(owner.token).post(`/v1/docs/${doc.id}/versions`, { name: "백번째" })).status).toBe(201);
    expect((await api(owner.token).post(`/v1/docs/${doc.id}/versions/1/restore`)).status).toBe(200);
    const over = await api(owner.token).post<any>(`/v1/docs/${doc.id}/versions`, { name: "백한번째" });
    expect(over.status).toBe(422);
    expect(over.body.error.code).toBe("limit_exceeded");
    expect(over.body.error.details).toEqual({ what: "named_versions", limit: 100 });
    expect((await versionsOf(owner.token, doc.id)).length).toBe(101);
  });
});

describe("automatic versions and retention (DO alarm)", () => {
  it("200 seqs -> one auto version; 200 more within 5 minutes -> none; D1 head_seq follows", async () => {
    const { owner, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch");
    const a = await connectDoc(owner.token, doc.id);
    let t = Date.now();
    await setClock(doc.id, () => (t += 60)); // under 20 op frames a simulated second
    await edit(a, [{ op: "put", id: "n1", type: "node", fields: {} }]);
    for (let i = 0; i < 198; i++) await edit(a, [{ op: "set", id: "n1", field: "cpu", value: i }]);
    expect(await runDurableObjectAlarm(hub(doc.id))).toBe(true);
    expect(await versionsOf(owner.token, doc.id)).toEqual([]); // 199 seqs since nothing, edits not idle
    await edit(a, [{ op: "set", id: "n1", field: "cpu", value: 200 }]);
    await runDurableObjectAlarm(hub(doc.id));
    let items = await versionsOf(owner.token, doc.id);
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ no: 1, seq: 200, name: null, auto: true, restored_from: null, created_by: null });
    expect((await api(owner.token).get<any>(`/v1/docs/${doc.id}`)).body.doc.head_seq).toBe(200);

    for (let i = 0; i < 200; i++) await edit(a, [{ op: "set", id: "n1", field: "cpu", value: i }]);
    await runDurableObjectAlarm(hub(doc.id));
    items = await versionsOf(owner.token, doc.id);
    expect(items).toHaveLength(1);
    // five minutes after the last automatic one, the 200-seq rule stands again
    t += 5 * 60_000;
    await runDurableObjectAlarm(hub(doc.id));
    items = await versionsOf(owner.token, doc.id);
    expect(items.map((v) => [v.no, v.seq, v.auto])).toEqual([
      [2, 400, true],
      [1, 200, true],
    ]);
  }, 60_000);

  it("idle 5 minutes -> auto version; 31 days later record versions are gone, named ones stay", async () => {
    const { owner, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    const a = await connectDoc(owner.token, doc.id);
    let t = Date.now();
    await setClock(doc.id, () => t);
    await edit(a, [{ op: "set", id: "n1", field: "cpu", value: 5 }]);
    expect((await api(owner.token).post(`/v1/docs/${doc.id}/versions`, { name: "이름" })).status).toBe(201); // no 1, seq 2
    await edit(a, [{ op: "set", id: "n1", field: "cpu", value: 6 }]);
    t += 4 * 60_000;
    await runDurableObjectAlarm(hub(doc.id));
    expect((await versionsOf(owner.token, doc.id)).map((v) => v.no)).toEqual([1]);
    t += 60_000;
    await runDurableObjectAlarm(hub(doc.id));
    expect((await versionsOf(owner.token, doc.id)).map((v) => [v.no, v.auto, v.seq])).toEqual([
      [2, true, 3],
      [1, false, 2],
    ]);
    // nothing new since: no further auto version, however long
    t += 10 * 60_000;
    await runDurableObjectAlarm(hub(doc.id));
    expect((await versionsOf(owner.token, doc.id)).length).toBe(2);
    t += 31 * 24 * 3600_000;
    expect(await runDurableObjectAlarm(hub(doc.id))).toBe(true); // the expiry itself keeps an alarm armed
    expect((await versionsOf(owner.token, doc.id)).map((v) => v.no)).toEqual([1]);
    expect((await api(owner.token).get(`/v1/docs/${doc.id}/versions/2`)).status).toBe(404);
  });

  it("the 201st record version drops the oldest record version; named versions stay", async () => {
    const { owner, space } = await room();
    const doc = await makeDoc(owner, space.id, "arch", N1);
    const a = await connectDoc(owner.token, doc.id);
    let t = Date.now();
    await setClock(doc.id, () => t);
    expect((await api(owner.token).post(`/v1/docs/${doc.id}/versions`, { name: "keep" })).status).toBe(201); // no 1
    for (let i = 0; i < 201; i++) {
      await edit(a, [{ op: "set", id: "n1", field: "cpu", value: i }]);
      t += 5 * 60_000 + 1;
      await runDurableObjectAlarm(hub(doc.id));
    }
    const items = await versionsOf(owner.token, doc.id);
    const records = items.filter((v) => v.name === null);
    expect(records).toHaveLength(200);
    expect(records.map((v) => v.no)).not.toContain(2); // the first auto version (no 2) went
    expect(records[0].no).toBe(202);
    expect(items.filter((v) => v.name === "keep").map((v) => v.no)).toEqual([1]);
  }, 60_000);
});

describe("lock", () => {
  it("a locked doc: a member's op -> reject locked, the creator's passes; unlocked -> the member edits again", async () => {
    const { owner, member, space } = await room();
    const doc = await makeDoc(member, space.id, "arch", N1); // the member is the creator here
    const other = await signup();
    const inv = await api(owner.token).post<any>(`/v1/spaces/${space.id}/invites`, { max_uses: null });
    expect((await api(other.token).post(`/v1/invites/${inv.body.invite.code}/accept`)).status).toBe(200);

    const creator = await connectDoc(member.token, doc.id);
    const plain = await connectDoc(other.token, doc.id);
    expect((await api(member.token).patch(`/v1/docs/${doc.id}`, { locked: true })).status).toBe(200);
    plain.send({ t: "op", client_op_id: "locked1", base_seq: 1, ops: [{ op: "set", id: "n1", field: "cpu", value: 3 }] });
    expect(await plain.next("reject", (f) => f.client_op_id === "locked1")).toEqual({ t: "reject", client_op_id: "locked1", code: "locked" });
    expect(await edit(creator, [{ op: "set", id: "n1", field: "cpu", value: 4 }])).toBe(2);
    // the owner (space owner -> admin) edits too
    const own = await connectDoc(owner.token, doc.id);
    expect(await edit(own, [{ op: "set", id: "n1", field: "mem", value: 1 }])).toBe(3);
    // restore on a locked doc: editors only
    expect((await api(member.token).post(`/v1/docs/${doc.id}/versions`, { name: "a" })).status).toBe(201);
    expect((await api(other.token).post(`/v1/docs/${doc.id}/versions/1/restore`)).status).toBe(403);
    // a new connection after the lock sees it too (D1 is the truth)
    const plain2 = await connectDoc(other.token, doc.id);
    plain2.send({ t: "op", client_op_id: "locked2", base_seq: 3, ops: [{ op: "set", id: "n1", field: "cpu", value: 3 }] });
    expect((await plain2.next("reject", (f) => f.client_op_id === "locked2")).code).toBe("locked");

    expect((await api(owner.token).patch(`/v1/docs/${doc.id}`, { locked: false })).status).toBe(200);
    expect(await edit(plain, [{ op: "set", id: "n1", field: "cpu", value: 7 }])).toBe(4);
    expect((await api(other.token).post(`/v1/docs/${doc.id}/versions/1/restore`)).status).toBe(200);
  });
});
