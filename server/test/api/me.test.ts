import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import { newId } from "../../src/lib/ids";
import { r2Key } from "../../src/lib/uploads";
import { api, connectWs, signup } from "../helpers";

// ---- local fixtures (this file only) ----

type U = { user: { id: string } };

/** Friendship row as the friends service writes it (user_a < user_b). */
async function makeFriendsSql(a: U, b: U): Promise<void> {
  const [lo, hi] = a.user.id < b.user.id ? [a.user.id, b.user.id] : [b.user.id, a.user.id];
  await env.DB.prepare("INSERT INTO friendships (user_a, user_b, created_at) VALUES (?1, ?2, ?3)").bind(lo, hi, Date.now()).run();
}

// 1x1 transparent PNG
const PNG = Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="), (ch) => ch.charCodeAt(0));

/** A pending upload row + its R2 object, as POST /v1/uploads would leave them. */
async function rawUpload(owner: U, purpose: "avatar" | "post" | "story"): Promise<{ id: string; key: string }> {
  const id = newId();
  const key = r2Key(purpose, owner.user.id, id, "png");
  await env.MEDIA.put(key, PNG, { httpMetadata: { contentType: "image/png" }, customMetadata: { upload_id: id, owner_id: owner.user.id, purpose } });
  await env.DB.prepare(
    `INSERT INTO uploads (id, owner_id, purpose, r2_key, content_type, size, width, height, status, created_at)
     VALUES (?1, ?2, ?3, ?4, 'image/png', ?5, 1, 1, 'pending', ?6)`,
  )
    .bind(id, owner.user.id, purpose, key, PNG.byteLength, Date.now())
    .run();
  return { id, key };
}

