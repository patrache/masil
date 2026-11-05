// ULID (Crockford base32): 48-bit ms timestamp (10 chars) + 80-bit randomness (16 chars).
// Monotonic within one isolate (data-model 1.1): the same ms, or a clock going backwards, keeps the
// last timestamp and increments the random part; on 80-bit overflow the timestamp moves to lastMs + 1.

const CROCKFORD = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const TIME_LEN = 10;
const RAND_LEN = 16;
const RAND_MAX = (1n << 80n) - 1n;
const ID_RE = /^[0-7][0-9A-HJKMNP-TV-Z]{25}$/; // first char <= 7: timestamp fits in 48 bits

const INVITE_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"; // 56 chars, no 0 O o 1 l I
const INVITE_LEN = 8;
const INVITE_REJECT_AT = 256 - (256 % INVITE_ALPHABET.length); // 224: bytes >= this are resampled (no modulo bias)

let lastMs = -1;
let lastRand = 0n;

function encodeTime(ms: number): string {
  let out = "";
  let t = Math.floor(ms);
  for (let i = 0; i < TIME_LEN; i++) {
    out = CROCKFORD[t % 32] + out;
    t = Math.floor(t / 32);
  }
  return out;
}

function encodeRand(r: bigint): string {
  let out = "";
  for (let i = 0; i < RAND_LEN; i++) {
    out = CROCKFORD[Number(r & 31n)] + out;
    r >>= 5n;
  }
  return out;
}

function randomBits80(): bigint {
  let r = 0n;
  for (const b of crypto.getRandomValues(new Uint8Array(10))) r = (r << 8n) | BigInt(b);
  return r;
}

/** Monotonic ULID. Never pass a future ms: the module keeps the largest ms it has seen. */
export function newId(nowMs: number = Date.now()): string {
  if (nowMs > lastMs) {
    lastMs = nowMs;
    lastRand = randomBits80();
  } else if (lastRand >= RAND_MAX) {
    lastMs += 1;
    lastRand = randomBits80();
  } else {
    lastRand += 1n;
  }
  return encodeTime(lastMs) + encodeRand(lastRand);
}

export function isId(s: string): boolean {
  return ID_RE.test(s);
}

/** Smallest ULID at time ms (for "since N days" bounds). */
export function idFloor(ms: number): string {
  return encodeTime(ms) + "0".repeat(RAND_LEN);
}

/** 8 chars from an unambiguous 56-char alphabet, unbiased via rejection sampling. */
export function newInviteCode(): string {
  let out = "";
  while (out.length < INVITE_LEN) {
    for (const b of crypto.getRandomValues(new Uint8Array(INVITE_LEN * 2))) {
      if (b >= INVITE_REJECT_AT) continue;
      out += INVITE_ALPHABET[b % INVITE_ALPHABET.length];
      if (out.length === INVITE_LEN) break;
    }
  }
  return out;
}
