// Password hashing (PBKDF2-HMAC-SHA256), session tokens and SHA-256 hex (api.md 1.2, 1.3).

const ALGO = "pbkdf2_sha256";
const ITERATIONS = 100_000; // Workers' PBKDF2 iteration cap (workerd rejects more with NotSupportedError)
const SALT_BYTES = 16;
const HASH_BYTES = 32;
const B64URL_RE = /^[A-Za-z0-9_-]+$/;

// Fixed, well-formed hash (all-zero salt and digest) so unknown-handle logins pay one full derivation.
const DUMMY_HASH = [ALGO, String(ITERATIONS), "A".repeat(22), "A".repeat(43)].join("$");

function toBase64Url(bytes: Uint8Array): string {
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function fromBase64Url(s: string): Uint8Array {
  const b64 = s.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat((4 - (s.length % 4)) % 4);
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

async function derive(password: string, salt: Uint8Array, iterations: number): Promise<Uint8Array> {
  const key = await crypto.subtle.importKey("raw", new TextEncoder().encode(password), "PBKDF2", false, ["deriveBits"]);
  const bits = await crypto.subtle.deriveBits({ name: "PBKDF2", hash: "SHA-256", salt, iterations }, key, HASH_BYTES * 8);
  return new Uint8Array(bits);
}

/** "pbkdf2_sha256$100000$<salt b64url>$<hash b64url>" (no padding). */
export async function hashPassword(password: string): Promise<string> {
  const salt = crypto.getRandomValues(new Uint8Array(SALT_BYTES));
  const hash = await derive(password, salt, ITERATIONS);
  return [ALGO, String(ITERATIONS), toBase64Url(salt), toBase64Url(hash)].join("$");
}

/** Uses the stored iteration count; any malformed input is false (never throws). */
export async function verifyPassword(password: string, stored: string): Promise<boolean> {
  try {
    const parts = stored.split("$");
    if (parts.length !== 4 || parts[0] !== ALGO) return false;
    const [, iterStr, saltStr, hashStr] = parts;
    if (!/^[1-9][0-9]{0,5}$/.test(iterStr)) return false;
    const iterations = Number(iterStr);
    if (iterations > ITERATIONS) return false;
    if (!B64URL_RE.test(saltStr) || !B64URL_RE.test(hashStr)) return false;
    const salt = fromBase64Url(saltStr);
    const expected = fromBase64Url(hashStr);
    if (expected.length !== HASH_BYTES) return false;
    const actual = await derive(password, salt, iterations);
    return crypto.subtle.timingSafeEqual(actual, expected);
  } catch {
    return false;
  }
}

/** Timing parity for unknown handles: one full verification against a fixed dummy hash. */
export async function dummyVerify(): Promise<void> {
  await verifyPassword("masil-dummy-password", DUMMY_HASH);
}

/** "msl_" + base64url (no padding) of 32 random bytes = 47 chars. */
export function newSessionToken(): string {
  return "msl_" + toBase64Url(crypto.getRandomValues(new Uint8Array(32)));
}

/** Lowercase hex SHA-256 of the UTF-8 bytes of s. */
export async function sha256Hex(s: string): Promise<string> {
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(s)));
  let out = "";
  for (const b of digest) out += b.toString(16).padStart(2, "0");
  return out;
}
