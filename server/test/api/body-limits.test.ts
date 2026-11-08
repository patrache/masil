// Request body caps (pending-fixes item 3): the 64KB JSON cap and the 5MB upload cap are enforced on the bytes
// actually received, never on the client's Content-Length alone, and an over-cap body is never buffered whole.
import { env, SELF } from "cloudflare:test";
import { Hono } from "hono";
import { describe, expect, test } from "vitest";
import { createApp, JSON_BODY_MAX_BYTES, jsonParseCheck } from "../../src/app";
import type { AppEnv } from "../../src/env";
import { onError } from "../../src/lib/errors";
import { UPLOAD_MAX_BYTES } from "../../src/routes/uploads";
import { BASE, PNG_1X1, signup, uniqueIp, uniqueHandle } from "../helpers";

const UA = "masil/0.1.0 (test; test)";
const CHUNK = 64 * 1024;

/** A body stream of `total` bytes that records how many bytes the server pulled out of it. */
function countingStream(total: number, fill = 0x20, head?: Uint8Array) {
  const counter = { pulled: 0 };
  const stream = new ReadableStream<Uint8Array>({
    pull(ctrl) {
      if (counter.pulled >= total) return ctrl.close();
      const n = Math.min(CHUNK, total - counter.pulled);
      const chunk = new Uint8Array(n).fill(fill);
      if (head && counter.pulled === 0) chunk.set(head.subarray(0, n));
      counter.pulled += n;
      ctrl.enqueue(chunk);
    },
  });
  return { stream, counter };
}

/** "{" + spaces + "}" — valid JSON of exactly `size` bytes, so only a size check can reject it. */
const paddedJson = (size: number) => "{" + " ".repeat(size - 2) + "}";

function headers(extra: Record<string, string>, token?: string): Headers {
  const h = new Headers({ "User-Agent": UA, "CF-Connecting-IP": uniqueIp(), ...extra });
  if (token) h.set("Authorization", `Bearer ${token}`);
  return h;
}

async function errorCode(r: Response): Promise<string | undefined> {
  return ((await r.json()) as { error?: { code?: string } }).error?.code;
}

