// Uploads and media (api.md 5.14, data-model.md 3.6 / 5): POST /uploads, DELETE /uploads/:upload_id,
// GET /media/:upload_id. The upload body is raw image bytes (app.ts skips the 64KB JSON cap for this route;
// the 5MB cap here counts received bytes, see lib/body.ts).
import { Hono } from "hono";
import * as z from "zod";
import type { AppContext, AppEnv } from "../env";
import { getAuth, isSuspendedNow, requireAuth } from "../lib/auth";
import { declaredLength, readBodyCapped } from "../lib/body";
import { first, run } from "../lib/db";
import { ApiError, err } from "../lib/errors";
import { isId, newId } from "../lib/ids";
import { canViewPost, canViewUserContent } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { serializeUpload } from "../lib/serialize";
import { now } from "../lib/time";
import { imageMetadataCheck, r2Key, removeUploads, sniffImage } from "../lib/uploads";
import { vQuery } from "../lib/validate";
import type { PostRow, StoryRow, UploadRow, UserRow } from "../types/rows";

/** api.md 0.3 / 5.14: 1..5,242,880 bytes per image. */
export const UPLOAD_MAX_BYTES = 5_242_880;
/** api.md 5.14: at most 20 pending uploads per user. */
export const PENDING_UPLOADS_MAX = 20;
const MEDIA_CACHE_CONTROL = "private, max-age=31536000, immutable";

const UploadQuery = z.object({ purpose: z.enum(["avatar", "post", "story"]) });

function tooLarge(): ApiError {
  return new ApiError(413, "payload_too_large", `upload exceeds ${UPLOAD_MAX_BYTES} bytes`);
}

function unsupported(): ApiError {
  return new ApiError(415, "unsupported_media_type", "unsupported or mismatched image type");
}

/** Media type part of a Content-Type header, lowercased ("image/png; x=y" -> "image/png"). */
function mediaType(header: string | undefined): string | null {
  if (!header) return null;
  return header.split(";")[0].trim().toLowerCase() || null;
}

export const uploadRoutes = new Hono<AppEnv>();

uploadRoutes.post("/uploads", requireAuth, limit("RL_UPLOAD"), vQuery(UploadQuery), async (c) => {
  const me = getAuth(c);
  const { purpose } = c.req.valid("query");

  // Size guard: a declared Content-Length over the cap is refused before reading; the body itself is then read with a
  // running byte count (the header can lie), so at most UPLOAD_MAX_BYTES (+ one chunk) is ever buffered.
  const declared = declaredLength(c.req.raw);
  if (declared === "absent") throw err.invalid({ "content-length": "required" });
  if (declared === "invalid") throw err.invalid({ "content-length": "invalid" });
  if (declared > UPLOAD_MAX_BYTES) throw tooLarge();
  if (declared === 0) throw err.invalid({ body: "required" });

  const bytes = await readBodyCapped(c.req.raw, UPLOAD_MAX_BYTES);
  if (bytes === null) throw tooLarge();
  if (bytes.byteLength === 0) throw err.invalid({ body: "required" });

  const sniffed = sniffImage(bytes);
  if (sniffed === null || sniffed.contentType !== mediaType(c.req.header("Content-Type"))) throw unsupported();
  // Pending item 12: WebP/GIF metadata (EXIF GPS, XMP, ICC, comments) is refused, broken containers too.
  const meta = imageMetadataCheck(bytes, sniffed.ext);
  if (meta === "metadata") throw err.invalid({ body: "not_allowed" });
  if (meta === "malformed") throw err.invalid({ body: "invalid" });

  // Cheap pre-check so a user at the cap does not write to R2; the INSERT below re-checks atomically.
  const pending = await first<{ n: number }>(
    c.env.DB,
    "SELECT COUNT(*) AS n FROM uploads WHERE owner_id = ?1 AND status = 'pending'",
    me.userId,
  );
  if ((pending?.n ?? 0) >= PENDING_UPLOADS_MAX) throw err.limit("pending_uploads", PENDING_UPLOADS_MAX);

  const id = newId();
  const key = r2Key(purpose, me.userId, id, sniffed.ext);
  await c.env.MEDIA.put(key, bytes, {
    httpMetadata: { contentType: sniffed.contentType },
    customMetadata: { upload_id: id, owner_id: me.userId, purpose },
  });

  const createdAt = now();
  const res = await run(
    c.env.DB,
    `INSERT INTO uploads (id, owner_id, purpose, r2_key, content_type, size, width, height, status, created_at)
     SELECT ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, 'pending', ?9
      WHERE (SELECT COUNT(*) FROM uploads WHERE owner_id = ?2 AND status = 'pending') < ?10`,
    id,
    me.userId,
    purpose,
    key,
    sniffed.contentType,
    bytes.byteLength,
    sniffed.width,
    sniffed.height,
    createdAt,
    PENDING_UPLOADS_MAX,
  );
  if ((res.meta.changes ?? 0) === 0) {
    // A concurrent upload took the last pending slot: drop the orphan object.
    await c.env.MEDIA.delete(key);
    throw err.limit("pending_uploads", PENDING_UPLOADS_MAX);
  }

  const row: UploadRow = {
    id,
    owner_id: me.userId,
    purpose,
    r2_key: key,
    content_type: sniffed.contentType,
    size: bytes.byteLength,
    width: sniffed.width,
    height: sniffed.height,
    status: "pending",
    attached_to: null,
    created_at: createdAt,
    removed_at: null,
  };
  return c.json({ upload: serializeUpload(row) }, 201);
});

