// api.md 5.24 / collab.md 4.1: open document sockets follow membership, sessions and roles — kicked{reason} + 4100 on
// leave / kick / ban / space delete / doc delete, 4001 / 4003 on session revocation, and the lock check sees a new role
// without a reconnect.
import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { api, connectDoc, login, makeAdmin, makeDoc, makeSpace, signup, type DocClient, type TestUser } from "../helpers";

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

  it("an operator's forced space removal -> space_deleted on every doc socket of the space; ws 404 afterwards", async () => {
    const { owner, member, space, doc } = await room();
    const adm = await signup();
    await makeAdmin(adm);
    const o = await connectDoc(owner.token, doc.id);
    const m = await connectDoc(member.token, doc.id);
    const r = await api(adm.token).post("/v1/admin/content/remove", { reason: "illegal", target_type: "space", target_id: space.id });
    expect(r.status).toBe(200);
    for (const c of [o, m]) await expectKicked(c, "space_deleted");
    expect(await wsStatus(member.token, doc.id)).toBe(404);
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

// The Worker's X-Masil-Doc-Auth is built from a D1 read made before the upgrade reaches the hub. A lock, role,
// membership or session change can commit (and its RPC reach the hub, finding no socket) in between. The hub then
// sees a stale header: these tests hand it one directly, as that late upgrade would.
async function sessionIdOf(u: TestUser): Promise<string> {
  const hash = [...new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(u.token)))].map((b) => b.toString(16).padStart(2, "0")).join("");
  const row = await env.DB.prepare("SELECT id FROM sessions WHERE token_hash = ?1").bind(hash).first<{ id: string }>();
  if (!row) throw new Error("no session for token");
  return row.id;
}

async function lateUpgrade(u: TestUser, sessionId: string, docId: string, role: string, locked: boolean): Promise<Response> {
  const header = JSON.stringify({ userId: u.user.id, sessionId, role, docId, docType: "arch", locked });
  return env.COLLAB_HUB.getByName(`doc:${docId}`).fetch(`http://do/v1/docs/${docId}/ws`, { headers: { Upgrade: "websocket", "X-Masil-Doc-Auth": header } });
}

/** hello on a raw accepted socket, then one op: the server's answer frame (ack / reject). */
async function opOnRaw(res: Response): Promise<any> {
  const ws = res.webSocket!;
  const frames: any[] = [];
  const got = new Promise<any>((resolve) =>
    ws.addEventListener("message", (e) => {
      const f = JSON.parse(e.data as string);
      frames.push(f);
      if (f.t === "ack" || f.t === "reject") resolve(f);
    }),
  );
  ws.accept();
  ws.send(JSON.stringify({ t: "hello", since: null }));
  ws.send(JSON.stringify({ t: "op", client_op_id: `late${++opNo}`, base_seq: 0, ops: [{ op: "set", id: "n1", field: "x", value: 9 }] }));
  return got;
}

describe("a late upgrade with a stale X-Masil-Doc-Auth", () => {
  it("lock committed first: the stale locked=false does not unlock the hub; the member's op is refused", async () => {
    const { owner, member, doc } = await room();
    const sid = await sessionIdOf(member);
    expect((await api(owner.token).patch(`/v1/docs/${doc.id}`, { locked: true })).status).toBe(200);
    const res = await lateUpgrade(member, sid, doc.id, "member", false);
    expect(res.status).toBe(101);
    expect((await opOnRaw(res)).code).toBe("locked");
  });

  it("unlock committed first: the stale locked=true does not re-lock the hub", async () => {
    const { owner, member, doc } = await room();
    const sid = await sessionIdOf(member);
    expect((await api(owner.token).patch(`/v1/docs/${doc.id}`, { locked: true })).status).toBe(200);
    expect((await api(owner.token).patch(`/v1/docs/${doc.id}`, { locked: false })).status).toBe(200);
    const res = await lateUpgrade(member, sid, doc.id, "member", true);
    expect((await opOnRaw(res)).t).toBe("ack");
  });

  it("demotion committed first: the stale role admin does not edit a locked doc", async () => {
    const { owner, member, space, doc } = await room();
    const sid = await sessionIdOf(member);
    expect((await api(owner.token).patch(`/v1/spaces/${space.id}/members/@${member.handle}`, { role: "admin" })).status).toBe(200);
    const open = await connectDoc(member.token, doc.id);
    expect((await api(owner.token).patch(`/v1/docs/${doc.id}`, { locked: true })).status).toBe(200);
    expect((await api(owner.token).patch(`/v1/spaces/${space.id}/members/@${member.handle}`, { role: "member" })).status).toBe(200);
    // the setRole RPC has reached the hub (it runs in the background): the open socket is a member now
    await expect.poll(async () => (await op(open, [{ op: "set", id: "n1", field: "x", value: 1 }])).code, { timeout: 3000 }).toBe("locked");
    const res = await lateUpgrade(member, sid, doc.id, "admin", true);
    expect((await opOnRaw(res)).code).toBe("locked");
  });

  it("removal committed (and kick found no socket): the stale upgrade is refused with 404", async () => {
    const { owner, member, space, doc } = await room();
    const sid = await sessionIdOf(member);
    expect((await api(owner.token).del(`/v1/spaces/${space.id}/members/@${member.handle}`)).status).toBe(204);
    const res = await lateUpgrade(member, sid, doc.id, "member", false);
    expect(res.status).toBe(404);
    expect(res.webSocket).toBeNull();
  });

  it("session revoked first: the stale upgrade is refused with 401", async () => {
    const { member, doc } = await room();
    const sid = await sessionIdOf(member);
    expect((await api(member.token).post("/v1/auth/logout")).status).toBe(204);
    const res = await lateUpgrade(member, sid, doc.id, "member", false);
    expect(res.status).toBe(401);
  });
});