describe("64KB JSON cap", () => {
  test("forged small Content-Length with a >64KB body -> 413 (Content-Length is not trusted)", async () => {
    const r = await SELF.fetch(BASE + "/v1/auth/signup", {
      method: "POST",
      headers: headers({ "Content-Type": "application/json", "Content-Length": "2" }),
      body: paddedJson(200_000),
    });
    expect(r.status).toBe(413);
    expect(await errorCode(r)).toBe("payload_too_large");
  });

  test("chunked body (no Content-Length) over 64KB -> 413", async () => {
    const { stream } = countingStream(200_000);
    const r = await SELF.fetch(BASE + "/v1/auth/signup", {
      method: "POST",
      headers: headers({ "Content-Type": "application/json" }),
      body: stream,
    });
    expect(r.status).toBe(413);
    expect(await errorCode(r)).toBe("payload_too_large");
  });

  test("a huge body with a forged Content-Length is not buffered: reading stops just past the cap", async () => {
    const { stream, counter } = countingStream(50 * 1024 * 1024);
    const req = new Request(BASE + "/v1/auth/signup", {
      method: "POST",
      headers: headers({ "Content-Type": "application/json", "Content-Length": "2" }),
      body: stream,
    });
    const r = await createApp().fetch(req, env);
    expect(r.status).toBe(413);
    expect(counter.pulled).toBeLessThanOrEqual(JSON_BODY_MAX_BYTES + 2 * CHUNK);
  });

  test("non-JSON content type is capped too", async () => {
    const r = await SELF.fetch(BASE + "/v1/auth/signup", {
      method: "POST",
      headers: headers({ "Content-Type": "text/plain", "Content-Length": "2" }),
      body: "x".repeat(200_000),
    });
    expect(r.status).toBe(413);
  });

  test("jsonParseCheck enforces the cap on its own (does not rely on running after jsonBodyLimit)", async () => {
    const app = new Hono<AppEnv>();
    app.onError(onError);
    app.use("*", jsonParseCheck);
    app.post("/echo", async (c) => c.json({ keys: Object.keys(await c.req.json<object>()) }));
    const { stream, counter } = countingStream(50 * 1024 * 1024);
    const big = await app.fetch(
      new Request(BASE + "/echo", { method: "POST", headers: { "Content-Type": "application/json", "Content-Length": "2" }, body: stream }),
      env,
    );
    expect(big.status).toBe(413);
    expect(counter.pulled).toBeLessThanOrEqual(JSON_BODY_MAX_BYTES + 2 * CHUNK);
    // the route still sees the body after the check read it
    const ok = await app.fetch(
      new Request(BASE + "/echo", { method: "POST", headers: { "Content-Type": "application/json" }, body: '{"a":1,"b":2}' }),
      env,
    );
    expect(ok.status).toBe(200);
    expect(await ok.json()).toEqual({ keys: ["a", "b"] });
  });

  test("bodies at the cap and normal requests still pass", async () => {
    const atCap = await SELF.fetch(BASE + "/v1/auth/signup", {
      method: "POST",
      headers: headers({ "Content-Type": "application/json" }),
      body: paddedJson(JSON_BODY_MAX_BYTES),
    });
    expect(atCap.status).toBe(400); // passes the cap, then fails validation (handle/password missing)
    expect(await errorCode(atCap)).toBe("invalid_request");
    const handle = uniqueHandle();
    const ok = await SELF.fetch(BASE + "/v1/auth/signup", {
      method: "POST",
      headers: headers({ "Content-Type": "application/json" }),
      body: JSON.stringify({ handle, password: "correct horse battery" }),
    });
    expect(ok.status).toBe(201);
  });
});

describe("5MB upload cap", () => {
  const url = BASE + "/v1/uploads?purpose=post";

  test("forged small Content-Length with a >5MB body -> 413", async () => {
    const a = await signup();
    const body = new Uint8Array(UPLOAD_MAX_BYTES + 1);
    body.set(PNG_1X1);
    const r = await SELF.fetch(url, {
      method: "POST",
      headers: headers({ "Content-Type": "image/png", "Content-Length": String(PNG_1X1.length) }, a.token),
      body,
    });
    expect(r.status).toBe(413);
    expect(await errorCode(r)).toBe("payload_too_large");
  });

  test("a huge upload with a forged Content-Length is not buffered: reading stops just past 5MB", async () => {
    const a = await signup();
    const { stream, counter } = countingStream(200 * 1024 * 1024, 0, PNG_1X1);
    const req = new Request(url, {
      method: "POST",
      headers: headers({ "Content-Type": "image/png", "Content-Length": "100" }, a.token),
      body: stream,
    });
    const r = await createApp().fetch(req, env);
    expect(r.status).toBe(413);
    expect(counter.pulled).toBeLessThanOrEqual(UPLOAD_MAX_BYTES + 2 * CHUNK);
    const pending = await env.DB.prepare("SELECT COUNT(*) AS n FROM uploads WHERE owner_id = ?").bind(a.user.id).first<{ n: number }>();
    expect(pending!.n).toBe(0);
  });

  test("declared Content-Length over 5MB -> 413 before any byte is read", async () => {
    const a = await signup();
    const { stream, counter } = countingStream(16, 0, PNG_1X1);
    const req = new Request(url, {
      method: "POST",
      headers: headers({ "Content-Type": "image/png", "Content-Length": String(UPLOAD_MAX_BYTES + 1) }, a.token),
      body: stream,
    });
    const r = await createApp().fetch(req, env);
    expect(r.status).toBe(413);
    expect(counter.pulled).toBeLessThanOrEqual(CHUNK);
  });

  test("an upload of exactly 5MB, and a normal small upload, pass", async () => {
    const a = await signup();
    const exact = new Uint8Array(UPLOAD_MAX_BYTES);
    exact.set(PNG_1X1);
    const r = await SELF.fetch(url, { method: "POST", headers: headers({ "Content-Type": "image/png" }, a.token), body: exact });
    expect(r.status).toBe(201);
    expect(((await r.json()) as { upload: { size: number } }).upload.size).toBe(UPLOAD_MAX_BYTES);
    const small = await SELF.fetch(url, { method: "POST", headers: headers({ "Content-Type": "image/png" }, a.token), body: PNG_1X1 });
    expect(small.status).toBe(201);
    expect(((await small.json()) as { upload: { size: number } }).upload.size).toBe(PNG_1X1.length);
  });
});

