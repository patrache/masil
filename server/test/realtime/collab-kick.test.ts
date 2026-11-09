// api.md 5.24 / collab.md 4.1: open document sockets follow membership, sessions and roles — kicked{reason} + 4100 on
// leave / kick / ban / space delete / doc delete, 4001 / 4003 on session revocation, and the lock check sees a new role
// without a reconnect.
import { describe, it, expect } from "vitest";
import { api, connectDoc, login, makeAdmin, makeDoc, makeSpace, signup, type DocClient } from "../helpers";

async function room() {
  const owner = await signup();
  const member = await signup();
  const { space } = await makeSpace(owner, [member]);
  const doc = await makeDoc(owner, space.id, "arch", { seq: 0, objects: [{ id: "n1", type: "node", fields: {} }] });
  return { owner, member, space, doc };
}

/** kicked{reason} then close 4100. */
async function expectKicked(c: DocClient, reason: string): Promise<void> {
  expect(await c.next("kicked")).toEqual({ t: "kicked", reason });
  expect((await c.closed).code).toBe(4100);
}

const wsStatus = async (token: string, docId: string) => (await api(token).raw(`/v1/docs/${docId}/ws`, { headers: { Upgrade: "websocket" } })).status;

let opNo = 0;
async function op(c: DocClient, ops: unknown[]): Promise<any> {
  const id = `k${++opNo}`;
  c.send({ t: "op", client_op_id: id, base_seq: 0, ops });
  return Promise.race([c.next("ack", (f) => f.client_op_id === id), c.next("reject", (f) => f.client_op_id === id)]);
}

describe("kicked: leave, kick, ban", () => {
  it("the owner kicks a member: their doc socket gets kicked{kicked} + 4100, reconnecting is 404; others stay", async () => {
    const { owner, member, space, doc } = await room();
    const other = await makeSpace(member);
    const otherDoc = await makeDoc(member, other.space.id, "arch");
    const m = await connectDoc(member.token, doc.id);
    const mOther = await connectDoc(member.token, otherDoc.id);
    const o = await connectDoc(owner.token, doc.id);
    expect((await api(owner.token).del(`/v1/spaces/${space.id}/members/@${member.handle}`)).status).toBe(204);
    await expectKicked(m, "kicked");
    expect(await wsStatus(member.token, doc.id)).toBe(404);
    // the owner's socket and the member's socket in another room are untouched
    expect((await op(o, [{ op: "set", id: "n1", field: "x", value: 1 }])).t).toBe("ack");
    expect((await op(mOther, [{ op: "put", id: "a", type: "node", fields: {} }])).t).toBe("ack");
  });

  it("leaving -> left; a ban -> banned", async () => {
    const { owner, member, space, doc } = await room();
    const third = await signup();
    const inv = await api(owner.token).post<any>(`/v1/spaces/${space.id}/invites`, { max_uses: null });
    expect((await api(third.token).post(`/v1/invites/${inv.body.invite.code}/accept`)).status).toBe(200);
    const m = await connectDoc(member.token, doc.id);
    const t = await connectDoc(third.token, doc.id);
    expect((await api(member.token).del(`/v1/spaces/${space.id}/members/@me`)).status).toBe(204);
    await expectKicked(m, "left");
    expect((await api(owner.token).del(`/v1/spaces/${space.id}/members/@${third.handle}?ban=true`)).status).toBe(204);
    await expectKicked(t, "banned");
  });
});

