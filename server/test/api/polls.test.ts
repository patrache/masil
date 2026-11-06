// test/api/polls.test.ts
import { env } from "cloudflare:test";
import { expect, test } from "vitest";
import { api, signup, createPost, connectWs, follow, makeFriends, makeSpace, watch } from "../helpers";

test("chat poll: vote replaces, poll.updated to channel with empty my_votes, non-member 404", async () => {
  const a = await signup(); const b = await signup(); const x = await signup();
  const { generalChannelId } = await makeSpace(a, [b]);
  const m = (await api(a.token).post<any>(`/v1/channels/${generalChannelId}/messages`,
    { client_nonce: "p1", kind: "poll", body: "", poll: { question: "점심?", options: ["국밥", "라멘", "샐러드"] } })).body.message;
  const pid = m.poll.id;
  const wa = await connectWs(a.token);
  const r = await api(b.token).put<any>(`/v1/polls/${pid}/vote`, { option_idxs: [1] });
  expect(r.body.poll).toMatchObject({ voter_count: 1, my_votes: [1], closed: false });
  expect(r.body.poll.options.map((o: any) => o.vote_count)).toEqual([0, 1, 0]);
  const ev = await wa.next("poll.updated");
  expect(ev.seq).not.toBeNull(); expect(ev.data.poll.my_votes).toEqual([]);
  const r2 = await api(b.token).put<any>(`/v1/polls/${pid}/vote`, { option_idxs: [2] });
  expect(r2.body.poll.options.map((o: any) => o.vote_count)).toEqual([0, 0, 1]);
  expect((await api(x.token).get(`/v1/polls/${pid}`)).status).toBe(404);
});
test("single needs exactly 1, multi 1..n distinct in range; retract; close by creator only; closed -> 409", async () => {
  const a = await signup(); const b = await signup();
  const p = await createPost(a, "", { poll: { question: "고르기", options: ["a", "b", "c"], multi: true } });
  const pid = p.poll!.id;
  for (const bad of [[], [0, 0], [3], [-1]]) expect((await api(b.token).put(`/v1/polls/${pid}/vote`, { option_idxs: bad })).status).toBe(400);
  expect((await api(b.token).put<any>(`/v1/polls/${pid}/vote`, { option_idxs: [0, 2] })).body.poll.my_votes).toEqual([0, 2]);
  expect((await api(b.token).del<any>(`/v1/polls/${pid}/vote`)).body.poll).toMatchObject({ voter_count: 0, my_votes: [] });
  expect((await api(b.token).post(`/v1/polls/${pid}/close`)).status).toBe(403);
  expect((await api(a.token).post<any>(`/v1/polls/${pid}/close`)).body.poll.closed).toBe(true);
  expect((await api(b.token).put<any>(`/v1/polls/${pid}/vote`, { option_idxs: [0] })).body.error.code).toBe("poll_closed");
  const s = await createPost(a, "", { poll: { question: "단일", options: ["x", "y"] } });
  expect((await api(b.token).put(`/v1/polls/${s.poll!.id}/vote`, { option_idxs: [0, 1] })).status).toBe(400);
});
test("closes_at in the past counts as closed; post poll.updated on topic", async () => {
  const a = await signup(); const b = await signup();
  const p = await createPost(a, "", { poll: { question: "q", options: ["x", "y"], duration_sec: 60 } });
  const w = await connectWs(a.token); await watch(w, [`post:${p.id}`]);
  await api(b.token).put(`/v1/polls/${p.poll!.id}/vote`, { option_idxs: [0] });
  expect((await w.next("poll.updated")).seq).toBeNull();
  await env.DB.prepare("UPDATE polls SET closes_at=? WHERE id=?").bind(Date.now() - 1, p.poll!.id).run();
  expect((await api(b.token).get<any>(`/v1/polls/${p.poll!.id}`)).body.poll.closed).toBe(true);
  expect((await api(b.token).put(`/v1/polls/${p.poll!.id}/vote`, { option_idxs: [1] })).status).toBe(409);
});
test("concurrent double vote keeps voter_count 1", async () => {
  const a = await signup(); const b = await signup();
  const p = await createPost(a, "", { poll: { question: "q", options: ["x", "y"] } });
  await Promise.all([api(b.token).put(`/v1/polls/${p.poll!.id}/vote`, { option_idxs: [0] }), api(b.token).put(`/v1/polls/${p.poll!.id}/vote`, { option_idxs: [1] })]);
  const poll = (await api(a.token).get<any>(`/v1/polls/${p.poll!.id}`)).body.poll;
  expect(poll.voter_count).toBe(1);
  expect(poll.options.reduce((s: number, o: any) => s + o.vote_count, 0)).toBe(1);
});
test("post poll.updated reaches only watchers who can view the post (blocked / non-follower of a private author get none)", async () => {
  const settle = () => new Promise<void>((r) => setTimeout(r, 150));
  // private author P: follower F gets it, stranger S does not
  const p = await signup(); const f = await signup(); const s = await signup();
  expect((await api(p.token).patch(`/v1/me/settings`, { is_private: true })).status).toBe(200);
  expect(await follow(f, p)).toBe("pending");
  expect((await api(p.token).post(`/v1/me/follow-requests/@${f.handle}/accept`)).status).toBeLessThan(300);
  const pp = await createPost(p, "", { poll: { question: "비밀 질문?", options: ["x", "y"] } });
  const wf = await connectWs(f.token); await watch(wf, [`post:${pp.id}`]);
  const ws = await connectWs(s.token); await watch(ws, [`post:${pp.id}`]);
  expect((await api(s.token).get(`/v1/polls/${pp.poll!.id}`)).status).toBe(404);
  await api(p.token).put(`/v1/polls/${pp.poll!.id}/vote`, { option_idxs: [0] });
  expect((await wf.next("poll.updated")).data.poll.question).toBe("비밀 질문?");
  // public author A blocked by B, and C blocked by A: neither sees updates; an unrelated watcher W does
  const a = await signup(); const b = await signup(); const c = await signup(); const w = await signup();
  const ap = await createPost(a, "", { poll: { question: "공개?", options: ["x", "y"] } });
  const wb = await connectWs(b.token); await watch(wb, [`post:${ap.id}`]);
  const wc = await connectWs(c.token); await watch(wc, [`post:${ap.id}`]);
  const ww = await connectWs(w.token); await watch(ww, [`post:${ap.id}`]);
  expect((await api(b.token).put(`/v1/blocks/@${a.handle}`)).status).toBe(204);
  expect((await api(a.token).put(`/v1/blocks/@${c.handle}`)).status).toBe(204);
  await api(a.token).put(`/v1/polls/${ap.poll!.id}/vote`, { option_idxs: [1] });
  await ww.next("poll.updated");
  await settle();
  for (const cl of [ws, wb, wc]) expect(cl.frames.filter((fr) => fr.type === "poll.updated")).toEqual([]);
});

