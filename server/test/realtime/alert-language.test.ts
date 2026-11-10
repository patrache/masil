// i18n 7.3 / events.md 4.7 "alert.template": an alert is stored once (template + Korean fallback) and its title/body are
// rendered per connection from that connection's Accept-Language at send time — live, on resume, and in focus summaries.
import { describe, it, expect } from "vitest";
import { api, connectWs, follow, makeFriends, signup } from "../helpers";

async function socialOn(token: string): Promise<void> {
  expect((await api(token).patch("/v1/me/settings", { notify: { social: true } })).status).toBe(200); // social alerts default off
}

describe("alert fallback text follows the connection language", () => {
  it("en connection gets English, a header-less (0.1.2) connection gets the 0.1.2 Korean; both carry the template", async () => {
    const [a, b] = await Promise.all([signup({ displayName: "하나" }), signup()]);
    await socialOn(b.token);
    const en = await connectWs(b.token, { headers: { "Accept-Language": "en" } });
    const old = await connectWs(b.token);
    expect(await follow(a, b)).toBe("active");
    const fe = (await en.next("alert", (d: any) => d.alert.kind === "social")).data.alert;
    const fo = (await old.next("alert", (d: any) => d.alert.kind === "social")).data.alert;
    expect(fe.id).toBe(fo.id);
    expect(fe.title).toBe("하나 followed you");
    expect(fo.title).toBe("하나님이 팔로우했어요");
    expect(fe.template).toEqual({ id: "social.follow", params: { actor: "하나" } });
    expect(fo.template).toEqual({ id: "social.follow", params: { actor: "하나" } });
  });

  it("anything but exactly ko / en / zh-Hans reads as ko; zh-Hans gets its own text", async () => {
    const [a, b] = await Promise.all([signup({ displayName: "하나" }), signup()]);
    const us = await connectWs(b.token, { headers: { "Accept-Language": "en-US" } });
    const zh = await connectWs(b.token, { headers: { "Accept-Language": "zh-Hans" } });
    expect((await api(a.token).post("/v1/friends/requests", { user: `@${b.handle}` })).status).toBe(201);
    const ue = (await us.next("alert", (d: any) => d.alert.kind === "friend_request")).data.alert;
    const ze = (await zh.next("alert", (d: any) => d.alert.kind === "friend_request")).data.alert;
    expect(ue.title).toBe("하나님의 친구 요청");
    expect(ze.title).not.toBe("하나님의 친구 요청");
    expect(ze.title).toContain("하나");
    expect(ze.template).toEqual({ id: "social.friend_request", params: { actor: "하나" } });
  });

  it("resume replays the logged alert rendered in the new connection's language", async () => {
    const [a, b] = await Promise.all([signup({ displayName: "하나" }), signup()]);
    await socialOn(b.token);
    const first = await connectWs(b.token);
    const hello = first.frames.find((f) => f.type === "hello")!.data;
    expect(await follow(a, b)).toBe("active");
    const live = await first.next("alert", (d: any) => d.alert.kind === "social");
    expect(live.data.alert.title).toBe("하나님이 팔로우했어요");
    first.close();
    const again = await connectWs(b.token, {
      headers: { "Accept-Language": "en" },
      resume: { epoch: hello.epoch, last_seq: hello.seq },
    });
    const replayed = await again.next("alert", (d: any) => d.alert.id === live.data.alert.id);
    expect(replayed.seq).toBe(live.seq);
    expect(replayed.data.alert.title).toBe("하나 followed you");
  });

  it("a message alert keeps its verbatim body and renders the title per language", async () => {
    const [a, b] = await Promise.all([signup({ displayName: "하나" }), signup()]);
    await makeFriends(a, b);
    const dm = await api(a.token).post<any>("/v1/dms", { users: [`@${b.handle}`] });
    expect([200, 201]).toContain(dm.status);
    const en = await connectWs(b.token, { headers: { "Accept-Language": "en" } });
    const ko = await connectWs(b.token, { headers: { "Accept-Language": "ko" } });
    const sent = await api(a.token).post(`/v1/channels/${dm.body.channel.id}/messages`, { client_nonce: crypto.randomUUID(), body: "안녕 **반가워**" });
    expect(sent.status).toBe(201);
    const ae = (await en.next("alert", (d: any) => d.alert.kind === "dm")).data.alert;
    const ak = (await ko.next("alert", (d: any) => d.alert.kind === "dm")).data.alert;
    expect(ae.template).toEqual({ id: "message.dm", params: { actor: "하나" } });
    expect(ak.title).toBe("하나");
    expect(ae.title).toBe("하나");
    expect(ae.body).toBe(ak.body);
    expect(ak.body).toBe("안녕 반가워");
  });

  it("focus summary: DELETE /me/focus items and the focus.ended / focus_summary frames follow each request / connection", async () => {
    const [a, b] = await Promise.all([signup(), signup({ displayName: "하나" })]);
    expect((await api(a.token).post("/v1/me/focus", { duration_sec: 600 })).status).toBe(200);
    const en = await connectWs(a.token, { headers: { "Accept-Language": "en" } });
    const ko = await connectWs(a.token);
    expect((await api(b.token).post("/v1/friends/requests", { user: `@${a.handle}` })).status).toBe(201);
    expect((await en.next("alert")).data.alert).toMatchObject({ suppressed: true, title: "하나 sent you a friend request" });
    const end = await api(a.token).raw("/v1/me/focus", { method: "DELETE", headers: { "Accept-Language": "en" } });
    expect(end.status).toBe(200);
    const summary = ((await end.json()) as any).summary;
    expect(summary.items.map((i: any) => i.title)).toEqual(["하나 sent you a friend request"]);
    expect(summary.items[0].template).toEqual({ id: "social.friend_request", params: { actor: "하나" } });

    const fe = await en.next("focus.ended");
    const fk = await ko.next("focus.ended");
    expect(fe.data.summary.items[0].title).toBe("하나 sent you a friend request");
    expect(fk.data.summary.items[0].title).toBe("하나님의 친구 요청");
    const se = (await en.next("alert", (d: any) => d.alert.kind === "focus_summary")).data.alert;
    const sk = (await ko.next("alert", (d: any) => d.alert.kind === "focus_summary")).data.alert;
    expect(sk.title).toBe("집중 모드 끝 — 멘션 0, DM 0");
    expect(sk.body).toBe("스레드 답글 0 · 초대 0 · 친구 요청 1 · 기타 0");
    expect(se.title).not.toBe(sk.title);
    expect(se.template).toEqual({
      id: "focus.summary",
      params: { mention: 0, dm: 0, thread_reply: 0, invite: 0, friend_request: 1, other: 0 },
    });
  });
});
