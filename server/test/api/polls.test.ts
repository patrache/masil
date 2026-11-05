// test/api/polls.test.ts
import { env } from "cloudflare:test";
import { expect, test } from "vitest";
import { api, signup, createPost, connectWs, follow, makeSpace, watch } from "../helpers";

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