// ---- pending item 11: a vote is a write in the conversation (api.md 5.19) ----

const settle = () => new Promise<void>((r) => setTimeout(r, 150));
const pollUpdates = (cl: { frames: { type: string }[] }) => cl.frames.filter((fr) => fr.type === "poll.updated");

/** a and b are friends with a 1:1 dm holding a's poll; b has voted [0] while it was writable. */
async function dmPollPair() {
  const a = await signup(); const b = await signup();
  await makeFriends(a, b);
  const ch = (await api(a.token).post<any>("/v1/dms", { users: [`@${b.handle}`] })).body.channel.id as string;
  const m = (await api(a.token).post<any>(`/v1/channels/${ch}/messages`,
    { client_nonce: "dm-poll", kind: "poll", body: "", poll: { question: "몇 시?", options: ["1시", "2시"] } })).body.message;
  const pid = m.poll.id as string;
  expect((await api(b.token).put(`/v1/polls/${pid}/vote`, { option_idxs: [0] })).status).toBe(200);
  return { a, b, ch, pid };
}

test("read-only 1:1 dm (blocked): vote, retract and close -> 403 read_only for both sides, nothing changes, no poll.updated; reading still works", async () => {
  const { a, b, pid } = await dmPollPair();
  expect((await api(a.token).put(`/v1/blocks/@${b.handle}`)).status).toBe(204);
  const wa = await connectWs(a.token); const wb = await connectWs(b.token);

  const vb = await api(b.token).put<any>(`/v1/polls/${pid}/vote`, { option_idxs: [1] });
  expect(vb.status).toBe(403); expect(vb.body.error.code).toBe("read_only");
  const rb = await api(b.token).del<any>(`/v1/polls/${pid}/vote`);
  expect(rb.status).toBe(403); expect(rb.body.error.code).toBe("read_only");
  const va = await api(a.token).put<any>(`/v1/polls/${pid}/vote`, { option_idxs: [1] });
  expect(va.status).toBe(403); expect(va.body.error.code).toBe("read_only");
  const ca = await api(a.token).post<any>(`/v1/polls/${pid}/close`);
  expect(ca.status).toBe(403); expect(ca.body.error.code).toBe("read_only");

  const g = await api(b.token).get<any>(`/v1/polls/${pid}`);
  expect(g.status).toBe(200);
  expect(g.body.poll).toMatchObject({ voter_count: 1, my_votes: [0], closed: false });
  expect(g.body.poll.options.map((o: any) => o.vote_count)).toEqual([1, 0]);
  await settle();
  expect(pollUpdates(wa)).toEqual([]); expect(pollUpdates(wb)).toEqual([]);
  wa.close(); wb.close();
});

