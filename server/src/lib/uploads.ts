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

/** Result of the upload metadata check: clean, carries metadata (or a block we cannot vouch for), or a broken container. */
export type MetadataCheck = "ok" | "metadata" | "malformed";

/** WebP chunks that carry pixels or animation only; EXIF, "XMP ", ICCP and anything unknown are refused. */
const WEBP_ALLOWED = new Set(["VP8 ", "VP8L", "VP8X", "ALPH", "ANIM", "ANMF"]);
/** Chunks an ANMF frame may hold after its 16-byte frame header: the frame's bitstream and its alpha plane. */
const WEBP_FRAME_ALLOWED = new Set(["VP8 ", "VP8L", "ALPH"]);
const ANMF_HEADER = 16;
/** GIF application extensions that only control looping. */
const GIF_APP_ALLOWED = new Set(["NETSCAPE2.0", "ANIMEXTS1.0"]);

const fourcc = (b: Uint8Array, i: number): string => String.fromCharCode(b[i], b[i + 1], b[i + 2], b[i + 3]);
const le32 = (b: Uint8Array, i: number): number => (b[i] | (b[i + 1] << 8) | (b[i + 2] << 16) | (b[i + 3] << 24)) >>> 0;

/**
 * Walks the RIFF chunks in b[from, to): every chunk (and its padding byte) must end inside the range and the last one
 * must end exactly at `to`. Chunks outside `allowed` make it "metadata"; an ANMF chunk's frame (header + sub-chunks) is
 * walked the same way, so EXIF / XMP / ICCP hidden in a frame are refused too.
 */
function webpChunks(b: Uint8Array, from: number, to: number, allowed: ReadonlySet<string>): MetadataCheck {
  let i = from;
  let chunks = 0;
  let verdict: MetadataCheck = "ok";
  while (i < to) {
    if (i + 8 > to) return "malformed";
    const id = fourcc(b, i);
    const size = le32(b, i + 4);
    const end = i + 8 + size + (size & 1);
    if (end > to) return "malformed";
    if (!allowed.has(id)) verdict = "metadata";
    else if (id === "ANMF") {
      if (size < ANMF_HEADER) return "malformed";
      const frame = webpChunks(b, i + 8 + ANMF_HEADER, i + 8 + size, WEBP_FRAME_ALLOWED);
      if (frame === "malformed") return "malformed";
      if (frame === "metadata") verdict = "metadata";
    }
    chunks++;
    i = end;
  }
  return chunks === 0 ? "malformed" : verdict;
}

function webpCheck(b: Uint8Array): MetadataCheck {
  if (b.length < 12 || le32(b, 4) + 8 !== b.length) return "malformed";
  return webpChunks(b, 12, b.length, WEBP_ALLOWED);
}

/** Skips GIF data sub-blocks starting at i; returns the index after the 0 terminator, or -1 when truncated. */
function gifSkipSubBlocks(b: Uint8Array, i: number): number {
  while (i < b.length) {
    const n = b[i];
    if (n === 0) return i + 1;
    i += 1 + n;
  }
  return -1;
}

/** True when the sub-blocks at i are exactly one block of `size` bytes (whose first byte is `first`, if given). */
function gifOneSubBlock(b: Uint8Array, i: number, size: number, first?: number): boolean {
  return b[i] === size && b[i + 1 + size] === 0 && (first === undefined || b[i + 1] === first);
}

function gifCheck(b: Uint8Array): MetadataCheck {
  if (b.length < 13) return "malformed";
  let i = 13;
  if (b[10] & 0x80) i += 3 * (1 << ((b[10] & 7) + 1)); // global colour table
  let verdict: MetadataCheck = "ok";
  while (i < b.length) {
    const t = b[i];
    if (t === 0x3b) return i + 1 === b.length ? verdict : "malformed"; // trailer must end the file
    if (t === 0x2c) {
      if (i + 10 > b.length) return "malformed";
      const packed = b[i + 9];
      i += 10;
      if (packed & 0x80) i += 3 * (1 << ((packed & 7) + 1)); // local colour table
      i += 1; // LZW minimum code size
      if (i > b.length) return "malformed";
      i = gifSkipSubBlocks(b, i);
      if (i < 0) return "malformed";
      continue;
    }
    if (t === 0x21) {
      if (i + 2 > b.length) return "malformed";
      const label = b[i + 1];
      let next = i + 2;
      if (label === 0xff) {
        if (i + 3 + 11 > b.length || b[i + 2] !== 11) return "malformed";
        const app = String.fromCharCode(...b.subarray(i + 3, i + 14));
        next = i + 14;
        // Only the loop sub-block (id 1, 16-bit count), alone: anything else could carry data.
        if (!GIF_APP_ALLOWED.has(app) || !gifOneSubBlock(b, next, 3, 1)) verdict = "metadata";
      } else if (label === 0xf9) {
        if (!gifOneSubBlock(b, next, 4)) verdict = "metadata"; // a GCE is one 4-byte sub-block
      } else {
        verdict = "metadata"; // 0xFE comment, 0x01 plain text, or an extension we cannot vouch for
      }
      i = gifSkipSubBlocks(b, next);
      if (i < 0) return "malformed";
      continue;
    }
    return "malformed";
  }
  return "malformed"; // no trailer
}

/**
 * Pending item 12: the server stores bytes as-is, so a WebP/GIF that still carries EXIF/XMP/ICC (or a comment /
 * unknown block) is refused rather than trusted to the client. PNG/JPEG are re-encoded by the client (api.md 5.14).
 */
export function imageMetadataCheck(b: Uint8Array, ext: SniffedImage["ext"]): MetadataCheck {
  if (ext === "webp") return webpCheck(b);
  if (ext === "gif") return gifCheck(b);
  return "ok";
}
