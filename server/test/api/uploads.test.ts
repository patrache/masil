import { env } from "cloudflare:test";
import { expect, test } from "vitest";
import { newId } from "../../src/lib/ids";
import { api, signup, uploadPng, PNG_1X1 } from "../helpers";

const post = (tok: string, purpose: string, body: Uint8Array, ct = "image/png") =>
  api(tok).raw(`/v1/uploads?purpose=${purpose}`, { method: "POST", headers: { "Content-Type": ct }, body });

test("upload stores R2 object with metadata and returns pending Upload", async () => {
  const a = await signup();
  const up = await uploadPng(a, "post");
  expect(up).toMatchObject({ purpose: "post", status: "pending", content_type: "image/png", size: PNG_1X1.length, width: 1, height: 1, url: `/v1/media/${up.id}` });
  expect(Object.keys(up).sort()).toEqual(["content_type", "created_at", "height", "id", "purpose", "size", "status", "url", "width"]);
  expect(typeof up.created_at).toBe("string");
  const row = await env.DB.prepare("SELECT r2_key FROM uploads WHERE id=?").bind(up.id).first<{ r2_key: string }>();
  expect(row!.r2_key).toBe(`posts/${a.user.id}/${up.id}.png`);
  const obj = await env.MEDIA.head(row!.r2_key);
  expect(obj!.httpMetadata!.contentType).toBe("image/png");
  expect(obj!.customMetadata).toEqual({ upload_id: up.id, owner_id: a.user.id, purpose: "post" });
});
test("content-type mismatch -> 415, unknown bytes -> 415, >5MB -> 413, bad purpose -> 400", async () => {
  const a = await signup();
  expect((await post(a.token, "post", PNG_1X1, "image/jpeg")).status).toBe(415);
  expect((await post(a.token, "post", new TextEncoder().encode("not an image"))).status).toBe(415);
  const big = new Uint8Array(5_242_881); big.set(PNG_1X1);
  const r = await post(a.token, "post", big);
  expect(r.status).toBe(413);
  expect((await r.json() as any).error.code).toBe("payload_too_large");
  expect((await post(a.token, "banner", PNG_1X1)).status).toBe(400);
});
test("POST /v1/uploads never parses a JSON-typed body: unauth + application/json >64KB -> 401, not invalid_json", async () => {
  const body = "{" + "a".repeat(70_000); // invalid JSON: if the global JSON check read it, this would be 400 invalid_json
  const r = await api().raw("/v1/uploads?purpose=post", { method: "POST", headers: { "Content-Type": "application/json" }, body });
  expect(r.status).toBe(401);
  expect((await r.json() as any).error.code).not.toBe("invalid_json");
  const authed = await signup();
  const r2 = await post(authed.token, "post", new TextEncoder().encode(body), "application/json");
  expect(r2.status).toBe(415);
});
test("21st pending upload -> 422 pending_uploads", async () => {
  const a = await signup();
  await env.DB.prepare(`INSERT INTO uploads (id, owner_id, purpose, r2_key, content_type, size, status, created_at)
    SELECT 'P' || ?1 || value, ?1, 'post', 'k/' || ?1 || '/' || value, 'image/png', 1, 'pending', ?2 FROM json_each(?3)`)
    .bind(a.user.id, Date.now(), JSON.stringify([...Array(20).keys()].map(String))).run();
  const r = await post(a.token, "post", PNG_1X1);
  expect(r.status).toBe(422);
  expect((await r.json() as any).error.details).toEqual({ what: "pending_uploads", limit: 20 });
});
test("media: pending visible to owner only; ETag/304; cache headers", async () => {
  const a = await signup(); const b = await signup();
  const up = await uploadPng(a, "post");
  const r = await api(a.token).raw(`/v1/media/${up.id}`, { method: "GET" });
  expect(r.status).toBe(200);
  expect(new Uint8Array(await r.arrayBuffer())).toEqual(PNG_1X1);
  expect(r.headers.get("Cache-Control")).toBe("private, max-age=31536000, immutable");
  const etag = r.headers.get("ETag")!;
  expect((await api(a.token).raw(`/v1/media/${up.id}`, { method: "GET", headers: { "If-None-Match": etag } })).status).toBe(304);
  expect((await api(b.token).raw(`/v1/media/${up.id}`, { method: "GET" })).status).toBe(404);
});
test("delete pending upload: owner only, removes R2 object", async () => {
  const a = await signup(); const b = await signup();
  const up = await uploadPng(a, "avatar");
  expect((await api(b.token).del(`/v1/uploads/${up.id}`)).status).toBe(404);
  expect((await api(a.token).del(`/v1/uploads/${up.id}`)).status).toBe(204);
  expect(await env.MEDIA.head(`avatars/${a.user.id}/${up.id}.png`)).toBeNull();
  expect((await api(a.token).raw(`/v1/media/${up.id}`, { method: "GET" })).status).toBe(404);
});
test("media of attached uploads: avatar anyone, post by 3.4 rules, story only while unexpired", async () => {
  const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
  const media = (tok: string, id: string) => api(tok).raw(`/v1/media/${id}`, { method: "GET" });
  const attach = (id: string, to: string) =>
    env.DB.prepare("UPDATE uploads SET status='attached', attached_to=? WHERE id=?").bind(to, id).run();
  const [avatar, postImg, storyImg] = await Promise.all([uploadPng(a, "avatar"), uploadPng(a, "post"), uploadPng(a, "story")]);
  await attach(avatar.id, `avatar:${a.user.id}`);
  expect((await media(b.token, avatar.id)).status).toBe(200);

  const postId = newId(); const t = Date.now();
  await env.DB.prepare("INSERT INTO posts (id, author_id, body, created_at) VALUES (?, ?, 'hi', ?)").bind(postId, a.user.id, t).run();
  await attach(postImg.id, `post:${postId}`);
  expect((await media(b.token, postImg.id)).status).toBe(200);
  await env.DB.prepare("INSERT INTO blocks (blocker_id, blocked_id, created_at) VALUES (?, ?, ?)").bind(a.user.id, c.user.id, t).run();
  expect((await media(c.token, postImg.id)).status).toBe(404);
  await env.DB.prepare("UPDATE users SET is_private = 1 WHERE id = ?").bind(a.user.id).run();
  expect((await media(b.token, postImg.id)).status).toBe(404);
  expect((await media(a.token, postImg.id)).status).toBe(200);
  await env.DB.prepare("UPDATE users SET is_private = 0 WHERE id = ?").bind(a.user.id).run();

  const storyId = newId();
  await env.DB.prepare("INSERT INTO stories (id, author_id, media_upload_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?)")
    .bind(storyId, a.user.id, storyImg.id, t, t + 86_400_000).run();
  await attach(storyImg.id, `story:${storyId}`);
  expect((await media(b.token, storyImg.id)).status).toBe(200);
  await env.DB.prepare("UPDATE stories SET expires_at = ? WHERE id = ?").bind(t - 1, storyId).run();
  expect((await media(b.token, storyImg.id)).status).toBe(404);
});