describe("kicked: space and doc deleted", () => {
  it("space delete -> every doc socket of the space gets space_deleted; GET and ws are 404 afterwards", async () => {
    const { owner, member, space, doc } = await room();
    const doc2 = await makeDoc(member, space.id, "apicol");
    const o = await connectDoc(owner.token, doc.id);
    const m = await connectDoc(member.token, doc.id);
    const m2 = await connectDoc(member.token, doc2.id);
    expect((await api(owner.token).del(`/v1/spaces/${space.id}`)).status).toBe(204);
    for (const c of [o, m, m2]) await expectKicked(c, "space_deleted");
    expect((await api(member.token).get(`/v1/docs/${doc.id}`)).status).toBe(404);
    expect(await wsStatus(member.token, doc2.id)).toBe(404);
  });

  it("doc delete -> doc_deleted on that doc only", async () => {
    const { owner, member, space, doc } = await room();
    const keep = await makeDoc(owner, space.id, "arch");
    const m = await connectDoc(member.token, doc.id);
    const k = await connectDoc(member.token, keep.id);
    expect((await api(owner.token).del(`/v1/docs/${doc.id}`)).status).toBe(204);
    await expectKicked(m, "doc_deleted");
    expect((await op(k, [{ op: "put", id: "a", type: "node", fields: {} }])).t).toBe("ack");
    expect(await wsStatus(member.token, doc.id)).toBe(404);
  });
});

describe("sessions and roles", () => {
  it("logout closes that session's doc socket with 4001; another session's socket stays", async () => {
    const { member, doc } = await room();
    const second = await login(member.handle, member.password);
    const s1 = await connectDoc(member.token, doc.id);
    const s2 = await connectDoc(second.token, doc.id);
    expect((await api(member.token).post("/v1/auth/logout")).status).toBe(204);
    expect((await s1.closed).code).toBe(4001);
    expect(s1.frames.some((f) => f.t === "kicked")).toBe(false);
    expect((await op(s2, [{ op: "set", id: "n1", field: "x", value: 1 }])).t).toBe("ack");
  });

  it("a password change closes the other sessions' doc sockets with 4001", async () => {
    const { member, doc } = await room();
    const second = await login(member.handle, member.password);
    const other = await connectDoc(second.token, doc.id);
    const r = await api(member.token).post("/v1/auth/password", { current_password: member.password, new_password: `${member.password}-new` });
    expect(r.status).toBe(204);
    expect((await other.closed).code).toBe(4001);
  });

  it("a suspension closes the doc sockets with 4003", async () => {
    const { member, doc } = await room();
    const adm = await signup();
    await makeAdmin(adm);
    const m = await connectDoc(member.token, doc.id);
    expect((await api(adm.token).post(`/v1/admin/users/@${member.handle}/suspend`, { reason: "spam" })).status).toBe(200);
    expect((await m.closed).code).toBe(4003);
  });

  it("an admin demoted to member hits the lock on the next op, without reconnecting; promoted back, edits again", async () => {
    const { owner, member, space, doc } = await room();
    expect((await api(owner.token).patch(`/v1/spaces/${space.id}/members/@${member.handle}`, { role: "admin" })).status).toBe(200);
    const m = await connectDoc(member.token, doc.id);
    expect((await api(owner.token).patch(`/v1/docs/${doc.id}`, { locked: true })).status).toBe(200);
    expect((await op(m, [{ op: "set", id: "n1", field: "x", value: 1 }])).t).toBe("ack");
    expect((await api(owner.token).patch(`/v1/spaces/${space.id}/members/@${member.handle}`, { role: "member" })).status).toBe(200);
    await expect.poll(async () => (await op(m, [{ op: "set", id: "n1", field: "x", value: 2 }])).code, { timeout: 3000 }).toBe("locked");
    expect((await api(owner.token).patch(`/v1/spaces/${space.id}/members/@${member.handle}`, { role: "admin" })).status).toBe(200);
    await expect.poll(async () => (await op(m, [{ op: "set", id: "n1", field: "x", value: 3 }])).t, { timeout: 3000 }).toBe("ack");
  });

  it("a transfer makes the new owner an editor of locked docs on their open socket", async () => {
    const { owner, member, space, doc } = await room();
    const m = await connectDoc(member.token, doc.id);
    expect((await api(owner.token).patch(`/v1/docs/${doc.id}`, { locked: true })).status).toBe(200);
    expect((await op(m, [{ op: "set", id: "n1", field: "x", value: 1 }])).code).toBe("locked");
    expect((await api(owner.token).post(`/v1/spaces/${space.id}/transfer`, { user: `@${member.handle}` })).status).toBe(200);
    await expect.poll(async () => (await op(m, [{ op: "set", id: "n1", field: "x", value: 2 }])).t, { timeout: 3000 }).toBe("ack");
  });
});
