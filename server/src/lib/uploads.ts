// Upload helpers (server-architecture.md 4.13, data-model.md 5). Plan 01 owns r2Key/removeUploads only;
// plan 02 adds sniffImage and the /v1/uploads routes.
import type { Env } from "../env";
import type { Media } from "../types/api";
import { all, jsonList, run } from "./db";
import { now } from "./time";

type UploadPurpose = "avatar" | "post" | "story";

const PURPOSE_DIR: Record<UploadPurpose, string> = { avatar: "avatars", post: "posts", story: "stories" };

/** R2 object key: "<avatars|posts|stories>/<owner_user_id>/<upload_id>.<ext>". */
export function r2Key(purpose: UploadPurpose, ownerId: string, uploadId: string, ext: string): string {
  return `${PURPOSE_DIR[purpose]}/${ownerId}/${uploadId}.${ext}`;
}

/** R2 accepts up to 1000 keys per delete call. */
const R2_DELETE_CHUNK = 1000;

/**
 * Deletes the R2 objects of the given uploads, then marks them status='removed', removed_at=now.
 * Unknown or already removed ids are skipped. R2 is deleted first so a failure leaves the row retryable.
 */
export async function removeUploads(env: Env, uploadIds: string[]): Promise<void> {
  const ids = [...new Set(uploadIds)];
  if (ids.length === 0) return;
  const rows = await all<{ id: string; r2_key: string }>(
    env.DB,
    "SELECT id, r2_key FROM uploads WHERE id IN (SELECT value FROM json_each(?1)) AND status <> 'removed'",
    jsonList(ids),
  );
  if (rows.length === 0) return;
  const keys = rows.map((r) => r.r2_key);
  for (let i = 0; i < keys.length; i += R2_DELETE_CHUNK) await env.MEDIA.delete(keys.slice(i, i + R2_DELETE_CHUNK));
  await run(
    env.DB,
    "UPDATE uploads SET status = 'removed', removed_at = ?2 WHERE id IN (SELECT value FROM json_each(?1)) AND status <> 'removed'",
    jsonList(rows.map((r) => r.id)),
    now(),
  );
}

export interface SniffedImage {
  contentType: Media["content_type"];
  ext: "png" | "jpg" | "gif" | "webp";
  width: number | null;
  height: number | null;
}

function startsWith(b: Uint8Array, at: number, sig: readonly number[]): boolean {
  if (b.length < at + sig.length) return false;
  for (let i = 0; i < sig.length; i++) if (b[at + i] !== sig[i]) return false;
  return true;
}

function ascii(s: string): number[] {
  return [...s].map((ch) => ch.charCodeAt(0));
}

const be16 = (b: Uint8Array, i: number): number => (b[i] << 8) | b[i + 1];
const le16 = (b: Uint8Array, i: number): number => b[i] | (b[i + 1] << 8);
const le24 = (b: Uint8Array, i: number): number => b[i] | (b[i + 1] << 8) | (b[i + 2] << 16);
const be32 = (b: Uint8Array, i: number): number => ((b[i] << 24) | (b[i + 1] << 16) | (b[i + 2] << 8) | b[i + 3]) >>> 0;

const PNG_SIG = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a] as const;
const PNG_IHDR = ascii("IHDR");
const GIF87 = ascii("GIF87a");
const GIF89 = ascii("GIF89a");
const RIFF = ascii("RIFF");
const WEBP = ascii("WEBP");
const VP8_LOSSY = ascii("VP8 ");
const VP8_LOSSLESS = ascii("VP8L");
const VP8_EXTENDED = ascii("VP8X");

/** A positive pixel dimension, or null (0 means the header did not carry a usable size). */
const dim = (n: number): number | null => (n > 0 ? n : null);

function sized(base: Pick<SniffedImage, "contentType" | "ext">, w: number | null, h: number | null): SniffedImage {
  const width = w === null ? null : dim(w);
  const height = h === null ? null : dim(h);
  return width === null || height === null ? { ...base, width: null, height: null } : { ...base, width, height };
}

/** JPEG: walk segment markers from offset 2 to the first SOFn (C0..CF except C4 DHT, C8 JPG, CC DAC). */
function jpegSize(b: Uint8Array): [number, number] | null {
  let i = 2;
  while (i + 4 <= b.length) {
    if (b[i] !== 0xff) return null;
    const marker = b[i + 1];
    if (marker === 0xff) {
      i++; // fill byte
      continue;
    }
    if (marker === 0x01 || (marker >= 0xd0 && marker <= 0xd8)) {
      i += 2; // standalone markers carry no length
      continue;
    }
    if (marker === 0xd9 || marker === 0xda) return null; // EOI / start of scan before any frame header
    const segLen = be16(b, i + 2);
    if (segLen < 2) return null;
    if (marker >= 0xc0 && marker <= 0xcf && marker !== 0xc4 && marker !== 0xc8 && marker !== 0xcc) {
      if (i + 9 > b.length) return null;
      return [be16(b, i + 7), be16(b, i + 5)]; // [width, height]; SOF: len(2) precision(1) height(2) width(2)
    }
    i += 2 + segLen;
  }
  return null;
}

/**
 * Detects PNG/JPEG/GIF/WebP from magic numbers and reads the pixel size from the header (api.md 5.14).
 * Unknown format -> null. Known format whose size cannot be read -> width/height null.
 */
export function sniffImage(head: Uint8Array): SniffedImage | null {
  const b = head;
  if (startsWith(b, 0, PNG_SIG)) {
    const base = { contentType: "image/png", ext: "png" } as const;
    if (b.length < 24 || !startsWith(b, 12, PNG_IHDR)) return sized(base, null, null);
    return sized(base, be32(b, 16), be32(b, 20));
  }
  if (startsWith(b, 0, [0xff, 0xd8, 0xff])) {
    const size = jpegSize(b);
    return sized({ contentType: "image/jpeg", ext: "jpg" }, size?.[0] ?? null, size?.[1] ?? null);
  }
  if (startsWith(b, 0, GIF87) || startsWith(b, 0, GIF89)) {
    const base = { contentType: "image/gif", ext: "gif" } as const;
    if (b.length < 10) return sized(base, null, null);
    return sized(base, le16(b, 6), le16(b, 8));
  }
  if (startsWith(b, 0, RIFF) && startsWith(b, 8, WEBP)) {
    const base = { contentType: "image/webp", ext: "webp" } as const;
    if (startsWith(b, 12, VP8_LOSSY) && b.length >= 30 && startsWith(b, 23, [0x9d, 0x01, 0x2a])) {
      return sized(base, le16(b, 26) & 0x3fff, le16(b, 28) & 0x3fff);
    }
    if (startsWith(b, 12, VP8_LOSSLESS) && b.length >= 25 && b[20] === 0x2f) {
      const bits = (b[21] | (b[22] << 8) | (b[23] << 16) | (b[24] << 24)) >>> 0;
      return sized(base, (bits & 0x3fff) + 1, ((bits >>> 14) & 0x3fff) + 1);
    }
    if (startsWith(b, 12, VP8_EXTENDED) && b.length >= 30) {
      return sized(base, le24(b, 24) + 1, le24(b, 27) + 1);
    }
    return sized(base, null, null);
  }
  return null;
}
