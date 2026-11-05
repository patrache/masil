// Bounded request-body reading (api.md 0.3 / 5.14; pending-fixes item 3). Content-Length is a hint, never a bound:
// a client can send a small Content-Length (or none, chunked) with a huge body, so the cap is enforced on the bytes
// actually pulled from the stream, and reading stops as soon as the cap is passed.

/** "absent" when there is no usable Content-Length; otherwise its value (a non-negative integer). */
export function declaredLength(req: Request): number | "absent" | "invalid" {
  const v = req.headers.get("Content-Length");
  if (v === null || v === "") return "absent";
  if (!/^\d+$/.test(v)) return "invalid";
  return Number(v);
}

/**
 * Reads the whole body of `req` into memory, but never more than `max` bytes.
 * Returns null as soon as the body is known to exceed `max` (declared Content-Length over `max`, or more than `max`
 * bytes received) — the stream is cancelled, so the rest is never buffered. A request without a body yields 0 bytes.
 */
export async function readBodyCapped(req: Request, max: number): Promise<Uint8Array | null> {
  const declared = declaredLength(req);
  if (typeof declared === "number" && declared > max) {
    await req.body?.cancel().catch(() => undefined);
    return null;
  }
  if (req.body === null) return new Uint8Array(0);
  const reader = req.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > max) {
        await reader.cancel().catch(() => undefined);
        return null;
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  if (chunks.length === 1) return chunks[0];
  const out = new Uint8Array(size);
  let at = 0;
  for (const ch of chunks) {
    out.set(ch, at);
    at += ch.byteLength;
  }
  return out;
}

/**
 * The same request with its body replaced by `bytes` and Content-Length set to their real length
 * (so a forged header does not survive into the route).
 */
export function withBufferedBody(req: Request, bytes: Uint8Array): Request {
  const headers = new Headers(req.headers);
  headers.set("Content-Length", String(bytes.byteLength));
  headers.delete("Transfer-Encoding");
  return new Request(req, { headers, body: bytes });
}