uploadRoutes.delete("/uploads/:upload_id", requireAuth, async (c) => {
  const me = getAuth(c);
  const id = c.req.param("upload_id");
  if (!isId(id)) throw err.notFound("upload");
  const row = await first<{ id: string }>(
    c.env.DB,
    "SELECT id FROM uploads WHERE id = ?1 AND owner_id = ?2 AND status = 'pending'",
    id,
    me.userId,
  );
  if (row === null) throw err.notFound("upload");
  await removeUploads(c.env, [id]);
  return c.body(null, 204);
});

/** api.md 5.14 media access: avatar -> anyone, except a suspended owner's avatar -> only the owner and operators (D1,
 *  3.9); post -> 3.4; story -> story rules + unexpired; pending -> owner only. */
async function canViewMedia(c: AppContext, viewerId: string, up: UploadRow): Promise<boolean> {
  const db = c.env.DB;
  if (up.status === "removed") return false;
  if (up.status === "pending") return up.owner_id === viewerId;
  const target = up.attached_to ?? "";
  const sep = target.indexOf(":");
  if (sep < 0) return false;
  const kind = target.slice(0, sep);
  const targetId = target.slice(sep + 1);
  if (kind === "avatar") {
    if (up.owner_id === viewerId || getAuth(c).role === "admin") return true;
    const owner = await first<Pick<UserRow, "suspended_at" | "suspended_until">>(
      db,
      "SELECT suspended_at, suspended_until FROM users WHERE id = ?1",
      up.owner_id,
    );
    return owner !== null && !isSuspendedNow(owner, now());
  }
  if (kind === "post") {
    const post = await first<PostRow>(db, "SELECT * FROM posts WHERE id = ?1 AND deleted_at IS NULL", targetId);
    return post !== null && (await canViewPost(db, viewerId, post));
  }
  if (kind === "story") {
    // canViewUserContent covers api.md 3.4 rules 2-4 (author suspended now, block either way, private rule).
    const story = await first<Pick<StoryRow, "author_id">>(
      db,
      "SELECT author_id FROM stories WHERE id = ?1 AND deleted_at IS NULL AND expires_at > ?2",
      targetId,
      now(),
    );
    if (story === null) return false;
    return (await canViewUserContent(db, viewerId, story.author_id)) === "ok";
  }
  return false;
}

/** R2 get with onlyIf returns an R2Object without a body when the precondition fails. */
function hasBody(obj: R2Object | R2ObjectBody): obj is R2ObjectBody {
  return "body" in obj;
}

uploadRoutes.get("/media/:upload_id", requireAuth, async (c) => {
  const me = getAuth(c);
  const id = c.req.param("upload_id");
  if (!isId(id)) throw err.notFound("media");
  const up = await first<UploadRow>(c.env.DB, "SELECT * FROM uploads WHERE id = ?1", id);
  if (up === null || !(await canViewMedia(c, me.userId, up))) throw err.notFound("media");

  // Only If-None-Match is honoured (api.md 5.14): a failed precondition then always means 304.
  const ifNoneMatch = c.req.header("If-None-Match");
  const obj = ifNoneMatch
    ? await c.env.MEDIA.get(up.r2_key, { onlyIf: new Headers({ "If-None-Match": ifNoneMatch }) })
    : await c.env.MEDIA.get(up.r2_key);
  if (obj === null) throw err.notFound("media");
  const headers = new Headers();
  headers.set("ETag", obj.httpEtag);
  headers.set("Cache-Control", MEDIA_CACHE_CONTROL);
  if (!hasBody(obj)) {
    // Precondition (If-None-Match) matched: no body.
    return new Response(null, { status: 304, headers });
  }
  obj.writeHttpMetadata(headers);
  if (!headers.has("Content-Type")) headers.set("Content-Type", up.content_type);
  headers.set("Content-Length", String(obj.size));
  return new Response(obj.body, { status: 200, headers });
});