test("read-only 1:1 dm (not eligible after unfriend): vote -> 403 read_only reason not_eligible; the creator on the read-only side cannot close", async () => {
  const { a, b, ch, pid } = await dmPollPair();
  // b's own poll in the same dm, created while writable
  const bm = (await api(b.token).post<any>(`/v1/channels/${ch}/messages`,
    { client_nonce: "dm-poll-b", kind: "poll", body: "", poll: { question: "b 질문", options: ["x", "y"] } })).body.message;
  expect((await api(a.token).del(`/v1/friends/@${b.handle}`)).status).toBe(204);
  const r = await api(b.token).put<any>(`/v1/polls/${pid}/vote`, { option_idxs: [1] });
  expect(r.status).toBe(403);
  expect(r.body.error).toMatchObject({ code: "read_only", details: { reason: "not_eligible" } });
  const c = await api(b.token).post<any>(`/v1/polls/${bm.poll.id}/close`);
  expect(c.status).toBe(403); expect(c.body.error.code).toBe("read_only");
  expect((await api(a.token).get<any>(`/v1/polls/${bm.poll.id}`)).body.poll.closed).toBe(false);
});

test("writable 1:1 dm: vote, retract and creator close -> 200", async () => {
  const { a, b, pid } = await dmPollPair();
  const wa = await connectWs(a.token);
  expect((await api(b.token).put<any>(`/v1/polls/${pid}/vote`, { option_idxs: [1] })).body.poll.my_votes).toEqual([1]);
  await wa.next("poll.updated");
  expect((await api(b.token).del<any>(`/v1/polls/${pid}/vote`)).body.poll).toMatchObject({ voter_count: 0, my_votes: [] });
  expect((await api(a.token).post<any>(`/v1/polls/${pid}/close`)).body.poll.closed).toBe(true);
  wa.close();
});

test("post poll of a blocked relation (either direction): vote and retract refused like a like (404, post hidden), counts unchanged; unrelated voter 200", async () => {
  const a = await signup(); const b = await signup(); const c = await signup(); const w = await signup();
  const p = await createPost(a, "", { poll: { question: "공개 투표", options: ["x", "y"] } });
  const pid = p.poll!.id;
  expect((await api(b.token).put(`/v1/polls/${pid}/vote`, { option_idxs: [0] })).status).toBe(200);
  expect((await api(c.token).put(`/v1/polls/${pid}/vote`, { option_idxs: [0] })).status).toBe(200);
  expect((await api(a.token).put(`/v1/blocks/@${b.handle}`)).status).toBe(204); // the author blocked the voter
  expect((await api(c.token).put(`/v1/blocks/@${a.handle}`)).status).toBe(204); // the voter blocked the author
  const wa = await connectWs(a.token); await watch(wa, [`post:${p.id}`]);
  for (const u of [b, c]) {
    const v = await api(u.token).put<any>(`/v1/polls/${pid}/vote`, { option_idxs: [1] });
    expect(v.status).toBe(404); expect(v.body.error.code).toBe("not_found");
    expect((await api(u.token).del(`/v1/polls/${pid}/vote`)).status).toBe(404);
  }
  await settle();
  expect(pollUpdates(wa)).toEqual([]);
  const now = (await api(a.token).get<any>(`/v1/polls/${pid}`)).body.poll;
  expect(now.voter_count).toBe(2);
  expect(now.options.map((o: any) => o.vote_count)).toEqual([2, 0]);
  expect((await api(w.token).put<any>(`/v1/polls/${pid}/vote`, { option_idxs: [1] })).status).toBe(200);
  await wa.next("poll.updated");
  wa.close();
});

test("a creator who left the conversation cannot close the poll (404) and it stays open", async () => {
  // group dm
  const o = await signup(); const x = await signup(); const y = await signup();
  await makeFriends(o, x); await makeFriends(o, y);
  const g = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`, `@${y.handle}`] })).body.channel;
  const gm = (await api(x.token).post<any>(`/v1/channels/${g.id}/messages`,
    { client_nonce: "g-poll", kind: "poll", body: "", poll: { question: "그룹?", options: ["x", "y"] } })).body.message;
  expect((await api(x.token).del(`/v1/channels/${g.id}/members/@me`)).status).toBe(204);
  expect((await api(x.token).post(`/v1/polls/${gm.poll.id}/close`)).status).toBe(404);
  expect((await api(x.token).put(`/v1/polls/${gm.poll.id}/vote`, { option_idxs: [0] })).status).toBe(404);
  expect((await api(o.token).get<any>(`/v1/polls/${gm.poll.id}`)).body.poll.closed).toBe(false);
  // space channel
  const s = await signup(); const m = await signup();
  const { space, generalChannelId } = await makeSpace(s, [m]);
  const sm = (await api(m.token).post<any>(`/v1/channels/${generalChannelId}/messages`,
    { client_nonce: "s-poll", kind: "poll", body: "", poll: { question: "방?", options: ["x", "y"] } })).body.message;
  expect((await api(m.token).del(`/v1/spaces/${space.id}/members/@me`)).status).toBe(204);
  expect((await api(m.token).post(`/v1/polls/${sm.poll.id}/close`)).status).toBe(404);
  expect((await api(s.token).get<any>(`/v1/polls/${sm.poll.id}`)).body.poll.closed).toBe(false);
});