// GET/HEAD cannot carry a body in the Fetch API (`new Request(getReq, { body })` throws), but workerd hands the
// Worker a non-null (possibly empty) body stream whenever the request has body framing (Content-Length or
// Transfer-Encoding). Such a body is ignored — never buffered into a rebuilt Request (that threw -> 500 on every route).
describe("GET/HEAD with body framing", () => {
  const cases: { name: string; extra: Record<string, string> }[] = [
    { name: "Content-Length: 0", extra: { "Content-Length": "0" } },
    { name: "Transfer-Encoding: chunked", extra: { "Transfer-Encoding": "chunked" } },
    { name: "declared Content-Length over the cap", extra: { "Content-Length": String(JSON_BODY_MAX_BYTES + 1) } },
    { name: "JSON Content-Type + Content-Length: 0", extra: { "Content-Type": "application/json", "Content-Length": "0" } },
    // stripInternalHeaders rebuilds the Request first (no body in init) — that path must survive a framed GET too
    { name: "X-Masil-* header + Content-Length: 0", extra: { "X-Masil-Auth": "{}", "Content-Length": "0" } },
  ];

  for (const { name, extra } of cases) {
    test(`${name}: answers exactly as without the header`, async () => {
      const a = await signup();
      const probes: { method: string; path: string; token?: string; want: number }[] = [
        { method: "GET", path: "/v1/meta", want: 200 },
        { method: "HEAD", path: "/v1/meta", want: 200 },
        { method: "GET", path: "/v1/me", want: 401 },
        { method: "GET", path: "/v1/me", token: a.token, want: 200 },
        { method: "HEAD", path: "/v1/me", token: a.token, want: 200 },
        { method: "GET", path: "/v1/timeline/following", token: a.token, want: 200 },
      ];
      for (const p of probes) {
        const plain = await SELF.fetch(BASE + p.path, { method: p.method, headers: headers({}, p.token) });
        const framed = await SELF.fetch(BASE + p.path, { method: p.method, headers: headers(extra, p.token) });
        expect({ ...p, status: plain.status }).toEqual({ ...p, status: p.want });
        expect({ ...p, status: framed.status }).toEqual({ ...p, status: p.want });
      }
    });
  }

  test("a GET carrying a huge streamed body: never 500, and the body is not pulled", async () => {
    // The Fetch API refuses to build a GET with a body, so present a POST-built request as GET — the same shape
    // (method GET, non-null body stream) workerd gives the Worker for a GET that arrives with a body.
    class GetWithBody extends Request {
      override get method() {
        return "GET";
      }
    }
    const a = await signup();
    for (const path of ["/v1/meta", "/v1/me"]) {
      const { stream, counter } = countingStream(50 * 1024 * 1024);
      const req = new GetWithBody(BASE + path, {
        method: "POST",
        headers: headers({ "Content-Type": "application/json" }, a.token),
        body: stream,
      });
      expect(req.method).toBe("GET");
      expect(req.body).not.toBeNull();
      const r = await createApp().fetch(req, env);
      expect({ path, status: r.status }).toEqual({ path, status: 200 });
      expect(counter.pulled).toBeLessThanOrEqual(JSON_BODY_MAX_BYTES + 2 * CHUNK);
    }
  });
});
