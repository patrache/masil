// Guestbook (api.md 5.20, plan 02 Task 12).
import { expect, test } from "vitest";
import { api, signup, follow, makeFriends, type TestUser } from "../helpers";

const gb = async (viewer: TestUser, owner: TestUser) =>
  (await api(viewer.token).get<any>(`/v1/users/@${owner.handle}/guestbook`)).body;

test("write, notify owner, secret entry hidden from third party", async () => {
  const o = await signup();
  const a = await signup();
  const x = await signup();
  const e1 = (await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "놀러왔다 감 👋" })).body.entry;
  const e2 = (await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "비밀", is_secret: true })).body.entry;
  expect((await api(o.token).get<any>("/v1/notifications")).body.items[0]).toMatchObject({
    type: "guestbook_entry",
    subject: { type: "guestbook_entry", id: e2.id },
  });
  const third = (await gb(x, o)).items;
  expect(third.map((e: any) => [e.id, e.hidden, e.body, e.author])).toEqual([
    [e2.id, true, "", null],
    [e1.id, false, "놀러왔다 감 👋", expect.objectContaining({ id: a.user.id })],
  ]);
  expect(third[0]).toMatchObject({ is_secret: true, deleted: false, owner_id: o.user.id });
  expect((await gb(o, o)).items[0]).toMatchObject({ id: e2.id, hidden: false, body: "비밀" });
  expect((await gb(a, o)).items[0].body).toBe("비밀");
  await api(o.token).post(`/v1/users/@me/guestbook`, { body: "내 방명록" });
  expect(
    (await api(o.token).get<any>("/v1/notifications")).body.items.filter((n: any) => n.type === "guestbook_entry").length,
  ).toBe(2);
});

test("blocked -> 403 action_not_allowed; private owner: friend or active follower only", async () => {
  const o = await signup();
  const a = await signup();
  const f = await signup();
  const fr = await signup();
  await api(o.token).put(`/v1/blocks/@${a.handle}`);
  expect((await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "hi" })).body.error.code).toBe(
    "action_not_allowed",
  );
  await api(o.token).patch("/v1/me/settings", { is_private: true });
  const s = await signup();
  expect((await api(s.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "hi" })).body.error.code).toBe(
    "private_account",
  );
  await follow(f, o);
  await api(o.token).post(`/v1/me/follow-requests/@${f.handle}/accept`);
  expect((await api(f.token).post(`/v1/users/@${o.handle}/guestbook`, { body: "hi" })).status).toBe(201);
  await makeFriends(fr, o);
  expect((await api(fr.token).post(`/v1/users/@${o.handle}/guestbook`, { body: "hi" })).status).toBe(201);
  expect((await api(s.token).get<any>(`/v1/users/@${o.handle}/guestbook`)).body.error.code).toBe("private_account");
});

test("delete by author or owner only; body length 1..300", async () => {
  const o = await signup();
  const a = await signup();
  const x = await signup();
  const e = (await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "hi" })).body.entry;
  expect((await api(x.token).del(`/v1/guestbook/${e.id}`)).status).toBe(403);
  expect((await api(o.token).del(`/v1/guestbook/${e.id}`)).status).toBe(204);
  expect((await gb(a, o)).items).toEqual([]);
  expect(
    (await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "가".repeat(301) })).body.error.details.fields
      .body,
  ).toBe("too_long");
  expect((await api(a.token).post(`/v1/users/@${o.handle}/guestbook`, { body: "   " })).status).toBe(400);
});