describe("me", () => {
  it("r2Key layout", () => {
    expect(r2Key("avatar", "U1", "X1", "png")).toBe("avatars/U1/X1.png");
    expect(r2Key("post", "U1", "X1", "jpg")).toBe("posts/U1/X1.jpg");
    expect(r2Key("story", "U1", "X1", "webp")).toBe("stories/U1/X1.webp");
  });

  it("GET /me returns Me", async () => {
    const a = await signup();
    const r = await api(a.token).get<any>("/v1/me");
    expect(r.status).toBe(200);
    expect(r.body.user).toMatchObject({ id: a.user.id, handle: a.handle, settings: { dm_policy: "friends_and_spaces" } });
    expect((await api().get("/v1/me")).status).toBe(401);
  });

  it("profile patch emits user.updated to friends and self", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await makeFriendsSql(a, b);
    const [ca, cb] = await Promise.all([connectWs(a.token), connectWs(b.token)]);
    const r = await api(a.token).patch<any>("/v1/me/profile", { display_name: "새 이름", bio: "안녕" });
    expect(r.body.user).toMatchObject({ display_name: "새 이름", bio: "안녕" });
    expect((await cb.next("user.updated")).data.user.display_name).toBe("새 이름");
    await ca.next("user.updated");
    expect((await api(a.token).patch<any>("/v1/me/profile", { display_name: "가".repeat(33) })).body.error.details.fields.display_name).toBe("too_long");
    expect((await api(a.token).patch<any>("/v1/me/profile", { bio: "가".repeat(161) })).body.error.details.fields.bio).toBe("too_long");
    expect((await api(a.token).patch<any>("/v1/me/profile", { display_name: "   " })).body.error.details.fields.display_name).toBe("too_short");
  });

  it("display name sent as NFD is stored as NFC", async () => {
    const a = await signup();
    const r = await api(a.token).patch<any>("/v1/me/profile", { display_name: "  개발 ".normalize("NFD") });
    expect(r.body.user.display_name).toBe("개발".normalize("NFC"));
  });

  it("avatar attach replaces and removes previous R2 object", async () => {
    const a = await signup();
    const [u1, u2] = [await rawUpload(a, "avatar"), await rawUpload(a, "avatar")];
    await api(a.token).patch("/v1/me/profile", { avatar_upload_id: u1.id });
    const me = (await api(a.token).patch<any>("/v1/me/profile", { avatar_upload_id: u2.id })).body.user;
    expect(me.avatar_url).toBe(`/v1/media/${u2.id}`);
    expect(await env.MEDIA.get(u1.key)).toBeNull();
    expect((await env.DB.prepare("SELECT status FROM uploads WHERE id=?").bind(u1.id).first<any>()).status).toBe("removed");
    expect(await env.DB.prepare("SELECT status, attached_to FROM uploads WHERE id=?").bind(u2.id).first<any>()).toEqual({
      status: "attached",
      attached_to: `avatar:${a.user.id}`,
    });
  });

  it("avatar null removes it; foreign, non-avatar or unknown upload -> 400 invalid", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const mine = await rawUpload(a, "avatar");
    const postUpload = await rawUpload(a, "post");
    const theirs = await rawUpload(b, "avatar");
    await api(a.token).patch("/v1/me/profile", { avatar_upload_id: mine.id });
    for (const id of [postUpload.id, theirs.id, newId(), "nope"]) {
      const r = await api(a.token).patch<any>("/v1/me/profile", { avatar_upload_id: id });
      expect(r.status).toBe(400);
      expect(r.body.error.details.fields.avatar_upload_id).toBe("invalid");
    }
    const cleared = (await api(a.token).patch<any>("/v1/me/profile", { avatar_upload_id: null })).body.user;
    expect(cleared.avatar_url).toBeNull();
    expect(await env.MEDIA.get(mine.key)).toBeNull();
    expect((await env.DB.prepare("SELECT status FROM uploads WHERE id=?").bind(mine.id).first<any>()).status).toBe("removed");
    expect(await env.MEDIA.get(theirs.key)).not.toBeNull(); // untouched
  });

  it("settings patch + settings.updated; status with expiry", async () => {
    const a = await signup();
    const c = await connectWs(a.token);
    const s = await api(a.token).patch<any>("/v1/me/settings", { dm_policy: "friends_only", notify: { social: true }, focus_default_min: 50 });
    expect(s.body.settings).toMatchObject({ dm_policy: "friends_only", notify: { social: true, dm: true }, focus_default_min: 50 });
    await c.next("settings.updated");
    expect((await api(a.token).get<any>("/v1/me/settings")).body.settings).toEqual(s.body.settings);
    const st = await api(a.token).put<any>("/v1/me/status", { emoji: "🎧", text: "듣는 중", expires_in_sec: 3600 });
    expect(st.body.status).toMatchObject({ emoji: "🎧", text: "듣는 중" });
    expect(Date.parse(st.body.status.expires_at) - Date.now()).toBeGreaterThan(3_590_000);
    expect((await api(a.token).put<any>("/v1/me/status", { emoji: "🎧🎧", text: "x" })).status).toBe(400);
    expect((await api(a.token).put<any>("/v1/me/status", { text: "🔨 배포 중" })).status).toBe(200); // emoji optional
    expect((await api(a.token).del("/v1/me/status")).status).toBe(204);
    expect((await api(a.token).get<any>("/v1/me")).body.user.status).toBeNull();
  });

  it("settings validation and is_private reflected in user summary", async () => {
    const a = await signup();
    const bad = async (j: unknown) => (await api(a.token).patch<any>("/v1/me/settings", j)).body.error.details.fields;
    expect(await bad({ dm_policy: "everyone" })).toEqual({ dm_policy: "invalid" });
    expect(await bad({ focus_default_min: 0 })).toEqual({ focus_default_min: "invalid" });
    expect(await bad({ focus_default_min: 721 })).toEqual({ focus_default_min: "invalid" });
    expect(await bad({ notify: { dm: "yes" } })).toEqual({ "notify.dm": "invalid" });
    const r = await api(a.token).patch<any>("/v1/me/settings", { is_private: true, notify: { dm: false } });
    expect(r.body.settings).toMatchObject({ is_private: true, notify: { dm: false, social: false } });
    expect((await api(a.token).get<any>("/v1/me")).body.user.is_private).toBe(true);
  });

  it("status validation, no-expiry status and user.updated to a friend", async () => {
    const [a, f] = await Promise.all([signup(), signup()]);
    await makeFriendsSql(a, f);
    const wf = await connectWs(f.token);
    const bad = async (j: unknown) => (await api(a.token).put<any>("/v1/me/status", j)).body.error.details.fields;
    expect(await bad({ text: "가".repeat(65) })).toEqual({ text: "too_long" });
    expect(await bad({ emoji: "🎧🎧", text: "x" })).toEqual({ emoji: "invalid" });
    expect(await bad({ emoji: "a", text: "x" })).toEqual({ emoji: "invalid" });
    expect(await bad({ text: "x", expires_in_sec: 59 })).toEqual({ expires_in_sec: "invalid" });
    expect(await bad({ text: "x", expires_in_sec: 2_592_001 })).toEqual({ expires_in_sec: "invalid" });
    expect(await bad({ emoji: "🎧" })).toEqual({ text: "required" });
    const ok = await api(a.token).put<any>("/v1/me/status", { emoji: "🇰🇷", text: "", expires_in_sec: null });
    expect(ok.status).toBe(200);
    expect(ok.body.status).toEqual({ emoji: "🇰🇷", text: "", expires_at: null });
    expect((await wf.next("user.updated")).data.user.status).toEqual({ emoji: "🇰🇷", text: "", expires_at: null });
    expect((await api(a.token).del("/v1/me/status")).status).toBe(204);
    expect((await wf.next("user.updated")).data.user.status).toBeNull();
  });
});
