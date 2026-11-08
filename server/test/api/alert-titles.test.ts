// v0.1.1 D8: alert titles attach '님' to the actor's display name ("하나님의 친구 요청", not "하나 님의 친구 요청").
import { describe, it, expect } from "vitest";
import { api, connectWs, follow, signup } from "../helpers";

describe("alert titles attach 님 to the name (D8)", () => {
  it("friend request, follow and guestbook alerts", async () => {
    const [a, b] = await Promise.all([signup({ displayName: "하나" }), signup()]);
    expect((await api(b.token).patch("/v1/me/settings", { notify: { social: true } })).status).toBe(200); // social alerts default off
    const cb = await connectWs(b.token);
    const name = a.user.display_name;
    expect(name).toBe("하나");

    expect((await api(a.token).post("/v1/friends/requests", { user: `@${b.handle}` })).status).toBe(201);
    expect((await cb.next("alert", (d: any) => d.alert.kind === "friend_request")).data.alert.title).toBe("하나님의 친구 요청");

    expect(await follow(a, b)).toBe("active");
    expect((await cb.next("alert", (d: any) => d.alert.kind === "social")).data.alert.title).toBe("하나님이 팔로우했어요");

    expect((await api(a.token).post(`/v1/users/@${b.handle}/guestbook`, { body: "놀러왔어요" })).status).toBe(201);
    expect((await cb.next("alert", (d: any) => d.alert.title.includes("방명록"))).data.alert.title).toBe("하나님의 방명록");
  });
});
