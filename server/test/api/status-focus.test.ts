// Status message and focus mode (api.md 5.1, plan 02 Task 13).
import { expect, test } from "vitest";
import { env } from "cloudflare:test";
import { api, signup, connectWs, makeFriends } from "../helpers";

test("status put/delete, user.updated to friend, expiry hides it", async () => {
  const a = await signup(); const f = await signup();
  await makeFriends(a, f);
  const wf = await connectWs(f.token);
  const r = await api(a.token).put<any>("/v1/me/status", { emoji: "🎧", text: "듣는 중…", expires_in_sec: 3600 });
  expect(r.body.status).toMatchObject({ emoji: "🎧", text: "듣는 중…" });
  expect(Date.parse(r.body.status.expires_at) - Date.now()).toBeGreaterThan(3_590_000);
  expect((await wf.next("user.updated")).data.user.status.text).toBe("듣는 중…");
  await env.DB.prepare("UPDATE users SET status_expires_at=? WHERE id=?").bind(Date.now() - 1, a.user.id).run();
  expect((await api(f.token).get<any>(`/v1/users/@${a.handle}`)).body.user.status).toBeNull();
  await api(a.token).put("/v1/me/status", { text: "🔨 배포 중" });
  expect((await api(a.token).del("/v1/me/status")).status).toBe(204);
  expect((await api(a.token).get<any>("/v1/me")).body.user.status).toBeNull();
});
test("status validation", async () => {
  const a = await signup();
  const bad = async (j: any) => (await api(a.token).put<any>("/v1/me/status", j)).body.error.details.fields;
  expect(await bad({ text: "가".repeat(65) })).toEqual({ text: "too_long" });
  expect(await bad({ emoji: "🎧🎧", text: "x" })).toEqual({ emoji: "invalid" });
  expect(await bad({ emoji: "a", text: "x" })).toEqual({ emoji: "invalid" });
  expect(await bad({ text: "x", expires_in_sec: 59 })).toEqual({ expires_in_sec: "invalid" });
  expect((await api(a.token).put("/v1/me/status", { emoji: "🇰🇷", text: "" })).status).toBe(200);
});
test("focus: default duration, suppressed alerts, early end summary, 409 when not focusing", async () => {
  const a = await signup(); const b = await signup();
  const r = await api(a.token).post<any>("/v1/me/focus", {});
  expect(Date.parse(r.body.focus.ends_at) - Date.parse(r.body.focus.started_at)).toBe(25 * 60_000);
  expect((await api(a.token).get<any>("/v1/me/focus")).body.focus).toEqual(r.body.focus);
  const w = await connectWs(a.token);
  await api(b.token).post("/v1/friends/requests", { user: `@${a.handle}` });
  expect((await w.next("alert")).data.alert).toMatchObject({ kind: "friend_request", suppressed: true, suppressed_reason: "focus" });
  const end = await api(a.token).del<any>("/v1/me/focus");
  expect(end.body.summary.counts.friend_request).toBe(1);
  expect(end.body.summary.items.length).toBe(1);
  expect((await w.next("focus.ended")).data.summary.counts.friend_request).toBe(1);
  // early end must also emit the focus_summary alert: it is the client's only trigger for the summary overlay (CA 9.1)
  const sa = await w.next("alert", (d: any) => d.alert.kind === "focus_summary");
  expect(sa.data.alert).toMatchObject({ kind: "focus_summary", suppressed: false });
  expect(sa.data.alert.title).toMatch(/^집중 모드 끝/);
  const again = await api(a.token).del<any>("/v1/me/focus");
  expect(again.status).toBe(409); expect(again.body.error.details.reason).toBe("not_focusing");
  expect((await api(a.token).get<any>("/v1/me/focus")).body.focus).toBeNull();
});
test("focus duration bounds and restart replaces ends_at", async () => {
  const a = await signup();
  expect((await api(a.token).post("/v1/me/focus", { duration_sec: 59 })).status).toBe(400);
  expect((await api(a.token).post("/v1/me/focus", { duration_sec: 43_201 })).status).toBe(400);
  const f1 = (await api(a.token).post<any>("/v1/me/focus", { duration_sec: 60 })).body.focus;
  const f2 = (await api(a.token).post<any>("/v1/me/focus", { duration_sec: 120 })).body.focus;
  expect(Date.parse(f2.ends_at)).toBeGreaterThan(Date.parse(f1.ends_at));
  expect((await api(a.token).get<any>("/v1/me")).body.user.presence.status).toBe("dnd");
});
